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
from enum import Enum
from pydantic import BaseModel, Field
from typing import List, Optional
from datetime import datetime, timezone


# =============================================================================
# Progress Event Types for Debate Streaming (P6)
# =============================================================================


class ProgressEventType(str, Enum):
    """
    Enumeration of progress event types emitted during verified pipeline execution.

    # Description

    Defines the discrete stages of the skeptic/optimist debate pattern.
    Each stage emits a progress event that can be streamed to the client.

    # Values

    - RETRIEVAL_START: Document retrieval has begun
    - RETRIEVAL_COMPLETE: Documents retrieved, includes count and sources
    - DRAFT_START: Optimist is generating initial answer
    - DRAFT_COMPLETE: Initial draft ready for skeptic review
    - SKEPTIC_AUDIT_START: Skeptic is analyzing the draft
    - SKEPTIC_AUDIT_COMPLETE: Skeptic has rendered verdict
    - REFINEMENT_START: Refiner is correcting hallucinations
    - REFINEMENT_COMPLETE: Refined answer ready for re-audit
    - VERIFICATION_COMPLETE: Final answer verified, debate concluded
    - ERROR: An error occurred during pipeline execution

    # Examples

        event_type = ProgressEventType.SKEPTIC_AUDIT_START
        if event_type == ProgressEventType.SKEPTIC_AUDIT_START:
            print("Skeptic is analyzing...")

    # Limitations

    - Event types are fixed; custom stages require code changes
    - Not all stages emit events at all verbosity levels

    # Assumptions

    - The pipeline follows the optimist -> skeptic -> refiner pattern
    - Events are emitted in order (no parallel stages)
    """
    RETRIEVAL_START = "retrieval_start"
    RETRIEVAL_COMPLETE = "retrieval_complete"
    DRAFT_START = "draft_start"
    DRAFT_COMPLETE = "draft_complete"
    SKEPTIC_AUDIT_START = "skeptic_audit_start"
    SKEPTIC_AUDIT_COMPLETE = "skeptic_audit_complete"
    REFINEMENT_START = "refinement_start"
    REFINEMENT_COMPLETE = "refinement_complete"
    VERIFICATION_COMPLETE = "verification_complete"
    ERROR = "error"


class SkepticAuditDetails(BaseModel):
    """
    Detailed information about a skeptic audit for progress events.

    # Description

    Contains the full output of the skeptic's analysis, including
    whether the draft was verified, specific hallucinations found,
    and what evidence was missing. This is included in progress
    events at verbosity level 2.

    # Fields

    - is_verified: Whether all claims were supported by evidence
    - reasoning: The skeptic's explanation of the verdict
    - hallucinations: List of specific unsupported claims
    - missing_evidence: List of facts that lacked supporting evidence
    - sources_cited: Source indices referenced in the audit

    # Examples

        details = SkepticAuditDetails(
            is_verified=False,
            reasoning="Claim about GDP lacks source citation",
            hallucinations=["Detroit's GDP is $50 billion"],
            missing_evidence=["GDP statistics"],
            sources_cited=[0, 2, 3]
        )

    # Limitations

    - sources_cited indices match the order of retrieved documents
    - May be empty if skeptic fails to parse sources

    # Assumptions

    - Hallucinations list contains exact quoted claims from the draft
    - Source indices are 0-based
    """
    is_verified: bool = Field(..., description="True if all claims are supported by evidence.")
    reasoning: str = Field(..., description="The skeptic's explanation of the verdict.")
    hallucinations: List[str] = Field(default_factory=list, description="Specific unsupported claims.")
    missing_evidence: List[str] = Field(default_factory=list, description="Facts needing evidence.")
    sources_cited: List[int] = Field(default_factory=list, description="Indices of sources referenced.")


class RetrievalDetails(BaseModel):
    """
    Detailed information about document retrieval for progress events.

    # Description

    Contains information about the documents retrieved during the
    retrieval phase. Included in RETRIEVAL_COMPLETE events at
    verbosity level 2.

    # Fields

    - document_count: Number of documents retrieved
    - sources: List of source identifiers (filenames, URLs, etc.)
    - has_relevant_docs: Whether any relevant documents were found

    # Examples

        details = RetrievalDetails(
            document_count=5,
            sources=["doc1.pdf", "doc2.txt", "webpage.html"],
            has_relevant_docs=True
        )

    # Limitations

    - sources may be truncated for very long filenames
    - document_count may differ from len(sources) if deduplication applied

    # Assumptions

    - Sources are in rerank-score order (highest first)
    """
    document_count: int = Field(..., description="Number of documents retrieved.")
    sources: List[str] = Field(default_factory=list, description="Source identifiers.")
    has_relevant_docs: bool = Field(True, description="Whether relevant documents were found.")


class ProgressEvent(BaseModel):
    """
    A progress event emitted during verified pipeline execution.

    # Description

    Represents a single progress update in the skeptic/optimist debate.
    Events are streamed to the client via SSE (Server-Sent Events) to
    provide real-time feedback during pipeline execution.

    # Fields

    - event_type: The type of progress event (from ProgressEventType enum)
    - message: Human-readable summary message (always present)
    - timestamp: ISO 8601 timestamp when the event occurred
    - attempt: Current verification attempt (1-indexed, max 3)
    - trace_id: OpenTelemetry trace ID for debugging (verbosity >= 2)
    - retrieval_details: Details about retrieval (RETRIEVAL_COMPLETE only)
    - audit_details: Details about skeptic audit (SKEPTIC_AUDIT_COMPLETE only)
    - error_message: Error description (ERROR event type only)

    # Examples

        # Verbosity 1 event (summary)
        event = ProgressEvent(
            event_type=ProgressEventType.SKEPTIC_AUDIT_START,
            message="Skeptic auditing claims (attempt 1/3)...",
            timestamp=datetime.utcnow(),
            attempt=1
        )

        # Verbosity 2 event (detailed)
        event = ProgressEvent(
            event_type=ProgressEventType.SKEPTIC_AUDIT_COMPLETE,
            message="Skeptic found 2 unsupported claims",
            timestamp=datetime.utcnow(),
            attempt=1,
            trace_id="abc123def456",
            audit_details=SkepticAuditDetails(
                is_verified=False,
                reasoning="...",
                hallucinations=["claim 1", "claim 2"]
            )
        )

    # Limitations

    - trace_id requires OpenTelemetry to be configured
    - detail fields are only populated at verbosity level 2

    # Assumptions

    - Events are serialized to JSON for SSE streaming
    - Timestamps are UTC
    - attempt is 1-indexed (1, 2, or 3)
    """
    event_type: ProgressEventType = Field(..., description="The type of progress event.")
    message: str = Field(..., description="Human-readable summary message.")
    timestamp: datetime = Field(default_factory=lambda: datetime.now(timezone.utc), description="When the event occurred.")
    attempt: int = Field(1, description="Current verification attempt (1-indexed).", ge=1, le=3)
    trace_id: Optional[str] = Field(None, description="OpenTelemetry trace ID for debugging.")
    retrieval_details: Optional[RetrievalDetails] = Field(None, description="Retrieval details (verbosity 2).")
    audit_details: Optional[SkepticAuditDetails] = Field(None, description="Skeptic audit details (verbosity 2).")
    error_message: Optional[str] = Field(None, description="Error description for ERROR events.")

    class Config:
        """Pydantic configuration for JSON serialization."""
        json_encoders = {
            datetime: lambda v: v.isoformat() + "Z"
        }

    def to_sse_data(self) -> str:
        """
        Serialize the event to SSE data format.

        # Description

        Converts the event to a JSON string suitable for SSE streaming.
        Excludes None fields for a cleaner payload.

        # Returns

        A JSON string representation of the event.

        # Examples

            event = ProgressEvent(
                event_type=ProgressEventType.DRAFT_START,
                message="Generating initial draft..."
            )
            sse_data = event.to_sse_data()
            # '{"event_type": "draft_start", "message": "Generating initial draft...", ...}'
        """
        return self.model_dump_json(exclude_none=True)


class ProgressCallback:
    """
    Type alias for progress callback functions.

    # Description

    Defines the signature for callback functions that receive progress
    events during pipeline execution. The callback is invoked for each
    progress event, allowing real-time streaming to the client.

    # Signature

        async def callback(event: ProgressEvent) -> None

    # Examples

        async def my_callback(event: ProgressEvent) -> None:
            print(f"[{event.event_type.value}] {event.message}")
            await sse_queue.put(event.to_sse_data())

        await pipeline.run_with_progress(
            query="What is Detroit?",
            progress_callback=my_callback
        )

    # Limitations

    - Callback must be async (use asyncio)
    - Callback should not raise exceptions (they will be logged but not propagated)

    # Assumptions

    - Callback completes quickly (non-blocking)
    - Callback handles its own error logging
    """
    pass  # This is a documentation-only class; actual type is Callable[[ProgressEvent], Awaitable[None]]

class SkepticAuditRequest(BaseModel):
    """Payload sent to the Skeptic Agent."""
    query: str
    proposed_answer: str
    evidence_text: str

class SkepticAuditResult(BaseModel):
    """The structured verdict returned by the Skeptic Agent."""
    is_verified: bool = Field(..., description="True if all claims are supported by evidence.")
    reasoning: str = Field(..., description="Brief explanation of the analysis.")
    hallucinations: List[str] = Field(default_factory=list, description="Specific claims found to be unsupported.")
    missing_evidence: List[str] = Field(default_factory=list, description="Facts that required evidence but had none.")

class RefinerRequest(BaseModel):
    """Payload sent to the Refiner Agent."""
    query: str
    draft_answer: str
    audit_result: SkepticAuditResult
    evidence_text: Optional[str] = Field(default=None, description="The evidence documents for reference during refinement.")

class VerificationState(BaseModel):
    """Tracks the state of the verification loop."""
    current_answer: str
    attempt_count: int = 0
    is_final_verified: bool = False
    history: List[SkepticAuditResult] = Field(default_factory=list)

    def mark_verified(self):
        self.is_final_verified = True

    def add_audit(self, audit: SkepticAuditResult):
        self.history.append(audit)
        self.attempt_count += 1


class RelevantHistoryItem(BaseModel):
    """A conversation turn retrieved from semantic memory search.

    # Description

    RelevantHistoryItem represents a past conversation turn that has been
    identified as relevant to the current query, either by recency or
    semantic similarity. This is passed from Go orchestrator to Python
    RAG engine to provide conversation context.

    # Fields

    - question: The user's original query for this turn.
    - answer: The AI's response for this turn.
    - turn_number: Sequential turn number within the session (None if unknown).
    - similarity_score: Cosine similarity from vector search (None if retrieved by recency).

    # Example

    ```python
    item = RelevantHistoryItem(
        question="What is Chrysler?",
        answer="Chrysler is an American automotive company...",
        turn_number=5,
        similarity_score=0.87
    )
    ```

    # Assumptions

    - JSON field names match Go struct tags exactly.
    - Fields are optional to support legacy data without turn_number.
    """
    question: str = Field(..., description="The user's original query for this turn.")
    answer: str = Field(..., description="The AI's response for this turn.")
    turn_number: int | None = Field(default=None, description="Sequential turn number (None if unknown).")
    similarity_score: float | None = Field(default=None, description="Cosine similarity score (None if retrieved by recency).")


class ExpandedQueryItem(BaseModel):
    """P8: Query expansion result from Go orchestrator.

    # Description

    ExpandedQueryItem contains the results of the P8 query expansion phase.
    When a user submits an ambiguous query like "tell me more", the Go
    orchestrator uses an LLM to generate multiple expanded query variations
    that capture the intent based on conversation history.

    # Fields

    - original: The user's original query before expansion.
    - queries: List of expanded query variations (SPECIFIC, BROAD, CONTEXTUAL).
    - expanded: Whether expansion actually occurred (False if original was clear).

    # Examples

    ```python
    # User said "tell me more" after asking about Motown
    item = ExpandedQueryItem(
        original="tell me more",
        queries=[
            "History of Motown Records founding by Berry Gordy",
            "Motown Records artists and musical influence",
            "Berry Gordy Detroit music label soul artists"
        ],
        expanded=True
    )
    ```

    # Assumptions

    - If expanded is False, queries list will contain just the original query.
    - First query in the list is the most specific/preferred for reranking.
    """
    original: str = Field(..., description="The user's original query before expansion.")
    queries: list[str] = Field(default_factory=list, description="Expanded query variations.")
    expanded: bool = Field(default=False, description="Whether expansion actually occurred.")