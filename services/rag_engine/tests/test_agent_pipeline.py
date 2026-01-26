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
from datatypes.agent import AgentMessage, AgentStepRequest
from pipelines.agent import AgentPipeline


# Mock the BasePipeline dependencies
@pytest.fixture
def mock_pipeline():
    # Instantiate with dummy config containing ALL required fields
    config = {
        "llm_backend_type": "ollama",
        "llm_service_url": "http://mock-url",
        "embedding_url": "http://mock-embedding-url"  # <-- ADD THIS LINE
    }
    # We mock weaviate_client as it's required by __init__
    return AgentPipeline(weaviate_client=MagicMock(), config=config)

def test_history_conversion_ollama(mock_pipeline):
    """Test that generic AgentMessage history converts to Ollama dicts correctly."""
    mock_pipeline.agent_backend = "ollama"

    history = [
        AgentMessage(role="user", content="Read main.go"),
        AgentMessage(role="assistant", tool_calls=[
            {"id": "call_1", "type": "function",
             "function": {"name": "read_file", "arguments": "{}"}}
        ]),
        AgentMessage(role="tool", tool_call_id="call_1", content="package main")
    ]

    llm_msgs = mock_pipeline._convert_history_to_llm_format(history)

    assert len(llm_msgs) == 3
    assert llm_msgs[0]['role'] == "user"
    assert llm_msgs[0]['content'] == "Read main.go"
    # Note: Check specific Ollama format implementation details in your code for tool/assistant
    assert llm_msgs[2]['role'] == "tool"
    assert llm_msgs[2]['content'] == "package main"


@pytest.mark.asyncio
async def test_run_step_tool_call(mock_pipeline):
    """Test that the pipeline correctly parses a Tool Call from the LLM."""

    # Mock the _call_model_agnostic method to simulate LLM response
    mock_response = {
        "tool_calls": [
            {"name": "list_files", "args": '{"path": "."}', "id": "call_123"}
        ],
        "content": ""
    }
    mock_pipeline._call_model_agnostic = AsyncMock(return_value=mock_response)

    request = AgentStepRequest(query="List files", history=[])

    response = await mock_pipeline.run_step(request)

    assert response.type == "tool_call"
    assert response.tool == "list_files"
    assert response.args == {"path": "."}
    assert response.tool_id == "call_123"


@pytest.mark.asyncio
async def test_run_step_answer(mock_pipeline):
    """Test that the pipeline correctly returns a final answer."""

    mock_response = {
        "tool_calls": [],
        "content": "Here is the answer."
    }
    mock_pipeline._call_model_agnostic = AsyncMock(return_value=mock_response)

    request = AgentStepRequest(query="Hi", history=[])

    response = await mock_pipeline.run_step(request)

    assert response.type == "answer"
    assert response.content == "Here is the answer."