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
import subprocess
import tempfile
import logging
from enum import Enum
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from huggingface_hub import snapshot_download

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class QuantizeType(str, Enum):
    F32 = "f32"
    BF16 = "bf16"
    F16 = "f16"
    INT8 = "q8_0"

class ConvertRequest(BaseModel):
    model_id: str
    quantize_type: QuantizeType
    is_local_path: bool=False

app = FastAPI(
    title="GGUF Model Conversion Service",
    description="Converts a huggingface model to the GGUF format using llama.cpp to make it "
                "efficiently run on your Mac"
)

LLAMA_CPP_DIR = "/app/llama.cpp"
CONVERT_SCRIPT = os.path.join(LLAMA_CPP_DIR, "convert_hf_to_gguf.py")
OUTPUT_DIR = "/models"

def get_hf_token():
    secret_path = "/run/secrets/huggingface_token"
    try:
        with open(secret_path, 'r') as f:
            return f.read().strip()
    except FileNotFoundError:
        logger.warning(f"Hugging Face token secret not found at {secret_path}. Downloads may fail for private models.")
        return None
    except Exception as e:
        logger.error(f"Error reading Hugging Face token secret: {e}")
        return None

HUGGINGFACE_TOKEN = get_hf_token()

@app.post("/convert")
async def convert_model_to_gguf(request: ConvertRequest):
    """
    Downloads a hugging face model and converts it to gguf
    :param request:
    :return:
    """
    logger.info(f"Received a gguf conversion request for {request.model_id} to {request.quantize_type}")
    model_input_dir = ""
    if request.is_local_path:
        logger.info(f"Using the local model path for the gguf conversion: {request.model_id}")
        model_input_dir = request.model_id
        if not os.path.isdir(model_input_dir):
            logger.error(f"Local path not found inside container: {model_input_dir}")
            raise HTTPException(status_code=400, detail=f"Local path not found {model_input_dir}")
    else:
        temp_dir = tempfile.TemporaryDirectory()
        model_input_dir = temp_dir.name
        try:
            logger.info(f"Downloading {request.model_id} to {model_input_dir}")
            snapshot_download(
                repo_id=request.model_id,
                local_dir=model_input_dir,
                local_dir_use_symlinks=False,
                revision="main",
                cache_dir="/root/.cache/huggingface/",
                token=HUGGINGFACE_TOKEN
            )
            logger.info("Download complete.")
        except Exception as e:
            # ... (your existing exception handling) ...
            temp_dir.cleanup()  # Clean up the temp dir on failure
            raise HTTPException(status_code=400, detail=f"Failed to download model: {e}")
    try:
        # Define output path and conversion command
        model_name = request.model_id.split("/")[-1]
        output_filename = f"{model_name}-{request.quantize_type.value}.gguf"
        output_path = os.path.join(OUTPUT_DIR, output_filename)

        # Convert the model to GGUF Format
        command = [
            "python3",
            CONVERT_SCRIPT,
            model_input_dir,
            "--outfile", output_path,
            "--outtype", request.quantize_type.value
        ]
        logger.info(f"Running the gguf conversion command: {' '.join(command)}")
        # Use subprocess.run to execute the command
        result = subprocess.run(
            command,
            capture_output=True,
            text=True,
            check=True
        )

        logger.info("Conversion successful.")
        logger.info(f"STDOUT: {result.stdout}")

        return {
            "status": "success",
            "message": f"Model {request.model_id} converted successfully.",
            "output_path": output_path,
            "logs": result.stdout
        }

    except subprocess.CalledProcessError as e:
        logger.error(f"Conversion script failed with exit code {e.returncode}")
        logger.error(f"STDERR: {e.stderr}")
        raise HTTPException(
            status_code=500,
            detail=f"Conversion script failed: {e.stderr}"
        )
    except Exception as e:
        logger.error(f"An unexpected error occurred: {e}")
        raise HTTPException(status_code=500, detail=f"An unexpected error occurred: {e}")
    finally:
        if not request.is_local_path and 'temp_dir' in locals():
            temp_dir.cleanup()

@app.get("/health")
def health_check():
    return {"status": "ok"}


