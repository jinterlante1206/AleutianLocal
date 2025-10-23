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
    Base class for RAG pipelines, containing shared methods for configuration,
    embedding retrieval, LLM calls, and prompt building.
    """
    def __init__(self, weaviate_client: weaviate.WeaviateClient, config: dict):
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
        # self.anthropic_api_key = self._read_secret("anthropic_api_key")

        # --- Validation ---
        if not self.embedding_url:
            raise ValueError("Embedding service URL not configured.")
        if not self.llm_url and self.llm_backend != "mock":
             raise ValueError(f"LLM service URL not configured for backend: {self.llm_backend}")
        logger.info(f"BaseRAGPipeline initialized for backend: {self.llm_backend}")


    def _read_secret(self, secret_name: str) -> str | None:
        """Reads a secret from the path /run/secrets/{secret_name}."""
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
        """Calls the embedding service to get a vector for the text."""
        if not text:
             logger.warning("Empty text passed to _get_embedding.")
             return []
        try:
            response = await self.http_client.post(self.embedding_url, json={"text": text})
            response.raise_for_status()
            data = response.json()
            if "vector" not in data or not isinstance(data["vector"], list):
                logger.error(f"Invalid embedding response format: {data}")
                raise ValueError("Invalid embedding response format")
            return data["vector"]
        except httpx.RequestError as e:
            logger.error(f"HTTP error calling embedding service at {self.embedding_url}: {e}")
            raise ConnectionError(f"Failed to connect to embedding service: {e}")
        except Exception as e:
            logger.error(f"Failed to get embedding: {e}", exc_info=True)
            raise RuntimeError(f"Embedding generation failed: {e}")

    # --- Moved from standard.py ---
    async def _call_llm(self, prompt: str) -> str:
        """Calls the configured LLM backend with configured parameters."""
        headers = {"Content-Type": "application/json"}
        payload = {}
        api_url = ""

        logger.debug(f"Calling LLM backend: {self.llm_backend}")

        # --- Backend Specific Logic with Params ---
        if self.llm_backend == "ollama":
            api_url = f"{self.llm_url}/api/generate"
            payload = {
                "model": self.ollama_model,
                "prompt": prompt,
                "stream": False,
                "options": {
                    "temperature": self.default_llm_params["temperature"],
                    "num_predict": self.default_llm_params["max_tokens"],
                    "top_k": self.default_llm_params["top_k"],
                    "top_p": self.default_llm_params["top_p"],
                    "stop": self.default_llm_params["stop"]
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
        """Builds the LLM prompt using the retrieved context and configured template."""
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

    async def run(self, query: str) -> tuple[str, list[dict]]:
        """Placeholder run method - subclasses must implement."""
        raise NotImplementedError("Subclasses must implement the 'run' method.")