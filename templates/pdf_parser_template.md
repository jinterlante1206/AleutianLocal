# Blueprint: Local PDF Document Q&A (Integrated Parsing)

**Goal:** Ingest local PDF documents (e.g., company policies, research papers) into AleutianLocal *seamlessly*, ensuring text is extracted automatically, and then ask questions about the content using a locally running LLM via RAG.

**Use Case:** Creating a private, offline knowledge base from existing PDF files with minimal setup.

**Difficulty:** Easy (Requires adding one container via override, but usage is simple)

**Aleutian Features Used:**
* Core Stack (`orchestrator`, `embedding-server`, `rag-engine`, `weaviate-db`)
* `aleutian populate` CLI command (with automatic PDF handling)
* `aleutian ask` CLI command (or OpenWebUI)
* Observability Stack (for verification/debugging)
* **Custom Container:** PDF Parser Service (added via override)
* **Configuration:** Podman Compose Override, Orchestrator Environment Variable

---

## 🛠️ Setup Steps

### 1. Prerequisites

* A running AleutianLocal core stack (v0.1.8+ recommended - *version containing integrated parsing logic*) installed via the [README instructions](../README.md#getting-started).
* Ollama running locally with a suitable model downloaded (e.g., `ollama pull llama3:8b`).
* Your PDF documents located in a folder on your Mac.

### 2. Add the PDF Parser Service & Configure Orchestrator

This step adds the container that knows how to read PDFs and tells the main Aleutian orchestrator how to find it.

* **Create Parser Code:** Ensure the PDF parser service code exists under `services/pdf_parser/` (including `pdf_parser.py`, `requirements.txt`, `Dockerfile` as detailed in the main documentation or previous examples). You only need to *have* this code locally; you don't run it directly.
* **Create/Edit Override File:** In your stack directory (`~/.aleutian/stack/` if installed via brew, or your cloned repo if running manually), create or edit the file named `podman-compose.override.yml` with the following content:

    ```yaml
    # ~/.aleutian/stack/podman-compose.override.yml
    services:
      # Define the PDF Parser Service
      pdf-parser:
        build:
          # Assumes override file is in the stack root where 'services/' exists
          context: ./services/pdf_parser
          dockerfile: Dockerfile
        container_name: aleutian-pdf-parser
        restart: unless-stopped
        networks:
          - aleutian-network # Connect to the Aleutian network
        # No host ports needed unless debugging directly
        healthcheck:
          test: ["CMD", "curl", "-f", "http://localhost:8001/health"] # Internal check
          interval: 10s
          timeout: 5s
          retries: 5

      # Configure the Orchestrator to USE the Parser
      orchestrator:
        environment:
          # This tells the orchestrator where to send PDFs for text extraction
          PDF_PARSER_URL: http://pdf-parser:8001/extract # Internal URL using service name
        depends_on:
          # Make orchestrator wait for the parser to be healthy before starting fully
          pdf-parser:
            condition: service_healthy
    ```

### 3. Restart Aleutian Stack with Parser Included

* **Apply Changes:** From *any* directory (if using brew-installed CLI) or your repo root (if running manually), run:
    ```bash
    # Stop the current stack if running
    aleutian stack stop
    # Start. The CLI automatically includes the override file. --build creates the pdf-parser image.
    aleutian stack start # --build is included by default in runStart now
    ```
* **Verify:** Wait a minute for startup and health checks, then run `podman ps -a`. You should see `aleutian-pdf-parser` in a `running (healthy)` state.

**⚡ Aleutian Value Add:** Adding specialized processing capabilities is straightforward via standard compose overrides and simple environment variable configuration.

---

## 🚀 Workflow: Ingesting and Querying PDFs

### 1. Prepare Your Documents

* Place the PDF files you want to ingest into a specific directory, for example: `~/Documents/CompanyPolicies/`.

### 2. Ingest Documents using `populate`

* Run the `aleutian populate` command, pointing it to your PDF directory:
    ```bash
    ./aleutian populate ~/Documents/CompanyPolicies/
    ```
* **What Happens (Simplified Flow):**
    * The CLI scans the directory.
    * For each PDF file, it performs the privacy scan.
    * If approved, the CLI sends the file path/content to the orchestrator's `/v1/documents` endpoint.
    * The orchestrator detects the PDF, sees `PDF_PARSER_URL` is set, and **automatically calls the `pdf-parser` service** to get the text.
    * The orchestrator then gets embeddings for the extracted text and stores everything in Weaviate.
    * Progress is logged to your terminal and `scan_log_*.json`.

**⚡ Aleutian Value Add:** Complex file processing (PDF extraction) is handled *automatically* during the standard ingestion command, requiring no extra steps or external tools like Airflow for the user.

### 3. Ask Questions

* Use the `aleutian ask` command or the integrated OpenWebUI.
    ```bash
    aleutian ask "What is the policy regarding vacation time?"
    # OR
    aleutian ask --pipeline standard "Summarize the section on code of conduct."
    ```
* **What Happens:** (Same as before) The RAG system finds the relevant text chunks (which were extracted from your PDFs) in Weaviate and uses them to generate an answer with the LLM.

**⚡ Aleutian Value Add:** Querying works identically regardless of the original file format, thanks to the integrated ingestion pipeline.

---

## 📊 Verification & Debugging

* **Check Ingestion:** Use `aleutian weaviate summary` to see if the `Document` count increased.
* **Check Logs:**
    * `aleutian stack logs pdf-parser`: **Crucial** for seeing PDF extraction success or errors.
    * `aleutian stack logs aleutian-go-orchestrator`: Shows calls to the parser, embedder, and Weaviate during ingestion.
    * `aleutian stack logs aleutian-rag-engine`: Shows logs for the RAG process during `ask`.
* **Check Observability:**
    * **Jaeger (`http://localhost:16686`):** Look for traces from `/v1/documents` calls (should ideally show spans for the orchestrator calling the `pdf-parser`) and `/v1/rag` calls.
    * **Grafana (`http://localhost:3000`):** Check dashboards for resource usage of `aleutian-pdf-parser` during ingestion and standard metrics during querying.

**⚡ Aleutian Value Add:** Integrated observability provides visibility into the entire flow, including the custom parser service, making it easier to diagnose issues.

