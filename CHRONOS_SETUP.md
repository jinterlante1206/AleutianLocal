# Amazon Chronos Models Integration Guide

## Overview

This guide explains how to use Amazon Chronos time series foundation models with Aleutian's forecast module.

## Supported Models

| Model ID | Size | Description |
|----------|------|-------------|
| `amazon/chronos-t5-tiny` | ~8M params | Fastest, lowest memory |
| `amazon/chronos-t5-mini` | ~20M params | Good balance |
| `amazon/chronos-t5-small` | ~46M params | Better accuracy |
| `amazon/chronos-t5-base` | ~200M params | High accuracy |
| `amazon/chronos-t5-large` | ~710M params | Best accuracy |

## Quick Start

### 1. Configure Forecast Mode

In `~/.aleutian/aleutian.yaml`:

```yaml
forecast:
  enabled: true
  mode: "standalone"  # Uses Aleutian's built-in forecast service
```

### 2. Start the Stack

```bash
./aleutian stack up
```

### 3. Create a Strategy

**File:** `strategies/spy_chronos_tiny_v1.yaml`
```yaml
metadata:
  id: "spy-chronos-tiny-demo"
  version: "1.0.0"
  description: "Testing SPY with Chronos T5 Tiny model"

evaluation:
  ticker: "SPY"
  fetch_start_date: "20221201"
  start_date: "20230101"
  end_date: "20240101"

forecast:
  model: "amazon/chronos-t5-tiny"
  context_size: 252
  horizon_size: 20

trading:
  initial_capital: 100000.0
  strategy_type: "threshold"
  params:
    threshold_type: "absolute"
    threshold_value: 2.0
    execution_size: 10.0
```

### 4. Run Evaluation

```bash
# Fetch historical data
./aleutian timeseries fetch SPY --days 1800

# Run evaluation
./aleutian evaluate run --ticker SPY --config strategies/spy_chronos_tiny_v1.yaml
```

## API Usage

### Direct Forecast Request

```bash
curl -X POST http://localhost:8000/v1/timeseries/forecast \
  -H "Content-Type: application/json" \
  -d '{
    "model": "amazon/chronos-t5-tiny",
    "data": [1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0],
    "horizon": 5,
    "num_samples": 20
  }'
```

### List Available Models

```bash
curl http://localhost:8000/v1/models
```

### Check Service Health

```bash
curl http://localhost:8000/health
```

## Model Selection Guide

| Use Case | Recommended Model |
|----------|-------------------|
| Quick testing / prototyping | `chronos-t5-tiny` |
| Daily forecasting workflows | `chronos-t5-mini` or `chronos-t5-small` |
| Production with accuracy priority | `chronos-t5-base` |
| Research / benchmarking | `chronos-t5-large` |

## Resource Requirements

| Model | RAM (approx) | Load Time |
|-------|--------------|-----------|
| chronos-t5-tiny | ~500MB | ~5s |
| chronos-t5-mini | ~1GB | ~10s |
| chronos-t5-small | ~2GB | ~15s |
| chronos-t5-base | ~4GB | ~30s |
| chronos-t5-large | ~8GB | ~60s |

## Troubleshooting

### Model fails to load

Check forecast service logs:
```bash
podman logs forecast-service
```

### Out of memory

Use a smaller model or increase container memory in `aleutian.yaml`:
```yaml
machine:
  memory_amount: 32768  # 32GB
```

### Slow inference

- Use `chronos-t5-tiny` for faster results
- Reduce `num_samples` in forecast request
- Enable GPU if available: set `DEVICE=cuda` in container env
