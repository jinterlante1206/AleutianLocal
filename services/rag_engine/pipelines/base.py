"""
// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

Base RAG Pipeline Module.

This module provides the abstract foundation for all Retrieval-Augment-Generate
(RAG) pipelines within the Aleutian RAG Engine. It defines the core, shared
functionality that specific pipeline implementations (like 'standard' or
'reranking') will inherit.

Module-Level Components:
------------------------
- **Constants**: Defines default configurations for the LLM prompt template
  and generation parameters (temperature, max_tokens, etc.). These are read
  from environment variables, providing a central place for configuration.
- **BaseRAGPipeline (Class)**: An abstract class that manages:
    - Configuration and validation.
    - A shared asynchronous HTTP client (`httpx.AsyncClient`).
    - Secure secret reading (e.g., API keys).
    - Core RAG step implementations:
        1. `_get_embedding`: Fetches query vectors (The "R" in RAG).
        2. `_build_prompt`: Formats the prompt (The "A" in RAG).
        3. `_call_llm`: Gets the final answer (The "G" in RAG).
    - Session-aware data scoping logic:
        1. `_get_session_uuid`: Translates a human-readable session ID
           into a Weaviate database UUID.
        2. `_get_session_aware_filter`: Constructs the dynamic Weaviate
           filter to query both "Global" and "Session-Scoped" documents.

How it Fits:
------------
This file is the "engine" of the RAG service. The `server.py` file acts as the
API frontend, receiving requests from the orchestrator. When a request comes in,
`server.py` instantiates a *concrete* pipeline (like `StandardRAGPipeline` from
`standard.py`), which *inherits* from the `BaseRAGPipeline` defined here.

This base class provides all the tools the concrete pipeline needs to
perform its job, ensuring that all pipelines are consistent in how they
handle embeddings, LLM calls, and data scoping.
"""
import httpx
import logging
import os
import json
import weaviate
import weaviate.classes as wvc

logger = logging.getLogger(__name__)

# --- Strict RAG Mode Constants ---
# Relevance thresholds for strict mode filtering
# Score is sigmoid-normalized (0-1 range). 0.5 = neutral, higher = more relevant.
# 0.3 corresponds to raw logit of ~-0.85, accepting moderately relevant docs.
RERANK_SCORE_THRESHOLD = 0.3  # Minimum rerank score to consider relevant
DISTANCE_THRESHOLD = 0.8      # Maximum distance to consider relevant (lower is better)
NO_RELEVANT_DOCS_MESSAGE = "No relevant documents found. Use `aleutian populate vectordb <file>` to add documents."

# --- P8: History Injection Constants ---
# Maximum characters for answer content in history pseudo-documents
# Semantic meaning is usually in the first paragraph - truncate to reduce reranking cost
HISTORY_ANSWER_MAX_CHARS = int(os.getenv("HISTORY_ANSWER_MAX_CHARS", "300"))

# --- P8: Relevance Gate Constants ---
# Relevance gate prevents hallucination by checking if retrieved content is actually relevant.
# If the best reranked document scores below this threshold, we don't trust the results.
# 0.5 = strict (better to say "I don't know" than hallucinate)
RELEVANCE_GATE_THRESHOLD = float(os.getenv("RELEVANCE_GATE_THRESHOLD", "0.5"))
RELEVANCE_GATE_ENABLED = os.getenv("RELEVANCE_GATE_ENABLED", "true").lower() == "true"
LOW_RELEVANCE_MESSAGE = "I checked my knowledge base but couldn't find relevant information for your question. Could you rephrase or provide more context?"


# =============================================================================
# Document Format Contract (P8)
# =============================================================================
# This defines the explicit contract for document format used across the pipeline.
# Both _inject_history_as_documents and _rerank_docs must follow this contract.
#
# IMPORTANT: If you change this format, you MUST update both:
#   1. _inject_history_as_documents() - creates documents in this format
#   2. _rerank_docs() in reranking.py - consumes documents in this format
#
# Document Format:
# {
#     "properties": {
#         "content": str,     # Main text content
#         "source": str,      # Source identifier
#         "is_history": bool, # True if from conversation history (optional)
#         "turn_number": int, # Turn number for history docs (optional)
#         ...                 # Other properties allowed
#     },
#     "metadata": {
#         ...                 # Metadata fields (rerank_score, distance, etc.)
#     }
# }

def validate_document_format(doc: dict, context: str = "") -> bool:
    """
    Validates that a document matches the expected format contract.

    What it Does:
    Checks that the document has the required structure for reranking.
    This ensures format coupling issues are caught early.

    Parameters
    ----------
    doc : dict
        The document to validate.
    context : str
        Optional context for error messages (e.g., "history injection").

    Returns
    -------
    bool
        True if valid, raises ValueError if invalid.

    Raises
    ------
    ValueError
        If document doesn't match the expected format.

    Examples
    --------
    >>> doc = {"properties": {"content": "text", "source": "file.txt"}, "metadata": {}}
    >>> validate_document_format(doc)
    True
    """
    if not isinstance(doc, dict):
        raise ValueError(f"[{context}] Document must be dict, got {type(doc)}")

    if "properties" not in doc:
        raise ValueError(f"[{context}] Document missing 'properties' key: {doc.keys()}")

    props = doc["properties"]
    if not isinstance(props, dict):
        raise ValueError(f"[{context}] 'properties' must be dict, got {type(props)}")

    if "content" not in props:
        raise ValueError(f"[{context}] 'properties' missing 'content' key")

    return True


def create_document(
    content: str,
    source: str,
    metadata: dict | None = None,
    is_history: bool = False,
    turn_number: int | None = None,
    **extra_properties
) -> dict:
    """
    Factory function to create a document in the standard format.

    What it Does:
    Creates a document dict with the correct structure that both
    _inject_history_as_documents and _rerank_docs expect.

    Why it Exists:
    This function enforces the document format contract. Using this factory
    instead of manually constructing dicts ensures consistency and catches
    format changes at compile time rather than runtime.

    Parameters
    ----------
    content : str
        The main text content of the document.
    source : str
        Source identifier (filename, URL, or "conversation_history_turn_N").
    metadata : dict | None
        Optional metadata dict (will be created if None).
    is_history : bool
        True if this is a conversation history pseudo-document.
    turn_number : int | None
        Turn number for history documents.
    **extra_properties
        Additional properties to include in the document.

    Returns
    -------
    dict
        Document in the standard format.

    Examples
    --------
    >>> doc = create_document(
    ...     content="User asked about Motown...",
    ...     source="conversation_history_turn_1",
    ...     is_history=True,
    ...     turn_number=1
    ... )
    >>> doc["properties"]["content"]
    'User asked about Motown...'
    """
    props = {
        "content": content,
        "source": source,
        **extra_properties
    }

    if is_history:
        props["is_history"] = True
        if turn_number is not None:
            props["turn_number"] = turn_number

    meta = metadata if metadata is not None else {}
    if is_history:
        meta["is_history"] = True
        if turn_number is not None:
            meta["turn_number"] = turn_number

    return {
        "properties": props,
        "metadata": meta
    }

# --- Default Configurable Parameters ---
# Prompt Template
DEFAULT_PROMPT_TEMPLATE = """You are a helpful assistant. Answer the user's question based *only* on the provided context. If the context does not contain the answer, state that you don't have enough information from the provided documents. Do not use any prior knowledge.

Context:
{context}

Question: {query}
Answer:"""
PROMPT_TEMPLATE = os.getenv("RAG_PROMPT_TEMPLATE", DEFAULT_PROMPT_TEMPLATE)

# LLM Generation Parameters
try:
    LLM_DEFAULT_TEMPERATURE = float(os.getenv("LLM_DEFAULT_TEMPERATURE", "0.5"))
except ValueError:
    LLM_DEFAULT_TEMPERATURE = 0.5
    logger.warning("Invalid LLM_DEFAULT_TEMPERATURE, using 0.5")
try:
    LLM_DEFAULT_TOP_P = float(os.getenv("LLM_DEFAULT_TOP_P", "0.9"))
except ValueError:
    LLM_DEFAULT_TOP_P = 0.9
    logger.warning("Invalid LLM_DEFAULT_TOP_P, using 0.9")

LLM_DEFAULT_STOP_SEQUENCES_JSON = os.getenv("LLM_DEFAULT_STOP_SEQUENCES", '[]')
try:
    LLM_DEFAULT_STOP_SEQUENCES = json.loads(LLM_DEFAULT_STOP_SEQUENCES_JSON)
    if not isinstance(LLM_DEFAULT_STOP_SEQUENCES, list):
         raise ValueError("Must be a JSON list of strings")
except (json.JSONDecodeError, ValueError) as e:
     logger.warning(f"Invalid LLM_DEFAULT_STOP_SEQUENCES JSON: {e}. Using default [].")
     LLM_DEFAULT_STOP_SEQUENCES = []
try:
    LLM_DEFAULT_MAX_TOKENS = int(os.getenv("LLM_DEFAULT_MAX_TOKENS", "1024"))
except ValueError:
    LLM_DEFAULT_MAX_TOKENS = 1024
    logger.warning("Invalid LLM_DEFAULT_MAX_TOKENS, using 1024")
try:
    LLM_DEFAULT_TOP_K = int(os.getenv("LLM_DEFAULT_TOP_K", "40"))
except ValueError:
    LLM_DEFAULT_TOP_K = 40
    logger.warning("Invalid LLM_DEFAULT_TOP_K, using 40")
try:
    LLM_DEFAULT_TOP_P = float(os.getenv("LLM_DEFAULT_TOP_P", "0.9"))
except ValueError:
    LLM_DEFAULT_TOP_P = 0.9
    logger.warning("Invalid LLM_DEFAULT_TOP_P, using 0.9")
LLM_DEFAULT_STOP_SEQUENCES_JSON = os.getenv("LLM_DEFAULT_STOP_SEQUENCES", '[]')
try:
    LLM_DEFAULT_STOP_SEQUENCES = json.loads(LLM_DEFAULT_STOP_SEQUENCES_JSON)
    if not isinstance(LLM_DEFAULT_STOP_SEQUENCES, list):
         raise ValueError("Must be a JSON list of strings")
except (json.JSONDecodeError, ValueError) as e:
     logger.warning(f"Invalid LLM_DEFAULT_STOP_SEQUENCES JSON: {e}. Using default [].")
     LLM_DEFAULT_STOP_SEQUENCES = []


class BaseRAGPipeline:
    """
    Abstract base class for RAG pipelines.

    This class provides the core, shared functionality required for any RAG
    pipeline, including configuration management, client initialization
    (HTTP, Weaviate), and implementations for the fundamental RAG steps.

    Subclasses (like `StandardRAGPipeline` or `RerankingPipeline`) are expected
    to inherit from this class and implement the `run` method, which will
    orchestrate the flow using the helper methods provided here.

    Attributes
    ----------
    weaviate_client : weaviate.WeaviateClient
        An active and connected Weaviate client instance, passed from the
        FastAPI lifespan manager in `server.py`.
    config : dict
        A configuration dictionary containing URLs, backend types, and
        model names, passed from `server.py`.
    embedding_url : str
        The full URL to the embedding service (e.g.,
        'http://embedding-server:8000/embed').
    llm_backend : str
        The type of LLM backend to use (e.g., "ollama", "openai", "local").
    llm_url : str
        The base URL for the configured LLM backend.
    prompt_template : str
        The f-string template used to build the final prompt.
    ollama_model : str
        The model name to use if `llm_backend` is "ollama".
    openai_model : str
        The model name to use if `llm_backend` is "openai".
    default_llm_params : dict
        A dictionary of default generation parameters (temperature, top_k,
        etc.) to be used in LLM calls.
    http_client : httpx.AsyncClient
        A shared asynchronous HTTP client for making external API calls
        (to embeddings and LLM backends).
    openai_api_key : str | None
        The OpenAI API key, securely read from Podman/Docker secrets.

    Notes
    -----
    This class is designed to be abstract and should not be instantiated
    directly. It provides the "scaffolding" and shared tools. Concrete
    implementations must provide their own `run` method and, optionally,
    override helper methods (like `_search_weaviate_initial`) to define their
    specific retrieval strategy.
    """
    def __init__(self, weaviate_client: weaviate.WeaviateClient, config: dict):
        """
        Initializes the BaseRAGPipeline.

        This constructor sets up all necessary configurations, clients,
        and parameters needed for the pipeline to function. It validates
        that essential URLs are configured and securely reads API keys.

        Parameters
        ----------
        weaviate_client : weaviate.WeaviateClient
            An active and connected Weaviate client instance. This is
            injected by `server.py`.
        config : dict
            A dictionary containing all necessary configuration, such as
            service URLs and model names.

        Raises
        ------
        ValueError
            If the `embedding_url` is not provided in the config.
        ValueError
            If the `llm_service_url` is not provided and the backend
            is not "mock".
        """
        self.weaviate_client = weaviate_client
        self.config = config

        # --- Shared Configuration ---
        self.embedding_url = config.get("embedding_url")
        self.embedding_model = config.get("embedding_model", "nomic-embed-text-v2-moe")
        self.llm_backend = config.get("llm_backend_type", "ollama")
        self.llm_url = config.get("llm_service_url") # Base URL for the backend
        self.prompt_template = PROMPT_TEMPLATE # Use template read from env

        # Backend-specific models
        self.ollama_model = config.get("ollama_model", "gpt-oss")
        self.openai_model = config.get("openai_model", "gpt-4o-mini")
        # Add others as needed (local_model, claude_model etc.)

        # Default LLM params for generation
        self.default_llm_params = {
            "temperature": LLM_DEFAULT_TEMPERATURE,
            "max_tokens": LLM_DEFAULT_MAX_TOKENS,
            "top_k": LLM_DEFAULT_TOP_K,
            "top_p": LLM_DEFAULT_TOP_P,
            "stop": LLM_DEFAULT_STOP_SEQUENCES
        }

        # Shared HTTP client
        self.http_client = httpx.AsyncClient(timeout=180.0)

        # Read secrets needed for different backends
        self.openai_api_key = self._read_secret("openai_api_key")
        self.anthropic_api_key = self._read_secret("anthropic_api_key")

        # --- Thinking Mode Configuration (Anthropic only) ---
        # Parse once at init to avoid repeated env lookups and potential crashes
        self.enable_thinking = os.getenv("ENABLE_THINKING", "false").lower() == "true"
        try:
            self.thinking_budget = int(os.getenv("THINKING_BUDGET", "2048"))
            if self.thinking_budget <= 0:
                logger.warning("THINKING_BUDGET must be positive. Using default 2048.")
                self.thinking_budget = 2048
        except ValueError:
            raw_value = os.getenv("THINKING_BUDGET")
            logger.error(f"Invalid THINKING_BUDGET value '{raw_value}'. Using default 2048.")
            self.thinking_budget = 2048

        # --- Validation ---
        if not self.embedding_url:
            raise ValueError("Embedding service URL not configured.")
        if not self.llm_url and self.llm_backend != "mock":
             raise ValueError(f"LLM service URL not configured for backend: {self.llm_backend}")
        logger.info(f"BaseRAGPipeline initialized for backend: {self.llm_backend}")


    def _read_secret(self, secret_name: str) -> str | None:
        """
        Reads a secret from the path /run/secrets/{secret_name}.

        How it Fits:
        This is the standard, secure way for a containerized service (like
        this RAG engine) to access secrets (like 'openai_api_key') that
        are mounted by the container runtime (e.g., Podman secrets).

        Why it Does This:
        To avoid hardcoding sensitive API keys in environment variables,
        configuration files, or the container image itself, which is a
        major security risk.

        Parameters
        ----------
        secret_name : str
            The name of the secret file to read (e.g., "openai_api_key").

        Returns
        -------
        str | None
            The content of the secret file, stripped of whitespace, or
            None if the file cannot be found or read.
        """
        try:
            with open(f"/run/secrets/{secret_name}", 'r') as f:
                return f.read().strip()
        except FileNotFoundError:
            logger.warning(f"Secret file not found: /run/secrets/{secret_name}")
            return None
        except Exception as e:
            logger.error(f"Error reading secret {secret_name}: {e}")
            return None

    async def _get_embedding(self, text: str) -> list[float]:
        """
        Calls Ollama's embedding API to get a vector for the query text.

        What it Does:
        Takes a string of text (the user's query), prepends the "search_query:"
        prefix required by nomic-embed-text models, sends it to Ollama's
        /api/embed endpoint, and returns the resulting vector.

        How it Fits:
        This is the very first step of the "Retrieve" phase. The user's
        `query` is passed to this function to get a vector, which is
        then used in a `near_vector` search against the Weaviate database.

        Why it Does This:
        To translate the user's human-language query into the mathematical
        representation (a vector) that Weaviate uses to perform semantic
        similarity searches. The "search_query:" prefix tells the nomic
        model this is a query (not a document), enabling asymmetric search.

        Parameters
        ----------
        text : str
            The input text (e.g., the user's query) to embed.

        Returns
        -------
        list[float]
            The embedding vector for the input text.

        Raises
        ------
        RuntimeError
            If the embedding service returns an error or an
            invalid response format.
        ConnectionError
            If the HTTP client fails to connect to the embedding service.
        """
        if not text:
             logger.warning("Empty text passed to _get_embedding.")
             return []

        # Add search_query prefix for nomic-embed-text models
        prefixed_text = f"search_query: {text}"

        try:
            # Ollama /api/embed format
            payload = {
                "model": self.embedding_model,
                "input": [prefixed_text]
            }
            response = await self.http_client.post(self.embedding_url, json=payload)
            response.raise_for_status()
            data = response.json()

            # Ollama returns {"model": "...", "embeddings": [[...]]}
            if "embeddings" not in data or not isinstance(data["embeddings"], list):
                logger.error(f"Invalid Ollama embedding response format: {data}")
                raise ValueError("Invalid embedding response format")
            if len(data["embeddings"]) == 0:
                logger.error("Ollama returned empty embeddings array")
                raise ValueError("Empty embeddings returned")

            return data["embeddings"][0]
        except httpx.HTTPStatusError as e:
            error_detail = "No detail provided"
            try:
                error_detail = e.response.json().get("detail", e.response.text)
            except Exception:
                error_detail = e.response.text

            logger.error(
                f"Ollama embedding service at {self.embedding_url} returned status {e.response.status_code}. "
                f"Detail: {error_detail}",
                exc_info=True
            )
            raise RuntimeError(f"Embedding service failed: {error_detail}")
        except httpx.RequestError as httpxRequestError:
            logger.error(f"HTTP error calling Ollama embedding service at {self.embedding_url}: {httpxRequestError}")
            raise ConnectionError(f"Failed to connect to embedding service: {httpxRequestError}")
        except Exception as e:
            logger.error(f"Failed to get embedding: {e}", exc_info=True)
            raise RuntimeError(f"Embedding generation failed: {e}")

    # --- Moved from standard.py ---
    async def _call_llm(
        self,
        prompt: str,
        model_override: str = None,
        temperature: float = None,
        **kwargs
    ) -> str:
        """
        Calls the configured LLM backend with the final prompt.

        What it Does:
        Takes the fully constructed prompt (context + query) and sends it
        to the appropriate LLM backend specified in the configuration
        (e.g., "ollama", "openai"). It handles the different API request
        formats and parses their specific response formats.

        How it Fits:
        This is the "Generate" phase, the final step of the RAG pipeline.
        It takes the augmented prompt and generates the final text answer
        that is sent back to the user.

        Why it Does This:
        To abstract away the complexity of different LLM provider APIs.
        The rest of the pipeline doesn't need to know *how* to talk to
        Ollama vs. OpenAI; it just calls `_call_llm` and gets a string
        back.

        Parameters
        ----------
        prompt : str
            The final, formatted prompt to be sent to the LLM.
        model_override : str | None
            Optional model name to use instead of the default configured model.
            Useful for using different models for different pipeline stages
            (e.g., a specialized skeptic model for verification).
            If None, uses the default model for the configured backend.
        temperature : float | None
            Optional temperature override for this specific call.
            Useful for role-specific temperatures (e.g., lower for skeptic,
            higher for optimist). If None, uses the default from config.

        Returns
        -------
        str
            The text-only answer from the LLM.

        Raises
        ------
        ValueError
            If the `llm_backend_type` is not supported or an API key
            is missing.
        ConnectionError
            If the HTTP client fails to connect to the LLM backend.
        RuntimeError
            If the LLM backend returns an error or an invalid response.
        """
        headers = {"Content-Type": "application/json"}
        payload = {}
        api_url = ""
        generation_params = self.default_llm_params.copy()
        generation_params.update(kwargs)

        # Apply temperature override if provided (P4-2: role-specific temperatures)
        if temperature is not None:
            generation_params["temperature"] = temperature
            logger.debug(f"Using temperature override: {temperature}")

        logger.debug(f"Calling LLM backend: {self.llm_backend}")

        # --- Backend Specific Logic with Params ---
        if self.llm_backend in ["claude", "anthropic"]:
            if not self.anthropic_api_key:
                raise ValueError("Anthropic API key secret not configured")

            api_url = "https://api.anthropic.com/v1/messages"
            headers = {
                "x-api-key": self.anthropic_api_key,
                "anthropic-version": "2023-06-01",
                "content-type": "application/json"
            }

            # Use model_override if provided, otherwise use default from env
            default_claude_model = os.getenv("CLAUDE_MODEL", "claude-3-5-sonnet-20240620")
            effective_model = model_override if model_override else default_claude_model
            if model_override:
                logger.debug(f"Using model override: {model_override} (default: {default_claude_model})")

            # Note: For RAG, we treat the whole prompt as the user message for now.
            # To use "Prompt Caching" effectively, you would need to split
            # the 'context' out into a 'system' block here.
            payload = {
                "model": effective_model,
                "max_tokens": generation_params["max_tokens"],
                "messages": [{"role": "user", "content": prompt}]
            }

            # Handle temperature based on thinking mode (A1 fix)
            # When thinking mode is enabled, temperature must be None (API requirement)
            # When thinking mode is disabled, use the generation_params temperature
            if self.enable_thinking:
                payload["thinking"] = {
                    "type": "enabled",
                    "budget_tokens": self.thinking_budget
                }
                payload["temperature"] = None  # Required for thinking
            else:
                # Apply temperature override when thinking is disabled
                payload["temperature"] = generation_params["temperature"]
        elif self.llm_backend == "ollama":
            api_url = f"{self.llm_url}/api/generate"
            # Use model_override if provided, otherwise use default ollama_model
            effective_model = model_override if model_override else self.ollama_model
            if model_override:
                logger.debug(f"Using model override: {model_override} (default: {self.ollama_model})")
            payload = {
                "model": effective_model,
                "prompt": prompt,
                "stream": False,
                "options": {
                    "temperature": generation_params["temperature"],
                    "num_predict": generation_params["max_tokens"],
                    "top_k": generation_params["top_k"],
                    "top_p": generation_params["top_p"],
                    "stop": generation_params["stop"]
                }
            }
        elif self.llm_backend == "openai":
            if not self.openai_api_key:
                raise ValueError("OpenAI API key secret not configured")
            api_url = f"{self.llm_url}/chat/completions"
            headers["Authorization"] = f"Bearer {self.openai_api_key}"
            # Use model_override if provided, otherwise use default openai_model
            effective_model = model_override if model_override else self.openai_model
            if model_override:
                logger.debug(f"Using model override: {model_override} (default: {self.openai_model})")
            payload = {
                "model": effective_model,
                "messages": [{"role": "user", "content": prompt}],
                "temperature": generation_params["temperature"],
                "max_tokens": generation_params["max_tokens"],
                "top_p": generation_params["top_p"],
                "stop": generation_params["stop"]
            }
        elif self.llm_backend == "local":
            api_url = f"{self.llm_url}/completion"
            # Local backend: model_override could specify a different model path/name
            # Log if override is provided (less common for local)
            if model_override:
                logger.debug(f"Local backend model_override specified: {model_override}")
            payload = {
                "prompt": prompt,
                "n_predict": generation_params["max_tokens"],
                "temperature": generation_params["temperature"],
                "top_k": generation_params["top_k"],
                "top_p": generation_params["top_p"],
                "stop": generation_params["stop"]
            }
            # Add model to payload if override specified (llama.cpp server format)
            if model_override:
                payload["model"] = model_override
        else:
            raise ValueError(f"Unsupported LLM backend in RAG engine: {self.llm_backend}")

        # --- Make the API Call ---
        try:
            response = await self.http_client.post(api_url, json=payload, headers=headers)
            response.raise_for_status()

            # --- Parse Response ---
            resp_data = response.json()
            answer = ""
            logger.debug("Parsing LLM response...") # Changed log level
            if self.llm_backend == "ollama":
                answer = resp_data.get("response", "").strip()
            elif self.llm_backend == "openai":
                 if resp_data.get("choices") and len(resp_data["choices"]) > 0:
                      answer = resp_data["choices"][0].get("message", {}).get("content", "").strip()
            elif self.llm_backend == "local":
                 answer = resp_data.get("content", "").strip()
            else:
                 answer = "Response parsing not implemented"
                 logger.error(answer)

            if not answer:
                 logger.warning(f"LLM backend {self.llm_backend} returned an empty answer.")

            return answer

        except httpx.RequestError as e:
            logger.error(f"HTTP error calling LLM backend {self.llm_backend} at {api_url}: {e}")
            raise ConnectionError(f"Failed to connect to LLM backend: {e}")
        except Exception as e:
            logger.error(f"Failed to call or parse LLM backend {self.llm_backend}: {e}", exc_info=True)
            raise RuntimeError(f"LLM call failed: {e}")

    # --- Moved from standard.py ---
    def _build_prompt(self, query: str, context_docs: list[dict]) -> str:
        """
        Builds the LLM prompt using the retrieved context and template.

        What it Does:
        Takes the list of retrieved context documents and the user's
        original query, then formats them into a single string using the
        `PROMPT_TEMPLATE`.

        How it Fits:
        This is the "Augment" phase of RAG. It happens *after* the
        "Retrieve" step (e.g., `_search_weaviate_initial`) and *before*
        the "Generate" step (`_call_llm`).

        Why it Does This:
        To create the final, augmented prompt that instructs the LLM to
        answer the query *only* using the provided context. This is the
        core mechanism that prevents the LLM from "hallucinating" or
        using outside knowledge.

        Parameters
        ----------
        query : str
            The original query from the user.
        context_docs : list[dict]
            A list of document dictionaries, where each dictionary
            contains the 'content' and 'source' of a retrieved chunk.

        Returns
        -------
        str
            The fully formatted prompt ready to be sent to the LLM.
        """
        if not context_docs:
            logger.warning("No context documents found for query.")
            context_str = "No relevant context found." # Provide a clear indicator
        else:
            context_str = "\n\n---\n\n".join(
                [f"Source: {doc.get('source', 'Unknown')}\nContent: {doc.get('content', '')}"
                 for doc in context_docs]
            )

        try:
            prompt = self.prompt_template.format(context=context_str, query=query)
            return prompt
        except KeyError as e:
             logger.error(f"Failed to format prompt template. Missing key: {e}. Using basic format.")
             return f"Context:\n{context_str}\n\nQuestion: {query}\nAnswer:" # Fallback
        except Exception as e:
            logger.error(f"An unexpected error occurred during prompt formatting: {e}", exc_info=True)
            return f"Context:\n{context_str}\n\nQuestion: {query}\nAnswer:" # Fallback

    def _get_session_aware_filter(self, session_uuid: str | None) -> wvc.query.Filter:
        """
        Builds the session-aware filter for Weaviate queries.

        What it Does:
        Constructs a dynamic `wvc.query.Filter` object that searches
        for documents that are EITHER:
        1. **Global**: The `inSession` reference has no links (ref count = 0).
        2. **Session-Scoped**: The `inSession` property links to a
           `Session` object whose `session_id` matches the provided value.



        How it Fits:
        This is the core of the data-scoping feature. This function is
        called by the `_search_weaviate_initial` method in subclasses
        (like `standard.py`). The returned filter object is plugged
        directly into the `filters` argument of the
        `documents_collection.query.near_vector()` call.

        Why it Does This:
        This allows a *single* RAG query to seamlessly retrieve context
        from both the "global" pool of documents (available to all users)
        and the "session-scoped" documents that the user just
        uploaded in their current chat.

        Parameters
        ----------
        session_uuid : str | None
            The human-readable session ID (e.g., "my-chat-session").
            This is matched against Session.session_id. If None, the
            filter will *only* return global documents.

        Returns
        -------
        wvc.query.Filter
            The fully constructed, dynamic filter object to be used
            in a Weaviate query.

        Raises
        ------
        Exception
            Logs any exception during filter creation and gracefully
            defaults to the "global-only" filter.
        """
        # 1. Filter for GLOBAL documents (where inSession has no references)
        # NOTE: inSession is a cross-reference, not a scalar property.
        # For references, use by_ref_count() instead of is_none().
        # Documents without inSession set have a reference count of 0.
        global_filter = wvc.query.Filter.by_ref_count("inSession").equal(0)

        if not session_uuid:
            # If no session, only return global docs
            logger.info("Querying with scope: global-only (no session_id)")
            return global_filter

        try:
            # 2. Filter for SESSION-SCOPED documents
            # This looks for a reference link on the 'inSession' property
            # that contains the target session's internal UUID.
            session_filter = wvc.query.Filter.by_ref("inSession").by_property("session_id").equal(
                session_uuid
            )

            # 3. Combine them: (GLOBAL) OR (SESSION-SCOPED)
            final_filter = wvc.query.Filter.any_of([
                global_filter,
                session_filter
            ])
            logger.info(f"Querying with scope: global AND session UUID {session_uuid}")
            return final_filter

        except Exception as e:
            logger.error(f"Failed to build session filter, defaulting to global-only: {e}")
            return global_filter


    async def run(self, query: str, session_id: str | None = None, strict_mode: bool = True) -> tuple[str, list[dict]]:
        """
        Abstract 'run' method for the RAG pipeline.

        What it Does:
        This is the main entry point for a pipeline. Subclasses
        (e.g., `StandardRAGPipeline`, `RerankingPipeline`) *must*
        implement this method.

        How it Fits:
        This method is called directly by the FastAPI endpoints in
        `server.py` (e.g., `run_standard_rag`). It is responsible for
        orchestrating all the steps:
        1. `_get_embedding`
        2. `_search_weaviate_initial` (or similar)
        3. `_rerank_docs` (if applicable)
        4. `_build_prompt`
        5. `_call_llm`

        Why it Does This:
        It defines a standard "contract" or interface for all
        pipelines. This ensures that `server.py` can treat all
        pipelines identically, simply calling `.run(query, session_id, strict_mode)`
        regardless of their internal complexity.

        Parameters
        ----------
        query : str
            The user's query.
        session_id : str | None
            The current session ID, passed from the orchestrator.
        strict_mode : bool
            If True, only answer from documents. If no relevant docs,
            return NO_RELEVANT_DOCS_MESSAGE instead of using LLM fallback.
            Default: True (strict mode).

        Returns
        -------
        tuple[str, list[dict]]
            A tuple containing the string answer and a list of
            source document dictionaries.

        Raises
        ------
        NotImplementedError
            If a subclass fails to implement this method.
        """
        raise NotImplementedError("Subclasses must implement the 'run' method.")

    async def retrieve_only(
        self,
        query: str,
        session_id: str | None = None,
        strict_mode: bool = True,
        max_chunks: int = 5
    ) -> tuple[list[dict], str, bool]:
        """
        Retrieval-only mode: returns documents without LLM generation.

        What it Does:
        Performs only the retrieval and reranking steps of the RAG pipeline,
        returning the raw document content instead of generating an LLM answer.
        This enables the Go orchestrator to stream responses while having
        full access to the actual document content.

        How it Fits:
        This method is called by the `/rag/retrieve/{pipeline}` endpoint in
        `server.py`. It supports the streaming integration where:
        1. Python RAG retrieves and reranks documents (this method)
        2. Go orchestrator receives raw document content
        3. Go orchestrator streams LLM response with full document context

        Why it Does This:
        The original RAG flow had a "two-LLM" problem where Python generated
        an answer, then Go used that answer as "context" for a second LLM.
        The second LLM would judge Python's answer as insufficient, saying
        "no documents found" even when sources were displayed. This method
        fixes that by returning the actual documents.

        Parameters
        ----------
        query : str
            The user's query to retrieve documents for.
        session_id : str | None
            The current session ID for session-scoped document filtering.
        strict_mode : bool
            If True, only return documents above relevance threshold.
            Default: True.
        max_chunks : int
            Maximum number of document chunks to return.
            Default: 5 (matches top_k_final in reranking).

        Returns
        -------
        tuple[list[dict], str, bool]
            A tuple containing:
            - chunks: List of document dictionaries with keys:
                - content: The document text
                - source: The source file/URL
                - rerank_score: Optional relevance score
            - context_text: Formatted string for LLM context with
              "[Document N: source]" headers for clear citation
            - has_relevant_docs: Boolean indicating if relevant documents
              were found (respects strict_mode threshold)

        Raises
        ------
        NotImplementedError
            If a subclass does not implement this method.

        Examples
        --------
        >>> pipeline = RerankingPipeline(weaviate_client, config)
        >>> chunks, context, has_docs = await pipeline.retrieve_only(
        ...     "What is George Washington known for?",
        ...     session_id="abc123",
        ...     strict_mode=True,
        ...     max_chunks=5
        ... )
        >>> print(has_docs)
        True
        >>> print(context[:100])
        [Document 1: washington_biography.md]
        George Washington was the first President of the United...
        """
        raise NotImplementedError(
            "Subclasses must implement 'retrieve_only' for retrieval-only mode. "
            "Currently only RerankingPipeline supports this."
        )

    def _inject_history_as_documents(
        self,
        documents: list[dict],
        relevant_history: list[dict] | None
    ) -> list[dict]:
        """
        Converts conversation history turns into pseudo-documents for unified reranking.

        What it Does:
        Takes the relevant conversation history from P7 semantic memory and formats
        each turn as a "pseudo-document" that can compete with real documents in
        reranking. This allows the cross-encoder reranker to evaluate whether the
        user's previous conversation is more relevant than retrieved documents.

        How it Fits:
        This is part of the P8 conversational context enhancement. It happens AFTER
        initial document retrieval and BEFORE reranking. The history pseudo-documents
        are added to the document pool, then the reranker scores all sources equally.

        Why it Does This:
        For follow-up queries like "tell me more", the previous conversation turn
        may be the most relevant context. By injecting history into the reranking
        pool, we let the cross-encoder determine if history is more relevant than
        documents, rather than treating them separately.

        Parameters
        ----------
        documents : list[dict]
            The list of retrieved documents from Weaviate. Each dict contains
            'content', 'source', and optionally 'rerank_score'.
        relevant_history : list[dict] | None
            The relevant conversation history from P7 semantic memory. Each dict
            contains 'question', 'answer', 'turn_number', and 'similarity_score'.
            May be None or empty if no history is available.

        Returns
        -------
        list[dict]
            The combined list of documents and history pseudo-documents.
            History documents have:
            - content: "Previous conversation:\\nQ: {question}\\nA: {truncated_answer}"
            - source: "conversation_history_turn_{N}"
            - is_history: True
            - turn_number: The original turn number
            - similarity_score: The P7 similarity score

        Notes
        -----
        - Answers are truncated to HISTORY_ANSWER_MAX_CHARS (default 300) to
          reduce reranking cost and prevent history from dominating.
        - History documents are clearly marked with is_history=True for
          downstream processing (e.g., citation formatting).

        Examples
        --------
        >>> history = [{"question": "What is Motown?", "answer": "Motown was...", "turn_number": 1}]
        >>> docs = [{"content": "Document about music", "source": "music.md"}]
        >>> combined = pipeline._inject_history_as_documents(docs, history)
        >>> len(combined)
        2
        >>> combined[1]["is_history"]
        True
        """
        if not relevant_history:
            return documents

        history_docs = []
        for turn in relevant_history:
            # Truncate long answers - semantic meaning is usually in first paragraph
            answer = turn.get("answer", "")
            if len(answer) > HISTORY_ANSWER_MAX_CHARS:
                answer = answer[:HISTORY_ANSWER_MAX_CHARS] + "..."

            question = turn.get("question", "")
            turn_number = turn.get("turn_number")

            # Format as pseudo-document using the factory function
            # This ensures format consistency with _rerank_docs expectations
            content = f"Previous conversation:\nQ: {question}\nA: {answer}"

            history_doc = create_document(
                content=content,
                source=f"conversation_history_turn_{turn_number or 'unknown'}",
                metadata={"similarity_score": turn.get("similarity_score", 0.0)},
                is_history=True,
                turn_number=turn_number
            )
            history_docs.append(history_doc)

        logger.debug(
            f"Injected {len(history_docs)} history pseudo-documents into pool of {len(documents)} documents"
        )

        return documents + history_docs

    def _format_sources_with_history(self, documents: list[dict]) -> str:
        """
        Formats document sources with special handling for history pseudo-documents.

        What it Does:
        Creates a formatted string of sources where history pseudo-documents are
        marked as "[Conversation Turn N]" and regular documents are marked as
        "[Document N: source]".

        How it Fits:
        This is called when building the LLM prompt after reranking. It ensures
        that sources from conversation history are clearly distinguished from
        retrieved documents in the final answer.

        Why it Does This:
        Users need to understand which parts of the answer come from their previous
        conversation vs. retrieved documents. Clear citation formatting prevents
        confusion and enables proper attribution.

        Parameters
        ----------
        documents : list[dict]
            The reranked list of documents, potentially including history
            pseudo-documents (marked with is_history=True).

        Returns
        -------
        str
            A formatted string with all sources, suitable for inclusion in the
            LLM prompt or response metadata.

        Examples
        --------
        >>> docs = [
        ...     {"content": "Doc content", "source": "file.md"},
        ...     {"content": "History content", "source": "turn_1", "is_history": True, "turn_number": 1}
        ... ]
        >>> formatted = pipeline._format_sources_with_history(docs)
        >>> print(formatted)
        [Document 1: file.md]
        Doc content

        ---

        [Conversation Turn 1]
        History content
        """
        formatted_parts = []

        doc_counter = 1
        for doc in documents:
            # Handle both flat and nested structures
            props = doc.get("properties", doc)
            meta = doc.get("metadata", {})

            is_history = props.get("is_history") or meta.get("is_history", False)
            content = props.get("content", "")
            source = props.get("source", "Unknown")
            turn_num = props.get("turn_number") or meta.get("turn_number", "?")

            if is_history:
                formatted_parts.append(
                    f"[Conversation Turn {turn_num}]\n{content}"
                )
            else:
                formatted_parts.append(
                    f"[Document {doc_counter}: {source}]\n{content}"
                )
                doc_counter += 1

        return "\n\n---\n\n".join(formatted_parts)

    def _check_relevance_gate(
        self,
        reranked_docs: list[dict],
        has_history: bool = False
    ) -> tuple[list[dict], bool, str | None]:
        """
        Checks if reranked documents pass the relevance threshold gate.

        What it Does:
        Examines the rerank scores of the top documents. If the best score is below
        the RELEVANCE_GATE_THRESHOLD (default 0.5), the documents are considered
        "garbage" and we shouldn't use them to generate an answer.

        How it Fits:
        This is called AFTER reranking and BEFORE generation. It acts as a safety
        valve to prevent hallucination when retrieval fails to find relevant content.

        Why it Does This:
        Without this check, the LLM might hallucinate an answer based on irrelevant
        documents that happened to be the "best" matches in a bad retrieval. Better
        to honestly say "I don't know" than to confidently provide wrong information.

        Parameters
        ----------
        reranked_docs : list[dict]
            The reranked documents with metadata containing rerank_score.
        has_history : bool
            True if conversation history is available as fallback context.

        Returns
        -------
        tuple[list[dict], bool, str | None]
            - filtered_docs: Documents above threshold (may be empty)
            - passed_gate: True if at least one doc passed the threshold
            - message: If gate failed, a user-friendly message; else None

        Notes
        -----
        - If RELEVANCE_GATE_ENABLED is False, always passes.
        - If has_history is True and gate fails, we still allow proceeding
          (history provides context even if documents don't).

        Examples
        --------
        >>> docs = [{"metadata": {"rerank_score": 0.3}}]  # Below 0.5 threshold
        >>> filtered, passed, msg = pipeline._check_relevance_gate(docs, has_history=False)
        >>> passed
        False
        >>> msg
        "I checked my knowledge base but couldn't find relevant information..."
        """
        if not RELEVANCE_GATE_ENABLED:
            return reranked_docs, True, None

        if not reranked_docs:
            if has_history:
                # No docs but we have history - proceed with history only
                logger.info("Relevance gate: no documents, but history available")
                return [], True, None
            else:
                logger.info("Relevance gate: no documents and no history")
                return [], False, LOW_RELEVANCE_MESSAGE

        def _get_score(doc: dict) -> float:
            """Get rerank_score handling both Weaviate metadata objects and plain dicts."""
            meta = doc.get("metadata")
            if meta is None:
                return 0.0
            # Try attribute access first (Weaviate MetadataReturn objects)
            if hasattr(meta, "rerank_score"):
                return meta.rerank_score or 0.0
            # Fall back to dict access (history pseudo-documents)
            if isinstance(meta, dict):
                return meta.get("rerank_score", 0.0)
            return 0.0

        def _is_history(doc: dict) -> bool:
            """Check if doc is a history pseudo-document, handling both metadata formats."""
            props = doc.get("properties", {})
            if isinstance(props, dict) and props.get("is_history"):
                return True
            meta = doc.get("metadata")
            if meta is None:
                return False
            # Try attribute access first (Weaviate MetadataReturn objects)
            if hasattr(meta, "is_history"):
                return bool(getattr(meta, "is_history", False))
            # Fall back to dict access (history pseudo-documents)
            if isinstance(meta, dict):
                return bool(meta.get("is_history", False))
            return False

        # Get the best rerank score
        best_score = 0.0
        for doc in reranked_docs:
            score = _get_score(doc)
            if score > best_score:
                best_score = score

        logger.debug(f"Relevance gate: best_score={best_score:.3f}, threshold={RELEVANCE_GATE_THRESHOLD}")

        if best_score < RELEVANCE_GATE_THRESHOLD:
            if has_history:
                # Below threshold but we have history - proceed cautiously
                logger.info(f"Relevance gate: below threshold ({best_score:.3f} < {RELEVANCE_GATE_THRESHOLD}), but history available")
                # Filter to only include history docs (if any)
                history_only = [d for d in reranked_docs if _is_history(d)]
                return history_only if history_only else reranked_docs, True, None
            else:
                logger.info(f"Relevance gate FAILED: best_score={best_score:.3f} < threshold={RELEVANCE_GATE_THRESHOLD}")
                return [], False, LOW_RELEVANCE_MESSAGE

        # Filter docs to only those above threshold
        filtered = [doc for doc in reranked_docs if _get_score(doc) >= RELEVANCE_GATE_THRESHOLD]

        logger.debug(f"Relevance gate passed: {len(filtered)}/{len(reranked_docs)} docs above threshold")
        return filtered if filtered else reranked_docs, True, None