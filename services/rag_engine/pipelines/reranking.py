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
import math
import os
import torch
from opentelemetry import trace
from sentence_transformers import CrossEncoder

# Import the base class and strict mode constants
from .base import BaseRAGPipeline, RERANK_SCORE_THRESHOLD, NO_RELEVANT_DOCS_MESSAGE, validate_document_format
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


def _sigmoid(x: float) -> float:
    """
    Convert raw logit score to normalized probability in range [0, 1].

    Cross-encoder models like MS-MARCO MiniLM return raw logits which can
    range from approximately -10 to +10. This function normalizes them to
    a 0-1 range using the sigmoid function for compatibility with threshold
    comparisons.

    Parameters
    ----------
    x : float
        Raw logit score from cross-encoder.

    Returns
    -------
    float
        Normalized score in range [0, 1].
    """
    # Clamp to prevent overflow in exp()
    x = max(-20.0, min(20.0, x))
    return 1.0 / (1.0 + math.exp(-x))


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

    async def _search_weaviate_initial(self, query_vector: list[float], session_id: str | None = None) -> list[dict]:
        """
        Performs Parent Document Retrieval with session-aware filtering.
            1. Creates a filter for "Global" docs OR "Session" docs.
            2. Finds the most relevant child chunks using this filter.
            3. Gets their unique parent_source ID.
            4. Retrieves all chunks for those parent documents for the full context.
        """
        if not query_vector: return []
        try:
            documents_collection = self.weaviate_client.collections.get("Document")
            combined_filter = self._get_session_aware_filter(session_id)
            # 1. Find the most relevant child chunks
            response = documents_collection.query.near_vector(
                near_vector=query_vector,
                limit=self.top_k_initial,
                filters=combined_filter,
                return_metadata=wvc.query.MetadataQuery(distance=True),
                return_properties=["content", "source", "parent_source"]
            )
            # 2. Get the unique parent_source ID
            if not response.objects:
                logger.warning("No documents found")
                return []
            parent_sources = list(set(
                obj.properties["parent_source"]
                for obj in response.objects
                if "parent_source" in obj.properties
            ))
            if not parent_sources:
                logger.warning("Found orphaned chunks. just returning the child chunks")
                return [{"properties": obj.properties, "metadata": obj.metadata} for obj in
                        response.objects]
            logger.info(f"Found {len(response.objects)} child chunks pointing to {len(parent_sources)} parent(s).")

            # 3. Retrieve all chunks for those parents (PDR)
            parent_response = documents_collection.query.fetch_objects(
                filters=wvc.query.Filter.by_property("parent_source").contains_any(parent_sources),
                limit=100
            )
            context_docs_with_meta = [
                {"properties": obj.properties, "metadata": obj.metadata}
                for obj in parent_response.objects
            ]
            logger.info(
                f"Retrieved {len(context_docs_with_meta)} total chunks from {len(parent_sources)} parent documents for PDR context.")
            return context_docs_with_meta

        except WeaviateQueryException as e:
            logger.error(f"Weaviate PDR query failed: {e}")
            raise RuntimeError(f"Weaviate PDR search failed: {e}")
        except Exception as e:
            logger.error(f"Failed PDR Weaviate search: {e}", exc_info=True)
            raise RuntimeError(f"Weaviate interaction failed: {e}")

    async def _rerank_docs(self, query: str, initial_docs_with_meta: list[dict]) -> list[dict]:
        """Reranks the initial documents using the cross-encoder."""
        if not self.reranker or not initial_docs_with_meta:
            logger.warning("Skipping reranking (no model or no initial docs).")
            # Return top N of the initial results if reranker isn't available
            return initial_docs_with_meta[:self.top_k_final]

        # Validate document format (P8 contract enforcement)
        # This catches format coupling issues early rather than failing silently
        for i, doc in enumerate(initial_docs_with_meta):
            try:
                validate_document_format(doc, context=f"_rerank_docs[{i}]")
            except ValueError as e:
                logger.error(f"Document format validation failed: {e}")
                # Log the offending document structure for debugging
                logger.debug(f"Invalid document keys: {doc.keys() if isinstance(doc, dict) else type(doc)}")
                raise

        logger.debug(f"Preparing {len(initial_docs_with_meta)} passages for reranking...")
        passages = [d["properties"].get("content", "") for d in initial_docs_with_meta]
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
             return initial_docs_with_meta[:self.top_k_final] # Fallback

        valid_initial_docs = [d for d in initial_docs_with_meta if
                              d["properties"].get("content", "")]
        if len(scores) != len(valid_initial_docs):
             logger.error(f"Mismatch score/passage count. Skipping reranking.")
             return initial_docs_with_meta[:self.top_k_final] # Fallback

        scored_docs = list(zip(scores, valid_initial_docs))
        scored_docs.sort(key=lambda x: x[0], reverse=True)

        reranked_docs_with_meta = [doc for score, doc in scored_docs[:self.top_k_final]]
        logger.info(f"Reranked from {len(initial_docs_with_meta)} down to {len(reranked_docs_with_meta)} documents.")
        for i, (score, doc) in enumerate(scored_docs[:self.top_k_final]):
                # Normalize raw logit score to 0-1 range using sigmoid
                # MS-MARCO cross-encoder returns logits, not probabilities
                normalized_score = _sigmoid(score)
                reranked_docs_with_meta[i]["metadata"].rerank_score = normalized_score
                logger.debug(f"Doc {i}: raw_score={score:.4f} -> normalized={normalized_score:.4f}")
        return reranked_docs_with_meta

    async def run(
        self,
        query: str,
        session_id: str | None = None,
        strict_mode: bool = True,
        relevant_history: list[dict] | None = None,
    ) -> tuple[str, list[dict]]:
        """Executes the RAG pipeline with reranking.

        Parameters
        ----------
        query : str
            The user's query.
        session_id : str | None
            The current session ID, passed from the orchestrator.
        strict_mode : bool
            If True, only answer from documents. If no relevant docs (rerank score < threshold),
            return NO_RELEVANT_DOCS_MESSAGE instead of using LLM fallback.
            Default: True (strict mode).
        relevant_history : list[dict] | None
            Relevant conversation history from P7 semantic memory. Each dict contains
            'question', 'answer', 'turn_number', and 'similarity_score'. If provided,
            history turns are injected as pseudo-documents before reranking.
        """
        tracer = trace.get_tracer("aleutian.rag.reranking")
        with tracer.start_as_current_span("reranking_pipeline.run") as span:
            span.set_attribute("query.length", len(query))
            span.set_attribute("strict_mode", strict_mode)
            if session_id:
                span.set_attribute("session.id", session_id)
            logger.info(f"Reranking RAG run started (strict_mode={strict_mode})...")

            with tracer.start_as_current_span("get_embedding"):
                query_vector = await self._get_embedding(query)

            with tracer.start_as_current_span("initial_search"):
                initial_docs_with_meta = await self._search_weaviate_initial(query_vector,
                                                                             session_id)
                span.set_attribute("retrieved.initial_count", len(initial_docs_with_meta))

            # P8: Inject conversation history as pseudo-documents before reranking
            # History competes with retrieved docs during reranking
            if relevant_history:
                initial_docs_with_meta = self._inject_history_as_documents(
                    initial_docs_with_meta, relevant_history
                )
                span.set_attribute("history.injected_count", len(relevant_history))
                logger.info(f"Injected {len(relevant_history)} history turns into document pool for reranking")

            with tracer.start_as_current_span("rerank_docs"):
                context_docs_with_meta = await self._rerank_docs(query, initial_docs_with_meta)
                span.set_attribute("retrieved.reranked_count", len(context_docs_with_meta))

            # Apply relevance threshold filtering in strict mode
            if strict_mode:
                relevant_docs = [
                    d for d in context_docs_with_meta
                    if d.get("metadata") and hasattr(d["metadata"], "rerank_score")
                    and d["metadata"].rerank_score >= RERANK_SCORE_THRESHOLD
                ]
                span.set_attribute("retrieved.relevant_count", len(relevant_docs))
                logger.info(f"Strict mode: {len(relevant_docs)} of {len(context_docs_with_meta)} docs above threshold {RERANK_SCORE_THRESHOLD}")

                if not relevant_docs:
                    logger.info("No relevant documents found in strict mode, returning message")
                    return NO_RELEVANT_DOCS_MESSAGE, []

                context_docs_with_meta = relevant_docs

            context_docs_props = [d["properties"] for d in context_docs_with_meta]
            span.set_attribute("retrieved.final_count", len(context_docs_props))

            with tracer.start_as_current_span("build_prompt"):
                prompt = self._build_prompt(query, context_docs_props)
                span.set_attribute("prompt.length", len(prompt))

            with tracer.start_as_current_span("call_llm"):
                span.set_attribute("llm.backend", self.llm_backend)
                answer = await self._call_llm(prompt)
                span.set_attribute("answer.length", len(answer))

            logger.info("Reranking RAG run finished.")
            sources = [
                {
                    "source": d["properties"].get("source", "Unknown"),
                    "distance": d["metadata"].distance if d.get("metadata") else None,
                    "score": d["metadata"].rerank_score if (
                                d.get("metadata") and hasattr(d["metadata"],
                                                              "rerank_score")) else None,
                } for d in context_docs_with_meta
            ]

            return answer, sources  # Return both

    async def retrieve_only(
        self,
        query: str,
        session_id: str | None = None,
        strict_mode: bool = True,
        max_chunks: int = 5
    ) -> tuple[list[dict], str, bool]:
        """
        Retrieval-only mode: returns documents without LLM generation.

        This method performs the retrieval and reranking steps without
        calling the LLM, returning raw document content that can be used
        by the Go orchestrator for streaming.

        Parameters
        ----------
        query : str
            The user's query to retrieve documents for.
        session_id : str | None
            The current session ID for session-scoped document filtering.
        strict_mode : bool
            If True, only return documents above relevance threshold.
        max_chunks : int
            Maximum number of document chunks to return.

        Returns
        -------
        tuple[list[dict], str, bool]
            - chunks: List of document dicts with content, source, rerank_score
            - context_text: Formatted string with "[Document N: source]" headers
            - has_relevant_docs: Whether relevant documents were found
        """
        tracer = trace.get_tracer("aleutian.rag.reranking")
        with tracer.start_as_current_span("reranking_pipeline.retrieve_only") as span:
            span.set_attribute("query.length", len(query))
            span.set_attribute("strict_mode", strict_mode)
            span.set_attribute("max_chunks", max_chunks)
            if session_id:
                span.set_attribute("session.id", session_id)
            logger.info(f"Retrieval-only mode started (strict_mode={strict_mode}, max_chunks={max_chunks})...")

            # Step 1: Get query embedding
            with tracer.start_as_current_span("get_embedding"):
                query_vector = await self._get_embedding(query)

            if not query_vector:
                logger.warning("Empty query vector, returning no documents")
                return [], "", False

            # Step 2: Initial search
            with tracer.start_as_current_span("initial_search"):
                initial_docs_with_meta = await self._search_weaviate_initial(
                    query_vector, session_id
                )
                span.set_attribute("retrieved.initial_count", len(initial_docs_with_meta))

            if not initial_docs_with_meta:
                logger.info("No documents found in initial search")
                return [], "", False

            # Step 3: Rerank documents
            with tracer.start_as_current_span("rerank_docs"):
                reranked_docs = await self._rerank_docs(query, initial_docs_with_meta)
                span.set_attribute("retrieved.reranked_count", len(reranked_docs))

            # Step 4: Apply strict mode filtering
            has_relevant_docs = True
            if strict_mode:
                relevant_docs = [
                    d for d in reranked_docs
                    if d.get("metadata") and hasattr(d["metadata"], "rerank_score")
                    and d["metadata"].rerank_score >= RERANK_SCORE_THRESHOLD
                ]
                span.set_attribute("retrieved.relevant_count", len(relevant_docs))
                logger.info(
                    f"Strict mode: {len(relevant_docs)} of {len(reranked_docs)} docs "
                    f"above threshold {RERANK_SCORE_THRESHOLD}"
                )

                if not relevant_docs:
                    has_relevant_docs = False
                    # Still return the best docs even if below threshold,
                    # but flag has_relevant_docs as False
                    relevant_docs = reranked_docs

                reranked_docs = relevant_docs

            # Step 5: Limit to max_chunks
            final_docs = reranked_docs[:max_chunks]
            span.set_attribute("retrieved.final_count", len(final_docs))

            # Step 6: Build chunks list for response
            chunks = []
            for doc in final_docs:
                chunk = {
                    "content": doc["properties"].get("content", ""),
                    "source": doc["properties"].get("source", "Unknown"),
                }
                if doc.get("metadata") and hasattr(doc["metadata"], "rerank_score"):
                    chunk["rerank_score"] = doc["metadata"].rerank_score
                chunks.append(chunk)

            # Step 7: Format context text with [Document N: source] headers
            context_parts = []
            for i, chunk in enumerate(chunks, 1):
                source = chunk["source"]
                content = chunk["content"]
                context_parts.append(f"[Document {i}: {source}]\n{content}")

            context_text = "\n\n".join(context_parts)
            span.set_attribute("context.length", len(context_text))

            logger.info(
                f"Retrieval-only complete: {len(chunks)} chunks, "
                f"has_relevant_docs={has_relevant_docs}"
            )

            return chunks, context_text, has_relevant_docs