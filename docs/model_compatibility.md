# Aleutian Model Compatibility Matrix

This document tracks which time-series forecasting models are supported by Aleutian.

## Status Legend

| Status | Meaning |
|--------|---------|
| **VERIFIED** | Tested and confirmed working |
| **UNTESTED** | Listed but not yet verified |
| **BROKEN** | Known issues, do not use |

## Chronos (Amazon)

| Model | Slug | VRAM | Status | Notes |
|-------|------|------|--------|-------|
| Chronos T5 Tiny | `chronos-t5-tiny` | 0.5 GB | **VERIFIED** | Best for CPU/low-memory |
| Chronos T5 Mini | `chronos-t5-mini` | 1.0 GB | **VERIFIED** | Good balance |
| Chronos T5 Small | `chronos-t5-small` | 2.0 GB | **VERIFIED** | |
| Chronos T5 Base | `chronos-t5-base` | 4.0 GB | **VERIFIED** | |
| Chronos T5 Large | `chronos-t5-large` | 8.0 GB | **VERIFIED** | Best accuracy |
| Chronos Bolt Mini | `chronos-bolt-mini` | 1.0 GB | **BROKEN** | Do not use |
| Chronos Bolt Small | `chronos-bolt-small` | 2.0 GB | **BROKEN** | Do not use |
| Chronos Bolt Base | `chronos-bolt-base` | 4.0 GB | **BROKEN** | Do not use |

## TimesFM (Google)

| Model | Slug | VRAM | Status | Notes |
|-------|------|------|--------|-------|
| TimesFM 1.0 200M | `timesfm-1-0` | 8.0 GB | UNTESTED | Priority for testing |
| TimesFM 2.0 | `timesfm-2-0` | 16.0 GB | UNTESTED | |
| TimesFM 2.5 | `timesfm-2-5` | 16.0 GB | UNTESTED | |

## Moirai (Salesforce)

| Model | Slug | VRAM | Status | Notes |
|-------|------|------|--------|-------|
| Moirai 1.1 Small | `moirai-1-1-small` | 2.0 GB | UNTESTED | |
| Moirai 1.1 Base | `moirai-1-1-base` | 4.0 GB | UNTESTED | |
| Moirai 1.1 Large | `moirai-1-1-large` | 8.0 GB | UNTESTED | |

## Granite (IBM)

| Model | Slug | VRAM | Status | Notes |
|-------|------|------|--------|-------|
| Granite TTM R1 | `granite-ttm-r1` | 4.0 GB | UNTESTED | |
| Granite TTM R2 | `granite-ttm-r2` | 4.0 GB | UNTESTED | |

## MOMENT (AutonLab)

| Model | Slug | VRAM | Status | Notes |
|-------|------|------|--------|-------|
| MOMENT Small | `moment-small` | 2.0 GB | UNTESTED | |
| MOMENT Base | `moment-base` | 4.0 GB | UNTESTED | |
| MOMENT Large | `moment-large` | 8.0 GB | UNTESTED | |

## Other Models

| Model | Slug | VRAM | Status | Notes |
|-------|------|------|--------|-------|
| Lag-LLaMA | `lag-llama` | 4.0 GB | UNTESTED | |

---

## Testing Environment

### Ubuntu RTX 5090 Server
- **GPU**: NVIDIA RTX 5090 (32 GB VRAM)
- **CPU**: Intel i5
- **RAM**: 128 GB
- **Purpose**: Model verification and benchmarking

### Testing Procedure

1. Build forecast service with GPU support:
   ```bash
   cd services/forecast
   podman build -f Dockerfile.gpu -t aleutian-forecast:gpu .
   ```

2. Run container with GPU:
   ```bash
   podman run --gpus all -p 8000:8000 aleutian-forecast:gpu
   ```

3. Test model:
   ```bash
   curl -X POST http://localhost:8000/v1/timeseries/forecast \
     -H "Content-Type: application/json" \
     -d '{
       "model": "chronos-t5-tiny",
       "data": [100, 101, 102, 103, 104, 105, 106, 107, 108, 109],
       "horizon": 5
     }'
   ```

4. Check VRAM usage:
   ```bash
   nvidia-smi
   ```

5. Update this matrix with results.

---

## Last Updated
2025-12-28
