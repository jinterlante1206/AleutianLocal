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



RAG Pipeline with Reranking using a Sentence Transformers Cross-Encoder.

This module defines a RAG pipeline that enhances standard retrieval by adding
a reranking step. After an initial retrieval of potentially relevant documents
from the vector store, a more computationally intensive Cross-Encoder model
is used to score the relevance of each document specifically against the query.
Only the top-scoring documents after reranking are passed to the Language Model
for final answer generation.

This approach aims to improve the quality of the context provided to the LLM,
leading to more accurate and relevant answers, at the cost of slightly increased
latency due to the reranking step.

The reranker model is loaded once when the module is imported to optimize
performance for subsequent requests. It attempts to utilize MPS (Metal Performance
Shaders) on compatible Apple Silicon hardware if available, otherwise defaulting
to CPU. Configuration for the reranker model name and retrieval limits can be
set via environment variables.


"""
import asyncio
import logging
import os
import torch
from opentelemetry import trace
from sentence_transformers import CrossEncoder

# Import the base class
from .base import BaseRAGPipeline
# Import Weaviate classes needed for search override
import weaviate
import weaviate.classes as wvc
from weaviate.exceptions import WeaviateQueryException

logger = logging.getLogger(__name__)

# Constants
DEFAULT_RERANK_INITIAL_K = 20
DEFAULT_RERANK_FINAL_K = 5
DEFAULT_RERANKER_MODEL = "cross-encoder/ms-marco-MiniLM-L-6-v2"
RERANKER_MODEL_NAME = os.getenv("RERANKER_MODEL", DEFAULT_RERANKER_MODEL)
reranker_model = None
reranker_device = None

def _load_reranker_model():
    """
    Loads the Sentence Transformers CrossEncoder model specified by the
    RERANKER_MODEL environment variable.

    Attempts to load the model onto the most performant available device,
    prioritizing MPS (Apple Silicon GPU) if available, otherwise CPU.

    Handles potential errors during model loading and logs the outcome.

    Returns:
        tuple: A tuple containing the loaded CrossEncoder model instance
               (or None if loading failed) and the device string ('mps' or 'cpu').
    """
    global reranker_model, reranker_device # Allow modification of global variables

    target_device = 'cpu' # Default to CPU
    try:
        # Check for MPS (Apple Silicon GPU)
        if torch.backends.mps.is_available() and torch.backends.mps.is_built():
            target_device = 'mps'
            logger.info(f"MPS device detected. Attempting to load reranker model '{RERANKER_MODEL_NAME}' onto MPS.")
        # NOTE: Add elif torch.cuda.is_available(): here if CUDA support is needed
        else:
            logger.info(f"MPS/CUDA not available. Loading reranker model '{RERANKER_MODEL_NAME}' onto CPU.")

        # Load the CrossEncoder model
        # The 'device' argument tells Sentence Transformers where to place the model.
        # max_length can be tuned depending on expected passage length vs memory.
        model = CrossEncoder(RERANKER_MODEL_NAME, max_length=512, device=target_device)

        logger.info(f"Successfully loaded reranker model '{RERANKER_MODEL_NAME}' on device '{target_device}'.")
        return model, target_device

    except ImportError:
        logger.error(f"Failed to load reranker: 'sentence-transformers' or 'torch' library not found. "
                     f"Please install them (`pip install sentence-transformers torch`). Reranking disabled.")
        return None, 'cpu' # Return None, default device
    except Exception as e:
        logger.error(f"Failed to load reranker model '{RERANKER_MODEL_NAME}' on device '{target_device}': {e}. "
                     f"Reranking will be disabled.", exc_info=True)
        return None, 'cpu'

# --- Load the model when the module is first imported ---
reranker_model, reranker_device = _load_reranker_model()


class RerankingPipeline(BaseRAGPipeline):
    """
    Implements RAG with reranking, inheriting common methods from BaseRAGPipeline.
    Uses a Sentence Transformers CrossEncoder model for the reranking step.
    """
    def __init__(self, weaviate_client: weaviate.WeaviateClient, config: dict):
        super().__init__(weaviate_client, config)
        self.reranker = reranker_model
        self.top_k_initial = int(config.get("rerank_initial_k", DEFAULT_RERANK_INITIAL_K))
        self.top_k_final = int(config.get("rerank_final_k", DEFAULT_RERANK_FINAL_K))
        if self.top_k_final >= self.top_k_initial:
            logger.warning(
                f"Rerank final K ({self.top_k_final}) is >= initial K ({self.top_k_initial}). "
                f"Reranking might not reduce documents effectively.")
        logger.info(
            f"RerankingPipeline initialized: Initial K={self.top_k_initial}, Final K={self.top_k_final}.")
        if not self.reranker:
            logger.warning("Reranker model is not available, reranking step will be skipped.")

    async def _search_weaviate_initial(self, query_vector: list[float]) -> list[dict]:
        """ Performs the initial wider search for reranking. """
        if not query_vector: return []
        try:
            documents_collection = self.weaviate_client.collections.get("Document")
            response = await documents_collection.query.near_vector(
                near_vector=query_vector,
                limit=self.top_k_initial, # Use initial K
                return_metadata=wvc.query.MetadataQuery(distance=True),
                return_properties=["content", "source"]
            )
            context_docs = [obj.properties for obj in response.objects]
            logger.info(f"Retrieved {len(context_docs)} initial documents from Weaviate (limit={self.top_k_initial})")
            return context_docs
        except WeaviateQueryException as e:
            logger.error(f"Weaviate initial query failed: {e}")
            raise RuntimeError(f"Weaviate initial search failed: {e}")
        except Exception as e:
            logger.error(f"Failed initial Weaviate search: {e}", exc_info=True)
            raise RuntimeError(f"Weaviate interaction failed: {e}")

    async def _rerank_docs(self, query: str, initial_docs: list[dict]) -> list[dict]:
        """Reranks the initial documents using the cross-encoder."""
        if not self.reranker or not initial_docs:
            logger.warning("Skipping reranking (no model or no initial docs).")
            # Return top N of the initial results if reranker isn't available
            return initial_docs[:self.top_k_final]

        logger.debug(f"Preparing {len(initial_docs)} passages for reranking...")
        passages = [doc.get("content", "") for doc in initial_docs]
        query_passage_pairs = [[query, passage] for passage in passages if passage]

        if not query_passage_pairs:
             logger.warning("No valid passages found for reranking.")
             return []

        try:
             logger.debug(f"Reranking {len(query_passage_pairs)} pairs...")
             # Consider asyncio.to_thread if predict is slow
             scores = await asyncio.to_thread(self.reranker.predict, query_passage_pairs)
             logger.debug("Reranking complete.")
        except Exception as e:
             logger.error(f"Error during reranker prediction: {e}. Skipping reranking.", exc_info=True)
             return initial_docs[:self.top_k_final] # Fallback

        valid_initial_docs = [doc for doc in initial_docs if doc.get("content", "")]
        if len(scores) != len(valid_initial_docs):
             logger.error(f"Mismatch score/passage count. Skipping reranking.")
             return initial_docs[:self.top_k_final] # Fallback

        scored_docs = list(zip(scores, valid_initial_docs))
        scored_docs.sort(key=lambda x: x[0], reverse=True)

        reranked_docs = [doc for score, doc in scored_docs[:self.top_k_final]]
        logger.info(f"Reranked from {len(initial_docs)} down to {len(reranked_docs)} documents.")
        return reranked_docs

    async def run(self, query: str) -> str:
        """Executes the RAG pipeline with reranking."""
        tracer = trace.get_tracer("aleutian.rag.reranking")
        with tracer.start_as_current_span("reranking_pipeline.run") as span:
            span.set_attribute("query.length", len(query))
            logger.info(f"Reranking RAG run started...")  # Keep logs

            with tracer.start_as_current_span("get_embedding"):
                query_vector = await self._get_embedding(query)

            with tracer.start_as_current_span("initial_search"):
                initial_docs = await self._search_weaviate_initial(query_vector)
                span.set_attribute("retrieved.initial_count", len(initial_docs))

            with tracer.start_as_current_span("rerank_docs"):
                context_docs = await self._rerank_docs(query, initial_docs)
                span.set_attribute("retrieved.final_count", len(context_docs))

            with tracer.start_as_current_span("build_prompt"):
                prompt = self._build_prompt(query, context_docs)
                span.set_attribute("prompt.length", len(prompt))

            with tracer.start_as_current_span("call_llm"):
                span.set_attribute("llm.backend", self.llm_backend)
                answer = await self._call_llm(prompt)
                span.set_attribute("answer.length", len(answer))

            logger.info("Reranking RAG run finished.")
            return answer