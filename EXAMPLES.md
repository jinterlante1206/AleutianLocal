# Aleutian Local Examples

This document provides detailed examples for Aleutian's features. For quick reference, see the [README.md](README.md).

---

## Table of Contents

1. [Chat Sessions](#chat-sessions)
2. [Session Verification](#session-verification)
3. [RAG Queries](#rag-queries)
4. [Document Ingestion](#document-ingestion)
5. [Code Analysis Agent](#code-analysis-agent)
6. [Time Series Forecasting](#time-series-forecasting)
7. [Configuration Examples](#configuration-examples)
8. [REST API Examples](#rest-api-examples)
9. [Python SDK Examples](#python-sdk-examples)

---

## Chat Sessions

### Basic Chat

```bash
# Start a new chat session
aleutian chat

# Chat with RAG disabled (direct LLM)
aleutian chat --no-rag
```

### Extended Thinking Mode

For complex reasoning tasks with Claude 3.7+ or thinking-enabled Ollama models:

```bash
# Enable thinking with default budget (2048 tokens)
aleutian chat --thinking

# Enable thinking with higher budget for complex tasks
aleutian chat --thinking --budget 8000

# Use thinking with specific model
aleutian chat --thinking --budget 4096 --model gpt-oss:20b
```

### Resuming Sessions

```bash
# List available sessions
aleutian session list

# Resume a specific session (LLM retains full context)
aleutian chat --resume c55ce14f-759c-5888-b59c-759cc55ce14f
```

### Personality Modes

Control output verbosity with the `ALEUTIAN_PERSONALITY` environment variable:

```bash
# Full mode (default) - rich formatting with boxes and icons
ALEUTIAN_PERSONALITY=full aleutian chat

# Standard mode - colors and icons, moderate detail
ALEUTIAN_PERSONALITY=standard aleutian chat

# Minimal mode - compact output
ALEUTIAN_PERSONALITY=minimal aleutian chat

# Machine mode - plain text for scripting
ALEUTIAN_PERSONALITY=machine aleutian chat
```

---

## Session Verification

### CLI Verification

```bash
# Quick verification (check hash chain links)
aleutian session verify c55ce14f-759c-5888-b59c-759cc55ce14f

# Full verification (recompute all hashes)
aleutian session verify c55ce14f-759c-5888-b59c-759cc55ce14f --full

# JSON output for scripting
aleutian session verify c55ce14f-759c-5888-b59c-759cc55ce14f --json

# Check verification status in scripts
if aleutian session verify $SESSION_ID --json | jq -e '.verified'; then
    echo "Session integrity verified"
else
    echo "WARNING: Session may have been tampered with!"
fi
```

### REST API Verification

```bash
# Verify session via API
curl -X POST http://localhost:12210/v1/sessions/c55ce14f-759c-5888-b59c-759cc55ce14f/verify

# Response (success):
# {
#   "session_id": "c55ce14f-759c-5888-b59c-759cc55ce14f",
#   "verified": true,
#   "turn_count": 5,
#   "chain_hash": "a3f2c8d9e1b4f7a6c5d8e9f0a1b2c3d4...",
#   "verified_at": 1735657200000
# }

# Response (failure):
# {
#   "session_id": "c55ce14f-759c-5888-b59c-759cc55ce14f",
#   "verified": false,
#   "turn_count": 5,
#   "error_details": "hash mismatch at turn 3",
#   "verified_at": 1735657200000
# }
```

### Example Output (Full Personality)

**Successful Verification:**
```
╔══════════════════════════════════════════════════════════════╗
║           INTEGRITY VERIFICATION SUCCESSFUL                  ║
╚══════════════════════════════════════════════════════════════╝

  Session:    c55ce14f-759c-5888-b59c-759cc55ce14f
  Status:     ✓ VERIFIED
  Turns:      5 conversation turns verified
  Chain Hash: a3f2c8d9...e7f8a9b0
  Verified:   2026-01-07T10:30:00Z

  The hash chain is intact. No tampering detected.
```

**Failed Verification:**
```
╔══════════════════════════════════════════════════════════════╗
║           INTEGRITY VERIFICATION FAILED                      ║
╚══════════════════════════════════════════════════════════════╝

  Session:    c55ce14f-759c-5888-b59c-759cc55ce14f
  Status:     ✗ FAILED
  Error:      hash mismatch at turn 3

  ⚠ WARNING: The hash chain may have been tampered with.
  Please investigate this session's history.
```

---

## RAG Queries

### Basic Query

```bash
# RAG-powered question answering
aleutian ask "What is the authentication flow in this codebase?"

# Use specific retrieval pipeline
aleutian ask "How does the policy engine work?" --pipeline reranking
aleutian ask "List all API endpoints" --pipeline standard
```

### Direct LLM Query (No RAG)

```bash
# Skip retrieval, ask LLM directly
aleutian ask "Explain OAuth 2.0" --no-rag
```

---

## Document Ingestion

### Basic Ingestion

```bash
# Ingest a single file
aleutian populate vectordb ./README.md

# Ingest a directory
aleutian populate vectordb ./docs/

# Ingest multiple paths
aleutian populate vectordb ./src/ ./docs/ ./README.md
```

### With Data Spaces and Versioning

```bash
# Organize into logical data spaces
aleutian populate vectordb ./engineering-docs --data-space engineering
aleutian populate vectordb ./legal-docs --data-space legal --version v2.0

# Force ingestion (skip security prompts)
aleutian populate vectordb ./code --force
```

### Security Scan Output

When PII or secrets are detected:

```
╔══════════════════════════════════════════════════════════════╗
║                    SECURITY SCAN RESULTS                     ║
╚══════════════════════════════════════════════════════════════╝

  Scanned: 47 files

  ⚠  FINDINGS DETECTED:

  ./config.py:23
    Pattern: API_KEY (HIGH)
    Match: "sk-...xxxx"

  ./users.csv:156
    Pattern: EMAIL (MEDIUM)
    Match: "john.doe@example.com"

  Do you want to proceed with ingestion? [y/N]:
```

---

## Code Analysis Agent

### Basic Usage

```bash
# Analyze specific functionality
aleutian trace "How does the authentication system work?"

# Architectural analysis
aleutian trace "What is the data flow from API request to database?"

# Find specific implementations
aleutian trace "Where is the rate limiting logic implemented?"
```

### Example Output

```
🔍 Agent Analysis: Authentication System

The authentication system uses JWT tokens with the following flow:

1. Login Request (cmd/auth/login.go:45)
   - Validates credentials against database
   - Generates JWT with 24h expiry

2. Middleware (services/auth/middleware.go:23)
   - Extracts token from Authorization header
   - Validates signature and expiry
   - Adds user context to request

3. Protected Routes (routes/api.go:67)
   - All /api/* routes use AuthMiddleware
   - Role-based access control via Claims

Files examined:
  - cmd/auth/login.go
  - services/auth/middleware.go
  - services/auth/jwt.go
  - routes/api.go
```

---

## Time Series Forecasting

### Fetch Historical Data

```bash
# Fetch 1 year of data for single ticker
aleutian timeseries fetch SPY

# Fetch multiple tickers
aleutian timeseries fetch SPY QQQ AAPL

# Specify date range
aleutian timeseries fetch SPY --days 500
```

### Run Forecast

```bash
# Basic forecast (standalone mode)
aleutian timeseries forecast SPY

# With specific model and horizon
aleutian timeseries forecast SPY --model chronos-t5-small --horizon 30

# Sapheneia mode (requires sapheneia containers)
aleutian stack start --forecast-mode sapheneia
aleutian timeseries forecast SPY --model amazon/chronos-t5-tiny
```

### Backtesting

```bash
# Run evaluation with strategy file
aleutian evaluate run --config strategies/spy_threshold_v1.yaml

# Run for specific date
aleutian evaluate run --config strategies/spy_threshold_v1.yaml --date 20251220

# Export results
aleutian evaluate export abc123 -o my_backtest.csv
```

---

## Configuration Examples

### Full Configuration (`~/.aleutian/aleutian.yaml`)

```yaml
# Podman machine settings (macOS)
machine:
  name: aleutian
  cpus: 8
  memory: "16384"
  disk_size: "100"
  rootful: true

# Ollama settings
ollama:
  host: "http://host.containers.internal:11434"
  model: "qwen2.5-coder:14b"
  embedding_model: "nomic-embed-text-v2-moe"
  num_ctx: 32768
  num_gpu: -1

# Hardware profile
profile:
  mode: auto  # auto, low, standard, performance, ultra, manual

# Forecast service
forecast:
  enabled: true
  mode: standalone  # standalone or sapheneia

# Session integrity (new in v0.3.5)
session_integrity:
  enabled: true
  verification_mode: quick
  auto_verify_on_end: true
  show_hash_in_summary: true

  enterprise:
    hmac:
      enabled: false
      key_provider: vault
      key_id: "aleutian-hmac-key-v1"
    tsa:
      enabled: false
      provider: digicert
    hsm:
      enabled: false
      provider: pkcs11
    audit:
      enabled: false
      destination: siem
      retention_days: 2555
    rate_limiting:
      enabled: true
      requests_per_minute: 60
    caching:
      enabled: true
      backend: redis
      ttl: 5m
    alerting:
      enabled: false
      destinations: [slack, pagerduty]

# Model management
model_management:
  auto_pull: true
  version_pinning:
    enabled: false
  integrity:
    verify_checksums: true
```

### Override File (`~/.aleutian/stack/podman-compose.override.yml`)

```yaml
services:
  orchestrator:
    environment:
      # Switch to Anthropic backend
      LLM_BACKEND_TYPE: anthropic
      ANTHROPIC_MODEL: claude-3-5-sonnet-20241022

      # Enable extended thinking
      ENABLE_THINKING: "true"
      THINKING_BUDGET_TOKENS: "4000"

      # Custom ports
      PORT: "12210"

    # Add secrets
    secrets:
      - anthropic_api_key

  # Add custom service
  my-pdf-parser:
    build:
      context: /path/to/my-pdf-parser
    networks:
      - aleutian-network
```

---

## REST API Examples

### Health Check

```bash
curl http://localhost:12210/health
# {"status": "ok", "version": "0.3.5"}
```

### Chat API

```bash
# Direct chat (no RAG)
curl -X POST http://localhost:12210/v1/chat/direct/stream \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [{"role": "user", "content": "Hello!"}],
    "session_id": "new-session-123"
  }'

# RAG chat
curl -X POST http://localhost:12210/v1/chat/rag/stream \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [{"role": "user", "content": "How does auth work?"}],
    "session_id": "rag-session-456"
  }'
```

### Session Management

```bash
# List sessions
curl http://localhost:12210/v1/sessions

# Get session history
curl http://localhost:12210/v1/sessions/c55ce14f-759c-5888-b59c-759cc55ce14f/history

# Get session documents
curl http://localhost:12210/v1/sessions/c55ce14f-759c-5888-b59c-759cc55ce14f/documents

# Verify session integrity
curl -X POST http://localhost:12210/v1/sessions/c55ce14f-759c-5888-b59c-759cc55ce14f/verify

# Delete session
curl -X DELETE http://localhost:12210/v1/sessions/c55ce14f-759c-5888-b59c-759cc55ce14f
```

### Weaviate Queries

```bash
# Database summary
curl http://localhost:12210/v1/weaviate/summary

# Backup
curl -X POST http://localhost:12210/v1/weaviate/backups \
  -H "Content-Type: application/json" \
  -d '{"id": "backup-2026-01-07", "action": "create"}'

# Restore
curl -X POST http://localhost:12210/v1/weaviate/backups \
  -H "Content-Type: application/json" \
  -d '{"id": "backup-2026-01-07", "action": "restore"}'
```

---

## Python SDK Examples

### Installation

```bash
pip install aleutian-client
```

### Basic Usage

```python
from aleutian_client import AleutianClient, Message

with AleutianClient() as client:
    # Health check
    health = client.health_check()
    print(f"Status: {health['status']}")

    # RAG query
    response = client.ask("What is OAuth?", pipeline="reranking")
    print(f"Answer: {response.answer}")
    for source in response.sources:
        print(f"  Source: {source.source}")

    # Chat with thinking
    messages = [Message(role="user", content="Explain quantum computing")]
    response = client.chat(
        messages=messages,
        enable_thinking=True,
        budget_tokens=4000
    )
    print(f"Answer: {response.answer}")
```

### Session Management

```python
from aleutian_client import AleutianClient

with AleutianClient() as client:
    # List sessions
    sessions = client.list_sessions()
    for session in sessions:
        print(f"Session: {session.session_id}")
        print(f"  Summary: {session.summary}")

    # Get session history
    history = client.get_session_history("c55ce14f-759c-5888-b59c-759cc55ce14f")
    for turn in history:
        print(f"Q: {turn.question}")
        print(f"A: {turn.answer[:100]}...")

    # Verify session integrity
    result = client.verify_session("c55ce14f-759c-5888-b59c-759cc55ce14f")
    if result.verified:
        print(f"✓ Session verified ({result.turn_count} turns)")
    else:
        print(f"✗ Verification failed: {result.error_details}")
```

### Document Ingestion

```python
from aleutian_client import AleutianClient
from pathlib import Path

with AleutianClient() as client:
    # Ingest a file
    result = client.ingest_document(
        path=Path("./docs/README.md"),
        data_space="documentation",
        version="v1.0"
    )
    print(f"Ingested {result.chunks} chunks")

    # Ingest directory
    results = client.ingest_directory(
        path=Path("./src"),
        data_space="source-code",
        recursive=True
    )
    print(f"Ingested {len(results)} files")
```

### Forecasting

```python
from aleutian_client import AleutianClient

with AleutianClient() as client:
    # Fetch historical data
    client.fetch_timeseries(["SPY", "QQQ"], days=365)

    # Run forecast
    forecast = client.forecast(
        ticker="SPY",
        model="chronos-t5-small",
        horizon=20,
        context=300
    )

    print(f"Forecast for {forecast.ticker}:")
    for i, pred in enumerate(forecast.predictions):
        print(f"  Day {i+1}: {pred:.2f}")
```

---

## Troubleshooting

### Common Issues

**Stack won't start:**
```bash
# Check logs
aleutian stack logs orchestrator

# Force recreate machine
aleutian stack start --force-recreate
```

**Session verification fails:**
```bash
# Check session exists
aleutian session list | grep <session_id>

# Check Weaviate connectivity
curl http://localhost:12127/v1/.well-known/ready
```

**Models not loading:**
```bash
# Check Ollama status
curl http://localhost:11434/api/tags

# Force model pull
aleutian pull qwen2.5-coder:14b
```

---

For more information, see the [README.md](README.md) or visit the [GitHub repository](https://github.com/jinterlante1206/AleutianLocal).
