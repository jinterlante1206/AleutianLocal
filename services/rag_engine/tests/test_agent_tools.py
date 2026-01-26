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
import json
import pytest
import time
from unittest.mock import patch, MagicMock

# Import module-level functions and constants
from pipelines.agent import (
    call_code_buddy,
    _validate_symbol_name,
    _validate_file_path,
    _is_circuit_open,
    _record_code_buddy_failure,
    _record_code_buddy_success,
    _CODE_BUDDY_FAILURE_THRESHOLD,
    TOOLS
)


class TestValidation:
    """Test input validation functions."""

    def test_validate_symbol_name_valid(self):
        assert _validate_symbol_name("HandleUser") == "HandleUser"
        assert _validate_symbol_name("User") == "User"
        assert _validate_symbol_name("pkg.Function") == "pkg.Function"
        assert _validate_symbol_name("_private") == "_private"

    def test_validate_symbol_name_invalid(self):
        with pytest.raises(ValueError, match="must be 1-200 characters"):
            _validate_symbol_name("")

        with pytest.raises(ValueError, match="must be 1-200 characters"):
            _validate_symbol_name("a" * 201)

        with pytest.raises(ValueError, match="Invalid symbol name format"):
            _validate_symbol_name("123invalid")

        with pytest.raises(ValueError, match="Invalid symbol name format"):
            _validate_symbol_name("has spaces")

    def test_validate_file_path_valid(self):
        assert _validate_file_path("main.go") == "main.go"
        assert _validate_file_path("handlers/user.go") == "handlers/user.go"

    def test_validate_file_path_invalid(self):
        with pytest.raises(ValueError, match="must be 1-500 characters"):
            _validate_file_path("")

        with pytest.raises(ValueError, match="must be 1-500 characters"):
            _validate_file_path("a" * 501)

        with pytest.raises(ValueError, match="Path traversal not allowed"):
            _validate_file_path("../etc/passwd")


class TestCircuitBreaker:
    """Test circuit breaker functionality."""

    def setup_method(self):
        """Reset circuit breaker state before each test."""
        import pipelines.agent as agent_module
        agent_module._code_buddy_failures = 0
        agent_module._code_buddy_circuit_open_until = 0.0

    def test_circuit_initially_closed(self):
        assert not _is_circuit_open()

    def test_circuit_opens_after_failures(self):
        for _ in range(_CODE_BUDDY_FAILURE_THRESHOLD):
            _record_code_buddy_failure()

        assert _is_circuit_open()

    def test_circuit_closes_on_success(self):
        for _ in range(_CODE_BUDDY_FAILURE_THRESHOLD):
            _record_code_buddy_failure()

        assert _is_circuit_open()

        _record_code_buddy_success()
        assert not _is_circuit_open()

    def test_circuit_recovers_after_timeout(self):
        import pipelines.agent as agent_module

        for _ in range(_CODE_BUDDY_FAILURE_THRESHOLD):
            _record_code_buddy_failure()

        # Set recovery to past
        agent_module._code_buddy_circuit_open_until = time.time() - 1
        assert not _is_circuit_open()


class TestCallCodeBuddy:
    """Test Code Buddy HTTP client."""

    def setup_method(self):
        """Reset circuit breaker state before each test."""
        import pipelines.agent as agent_module
        agent_module._code_buddy_failures = 0
        agent_module._code_buddy_circuit_open_until = 0.0

    @patch('pipelines.agent.httpx.Client')
    def test_successful_get_request(self, mock_client_class):
        mock_response = MagicMock()
        mock_response.status_code = 200
        mock_response.json.return_value = {"callers": []}

        mock_client = MagicMock()
        mock_client.get.return_value = mock_response
        mock_client.__enter__ = MagicMock(return_value=mock_client)
        mock_client.__exit__ = MagicMock(return_value=False)
        mock_client_class.return_value = mock_client

        result = call_code_buddy("callers", params={"function": "test"})

        assert result == {"callers": []}
        mock_client.get.assert_called_once()

    @patch('pipelines.agent.httpx.Client')
    def test_successful_post_request(self, mock_client_class):
        mock_response = MagicMock()
        mock_response.status_code = 200
        mock_response.json.return_value = {"context": "..."}

        mock_client = MagicMock()
        mock_client.post.return_value = mock_response
        mock_client.__enter__ = MagicMock(return_value=mock_client)
        mock_client.__exit__ = MagicMock(return_value=False)
        mock_client_class.return_value = mock_client

        result = call_code_buddy("context", body={"query": "test"})

        assert result == {"context": "..."}
        mock_client.post.assert_called_once()

    @patch('pipelines.agent.httpx.Client')
    def test_404_error_handling(self, mock_client_class):
        mock_response = MagicMock()
        mock_response.status_code = 404
        mock_response.json.return_value = {"error": "not found"}

        mock_client = MagicMock()
        mock_client.get.return_value = mock_response
        mock_client.__enter__ = MagicMock(return_value=mock_client)
        mock_client.__exit__ = MagicMock(return_value=False)
        mock_client_class.return_value = mock_client

        result = call_code_buddy("symbol/NotFound", context={"name": "NotFound"})

        assert "error" in result
        assert "not found" in result["error"].lower()
        assert "suggestion" in result

    @patch('pipelines.agent.httpx.Client')
    def test_circuit_breaker_returns_fast_failure(self, mock_client_class):
        import pipelines.agent as agent_module

        # Open the circuit
        agent_module._code_buddy_circuit_open_until = time.time() + 60

        result = call_code_buddy("test")

        assert "error" in result
        assert "temporarily disabled" in result["error"]
        assert "suggestion" in result

        # Client should not be called
        mock_client_class.assert_not_called()


class TestToolDefinitions:
    """Test tool definitions are valid."""

    def test_all_tools_have_required_fields(self):
        for tool in TOOLS:
            assert tool["type"] == "function"
            assert "function" in tool
            func = tool["function"]
            assert "name" in func
            assert "description" in func
            assert "parameters" in func

    def test_tools_have_valid_parameters(self):
        for tool in TOOLS:
            func = tool["function"]
            params = func["parameters"]
            assert params["type"] == "object"
            assert "properties" in params

    def test_new_tools_present(self):
        tool_names = [t["function"]["name"] for t in TOOLS]

        expected_tools = [
            "list_files", "read_file",  # Existing
            "get_context", "find_symbol", "find_callers", "find_callees",
            "find_implementations", "find_references", "get_type_info",
            "get_imports", "get_dependency_tree", "search_library_docs"
        ]

        for name in expected_tools:
            assert name in tool_names, f"Missing tool: {name}"

    def test_tools_have_limit_parameters(self):
        """Tools that can return many results should have limit parameters."""
        tools_needing_limits = ["find_callers", "find_callees", "find_implementations", "find_references"]

        for tool in TOOLS:
            name = tool["function"]["name"]
            if name in tools_needing_limits:
                props = tool["function"]["parameters"]["properties"]
                assert "limit" in props, f"{name} should have a limit parameter"


class TestToolExecution:
    """Test tool execution with mocked Code Buddy."""

    def setup_method(self):
        """Reset circuit breaker state before each test."""
        import pipelines.agent as agent_module
        agent_module._code_buddy_failures = 0
        agent_module._code_buddy_circuit_open_until = 0.0

    @patch('pipelines.agent.call_code_buddy')
    def test_find_callers_validates_input(self, mock_call):
        mock_call.return_value = {"callers": []}

        from pipelines.agent import AgentPipeline

        # Create a minimal mock config
        mock_config = {"llm_service_url": "http://localhost:11434"}
        mock_client = MagicMock()

        with patch.object(AgentPipeline, '__init__', lambda x, y, z: None):
            agent = AgentPipeline.__new__(AgentPipeline)
            agent.project_root = "/app/codebase"

            # Test with valid input
            result = agent._execute_tool("find_callers", {"function_name": "HandleUser"})
            parsed = json.loads(result)
            assert "callers" in parsed

    @patch('pipelines.agent.call_code_buddy')
    def test_find_callers_enforces_limit(self, mock_call):
        mock_call.return_value = {"callers": []}

        from pipelines.agent import AgentPipeline

        with patch.object(AgentPipeline, '__init__', lambda x, y, z: None):
            agent = AgentPipeline.__new__(AgentPipeline)
            agent.project_root = "/app/codebase"

            # Request limit above maximum
            agent._execute_tool("find_callers", {"function_name": "Test", "limit": 1000})

            # Should cap at 200
            call_args = mock_call.call_args
            assert call_args[1]["params"]["limit"] <= 200
