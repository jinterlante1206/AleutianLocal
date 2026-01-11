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
import time

import weaviate
import weaviate.classes as wvc
from sympy import false
from weaviate.connect import ConnectionParams
from fastapi import FastAPI, HTTPException, Request
from pydantic import BaseModel
from contextlib import asynccontextmanager

from opentelemetry import trace, metrics
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import SERVICE_NAME, Resource

from pipelines import standard, reranking, agent, verified
from datatypes.agent import AgentStepResponse, AgentStepRequest

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# --- Global Configuration ---
WEAVIATE_SERVICE_URL = os.getenv("WEAVIATE_SERVICE_URL", "http://weaviate-db:8080")
WEAVIATE_GRPC_PORT = int(os.getenv("WEAVIATE_GRPC_PORT", "50051"))
EMBEDDING_SERVICE_URL = os.getenv("EMBEDDING_SERVICE_URL", "http://ollama-embeddings:11434/api/embed")
EMBEDDING_MODEL = os.getenv("EMBEDDING_MODEL", "nomic-embed-text-v2-moe")
LLM_BACKEND_TYPE = os.getenv("LLM_BACKEND_TYPE", "ollama")
OLLAMA_BASE_URL = os.getenv("OLLAMA_BASE_URL", "http://host.containers.internal:11434")
OLLAMA_MODEL = os.getenv("OLLAMA_MODEL", "llama3") # Changed default from gpt-oss
LOCAL_LLM_URL_BASE = os.getenv("LLM_SERVICE_URL_BASE") # Base URL for llama.cpp server
OPENAI_MODEL = os.getenv("OPENAI_MODEL", "gpt-4o-mini")
OPENAI_URL_BASE = os.getenv("OPENAI_URL_BASE", "https://api.openai.com/v1")
HF_SERVER_URL = os.getenv("HF_SERVER_URL")
OTEL_URL = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "aleutian-otel-collector:4317")
SKEPTIC_MODEL = os.getenv("SKEPTIC_MODEL", OLLAMA_MODEL)

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
reader = PeriodicExportingMetricReader(OTLPMetricExporter(endpoint=OTEL_URL, insecure=True))
meter_provider = MeterProvider(resource=resource, metric_readers=[reader])
metrics.set_meter_provider(meter_provider)
meter = metrics.get_meter(__name__)

rag_request_counter = meter.create_counter(
    name="rag.requests.total",
    description="Total number of RAG requests",
    unit="1"
)

# --- Global Resources / Clients (Load on Startup) ---
weaviate_client: weaviate.WeaviateClient = None
embedding_service_url = os.getenv("EMBEDDING_SERVICE_URL", "http://embedding-server:8000/embed")
llm_backend_type = os.getenv("LLM_BACKEND_TYPE", "ollama")  # Get backend type
ollama_model = os.getenv("OLLAMA_MODEL", "llama3")  # Get ollama model if used

class SourceDocument(BaseModel):
    source: str
    distance: float | None = None

class RAGEngineResponse(BaseModel):
    answer: str
    sources: list[SourceDocument] = []

@asynccontextmanager
async def lifespan(app: FastAPI):
    global weaviate_client
    logger.info("RAG Engine Lifespan: Startup sequence initiating.")
    max_retries = 10
    retry_delay_seconds = 5
    connected = False
    for attempt in range(max_retries):
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
                connected = True
                break
            else:
                logger.error("Weaviate connection check failed.")
                raise RuntimeError("Failed to connect to Weaviate")
            logger.info("RAG Engine Lifespan: Startup complete.")
        except Exception as e:
            logger.warning(
                f"Error during Weaviate connection attempt: {e}. Retrying in {retry_delay_seconds}s...")
            if weaviate_client and weaviate_client.is_connected():
                try:
                    weaviate_client.close()
                except Exception as close_e:
                    logger.error(f"Error during close on failure: {close_e}")

        time.sleep(retry_delay_seconds)
    if not connected:
        logger.error("Failed to connect to Weaviate after all retries")
        raise RuntimeError("Failed to connect to Weaviate")

    yield
    if weaviate_client and weaviate_client.is_connected():
        try:
            weaviate_client.close()
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
    strict_mode: bool = True  # Strict RAG: only answer from docs (no LLM fallback)

pipeline_config = {
    "embedding_url": EMBEDDING_SERVICE_URL,
    "embedding_model": EMBEDDING_MODEL,
    "llm_backend_type": LLM_BACKEND_TYPE,
    "llm_service_url": llm_service_url, # Pass the determined URL
    "ollama_model": OLLAMA_MODEL,
    "openai_model": OPENAI_MODEL,
    "skeptic_model": SKEPTIC_MODEL,
    # Add other LLM model names (Claude, Gemini, HF) here if needed
    # "claude_model": CLAUDE_MODEL,
}

@app.post("/rag/standard", response_model=RAGEngineResponse)
async def run_standard_rag(request: RAGEngineRequest):
    rag_request_counter.add(1, {"pipeline": "standard"})
    logger.info(f"Running Standard RAG for query: {request.query[:50]}...")
    if not weaviate_client or not weaviate_client.is_connected():
         raise HTTPException(status_code=503, detail="Weaviate client not connected")
    try:
        pipeline = standard.StandardRAGPipeline(weaviate_client, pipeline_config)
        answer, source_docs = await pipeline.run(request.query, request.session_id, request.strict_mode)
        return RAGEngineResponse(answer=answer, sources=source_docs)
    except Exception as e:
        logger.error(f"Error in standard RAG pipeline: {e}", exc_info=True)
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/rag/reranking", response_model=RAGEngineResponse)
async def run_reranking_rag(request: RAGEngineRequest):
    rag_request_counter.add(1, {"pipeline": "reranking"})
    logger.info(f"Running Reranking RAG for query: {request.query[:50]}...")
    if not weaviate_client or not weaviate_client.is_connected():
         raise HTTPException(status_code=503, detail="Weaviate client not connected")
    try:
        pipeline = reranking.RerankingPipeline(weaviate_client, pipeline_config)
        answer, source_docs = await pipeline.run(request.query, request.session_id, request.strict_mode)
        return RAGEngineResponse(answer=answer, sources=source_docs)
    except Exception as e:
        logger.error(f"Error in reranking RAG pipeline: {e}", exc_info=True)
        raise HTTPException(status_code=500, detail=str(e))

#
# @app.post("/rag/raptor", response_model=RAGEngineResponse)
# async def run_raptor_rag(request: RAGEngineRequest):
#     rag_request_counter.add(1, {"pipeline": "raptor"})
#     logger.info(f"Running RAPTOR RAG for query: {request.query[:50]}...")
#     if not weaviate_client or not weaviate_client.is_connected():
#          raise HTTPException(status_code=503, detail="Weaviate client not connected")
#     try:
#         pipeline = raptor.RaptorPipeline(weaviate_client, pipeline_config)
#         answer = await pipeline.run(request.query)
#         return RAGEngineResponse(answer=answer)
#     except NotImplementedError:
#          logger.warning("RAPTOR pipeline execution is not implemented.")
#          raise HTTPException(status_code=501, detail="RAPTOR pipeline not implemented yet.")
#     except Exception as e:
#         logger.error(f"Error in RAPTOR RAG pipeline: {e}", exc_info=True)
#         raise HTTPException(status_code=500, detail=str(e))
#
#
# @app.post("/rag/graph", response_model=RAGEngineResponse)
# async def run_graph_rag(request: RAGEngineRequest):
#     rag_request_counter.add(1, {"pipeline": "graph"})
#     logger.info(f"Running Graph RAG for query: {request.query[:50]}...")
#     if not weaviate_client or not weaviate_client.is_connected():
#          raise HTTPException(status_code=503, detail="Weaviate client not connected")
#     try:
#         pipeline = graph.GraphRAGPipeline(weaviate_client, pipeline_config)
#         answer = await pipeline.run(request.query)
#         return RAGEngineResponse(answer=answer)
#     except NotImplementedError:
#          logger.warning("Graph RAG pipeline execution is not implemented.")
#          raise HTTPException(status_code=501, detail="Graph RAG pipeline not implemented yet.")
#     except Exception as e:
#         logger.error(f"Error in Graph RAG pipeline: {e}", exc_info=True)
#         raise HTTPException(status_code=500, detail=str(e))

# TODO: add semantic RAG

# TODO: add RIG


@app.post("/rag/verified", response_model=RAGEngineResponse)
async def run_verified_rag(request: RAGEngineRequest):
    rag_request_counter.add(1, {"pipeline": "verified"})
    logger.info(f"Running Verified RAG (Skeptic Mode) for query: {request.query[:50]}...")
    if not weaviate_client or not weaviate_client.is_connected():
        raise HTTPException(status_code=503, detail="Weaviate client not connected")
    try:
        # Initialize the Verified pipeline
        pipeline = verified.VerifiedRAGPipeline(weaviate_client, pipeline_config)

        # Run it
        answer, source_docs = await pipeline.run(request.query, request.session_id, request.strict_mode)

        return RAGEngineResponse(answer=answer, sources=source_docs)
    except Exception as e:
        logger.error(f"Error in Verified RAG pipeline: {e}", exc_info=True)
        raise HTTPException(status_code=500, detail=str(e))


class AgentRequest(BaseModel):
    query: str

@app.post("/agent/step", response_model=AgentStepResponse)
async def run_agent_step(request: AgentStepRequest):
    # No logging of full history to keep logs clean
    logger.info(f"Agent Step Request for query: {request.query}")
    if not weaviate_client or not weaviate_client.is_connected():
        raise HTTPException(status_code=503, detail="Weaviate client not connected")

    try:
        pipeline = agent.AgentPipeline(weaviate_client, pipeline_config)
        return await pipeline.run_step(request)
    except Exception as e:
        logger.error(f"Agent Step error: {e}", exc_info=True)
        raise HTTPException(status_code=500, detail=str(e))

@app.get("/health")
def health_check():
    return {"status": "ok", "weaviate_connected": weaviate_client is not None}