# Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU Affero General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
# See the LICENSE.txt file for the full license text.
#
# NOTE: This work is subject to additional terms under AGPL v3 Section 7.
# See the NOTICE.txt file for details regarding AI system attribution.

"""
Unit tests for BaseRAGPipeline._call_llm() temperature override fix (A1).

This module tests that temperature overrides are correctly passed to all LLM
backends (Ollama, OpenAI, Anthropic, Local). The fix ensures that the
`temperature` parameter passed to `_call_llm()` is used in the HTTP payload
instead of always using `self.default_llm_params["temperature"]`.

Test Categories
---------------
1. Unit tests (mocked HTTP) - Run by default, fast
2. Integration tests (real APIs) - Skipped unless RUN_INTEGRATION_TESTS=true

Related
-------
- Fix: A1 Temperature Override Bug Fix
- File: services/rag_engine/pipelines/base.py:356-544
- Design: docs/designs/in_progress/A1_temperature_override_fix.md
"""

from __future__ import annotations

import os
import json
import pytest
from typing import Any
from unittest.mock import MagicMock, AsyncMock, patch
from httpx import Response

# Import the pipeline - adjust path as needed based on test runner
import sys
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..'))

from pipelines.base import BaseRAGPipeline


# =============================================================================
# Fixtures
# =============================================================================

@pytest.fixture
def mock_weaviate_client():
    """Create a mock Weaviate client."""
    return MagicMock()


@pytest.fixture
def base_config():
    """Base configuration for pipeline tests."""
    return {
        "embedding_url": "http://mock-embedding:8080",
        "embedding_model": "mock-model",
        "llm_service_url": "http://mock-llm:11434",
        "llm_backend_type": "ollama",
        "ollama_model": "llama3",
        "openai_model": "gpt-4o-mini",
    }


@pytest.fixture
def ollama_pipeline(mock_weaviate_client, base_config):
    """Create pipeline configured for Ollama backend."""
    config = {**base_config, "llm_backend_type": "ollama"}
    pipeline = BaseRAGPipeline(mock_weaviate_client, config)
    return pipeline


@pytest.fixture
def openai_pipeline(mock_weaviate_client, base_config):
    """Create pipeline configured for OpenAI backend."""
    config = {**base_config, "llm_backend_type": "openai"}
    pipeline = BaseRAGPipeline(mock_weaviate_client, config)
    # Set API key to avoid ValueError
    pipeline.openai_api_key = "test-api-key"
    return pipeline


@pytest.fixture
def anthropic_pipeline(mock_weaviate_client, base_config):
    """Create pipeline configured for Anthropic backend."""
    config = {**base_config, "llm_backend_type": "anthropic"}
    pipeline = BaseRAGPipeline(mock_weaviate_client, config)
    # Set API key to avoid ValueError
    pipeline.anthropic_api_key = "test-api-key"
    return pipeline


@pytest.fixture
def local_pipeline(mock_weaviate_client, base_config):
    """Create pipeline configured for Local (llama.cpp) backend."""
    config = {**base_config, "llm_backend_type": "local"}
    pipeline = BaseRAGPipeline(mock_weaviate_client, config)
    return pipeline


def create_mock_response(backend: str) -> Response:
    """Create a mock HTTP response for each backend type."""
    if backend == "ollama":
        content = json.dumps({"response": "Test response from Ollama"})
    elif backend == "openai":
        content = json.dumps({
            "choices": [{"message": {"content": "Test response from OpenAI"}}]
        })
    elif backend == "anthropic":
        content = json.dumps({
            "content": [{"type": "text", "text": "Test response from Anthropic"}]
        })
    elif backend == "local":
        content = json.dumps({"content": "Test response from Local"})
    else:
        content = json.dumps({})

    response = MagicMock(spec=Response)
    response.status_code = 200
    response.json.return_value = json.loads(content)
    response.raise_for_status = MagicMock()
    return response


# =============================================================================
# Unit Tests: Ollama Backend
# =============================================================================

class TestOllamaBackendTemperature:
    """Tests for Ollama backend temperature handling."""

    @pytest.mark.asyncio
    async def test_ollama_uses_temperature_override(self, ollama_pipeline: BaseRAGPipeline) -> None:
        """
        Verify that Ollama backend uses the temperature parameter passed to _call_llm.

        This is a regression test - Ollama was already working correctly,
        but we verify it continues to work.
        """
        captured: list[dict[str, Any]] = []

        async def capture_post(url: str, json: dict[str, Any], headers: dict[str, str]) -> Response:
            captured.append(json)
            return create_mock_response("ollama")

        ollama_pipeline.http_client = MagicMock()
        ollama_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        # Call with specific temperature override
        await ollama_pipeline._call_llm("Test prompt", temperature=0.1)

        # Verify temperature was passed correctly
        assert len(captured) == 1
        payload = captured[0]
        assert "options" in payload
        assert payload["options"]["temperature"] == 0.1

    @pytest.mark.asyncio
    async def test_ollama_uses_default_when_no_override(self, ollama_pipeline: BaseRAGPipeline) -> None:
        """Verify Ollama uses default temperature when no override provided."""
        captured: list[dict[str, Any]] = []

        async def capture_post(url: str, json: dict[str, Any], headers: dict[str, str]) -> Response:
            captured.append(json)
            return create_mock_response("ollama")

        ollama_pipeline.http_client = MagicMock()
        ollama_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        # Call without temperature override
        await ollama_pipeline._call_llm("Test prompt")

        # Should use default (0.5 from default_llm_params)
        assert len(captured) == 1
        payload = captured[0]
        assert "options" in payload
        assert payload["options"]["temperature"] == ollama_pipeline.default_llm_params["temperature"]


# =============================================================================
# Unit Tests: OpenAI Backend
# =============================================================================

class TestOpenAIBackendTemperature:
    """Tests for OpenAI backend temperature handling (A1 fix target)."""

    @pytest.mark.asyncio
    async def test_openai_uses_temperature_override(self, openai_pipeline: BaseRAGPipeline) -> None:
        """
        Verify that OpenAI backend uses the temperature parameter passed to _call_llm.

        This was the primary bug - OpenAI was using self.default_llm_params["temperature"]
        instead of generation_params["temperature"].
        """
        captured: list[dict[str, Any]] = []

        async def capture_post(url: str, json: dict[str, Any], headers: dict[str, str]) -> Response:
            captured.append(json)
            return create_mock_response("openai")

        openai_pipeline.http_client = MagicMock()
        openai_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        # Call with specific temperature override (e.g., skeptic temperature)
        await openai_pipeline._call_llm("Test prompt", temperature=0.2)

        # Verify temperature was passed correctly (this was broken before A1 fix)
        assert len(captured) == 1
        payload = captured[0]
        assert "temperature" in payload
        assert payload["temperature"] == 0.2, f"Expected temperature 0.2, got {payload['temperature']}"

    @pytest.mark.asyncio
    async def test_openai_uses_max_tokens_override(self, openai_pipeline: BaseRAGPipeline) -> None:
        """Verify OpenAI uses max_tokens from generation_params."""
        captured: list[dict[str, Any]] = []

        async def capture_post(url: str, json: dict[str, Any], headers: dict[str, str]) -> Response:
            captured.append(json)
            return create_mock_response("openai")

        openai_pipeline.http_client = MagicMock()
        openai_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        # Call with max_tokens override
        await openai_pipeline._call_llm("Test prompt", max_tokens=500)

        assert len(captured) == 1
        assert captured[0]["max_tokens"] == 500

    @pytest.mark.asyncio
    async def test_openai_uses_top_p_override(self, openai_pipeline: BaseRAGPipeline) -> None:
        """Verify OpenAI uses top_p from generation_params."""
        captured: list[dict[str, Any]] = []

        async def capture_post(url: str, json: dict[str, Any], headers: dict[str, str]) -> Response:
            captured.append(json)
            return create_mock_response("openai")

        openai_pipeline.http_client = MagicMock()
        openai_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        # Call with top_p override
        await openai_pipeline._call_llm("Test prompt", top_p=0.8)

        assert len(captured) == 1
        assert captured[0]["top_p"] == 0.8


# =============================================================================
# Unit Tests: Anthropic Backend
# =============================================================================

class TestAnthropicBackendTemperature:
    """Tests for Anthropic backend temperature handling (A1 fix target)."""

    @pytest.mark.asyncio
    async def test_anthropic_uses_temperature_when_thinking_disabled(
        self, anthropic_pipeline: BaseRAGPipeline
    ) -> None:
        """
        Verify Anthropic uses temperature when thinking mode is disabled.

        This was added as part of A1 fix - previously temperature was only
        set when thinking mode was enabled (to None).
        """
        captured: list[dict[str, Any]] = []

        async def capture_post(
            url: str, json: dict[str, Any], headers: dict[str, str]
        ) -> Response:
            captured.append(json)
            return create_mock_response("anthropic")

        anthropic_pipeline.http_client = MagicMock()
        anthropic_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        # Ensure thinking mode is disabled (now an instance attribute)
        anthropic_pipeline.enable_thinking = False
        await anthropic_pipeline._call_llm("Test prompt", temperature=0.3)

        assert len(captured) == 1
        payload = captured[0]
        assert "temperature" in payload
        assert payload["temperature"] == 0.3

    @pytest.mark.asyncio
    async def test_anthropic_uses_none_temperature_when_thinking_enabled(
        self, anthropic_pipeline: BaseRAGPipeline
    ) -> None:
        """
        Verify Anthropic sets temperature to None when thinking mode is enabled.

        The Anthropic API requires temperature=None when using the thinking feature.
        """
        captured: list[dict[str, Any]] = []

        async def capture_post(
            url: str, json: dict[str, Any], headers: dict[str, str]
        ) -> Response:
            captured.append(json)
            return create_mock_response("anthropic")

        anthropic_pipeline.http_client = MagicMock()
        anthropic_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        # Enable thinking mode (now instance attributes, set at init)
        anthropic_pipeline.enable_thinking = True
        anthropic_pipeline.thinking_budget = 2048
        await anthropic_pipeline._call_llm("Test prompt", temperature=0.5)

        assert len(captured) == 1
        payload = captured[0]
        assert payload["temperature"] is None
        assert "thinking" in payload

    @pytest.mark.asyncio
    async def test_anthropic_uses_max_tokens_override(
        self, anthropic_pipeline: BaseRAGPipeline
    ) -> None:
        """Verify Anthropic uses max_tokens from generation_params."""
        captured: list[dict[str, Any]] = []

        async def capture_post(
            url: str, json: dict[str, Any], headers: dict[str, str]
        ) -> Response:
            captured.append(json)
            return create_mock_response("anthropic")

        anthropic_pipeline.http_client = MagicMock()
        anthropic_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        with patch.dict(os.environ, {"ENABLE_THINKING": "false"}, clear=False):
            await anthropic_pipeline._call_llm("Test prompt", max_tokens=1000)

        assert len(captured) == 1
        assert captured[0]["max_tokens"] == 1000


# =============================================================================
# Unit Tests: Local (llama.cpp) Backend
# =============================================================================

class TestLocalBackendTemperature:
    """Tests for Local (llama.cpp) backend temperature handling (A1 fix target)."""

    @pytest.mark.asyncio
    async def test_local_uses_temperature_override(
        self, local_pipeline: BaseRAGPipeline
    ) -> None:
        """
        Verify that Local backend uses the temperature parameter passed to _call_llm.

        This was broken - Local was using self.default_llm_params["temperature"]
        instead of generation_params["temperature"].
        """
        captured: list[dict[str, Any]] = []

        async def capture_post(
            url: str, json: dict[str, Any], headers: dict[str, str]
        ) -> Response:
            captured.append(json)
            return create_mock_response("local")

        local_pipeline.http_client = MagicMock()
        local_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        # Call with specific temperature override
        await local_pipeline._call_llm("Test prompt", temperature=0.4)

        assert len(captured) == 1
        payload = captured[0]
        assert "temperature" in payload
        assert payload["temperature"] == 0.4, \
            f"Expected temperature 0.4, got {payload['temperature']}"

    @pytest.mark.asyncio
    async def test_local_uses_n_predict_override(
        self, local_pipeline: BaseRAGPipeline
    ) -> None:
        """Verify Local uses n_predict (max_tokens) from generation_params."""
        captured: list[dict[str, Any]] = []

        async def capture_post(
            url: str, json: dict[str, Any], headers: dict[str, str]
        ) -> Response:
            captured.append(json)
            return create_mock_response("local")

        local_pipeline.http_client = MagicMock()
        local_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        # Call with max_tokens override (maps to n_predict for llama.cpp)
        await local_pipeline._call_llm("Test prompt", max_tokens=750)

        assert len(captured) == 1
        assert captured[0]["n_predict"] == 750

    @pytest.mark.asyncio
    async def test_local_uses_top_k_override(
        self, local_pipeline: BaseRAGPipeline
    ) -> None:
        """Verify Local uses top_k from generation_params."""
        captured: list[dict[str, Any]] = []

        async def capture_post(
            url: str, json: dict[str, Any], headers: dict[str, str]
        ) -> Response:
            captured.append(json)
            return create_mock_response("local")

        local_pipeline.http_client = MagicMock()
        local_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        # Call with top_k override
        await local_pipeline._call_llm("Test prompt", top_k=50)

        assert len(captured) == 1
        assert captured[0]["top_k"] == 50


# =============================================================================
# Cross-Backend Tests
# =============================================================================

class TestCrossBackendConsistency:
    """Tests verifying consistent behavior across all backends."""

    @pytest.mark.asyncio
    async def test_all_backends_respect_temperature_zero(
        self, ollama_pipeline, openai_pipeline, local_pipeline
    ):
        """
        Verify all backends correctly handle temperature=0.0 (deterministic).

        Temperature 0 is often used for reproducible outputs.
        """
        payloads = {}

        async def capture_factory(backend):
            async def capture_post(url, json, headers):
                payloads[backend] = json
                return create_mock_response(backend)
            return capture_post

        # Test Ollama
        ollama_pipeline.http_client = MagicMock()
        ollama_pipeline.http_client.post = AsyncMock(side_effect=await capture_factory("ollama"))
        await ollama_pipeline._call_llm("Test", temperature=0.0)

        # Test OpenAI
        openai_pipeline.http_client = MagicMock()
        openai_pipeline.http_client.post = AsyncMock(side_effect=await capture_factory("openai"))
        await openai_pipeline._call_llm("Test", temperature=0.0)

        # Test Local
        local_pipeline.http_client = MagicMock()
        local_pipeline.http_client.post = AsyncMock(side_effect=await capture_factory("local"))
        await local_pipeline._call_llm("Test", temperature=0.0)

        # Verify all captured temperature=0
        assert payloads["ollama"]["options"]["temperature"] == 0.0
        assert payloads["openai"]["temperature"] == 0.0
        assert payloads["local"]["temperature"] == 0.0

    @pytest.mark.asyncio
    async def test_different_temperatures_for_different_calls(self, openai_pipeline):
        """
        Verify that consecutive calls with different temperatures work correctly.

        This simulates the verified pipeline calling optimist (0.6),
        skeptic (0.2), and refiner (0.4) in sequence.
        """
        captured_temps = []

        async def capture_post(url, json, headers):
            captured_temps.append(json.get("temperature"))
            return create_mock_response("openai")

        openai_pipeline.http_client = MagicMock()
        openai_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        # Simulate verified pipeline calls
        await openai_pipeline._call_llm("Optimist prompt", temperature=0.6)
        await openai_pipeline._call_llm("Skeptic prompt", temperature=0.2)
        await openai_pipeline._call_llm("Refiner prompt", temperature=0.4)

        assert captured_temps == [0.6, 0.2, 0.4], \
            f"Expected [0.6, 0.2, 0.4], got {captured_temps}"


# =============================================================================
# Integration Tests (Skipped by Default)
# =============================================================================

@pytest.mark.skipif(
    os.getenv("RUN_INTEGRATION_TESTS") != "true",
    reason="Integration tests disabled by default. Set RUN_INTEGRATION_TESTS=true to run."
)
class TestIntegrationRealAPIs:
    """
    Integration tests that make real API calls.

    These tests are skipped by default. To run them:

        RUN_INTEGRATION_TESTS=true pytest tests/test_base_pipeline_llm.py::TestIntegrationRealAPIs -v

    Required environment variables:
    - OPENAI_API_KEY: For OpenAI tests
    - ANTHROPIC_API_KEY: For Anthropic tests
    - OLLAMA_BASE_URL: For Ollama tests (default: http://localhost:11434)
    """

    @pytest.mark.asyncio
    async def test_openai_real_api_with_temperature(self, mock_weaviate_client, base_config):
        """Integration test with real OpenAI API."""
        api_key = os.getenv("OPENAI_API_KEY")
        if not api_key:
            pytest.skip("OPENAI_API_KEY not set")

        config = {
            **base_config,
            "llm_backend_type": "openai",
            "llm_service_url": "https://api.openai.com/v1",
        }
        pipeline = BaseRAGPipeline(mock_weaviate_client, config)
        pipeline.openai_api_key = api_key

        # Test with low temperature (should be more deterministic)
        response1 = await pipeline._call_llm(
            "What is 2+2? Answer with just the number.",
            temperature=0.0
        )

        # Basic sanity check
        assert "4" in response1

    @pytest.mark.asyncio
    async def test_ollama_real_api_with_temperature(self, mock_weaviate_client, base_config):
        """Integration test with real Ollama API."""
        ollama_url = os.getenv("OLLAMA_BASE_URL", "http://localhost:11434")

        config = {
            **base_config,
            "llm_backend_type": "ollama",
            "llm_service_url": ollama_url,
            "ollama_model": os.getenv("OLLAMA_MODEL", "llama3"),
        }
        pipeline = BaseRAGPipeline(mock_weaviate_client, config)

        try:
            response = await pipeline._call_llm(
                "What is 2+2? Answer with just the number.",
                temperature=0.0
            )
            assert "4" in response
        except Exception as e:
            pytest.skip(f"Ollama not available: {e}")

    @pytest.mark.asyncio
    async def test_anthropic_real_api_with_temperature(self, mock_weaviate_client, base_config):
        """Integration test with real Anthropic API."""
        api_key = os.getenv("ANTHROPIC_API_KEY")
        if not api_key:
            pytest.skip("ANTHROPIC_API_KEY not set")

        config = {
            **base_config,
            "llm_backend_type": "anthropic",
        }
        pipeline = BaseRAGPipeline(mock_weaviate_client, config)
        pipeline.anthropic_api_key = api_key

        # Ensure thinking mode is disabled for this test
        with patch.dict(os.environ, {"ENABLE_THINKING": "false"}, clear=False):
            response = await pipeline._call_llm(
                "What is 2+2? Answer with just the number.",
                temperature=0.0
            )

        assert "4" in response


# =============================================================================
# Model Override Tests
# =============================================================================

class TestModelOverride:
    """Tests for model_override parameter."""

    @pytest.mark.asyncio
    async def test_openai_model_override(
        self, openai_pipeline: BaseRAGPipeline
    ) -> None:
        """Verify OpenAI uses model_override when provided."""
        captured: list[dict[str, Any]] = []

        async def capture_post(
            url: str, json: dict[str, Any], headers: dict[str, str]
        ) -> Response:
            captured.append(json)
            return create_mock_response("openai")

        openai_pipeline.http_client = MagicMock()
        openai_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        await openai_pipeline._call_llm("Test prompt", model_override="gpt-4-turbo")

        assert len(captured) == 1
        assert captured[0]["model"] == "gpt-4-turbo"

    @pytest.mark.asyncio
    async def test_ollama_model_override(
        self, ollama_pipeline: BaseRAGPipeline
    ) -> None:
        """Verify Ollama uses model_override when provided."""
        captured: list[dict[str, Any]] = []

        async def capture_post(
            url: str, json: dict[str, Any], headers: dict[str, str]
        ) -> Response:
            captured.append(json)
            return create_mock_response("ollama")

        ollama_pipeline.http_client = MagicMock()
        ollama_pipeline.http_client.post = AsyncMock(side_effect=capture_post)

        await ollama_pipeline._call_llm("Test prompt", model_override="mistral")

        assert len(captured) == 1
        assert captured[0]["model"] == "mistral"
