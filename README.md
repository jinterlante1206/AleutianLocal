# Aleutian Local: Your MLOps Control Plane for LLM Apps

## License

This project is licensed under the GNU Affero General Public License v3.0 - see the [LICENSE.txt](LICENSE.txt) file for details. Note the additional terms in [NOTICE.txt](NOTICE.txt) regarding AI system attribution under AGPLv3 Section 7.

## Purpose & Identity

**Aleutian Local** simplifies building, deploying, evaluating, and managing sophisticated LLM-native applications on your local machine (macOS M2/M3 recommended, Linux compatible) while being extensible to hybrid cloud setups.

It acts as an **opinionated but modular MLOps control plane**, providing the essential scaffolding around your chosen inference engine:

* **Data Ingestion:** Securely populate your vector database.
* **Privacy Scanning:** Built-in secret scanning before ingestion.
* **RAG Engine:** Multi-strategy Retrieval-Augmented Generation out-of-the-box.
* **Model Management:** Convert and quantize Hugging Face or local models to GGUF for efficient local inference.
* **Vector Storage:** Integrated Weaviate vector database.
* **Session Management:** Track and manage conversation history.
* **Observability:** Full-stack tracing, metrics, and logging via OpenTelemetry, Jaeger, Prometheus, and Grafana.
* **(Coming Soon):** Model evaluation frameworks, UI integration.

**Key Differentiator:** Aleutian embraces a **developer-first, extensibility-first approach**. Use `podman-compose.override.yml` and clearly defined service interfaces (like the Go `LLMClient`) to integrate custom components (databases like InfluxDB, specialized models, unique RAG pipelines) while leveraging a robust, pre-configured core stack optimized for local development (e.g., native Ollama with MPS acceleration).

Focus on your unique application logic and data, not complex infrastructure setup.

---

## System Requirements

* **Operating System:**
    * macOS (Ventura 13.0 or later recommended) - *Primary target*
    * Linux (Tested on Ubuntu 22.04 LTS; other distributions may work)
* **Processor:**
    * **Mac:** Apple Silicon (M1, M2, M3 or later) strongly recommended for best performance (leveraging Metal acceleration via Ollama). Intel Macs may work but performance will vary significantly.
    * **Linux:** Modern multi-core CPU (e.g., Intel Core i5/i7 8th gen+, AMD Ryzen 5/7 3000 series+).
* **RAM:** **16GB minimum**, 32GB+ recommended, especially for running larger language models or multiple services concurrently.
* **Disk Space:** 20GB+ free space recommended for container images, models, and data. Model sizes can vary significantly (several GB each).
* **Software:**
    * Podman & Podman Compose (Installation covered below).
    * Ollama (Installation covered below, required for default local LLM backend).
    * Git.

---

## Getting Started

### Prerequisites

1.  **Podman:** Install Podman Desktop for your OS. This manages the containers. ([https://podman-desktop.io/](https://podman-desktop.io/))
2.  **Podman Compose:** Ensure `podman-compose` is installed (often included with Podman Desktop, or install via `pip install podman-compose`).
3.  **(Optional) Ollama:** For local inference, install Ollama. ([https://ollama.com/](https://ollama.com/)) Aleutian defaults to using Ollama running *outside* the containers.
4.  **(Optional) Go & Python:** Needed only if you plan to modify or extend the core services.
5.  **Git:** To clone the repository.

### Installation & Setup
Choose the setup method that suits your operating system:

### Option 1: macOS Setup Script (Recommended for Mac Users)

This script automates most dependency checks and initial setup steps.

1.  **Clone the Repository:**
    ```bash
    git clone [https://github.com/jinterlante1206/AleutianLocal.git](https://github.com/jinterlante1206/AleutianLocal.git) # Replace with your repo URL
    cd AleutianLocal
    ```

2.  **Configure Secrets (If needed):** Even when using the script, you should configure secrets beforehand if you plan to use features requiring them (like OpenAI or GGUF conversion with private models).
    * **Hugging Face Token:**
        ```bash
        echo "your_hf_token_here" | podman secret create aleutian_hf_token -
        ```
    * **OpenAI API Key:**
        ```bash
        echo "your_openai_key_here" | podman secret create openai_api_key -
        ```

3.  **Run the Setup Script:**
    ```bash
    ./scripts/local_setup_mac.sh
    ```
    * **What it does:**
        * Checks for Homebrew, Podman (+ Compose, Desktop), and Ollama. Attempts to install them via Homebrew if missing.
        * Prompts you to ensure Podman Desktop and Ollama are running after installation, as they might need manual starting.
        * Creates necessary local directories (like `./models`).
        * Pulls the required container images specified in `podman-compose.yml`.
        * Copies the default `config/community.yaml` to `config.yaml` if it doesn't exist.
        * Performs an initial startup of all services using `podman-compose up -d`.
    * **Note:** The script uses `podman-compose up -d`. If you modify service code (Go/Python) later, you'll need to manually run `podman-compose up -d --build` to rebuild the images.

4.  **Verify Services:**
    ```bash
    podman ps -a
    ```
    Wait a few minutes for health checks. Most services should show `running (healthy)`.

### Option 2: Manual Installation (Linux / Advanced macOS)

Follow these steps if you're not on macOS or prefer manual setup.

1.  **Install Prerequisites:**
    * Install **Podman**, **Podman Compose**, and **Podman Desktop** (if applicable) following instructions for your OS. Ensure the Podman machine/service is running.
    * Install **Ollama** following instructions for your OS. Ensure the Ollama service is running (`ollama serve` or via the app).
    * Install **Git**.

2.  **Clone the Repository:**
    ```bash
    git clone [https://github.com/jinterlante1206/AleutianLocal.git](https://github.com/jinterlante1206/AleutianLocal.git) # Replace with your repo URL
    cd AleutianLocal
    ```

3.  **Configure Secrets (If needed):**
    * **Hugging Face Token:**
        ```bash
        echo "your_hf_token_here" | podman secret create aleutian_hf_token -
        ```
    * **OpenAI API Key:**
        ```bash
        echo "your_openai_key_here" | podman secret create openai_api_key -
        ```

4.  **Start the Stack:**
    ```bash
    # Use --build the first time or whenever you change service code (Go/Python)
    podman-compose up -d --build
    ```
    * `-d`: Run in detached mode.
    * `--build`: Build local service images (`orchestrator`, `rag-engine`, etc.). This command also downloads official images if they aren't present locally.

5.  **Verify Services:**
    ```bash
    podman ps -a
    ```
    Wait a few minutes for health checks to pass.
---

## Core Commands (`./aleutian ...`)

The `aleutian` CLI (built from `cmd/aleutian/main.go`) is your primary interface. *Run `go build` in the root directory to compile it if needed.*

### `stack`: Manage Local Services

* `./aleutian stack start`: Start all services defined in `podman-compose.yml` (`podman-compose up -d`).
* `./aleutian stack stop`: Stop all running services (`podman-compose down`).
* `./aleutian stack destroy`: **DANGER!** Stops services *and* removes all associated container data (including Weaviate database contents) by removing volumes (`podman-compose down -v`). Requires confirmation.
* `./aleutian stack logs [service_name]`: Stream logs from a specific service (e.g., `aleutian-go-orchestrator`) or all services if none specified (`podman-compose logs -f [service_name]`).

### `ask`: Stateless Q&A (with RAG by default)

Ask a question using the configured RAG pipeline.

* `./aleutian ask "Your question here?"`
* **Flags:**
    * `--pipeline <name>` (`-p <name>`): Specify the RAG pipeline.
        * `reranking` (Default): Retrieves documents then uses a cross-encoder to rerank for relevance before sending to LLM.
        * `standard`: Simple vector similarity search retrieval.
        * *(Coming Soon: `raptor`, `graph`, `rig`, `semantic`)*
    * `--no-rag`: Skip the RAG pipeline entirely. Sends the question directly to the configured LLM via the orchestrator. Useful for direct LLM interaction without context retrieval.

### `chat`: Stateful Conversational Interface

Start an interactive chat session.

* `./aleutian chat`
* **Behavior:** Maintains conversation history locally within the CLI session. Sends the *entire* history to the orchestrator's `/v1/chat/direct` endpoint, which bypasses the RAG engine and communicates directly with the configured LLM's chat capabilities. Type `exit` or `quit` to end.
* **Flags:**
    * `--resume <session_id>`: *(Currently informational)* While the backend saves turns for `ask`, the `chat` command's state is currently local to the CLI session. Future versions may leverage this to load history from Weaviate.

### `populate vectordb`: Ingest Documents Securely

Scan and add local files or directories to the Weaviate vector database.

* `./aleutian populate vectordb <path/to/file_or_dir> [another/path...]`
* **Behavior:**
    1.  Recursively finds all files in the given paths.
    2.  Reads each file's content.
    3.  Scans the content using the **Policy Engine** based on patterns in `internal/policy_engine/enforcement/data_classification_patterns.yaml`.
    4.  If potential secrets/PII are found, displays them and asks for confirmation (`yes/no`) before proceeding *with that specific file*.
    5.  If confirmed (or no issues found), sends the content and source path to the orchestrator's `/v1/documents` endpoint.
    6.  The orchestrator gets embeddings and stores the document in Weaviate.
    7.  A log file (`scan_log_*.json`) is created recording all findings and user decisions.

### `convert`: Transform Models to GGUF

Download and convert Hugging Face or local models to the GGUF format for efficient CPU/GPU (incl. Apple Metal) inference via Ollama or llama.cpp.

* `./aleutian convert <model_id_or_local_path>`
* **Behavior:** Calls the `gguf-converter` service to perform the conversion. Primarily useful for **text-based transformer models**.
* **Flags:**
    * `--quantize <type>`: Specify quantization level (e.g., `q8_0` (default), `q4_K_M`, `f16`, `f32`). Lower quantization uses less RAM/disk but may reduce accuracy.
    * `--is-local-path`: Treat the `<model_id_or_local_path>` argument as a path *inside* the `models` volume mount (e.g., `/models/my_downloaded_model`), rather than a Hugging Face Hub ID.
    * `--register`: After successful conversion, automatically create a Modelfile and register the GGUF model with the locally running Ollama instance (using the original `<model_id>_local` as the Ollama model name).

### `session`: Manage Conversation History

Interact with session metadata stored in Weaviate (primarily generated by the `ask` command).

* `./aleutian session list`: Show all session IDs and their LLM-generated summaries.
* `./aleutian session delete <session_id> [another_id...]`: Delete a specific session and all associated conversation turns from Weaviate.

### `weaviate`: Administer the Vector DB

Perform administrative tasks directly on the Weaviate instance via the orchestrator.

* `./aleutian weaviate backup <backup_id>`: Create a filesystem backup within the Weaviate container.
* `./aleutian weaviate restore <backup_id>`: Restore from a previous backup ID.
* `./aleutian weaviate summary`: Display the current Weaviate schema.
* `./aleutian weaviate wipeout --force`: **DANGER!** Deletes *all* data and schemas from Weaviate. Requires `--force` flag and `yes` confirmation. Equivalent to `stack destroy` but *only* affects Weaviate data.

### `upload`: (Example) Send Data to Cloud Storage

Example commands for uploading data (requires GCP configuration).

* `./aleutian upload logs <local_directory>`: Uploads local log files to GCS.
* `./aleutian upload backups <local_directory>`: Uploads local backup files to GCS.
* *(Note: Requires service account key configured in `cmd/aleutian/gcs/client.go` and relevant config in `config.yaml`)*.

---

## Architecture & Core Components

### Default Services

When you run `podman-compose up`, the following services are started by default:

* **`orchestrator` (Go):** The central API gateway. Handles CLI requests, manages workflows, interacts with other services, and enforces policies.
* **`rag-engine` (Python):** Executes RAG pipelines. Fetches embeddings, queries Weaviate, interacts with LLMs (via Orchestrator or directly), and potentially reranks results.
* **`embedding-server` (Python):** Simple server providing text embeddings using Sentence Transformers. Used by the orchestrator during data ingestion and by the RAG engine for queries.
* **`gguf-converter` (Python):** Service dedicated to downloading and converting models to GGUF format.
* **`weaviate-db`:** The vector database storing ingested documents and conversation history.
* **`otel-collector`:** Receives OpenTelemetry data (traces, metrics) from services.
* **`aleutian-jaeger`:** Stores and visualizes trace data from the collector.
* **`aleutian-prometheus`:** Stores metrics data from the collector (and scrapes other targets like Ollama if configured).
* **`aleutian-grafana`:** Dashboard UI for visualizing metrics (from Prometheus) and exploring traces (linking to Jaeger).

### Policy Engine

The `populate vectordb` command uses a built-in Policy Engine before ingesting data.

* **Configuration:** `internal/policy_engine/enforcement/data_classification_patterns.yaml`
* **Customization:** You can **edit this YAML file** to add your own regular expressions for identifying sensitive data patterns (e.g., specific internal project codenames, custom ID formats, etc.). The engine uses these patterns to scan file content. Add new patterns under the `patterns:` list, following the existing format (name, description, regex, confidence). The orchestrator container needs to be rebuilt (`podman-compose up --build`) if you change this file within the repo, or you can mount your custom file using `podman-compose.override.yml`.

### RAG Engine Details

The RAG engine provides different strategies for retrieving context before generation.

* **Default:** `reranking`
    1.  The query is embedded.
    2.  A broad vector search retrieves an initial set of potentially relevant documents (default: 20) from Weaviate.
    3.  A more computationally intensive **Cross-Encoder model** (e.g., `cross-encoder/ms-marco-MiniLM-L-6-v2`) scores the relevance of *each* retrieved document specifically against the *original query text*.
    4.  Only the top-scoring documents after reranking (default: 5) are passed as context to the LLM.
    5.  This aims for higher quality context at the cost of slightly increased latency.
* **Other Pipelines:**
    * `standard`: Uses only the initial vector search results. Faster but potentially less relevant context. Use with `ask --pipeline standard`.
    * *(Coming Soon): Raptor, GraphRAG, RIG, Semantic Search variations.*

### External Model Integration

Aleutian acts as a control plane and can integrate with various LLM backends:

* **Configuration:** Primarily controlled by environment variables set for the `orchestrator` and `rag-engine` services in `podman-compose.yml` or `podman-compose.override.yml`.
* **Supported Backends (via Orchestrator's `LLMClient`):**
    * **Ollama:** Set `LLM_BACKEND_TYPE="ollama"`. The orchestrator talks to `OLLAMA_BASE_URL` (defaults to `http://host.containers.internal:11434` to reach Ollama running on the host Mac). Configure `OLLAMA_MODEL`.
    * **OpenAI:** Set `LLM_BACKEND_TYPE="openai"`. Requires the `openai_api_key` Podman secret to be created. Configure `OPENAI_MODEL` and optionally `OPENAI_URL_BASE`.
    * **Local Llama.cpp Server:** Set `LLM_BACKEND_TYPE="local"`. Configure `LLM_SERVICE_URL_BASE` to point to your `llama-cpp-python` server endpoint.
    * **Remote/Custom:** Set `LLM_BACKEND_TYPE="remote"` (requires adding a `RemoteLLMClient` implementation in Go) and configure `REMOTE_LLM_URL`.
    * **Hugging Face TGI/vLLM:** Set `LLM_BACKEND_TYPE="hf_transformers"` (requires implementing the Go client) and configure `HF_SERVER_URL` to point to your TGI/vLLM instance (potentially running as another container in the stack).
* **Control:** Even when using external models, Aleutian manages the interaction. The orchestrator (or RAG engine) constructs the prompt (including RAG context if used) and sends the request.

### Conversation History & Session Management

Aleutian aims to capture interaction history for context and review.

* **`ask` Command:**
    * Creates a unique `session_id` on the first turn.
    * Saves each question/answer pair to the `Conversation` collection in Weaviate, tagged with the `session_id`.
    * On the first turn, calls the LLM to generate a short summary/title for the session and saves it to the `Session` collection in Weaviate.
    * This happens regardless of whether `--no-rag` is used.
* **`chat` Command:**
    * Currently manages history **locally** within the CLI's active session.
    * Sends the full history to the `/v1/chat/direct` endpoint, which is stateless on the backend.
    * *(Future Enhancement): Could be modified to optionally save turns to the `Conversation` collection in Weaviate, similar to `ask`, potentially using the `--resume` flag.*
* **Vector DB Storage:** Storing conversation turns in Weaviate allows for semantic searching across past interactions, although this feature isn't explicitly exposed via the CLI yet.

---

## Modularity & Extensibility

Aleutian is designed to be customized:

* **`podman-compose.override.yml`:** Create this file locally (it's ignored by git). Define services or modify existing ones here to:
    * Change environment variables (e.g., point to different models, API keys, service URLs).
    * Mount local directories as volumes (e.g., mount custom configuration files, data).
    * Add entirely new services (e.g., a specific database like InfluxDB, a custom model server, a UI).
    * Override resource limits or container commands.
* **Service Interfaces:** Core interactions often happen through defined interfaces (like Go's `LLMClient`). You can implement these interfaces for custom backends (e.g., add support for Anthropic Claude by creating `anthropic_llm.go`).
* **Adding Containers:** Simply define a new service in your override file, connect it to the `aleutian-network`, and configure other services (like the orchestrator) via environment variables to talk to it.

---

## Observability

The integrated observability stack provides deep insights:

* **Jaeger UI (`http://localhost:16686`):** Explore distributed traces. See how a request flows from the CLI -> Orchestrator -> RAG Engine -> Embedding Server -> LLM and back. Identify bottlenecks and errors. * **Prometheus UI (`http://localhost:9090`):** Query raw metrics exposed by services (like request counters, durations) and the Otel Collector. Useful for debugging metric collection.
* **Grafana UI (`http://localhost:3000`):** Your main dashboarding tool (login: `admin`/`admin`).
    * Pre-configured datasources for Prometheus and Jaeger.
    * Use the "Explore" view to query Prometheus metrics (e.g., `rag_requests_total`) and visualize them.
    * Build custom dashboards to monitor service health, request rates, LLM performance, etc. * **How it Works:** Services use OpenTelemetry SDKs to generate traces and metrics. They export this data via the OTLP protocol to the `otel-collector`. The collector then processes and forwards the data to Jaeger (traces) and Prometheus (metrics). Grafana queries Prometheus and Jaeger for display.

---

## Coming Soon

* **RAG Pipelines:** Implementation of Raptor, GraphRAG, RIG, and advanced Semantic Search strategies.
* **Model Evaluation:** Framework for benchmarking models (e.g., RAG accuracy, summarization quality, etc.).
* **UI Integration:** Potential integration with Open WebUI for a chat interface.
* **Cloud Deployment Examples:** Guides for deploying parts or all of the stack to cloud providers.
* **More LLM Backends:** Explicit support for Anthropic, Gemini, etc.

---
