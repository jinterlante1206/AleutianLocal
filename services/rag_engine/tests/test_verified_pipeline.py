# Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU Affero General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
# See the LICENSE.txt file for the full license text.
#
# NOTE: This work is subject to additional terms under AGPL v3 Section 7.
# See the NOTICE.txt file for details regarding AI system attribution.

import pytest
from unittest.mock import MagicMock, AsyncMock

# Import from the package structure that works with pytest
import sys
import os
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..'))

from pipelines.verified import VerifiedRAGPipeline
from datatypes.verified import SkepticAuditResult


@pytest.fixture
def mock_weaviate_client():
    return MagicMock()


@pytest.fixture
def verified_pipeline(mock_weaviate_client):
    config = {
        "embedding_url": "http://mock",
        "llm_service_url": "http://mock",
        "llm_backend_type": "mock"
    }
    pipeline = VerifiedRAGPipeline(mock_weaviate_client, config)

    # Mock the inherited Reranking/Base methods to avoid external calls
    pipeline._get_embedding = AsyncMock(return_value=[0.1, 0.2])
    pipeline._search_weaviate_initial = AsyncMock(return_value=[])
    # Note: metadata must include rerank_score above threshold (0.3) for strict mode
    pipeline._rerank_docs = AsyncMock(return_value=[
        {"properties": {"content": "The sky is blue.", "source": "test.txt"},
         "metadata": {"rerank_score": 0.9}}  # Score above RERANK_SCORE_THRESHOLD (0.3)
    ])
    pipeline._call_llm = AsyncMock()  # Default mock for LLM calls
    return pipeline


@pytest.mark.asyncio
async def test_immediate_verification(verified_pipeline):
    """
    Scenario: The Optimist generates a good answer immediately.
    Expectation: Skeptic returns True, Loop breaks immediately, Answer is returned.
    """
    # 1. Setup Optimist Answer
    # We mock _call_llm specifically for the draft generation phase
    verified_pipeline._call_llm.return_value = "The sky is blue."

    # 2. Mock the Skeptic Execution (Sub-method mocking is cleaner than prompt parsing)
    # We assume the prompt building logic works and just test the workflow
    verified_pipeline._execute_skeptic_scan = AsyncMock(return_value=SkepticAuditResult(
        is_verified=True,
        reasoning="Fact is in source.",
        hallucinations=[]
    ))

    # 3. Run
    answer, sources = await verified_pipeline.run("What color is the sky?")

    # 4. Assertions
    assert answer == "The sky is blue."
    assert len(sources) == 1
    # Ensure skeptic was called exactly once
    assert verified_pipeline._execute_skeptic_scan.call_count == 1
    # Ensure refiner was NEVER called
    # (We didn't mock _execute_refinement, so if it was called, it would crash or fail assertions)


@pytest.mark.asyncio
async def test_verification_refinement_loop(verified_pipeline):
    """
    Scenario: Optimist hallucinates. Skeptic catches it. Refiner fixes it. Skeptic verifies it.
    Expectation: Loop runs twice. Final answer is the corrected one.
    """
    # 1. Setup Optimist Answer (Draft)
    verified_pipeline._call_llm.return_value = "The sky is green."

    # 2. Mock Skeptic Responses (Sequence: False, then True)
    verified_pipeline._execute_skeptic_scan = AsyncMock(side_effect=[
        # First check: Fail
        SkepticAuditResult(is_verified=False, reasoning="Source says blue.",
                           hallucinations=["green"]),
        # Second check: Pass
        SkepticAuditResult(is_verified=True, reasoning="Correct.", hallucinations=[])
    ])

    # 3. Mock Refiner Response
    verified_pipeline._execute_refinement = AsyncMock(return_value="The sky is blue.")

    # 4. Run
    answer, sources = await verified_pipeline.run("What color is the sky?")

    # 5. Assertions
    assert answer == "The sky is blue."  # Should be the refined answer
    assert verified_pipeline._execute_skeptic_scan.call_count == 2
    assert verified_pipeline._execute_refinement.call_count == 1


@pytest.mark.asyncio
async def test_max_retries_exceeded(verified_pipeline):
    """
    Scenario: The model cannot fix the hallucination within MAX_VERIFICATION_RETRIES.
    Expectation: Returns the last attempt with a warning suffix.
    """
    # 1. Setup Optimist
    verified_pipeline._call_llm.return_value = "The sky is green."

    # 2. Mock Skeptic to ALWAYS fail
    fail_result = SkepticAuditResult(is_verified=False, reasoning="Still wrong.",
                                     hallucinations=["green"])
    verified_pipeline._execute_skeptic_scan = AsyncMock(return_value=fail_result)

    # 3. Mock Refiner (it keeps trying but failing in this scenario)
    verified_pipeline._execute_refinement = AsyncMock(return_value="The sky is purple.")

    # 4. Run
    answer, _ = await verified_pipeline.run("What color is the sky?")

    # 5. Assertions
    # It should contain the last refined answer + the warning
    assert "The sky is purple" in answer
    assert "(Warning: Verification incomplete)" in answer
    # Should call skeptic 3 times (Initial + Retry 1 + Retry 2)
    # assuming MAX_VERIFICATION_RETRIES = 2
    assert verified_pipeline._execute_skeptic_scan.call_count == 3


@pytest.mark.asyncio
async def test_no_documents_found(verified_pipeline):
    """Scenario: Reranker returns empty list.

    When no relevant documents are found and the relevance gate (P8) is enabled,
    the pipeline returns a user-friendly message asking for clarification.
    """
    # Override the reranker mock to return empty
    verified_pipeline._rerank_docs = AsyncMock(return_value=[])

    answer, sources = await verified_pipeline.run("Unrelated question")

    # P8 relevance gate returns LOW_RELEVANCE_MESSAGE when no docs are found
    assert "couldn't find relevant information" in answer
    assert sources == []


# =============================================================================
# P7: Conversation History Tests
# =============================================================================

class TestConversationHistory:
    """Tests for P7 semantic conversation memory integration."""

    def test_optimist_prompt_includes_history_strict_mode(self, verified_pipeline):
        """
        Verify that _build_optimist_prompt includes history when provided (strict mode).

        P7 Requirement: History should appear in a distinct "CONVERSATION HISTORY" section
        to help the LLM understand follow-up queries like "tell me more".
        """
        context_docs = [{"content": "Chrysler was founded in 1925.", "source": "cars.txt"}]
        history = [
            {
                "question": "What is Chrysler?",
                "answer": "Chrysler is an American automotive company.",
                "turn_number": 5,
                "similarity_score": 0.87
            }
        ]

        # Call the prompt builder directly
        prompt = verified_pipeline._build_optimist_prompt("Tell me more", context_docs, history)

        # Assertions
        assert "CONVERSATION HISTORY (Memory)" in prompt
        assert "What is Chrysler?" in prompt
        assert "Chrysler is an American automotive company" in prompt
        assert "[Turn 5]" in prompt
        assert "UPLOADED KNOWLEDGE BASE (Facts)" in prompt
        assert "cars.txt" in prompt

    def test_optimist_prompt_includes_history_balanced_mode(self, verified_pipeline):
        """
        Verify that _build_optimist_prompt includes history when provided (balanced mode).
        """
        # Set balanced mode
        verified_pipeline.optimist_strictness = "balanced"

        context_docs = [{"content": "Ford was founded in 1903.", "source": "cars.txt"}]
        history = [
            {
                "question": "What companies make cars?",
                "answer": "Ford, GM, and Chrysler are major US automakers.",
                "turn_number": 3,
                "similarity_score": 0.75
            }
        ]

        prompt = verified_pipeline._build_optimist_prompt("Tell me about Ford", context_docs, history)

        # Assertions
        assert "CONVERSATION HISTORY (Memory)" in prompt
        assert "What companies make cars?" in prompt
        assert "[Turn 3]" in prompt

    def test_optimist_prompt_works_without_history(self, verified_pipeline):
        """
        Verify backward compatibility: prompt works when no history is provided.

        P7 Requirement: The history parameter is optional. When None or empty,
        the prompt should work as before without a history section.
        """
        context_docs = [{"content": "The sky is blue.", "source": "nature.txt"}]

        # Call without history (default None)
        prompt = verified_pipeline._build_optimist_prompt("What color is the sky?", context_docs)

        # Should NOT contain history section
        assert "CONVERSATION HISTORY" not in prompt
        # Should still contain sources
        assert "nature.txt" in prompt
        assert "The sky is blue" in prompt

    def test_optimist_prompt_with_empty_history(self, verified_pipeline):
        """
        Verify that empty history list behaves same as None.
        """
        context_docs = [{"content": "Water is H2O.", "source": "chemistry.txt"}]

        # Call with empty list
        prompt = verified_pipeline._build_optimist_prompt("What is water?", context_docs, [])

        # Should NOT contain history section
        assert "CONVERSATION HISTORY" not in prompt
        assert "chemistry.txt" in prompt

    def test_optimist_prompt_truncates_long_answers(self, verified_pipeline):
        """
        Verify that very long answers in history are truncated to prevent context overflow.

        P7 Requirement: Answers longer than 500 chars should be truncated with "..."
        """
        context_docs = [{"content": "Brief fact.", "source": "test.txt"}]
        long_answer = "A" * 600  # 600 characters, should be truncated
        history = [
            {
                "question": "Long question?",
                "answer": long_answer,
                "turn_number": 1,
                "similarity_score": 0.9
            }
        ]

        prompt = verified_pipeline._build_optimist_prompt("Follow up", context_docs, history)

        # Should contain truncated answer (500 chars + "...")
        assert "A" * 500 in prompt
        assert "..." in prompt
        # Should NOT contain full 600 chars
        assert long_answer not in prompt

    def test_optimist_prompt_handles_none_turn_number(self, verified_pipeline):
        """
        Verify that None turn_number displays as '?' not 'None'.

        Edge case: RelevantHistoryItem allows turn_number: None for legacy data.
        The prompt should display [Turn ?] not [Turn None].
        """
        context_docs = [{"content": "Some fact.", "source": "test.txt"}]
        history = [
            {
                "question": "Earlier question?",
                "answer": "Earlier answer.",
                "turn_number": None,  # Explicit None
                "similarity_score": 0.8
            }
        ]

        prompt = verified_pipeline._build_optimist_prompt("Follow up", context_docs, history)

        # Should contain [Turn ?] for None turn_number
        assert "[Turn ?]" in prompt
        # Should NOT contain [Turn None]
        assert "[Turn None]" not in prompt
        assert "Earlier question?" in prompt

    @pytest.mark.asyncio
    async def test_run_passes_history_to_optimist(self, verified_pipeline):
        """
        Integration test: verify run() passes history through to _build_optimist_prompt.
        """
        # Setup mocks
        verified_pipeline._call_llm.return_value = "Chrysler merged with Fiat."
        verified_pipeline._execute_skeptic_scan = AsyncMock(return_value=SkepticAuditResult(
            is_verified=True,
            reasoning="Verified",
            hallucinations=[]
        ))

        history = [
            {
                "question": "What is Chrysler?",
                "answer": "Chrysler is an American automotive company.",
                "turn_number": 5,
                "similarity_score": 0.87
            }
        ]

        # Run with history
        answer, sources = await verified_pipeline.run(
            "Tell me more about their recent history",
            relevant_history=history
        )

        # Verify answer returned
        assert answer == "Chrysler merged with Fiat."

        # Verify _call_llm was called with a prompt containing history
        call_args = verified_pipeline._call_llm.call_args_list[0]
        prompt_used = call_args[0][0]  # First positional arg is the prompt
        assert "What is Chrysler?" in prompt_used
        assert "CONVERSATION HISTORY" in prompt_used