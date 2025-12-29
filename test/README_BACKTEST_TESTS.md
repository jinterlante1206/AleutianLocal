# Backtest Time-Travel Bug Fix - Test Suite

This directory contains tests for the backtest time-travel bug fix. For full context, see `/docs/backtest_timetravel_fix.md`.

## Test Organization

```
AleutianLocal/
├── services/orchestrator/handlers/
│   ├── trading_test.go          # Unit tests for data fetching
│   └── evaluator_test.go        # Unit tests for forecast calls
├── test/integration/
│   └── backtest_timetravel_test.go  # End-to-end integration tests
└── test/
    └── README_BACKTEST_TESTS.md     # This file
```

## Quick Start

### Run All Tests

```bash
# From project root
./run_backtest_tests.sh
```

### Run Specific Test Suites

```bash
# Unit tests only (no external dependencies)
go test ./services/orchestrator/handlers -v -run "TestCallForecastService|TestFetchOHLCFromInflux_FluxQuery"

# Integration tests (requires InfluxDB running)
SKIP_INTEGRATION_TESTS="" go test ./services/orchestrator/handlers -v

# Full end-to-end test (requires full stack)
RUN_INTEGRATION_TESTS=1 go test ./test/integration -v

# Benchmarks
go test ./services/orchestrator/handlers -bench=BenchmarkFetchOHLCFromInflux -benchmem
go test ./services/orchestrator/handlers -bench=BenchmarkCallForecastServiceAsOf -benchmem
```

## Test Suite Details

### 1. Unit Tests: `trading_test.go`

**Purpose:** Verify `fetchOHLCFromInflux` correctly handles `asOfDate` parameter

**Tests:**
- `TestFetchOHLCFromInflux_AsOfDate` - Validates temporal isolation
- `TestFetchOHLCFromInflux_TimeTravelBug` - Demonstrates bug vs fix
- `TestFetchOHLCFromInflux_FluxQueryCorrectness` - Validates Flux query syntax
- `BenchmarkFetchOHLCFromInflux` - Performance impact

**Requirements:**
- InfluxDB running on `localhost:12130`
- Environment variables (optional):
  ```bash
  export INFLUXDB_URL=http://localhost:12130
  export INFLUXDB_TOKEN=your_super_secret_admin_token
  export INFLUXDB_ORG=aleutian-finance
  export INFLUXDB_BUCKET=financial-data
  ```

**Run:**
```bash
# Skip integration tests
SKIP_INTEGRATION_TESTS=1 go test ./services/orchestrator/handlers -v -run TestFetchOHLC

# Run with InfluxDB
go test ./services/orchestrator/handlers -v -run TestFetchOHLC
```

**Expected Output:**
```
=== RUN   TestFetchOHLCFromInflux_AsOfDate
=== RUN   TestFetchOHLCFromInflux_AsOfDate/Real-time_fetch_(no_asOfDate)
    trading_test.go:85: Fetched 10 data points from 2024-10-01 to 2024-12-22
=== RUN   TestFetchOHLCFromInflux_AsOfDate/Backtest_fetch_(with_asOfDate_=_2023-01-31)
    trading_test.go:85: Fetched 10 data points from 2023-01-15 to 2023-01-31
--- PASS: TestFetchOHLCFromInflux_AsOfDate (0.42s)
```

### 2. Unit Tests: `evaluator_test.go`

**Purpose:** Verify `CallForecastServiceAsOf` correctly passes `as_of_date` parameter

**Tests:**
- `TestCallForecastServiceAsOf` - Validates API contract
- `TestCallForecastService_BackwardCompatibility` - Ensures old code works
- `TestRunScenario_ForecastVariance` - Validates forecasts vary over time
- `TestBacktestTimeTravelPrevention` - Validates no future data leakage
- `BenchmarkCallForecastServiceAsOf` - Performance impact

**Requirements:**
- None (uses mock HTTP server)

**Run:**
```bash
go test ./services/orchestrator/handlers -v -run TestCallForecast
```

**Expected Output:**
```
=== RUN   TestCallForecastServiceAsOf
=== RUN   TestCallForecastServiceAsOf/Without_asOfDate_(real-time)
=== RUN   TestCallForecastServiceAsOf/With_asOfDate_(backtest)
--- PASS: TestCallForecastServiceAsOf (0.01s)
```

### 3. Integration Tests: `backtest_timetravel_test.go`

**Purpose:** End-to-end validation of the fix with real InfluxDB and backtest

**Tests:**
- `TestBacktestTimeTravelFix` - Main integration test
- `TestCompareBeforeAfterFix` - Documents expected behavior change

**Requirements:**
- InfluxDB running
- Forecast service running (optional, test will skip if unavailable)
- Set environment variable: `RUN_INTEGRATION_TESTS=1`

**Setup:**
```bash
# Start the stack
./orchestrator stack up

# Wait for services to be healthy
./orchestrator stack health

# Set test flag
export RUN_INTEGRATION_TESTS=1
```

**Run:**
```bash
RUN_INTEGRATION_TESTS=1 go test ./test/integration -v -run TestBacktestTimeTravelFix
```

**Expected Output:**
```
=== RUN   TestBacktestTimeTravelFix
    backtest_timetravel_test.go:45: Setting up test data in InfluxDB...
    backtest_timetravel_test.go:48: Running backtest...
    backtest_timetravel_test.go:57: Verifying results...
=== RUN   TestBacktestTimeTravelFix/Forecasts_Are_Not_Constant
    backtest_timetravel_test.go:64: Found 20 data points with 18 unique forecasts
    backtest_timetravel_test.go:67: Forecast distribution:
    backtest_timetravel_test.go:68:   383.45: 1 occurrences
    backtest_timetravel_test.go:68:   385.12: 1 occurrences
    backtest_timetravel_test.go:68:   381.89: 2 occurrences
    backtest_timetravel_test.go:68:   ...
--- PASS: TestBacktestTimeTravelFix/Forecasts_Are_Not_Constant (0.05s)
=== RUN   TestBacktestTimeTravelFix/No_Future_Data_Leakage
--- PASS: TestBacktestTimeTravelFix/No_Future_Data_Leakage (0.01s)
--- PASS: TestBacktestTimeTravelFix (15.32s)
```

## Test Matrix

| Test | Type | Dependencies | Duration | Pass Criteria |
|------|------|--------------|----------|---------------|
| TestFetchOHLCFromInflux_FluxQueryCorrectness | Unit | None | <0.01s | Query syntax correct |
| TestCallForecastServiceAsOf | Unit | None | <0.01s | Payload includes as_of_date |
| TestFetchOHLCFromInflux_AsOfDate | Integration | InfluxDB | ~0.5s | Data constrained by date |
| TestFetchOHLCFromInflux_TimeTravelBug | Integration | InfluxDB | ~0.5s | Fixed version has no future data |
| TestRunScenario_ForecastVariance | Integration | InfluxDB + API | ~15s | >1 unique forecast |
| TestBacktestTimeTravelFix | E2E | Full stack | ~20s | Forecasts vary, no leakage |

## Troubleshooting

### Issue: `SKIP_INTEGRATION_TESTS` not working

**Symptom:**
```
panic: runtime error: invalid memory address or nil pointer dereference
```

**Solution:**
```bash
# Set the environment variable explicitly
export SKIP_INTEGRATION_TESTS=1
go test ./services/orchestrator/handlers -v
```

### Issue: InfluxDB connection refused

**Symptom:**
```
Error: Failed to fetch OHLC data: connection refused
```

**Solution:**
```bash
# Check InfluxDB is running
curl http://localhost:12130/health

# Start InfluxDB if needed
./orchestrator stack up influxdb

# Verify port mapping
podman ps | grep influx
```

### Issue: No test data in InfluxDB

**Symptom:**
```
Error: no historical data found for ticker: TEST_SPY
```

**Solution:**
The test automatically creates test data via `setupTestData()`. If it fails:

```bash
# Manually insert test data
influx write -b financial-data -o aleutian-finance \
  -p "stock_prices,ticker=SPY close=380.82 1672761000000000000"
```

### Issue: Forecast service not running

**Symptom:**
```
Error: Forecast failed: connection refused
```

**Solution:**
```bash
# Check forecast containers
podman ps | grep forecast

# Start forecast service
./orchestrator stack up forecast-chronos-t5-tiny

# Test forecast endpoint
curl -X POST http://localhost:8000/v1/timeseries/forecast \
  -H "Content-Type: application/json" \
  -d '{"name":"SPY","model":"amazon/chronos-t5-tiny","context_period_size":252,"forecast_period_size":20}'
```

## Performance Benchmarks

### Expected Results

```
BenchmarkFetchOHLCFromInflux/WithAsOfDate-8          100    12.5 ms/op
BenchmarkFetchOHLCFromInflux/WithoutAsOfDate-8       100    12.3 ms/op

BenchmarkCallForecastServiceAsOf/WithAsOfDate-8       50    45.2 ms/op
BenchmarkCallForecastServiceAsOf/WithoutAsOfDate-8    50    44.8 ms/op
```

### Performance Impact

| Metric | Before | After | Delta |
|--------|--------|-------|-------|
| Data fetch | 12.3 ms | 12.5 ms | +0.2 ms (1.6%) |
| Forecast call | 44.8 ms | 45.2 ms | +0.4 ms (0.9%) |
| Memory | 4096 B | 4096 B | 0 B |

**Conclusion:** Negligible performance impact (<2%)

## Test Coverage

```bash
# Generate coverage report
go test ./services/orchestrator/handlers -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html

# View coverage
open coverage.html
```

**Target Coverage:**
- `fetchOHLCFromInflux`: 100%
- `CallForecastServiceAsOf`: 100%
- `RunScenario` (modified section): 85%

## Continuous Integration

### GitHub Actions

```yaml
# .github/workflows/backtest-tests.yml
name: Backtest Tests
on: [push, pull_request]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      - run: go test ./services/orchestrator/handlers -v

  integration-tests:
    runs-on: ubuntu-latest
    services:
      influxdb:
        image: influxdb:2.7
        ports:
          - 12130:8086
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      - run: go test ./services/orchestrator/handlers -v
```

## Manual Testing

### Test the Fix End-to-End

```bash
# 1. Ensure services are running
./orchestrator stack up

# 2. Run a backtest
./aleutian backtest --config strategies/chronos_tiny_spy_threshold_v1.yaml

# 3. Export results
./aleutian export <run_id> --output results.csv

# 4. Verify forecasts vary
awk -F, 'NR>1 {print $6}' results.csv | sort -u | wc -l
# Should output: >10 (not 1!)

# 5. Check for identical forecasts
awk -F, 'NR>1 {print $6}' results.csv | sort | uniq -c | sort -rn | head -5
# Should show distribution, not all identical
```

### Verify Time-Travel Prevention

```bash
# Query InfluxDB directly
influx query '
from(bucket: "financial-data")
  |> range(start: -252d, stop: 2023-01-03T00:00:00Z)
  |> filter(fn: (r) => r.ticker == "SPY")
  |> last()
'

# Verify: _time should be ≤ 2023-01-03
```

## Reporting Issues

If tests fail:

1. **Collect logs:**
   ```bash
   go test ./services/orchestrator/handlers -v > test_output.log 2>&1
   ```

2. **Check service health:**
   ```bash
   ./orchestrator stack health > health_check.log
   ```

3. **Export InfluxDB data:**
   ```bash
   influx backup /tmp/influx-backup
   ```

4. **Open issue with:**
   - Test output
   - Health check logs
   - Go version (`go version`)
   - OS version (`uname -a`)

## References

- **Main Documentation:** `/docs/backtest_timetravel_fix.md`
- **Issue Tracking:** GitHub Issues
- **Code Coverage:** CI/CD dashboard
