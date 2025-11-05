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
import logging
import weaviate
import weaviate.classes as wvc
from weaviate.exceptions import WeaviateQueryException
from .base import BaseRAGPipeline

logger = logging.getLogger(__name__)

# Constants specific to standard RAG if any, otherwise use base defaults
DEFAULT_SEARCH_LIMIT = 3

class StandardRAGPipeline(BaseRAGPipeline):
    """
    Implements a standard Retrieve-Augment-Generate pipeline by inheriting
    common methods from BaseRAGPipeline.
    """
    def __init__(self, weaviate_client: weaviate.WeaviateClient, config: dict):
        super().__init__(weaviate_client, config)
        self.search_limit = config.get("standard_rag_limit", DEFAULT_SEARCH_LIMIT)
        logger.info("StandardRAGPipeline initialized.")

    async def _search_weaviate_initial(self, query_vector: list[float], session_id: str | None = None) -> list[dict]:
        """
        Performs Parent Document Retrieval with session-aware filtering.
            1. Creates a filter for "default" docs OR "session_id" docs.
            2. Finds the most relevant child chunks using this filter.
            3. Gets their unique parent_source ID.
            4. Retrieves all chunks for those parent documents for the full context.
        """
        # --- SIGNATURE MODIFIED to accept session_id ---
        if not query_vector: return []
        try:
            documents_collection = self.weaviate_client.collections.get("Document")

            # --- NEW FILTER LOGIC ---
            # 1. Always filter for default documents
            filter_docs = wvc.query.Filter.by_property("data_space").equal("default")

            combined_filter = None
            if session_id:
                # 2. If a session_id is provided, also filter for its memory
                logger.info(f"Applying session memory filter for: {session_id}")
                filter_memory = wvc.query.Filter.by_property("data_space").equal(session_id)
                # 3. Combine them with an OR
                combined_filter = wvc.query.Filter.any_of([filter_docs, filter_memory])
            else:
                # 4. Otherwise, just use the default filter
                combined_filter = filter_docs
            # --- END NEW FILTER LOGIC ---

            # 1. Find the most relevant child chunks
            response = documents_collection.query.near_vector(
                near_vector=query_vector,
                limit=self.search_limit, # differs from reranking which uses top k (e.g.20) this uses top 3
                filters=combined_filter, # <-- ADDED FILTER
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
            logger.info(
                f"Found {len(response.objects)} child chunks pointing to {len(parent_sources)} parent(s).")

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


    async def run(self, query: str, session_id: str | None = None) -> tuple[str, list[dict]]:
        """Executes the standard RAG pipeline."""
        # --- SIGNATURE MODIFIED to accept session_id ---

        logger.info(f"Standard RAG run started for query: {query[:50]}...")

        # 1. Get query embedding (uses inherited _get_embedding)
        logger.debug("Getting query embedding...")
        query_vector = await self._get_embedding(query)
        logger.debug("Query embedding received.")

        # 2. Search for relevant documents
        logger.debug("Searching Weaviate...")
        # --- MODIFIED LINE: Pass session_id and call correct function ---
        context_docs_with_meta = await self._search_weaviate_initial(query_vector, session_id)
        # --- END MODIFICATION ---

        context_docs_props = [d["properties"] for d in context_docs_with_meta]
        logger.debug(f"Found {len(context_docs_props)} context documents.")

        # 3. Build the prompt (uses inherited _build_prompt)
        logger.debug("Building prompt...")
        prompt = self._build_prompt(query, context_docs_props)

        # 4. Call the LLM (uses inherited _call_llm)
        logger.debug("Calling LLM...")
        answer = await self._call_llm(prompt)
        logger.info("Standard RAG run finished.")

        sources = [
            {
                "source": d["properties"].get("source", "Unknown"),
                "distance": d["metadata"].distance if (d.get("metadata") and hasattr(d["metadata"], "distance")) else None,
            } for d in context_docs_with_meta
        ]

        return answer, sources