# Aleutian Local: Your MLOps Control Plane for LLM Apps


## License

This project is licensed under the GNU Affero General Public License v3.0 - see the [LICENSE.txt](LICENSE.txt) file for details. Note the additional terms in [NOTICE.txt](NOTICE.txt) regarding AI system attribution under AGPLv3 Section 7.

## Purpose & Identity

**Aleutian Local** simplifies building, deploying, evaluating, and managing sophisticated LLM-native applications on your **local machine** (macOS M2/M3 recommended, Linux compatible) while being easily extensible to hybrid cloud setups.

AleutianLocal's architecture is not just for local prototyping; it is fundamentally production-ready. By adhering to cloud-native principles—containerized services defined by Dockerfiles, configuration via environment variables, and a decoupled microservice structure—the entire stack is designed for a straightforward migration.

A DevOps engineer can take the `podman-compose.yml` and `podman-compose.override.yml` files (which define the services, dependencies, and configuration) and translate them directly into deployment manifests for production orchestrators like **Kubernetes**, **Docker Swarm**, or other server environments. This "local-first, production-ready" design allows you to prototype with confidence, knowing the path to a scalable deployment is clear.

It acts as an **opinionated but modular MLOps control plane**, providing the essential **infrastructure and workflow automation** around your chosen inference engine:

* **Secure Data Ingestion:** A two-phase pipeline (aleutian populate) first scans all local files with the Policy Engine, prompts for user approval on any findings, and then ingests only the approved files. The orchestrator automatically performs high-speed, content-aware chunking (e.g., using different rules for .py vs. .md files), batch embedding, and batch storage, ensuring high throughput even on a local machine.
* **Flexible RAG Engine:** Utilize multiple Retrieval-Augmented Generation strategies (Standard, Reranking available now) out-of-the-box via a simple API.
* **Unified LLM Access:** Seamlessly switch between local models (Ollama, llama.cpp, HF TGI/vLLM) and external APIs (OpenAI, Anthropic, Gemini - *coming soon*) using a consistent interface.
* **Efficient Model Management:** Convert and quantize models to GGUF format for optimized local inference.
* **Integrated Observability:** Gain immediate insights into application performance and behavior with a pre-configured stack (OpenTelemetry, Jaeger, Prometheus, Grafana).
* **Easy Extensibility:** Add custom containers (data processors, tools, models) or modify configurations using standard `podman-compose.override.yml` practices without altering core code.
* **Developer-Centric Tooling:** Manage the stack via a simple CLI (`aleutian`) or programmatically via a Python SDK (*coming soon*).

**Key Differentiator:** Aleutian empowers developers to **own their AI stack locally**. It prioritizes **data privacy, control, and observability**, offering a robust, pre-configured foundation that integrates easily with diverse data sources and LLM backends (local or cloud). Focus on your application's unique value, not infrastructure headaches.

---

## System Requirements

* **Operating System:**
    * macOS (Ventura 13.0+ recommended) - *Primary target*
    * Linux (Tested on Ubuntu 22.04 LTS; other distributions may work)
* **Processor:**
    * **Mac:** Apple Silicon (M1+) strongly recommended for Metal acceleration via Ollama.
    * **Linux/Other:** Modern multi-core CPU (Intel Core i5/i7 8th gen+, AMD Ryzen 5/7 3000 series+).
* **RAM:** **16GB minimum**, 32GB+ recommended for larger models.
* **Disk Space:** 20GB+ free space (excluding models).
* **Software Dependencies:**
    * **Podman & Podman Compose:** Required for container management.
    * **Ollama:** Recommended for default local LLM inference.
    * **Homebrew (macOS):** Recommended for easiest installation of dependencies and the `aleutian` CLI itself.
    * **Git:** Required only if building CLI from source or contributing.

---

## Installation

Choose the method that suits your OS:

### Option 1: Homebrew (macOS / Linux) - Recommended

This installs the `aleutian` CLI tool system-wide. The CLI manages the download and setup of stack components.

1.  **Install Dependencies (if missing):** Homebrew will attempt to install Podman and Podman Compose if you don't have them. You still need to install **Ollama** manually ([https://ollama.com/](https://ollama.com/)) and ensure Podman Desktop (or the Podman machine) and Ollama are running.
    ```bash
    # Optional: Install dependencies explicitly first
    brew install podman podman-compose podman-desktop ollama
    # Make sure Podman Desktop & Ollama are running!
    ```

2.  **Add the Aleutian Tap:** (Only needs to be done once)
    ```bash
    brew tap jinterlante1206/aleutian
    ```

3.  **Install Aleutian CLI:**
    ```bash
    brew install aleutian
    ```

4.  **Verify CLI Installation:**
    ```bash
    aleutian --version
    ```
    *(Proceed to "First Run" section below)*

### Option 2: Download Pre-compiled Binary (Linux / macOS / Windows)

Recommended if you don't use Homebrew.

1.  **Install Dependencies:** Manually install **Podman**, **Podman Compose**, and **Ollama**. Ensure Podman and Ollama are running.
2.  **Download:** Go to the [Latest Release page](https://github.com/jinterlante1206/AleutianLocal/releases/latest). Download the correct archive (`.tar.gz` or `.zip`) for your OS/architecture.
3.  **Extract & Install:** Extract the `aleutian` binary and move it to a location in your system's `PATH` (e.g., `/usr/local/bin` or `~/bin`). Make it executable (`chmod +x /path/to/aleutian`).
4.  **Verify CLI Installation:**
    ```bash
    aleutian --version
    ```
    *(Proceed to "First Run" section below)*

### Option 3: Build from Source (Developers / Contributors)

Use this method if you want to modify the core Aleutian code.

1.  **Install Prerequisites:** Manually install **Podman**, **Podman Compose**, **Ollama**, **Git**, and **Go** (1.21+). Ensure Podman and Ollama are running.
2.  **Clone the Repository:**
    ```bash
    git clone https://github.com/jinterlante1206/AleutianLocal.git
    cd AleutianLocal
    ```
3.  **Build the CLI:**
    ```bash
    go build -o aleutian ./cmd/aleutian
    # Optional: Move ./aleutian to your PATH
    ```
4.  **Configure Secrets (If needed):**
    * `echo "YOUR_HF_TOKEN" | podman secret create aleutian_hf_token -`
    * `echo "YOUR_OPENAI_KEY" | podman secret create openai_api_key -`
5.  **Start the Stack Directly:** Since you cloned the repo, you can use `podman-compose` manually:
    ```bash
    # Use --build the first time or after changing service code
    podman-compose up -d --build
    ```
6.  **Verify Services:**
    ```bash
    podman ps -a
    ```

---

## First Run (`aleutian stack start`)

After installing the `aleutian` CLI via **Option 1 or 2**, run the following in your terminal (from *any* directory):

```bash
aleutian stack start
```

  * **What it does (First Time):**
      * Creates a directory `~/.aleutian/stack/`.
      * Downloads and extracts the source code and configuration files for the corresponding CLI version into `~/.aleutian/stack/`.
      * Creates default `models/` and `models_cache/` directories inside `~/.aleutian/stack/`.
      * Copies the default `config/community.yaml` to `config.yaml` within that directory.
      * Runs `podman-compose up -d --build` using the files in `~/.aleutian/stack/`, building necessary images and starting all core services.
  * **What it does (Subsequent Runs):**
      * Checks the version stored in `~/.aleutian/stack/.version`.
      * If the version matches the CLI version, it runs `podman-compose up -d` (no build needed usually).
      * If the version mismatches (e.g., after `brew upgrade aleutian`), it backs up your `config.yaml`, cleans the directory, downloads/extracts the new version's files, restores your `config.yaml`, and runs `podman-compose up -d --build`.
  * **Verify Services:**
    ```bash
    podman ps -a
    ```
    Wait a few minutes for health checks. Most services should show `running (healthy)`.

Your Aleutian stack is now ready\! You can manage it using `aleutian stack stop/logs/destroy` and interact with it using `aleutian ask/chat/populate` etc. User configurations (`config.yaml`) and overrides (`podman-compose.override.yml`) should be placed inside `~/.aleutian/stack/`.

-----

## Core Commands (`aleutian ...`)

The `aleutian` CLI is your primary interface for interacting with the AleutianLocal stack. Once installed (see [Installation](#-installation) below), these commands can be run from any directory.

---

### `stack`: Manage Local Services

These commands control the lifecycle of the Aleutian containers running via Podman Compose. They manage the necessary configuration and source files within `~/.aleutian/stack/`.

* `aleutian stack start`: Ensures stack files (~/.aleutian/stack/) exist (downloads/extracts if needed, respecting version), creates necessary directories (`models`, `models_cache`), copies default config, and then starts all services using `podman-compose up -d`. Automatically incorporates `~/.aleutian/stack/podman-compose.override.yml` if present. Use `--build` if you need to force rebuilding local images (e.g., after changing service code *within* `~/.aleutian/stack/`).
* `aleutian stack stop`: Stops all running Aleutian services defined in `~/.aleutian/stack/podman-compose.yml` (runs `podman-compose down`).
* `aleutian stack destroy`: **DANGER!** Stops services *and* removes all associated container data (including Weaviate database contents) by removing volumes (`podman-compose down -v`). Also prompts to optionally remove the `~/.aleutian/stack` directory. Requires confirmation.
* `aleutian stack logs [service_name]`: Streams logs from a specific service (e.g., `aleutian-go-orchestrator`) or all services if none specified. Runs `podman-compose logs -f` within the stack directory.

---

### `ask`: Stateless Q&A (with RAG by default)

Ask a question using the configured RAG pipeline against data ingested into Weaviate.

* `aleutian ask "Your question here?"`
* **Flags:**
    * `--pipeline <name>` (`-p <name>`): Specify the RAG pipeline.
        * `reranking` (Default): Retrieves documents then uses a cross-encoder to rerank for relevance before sending to LLM.
        * `standard`: Simple vector similarity search retrieval.
        * *(Coming Soon: `raptor`, `graph`, `rig`, `semantic`)*
    * `--no-rag`: Skip the RAG pipeline entirely. Sends the question directly to the configured LLM via the orchestrator. Useful for direct LLM interaction without context retrieval.

---

### `chat`: Stateful Conversational Interface

Start an interactive chat session with the configured LLM (bypasses RAG).

* `aleutian chat`
* **Behavior:** Maintains conversation history locally within the CLI session. Sends the *entire* history to the orchestrator's `/v1/chat/direct` endpoint, which communicates directly with the configured LLM's chat capabilities. Type `exit` or `quit` to end.
* **Flags:**
    * `--resume <session_id>`: *(Currently informational)* While the backend saves turns for `ask`, the `chat` command's state is currently local to the CLI session. Future versions may leverage this to load history from Weaviate.

---

### `populate vectordb`: Ingest Documents Securely

Scan and add local files or directories to the Weaviate vector database. Handles PDF extraction automatically if the parser service is configured (see Blueprints).

* `aleutian populate vectordb <path/to/file_or_dir> [another/path...]`
* **Behavior:**
  1.  Phase 1: Scan & Approve (Serial): The CLI recursively finds all files. It then loops through them one by one to perform a fast, in-memory Policy Engine scan.
  2.  If potential secrets/PII are found, it prompts you for confirmation (yes/no). A scan_log_*.json is generated.
  3.  Phase 2: Ingest (Parallel): A list of only the approved files is fed to a parallel worker pool.
  4.  Each worker sends one approved file's content to the orchestrator's /v1/documents endpoint.
  5.  The orchestrator then performs the high-throughput ingestion:
       - Content-Aware Chunking: Splits the text using different separators for code (.py) vs. 
        documents (.md).
       - Batch Embedding: Makes one call to the embedding server's `/batch_embed` endpoint to get vectors for all chunks at once.
       - Batch Storage: Makes one call to Weaviate to import all chunks (with their vectors and parent_source metadata) in a single transaction.
  7.  Generates a `scan_log_*.json` file in the directory where the command was run.
* **Flags:**
  * `--force`: Force ingestion and skip the interactive policy prompt. Files with findings will be ingested and logged as "accepted (forced)".
---

### `convert`: Transform Models to GGUF

Download and convert Hugging Face or local models to the GGUF format for efficient inference via Ollama or llama.cpp.

* `aleutian convert <model_id_or_local_path>`
* **Behavior:** Calls the `gguf-converter` service API. Primarily useful for **text-based transformer models**. Requires the `gguf-converter` service to be running. Output files are saved within the `~/.aleutian/stack/models` directory.
* **Flags:**
    * `--quantize <type>` (`-q <type>`): Specify quantization level (e.g., `q8_0` (default), `q4_K_M`, `f16`).
    * `--is-local-path`: Treat the argument as a path *relative to* `~/.aleutian/stack/models` (e.g., `my_downloaded_model`), rather than a Hugging Face Hub ID.
    * `--register`: After conversion, automatically create a Modelfile and register the GGUF model with the locally running Ollama instance (using `<original_name>_local` as the tag).

---

### `session`: Manage Conversation History

Interact with session metadata stored in Weaviate (primarily generated by the `ask` command).

* `aleutian session list`: Show all session IDs and their LLM-generated summaries.
* `aleutian session delete <session_id> [another_id...]`: Delete a specific session and all associated conversation turns from Weaviate.

---

### `weaviate`: Administer the Vector DB

Perform administrative tasks on the Weaviate instance via the orchestrator.

* `aleutian weaviate backup <backup_id>`: Create a filesystem backup within the Weaviate container.
* `aleutian weaviate restore <backup_id>`: Restore from a previous backup ID.
* `aleutian weaviate summary`: Display the current Weaviate schema and object counts.
* `aleutian weaviate wipeout --force`: **DANGER!** Deletes *all* data and schemas from Weaviate. Requires `--force` flag and `yes` confirmation.

---

### `upload`: (Example) Send Data to Cloud Storage 

Example commands for uploading data (requires GCP configuration in `config.yaml` and service account key).

* `aleutian upload logs <local_directory>`: Uploads local log files to the configured GCS bucket/path.
* `aleutian upload backups <local_directory>`: Uploads local backup files to the configured GCS bucket/path.
* *(Note: Requires service account key available at the expected path - see `gcs/client.go` - and relevant config in `~/.aleutian/stack/config.yaml`)*.

## Architecture & Core Components

AleutianLocal operates as a cohesive stack of containerized microservices managed via Podman Compose. The `aleutian` CLI orchestrates the setup and management of these components within a dedicated directory (`~/.aleutian/stack/`).

### Default Services

The core stack, started by `aleutian stack start`, includes:

* **`orchestrator` (Go):** Central API gateway. Handles CLI requests and manages workflows. It runs the Policy Engine during the populate pre-scan, performs high-speed content-aware chunking, and orchestrates batch ingestion. It also proxies requests to the rag-engine and llm backends.
* **`rag-engine` (Python):** Executes Retrieval-Augmented Generation pipelines. Receives requests from the orchestrator, retrieves context from Weaviate using specified strategies (Standard, Reranking), constructs prompts, and calls the configured LLM for generation. The ingestion pipeline makes all chunks PDR-ready (Parent Document Retriever), allowing this engine to be easily upgraded to retrieve full-document context.
* **`embedding-server` (Python):** Provides text embedding generation via an API, using Sentence Transformers. Called by the orchestrator during data ingestion (`populate`) and by the RAG engine during querying (`ask`).
* **`gguf-converter` (Python):** Downloads Hugging Face models and converts them to the GGUF format for use with local inference engines like Ollama. Called via the `aleutian convert` command.
* **`weaviate-db`:** Weaviate vector database instance. Stores ingested documents, embeddings, and conversation session metadata.
* **`otel-collector`:** OpenTelemetry Collector. Receives trace and metric data from instrumented services (Orchestrator, RAG Engine, etc.) via OTLP.
* **`aleutian-jaeger`:** Jaeger instance. Receives trace data from the Otel Collector for visualization and debugging of request flows.
* **`aleutian-prometheus`:** Prometheus instance. Scrapes metrics from the Otel Collector (application metrics) and potentially other targets (like Ollama) for monitoring and alerting.
* **`aleutian-grafana`:** Grafana instance. Provides dashboards for visualizing metrics queried from Prometheus and allows exploration of traces stored in Jaeger.

### Policy Engine

Data ingested via the `aleutian populate vectordb` command is first scanned by a built-in Policy Engine.

* **Configuration:** Rules are defined by regular expressions in `~/.aleutian/stack/internal/policy_engine/enforcement/data_classification_patterns.yaml`. This file is downloaded automatically by the CLI.
* **Customization:** Users can edit this YAML file to add custom patterns for identifying sensitive data specific to their needs. Changes require restarting the stack (`aleutian stack stop` followed by `aleutian stack start`) for the orchestrator to reload the rules, as the file is mounted into the container. Alternatively, mount a custom file location using `podman-compose.override.yml`.

### RAG Engine Details

The Retrieval-Augmented Generation engine offers distinct strategies for sourcing context.

* **Default Pipeline:** `reranking`
    1.  The user query is converted into an embedding vector.
    2.  An initial vector search in Weaviate retrieves a set of potentially relevant document chunks (default: 20).
    3.  A Cross-Encoder model then re-scores each retrieved chunk based on its direct relevance to the original query text.
    4.  Only the highest-scoring chunks after reranking (default: 5) are included in the context sent to the Language Model.
    5.  This method prioritizes context quality, potentially increasing response latency slightly compared to simpler methods.
* **Other Available Pipelines:**
    * `standard`: Uses only the results from the initial vector search. Select via `aleutian ask --pipeline standard`. Faster execution, potentially less precise context.
* **Future Pipelines:** Implementations for Raptor, GraphRAG, RIG, and Semantic Search strategies are planned.

#### Ingestion, Chunking, and PDR-Readiness
All retrieval strategies (Standard, Reranking, etc.) depend on the quality and structure of the data in Weaviate. The aleutian populate vectordb command is built to create a high-quality, high-performance knowledge base.

-  *Content-Aware Chunking:* The orchestrator's ingestion handler (documents.go) uses different 
   langchaingo text splitters based on file type. It applies rules for Python code (splitting on class and def) that are different from Markdown (splitting on # headers) or plain text (splitting on \n\n).
- *High-Throughput Batching:* To achieve maximum speed on a local machine, the orchestrator does 
  not use a slow, "chatty" token-based splitter. Instead, it uses fast character-based splitting which allows it to process all chunks in one batch to the embedding server and one batch to Weaviate. This is an engineering trade-off that massively prioritizes ingestion speed.
- *Parent Document Retriever (PDR) Ready:* This is a key feature of the ingestion pipeline. 
  Every chunk saved to Weaviate includes a parent_source property, linking it back to the original file (e.g., test/rag_files/detroit_history.txt). This prepares your system for advanced RAG techniques like PDR, where you can search on small, precise chunks but retrieve the entire parent document for the LLM to use as context, solving the "context-cutoff" problem.

### External Model Integration

Aleutian provides unified access to various Language Model backends through configuration.

* **Configuration Method:** Set environment variables for the `orchestrator` service, typically within `~/.aleutian/stack/podman-compose.override.yml`. The primary variable is `LLM_BACKEND_TYPE`.
* **Supported Backends (via Go `LLMClient` interface):**
    * **Ollama:** Set `LLM_BACKEND_TYPE="ollama"`. Configure `OLLAMA_BASE_URL` (defaults to `http://host.containers.internal:11434` for host access) and `OLLAMA_MODEL`.
    * **OpenAI:** Set `LLM_BACKEND_TYPE="openai"`. Requires `openai_api_key` Podman secret. Configure `OPENAI_MODEL` and optionally `OPENAI_URL_BASE`.
    * **Local Llama.cpp Server:** Set `LLM_BACKEND_TYPE="local"`. Configure `LLM_SERVICE_URL_BASE` for the server endpoint.
    * **Remote/Custom:** Set `LLM_BACKEND_TYPE="remote"`. Requires a `RemoteLLMClient` implementation (Go) and configure `REMOTE_LLM_URL`.
    * **Hugging Face TGI/vLLM:** Set `LLM_BACKEND_TYPE="hf_transformers"`. Requires client implementation (Go) and configure `HF_SERVER_URL`.
    * *(Anthropic, Gemini client implementations planned)*
* **Control Flow:** Aleutian's orchestrator (or RAG engine) manages the interaction, constructing the final prompt (including RAG context if applicable) before sending the request to the configured backend via the appropriate client.

### Conversation History & Session Management

Aleutian includes mechanisms to track interactions, primarily for auditing and context.

* **`ask` Command:** When using RAG or `--no-rag`:
    * A unique `session_id` is generated for the first query in a sequence (if not provided).
    * Each question/answer pair is saved as a `Conversation` object in Weaviate, linked by the `session_id`.
    * For new sessions, the orchestrator uses the LLM to generate a concise summary, stored as a `Session` object in Weaviate.
* **`chat` Command:**
    * Manages conversation history within the active CLI session only.
    * Sends the complete turn history to the orchestrator's stateless `/v1/chat/direct` endpoint on each interaction. Does not currently save turns to Weaviate.
* **Vector DB Storage:** Storing `Conversation` objects enables potential future semantic search capabilities across past interactions.

---

## Modularity & Extensibility

AleutianLocal allows customization through standard container practices. The core stack files reside in `~/.aleutian/stack/`, managed by the `aleutian` CLI. Modifications primarily involve editing configuration files within this directory and restarting the stack.

---

### Primary Customization Methods

1.  **Override File (`~/.aleutian/stack/podman-compose.override.yml`):**
    * **Purpose:** Add new services or modify existing ones (environment variables, volumes, ports, images). This is the main method for extending the stack.
    * **Action:** Create or edit this YAML file. Podman Compose automatically merges it with the base `podman-compose.yml` found in the same directory during startup.
    * **Effect:** Changes require a stack restart (`aleutian stack stop` followed by `aleutian stack start`).

2.  **Configuration File (`~/.aleutian/stack/config.yaml`):**
    * **Purpose:** Adjust core operational parameters read by the `aleutian` CLI (e.g., default ports, target host).
    * **Action:** Edit this YAML file directly. The CLI automatically creates it from a template on first run if missing.
    * **Effect:** Changes typically require a stack restart (`aleutian stack stop` followed by `aleutian stack start`) for services to use updated values passed via environment variables during startup. The CLI itself will read the updated file on its next execution.

3.  **Backend Extensibility (Go Interfaces - Advanced):**
    * **Purpose:** Add support for entirely new *types* of backends (e.g., a new LLM provider) directly into the orchestrator.
    * **Action:** Fork the main `AleutianLocal` repository, implement the relevant Go interface (e.g., `services/llm/client.go`), modify the orchestrator's `main.go` to add the new option, build a custom orchestrator image, and use the `override.yml` file to specify using your custom image for the `orchestrator` service.
    * **Effect:** Requires Go development experience and custom image management.

### How the Orchestrator Enables Extensibility (No Code Change Required)

The Aleutian Orchestrator is pre-configured with environment variable placeholders for common integrations. You **do not** need to modify the orchestrator's Go code or routes to use these built-in extension points.

The `orchestrator` service definition in the base `podman-compose.yml` includes variables like `PDF_PARSER_URL`, `DOCX_PARSER_URL`, `CUSTOM_TOOL_1_URL`, `EVALUATION_ENGINE_URL`, etc., all defaulting to empty strings.

When you define one of these variables in your `podman-compose.override.yml`, you are "activating" a pre-built capability.

* **Example (PDF Parser):** The orchestrator's `/v1/documents` handler *already* contains Go code that checks: `if os.Getenv("PDF_PARSER_URL") != ""`.
    * If **false** (default), it skips PDF parsing.
    * If **true** (you set it in your override), it executes the code path that calls the URL you provided.

Your role as an AI engineer is to **(A)** build the custom service (like the PDF parser) and **(B)** tell the orchestrator where to find it by setting the corresponding environment variable in your `override.yml`. No changes to the orchestrator's routes or handlers are needed for these pre-defined extension points.

---

### Common Customization Scenarios: Step-by-Step

### Scenario 1: Adding a Custom Service (Minimal "Hello World" Example)

**Goal:** Add a simple Python/FastAPI service that responds with "Hello World" and integrates with the Aleutian stack.

1.  **Develop (Create Service Code):**

      * Create a directory on your machine for this service, e.g., `/Users/me/dev/hello-aleutian/`.
      * Inside that directory, create the following three files:

    **File:** `/Users/me/dev/hello-aleutian/requirements.txt`

    ```txt
    fastapi
    uvicorn[standard]
    ```

    **File:** `/Users/me/dev/hello-aleutian/server.py`

    ```python
    from fastapi import FastAPI

    app = FastAPI(title="Hello Aleutian Service")

    @app.get("/")
    def read_root():
        return {"message": "Hello from your custom Aleutian service!"}

    @app.get("/health")
    def health_check():
        # Simple health check endpoint
        return {"status": "ok"}
    ```

    **File:** `/Users/me/dev/hello-aleutian/Dockerfile`

    ```dockerfile
    FROM python:3.11-slim
    WORKDIR /app
    COPY requirements.txt .
    RUN pip install --no-cache-dir -r requirements.txt
    COPY server.py .
    # Expose the port the server runs on inside the container
    EXPOSE 8080
    # Run the Uvicorn server
    CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8080"]
    ```

2.  **Define (Edit Override File):**

      * Create or edit the file `~/.aleutian/stack/podman-compose.override.yml`.
      * Add the following service definition, making sure to use the correct **absolute path** for `context`:

    <!-- end list -->

    ```yaml
    # ~/.aleutian/stack/podman-compose.override.yml
    services:
      # Define your new "Hello World" service
      hello-aleutian:
        build:
          # --- IMPORTANT: Use the ABSOLUTE path to YOUR code ---
          context: /Users/me/dev/hello-aleutian
          dockerfile: Dockerfile
        container_name: custom-hello-service # Optional name
        networks:
          - aleutian-network # Connect to the Aleutian network
        ports:
          # Optional: Expose on host for direct testing (e.g., localhost:9001)
          - "9001:8080" # Map host port 9001 to container port 8080
        restart: unless-stopped
        healthcheck: # Add healthcheck using the endpoint created in server.py
          test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
          interval: 15s
          timeout: 5s
          retries: 5

      # --- NO Orchestrator changes needed for this simple example ---
      # If the orchestrator *needed* to call this service, you would add:
      # orchestrator:
      #   environment:
      #     HELLO_SERVICE_URL: http://hello-aleutian:8080 # Service name, container port
      #   depends_on:
      #     hello-aleutian:
      #       condition: service_healthy
    ```

3.  **Configure (If Needed):** *Not required for this simple example, as the orchestrator doesn't call it.*

> This step refers specifically to configuring **existing Aleutian services** (primarily the 
> `orchestrator`) **to interact with the new custom service you just added**.
> Think of it like plugging a new appliance into your kitchen:
> 1.  **Develop:** You build the appliance (your custom service code + Dockerfile).
> 2.  **Define:** You tell the house's electrical plan (`podman-compose.override.yml`) that the appliance exists, where its wiring (build context) is, and connect it to the main power grid (`aleutian-network`).
> 3.  **Configure (If Needed):** **This step is about telling *other* appliances or systems *how* to use the new one.**
> **Why it wasn't needed for "Hello World":**
> * The "Hello World" service just sits there waiting for direct calls (like you testing it with `curl http://localhost:9001/`).
> * No *existing* Aleutian service (like the `orchestrator` or `rag-engine`) has built-in logic that *automatically* tries to call a generic "Hello World" service.
> * Therefore, you didn't need to tell the `orchestrator` (or any other service) where the "Hello World" service was located using an environment variable in the override file.
> **When Configuration *IS* Needed (Example: PDF Parser):**
> * The `orchestrator` has specific, pre-written code in its `/v1/documents` handler designed to handle different file types during ingestion.
> * Part of that code specifically checks if an environment variable named `PDF_PARSER_URL` is set.
> * If `PDF_PARSER_URL` *is* set (e.g., to `http://my-pdf-parser:8001/extract`), the orchestrator's code knows it should *call that URL* when it receives a PDF file.
> * If `PDF_PARSER_URL` is *not* set, the orchestrator skips the parsing step for PDFs.
> * So, for the PDF parser, the **"Configure (If Needed)" step involved editing the `orchestrator`'s environment variables** in the `podman-compose.override.yml` to set `PDF_PARSER_URL`, telling the orchestrator how to find and use the parser you defined.
> **In essence:**
> * You **always "Define"** your new service in the `override.yml` so Podman knows how to build and run it.
> * You only need to **"Configure" other services** (usually the `orchestrator` via its environment variables in the `override.yml`) **if those *other* services have pre-existing logic designed to look for and call your *type* of new service** based on specific environment variable names (like `PDF_PARSER_URL`, `DOCX_PARSER_URL`, `CUSTOM_TOOL_1_URL`, etc.).
> For simple tools called directly or by *other custom services* you add, you often don't need to configure the core Aleutian orchestrator itself.

4.  **Restart:** Apply the changes and build the new service:

    ```bash
    # Ensure any previous stack is stopped
    aleutian stack stop

    # Start the stack, including the override. --build is implicit now.
    aleutian stack start
    ```

      * Podman Compose will build the image for `hello-aleutian` using your code and Dockerfile.
      * It will start the new container along with the core Aleutian services.

5.  **Verify:**

      * Check container status: `podman ps -a` (look for `custom-hello-service` or the `hello-aleutian` image running).
      * Test the endpoint directly via the host port you exposed:
        ```bash
        curl http://localhost:9001/
        # Expected Output: {"message":"Hello from your custom Aleutian service!"}

        curl http://localhost:9001/health
        # Expected Output: {"status":"ok"}
        ```
      * **Test from another container (e.g., orchestrator):**
        ```bash
        # Get a shell inside the orchestrator
        podman exec -it aleutian-go-orchestrator /bin/sh

        # Inside the orchestrator container, use the service name and container port
        # (You might need to install curl inside the container first: apk add curl)
        curl http://hello-aleutian:8080/

        # Exit the container shell
        exit
        ```

This minimal example demonstrates the core workflow: write your service code, define it in the override file with the correct build path and network, and restart the stack. Communication happens via standard HTTP calls using service names within the container network.

### Scenario 1B: Blueprint: Adding a Custom Service (Embedding Proxy Example)

**Goal:** Add a simple Python/FastAPI service (`embed-proxy`) that takes text input via its own API endpoint, calls Aleutian's core `embedding-server` to get the vector, and returns the vector.

**Pattern Demonstration:** This is a minimal example illustrating how to add *any* custom containerized service to the Aleutian stack. While this service merely proxies calls to the existing embedding server, the same pattern applies for adding services with complex logic, such as data processors, agent tools, custom model servers, or integrations with external APIs. It shows how to define the service, configure its communication with other Aleutian components (if needed), and manage it within the stack.

**Use Case:** Demonstrating service addition and inter-service communication within Aleutian.

**Difficulty:** Easy (Requires adding one custom container via override)

**Aleutian Features Used:**
* Core Stack (`orchestrator`, `embedding-server`, etc.)
* `podman-compose.override.yml` for service definition and configuration
* Inter-service communication via `aleutian-network`
* `aleutian stack start/stop` commands

---

#### Setup Steps

##### 1. Prerequisites

* A running AleutianLocal core stack (v0.1.8+ recommended) installed via the [README instructions](../README.md#getting-started).
* Your custom service code prepared locally.

##### 2. Develop (Create Service Code)

* Create a directory on your machine for this service, e.g., `/Users/me/dev/embed-proxy/`.
* Inside that directory, create the following three files:

**File:** `/Users/me/dev/embed-proxy/requirements.txt`
```txt
fastapi
uvicorn[standard]
httpx # For making async HTTP calls
```

**File:** `/Users/me/dev/embed-proxy/server.py`

```python
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
import httpx
import os
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

app = FastAPI(title="Embedding Proxy Service")

# Get the URL of the core embedding service from environment variable
# This will be configured in the override.yml
ALEUTIAN_EMBEDDING_URL = os.getenv("ALEUTIAN_EMBEDDING_URL")
if not ALEUTIAN_EMBEDDING_URL:
    # Fail fast if the required configuration is missing
    raise RuntimeError("ALEUTIAN_EMBEDDING_URL environment variable is not set!")

# Reusable HTTP client
http_client = httpx.AsyncClient()

class EmbedRequest(BaseModel):
    text: str

class EmbedResponse(BaseModel):
    text: str
    vector: list[float] | None = None
    error: str | None = None

@app.post("/embed", response_model=EmbedResponse)
async def proxy_embedding(request: EmbedRequest):
    logger.info(f"Received text for embedding: '{request.text[:50]}...'")
    if not request.text:
        return EmbedResponse(text=request.text, error="Input text cannot be empty.")

    try:
        # Call the core Aleutian embedding service
        logger.info(f"Calling core embedding service at: {ALEUTIAN_EMBEDDING_URL}")
        response = await http_client.post(ALEUTIAN_EMBEDDING_URL, json={"text": request.text}, timeout=30.0)
        response.raise_for_status() # Raise exception for 4xx/5xx errors

        data = response.json()
        if "vector" not in data or not isinstance(data["vector"], list):
             logger.error(f"Invalid response format from core embedding service: {data}")
             raise ValueError("Invalid embedding response format from core service")

        logger.info(f"Successfully received embedding vector (dimension: {len(data['vector'])})")
        return EmbedResponse(text=request.text, vector=data["vector"])

    except httpx.RequestError as e:
        logger.error(f"HTTP error calling core embedding service: {e}", exc_info=True)
        return EmbedResponse(text=request.text, error=f"Failed to connect to core embedding service: {e}")
    except Exception as e:
        logger.error(f"Error during embedding proxy: {e}", exc_info=True)
        return EmbedResponse(text=request.text, error=f"An internal error occurred: {e}")

@app.get("/health")
def health_check():
    return {"status": "ok"}

# Add shutdown event for the client (good practice)
@app.on_event("shutdown")
async def shutdown_event():
    await http_client.aclose()
```

**File:** `/Users/me/dev/embed-proxy/Dockerfile`

```dockerfile
FROM python:3.11-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY server.py .
# Expose the port the server runs on inside the container
EXPOSE 8090 # Use a different internal port, e.g., 8090
# Run the Uvicorn server
CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8090"]
```

##### 3\. Define & Configure (Edit Override File)

  * Create or edit the file `~/.aleutian/stack/podman-compose.override.yml`.

  * Add the service definition for `embed-proxy`, including the necessary environment variable to tell it how to reach the core `embedding-server`:

    ```yaml
    # ~/.aleutian/stack/podman-compose.override.yml
    services:
      # Define the new Embedding Proxy service
      embed-proxy:
        build:
          # --- IMPORTANT: Use the ABSOLUTE path to YOUR code ---
          context: /Users/me/dev/embed-proxy # <-- ADJUST THIS PATH
          dockerfile: Dockerfile
        container_name: custom-embed-proxy
        networks:
          - aleutian-network
        ports:
          # Optional: Expose on host for direct testing (e.g., localhost:9002)
          - "9002:8090" # Map host port 9002 to container port 8090
        restart: unless-stopped
        healthcheck:
          test: ["CMD", "curl", "-f", "http://localhost:8090/health"]
          interval: 15s
          timeout: 5s
          retries: 5
        # --- Configuration Step: Tell this service where the core embedder is ---
        environment:
          # Use the SERVICE NAME of the core embedder and its CONTAINER port
          ALEUTIAN_EMBEDDING_URL: http://embedding-server:8000/embed
        depends_on:
           # Make sure the core embedder is ready first
           embedding-server:
             condition: service_healthy
    ```

##### 4\. Restart Stack

  * Apply the changes and build/start the new service:
    ```bash
    aleutian stack stop
    aleutian stack start
    ```
      * Podman Compose builds the `embed-proxy` image and starts the container. The environment variable is passed in.

##### 5\. Verify

  * Check container status: `podman ps -a` (look for `custom-embed-proxy` running).
  * Test the new proxy endpoint directly via the host port:
    ```bash
    curl -X POST http://localhost:9002/embed -H "Content-Type: application/json" -d '{"text": "Hello Aleutian Proxy!"}'
    ```
    **Expected Output:** A JSON response containing the original text and a "vector" list (e.g., `{"text":"Hello Aleutian Proxy!","vector":[-0.0123, 0.0456,...],"error":null}`).
  * **Test Interaction (Simulated):** Another custom service could now call `http://embed-proxy:8090/embed` to get embeddings via your proxy.

This example shows how a custom service can be added and configured via `podman-compose.override.yml` to interact with existing core Aleutian services using internal network communication.


### **Scenario 2: Selecting a Different Local LLM Backend (e.g., Your Own TGI Server)**
    1.  **Ensure Running:** Make sure your LLM server (e.g., TGI) is running and accessible (either as another container on `aleutian-network` or on the host).
    2.  **Define (If Containerized):** If running TGI as a container, add its service definition to `~/.aleutian/stack/podman-compose.override.yml`.
    3.  **Configure:** Edit `~/.aleutian/stack/podman-compose.override.yml` to set the orchestrator's environment variables:
        ```yaml
        services:
          # Optional: Define your TGI server if running as container
          # my-tgi-server:
          #   image: ghcr.io/huggingface/text-generation-inference:latest
          #   container_name: my-tgi
          #   # ... ports, volumes for models, command, networks: [aleutian-network] ...
          orchestrator:
            environment:
              LLM_BACKEND_TYPE: "hf_transformers" # Tells orchestrator to use HF client
              HF_SERVER_URL: "http://my-tgi-server:80" # Internal URL to TGI service
              # Or if TGI runs on host: "[http://host.containers.internal:8080](http://host.containers.internal:8080)" (adjust port)
            # depends_on: # Ensure orchestrator waits if TGI is in compose
            #   my-tgi-server:
            #     condition: service_started
        ```
    4.  **Restart:** Run `aleutian stack stop && aleutian stack start`. The orchestrator will now attempt to connect to your TGI server using the `HF_SERVER_URL`. (Requires `hf_transformers` client to be implemented in Go).

### **Scenario 3: Using a Public LLM API (e.g., OpenAI)**
    1.  **Create Secret:** Ensure the API key is stored as a Podman secret: `echo "YOUR_KEY" | podman secret create openai_api_key -`
    2.  **Configure:** Edit `~/.aleutian/stack/podman-compose.override.yml`:
        ```yaml
        services:
          orchestrator:
            environment:
              LLM_BACKEND_TYPE: "openai"
              OPENAI_MODEL: "gpt-4-turbo" # Optional: Override default model
            secrets:
              # Ensure the secret is mapped to the orchestrator
              - source: openai_api_key
        ```
    3.  **Restart:** Run `aleutian stack stop && aleutian stack start`. The orchestrator initializes the `OpenAIClient`, which reads the key from the mapped secret file (`/run/secrets/openai_api_key`).

### **Scenario 4: Connecting a Custom Service to Another Data Store (e.g., InfluxDB)**
    1.  **Define Both:** Add service definitions for both your custom service (e.g., `data-collector`) and the database (`influxdb`) in `~/.aleutian/stack/podman-compose.override.yml`. Ensure both are on `aleutian-network`.
    2.  **Configure Connection:** In the `environment` section for *your custom service* (`data-collector`), provide connection details using the database's **service name**:
        ```yaml
        services:
          influxdb:
            image: influxdb:2.7
            container_name: aleutian-influxdb
            networks: [aleutian-network]
            # ... ports, volumes, environment for setup ...
          data-collector:
            build: # ... path to your collector code ...
            networks: [aleutian-network]
            environment:
              INFLUXDB_URL: http://influxdb:8086 # Internal URL using service name
              INFLUXDB_TOKEN: your_influx_token # Use Podman secrets ideally
              # ... other config ...
            depends_on: [influxdb] # Ensure DB starts first
        ```
    3.  **Restart:** Run `aleutian stack stop && aleutian stack start`. Your `data-collector` can now connect to `influxdb` using the internal URL.

### **Scenario 5: Connecting to Existing External Containers/Services**
    1.  **Option A (Shared Network):** Configure your external container to join the `aleutian-network`. Services within Aleutian can then reach it via its container name.
    2.  **Option B (Host Access):** If the external service exposes a port on your host machine (e.g., `localhost:5432`), Aleutian services *might* reach it via `host.containers.internal:<port>` (Podman Desktop on Mac/Win) or the host's bridge IP. Set the relevant URL environment variable in `override.yml` for the Aleutian service that needs to connect. This method depends heavily on the specific Podman network setup.

### **Scenario 6: Integrating Aleutian into Existing Infrastructure (e.g., Airflow, CI/CD)**
    1.  **Use API/SDK:** The primary method is via Aleutian's orchestrator API or the Python SDK (*coming soon*). External systems make HTTP requests to the orchestrator's exposed port (default `http://localhost:12210`) to trigger actions like `POST /v1/documents` (ingestion), `POST /v1/rag` (querying), etc.
    2.  **Data Flow:** Configure external pipelines to push data into Aleutian via the API/SDK.
    3.  **Observability:** Configure Aleutian's `otel-collector` (via its config file in `~/.aleutian/stack/observability/`) to export telemetry to your existing central observability backend if desired.

---

### Friction Points & Considerations

* **Restarts Required:** Applying configuration changes via `override.yml` or `config.yaml` necessitates restarting the stack (`aleutian stack stop && aleutian stack start`).
* **Networking:** Connecting services across different container networks or accessing host services requires careful configuration and understanding of Podman networking (e.g., `host.containers.internal`, bridge IPs). Refer to Podman documentation.
* **Secret Management:** Securely manage API keys and credentials, preferably using Podman secrets and mounting them to the relevant containers via `override.yml`. Avoid hardcoding secrets in configuration files.
* **LLMClient Availability:** Using a specific `LLM_BACKEND_TYPE` requires that the corresponding client implementation exists in the version of the `orchestrator` image being run. Check release notes or source code.
* **File Paths:** When defining build contexts or volume mounts in `override.yml`, use **absolute paths** on your host machine for clarity and reliability.
---

## Observability

The stack includes an integrated suite for monitoring and debugging application behavior.

* **Components:** OpenTelemetry Collector (`otel-collector`), Jaeger (`aleutian-jaeger`), Prometheus (`aleutian-prometheus`), and Grafana (`aleutian-grafana`). Core services (Orchestrator, RAG Engine) are pre-instrumented.
* **Data Flow:** Services generate trace and metric data using OpenTelemetry SDKs. This data is sent via the OTLP protocol to the Otel Collector, which then exports traces to Jaeger and metrics to Prometheus.
* **Access:**
    * **Jaeger UI (`http://localhost:16686`):** Visualize distributed traces to understand request flows across services, identify errors, and analyze latency bottlenecks.
    * **Prometheus UI (`http://localhost:9090`):** Access raw metric data, view service discovery targets, and debug metric collection.
    * **Grafana UI (`http://localhost:3000`):** Visualize metrics through dashboards (login: `admin`/`admin`). Pre-configured data sources for Prometheus and Jaeger allow querying and dashboard creation to monitor application performance, resource usage, and system health. Starter dashboards are planned.

---

## Planned Features (Roadmap Highlights)

Future development focuses on enhancing usability, integration, and core MLOps capabilities:

* **Python Client SDK:** Enable programmatic interaction with Aleutian services from Python environments (Jupyter, Airflow, etc.).
* **Aleutian Control Panel UI:** A web interface for viewing stack status, configuration, key metrics, and accessing observability tools.
* **Integrated Data Parsing:** Automatic handling of common file types (starting with PDF) within the `populate` command workflow.
* **Expanded LLM Support:** Client implementations for Anthropic Claude and Google Gemini APIs.
* **CLI Distribution:** Simplified installation via Homebrew and downloadable binaries via GoReleaser/GitHub Releases.
* **Model Evaluation Framework:** Tools for benchmarking models and RAG strategies.
* **Advanced RAG Pipelines:** Implementation of Raptor, GraphRAG, and Semantic Search strategies.
* **OpenWebUI Integration:** Streamlined setup for using OpenWebUI as a chat interface.

---
