import pytest
from unittest.mock import MagicMock, AsyncMock, patch
from pipelines.verified import VerifiedRAGPipeline
from datatypes.verified import SkepticAuditResult, SkepticAuditRequest, RefinerRequest


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
    pipeline._rerank_docs = AsyncMock(return_value=[
        {"properties": {"content": "The sky is blue.", "source": "test.txt"},
         "metadata": MagicMock()}
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
    """Scenario: Reranker returns empty list."""
    # Override the reranker mock to return empty
    verified_pipeline._rerank_docs = AsyncMock(return_value=[])

    answer, sources = await verified_pipeline.run("Unrelated question")

    assert "No relevant documents" in answer
    assert sources == []