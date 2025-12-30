# Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
# Tests for the Aleutian Forecast Service

import pytest
from unittest.mock import Mock, patch, MagicMock
from fastapi.testclient import TestClient
import numpy as np
import torch

# Import the server module
from server import (
    app,
    ForecastRequest,
    ForecastResponse,
    ModelInfo,
    normalize_model_slug,
    MODEL_COMPATIBILITY,
    LOADED_MODELS,
    MODEL_IN_USE,
)


@pytest.fixture
def client():
    """Create a test client for the FastAPI app."""
    return TestClient(app)


@pytest.fixture(autouse=True)
def clear_models():
    """Clear loaded models before each test."""
    LOADED_MODELS.clear()
    MODEL_IN_USE.clear()
    yield
    LOADED_MODELS.clear()
    MODEL_IN_USE.clear()


class TestNormalizeModelSlug:
    """Tests for the normalize_model_slug function."""

    def test_strip_org_prefix(self):
        assert normalize_model_slug("amazon/chronos-t5-tiny") == "chronos-t5-tiny"
        assert normalize_model_slug("google/timesfm-1.0-200m") == "timesfm-1-0-200m"

    def test_already_normalized(self):
        assert normalize_model_slug("chronos-t5-tiny") == "chronos-t5-tiny"

    def test_replace_special_chars(self):
        assert normalize_model_slug("Chronos T5 (Tiny)") == "chronos-t5-tiny"
        assert normalize_model_slug("chronos_t5_tiny") == "chronos-t5-tiny"

    def test_case_insensitive(self):
        assert normalize_model_slug("CHRONOS-T5-TINY") == "chronos-t5-tiny"
        assert normalize_model_slug("Amazon/Chronos-T5-Tiny") == "chronos-t5-tiny"


class TestHealthEndpoint:
    """Tests for the /health endpoint."""

    def test_health_returns_status(self, client):
        response = client.get("/health")
        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "healthy"
        assert "cuda_available" in data
        assert "loaded_models" in data
        assert "slots_used" in data


class TestListModelsEndpoint:
    """Tests for the /v1/models endpoint."""

    def test_list_models(self, client):
        response = client.get("/v1/models")
        assert response.status_code == 200
        models = response.json()
        assert isinstance(models, list)
        assert len(models) > 0

        # Check structure of first model
        model = models[0]
        assert "slug" in model
        assert "status" in model
        assert "vram_gb" in model
        assert "huggingface_id" in model
        assert "loaded" in model

    def test_all_known_models_included(self, client):
        response = client.get("/v1/models")
        models = response.json()
        slugs = {m["slug"] for m in models}

        # Check some known models are present
        assert "chronos-t5-tiny" in slugs
        assert "chronos-t5-base" in slugs


class TestModelLoadEndpoint:
    """Tests for the /v1/models/load endpoint."""

    def test_load_unknown_model_returns_error(self, client):
        response = client.post(
            "/v1/models/load",
            json={"model": "nonexistent-model"}
        )
        assert response.status_code == 400
        assert "Unknown model" in response.json()["detail"]

    def test_load_broken_model_returns_error(self, client):
        response = client.post(
            "/v1/models/load",
            json={"model": "chronos-bolt-mini"}  # Marked as broken
        )
        assert response.status_code == 400
        assert "broken" in response.json()["detail"]

    @patch("server.ChronosPipeline")
    def test_load_model_success(self, mock_pipeline_class, client):
        """Test successful model loading with mocked pipeline."""
        mock_pipeline = MagicMock()
        mock_pipeline_class.from_pretrained.return_value = mock_pipeline

        response = client.post(
            "/v1/models/load",
            json={"model": "chronos-t5-tiny"}
        )

        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "loaded"
        assert data["model"] == "chronos-t5-tiny"
        assert "chronos-t5-tiny" in data["loaded_models"]


class TestModelUnloadEndpoint:
    """Tests for the /v1/models/unload endpoint."""

    def test_unload_not_loaded_returns_error(self, client):
        response = client.post(
            "/v1/models/unload",
            json={"model": "chronos-t5-tiny"}
        )
        assert response.status_code == 404
        assert "not loaded" in response.json()["detail"]

    @patch("server.ChronosPipeline")
    def test_unload_success(self, mock_pipeline_class, client):
        """Test successful model unloading."""
        mock_pipeline = MagicMock()
        mock_pipeline_class.from_pretrained.return_value = mock_pipeline

        # First load the model
        client.post("/v1/models/load", json={"model": "chronos-t5-tiny"})

        # Then unload
        response = client.post(
            "/v1/models/unload",
            json={"model": "chronos-t5-tiny"}
        )

        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "unloaded"
        assert "chronos-t5-tiny" not in data["loaded_models"]


class TestForecastEndpoint:
    """Tests for the /v1/timeseries/forecast endpoint."""

    def test_forecast_missing_data_returns_error(self, client):
        response = client.post(
            "/v1/timeseries/forecast",
            json={
                "model": "chronos-t5-tiny",
                "horizon": 5
            }
        )
        assert response.status_code == 422  # Validation error

    def test_forecast_insufficient_data_returns_error(self, client):
        response = client.post(
            "/v1/timeseries/forecast",
            json={
                "model": "chronos-t5-tiny",
                "data": [1.0, 2.0, 3.0],  # Only 3 points, need 10
                "horizon": 5
            }
        )
        assert response.status_code == 400
        assert "at least 10" in response.json()["detail"]

    def test_forecast_unknown_model_returns_error(self, client):
        response = client.post(
            "/v1/timeseries/forecast",
            json={
                "model": "nonexistent-model",
                "data": list(range(100)),
                "horizon": 5
            }
        )
        assert response.status_code == 400
        assert "Unknown model" in response.json()["detail"]

    def test_forecast_broken_model_returns_error(self, client):
        response = client.post(
            "/v1/timeseries/forecast",
            json={
                "model": "chronos-bolt-mini",
                "data": list(range(100)),
                "horizon": 5
            }
        )
        assert response.status_code == 400
        assert "broken" in response.json()["detail"]

    @patch("server.load_chronos_model")
    def test_forecast_success(self, mock_load_model, client):
        """Test successful forecast with mocked model."""
        # Create mock pipeline
        mock_pipeline = MagicMock()
        mock_forecast = torch.tensor([[[1.0, 2.0, 3.0, 4.0, 5.0]] * 20])
        mock_pipeline.predict.return_value = mock_forecast
        mock_load_model.return_value = mock_pipeline

        response = client.post(
            "/v1/timeseries/forecast",
            json={
                "model": "chronos-t5-tiny",
                "data": list(range(100)),
                "horizon": 5,
                "num_samples": 20
            }
        )

        assert response.status_code == 200
        data = response.json()
        assert data["model"] == "chronos-t5-tiny"
        assert len(data["forecast"]) == 5
        assert data["horizon"] == 5
        assert "forecast_low" in data
        assert "forecast_high" in data

    def test_forecast_accepts_recent_data_alias(self, client):
        """Test that recent_data field works as alias for data."""
        with patch("server.load_chronos_model") as mock_load:
            mock_pipeline = MagicMock()
            mock_forecast = torch.tensor([[[1.0, 2.0, 3.0, 4.0, 5.0]] * 20])
            mock_pipeline.predict.return_value = mock_forecast
            mock_load.return_value = mock_pipeline

            response = client.post(
                "/v1/timeseries/forecast",
                json={
                    "model": "chronos-t5-tiny",
                    "recent_data": list(range(100)),  # Using alias
                    "horizon": 5
                }
            )

            assert response.status_code == 200


class TestForecastRequest:
    """Tests for the ForecastRequest model."""

    def test_data_and_recent_data_both_work(self):
        # Using data field
        req1 = ForecastRequest(model="chronos-t5-tiny", data=[1.0, 2.0, 3.0])
        assert req1.data == [1.0, 2.0, 3.0]

        # Using recent_data field
        req2 = ForecastRequest(model="chronos-t5-tiny", recent_data=[4.0, 5.0, 6.0])
        assert req2.data == [4.0, 5.0, 6.0]

    def test_forecast_period_size_maps_to_horizon(self):
        req = ForecastRequest(
            model="chronos-t5-tiny",
            data=[1.0] * 20,
            forecast_period_size=10
        )
        assert req.horizon == 10

    def test_raises_when_no_data(self):
        with pytest.raises(ValueError, match="Either 'data' or 'recent_data'"):
            ForecastRequest(model="chronos-t5-tiny")


class TestModelCompatibility:
    """Tests for the MODEL_COMPATIBILITY configuration."""

    def test_all_chronos_t5_verified(self):
        for size in ["tiny", "mini", "small", "base", "large"]:
            slug = f"chronos-t5-{size}"
            assert slug in MODEL_COMPATIBILITY
            assert MODEL_COMPATIBILITY[slug]["status"] == "verified"

    def test_chronos_bolt_marked_broken(self):
        for size in ["mini", "small", "base"]:
            slug = f"chronos-bolt-{size}"
            assert slug in MODEL_COMPATIBILITY
            assert MODEL_COMPATIBILITY[slug]["status"] == "broken"

    def test_all_models_have_required_fields(self):
        for slug, info in MODEL_COMPATIBILITY.items():
            assert "status" in info, f"{slug} missing status"
            assert "vram_gb" in info, f"{slug} missing vram_gb"
            assert "huggingface_id" in info, f"{slug} missing huggingface_id"


class TestModelEviction:
    """Tests for FIFO model eviction."""

    @patch("server.ChronosPipeline")
    @patch("server.MAX_LOADED_MODELS", 2)  # Lower limit for testing
    def test_eviction_when_at_capacity(self, mock_pipeline_class, client):
        """Test that oldest model is evicted when at capacity."""
        mock_pipeline = MagicMock()
        mock_pipeline_class.from_pretrained.return_value = mock_pipeline

        # Load first model
        client.post("/v1/models/load", json={"model": "chronos-t5-tiny"})
        # Load second model
        client.post("/v1/models/load", json={"model": "chronos-t5-mini"})

        # Check both are loaded
        health = client.get("/health").json()
        assert len(health["loaded_models"]) == 2

        # Load third model (should evict first)
        client.post("/v1/models/load", json={"model": "chronos-t5-small"})

        health = client.get("/health").json()
        # First model should be evicted
        assert "chronos-t5-tiny" not in health["loaded_models"]
        assert "chronos-t5-mini" in health["loaded_models"]
        assert "chronos-t5-small" in health["loaded_models"]


class TestEvictOldestModel:
    """Tests for the evict_oldest_model function."""

    def test_eviction_with_max_attempts(self):
        """Test that eviction respects max_attempts parameter."""
        from server import evict_oldest_model, LOADED_MODELS, MODEL_IN_USE, MODEL_LOCK

        # Clear state
        LOADED_MODELS.clear()
        MODEL_IN_USE.clear()

        # Add a model that's in use
        LOADED_MODELS["test-model"] = MagicMock()
        MODEL_IN_USE["test-model"] = 1  # Mark as in use

        # Try to evict with only 2 attempts (should fail quickly)
        with pytest.raises(RuntimeError) as excinfo:
            evict_oldest_model(max_attempts=2)

        assert "Unable to evict" in str(excinfo.value)
        assert "2 attempts" in str(excinfo.value)

        # Clean up
        LOADED_MODELS.clear()
        MODEL_IN_USE.clear()

    def test_eviction_succeeds_when_model_available(self):
        """Test that eviction works when a model is not in use."""
        from server import evict_oldest_model, LOADED_MODELS, MODEL_IN_USE

        # Clear state
        LOADED_MODELS.clear()
        MODEL_IN_USE.clear()

        # Add a model that's NOT in use
        LOADED_MODELS["available-model"] = MagicMock()
        MODEL_IN_USE["available-model"] = 0

        # Eviction should succeed
        result = evict_oldest_model(max_attempts=5)
        assert result is True
        assert "available-model" not in LOADED_MODELS

        # Clean up
        LOADED_MODELS.clear()
        MODEL_IN_USE.clear()


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
