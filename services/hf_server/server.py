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
import os
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from transformers import pipeline, AutoModelForCausalLM, AutoTokenizer
import torch
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


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

MODEL_NAME = os.getenv("MODEL_NAME", "google/gemma-3-4b-it")
TASK = os.getenv("HF_TASK", "test-generation")

app = FastAPI(title="HuggingFace Transformers Server")

generator = None
try:
    model = AutoModelForCausalLM.from_pretrained(MODEL_NAME).to(device)
    tokenizer = AutoTokenizer.from_pretrained(MODEL_NAME)
    generator = pipeline(TASK, model=model, tokenizer=tokenizer, device=device)
    print(f"Loaded model {MODEL_NAME} successfully.")
except Exception as e:
    print(f"Error loading model {MODEL_NAME}: {e}")

class GenerationRequest(BaseModel):
    prompt: str
    max_length: int = os.getenv("HF_MAX_LENGTH", 4096)
    temperature: float = os.getenv("HF_TEMPERATURE", 0.5)
    top_k: int = os.getenv("HF_TOP_K", 20)
    top_p: float = os.getenv("HF_TOP_P", 0.9)

class GenerationResponse(BaseModel):
    generated_text: str

@app.post("/generate", response_model=GenerationResponse)
async def generate_text(request: GenerationRequest):
    if generator is None:
        raise HTTPException(status_code=503, detail=f"Model '{MODEL_NAME}' not loaded.")
    try:
        results = generator(
            request.prompt,
            max_length=request.max_length,
            temperature=request.temperature,
            top_k=request.top_k,
            top_p = request.top_p,
            num_return_sequences=1
        )
        generated_text = results[0]['generated_text']
        if generated_text.startswith(request.prompt):
             generated_text = generated_text[len(request.prompt):]

        return GenerationResponse(generated_text=generated_text.strip())
    except Exception as e:
        print(f"Error during generation: {e}")
        raise HTTPException(status_code=500, detail=str(e))

@app.get("/health")
def health_check():
    return {"status": "ok", "model_loaded": generator is not None}