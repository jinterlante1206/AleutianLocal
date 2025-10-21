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

app = FastAPI(
    title="GGUF Model Conversion Service",
    description="Converts a huggingface model to the GGUF format using llama.cpp to make it "
                "efficiently run on your Mac"
)

LLAMA_CPP_DIR = "/app/llama.cpp"
CONVERT_SCRIPT = os.path.join(LLAMA_CPP_DIR, "convert.py")
OUTPUT_DIR = "/models"

@app.post("/convert")
async def convert_model_to_gguf(request: ConvertRequest):
    """
    Downloads a hugging face model and converts it to gguf
    :param request:
    :return:
    """
    logger.info(f"Received a gguf conversion request for {request.model_id} to {request.quantize_type}")
    with tempfile.TemporaryDirectory() as model_dir:
        try:
            # 1. Download the Hugging Face model
            logger.info(f"Downloading {request.model_id} to {model_dir}")
            snapshot_download(
                repo_id=request.model_id,
                local_dir=model_dir,
                local_dir_use_symlinks=False,
                revision="main"
            )
            logger.info("Download complete.")

            # 2. Define output path and conversion command
            model_name = request.model_id.split("/")[-1]
            output_filename = f"{model_name}-{request.quantize_type.value}.gguf"
            output_path = os.path.join(OUTPUT_DIR, output_filename)

            # 3. Convert the model to GGUF Format
            command = [
                "python3",
                CONVERT_SCRIPT,
                model_dir,
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

        except snapshot_download as e:
            logger.error(f"Hugging Face download failed: {e}")
            raise HTTPException(status_code=400, detail=f"Failed to download model: {e}")
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

@app.get("/health")
def health_check():
    return {"status": "ok"}


