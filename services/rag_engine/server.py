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
import logging
import weaviate
import weaviate.classes as wvc
from weaviate.connect import ConnectionParams
from fastapi import FastAPI, HTTPException, Request
from pydantic import BaseModel
from contextlib import asynccontextmanager
import services.rag_engine.pipelines.raptor
import services.rag_engine.pipelines.reranking
import services.rag_engine.pipelines.graph

from opentelemetry import trace
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import SERVICE_NAME, Resource

from pipelines import standard, reranking, raptor, graph

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# --- Global Configuration ---
WEAVIATE_SERVICE_URL = os.getenv("WEAVIATE_SERVICE_URL", "http://weaviate-db:8080")
WEAVIATE_GRPC_PORT = int(os.getenv("WEAVIATE_GRPC_PORT", "50051"))
EMBEDDING_SERVICE_URL = os.getenv("EMBEDDING_SERVICE_URL", "http://embedding-server:8000/embed")
LLM_BACKEND_TYPE = os.getenv("LLM_BACKEND_TYPE", "ollama")
OLLAMA_BASE_URL = os.getenv("OLLAMA_BASE_URL", "http://host.containers.internal:11434")
OLLAMA_MODEL = os.getenv("OLLAMA_MODEL", "llama3") # Changed default from gpt-oss
LOCAL_LLM_URL_BASE = os.getenv("LLM_SERVICE_URL_BASE") # Base URL for llama.cpp server
OPENAI_MODEL = os.getenv("OPENAI_MODEL", "gpt-4o-mini")
OPENAI_URL_BASE = os.getenv("OPENAI_URL_BASE", "https://api.openai.com/v1")
HF_SERVER_URL = os.getenv("HF_SERVER_URL")
OTEL_URL = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4317")

# Add env vars for other backends (Claude, Gemini etc.)
# CLAUDE_MODEL = os.getenv("CLAUDE_MODEL", "claude-3-haiku-20240307")

llm_service_url = ""
if LLM_BACKEND_TYPE == "ollama":
    llm_service_url = OLLAMA_BASE_URL
elif LLM_BACKEND_TYPE == "local":
    llm_service_url = LOCAL_LLM_URL_BASE
elif LLM_BACKEND_TYPE == "openai":
    llm_service_url = OPENAI_URL_BASE
elif LLM_BACKEND_TYPE == "hf_transformers":
     llm_service_url = HF_SERVER_URL
# elif LLM_BACKEND_TYPE == "claude":
#     llm_service_url = "https://api.anthropic.com/v1"

resource = Resource(attributes={SERVICE_NAME: "rag-engine"})
provider = TracerProvider(resource=resource)
processor = BatchSpanProcessor(OTLPSpanExporter(endpoint=OTEL_URL, insecure=True))
provider.add_span_processor(processor)
trace.set_tracer_provider(provider)
tracer = trace.get_tracer(__name__)

# --- Global Resources / Clients (Load on Startup) ---
weaviate_client: weaviate.WeaviateClient = None
embedding_service_url = os.getenv("EMBEDDING_SERVICE_URL", "http://embedding-server:8000/embed")
llm_backend_type = os.getenv("LLM_BACKEND_TYPE", "ollama")  # Get backend type
ollama_model = os.getenv("OLLAMA_MODEL", "llama3")  # Get ollama model if used


@asynccontextmanager
async def lifespan(app: FastAPI):
    global weaviate_client
    logger.info("RAG Engine Lifespan: Startup sequence initiating.")
    try:
        logger.info(f"Attempting to connect to Weaviate: http at {WEAVIATE_SERVICE_URL}, grpc at port {WEAVIATE_GRPC_PORT}")
        weaviate_client = weaviate.connect_to_custom(
            http_host=WEAVIATE_SERVICE_URL.replace("http://", "").split(":")[0],
            http_port=int(WEAVIATE_SERVICE_URL.split(":")[-1]),
            http_secure=False,
            grpc_host=WEAVIATE_SERVICE_URL.replace("http://", "").split(":")[0],
            grpc_port=WEAVIATE_GRPC_PORT,
            grpc_secure=False,
            # auth_client_secret=wvc.auth.AuthApiKey(api_key="YOUR-WEAVIATE-API-KEY"),
        )
        weaviate_client.connect()
        if weaviate_client.is_ready():
            logger.info("Successfully connected to Weaviate.")
            # Optional: Log schema or node status
            meta = weaviate_client.get_meta()
            logger.info(f"Weaviate Meta: {meta}")
        else:
            logger.error("Weaviate connection check failed.")
            raise RuntimeError("Failed to connect to Weaviate")
        logger.info("RAG Engine Lifespan: Startup complete.")
    except Exception as e:
        logger.error(f"Error during RAG Engine startup (Weaviate connection?): {e}", exc_info=True)
        raise e
    yield
    if weaviate_client and weaviate_client.is_connected():
        try:
            weaviate_client.disconnect()
            logger.info("Weaviate connection closed.")
        except Exception as e:
            logger.error(f"Error disconnecting from Weaviate: {e}")
    logger.info("RAG Engine Lifespan: Shutdown complete.")


app = FastAPI(title="Aleutian RAG Engine", lifespan=lifespan)

if 'provider' in locals():
    FastAPIInstrumentor.instrument_app(app)

class RAGEngineRequest(BaseModel):
    query: str
    session_id: str | None = None

class RAGEngineResponse(BaseModel):
    answer: str

pipeline_config = {
    "embedding_url": EMBEDDING_SERVICE_URL,
    "llm_backend_type": LLM_BACKEND_TYPE,
    "llm_service_url": llm_service_url, # Pass the determined URL
    "ollama_model": OLLAMA_MODEL,
    "openai_model": OPENAI_MODEL,
    # Add other LLM model names (Claude, Gemini, HF) here if needed
    # "claude_model": CLAUDE_MODEL,
}

@app.post("/rag/standard", response_model=RAGEngineResponse)
async def run_standard_rag(request: RAGEngineRequest):
    logger.info(f"Running Standard RAG for query: {request.query[:50]}...")
    if not weaviate_client or not weaviate_client.is_connected():
         raise HTTPException(status_code=503, detail="Weaviate client not connected")
    try:
        pipeline = standard.StandardRAGPipeline(weaviate_client, pipeline_config)
        answer = await pipeline.run(request.query)
        return RAGEngineResponse(answer=answer)
    except Exception as e:
        logger.error(f"Error in standard RAG pipeline: {e}", exc_info=True)
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/rag/reranking", response_model=RAGEngineResponse)
async def run_reranking_rag(request: RAGEngineRequest):
    logger.info(f"Running Reranking RAG for query: {request.query[:50]}...")
    if not weaviate_client or not weaviate_client.is_connected():
         raise HTTPException(status_code=503, detail="Weaviate client not connected")
    try:
        pipeline = reranking.RerankingPipeline(weaviate_client, pipeline_config)
        answer = await pipeline.run(request.query)
        return RAGEngineResponse(answer=answer)
    except Exception as e:
        logger.error(f"Error in reranking RAG pipeline: {e}", exc_info=True)
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/rag/raptor", response_model=RAGEngineResponse)
async def run_raptor_rag(request: RAGEngineRequest):
    logger.info(f"Running RAPTOR RAG for query: {request.query[:50]}...")
    if not weaviate_client or not weaviate_client.is_connected():
         raise HTTPException(status_code=503, detail="Weaviate client not connected")
    try:
        pipeline = raptor.RaptorPipeline(weaviate_client, pipeline_config)
        answer = await pipeline.run(request.query)
        return RAGEngineResponse(answer=answer)
    except NotImplementedError:
         logger.warning("RAPTOR pipeline execution is not implemented.")
         raise HTTPException(status_code=501, detail="RAPTOR pipeline not implemented yet.")
    except Exception as e:
        logger.error(f"Error in RAPTOR RAG pipeline: {e}", exc_info=True)
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/rag/graph", response_model=RAGEngineResponse)
async def run_graph_rag(request: RAGEngineRequest):
    logger.info(f"Running Graph RAG for query: {request.query[:50]}...")
    if not weaviate_client or not weaviate_client.is_connected():
         raise HTTPException(status_code=503, detail="Weaviate client not connected")
    try:
        pipeline = graph.GraphRAGPipeline(weaviate_client, pipeline_config)
        answer = await pipeline.run(request.query)
        return RAGEngineResponse(answer=answer)
    except NotImplementedError:
         logger.warning("Graph RAG pipeline execution is not implemented.")
         raise HTTPException(status_code=501, detail="Graph RAG pipeline not implemented yet.")
    except Exception as e:
        logger.error(f"Error in Graph RAG pipeline: {e}", exc_info=True)
        raise HTTPException(status_code=500, detail=str(e))


@app.get("/health")
def health_check():
    return {"status": "ok", "weaviate_connected": weaviate_client is not None}