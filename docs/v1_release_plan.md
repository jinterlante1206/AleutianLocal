### `v1_release_plan.md` (Updated)

Here is the updated project plan reflecting architectural changes and progress.

*Key Changes:* Integrated native Ollama for LLM inference, added `rag-engine`, defined `LLMClient` interface.

## R1: Local-First ML Platform

* [x] `[podman-compose.yml]` Define the core services (orchestrator, weaviate, embedding, converter, rag-engine, otel, jaeger).
* [x] `[local_setup_mac.sh]` Create the initial dependency-checking and setup script.
    * [x] Add Podman check.
    * [ ] **Add Ollama check and installation instructions.**
* [ ] `[Dockerfile.embeddings]` Update Embedding server Dockerfile to use `python:3.11-slim`.
* [x] `[server.py]` Refactor `embedding-server`'s code to *attempt* MPS detection (falls back to CPU in container).
* ~~`[llm-server]` Implement the `llm-server` using `llama.cpp`'s web server.~~ (Replaced by native Ollama integration)
* [x] `[podman-compose.yml]` Ensure services mount `./models_cache` to `/root/.cache/huggingface/` (or `/cache`).

---

## R2: Extensible Microservice Architecture

* [x] `[aleutian-cli]` Stub the Go CLI with Cobra (`cli_commands.go`).
* [x] `[orchestrator]` Stub the Go Gin server (`main.go`, `routes.go`).
* [x] `[embedding-server]` Stub the Python/FastAPI service (`server.py`).
* [x] `[podman-compose.override.yml]` Create an example override file.
* [x] `[podman-compose.yml]` Define the core `aleutian-network`.
* [x] `[rag-engine]` Create the Python/FastAPI service structure.
    * [x] Add `server.py` with FastAPI boilerplate & endpoints.
    * [x] Add `podman-compose.yml` entry for `aleutian-rag-engine`.
    * [x] Add `Dockerfile`.
    * [x] Add `requirements.txt` and install via Dockerfile.
* [x] `[web-ui]` Add `open-webui` service to `podman-compose.yml`.
* [x] `[orchestrator]` Refactor environment variables for `LLMClient` interface (LLM\_BACKEND\_TYPE, OLLAMA\_*, OPENAI\_\*, etc.).
* [x] `[orchestrator]` Add `RAG_ENGINE_URL` environment variable.
* [ ] `[documentation]` Write dev-facing docs on the override-based extension pattern.
* [x] `[templates]` Create templates for custom Python service and external API integration.

---

## R3: Core Functionality (RAG & Chat)

* [ ] `[rag-engine]` Implement RAG pipelines in the Python service.
    * [x] Create `pipelines/base.py` with shared logic (`__init__`, `_read_secret`, `_get_embedding`, `_call_llm`, `_build_prompt`).
    * [x] Implement `pipelines/standard.py` inheriting from Base.
    * [x] Implement `pipelines/reranking.py` inheriting from Base, including reranker loading.
    * [ ] Implement `pipelines/raptor.py` (Placeholder exists).
    * [ ] Implement `pipelines/graph.py` (Placeholder exists).
    * [ ] Implement Agentic RAG pipeline.
* [x] `[rag-engine]` Expose pipelines via FastAPI endpoints (`/rag/standard`, `/rag/reranking`, etc.).
* [x] `[orchestrator]` Refactor `HandleRAGRequest` in `handlers/rag.go` to be a proxy to `rag-engine`.
* [ ] `[orchestrator]` Implement an OLLAMA-compatible `/v1/chat/completions` endpoint (Proxy to configured `LLMClient.Chat` or `Generate`).
* [x] `[web-ui]` Configure `open-webui` to point to the `orchestrator`.

---

## R4: Pre-Ingestion Privacy Engine

* [x] `[policy_engine]` Implement core regex scanning engine and types.
* [x] `[data_classification_patterns.yaml]` Create default rule set.
* [x] `[cli_commands.go]` Implement `populate vectordb` command using policy engine.
* [x] `[orchestrator]` Mount `data_classification_patterns.yaml`.

---

## R5: Data & Session Management

* [x] `[main.go]` Implement `ensureWeaviateSchema` (Document, Conversation, Session).
* [x] `[handlers/documents.go]` Implement `CreateDocument`.
* [x] `[handlers/sessions.go]` Implement `ListSessions` and `DeleteSessions`.
* [x] `[datatypes/conversation.go]` Implement `Conversation.Save()`.
* [x] `[handlers/session_summary.go]` Implement `SummarizeAndSaveSession` using `LLMClient`.
* [x] `[datatypes/session.go]` Implement `Session.Save()`.
* [x] `[cli_commands.go]` Implement `session list/delete` commands.

---

## R6: Unified CLI Tool (`aleutian`)

* [x] `[cli_commands.go]` Refactor `runDeploy` -> `runStart`, `runLogsCommand` -> local implementation.
* [x] `[cli_commands.go]` Remove obsolete Ansible-related functions and commands.
* [x] `[cli_commands.go]` Implement `stack start/stop/destroy/logs` commands wrapping `podman-compose`.
* [x] `[cli_commands.go]` Implement Weaviate admin commands (`weaviate backup/restore/summary/wipeout`).
* [x] `[cli_commands.go]` Implement `convert` command with `--quantize`, `--is-local-path`.
    * [x] Add `--register` flag to `convert` command to call `ollama create`.
* [x] `[cli_commands.go]` Implement `ask` command.
    * [x] Add `--pipeline` flag to `ask` command.
    * [ ] **Implement source display in `ask` output.**
* [x] `[cli_commands.go]` Implement `chat` command (currently uses RAG proxy).
    * [ ] **(Optional) Add `--no-rag` flag and direct chat path.**

---

## R7: Optional Cloud Integration (GCP)

* [x] `[gcs/client.go]` Implement GCS client wrapper.
* [x] `[cli_commands.go]` Implement `upload logs/backups` commands.
* [ ] `[documentation]` Write docs for GCP Service Account setup.
* [ ] `[documentation]` Add section on using `gcloud auth configure-docker`.

---

## R8: Extensible Evaluation Framework

* [ ] `[evaluator]` Create `Dockerfile` for `lm-evaluation-harness`.
* [ ] `[podman-compose.yml]` Add the `evaluator` service.
* [ ] `[cli_commands.go]` Implement `eval` command (`podman exec ...`).
    * [ ] **Update target:** Needs to target Ollama API (`--model ollama ...`) or other configured backend, not a specific `llm-server` URL.
* [ ] `[documentation]` Write docs for the `backtester` pattern.
* [ ] `[cli_commands.go]` Implement sample `backtest` command.

---

## R9: Observability

* [x] `[podman-compose.yml]` Add `otel-collector` service definition.
* [x] `[podman-compose.yml]` Add `jaeger` service definition.
* [ ] `[observability/otel-collector-config.yaml]` **Create and configure the collector YAML** (receivers: otlp; exporters: jaeger, logging; pipelines).
    * [ ] **(Optional) Add Prometheus receiver for Ollama metrics.**
* [ ] `[orchestrator]` Instrument Go app (Add OTel SDK init, `otelgin` middleware).
* [x] `[rag-engine]` Instrument Python app (`FastAPIInstrumentor` added).
    * [x] Add OTel SDK init code.
    * [x] Add custom spans within pipelines (example in reranking).
* [ ] `[embedding-server]` Instrument Python app (Add OTel SDK init, `FastAPIInstrumentor`).
* [ ] `[gguf-converter]` (Optional) Instrument Python app.

---

## R10: User-Friendly Installation

* [ ] `[homebrew-tap]` Create `homebrew-aleutian` repo.
* [ ] `[aleutian.rb]` Create Homebrew formula file.
* [ ] `[.github/workflows/goreleaser.yml]` Create GoReleaser workflow.
* [ ] `[GoReleaser]` Configure cross-compilation, release assets, tap update.
* [ ] `[documentation]` Update `README.md` with `brew install` instructions.