# Aleutian Forecast Module Architecture

## Overview

Aleutian is a privacy-focused platform for offline RAG (Retrieval-Augmented Generation) with agentic code processing. The **forecast module** is an optional bolt-on component that provides time series forecasting capabilities, primarily for financial data analysis.

This document describes the architecture of the forecast module and the changes made to support dual deployment modes.

## System Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              ALEUTIAN STACK                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────┐     ┌──────────────────┐     ┌─────────────────────────┐  │
│  │   CLI Tool   │────▶│   Orchestrator   │────▶│   Forecast Service      │  │
│  │  (Go Binary) │     │   (Go/Gin)       │     │   (Python/FastAPI)      │  │
│  └──────────────┘     └────────┬─────────┘     └─────────────────────────┘  │
│                                │                          │                  │
│                                │                          │                  │
│                    ┌───────────┴───────────┐    ┌────────┴────────┐        │
│                    ▼                       ▼    ▼                 ▼        │
│            ┌─────────────┐         ┌──────────────┐    ┌─────────────────┐ │
│            │  InfluxDB   │         │ Data Fetcher │    │  HuggingFace    │ │
│            │  (OHLC Data)│         │  (Go/Gin)    │    │  Model Cache    │ │
│            └─────────────┘         └──────────────┘    └─────────────────┘ │
│                                           │                                 │
│                                           ▼                                 │
│                                    ┌─────────────┐                         │
│                                    │ Yahoo/Alpha │                         │
│                                    │ Vantage API │                         │
│                                    └─────────────┘                         │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Core Services

### 1. CLI Tool (`cmd/aleutian/`)

The main entry point for Aleutian. Manages:
- Stack lifecycle (`stack up`, `stack down`)
- Configuration (`~/.aleutian/aleutian.yaml`)
- Evaluation jobs (`evaluate run`)
- Forecast commands (`forecast`)

Key files:
- `cmd_stack.go` - Podman container orchestration
- `cmd_evaluation.go` - Strategy evaluation runner
- `cmd_infra.go` - Infrastructure helpers (path resolution, lock management, disk space)
- `config/types.go` - Configuration types including `ForecastConfig`

### 2. Orchestrator (`services/orchestrator/`)

Go service that acts as the central API gateway. Routes requests to appropriate backend services.

**Handlers:**
- `handlers/timeseries.go` - Time series forecast routing
- `handlers/trading.go` - Trading data queries
- `handlers/evaluator.go` - LLM evaluation coordination

**Key Functions:**
```go
// Routes forecast requests based on mode and model
getSerivceURL(modelName string) (string, error)

// Proxies requests to forecast service with data injection
HandleTimeSeriesForecast() gin.HandlerFunc

// Proxies data fetch requests
HandleDataFetch() gin.HandlerFunc
```

### 3. Forecast Service (`services/forecast/`)

Python FastAPI service that runs time series models locally.

**Capabilities:**
- Model loading/unloading with FIFO eviction
- Multi-model support (Chronos T5, Chronos Bolt, TimesFM, etc.)
- Probabilistic forecasting with confidence intervals

**API Endpoints:**
```
GET  /health                 - Service health check
GET  /v1/models             - List available models
POST /v1/models/load        - Load a model into memory
POST /v1/models/unload      - Unload a model
POST /v1/timeseries/forecast - Generate forecast
```

### 4. Data Fetcher (`services/data_fetcher/`)

Go service that fetches and stores market data.

**Capabilities:**
- Fetch OHLC data from Yahoo Finance/Alpha Vantage
- Store in InfluxDB for historical queries
- Support for batch fetching multiple tickers

**API Endpoints:**
```
POST /v1/data/fetch  - Fetch and store market data
POST /v1/data/query  - Query stored data
GET  /v1/data/tickers - List available tickers
```

## Dual Deployment Modes

The forecast module supports two deployment modes configured in `~/.aleutian/aleutian.yaml`:

### Standalone Mode (Default)

```yaml
forecast:
  enabled: true
  mode: "standalone"
```

Aleutian runs its own forecast service. All models are loaded into a single container.

```
┌────────────────┐     ┌───────────────────┐
│  Orchestrator  │────▶│  forecast-service │ (single container, all models)
└────────────────┘     └───────────────────┘
```

**Pros:**
- Self-contained, no external dependencies
- Simpler deployment
- Automatic model eviction when memory is constrained

**Cons:**
- Limited by single container's GPU memory
- Model loading latency when switching models

### Sapheneia Mode

```yaml
forecast:
  enabled: true
  mode: "sapheneia"
```

Connects to external Sapheneia containers, each running a dedicated model.

```
┌────────────────┐     ┌─────────────────────────┐
│  Orchestrator  │────▶│ forecast-chronos-t5-tiny│
│                │────▶│ forecast-chronos-t5-base│
│                │────▶│ forecast-timesfm-2-0    │
│                │────▶│ forecast-moirai-1-1-base│
└────────────────┘     └─────────────────────────┘
```

**Pros:**
- Dedicated GPU per model
- No model loading latency
- Scales to many models

**Cons:**
- Requires external infrastructure
- More complex deployment

## Request Flow

### Forecast Request (Standalone Mode)

```
1. CLI sends POST /v1/timeseries/forecast to Orchestrator
2. Orchestrator reads ALEUTIAN_FORECAST_MODE=standalone
3. Orchestrator routes to forecast-service:8000
4. If request has only ticker (no data):
   a. Orchestrator fetches history from InfluxDB
   b. Injects data into request
5. Forecast service loads model (if needed)
6. Forecast service generates predictions
7. Response returns through Orchestrator to CLI
```

### Forecast Request (Sapheneia Mode)

```
1. CLI sends POST /v1/timeseries/forecast to Orchestrator
2. Orchestrator reads ALEUTIAN_FORECAST_MODE=sapheneia
3. Orchestrator normalizes model name to slug
4. Orchestrator routes to model-specific container:
   - chronos-t5-tiny → forecast-chronos-t5-tiny:8000
   - timesfm-2-0 → forecast-timesfm-2-0:8000
5. Model container generates predictions (already loaded)
6. Response returns through Orchestrator to CLI
```

## Configuration

### Global Config (`~/.aleutian/aleutian.yaml`)

```yaml
# Machine settings for Podman VM
machine:
  id: "podman-machine-default"
  cpu_count: 6
  memory_amount: 20480
  drives:
    - "/Users/jin"
    - "/Volumes"

# Model backend for LLM inference
model_backend:
  type: "ollama"
  base_url: "http://ollama:11434"

# Forecast module (optional)
forecast:
  enabled: true
  mode: "standalone"  # or "sapheneia"
```

### Strategy Config (`strategies/*.yaml`)

```yaml
name: spy-threshold-demo
version: "1.0.0"
type: "backtest"

forecast:
  model: "amazon/chronos-t5-tiny"
  context_period_size: 252
  forecast_period_size: 5

assets:
  - ticker: SPY
    weight: 1.0
```

## Model Compatibility

Supported models in the forecast service:

| Family | Models | Status |
|--------|--------|--------|
| Chronos T5 | tiny, mini, small, base, large | Verified |
| Chronos Bolt | mini, small, base | Broken (API incompatible) |
| TimesFM | 1.0, 2.0, 2.5 | Experimental |
| Moirai | 1.0, 1.1, 2.0 | Experimental |
| Granite | TTM-R1, TTM-R2 | Experimental |

## Changes in This Commit

### Bug Fixes

1. **Nil Pointer in Data Fetcher** (`services/data_fetcher/main.go`)

   InfluxDB can return nil for empty query results. Added guards:
   ```go
   if result == nil {
       // Return empty response instead of crashing
       c.JSON(http.StatusOK, DataQueryResponse{...})
       return
   }
   ```

2. **Unbounded Recursion in Model Eviction** (`services/forecast/server.py`)

   Old code used recursion to wait for models to become available:
   ```python
   # BEFORE: Stack overflow risk
   def evict_oldest_model():
       if all_models_in_use:
           time.sleep(0.5)
           return evict_oldest_model()  # Recursive!
   ```

   Fixed with iterative loop and max attempts:
   ```python
   # AFTER: Bounded, raises after 30 seconds
   def evict_oldest_model(max_attempts: int = 60):
       for attempt in range(max_attempts):
           # Try to evict
           if evicted:
               return True
           time.sleep(0.5)
       raise RuntimeError("Unable to evict")
   ```

3. **Test Helper Bug** (`cmd/aleutian/cmd_chat_test.go`)

   The `contains()` function was checking prefix instead of substring:
   ```go
   // BEFORE: Wrong - prefix check
   return s[0:len(substr)] == substr

   // AFTER: Correct
   return strings.Contains(s, substr)
   ```

### New Test Coverage

| File | Tests | Coverage Area |
|------|-------|--------------|
| `timeseries_test.go` | 19 | Model routing, URL resolution, request proxying |
| `main_test.go` | 8 | Data fetcher handlers, Yahoo parsing |
| `cmd_infra_test.go` | 23 | Path resolution, locks, disk space |
| `test_server.py` | 25 | Model loading, forecasting, eviction |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ALEUTIAN_FORECAST_MODE` | `standalone` | Deployment mode |
| `ALEUTIAN_TIMESERIES_TOOL` | `http://forecast-service:8000` | Forecast service URL |
| `ALEUTIAN_DATA_FETCHER_URL` | - | Data fetcher service URL |
| `INFLUXDB_URL` | `http://influxdb:8086` | InfluxDB connection |
| `INFLUXDB_TOKEN` | - | InfluxDB auth token |
| `INFLUXDB_ORG` | - | InfluxDB organization |
| `INFLUXDB_BUCKET` | - | InfluxDB bucket for OHLC data |
| `SAPHENEIA_TRADING_API_KEY` | - | API key for Sapheneia mode |

## Testing

Run unit tests:
```bash
# Go tests
go test ./cmd/... ./services/... -v

# Python tests
cd services/forecast && pytest test_server.py -v
```

Integration tests require external services:
```bash
# Start the stack first
./aleutian stack up

# Run integration tests
RUN_INTEGRATION_TESTS=1 go test ./test/integration/... -v
```

---

*Last updated: December 2025*
