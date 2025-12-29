# Amazon Chronos Models Integration Guide

## Overview

This guide explains how the Amazon Chronos models are integrated into the Aleutian evaluation framework. 
The implementation follows the **Aleutian-first** architecture where intelligence lives in 
AleutianLocal (Go) and Sapheneia provides generic model serving.

## Architecture

```
┌─────────────────────────────────────────────┐
│ AleutianLocal (Go)                          │
│  └─ timeseries.go                           │
│     - Routes to specific Chronos containers │
│     - Already configured ✅                 │
└─────────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────┐
│ Docker Network (aleutian-shared)            │
│  - forecast-chronos-t5-tiny:8000            │
│  - forecast-chronos-t5-mini:8000            │
│  - forecast-chronos-t5-small:8000           │
│  - forecast-chronos-t5-base:8000            │
│  - forecast-chronos-t5-large:8000           │
│  - forecast-chronos-bolt-mini:8000          │
│  - forecast-chronos-bolt-small:8000         │
│  - forecast-chronos-bolt-base:8000          │
└─────────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────┐
│ Model Cache (Pre-downloaded)                │
│  /Volumes/ai_models/aleutian_data/          │
│  models_cache/models--amazon--chronos-*     │
└─────────────────────────────────────────────┘
```

## What Was Implemented

### 1. Sapheneia Changes (Minimal)

**Created new module:** `forecast/models/chronos/`
- `services/model.py` - Generic Chronos model loader
- `routes/endpoints.py` - REST API endpoints
- `schemas/schema.py` - Request/response schemas

**Key Features:**
- Loads any Chronos variant from cache
- Uses `HF_HOME` environment variable
- Thread-safe model management
- Follows TimesFM pattern

**Modified files:**
- `forecast/models/__init__.py` - Added all 8 Chronos models to registry
- `forecast/main.py` - Included Chronos router
- `Dockerfile.forecast` - Added Chronos dependencies
- `docker-compose.yml` - Added 8 Chronos service definitions

### 2. AleutianLocal Changes

**No changes needed!** Your existing `timeseries.go` already has all the routing configured.

## Testing the Integration

### Step 1: Build the Chronos Container (One-time)

```bash
cd /Users/jin/PycharmProjects/sapheneia

# Build the Chronos image
podman-compose build forecast-chronos-t5-tiny
```

### Step 2: Start One Chronos Container

```bash
# Start just the tiny model for testing
podman-compose up -d forecast-chronos-t5-tiny

# Check logs
podman logs -f forecast-chronos-t5-tiny
```

**Expected output:**
```
✅ Included Chronos router at: /forecast/v1/chronos
🚀 Application Startup
```

### Step 3: Test the API Directly

```bash
# Check health
curl http://localhost:8100/health

# Initialize the model
curl -X POST http://localhost:8100/forecast/v1/chronos/initialization \
  -H "Content-Type: application/json" \
  -H "X-API-Key: default_trading_api_key_please_change" \
  -d '{}'

# Check status
curl http://localhost:8100/forecast/v1/chronos/status \
  -H "X-API-Key: default_trading_api_key_please_change"

# Run simple inference
curl -X POST http://localhost:8100/forecast/v1/chronos/inference \
  -H "Content-Type: application/json" \
  -H "X-API-Key: default_trading_api_key_please_change" \
  -d '{
    "context": [1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0],
    "prediction_length": 5,
    "num_samples": 20
  }'
```

### Step 4: Test from AleutianLocal

Create a test strategy configuration:

**File:** `strategies/spy_chronos_tiny_v1.yaml`
```yaml
metadata:
  id: "spy-chronos-tiny-demo"
  version: "1.0.0"
  description: "Testing SPY with Chronos T5 Tiny model"
  author: "Jin"

evaluation:
  ticker: "SPY"
  fetch_start_date: "20221201"
  start_date: "20230101"
  end_date: "20240101"

forecast:
  model: "amazon/chronos-t5-tiny"  # This will route to forecast-chronos-t5-tiny
  context_size: 252
  horizon_size: 20

trading:
  initial_capital: 100000.0
  initial_position: 0.0
  initial_cash: 100000.0
  strategy_type: "threshold"
  params:
    threshold_type: "absolute"
    threshold_value: 2.0
    execution_size: 10.0
```

**Run the evaluation:**
```bash
cd /Users/jin/GolandProjects/AleutianLocal

# Make sure SPY data is fetched
./aleutian timeseries fetch SPY --days 1800

# Run evaluation
./aleutian evaluate run --ticker SPY --config strategies/spy_chronos_tiny_v1.yaml
```

## Model Variants Available

| Model ID | Container Name | Port | Model Size |
|----------|----------------|------|------------|
| `amazon/chronos-t5-tiny` | `forecast-chronos-t5-tiny` | 8100 | Smallest |
| `amazon/chronos-t5-mini` | `forecast-chronos-t5-mini` | 8101 | Small |
| `amazon/chronos-t5-small` | `forecast-chronos-t5-small` | 8102 | Medium |
| `amazon/chronos-t5-base` | `forecast-chronos-t5-base` | 8103 | Base |
| `amazon/chronos-t5-large` | `forecast-chronos-t5-large` | 8104 | Large |
| `amazon/chronos-bolt-mini` | `forecast-chronos-bolt-mini` | 8105 | Fast Mini |
| `amazon/chronos-bolt-small` | `forecast-chronos-bolt-small` | 8106 | Fast Small |
| `amazon/chronos-bolt-base` | `forecast-chronos-bolt-base` | 8107 | Fast Base |

## Starting Multiple Models

You can run multiple models simultaneously:

```bash
cd /Users/jin/PycharmProjects/sapheneia

# Start multiple models
podman-compose up -d forecast-chronos-t5-tiny forecast-chronos-t5-mini forecast-chronos-t5-small

# Or start all Chronos models
podman-compose up -d \
  forecast-chronos-t5-tiny \
  forecast-chronos-t5-mini \
  forecast-chronos-t5-small \
  forecast-chronos-t5-base \
  forecast-chronos-t5-large \
  forecast-chronos-bolt-mini \
  forecast-chronos-bolt-small \
  forecast-chronos-bolt-base
```

## Resource Considerations

Each Chronos container will:
- Load one model from cache into memory
- Use CPU by default (can be changed to GPU via `DEVICE=cuda`)
- Share the read-only models cache (no duplication)

**Recommended approach:**
- Start with 1-2 models for testing
- Add more as needed for specific evaluations
- Stop unused containers to free resources

## Troubleshooting

### Container fails to start

**Check logs:**
```bash
podman logs forecast-chronos-t5-tiny
```

**Common issues:**
1. Models cache not mounted correctly
2. Missing `aleutian-network` - create it:
   ```bash
   podman network create aleutian-shared
   ```

### Model initialization fails

**Verify cache access:**
```bash
podman exec forecast-chronos-t5-tiny ls -la /models_cache/models--amazon--chronos-t5-tiny
```

**Check HF_HOME:**
```bash
podman exec forecast-chronos-t5-tiny env | grep HF_HOME
```

### AleutianLocal can't connect

**Verify container is on correct network:**
```bash
podman network inspect aleutian-shared | grep forecast-chronos-t5-tiny
```

**Test from host:**
```bash
curl http://localhost:8100/health
```

## Next Steps

1. **Test chronos-t5-tiny** - Smallest model, fastest to load
2. **Compare with TimesFM** - Run same evaluation with both models
3. **Benchmark models** - Test different Chronos variants
4. **Scale up** - Add more models as needed

## Files Modified

### Sapheneia (Python)
- ✅ `forecast/models/chronos/` - New module
- ✅ `forecast/models/__init__.py` - Model registry
- ✅ `forecast/main.py` - Router inclusion
- ✅ `Dockerfile.forecast` - Dependencies
- ✅ `docker-compose.yml` - Service definitions

### AleutianLocal (Go)
- ℹ️ No changes needed - `timeseries.go` already configured!

## Model Cache Location

```
/Volumes/ai_models/aleutian_data/models_cache/
├── models--amazon--chronos-t5-tiny/
├── models--amazon--chronos-t5-mini/
├── models--amazon--chronos-t5-small/
├── models--amazon--chronos-t5-base/
├── models--amazon--chronos-t5-large/
├── models--amazon--chronos-bolt-mini/
├── models--amazon--chronos-bolt-small/
└── models--amazon--chronos-bolt-base/
```

All mounted as **read-only** to containers at `/models_cache`.
