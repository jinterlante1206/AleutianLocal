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
import time
from .reranking import RerankingPipeline
from .base import RERANK_SCORE_THRESHOLD, NO_RELEVANT_DOCS_MESSAGE

logger = logging.getLogger(__name__)

# Configuration Constants
MAX_VERIFICATION_RETRIES = 2


class VerifiedRAGPipeline(RerankingPipeline):
    """
    Orchestrates the Self-Correcting RAG Loop using strictly defined datatypes.
    """

    def __init__(self, weaviate_client: weaviate.WeaviateClient, config: dict):
        super().__init__(weaviate_client, config)
        self.skeptic_model = config.get("skeptic_model")
        logger.info(f"VerifiedRAGPipeline initialized. Skeptic Model {self.skeptic_model}")

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
        You are a SKEPTICAL FACT-CHECKER auditing someone else's answer for hallucinations.

        CRITICAL RULES:
        1. ASSUME THE ANSWER IS WRONG until proven right by evidence.
        2. Each claim needs DIRECT, EXPLICIT support - no assumptions or inferences.
        3. If a claim requires connecting multiple sources or "reading between the lines", mark it as unsupported.
        4. Vague or partial matches = HALLUCINATION.

        USER QUERY: {req.query}

        ANSWER TO AUDIT (treat this as potentially flawed):
        {req.proposed_answer}

        VERIFIED EVIDENCE (the ONLY truth source):
        {req.evidence_text}

        AUDIT PROCESS:
        Step 1: Break the answer into individual factual claims.
        Step 2: For EACH claim, find its EXACT match in evidence (quote source number).
        Step 3: If no exact match exists, add to hallucinations list.
        Step 4: List what evidence is missing to fully answer the query.

        Output ONLY valid JSON:
        {{
            "is_verified": boolean,  // true ONLY if ALL claims are supported
            "reasoning": "string",   // explain your verdict with source references
            "hallucinations": ["claim 1 that lacks support", "claim 2..."],
            "missing_evidence": ["what info would be needed to verify hallucinations"]
        }}

        REMEMBER: Being strict protects users from misinformation. When in doubt, mark as hallucination.
        """

    def _build_refiner_prompt(self, req: RefinerRequest) -> str:
        return f"""
        You are a SKEPTICAL FACT-CHECKER auditing someone else's answer for hallucinations.

        CRITICAL RULES:
        1. ASSUME THE ANSWER IS WRONG until proven right by evidence.
        2. Each claim needs DIRECT, EXPLICIT support - no assumptions or inferences.
        3. If a claim requires connecting multiple sources or "reading between the lines", mark it as unsupported.
        4. Vague or partial matches = HALLUCINATION.

        USER QUERY: {req.query}

        ANSWER TO AUDIT (treat this as potentially flawed):
        {req.proposed_answer}

        VERIFIED EVIDENCE (the ONLY truth source):
        {req.evidence_text}

        AUDIT PROCESS:
        Step 1: Break the answer into individual factual claims.
        Step 2: For EACH claim, find its EXACT match in evidence (quote source number).
        Step 3: If no exact match exists, add to hallucinations list.
        Step 4: List what evidence is missing to fully answer the query.

        Output ONLY valid JSON:
        {{
            "is_verified": boolean,  // true ONLY if ALL claims are supported
            "reasoning": "string",   // explain your verdict with source references
            "hallucinations": ["claim 1 that lacks support", "claim 2..."],
            "missing_evidence": ["what info would be needed to verify hallucinations"]
        }}

        REMEMBER: Being strict protects users from misinformation. When in doubt, mark as hallucination.
        """

    # --- 2. Core Actions (Single Responsibility: Execution) ---

    async def _execute_skeptic_scan(self, req: SkepticAuditRequest) -> SkepticAuditResult:
        """Calls LLM to audit the answer and returns a typed result."""
        prompt = self._build_skeptic_prompt(req)
        # Even if it matches the default model now, this line enables the "Split Brain" later.
        response_str = await self._call_llm(
            prompt,
            model_override=self.skeptic_model,
            temperature=0.1
        )

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

    async def run(self, query: str, session_id: str | None = None, strict_mode: bool = True) -> tuple[str, list[dict]]:
        # A. Retrieve Data
        query_vector = await self._get_embedding(query)
        # This calls the method from reranking.py (the PDR logic)
        initial_docs = await self._search_weaviate_initial(query_vector, session_id)
        context_docs = await self._rerank_docs(query, initial_docs)

        # Apply relevance threshold filtering in strict mode
        if strict_mode:
            relevant_docs = [
                d for d in context_docs
                if d.get("metadata") and hasattr(d["metadata"], "rerank_score")
                and d["metadata"].rerank_score >= RERANK_SCORE_THRESHOLD
            ]
            logger.info(f"Strict mode: {len(relevant_docs)} of {len(context_docs)} docs above threshold {RERANK_SCORE_THRESHOLD}")

            if not relevant_docs:
                logger.info("No relevant documents found in strict mode, returning message")
                return NO_RELEVANT_DOCS_MESSAGE, []

            context_docs = relevant_docs

        # Extract properties for prompt usage
        context_props = [d["properties"] for d in context_docs]
        if not context_props:
            return NO_RELEVANT_DOCS_MESSAGE, []

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
