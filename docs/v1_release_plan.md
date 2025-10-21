Here is the project plan, broken down by our 10 core requirements.

## R1: Local-First ML Platform

* [x] `[podman-compose.yml]` Define the core services (orchestrator, weaviate, etc.).
* [x] `[local_setup_mac.sh]` Create the initial dependency-checking and setup script.
* [ ] `[Dockerfile.ml]` Create a *new* base Dockerfile for Python ML services (`embedding-server`, `llm-server`, `rag-engine`).
    * [ ] Use a standard `python:3.11-slim` base.
    * [ ] `pip install` `torch`, `transformers`, `fastapi`, `uvicorn`.
    * [ ] Add `llama-cpp-python` with `CMAKE_ARGS="-DLLAMA_METAL=on"` set in the Dockerfile for MPS-compiled wheels.
* [ ] `[server.py]` Refactor the `embedding-server`'s `load_llm_configuration` function.
    * [ ] Implement `torch.backends.mps.is_available()` device detection.
    * [ ] Ensure all tensors (`inputs`, `model`) are explicitly moved to the detected device (`.to(device)`).
* [ ] `[llm-server]` Implement the `llm-server` using `llama-cpp-python`'s web server, which natively supports MPS.
* [ ] `[podman-compose.yml]` Ensure all ML service definitions mount `~/.cache/huggingface` to `/root/.cache/huggingface` to persist model downloads.

---

## R2: Extensible Microservice Architecture

* [x] `[aleutian-cli]` Stub the Go CLI with Cobra (`cli_commands.go`).
* [x] `[orchestrator]` Stub the Go Gin server (`main.go`, `routes.go`).
* [x] `[embedding-server]` Stub the Python/FastAPI service (`server.py`).
* [x] `[podman-compose.override.yml]` Create an example override file demonstrating how to add new services.
* [x] `[podman-compose.yml]` Define the core `aleutian-network` as a named bridge network.
* [ ] `[rag-engine]` Create the new Python/FastAPI service for modular RAG.
    * [ ] Add `main.py` with FastAPI boilerplate.
    * [ ] Add `podman-compose.yml` entry for `rag-engine`, connecting it to `aleutian-network`.
    * [ ] Add `Dockerfile` (can inherit from `Dockerfile.ml`).
    * [ ] `pip install` `langchain`, `langchain-community`, `llama-index`.
* [ ] `[web-ui]` Add `open-webui` to the `podman-compose.yml`.
* [ ] `[orchestrator]` Refactor the `orchestrator`'s environment variables to read the internal service names for the new `rag-engine`, `llm-server`, etc.
* [ ] `[documentation]` Write dev-facing docs on the override-based extension pattern.

---

## R3: Core Functionality (RAG & Chat)

* [ ] `[rag-engine]` Implement RAG pipelines in the new Python service.
    * [ ] Create `pipelines/standard.py` for baseline RAG (Retrieve -> Augment -> Generate).
    * [ ] Create `pipelines/reranking.py` (Retrieve Top-K -> Re-rank -> Augment Top-N).
    * [ ] Create `pipelines/raptor.py` (Implement RAPTOR logic using LlamaIndex/LangChain).
    * [ ] Create `pipelines/graph.py` (Implement a GraphRAG pipeline).
    * [ ] Create `pipelines/agentic.py` (Implement an Agentic RAG pipeline, e.g., ReAct).
* [ ] `[rag-engine]` Expose pipelines as FastAPI endpoints (e.g., `/rag/raptor`, `/rag/graph`).
* [ ] `[orchestrator]` Refactor the `HandleRAGRequest` in `rag.go`.
    * [ ] Gut the existing Weaviate search and prompt-building logic.
    * [ ] Add logic to parse a `"pipeline"` key from the inbound JSON request.
    * [ ] Implement a dynamic proxy/dispatcher that forwards the request to the correct `rag-engine` endpoint (e.g., `http://rag-engine:8000/rag/raptor`).
* [ ] `[orchestrator]` Implement an OLLAMA-compatible `/v1/chat/completions` endpoint.
    * [ ] This endpoint will proxy requests to the `llm-server` for non-RAG chat.
* [ ] `[web-ui]` Configure the `open-webui` service in `podman-compose.yml` to point to the `orchestrator`'s OLLAMA-compatible endpoint.

---

## R4: Pre-Ingestion Privacy Engine

* [x] `[policy_engine]` Implement the core regex scanning engine (`engine.go`).
* [x] `[policy_engine]` Define the data structures for patterns and findings (`types.go`).
* [x] `[data_classification_patterns.yaml]` Create the default YAML rule set.
* [x] `[cli_commands.go]` Implement the `populateVectorDB` command to use the `policy_engine` for local file scanning *before* POSTing to the orchestrator.
* [x] `[orchestrator]` Mount the `data_classification_patterns.yaml` into the `orchestrator` container for potential future (e.g., on-the-fly) scanning.

---

## R5: Data & Session Management

* [x] `[main.go]` Implement `ensureWeaviateSchema` to create `Document`, `Conversation`, and `Session` classes on startup.
* [x] `[handlers/documents.go]` Implement the `CreateDocument` handler (this is in your `routes.go` file, but logically belongs here).
* [x] `[handlers/sessions.go]` Implement `ListSessions` and `DeleteSessions`.
* [x] `[rag.go]` Implement `convo.Save()` to persist Q&A to the `Conversation` schema.
* [x] `[rag.go]` Implement `summarizeAndSaveSession` to create the `Session` metadata object on the first turn.
* [x] `[cli_commands.go]` Implement `runListSessions` and `runDeleteSession` to call the orchestrator's admin endpoints.

---

## R6: Unified CLI Tool (`aleutian`)

* [ ] `[cli_commands.go]` Refactor `runDeploy` (which uses Ansible) to call `runLocalStart`.
* [ ] `[cli_commands.go]` Refactor `runLogsCommand` (which uses Ansible) to use `podman-compose logs -f [service_name]`.
* [ ] `[cli_commands.go]` Refactor `scanContainers` (Ansible) to be a `podman exec` command into a container with `trivy`, or remove it.
* [ ] `[cli_commands.go]` Remove `setupServer` (Ansible-based).
* [x] `[cli_commands.go]` Implement `runLocalStart` (`podman-compose up -d`).
* [x] `[cli_commands.go]` Implement `runLocalStop` (`podman-compose down`).
* [x] `[cli_commands.go]` Implement `runLocalDestroy` (`podman-compose down -v`).
* [x] `[cli_commands.go]` Implement Weaviate admin commands (backup, restore, etc.) as HTTP clients.

---

## R7: Optional Cloud Integration (GCP)

* [x] `[gcs/client.go]` Implement the GCS client wrapper.
* [x] `[cli_commands.go]` Implement `runUploadLogs` and `runUploadBackups`.
* [ ] `[documentation]` Write docs for users on how to create a GCP Service Account, get the JSON key, and place it (e.g., in `~/.config/aleutian/gcp_key.json`) for the CLI to use.
* [ ] `[documentation]` Add a section on how to use `gcloud auth configure-docker` to pull images from Google Artifact Registry.

---

## R8: Extensible Evaluation Framework

* [ ] `[evaluator]` Create `Dockerfile` for the `evaluator` service.
    * [ ] `pip install "lm-evaluation-harness[api]"`
* [ ] `[podman-compose.yml]` Add the `evaluator` service, mounting `./evaluation_results`, and using `command: ["sleep", "infinity"]`.
* [ ] `[cli_commands.go]` Implement `runEvaluation` command.
    * [ ] The command must use `podman exec aleutian-evaluator lm_eval ...`
    * [ ] It must target the `llm-server` via its internal name: `--model_args base_url=http://llm-server:8080`.
    * [ ] It must pass `--include_path /app/custom_tasks` to support user-defined benchmarks.
* [ ] `[documentation]` Write docs for the `backtester` pattern.
    * [ ] Create an `examples/backtester` directory with a sample `Dockerfile`, `backtest.py`, and `podman-compose.override.yml` snippet.
* [ ] `[cli_commands.go]` Implement a sample `runBacktest` command in the CLI to show users how to `podman exec` their custom backtester.

---

## R9: Observability

* [ ] `[podman-compose.yml]` Add `otel-collector` service.
    * [ ] Add `otel-collector-config.yaml` and mount it, configuring OTLP receivers (gRPC, HTTP) and exporters (Jaeger).
* [ ] `[podman-compose.yml]` Add `jaeger` service (all-in-one image) and expose port `16686`.
    * [ ] Configure Jaeger to receive data from the `otel-collector`.
* [ ] `[orchestrator]` Instrument the Go/Gin app.
    * [ ] Add OTel SDK initialization code (`initTracer`).
    * [ ] Add `otelgin.Middleware("orchestrator-service")` to the Gin router.
* [ ] `[rag-engine]` Instrument the Python/FastAPI app.
    * [ ] `pip install opentelemetry-instrumentation-fastapi`.
    * [ ] Add `FastAPIInstrumentor.instrument_app(app)` to `main.py`.
    * [ ] Configure OTLP exporter via environment variables.
* [ ] `[embedding-server]` Instrument the Python/FastAPI app (same as `rag-engine`).
* [ ] `[llm-server]` Instrument the Python/FastAPI app (same as `rag-engine`).

---

## R10: User-Friendly Installation

* [ ] `[homebrew-tap]` Create a new public GitHub repo named `homebrew-aleutian`.
* [ ] `[aleutian.rb]` Create the Homebrew formula file in the new tap repo.
* [ ] `[.github/workflows]` Create a `goreleaser.yml` in the *main* `aleutian` repo.
* [ ] `[GoReleaser]` Configure `GoReleaser` to:
    * [ ] Cross-compile binaries for `darwin/amd64` and `darwin/arm64`.
    * [ ] Create a GitHub Release and upload binaries as assets.
    * [ ] Use a PAT (Personal Access Token) to automatically push an update to the `aleutian.rb` file in the `homebrew-aleutian` repo with the new binary `sha256` hashes.
* [ ] `[documentation]` Update `README.md` with the new one-line install: `brew install aleutian/aleutian/aleutian`.