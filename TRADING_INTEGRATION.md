# Sapheneia Trading Integration Guide

This document describes the integration between the AleutianLocal orchestrator and the Sapheneia trading strategies service.

## Architecture Overview

```
┌─────────────────┐
│   User/Client   │
└────────┬────────┘
         │
         ▼
┌─────────────────────────────────────────────┐
│     Orchestrator (Go)                       │
│     POST /v1/trading/signal                 │
└──┬──────────┬───────────┬──────────────────┘
   │          │           │
   │          │           └──────────────────┐
   │          │                              │
   ▼          ▼                              ▼
┌──────┐  ┌────────────────┐    ┌──────────────────────┐
│InfluxDB  │ Forecast Svc   │    │ Trading Service      │
│  OHLC    │ (TimesFM 2.0)  │    │ (Sapheneia)          │
│  Data    │ Price Forecast │    │ /trading/execute     │
└──────┘  └────────────────┘    └──────────────────────┘
```

## Components

### 1. **Data Fetcher Service** (Go)
- **URL**: `http://finance-data-service:8000`
- **Endpoint**: `POST /v1/data/fetch`
- **Purpose**: Fetches historical data from Yahoo Finance and stores it in InfluxDB

### 2. **Forecast Service** (Python - TimesFM)
- **URL**: `http://finance-analysis-service:8000`
- **Endpoint**: `POST /v1/timeseries/forecast`
- **Purpose**: Generates price forecasts using TimesFM 2.0 model

### 3. **Trading Service** (Python - Sapheneia)
- **URL**: `http://sapheneia-trading-service:9000`
- **Endpoint**: `POST /trading/execute`
- **Purpose**: Executes trading strategies (threshold, return, quantile)

### 4. **Orchestrator** (Go - AleutianLocal)
- **URL**: `http://localhost:12210`
- **New Endpoint**: `POST /v1/trading/signal`
- **Purpose**: Orchestrates the entire workflow

## Setup Instructions

### 1. Start the Services

```bash
cd /Users/jin/GolandProjects/AleutianLocal
podman-compose -f podman-compose.yml -f podman-compose.override.yml up -d
```

### 2. Verify Services are Running

```bash
# Check orchestrator
curl http://localhost:12210/health

# Check InfluxDB
curl http://localhost:12130/health

# Check data fetcher
curl http://localhost:12131/health

# Check forecast service
curl http://localhost:12129/health

# Check trading service
curl http://localhost:12132/health
```

### 3. Load Historical Data

First, ensure you have historical data in InfluxDB:

```bash
curl -X POST http://localhost:12210/v1/data/fetch \
  -H "Content-Type: application/json" \
  -d '{
    "names": ["SPY", "AAPL", "TSLA"],
    "start_date": "2023-01-01",
    "interval": "1d"
  }'
```

## Usage Examples

### Example 1: Simple Threshold Strategy

Generate a trading signal using a threshold-based strategy:

```bash
curl -X POST http://localhost:12210/v1/trading/signal \
  -H "Content-Type: application/json" \
  -d '{
    "ticker": "SPY",
    "forecast_price": 580.0,
    "current_position": 100.0,
    "available_cash": 50000.0,
    "initial_capital": 100000.0,
    "strategy_type": "threshold",
    "strategy_params": {
      "threshold_type": "absolute",
      "threshold_value": 5.0,
      "execution_size": 10.0
    },
    "history_days": 252
  }'
```

**Response:**
```json
{
  "action": "buy",
  "size": 10.0,
  "value": 5750.0,
  "reason": "Forecast 580.00 > Price 575.00, magnitude 5.0000 > threshold 5.0000",
  "available_cash": 44250.0,
  "position_after": 110.0,
  "stopped": false,
  "current_price": 575.0,
  "forecast_price": 580.0,
  "ticker": "SPY"
}
```

### Example 2: Return-Based Strategy with Proportional Sizing

```bash
curl -X POST http://localhost:12210/v1/trading/signal \
  -H "Content-Type: application/json" \
  -d '{
    "ticker": "AAPL",
    "forecast_price": 195.0,
    "current_position": 50.0,
    "available_cash": 30000.0,
    "initial_capital": 50000.0,
    "strategy_type": "return",
    "strategy_params": {
      "position_sizing": "proportional",
      "threshold_value": 0.02,
      "execution_size": 5.0,
      "max_position_size": 20.0,
      "min_position_size": 1.0
    },
    "history_days": 120
  }'
```

### Example 3: Quantile Strategy

```bash
curl -X POST http://localhost:12210/v1/trading/signal \
  -H "Content-Type: application/json" \
  -d '{
    "ticker": "TSLA",
    "forecast_price": 280.0,
    "current_position": 20.0,
    "available_cash": 25000.0,
    "initial_capital": 50000.0,
    "strategy_type": "quantile",
    "strategy_params": {
      "which_history": "close",
      "window_history": 60,
      "quantile_signals": {
        "1": {
          "range": [0, 10],
          "signal": "sell",
          "multiplier": 0.5
        },
        "2": {
          "range": [90, 100],
          "signal": "buy",
          "multiplier": 0.75
        }
      },
      "position_sizing": "fixed",
      "execution_size": 5.0
    },
    "history_days": 180
  }'
```

### Example 4: Complete Workflow with Forecast

If you want to generate a forecast first, then use it for trading:

**Step 1: Generate Forecast**
```bash
FORECAST=$(curl -X POST http://localhost:12210/v1/timeseries/forecast \
  -H "Content-Type: application/json" \
  -d '{
    "name": "SPY",
    "context_period_size": 252,
    "forecast_period_size": 20,
    "model": "google/timesfm-2.0-500m-pytorch"
  }' | jq '.forecast[0]')

echo "Forecast: $FORECAST"
```

**Step 2: Get Trading Signal**
```bash
curl -X POST http://localhost:12210/v1/trading/signal \
  -H "Content-Type: application/json" \
  -d "{
    \"ticker\": \"SPY\",
    \"forecast_price\": $FORECAST,
    \"current_position\": 100.0,
    \"available_cash\": 50000.0,
    \"initial_capital\": 100000.0,
    \"strategy_type\": \"threshold\",
    \"strategy_params\": {
      \"threshold_type\": \"percentage\",
      \"threshold_value\": 2.0,
      \"execution_size\": 10.0
    },
    \"history_days\": 252
  }"
```

## API Reference

### POST /v1/trading/signal

Generates a trading signal by orchestrating OHLC data fetching and strategy execution.

#### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ticker` | string | Yes | Stock ticker symbol (e.g., "SPY", "AAPL") |
| `forecast_price` | float | Yes | Forecasted price (must be > 0) |
| `current_price` | float | No | Current price (fetched from InfluxDB if not provided) |
| `current_position` | float | Yes | Current position size (shares, must be >= 0) |
| `available_cash` | float | Yes | Available cash (must be >= 0) |
| `initial_capital` | float | Yes | Initial capital (must be > 0) |
| `strategy_type` | string | Yes | Strategy type: "threshold", "return", or "quantile" |
| `strategy_params` | object | Yes | Strategy-specific parameters |
| `history_days` | int | No | Days of historical data to fetch (default: 252) |

#### Strategy Parameters

**Threshold Strategy:**
```json
{
  "threshold_type": "absolute|percentage|std_dev|atr",
  "threshold_value": 5.0,
  "execution_size": 10.0,
  "which_history": "close",
  "window_history": 20
}
```

**Return Strategy:**
```json
{
  "position_sizing": "fixed|proportional|normalized",
  "threshold_value": 0.05,
  "execution_size": 10.0,
  "max_position_size": 100.0,
  "min_position_size": 1.0
}
```

**Quantile Strategy:**
```json
{
  "which_history": "close",
  "window_history": 60,
  "quantile_signals": {
    "1": {"range": [0, 10], "signal": "sell", "multiplier": 0.5},
    "2": {"range": [90, 100], "signal": "buy", "multiplier": 0.75}
  },
  "position_sizing": "fixed",
  "execution_size": 5.0
}
```

#### Response

```json
{
  "action": "buy|sell|hold",
  "size": 10.0,
  "value": 5750.0,
  "reason": "Explanation of the trading decision",
  "available_cash": 44250.0,
  "position_after": 110.0,
  "stopped": false,
  "current_price": 575.0,
  "forecast_price": 580.0,
  "ticker": "SPY"
}
```

## Environment Variables

Configure these in `podman-compose.override.yml`:

```yaml
orchestrator:
  environment:
    # Service URLs
    ALEUTIAN_TIMESERIES_TOOL: http://finance-analysis-service:8000
    ALEUTIAN_DATA_FETCHER_URL: http://finance-data-service:8000
    SAPHENEIA_TRADING_SERVICE_URL: http://sapheneia-trading-service:9000

    # Trading API Key
    SAPHENEIA_TRADING_API_KEY: your_secure_api_key_here

    # InfluxDB Configuration
    INFLUXDB_URL: http://influxdb:8086
    INFLUXDB_TOKEN: your_super_secret_admin_token
    INFLUXDB_ORG: aleutian-finance
    INFLUXDB_BUCKET: financial-data
```

## Troubleshooting

### Issue: "No historical data found"

**Solution**: Make sure you've loaded data into InfluxDB first:
```bash
curl -X POST http://localhost:12210/v1/data/fetch \
  -H "Content-Type: application/json" \
  -d '{
    "names": ["YOUR_TICKER"],
    "start_date": "2023-01-01",
    "interval": "1d"
  }'
```

### Issue: "Failed to connect to trading service"

**Solution**: Check if the trading service is running:
```bash
curl http://localhost:12132/health
```

If not running, restart the services:
```bash
podman-compose -f podman-compose.yml -f podman-compose.override.yml restart sapheneia-trading-service
```

### Issue: "InfluxDB configuration not set"

**Solution**: Ensure all InfluxDB environment variables are set in the orchestrator configuration.

## Performance Considerations

1. **Historical Data Window**: The `history_days` parameter affects both:
   - Time to fetch data from InfluxDB
   - Memory usage for OHLC arrays

   Recommended: 252 days (1 year) for most strategies

2. **Strategy Complexity**:
   - Threshold: Fastest (simple comparison)
   - Return: Medium (return calculations)
   - Quantile: Slowest (percentile calculations)

3. **Caching**: Consider implementing caching for frequently-requested tickers to reduce InfluxDB queries.

## Security Best Practices

1. **Change Default API Keys**: Always change `SAPHENEIA_TRADING_API_KEY` from default value
2. **Use Secrets**: In production, use Docker/Podman secrets instead of environment variables
3. **Network Isolation**: Keep services on internal network, only expose orchestrator
4. **Rate Limiting**: The trading service has built-in rate limiting (configurable)

# ==========================================
#  Sapheneia & Aleutian Integration Testing
# ==========================================

# --- 1. Sapheneia Direct API Testing (Port 8100) ---
# Use these commands to verify the Forecast Service is healthy and configured correctly.

# 1.1 Check Health (No Auth)
curl http://localhost:8100/health

# 1.2 Initialize Model (Chronos T5 Tiny)
# Note: Uses the API Key defined in docker-compose.yml
curl -X POST http://localhost:8100/forecast/v1/chronos/initialization \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer default_trading_api_key_please_change" \
  -d '{
    "model_variant": "amazon/chronos-t5-tiny",
    "device": "cpu"
  }'

# 1.3 Check Status
curl http://localhost:8100/forecast/v1/chronos/status \
  -H "Authorization: Bearer default_trading_api_key_please_change"

# 1.4 Run Inference (Direct)
curl -X POST http://localhost:8100/forecast/v1/chronos/inference \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer default_trading_api_key_please_change" \
  -d '{
    "context": [1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0],
    "prediction_length": 5,
    "num_samples": 20
  }'

# --- 2. Aleutian Orchestrator Testing (Port 12210) ---
# Use these commands to test the full pipeline via the Go CLI.

# 2.1 Fetch Historical Data (Populate InfluxDB)
# This step is required before running an evaluation.
./aleutian timeseries fetch SPY --days 1000

# 2.2 Run Single-Point Forecast (Test Connectivity)
./aleutian timeseries forecast SPY \
  --model "amazon/chronos-t5-tiny" \
  --context 50 \
  --horizon 10

# 2.3 Run Full Backtest Evaluation (Scenario)
# Requires a valid config file at strategies/spy_threshold_v1.yaml
./aleutian evaluate run --config strategies/spy_threshold_v1.yaml



## Next Steps

- Implement backtesting endpoint
- Add portfolio optimization strategies
- Create streaming WebSocket endpoint for real-time signals
- Integrate risk management constraints

## Support

For issues or questions:
- Sapheneia Trading: `/Users/jin/PycharmProjects/sapheneia/`
- Orchestrator: `/Users/jin/GolandProjects/AleutianLocal/`
- Data Services: `/Users/jin/GolandProjects/SapheneiaAleutianMLOps/`
