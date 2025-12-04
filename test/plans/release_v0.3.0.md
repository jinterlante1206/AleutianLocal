# ✅ Aleutian v0.3.0 Release Verification Checklist

**Date:** Nov 30, 2025
**Tester:** [Your Name]
**Environment:** macOS (Apple Silicon) / Podman

---

## 1. Infrastructure & Deployment (The Foundation)
*If the stack doesn't stand up, nothing else matters.*

### 1.1 Clean Install
- [x] **Prune Environment:** Run `podman system prune -a` (optional, but recommended for a 
  "fresh machine" simulation).
- [x] **Build Stack:** Run `./aleutian stack start`.
- [x] **Container Status:** Verify all containers are `Up` / `Running`:
    - [x] `aleutian-orchestrator` (Go)
    - [x] `aleutian-engine` (Python)
    - [x] `weaviate`
    - [x] `jaeger`

### 1.2 Configuration & Secrets
- [x] **Secret Safety:** Check Orchestrator logs (`podman logs aleutian-orchestrator`). Ensure 
  API keys (Anthropic/OpenAI) are loaded but **masked/hidden**.
- [x] **Volume Mounts:** Verify the Python engine can see the host filesystem.
    - *Command:* ` podman exec -it aleutian-rag-engine ls /root/.cache/huggingface` (or your configured mount path).
    - *Pass Criteria:* Must list your actual local project files.

---

## 2. Agentic Capabilities (New Feature)
*Testing the new "Trace" command and Go->Python proxy logic.*

### 2.1 The "Happy Path" (Standard Trace)
- [ ] **Command:** `aleutian trace "Read the first 5 lines of buffalo_history.txt located in test/rag_files/"`
- [ ] **Execution:** Watch logs. Agent should execute `list_files` then `read_file`.
- [ ] **Output:** CLI returns the text content of the file.
- [ ] **Loop Termination:** Ensure the agent doesn't get stuck in an infinite loop of reading the same file.

### 2.2 Security / Policy Enforcement
- [ ] **Command:** `aleutian trace "Read /etc/passwd"` (or any file outside the allowed mount).
- [ ] **Enforcement:** The Policy Engine (Go) or Pre-check (Python) must block the tool execution.
- [ ] **Result:** LLM receives "Access Denied" or "Path not allowed" error; User sees the refusal message.

### 2.3 LLM Backend Switching
- [ ] **Switch Model:** Change config/env to `LLM_BACKEND_TYPE=ollama`.
- [ ] **Retest Trace:** Run a simple trace.
- [ ] **Pass Criteria:** System functions using local model (e.g., Llama 3 / Gemma) instead of Anthropic.

---

## 3. Advanced LLM Features (Claude 3.7)
*Testing the new custom REST client integration.*

### 3.1 Extended Thinking
- [ ] **Command:** `aleutian chat --thinking --budget 2000 "Explain the relationship between quantum mechanics and general relativity"`
- [ ] **Thinking Logs:** CLI shows "Thinking..." blocks streaming or logging.
- [ ] **Clean Output:** Final response contains *only* the answer (no leaked JSON or raw XML thinking tags).

### 3.2 Prompt Caching
- [ ] **First Run:** Run a query with a large context (or large system prompt). Note the latency.
- [ ] **Second Run:** Run the exact same query immediately after.
- [ ] **Verification:** Response should be significantly faster (Time-to-First-Token). Check logs for `cache_hit` metrics if available.

---

## 4. Time-Series Forecasting (The "Heavy" Lift)
*Verifying the Unified API and Model Weights.*

### 4.1 Dependency Check
- [ ] **Library Conflict:** Ensure Python container stays running when both `torch` (Chronos) and `tensorflow/jax` (TimesFM) are imported.

### 4.2 Offline Caching
- [ ] **Download:** Run `aleutian cache-all`.
- [ ] **Verification:** Check `~/.aleutian/cache` (or configured path). Verify file sizes look appropriate (GBs, not KBs).

### 4.3 Model Accuracy & Execution
*Use a simple JSON payload for these API tests.*

- [ ] **Google TimesFM:**
    - Input: `model: timesfm-1.0-200m`
    - Result: Returns valid forecast array. No JAX OOM crashes.
- [ ] **Amazon Chronos:**
    - Input: `model: chronos-t5-small`
    - Result: Returns valid forecast.

---

## 5. Regression Testing (Existing RAG)
*Ensuring v0.2.0 features still work using your new test data.*

### 5.1 Ingestion
- [ ] **Ingest Test Data:** `aleutian ingest --file test/rag_files/buffalo_history.txt`
- [ ] **Ingest Test Data:** `aleutian ingest --file test/rag_files/detroit_history.txt`
- [ ] **DB Check:** Verify Weaviate object count increased.

### 5.2 Retrieval
- [ ] **Query:** `aleutian ask "What is the history of Buffalo?"`
- [ ] **Accuracy:** Response must mention facts specifically from `buffalo_history.txt`.
- [ ] **Hallucination Check:** Ensure it doesn't mix in Detroit history unless asked.

---

## 6. Automated Suite
*Running the script you built in `test/release/`.*

### 6.1 Python Test Script
- [ ] **Run:** `python3 test/release/test_v0.3.0.py`
- [ ] **Results:** All assertions pass (Health, RAG Status, Forecasting).