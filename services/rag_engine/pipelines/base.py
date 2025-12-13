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
from weaviate.exceptions import WeaviateQueryException

logger = logging.getLogger(__name__)

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

LLM_DEFAULT_STOP_SEQUENCES_JSON = os.getenv("LLM_DEFAULT_STOP_SEQUENCES", '["\\n"]')
try:
    LLM_DEFAULT_STOP_SEQUENCES = json.loads(LLM_DEFAULT_STOP_SEQUENCES_JSON)
    if not isinstance(LLM_DEFAULT_STOP_SEQUENCES, list):
         raise ValueError("Must be a JSON list of strings")
except (json.JSONDecodeError, ValueError) as e:
     logger.warning(f"Invalid LLM_DEFAULT_STOP_SEQUENCES JSON: {e}. Using default ['\\n'].")
     LLM_DEFAULT_STOP_SEQUENCES = ["\n"]
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
LLM_DEFAULT_STOP_SEQUENCES_JSON = os.getenv("LLM_DEFAULT_STOP_SEQUENCES", '["\\n"]')
try:
    LLM_DEFAULT_STOP_SEQUENCES = json.loads(LLM_DEFAULT_STOP_SEQUENCES_JSON)
    if not isinstance(LLM_DEFAULT_STOP_SEQUENCES, list):
         raise ValueError("Must be a JSON list of strings")
except (json.JSONDecodeError, ValueError) as e:
     logger.warning(f"Invalid LLM_DEFAULT_STOP_SEQUENCES JSON: {e}. Using default ['\\n'].")
     LLM_DEFAULT_STOP_SEQUENCES = ["\n"]


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
        Calls the embedding service to get a vector for the text.

        What it Does:
        Takes a string of text, sends it to the `embedding-server` via an
        HTTP POST request, and returns the resulting vector (list of floats).

        How it Fits:
        This is the very first step of the "Retrieve" phase. The user's
        `query` is passed to this function to get a vector, which is
        then used in a `near_vector` search against the Weaviate database.

        Why it Does This:
        To translate the user's human-language query into the mathematical
        representation (a vector) that Weaviate uses to perform semantic
        similarity searches.

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
        try:
            response = await self.http_client.post(self.embedding_url, json={"texts": [text,]})
            response.raise_for_status()
            data = response.json()
            if "vectors" not in data or not isinstance(data["vectors"], list):
                logger.error(f"Invalid embedding response format: {data}")
                raise ValueError("Invalid embedding response format")
            return data["vectors"][0]
        except httpx.HTTPStatusError as e:
            error_detail = "No detail provided"
            try:
                error_detail = e.response.json().get("detail", e.response.text)
            except Exception:
                error_detail = e.response.text

            logger.error(
                f"Embedding service at {self.embedding_url} returned status {e.response.status_code}. "
                f"Detail: {error_detail}",
                exc_info=True
            )
            raise RuntimeError(f"Embedding service failed: {error_detail}")
        except httpx.RequestError as httpxRequestError:
            logger.error(f"HTTP error calling embedding service at {self.embedding_url}: {httpxRequestError}")
            raise ConnectionError(f"Failed to connect to embedding service: {httpxRequestError}")
        except Exception as e:
            logger.error(f"Failed to get embedding: {e}", exc_info=True)
            raise RuntimeError(f"Embedding generation failed: {e}")

    # --- Moved from standard.py ---
    async def _call_llm(self, prompt: str, model_override: str=None, **kwargs) -> str:
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

            # Note: For RAG, we treat the whole prompt as the user message for now.
            # To use "Prompt Caching" effectively, you would need to split
            # the 'context' out into a 'system' block here.
            payload = {
                "model": os.getenv("CLAUDE_MODEL", "claude-3-5-sonnet-20240620"),
                "max_tokens": self.default_llm_params["max_tokens"],
                "messages": [{"role": "user", "content": prompt}]
            }

            # Example: Basic "Thinking" support for RAG
            if os.getenv("ENABLE_THINKING") == "true":
                payload["thinking"] = {
                    "type": "enabled",
                    "budget_tokens": int(os.getenv("THINKING_BUDGET", 2048))
                }
                payload["temperature"] = None  # Required for thinking
        elif self.llm_backend == "ollama":
            api_url = f"{self.llm_url}/api/generate"
            payload = {
                "model": self.ollama_model,
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
            payload = {
                "model": self.openai_model,
                "messages": [{"role": "user", "content": prompt}],
                "temperature": self.default_llm_params["temperature"],
                "max_tokens": self.default_llm_params["max_tokens"],
                "top_p": self.default_llm_params["top_p"],
                "stop": self.default_llm_params["stop"]
            }
        elif self.llm_backend == "local":
            api_url = f"{self.llm_url}/completion"
            payload = {
                "prompt": prompt,
                "n_predict": self.default_llm_params["max_tokens"],
                "temperature": self.default_llm_params["temperature"],
                "top_k": self.default_llm_params["top_k"],
                "top_p": self.default_llm_params["top_p"],
                "stop": self.default_llm_params["stop"]
            }
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
        1. **Global**: The `inSession` property is `None` (null).
        2. **Session-Scoped**: The `inSession` property links to a
           `Session` object whose internal `id` (UUID) matches the
           provided `session_uuid`.



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
            The internal Weaviate UUID for the *current* session, as
            retrieved by `_get_session_uuid`. If None, the filter
            will *only* return global documents.

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
        # 1. Filter for GLOBAL documents (where inSession is null)
        global_filter = wvc.query.Filter.by_property("inSession").is_none(True)

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


    async def run(self, query: str, session_id: str | None = None) -> tuple[str, list[dict]]:
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
        pipelines identically, simply calling `.run(query, session_id)`
        regardless of their internal complexity.

        Parameters
        ----------
        query : str
            The user's query.
        session_id : str | None
            The current session ID, passed from the orchestrator.

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