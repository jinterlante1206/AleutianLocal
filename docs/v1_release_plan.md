# Aleutian Local V1 - Updated Release Plan & Current Status

This document tracks the progress and remaining tasks for the AleutianLocal project based on the initial release plan and subsequent architectural decisions and feature additions.

*Key Changes Incorporated:* Native Ollama integration, dedicated Python `rag-engine` service, `LLMClient` interface in Go, full observability stack integration (Otel/Jaeger/Prometheus/Grafana), Python SDK planning, Control Panel UI planning, integrated data parsing planning.

**(Legend: `[x]` = Done, `[ ]` = To Do, `[~]` = Partially Done / In Progress, `[-]` = Superseded/Removed)**

---

## R1: Local-First ML Platform

* `[x]` `[podman-compose.yml]` Define the core services (orchestrator, weaviate, embedding, converter, rag-engine, otel, jaeger, prometheus, grafana).
* `[x]` `[local_setup_mac.sh]` Create the initial dependency-checking and setup script.
    * `[x]` Add Podman check.
    * `[x]` Add Ollama check and installation attempt.
* `[x]` `[Dockerfile.embeddings]` Update Embedding server Dockerfile to use `python:3.11-slim` (Assumed).
* `[x]` `[server.py]` Refactor `embedding-server`'s code to *attempt* MPS detection (falls back to CPU).
* `[-]` `[llm-server]` ~~Implement the `llm-server` using `llama.cpp`'s web server.~~ (Replaced by native Ollama integration)
* `[x]` `[podman-compose.yml]` Ensure services mount `./models_cache` to appropriate cache directories.

---

## R2: Extensible Microservice Architecture

* `[x]` `[aleutian-cli]` Implement the Go CLI with Cobra (`cli_commands.go`).
* `[x]` `[orchestrator]` Implement the Go Gin server (`main.go`, `routes.go`).
* `[x]` `[embedding-server]` Implement the Python/FastAPI service (`server.py`).
* `[x]` `[podman-compose.override.yml]` Create an example override file and document its use pattern.
* `[x]` `[podman-compose.yml]` Define the core `aleutian-network`.
* `[x]` `[rag-engine]` Create the Python/FastAPI service structure.
    * `[x]` Add `server.py` with FastAPI boilerplate & endpoints.
    * `[x]` Add `podman-compose.yml` entry for `aleutian-rag-engine`.
    * `[x]` Add `Dockerfile`.
    * `[x]` Add `requirements.txt` and install via Dockerfile.
* `[x]` `[web-ui]` Add `open-webui` service to `podman-compose.yml` (Planned Integration).
* `[x]` `[orchestrator]` Refactor environment variables for `LLMClient` interface (LLM\_BACKEND\_TYPE, OLLAMA\_*, OPENAI\_\*, etc.).
* `[x]` `[orchestrator]` Add `RAG_ENGINE_URL` environment variable.
* `[ ]` `[documentation]` **Write dev-facing docs on the override-based extension pattern.** (Crucial for demonstrating modularity).
* `[x]` `[templates]` Create templates for custom Python service and external API integration.

---

## R3: Core Functionality (RAG & Chat)

* `[ ]` `[rag-engine]` Implement RAG pipelines in the Python service.
    * `[x]` Create `pipelines/base.py` with shared logic.
    * `[x]` Implement `pipelines/standard.py`.
    * `[x]` Implement `pipelines/reranking.py`.
    * `[ ]` **Implement `pipelines/raptor.py`**.
    * `[ ]` **Implement `pipelines/graph.py`**.
    * `[ ]` **Implement `pipelines/semantic.py`**.
    * `[ ]` **(Low Priority) Implement Agentic RAG pipeline.**
* `[x]` `[rag-engine]` Expose pipelines via FastAPI endpoints (`/rag/standard`, `/rag/reranking`, etc.).
* `[x]` `[orchestrator]` Implement `HandleRAGRequest` in `handlers/rag.go` as a proxy to `rag-engine`.
* `[x]` `[orchestrator]` Implement `/v1/chat/direct` endpoint using `LLMClient.Chat`.
* `[ ]` `[orchestrator]` **Implement External API Hooks (Core LLM Flexibility):**
    * `[ ]` Add `Chat` method stubs/fallbacks to `local_llm.go`, `hf_transformers_llm.go`.
    * `[x]` Fully implement `Chat` method for `openai_llm.go`.
    * `[ ]` **Add new Go clients (`anthropic_llm.go`, `gemini_llm.go`) implementing `Generate` and `Chat`.**
    * `[x]` Update orchestrator `main.go` to handle new `LLM_BACKEND_TYPE`s.
    * `[ ]` **Document configuration/secrets for external LLM backends.**
* `[x]` `[web-ui]` Configure `open-webui` to point to the orchestrator's `/v1/chat/direct` endpoint (Planned Integration).

---

## R4: Pre-Ingestion Privacy Engine

* `[x]` `[policy_engine]` Implement core regex scanning engine and types.
* `[x]` `[data_classification_patterns.yaml]` Create default rule set.
* `[x]` `[cli_commands.go]` Implement `populate vectordb` command using policy engine.
* `[x]` `[orchestrator]` Mount `data_classification_patterns.yaml` via volume.

---

## R5: Data & Session Management

* `[x]` `[main.go]` Implement `ensureWeaviateSchema` (Document, Conversation, Session).
* `[x]` `[handlers/documents.go]` Implement `CreateDocument` (including duplicate handling).
* `[x]` `[handlers/sessions.go]` Implement `ListSessions` and `DeleteSessions`.
* `[x]` `[datatypes/conversation.go]` Implement `Conversation.Save()`.
* `[x]` `[handlers/session_summary.go]` Implement `SummarizeAndSaveSession` using `LLMClient`.
* `[x]` `[datatypes/session.go]` Implement `Session.Save()`.
* `[x]` `[cli_commands.go]` Implement `session list/delete` commands.

---

## R6: Unified CLI Tool (`aleutian`)

* `[x]` `[cli_commands.go]` Refactor old commands/remove obsolete code.
* `[x]` `[cli_commands.go]` Implement `stack start/stop/destroy/logs`.
* `[x]` `[cli_commands.go]` Implement `weaviate backup/restore/summary/wipeout`.
* `[x]` `[cli_commands.go]` Implement `convert` command with flags (`--quantize`, `--is-local-path`, `--register`).
* `[x]` `[cli_commands.go]` Implement `ask` command.
    * `[x]` Add `--pipeline` flag.
    * `[x]` Implement source display in output.
* `[x]` `[cli_commands.go]` Implement `chat` command (now uses `/v1/chat/direct`).
* `[x]` `[cli_commands.go]` Implement `--no-rag` flag for `ask` command.

---

## R7: Optional Cloud Integration (GCP)

* `[x]` `[gcs/client.go]` Implement GCS client wrapper.
* `[x]` `[cli_commands.go]` Implement `upload logs/backups` commands.
* `[ ]` `[documentation]` **Write docs for GCP Service Account setup.**
* `[ ]` `[documentation]` **Add section on using `gcloud auth configure-docker`.**

---

## R8: Extensible Evaluation Framework (Revised Plan)

* `[ ]` `[podman-compose.yml]` **Add `evaluation-engine` (Go), `InfluxDB`, and `PostgreSQL` service definitions.**
* `[ ]` `[evaluation-engine]` **Implement core service logic:**
    * `[ ]` Setup Gin server, database connections (InfluxDB, Postgres), Otel init.
    * `[ ]` Implement `/run` handler: fetch data (Influx), call LLM (via Orchestrator/direct), basic backtest logic, save results (Postgres).
    * `[ ]` Implement `/leaderboard` handler: query results (Postgres).
    * `[ ]` Instrument with OpenTelemetry spans.
* `[ ]` `[orchestrator]` **Add proxy handlers (`handlers/evaluation.go`) and routes (`/v1/evaluate/*`)**.
* `[ ]` `[orchestrator]` **Configure `EVALUATION_ENGINE_URL` env var.**
* `[ ]` `[cli_commands.go]` **Implement `evaluate start/leaderboard` commands.**
* `[ ]` `[aleutian-client]` **Add Python SDK methods for evaluation endpoints.**
* `[ ]` `[documentation]` **Write docs for setting up and using the evaluation framework.**

---

## R9: Observability

* `[x]` `[podman-compose.yml]` Add `otel-collector`, `jaeger`, `prometheus`, `grafana` service definitions.
* `[x]` `[observability/otel_collector_config.yaml]` Create and configure the collector YAML (OTLP receiver, OTLP exporter for Jaeger, Prometheus exporter, Prometheus receiver for Ollama, debug exporter, pipelines).
* `[x]` `[orchestrator]` Instrument Go app (SDK init, `otelgin`, custom spans in handlers/LLM clients).
* `[x]` `[rag-engine]` Instrument Python app (SDK init, `FastAPIInstrumentor`, custom spans).
* `[ ]` `[orchestrator/Grafana]` **Create starter Grafana dashboards** (application & resource metrics).
* `[ ]` `[observability]` **Configure resource metric collection** (e.g., via collector scraping Podman/node-exporter).
* `[ ]` `[embedding-server]` **Instrument Python app** (Add OTel SDK init, `FastAPIInstrumentor`).
* `[ ]` `[gguf-converter]` **(Optional) Instrument Python app.**

---

## R10: User-Friendly Installation & Distribution (CLI)

* `[ ]` `[homebrew-tap]` **Create `homebrew-aleutian` repo.**
* `[ ]` `[aleutian.rb]` **Finalize Homebrew formula file** (point to release, add checksum).
* `[ ]` `[.github/workflows/goreleaser.yml]` **Create GoReleaser workflow.**
* `[ ]` `[.goreleaser.yaml]` **Configure GoReleaser** (cross-compilation, release assets, tap update).
* `[ ]` `[documentation]` **Update `README.md` with `brew install`, binary download, and source build instructions.**

---

## R11: Enhanced Data Management & Ingestion (New Section)

* `[ ]` `[pdf-parser]` **Implement PDF Parser Service** (Python/FastAPI/PyMuPDF).
* `[ ]` `[orchestrator]` **Integrate PDF Parser into `populate` workflow:**
    * `[ ]` Add `PDF_PARSER_URL` env var config.
    * `[ ]` Modify `/v1/documents` handler to detect PDFs and call parser.
* `[ ]` `[documentation]` **Document adding/using the PDF parser via `override.yml`.**
* `[ ]` `[orchestrator/Postgres]` **Store basic dataset metadata** (source, timestamp, hash/UUID, *version*) during ingestion.
* `[ ]` `[orchestrator/Weaviate]` **Add optional `version` field** to Document schema.
* `[ ]` `[cli/SDK]` **Allow passing `--version` flag/parameter during `populate`.**
* `[ ]` `[documentation]` **Document basic data versioning concept.**
* `[ ]` **(Medium Priority) Implement additional parser containers** (DOCX, HTML Cleaner, etc.).
* `[ ]` **(Medium Priority) Implement basic data pipeline orchestration** within orchestrator or dedicated service for simple sequences.
* `[ ]` **(Medium Priority) Add data metadata/pipeline visualization** to Control Panel UI.

---

## R12: Enhanced User Experience (New Section)

* `[ ]` `[aleutian-client]` **Implement Python Client SDK** wrapping orchestrator APIs.
* `[ ]` `[control-panel-ui]` **Implement Aleutian Control Panel UI (Phase 1):** Read-only config/policy views, observability links, key metric display, dynamic setup visualization.
* `[ ]` `[control-panel-ui]` **Implement Aleutian Control Panel UI (Phase 2):** Configuration modification, evaluation UI integration, data metadata views.
* `[ ]` `[native-app]` **(Low Priority) Create Native Menu Bar App** wrapper for Control Panel UI.

---

## R13: Agent Support Features (New Section)

* `[ ]` `[orchestrator]` **Implement Explicit Tool Definition/Management:** Standardized way to register/discover Aleutian services (RAG, parsers) as tools via API.
* `[ ]` `[documentation]` **Document how agent frameworks (via SDK) can use Aleutian tools.**
* `[ ]` **(Low Priority) Enhance Session Management for Complex Agent State.**
* `[ ]` **(Very Low Priority) Multi-Agent Coordination support.**