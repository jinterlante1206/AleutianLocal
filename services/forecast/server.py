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
Aleutian Standalone Forecast Service

Provides time-series forecasting via REST API.
Currently supports: Chronos T5 (tiny, mini, small, base, large)

Model Management:
- Max 3 models loaded at once (FIFO eviction)
- Use /v1/models/load to preload models
- Use /v1/models/unload to free memory
"""

import os
import logging
import threading
from collections import OrderedDict
from typing import List, Optional
from contextlib import asynccontextmanager

import numpy as np
import torch
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field, model_validator

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Maximum models to keep loaded (FIFO eviction after this)
MAX_LOADED_MODELS = int(os.getenv("MAX_LOADED_MODELS", "3"))

# Model registry - OrderedDict tracks load order for FIFO
LOADED_MODELS: OrderedDict = OrderedDict()

# Track models currently in use (ref count)
MODEL_IN_USE: dict[str, int] = {}
MODEL_LOCK = threading.Lock()

# Model compatibility matrix
# Status: "verified" = confirmed working, "untested" = not yet verified, "broken" = known issues
MODEL_COMPATIBILITY = {
    # Amazon Chronos T5 - VERIFIED
    "chronos-t5-tiny": {"status": "verified", "vram_gb": 0.5, "huggingface_id": "amazon/chronos-t5-tiny"},
    "chronos-t5-mini": {"status": "verified", "vram_gb": 1.0, "huggingface_id": "amazon/chronos-t5-mini"},
    "chronos-t5-small": {"status": "verified", "vram_gb": 2.0, "huggingface_id": "amazon/chronos-t5-small"},
    "chronos-t5-base": {"status": "verified", "vram_gb": 4.0, "huggingface_id": "amazon/chronos-t5-base"},
    "chronos-t5-large": {"status": "verified", "vram_gb": 8.0, "huggingface_id": "amazon/chronos-t5-large"},

    # Amazon Chronos Bolt - BROKEN (do not use)
    "chronos-bolt-mini": {"status": "broken", "vram_gb": 1.0, "huggingface_id": "amazon/chronos-bolt-mini"},
    "chronos-bolt-small": {"status": "broken", "vram_gb": 2.0, "huggingface_id": "amazon/chronos-bolt-small"},
    "chronos-bolt-base": {"status": "broken", "vram_gb": 4.0, "huggingface_id": "amazon/chronos-bolt-base"},

    # Google TimesFM - UNTESTED
    "timesfm-1-0": {"status": "untested", "vram_gb": 8.0, "huggingface_id": "google/timesfm-1.0-200m"},

    # Add more models here as they are tested
}


class ForecastRequest(BaseModel):
    """Request body for forecast endpoint

    Accepts data via either 'data' or 'recent_data' field for compatibility
    with different clients (CLI uses 'data', evaluator uses 'recent_data')
    """
    model: str = Field(..., description="Model slug (e.g., 'chronos-t5-tiny')")
    data: Optional[List[float]] = Field(None, description="Historical time series data")
    recent_data: Optional[List[float]] = Field(None, description="Historical time series data (alias for 'data')")
    horizon: int = Field(default=5, description="Number of steps to forecast")
    num_samples: int = Field(default=20, description="Number of sample paths for probabilistic forecast")
    # Fields from evaluator - ignored but accepted for compatibility
    name: Optional[str] = Field(None, description="Ticker name (ignored, for compatibility)")
    as_of_date: Optional[str] = Field(None, description="As-of date (ignored, for compatibility)")
    context_period_size: Optional[int] = Field(None, description="Context size (ignored, for compatibility)")
    forecast_period_size: Optional[int] = Field(None, description="Forecast size (maps to horizon)")

    @model_validator(mode='after')
    def resolve_data_field(self):
        """Resolve data from either 'data' or 'recent_data' field"""
        # Use data if provided, otherwise fall back to recent_data
        if self.data is None and self.recent_data is not None:
            self.data = self.recent_data
        # Use forecast_period_size as horizon if provided and horizon is default
        if self.forecast_period_size is not None and self.horizon == 5:
            self.horizon = self.forecast_period_size
        # Final validation - need some data
        if self.data is None:
            raise ValueError("Either 'data' or 'recent_data' must be provided")
        return self


class ForecastResponse(BaseModel):
    """Response body for forecast endpoint"""
    model: str
    forecast: List[float]
    forecast_low: Optional[List[float]] = None  # 10th percentile
    forecast_high: Optional[List[float]] = None  # 90th percentile
    horizon: int


class ModelInfo(BaseModel):
    """Model information"""
    slug: str
    status: str
    vram_gb: float
    huggingface_id: str
    loaded: bool
    in_use: bool = False


class ModelLoadRequest(BaseModel):
    """Request to load or unload a model"""
    model: str = Field(..., description="Model slug to load/unload")


def evict_oldest_model(max_attempts: int = 60) -> bool:
    """Evict the oldest model that is not in use (FIFO)

    Args:
        max_attempts: Maximum number of retry attempts if all models are in use.
                      With 0.5s sleep, 60 attempts = 30 seconds max wait.

    Returns:
        True if a model was evicted, False if eviction failed after max attempts.

    Raises:
        RuntimeError: If unable to evict any model after max_attempts.
    """
    import time
    import gc

    for attempt in range(max_attempts):
        with MODEL_LOCK:
            for model_slug in list(LOADED_MODELS.keys()):
                # Skip models currently in use
                if MODEL_IN_USE.get(model_slug, 0) > 0:
                    logger.info(f"Model {model_slug} in use, skipping eviction")
                    continue

                # Evict this model
                logger.info(f"Evicting model {model_slug} (FIFO)")
                del LOADED_MODELS[model_slug]

                # Force garbage collection and clear CUDA cache
                gc.collect()
                if torch.cuda.is_available():
                    torch.cuda.empty_cache()

                return True

        # All models are in use, wait briefly and retry
        if attempt < max_attempts - 1:
            logger.warning(f"All models in use, waiting... (attempt {attempt + 1}/{max_attempts})")
            time.sleep(0.5)

    # Failed to evict after all attempts
    raise RuntimeError(
        f"Unable to evict any model after {max_attempts} attempts. "
        f"All {len(LOADED_MODELS)} models are in use."
    )


def mark_model_in_use(model_slug: str):
    """Increment the in-use ref count for a model"""
    with MODEL_LOCK:
        MODEL_IN_USE[model_slug] = MODEL_IN_USE.get(model_slug, 0) + 1


def mark_model_done(model_slug: str):
    """Decrement the in-use ref count for a model"""
    with MODEL_LOCK:
        if model_slug in MODEL_IN_USE:
            MODEL_IN_USE[model_slug] = max(0, MODEL_IN_USE[model_slug] - 1)


def load_chronos_model(model_slug: str):
    """Load a Chronos model with FIFO eviction when at capacity"""

    # Already loaded? Move to end of OrderedDict (most recently used)
    if model_slug in LOADED_MODELS:
        LOADED_MODELS.move_to_end(model_slug)
        return LOADED_MODELS[model_slug]

    try:
        from chronos import ChronosPipeline

        model_info = MODEL_COMPATIBILITY.get(model_slug)
        if not model_info:
            raise ValueError(f"Unknown model: {model_slug}")

        # Check if we need to evict
        while len(LOADED_MODELS) >= MAX_LOADED_MODELS:
            logger.info(f"At capacity ({MAX_LOADED_MODELS} models), evicting oldest")
            evict_oldest_model()

        hf_id = model_info["huggingface_id"]
        logger.info(f"Loading Chronos model: {hf_id}")

        # Determine device
        device = "cuda" if torch.cuda.is_available() else "cpu"
        dtype = torch.bfloat16 if device == "cuda" else torch.float32

        pipeline = ChronosPipeline.from_pretrained(
            hf_id,
            device_map=device,
            torch_dtype=dtype,
        )

        LOADED_MODELS[model_slug] = pipeline
        logger.info(f"Successfully loaded {model_slug} on {device} ({len(LOADED_MODELS)}/{MAX_LOADED_MODELS} slots)")
        return pipeline

    except Exception as e:
        logger.error(f"Failed to load model {model_slug}: {e}")
        raise


def run_chronos_forecast(model_slug: str, data: List[float], horizon: int, num_samples: int) -> dict:
    """Run forecast using a Chronos model with usage tracking"""
    pipeline = load_chronos_model(model_slug)

    # Mark model as in use (prevents eviction during inference)
    mark_model_in_use(model_slug)
    try:
        # Convert to tensor - Chronos expects shape (batch, sequence_length)
        context = torch.tensor(data, dtype=torch.float32).unsqueeze(0)

        # Generate forecast (positional args for chronos-forecasting library)
        forecast = pipeline.predict(
            context,
            horizon,
            num_samples=num_samples,
        )

        # Extract median and quantiles
        # Output shape is (batch, num_samples, prediction_length), squeeze batch dim
        forecast_np = forecast[0].numpy()  # Shape: (num_samples, prediction_length)
        median = np.median(forecast_np, axis=0).tolist()
        low = np.percentile(forecast_np, 10, axis=0).tolist()
        high = np.percentile(forecast_np, 90, axis=0).tolist()

        return {
            "forecast": median,
            "forecast_low": low,
            "forecast_high": high,
        }
    finally:
        mark_model_done(model_slug)


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Startup and shutdown events"""
    # Startup
    logger.info("Aleutian Forecast Service starting...")
    logger.info(f"CUDA available: {torch.cuda.is_available()}")
    if torch.cuda.is_available():
        logger.info(f"CUDA device: {torch.cuda.get_device_name(0)}")
        logger.info(f"CUDA memory: {torch.cuda.get_device_properties(0).total_memory / 1e9:.1f} GB")

    # Optionally preload a default model
    default_model = os.getenv("DEFAULT_MODEL", "")
    if default_model and default_model.startswith("chronos"):
        try:
            load_chronos_model(default_model)
        except Exception as e:
            logger.warning(f"Failed to preload default model: {e}")

    yield

    # Shutdown
    logger.info("Aleutian Forecast Service shutting down...")
    LOADED_MODELS.clear()


app = FastAPI(
    title="Aleutian Forecast Service",
    description="Standalone time-series forecasting for Aleutian",
    version="1.0.0",
    lifespan=lifespan,
)


@app.get("/health")
async def health():
    """Health check endpoint"""
    return {
        "status": "healthy",
        "cuda_available": torch.cuda.is_available(),
        "loaded_models": list(LOADED_MODELS.keys()),
        "slots_used": f"{len(LOADED_MODELS)}/{MAX_LOADED_MODELS}",
        "models_in_use": [slug for slug, count in MODEL_IN_USE.items() if count > 0],
    }


@app.get("/v1/models")
async def list_models() -> List[ModelInfo]:
    """List all available models and their status"""
    models = []
    for slug, info in MODEL_COMPATIBILITY.items():
        models.append(ModelInfo(
            slug=slug,
            status=info["status"],
            vram_gb=info["vram_gb"],
            huggingface_id=info["huggingface_id"],
            loaded=slug in LOADED_MODELS,
            in_use=MODEL_IN_USE.get(slug, 0) > 0,
        ))
    return models


def normalize_model_slug(model_name: str) -> str:
    """Normalize model name to standard slug format

    Examples:
        'amazon/chronos-t5-tiny' -> 'chronos-t5-tiny'
        'Chronos T5 (Tiny)' -> 'chronos-t5-tiny'
        'chronos_t5_tiny' -> 'chronos-t5-tiny'
    """
    import re
    s = model_name.lower()
    # Remove org prefix (e.g., 'amazon/', 'google/')
    if '/' in s:
        s = s.rsplit('/', 1)[-1]
    # Replace spaces, underscores, parens with hyphens
    s = re.sub(r'[^a-z0-9]+', '-', s)
    s = s.strip('-')
    return s


@app.post("/v1/models/load")
async def load_model(request: ModelLoadRequest):
    """Preload a model (like 'ollama pull')

    Loads the model into memory so subsequent forecasts are faster.
    Max 3 models can be loaded at once - oldest gets evicted (FIFO).
    """
    model_slug = normalize_model_slug(request.model)

    # Check model compatibility
    model_info = MODEL_COMPATIBILITY.get(model_slug)
    if not model_info:
        raise HTTPException(
            status_code=400,
            detail=f"Unknown model: {model_slug}. Use GET /v1/models to see available models."
        )

    if model_info["status"] == "broken":
        raise HTTPException(
            status_code=400,
            detail=f"Model {model_slug} is known to be broken and cannot be used."
        )

    # Load the model
    try:
        if model_slug.startswith("chronos"):
            load_chronos_model(model_slug)
        else:
            raise HTTPException(
                status_code=501,
                detail=f"Model family for {model_slug} not yet implemented"
            )

        return {
            "status": "loaded",
            "model": model_slug,
            "loaded_models": list(LOADED_MODELS.keys()),
            "slots_used": f"{len(LOADED_MODELS)}/{MAX_LOADED_MODELS}",
        }
    except HTTPException:
        raise
    except Exception as e:
        logger.exception(f"Failed to load model {model_slug}")
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/v1/models/unload")
async def unload_model(request: ModelLoadRequest):
    """Unload a model to free memory

    If the model is currently in use (running inference), this will wait
    until the inference completes before unloading.
    """
    import time

    model_slug = normalize_model_slug(request.model)

    if model_slug not in LOADED_MODELS:
        raise HTTPException(
            status_code=404,
            detail=f"Model {model_slug} is not loaded"
        )

    # Wait for model to finish if it's in use
    max_wait = 30  # seconds
    waited = 0
    while MODEL_IN_USE.get(model_slug, 0) > 0 and waited < max_wait:
        logger.info(f"Model {model_slug} in use, waiting...")
        time.sleep(0.5)
        waited += 0.5

    if MODEL_IN_USE.get(model_slug, 0) > 0:
        raise HTTPException(
            status_code=409,
            detail=f"Model {model_slug} is still in use after {max_wait}s. Try again later."
        )

    # Remove the model
    with MODEL_LOCK:
        if model_slug in LOADED_MODELS:
            del LOADED_MODELS[model_slug]
            if model_slug in MODEL_IN_USE:
                del MODEL_IN_USE[model_slug]

    # Free memory
    import gc
    gc.collect()
    if torch.cuda.is_available():
        torch.cuda.empty_cache()

    logger.info(f"Unloaded model {model_slug}")
    return {
        "status": "unloaded",
        "model": model_slug,
        "loaded_models": list(LOADED_MODELS.keys()),
        "slots_used": f"{len(LOADED_MODELS)}/{MAX_LOADED_MODELS}",
    }


@app.post("/v1/timeseries/forecast")
async def forecast(request: ForecastRequest) -> ForecastResponse:
    """Generate time-series forecast"""
    model_slug = normalize_model_slug(request.model)

    # Check model compatibility
    model_info = MODEL_COMPATIBILITY.get(model_slug)
    if not model_info:
        raise HTTPException(
            status_code=400,
            detail=f"Unknown model: {model_slug}. Use GET /v1/models to see available models."
        )

    if model_info["status"] == "broken":
        raise HTTPException(
            status_code=400,
            detail=f"Model {model_slug} is known to be broken and cannot be used."
        )

    if model_info["status"] == "untested":
        logger.warning(f"Model {model_slug} is untested. Results may be unreliable.")

    # Validate input
    if len(request.data) < 10:
        raise HTTPException(
            status_code=400,
            detail="Need at least 10 data points for forecasting"
        )

    # Route to appropriate model family
    try:
        if model_slug.startswith("chronos"):
            result = run_chronos_forecast(
                model_slug,
                request.data,
                request.horizon,
                request.num_samples,
            )
        else:
            raise HTTPException(
                status_code=501,
                detail=f"Model family for {model_slug} not yet implemented"
            )

        return ForecastResponse(
            model=model_slug,
            forecast=result["forecast"],
            forecast_low=result.get("forecast_low"),
            forecast_high=result.get("forecast_high"),
            horizon=request.horizon,
        )

    except HTTPException:
        raise
    except Exception as e:
        logger.exception(f"Forecast failed for {model_slug}")
        raise HTTPException(status_code=500, detail=str(e))


if __name__ == "__main__":
    import uvicorn
    port = int(os.getenv("PORT", "8000"))
    uvicorn.run(app, host="0.0.0.0", port=port)
