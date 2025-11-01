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
from torch import Tensor
from transformers import AutoTokenizer, AutoModel
from typing import List

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

MODEL_NAME = os.getenv("MODEL_NAME", "google/embeddinggemma-300m")

# create the request data class
class BatchEmbeddingRequest(BaseModel):
    texts: List[str]

class EmbeddingRequest(BaseModel):
    text: str

# create the response data class
class BatchEmbeddingResponse(BaseModel):
    id: str
    timestamp: int
    model: str
    vectors: List[List[float]]
    dim: int

class TokenizeRequest(BaseModel):
    text: str

class TokenizeResponse(BaseModel):
    model: str
    token_count: str


# instantiate a few global variables
tokenizer: AutoTokenizer = None
model: AutoModel = None
device: str = "cpu"
model_ready: bool = False

def last_token_pool(last_hidden_states: Tensor,
                    attention_mask: Tensor) -> Tensor:
    left_padding = (attention_mask[:, -1].sum() == attention_mask.shape[0])
    if left_padding:
        return last_hidden_states[:, -1]
    else:
        sequence_lengths = attention_mask.sum(dim=1) - 1
        batch_size = last_hidden_states.shape[0]
        return last_hidden_states[torch.arange(batch_size, device=last_hidden_states.device), sequence_lengths]

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
        logger.info(f"Loading LLM Model: " +  MODEL_NAME)

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
    logger.info("Starting up to get the embeddings")
    try:
        inputs = tokenizer(
            text,
            return_tensors="pt",
            padding=True,
            truncation=True,
            max_length=512
        ).to(device)
        logger.info("setup the model(**inputs)")

        with torch.no_grad():
            logger.info("sending the inputs to the model")
            outputs = model(**inputs)
            logger.info("outputs set")


        logger.info("determining which embedings to use for:" + MODEL_NAME)
        # Consistent embedding extraction (mean pooling)
        if MODEL_NAME == "google/embeddinggemma-300m":
            logger.info("processing for google/embeddinggemma-300m")
            embeddings = outputs.last_hidden_state.mean(dim=1)
        elif MODEL_NAME.split("/")[0].lower() == "qwen":
            logger.info("processing for Qwen")
            embeddings = last_token_pool(outputs.last_hidden_state, inputs['attention_mask'])
        else:
            logger.error(f"Failed to process for: {MODEL_NAME}. Define the embeddings processor")

        # Normalization (L2 norm) is common for embeddings
        normalized_embeddings = torch.nn.functional.normalize(embeddings, p=2, dim=1)
        return normalized_embeddings.cpu().numpy().tolist()[0]
    except Exception as getEmbeddingsError:
        logger.error(f"Error during embedding generation: {getEmbeddingsError}", exc_info=True)
        raise HTTPException(status_code=500, detail="Failed to generate embeddings")


@app.post("/embed", response_model=BatchEmbeddingResponse)
async def create_embeddings(message: BatchEmbeddingRequest):
    try:
        if not message.texts or len(message.texts) == 0:
            logger.error("Embed called with no texts in 'texts' list")
            raise HTTPException(status_code=400, detail="No text provided in 'texts' list")

        vector = get_embedding(message.texts[0])

        idVal = str(uuid.uuid4())
        timestamp = int(1000*datetime.datetime.now(datetime.UTC).timestamp())
        return BatchEmbeddingResponse(
            id=idVal,
            timestamp=timestamp,
            model=MODEL_NAME,  # Also add model name
            vectors=[vector],  # Return as a list of one
            dim=len(vector)
        )
    except Exception as e:
        print(f"ERROR: {e}")
        raise HTTPException(status_code=500, detail="Failed to generate embeddings")

@app.post("/tokenize", response_model=TokenizeResponse)
async def tokenize_text(message: TokenizeRequest):
    global tokenizer, model_ready, MODEL_NAME
    if not model_ready or not tokenizer:
        logger.error("Tokenizer is not available. Check the logs")
        raise HTTPException(status_code=503, detail="Tokenizer service is not ready")

    try:
        inputs = tokenizer(message.text, return_tensors="pt")
        token_count = len(inputs['input_ids'][0])
        return TokenizeResponse(
            model=MODEL_NAME,
            token_count=token_count
        )
    except Exception as e:
        logger.error(f"Error during the tokenization process {e}", exc_info=True)
        raise HTTPException(status_code=500, detail="Failed to count tokens")


@app.post("/batch_embed", response_model=BatchEmbeddingResponse)
async def batch_embed_text(request: BatchEmbeddingRequest):
    global tokenizer, model, model_ready, MODEL_NAME
    if not model_ready:
        logger.error("Batch embed called but the model isn't ready")
        raise HTTPException(status_code=503, detail="Model service not ready")
    if not request.texts:
        logger.info("Batch embed called with no texts.")
        return BatchEmbeddingResponse(vectors=[], model=MODEL_NAME, dim=0)
    logger.info(f"Received batch embed request with {len(request.texts)} documents.")
    try:
        inputs = tokenizer(
            request.texts,
            padding=True,
            truncation=True,
            return_tensors="pt",
            max_length=512  # Or your model's max sequence length
        )

        inputs = {k: v.to(device) for k, v in inputs.items()}

        with torch.no_grad():
            outputs = model(**inputs)

            logger.info("determining which embedings to use for:" + MODEL_NAME)
            if MODEL_NAME == "google/embeddinggemma-300m":
                logger.info("processing for google/embeddinggemma-300m")
                embeddings = outputs.last_hidden_state.mean(dim=1)
            elif MODEL_NAME.split("/")[0].lower() == "qwen":
                logger.info("processing for Qwen")
                embeddings = last_token_pool(outputs.last_hidden_state, inputs['attention_mask'])
            else:
                logger.error(
                    f"Failed to process for: {MODEL_NAME}. Define the embeddings processor")
                raise HTTPException(status_code=500,
                                    detail=f"Embedding processor not defined for {MODEL_NAME}")

        # Normalize embeddings
        embeddings = torch.nn.functional.normalize(embeddings, p=2, dim=1)

        # Move back to CPU and convert to a standard list
        vectors = embeddings.cpu().tolist()
        dim = len(vectors[0]) if vectors else 0
        idVal = str(uuid.uuid4())
        timestamp = int(1000 * datetime.datetime.now(datetime.UTC).timestamp())

        logger.info(f"Successfully generated {len(vectors)} vectors with dim {dim}.")
        return BatchEmbeddingResponse(
            id=idVal, timestamp=timestamp, vectors=vectors, model=MODEL_NAME, dim=dim)

    except Exception as e:
        logger.error(f"Error during batch embedding: {e}", exc_info=True)
        raise HTTPException(status_code=500, detail=f"Failed to process batch: {e}")


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