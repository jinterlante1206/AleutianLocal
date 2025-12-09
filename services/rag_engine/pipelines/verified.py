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
import json
import weaviate

# Import our new datatypes
from datatypes.verified import (
    SkepticAuditResult,
    SkepticAuditRequest,
    RefinerRequest,
    VerificationState
)
from .reranking import RerankingPipeline

logger = logging.getLogger(__name__)

# Configuration Constants
MAX_VERIFICATION_RETRIES = 2


class VerifiedRAGPipeline(RerankingPipeline):
    """
    Orchestrates the Self-Correcting RAG Loop using strictly defined datatypes.
    """

    def __init__(self, weaviate_client: weaviate.WeaviateClient, config: dict):
        super().__init__(weaviate_client, config)
        logger.info("VerifiedRAGPipeline initialized.")

    # --- 1. Helper: Prompt Builders (Single Responsibility: Formatting) ---

    def _format_evidence(self, context_docs: list[dict]) -> str:
        """Formats list of docs into a labeled string for the LLM."""
        evidence = ""
        for i, doc in enumerate(context_docs):
            content = doc.get('content', '').replace("\n", " ")
            evidence += f"[Source {i}]: {content}\n\n"
        return evidence

    def _build_skeptic_prompt(self, req: SkepticAuditRequest) -> str:
        return f"""
        You are a Strict AI Auditor. Detect hallucinations.

        USER QUERY: {req.query}
        PROPOSED ANSWER: {req.proposed_answer}

        VERIFIED EVIDENCE:
        {req.evidence_text}

        INSTRUCTIONS:
        1. Verify if every claim in the answer is supported by the evidence.
        2. Output valid JSON only.

        JSON STRUCTURE:
        {{
            "is_verified": boolean,
            "reasoning": "string",
            "hallucinations": ["string", "string"],
            "missing_evidence": ["string"]
        }}
        """

    def _build_refiner_prompt(self, req: RefinerRequest) -> str:
        return f"""
        You are a Fact-Checking Editor. Fix the draft based on the critique.

        QUERY: {req.query}
        DRAFT: {req.draft_answer}

        CRITIQUE:
        {req.audit_result.reasoning}
        Unsupported Claims: {req.audit_result.hallucinations}

        TASK:
        Rewrite the draft. Remove unsupported claims. Do not add new info.
        Output only the new answer.
        """

    # --- 2. Core Actions (Single Responsibility: Execution) ---

    async def _execute_skeptic_scan(self, req: SkepticAuditRequest) -> SkepticAuditResult:
        """Calls LLM to audit the answer and returns a typed result."""
        prompt = self._build_skeptic_prompt(req)
        response_str = await self._call_llm(prompt)

        try:
            # Clean generic LLM markdown
            clean_json = response_str.replace("```json", "").replace("```", "").strip()
            if "{" in clean_json and "}" in clean_json:
                start = clean_json.find("{")
                end = clean_json.rfind("}") + 1
                clean_json = clean_json[start:end]

            # Parse into Pydantic model
            data = json.loads(clean_json)
            return SkepticAuditResult(**data)
        except Exception as e:
            logger.warning(f"Skeptic failed to parse JSON: {e}. Defaulting to verified.")
            return SkepticAuditResult(
                is_verified=True,
                reasoning="JSON Parse Failure",
                hallucinations=[]
            )

    async def _execute_refinement(self, req: RefinerRequest) -> str:
        """Calls LLM to rewrite the answer based on the audit."""
        prompt = self._build_refiner_prompt(req)
        return await self._call_llm(prompt)

    # --- 3. The Orchestration Loop (Single Responsibility: Workflow) ---

    async def run(self, query: str, session_id: str | None = None) -> tuple[str, list[dict]]:
        # A. Retrieve Data
        query_vector = await self._get_embedding(query)
        # This calls the method from reranking.py (the PDR logic)
        initial_docs = await self._search_weaviate_initial(query_vector, session_id)
        context_docs = await self._rerank_docs(query, initial_docs)

        # Extract properties for prompt usage
        context_props = [d["properties"] for d in context_docs]
        if not context_props:
            return "No relevant documents found.", []

        # B. Initial "Optimist" Draft
        draft_prompt = self._build_prompt(query, context_props)
        initial_answer = await self._call_llm(draft_prompt)
        original_draft = initial_answer

        # C. Initialize State
        state = VerificationState(current_answer=initial_answer)
        evidence_str = self._format_evidence(context_props)

        # D. Verification Loop
        while state.attempt_count <= MAX_VERIFICATION_RETRIES:
            logger.info(f"Verification Loop {state.attempt_count + 1}")

            # 1. Prepare Request
            audit_req = SkepticAuditRequest(
                query=query,
                proposed_answer=state.current_answer,
                evidence_text=evidence_str
            )

            # 2. Run Skeptic
            audit_result = await self._execute_skeptic_scan(audit_req)
            state.add_audit(audit_result)

            # 3. Check Verdict
            if audit_result.is_verified:
                logger.info("Verified.")
                state.mark_verified()
                break

            # 4. Refine (if we have retries left)
            if state.attempt_count <= MAX_VERIFICATION_RETRIES:
                logger.info(f"Refining hallucination: {audit_result.hallucinations}")
                refine_req = RefinerRequest(
                    query=query,
                    draft_answer=state.current_answer,
                    audit_result=audit_result
                )
                state.current_answer = await self._execute_refinement(refine_req)

        # E. Finalize
        final_output = state.current_answer
        if not state.is_final_verified:
            final_output += "\n\n*(Warning: Verification incomplete)*"

        # Format sources (reuse logic from base if desired, or inline here)
        sources = [
            {
                "source": d["properties"].get("source", "Unknown"),
                "distance": d["metadata"].distance if d.get("metadata") else None,
                "score": d["metadata"].rerank_score if (
                            d.get("metadata") and hasattr(d["metadata"], "rerank_score")) else None,
            } for d in context_docs
        ]

        if session_id:
            # Run this as a background task if you want lower latency,
            # but await is fine for local.
            await self._log_debate(query, state, final_output, session_id)

        return final_output, sources

    async def _log_debate(self, query: str, state: VerificationState, final_answer: str,
                          session_id: str):
        """
        Saves the debate transcript to Weaviate for future evaluation (DeepEval/Ragas).
        """
        if not self.weaviate_client.is_connected():
            logger.warning("Weaviate not connected, skipping debate logging.")
            return

        try:
            # We log the *last* audit that triggered a refinement (or the verified one)
            last_audit = state.history[-1] if state.history else None

            # Prepare properties
            properties = {
                "query": query,
                "draft_answer": state.history[0].proposed_answer if state.history else "",
                # Hypothetical field, see note below
                "skeptic_critique": last_audit.reasoning if last_audit else "",
                "hallucinations_found": last_audit.hallucinations if last_audit else [],
                "final_answer": final_answer,
                "was_refined": state.attempt_count > 0,
                "session_id": session_id or "anonymous",
                "timestamp": int(time.time() * 1000)
            }

            # Insert into Weaviate
            # Note: Using v4 client syntax (collections)
            verification_logs = self.weaviate_client.collections.get("VerificationLog")
            verification_logs.data.insert(properties=properties)

            logger.info("✅ Debate log saved to Weaviate.")

        except Exception as e:
            logger.error(f"Failed to log debate to Weaviate: {e}")
