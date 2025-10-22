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
import datetime
import logging
import os
import uuid

import torch
import uvicorn

from contextlib import asynccontextmanager
from fastapi import FastAPI, HTTPException
from huggingface_hub import login
from pydantic import BaseModel
from transformers import AutoTokenizer, AutoModel
from typing import List

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

MODEL_NAME = os.getenv("MODEL_NAME", "google/embeddinggemma-300m")

# create the request data class
class EmbeddingRequest(BaseModel):
    text: str

# create the response data class
class EmbeddingResponse(BaseModel):
    id: str
    timestamp: int
    text: str
    vector: List[float]
    dim: int

# instantiate a few global variables
tokenizer: AutoTokenizer = None
model: AutoModel = None
device: str = "cpu"
model_ready: bool = False

@asynccontextmanager
async def lifespan(app: FastAPI):
    logger.info("Application startup: Loading the LLM configuration")
    load_llm_configuration()
    logger.info("LLM configuration loaded")
    yield
    logger.info("Application shutdown: Cleaning up resources")

# FastAPI initialization
app = FastAPI(
    title="Gemma Embedding Generation Service",
    description="A simple embeddings api service",
    version="1.0.0.",
    lifespan=lifespan
)

def load_llm_configuration():
    """ Load the LLM Model and Tokenizer into global variables """
    global model, tokenizer, device, model_ready
    try:
        try:
            with open('/run/secrets/aleutian_hf_token', 'r') as f:
                aleutian_hf_token = f.read().strip()
                if aleutian_hf_token:
                    login(token=aleutian_hf_token)
                else:
                    print("FATAL ERROR: Could not load HuggingFace Token")
        except FileNotFoundError:
            print("Huggingface Secret Token not found")
        logger.info(f"Loading LLM Model...")

        if torch.backends.mps.is_available():
            device_str = "mps"
            logger.info("MPS device found. Using Apple MetalKit")
        elif torch.cuda.is_available():
            device_str = "cuda"
            logger.info("CUDA device found. Using NVIDIA GPU")
        else:
            device_str = "cpu"
            logger.info("No GPU acceleration found; using the CPU")
        device = torch.device(device_str)
        model = AutoModel.from_pretrained(
            MODEL_NAME,
            torch_dtype="auto",
        ).to(device)
        tokenizer = AutoTokenizer.from_pretrained(MODEL_NAME)
        model_ready = True
    except Exception as e:
        print(f"ERROR: {e}")
        model_ready = False

def get_embedding(text: str) -> List[float]:
    if not model_ready or not model or not tokenizer:
        logger.error("LLM Resources are not available. Check startup logs.")
        raise HTTPException(status_code=503, detail="LLM service not ready or model not loaded")
    try:
        inputs = tokenizer(
            text,
            return_tensors="pt",
            padding=True,
            truncation=True,
            max_length=512
        ).to(device)

        with torch.no_grad():
            outputs = model(**inputs)
        # Consistent embedding extraction (mean pooling)
        embeddings = outputs.last_hidden_state.mean(dim=1)
        # Normalization (L2 norm) is common for embeddings
        normalized_embeddings = torch.nn.functional.normalize(embeddings, p=2, dim=1)
        return normalized_embeddings.cpu().numpy().tolist()[0]
    except Exception as e:
        logger.error(f"Error during embedding generation: {e}", exc_info=True)
        raise HTTPException(status_code=500, detail="Failed to generate embeddings")

@app.post("/embed", response_model=EmbeddingResponse)
async def create_embeddings(message: EmbeddingRequest):
    try:
        vector = get_embedding(message.text)
        idVal = str(uuid.uuid4())
        timestamp = int(1000*datetime.datetime.now(datetime.UTC).timestamp())
        return EmbeddingResponse(
            id=idVal,
            timestamp=timestamp,
            text=message.text,
            vector=vector,
            dim=len(vector)
        )
    except Exception as e:
        print(f"ERROR: {e}")
        raise HTTPException(status_code=500, detail="Failed to generate embeddings")

@app.get("/health", status_code=200)
async def health_check():
    if model_ready and model is not None and tokenizer is not None:
        return {"status": "ok"}
    else:
        return {"status": "initializing"}


if __name__ == "__main__":
    port_to_run = 8000
    log_level = os.getenv("LOG_LEVEL", "info")
    uvicorn.run(app, host="0.0.0.0", port=port_to_run, log_level=log_level)