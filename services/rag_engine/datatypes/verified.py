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
from pydantic import BaseModel, Field
from typing import List, Optional

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