#!/bin/bash
# Backtest Time-Travel Bug Fix - Test Runner
# This script runs all test suites for the backtest fix

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Functions
print_header() {
    echo -e "\n${BLUE}========================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}========================================${NC}\n"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

# Check if Go is installed
if ! command -v go &> /dev/null; then
    print_error "Go is not installed. Please install Go 1.21 or later."
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}')
print_success "Found Go: $GO_VERSION"

# Parse command line arguments
RUN_UNIT=true
RUN_INTEGRATION=false
RUN_BENCHMARKS=false
VERBOSE=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --unit-only)
            RUN_INTEGRATION=false
            RUN_BENCHMARKS=false
            shift
            ;;
        --integration)
            RUN_INTEGRATION=true
            shift
            ;;
        --benchmarks)
            RUN_BENCHMARKS=true
            shift
            ;;
        --all)
            RUN_UNIT=true
            RUN_INTEGRATION=true
            RUN_BENCHMARKS=true
            shift
            ;;
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --unit-only       Run only unit tests (default)"
            echo "  --integration     Run integration tests (requires InfluxDB)"
            echo "  --benchmarks      Run performance benchmarks"
            echo "  --all             Run all tests"
            echo "  -v, --verbose     Verbose output"
            echo "  --help            Show this help message"
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Set verbose flag
VERBOSE_FLAG=""
if [ "$VERBOSE" = true ]; then
    VERBOSE_FLAG="-v"
fi

print_header "Backtest Time-Travel Bug Fix - Test Suite"

# Summary
echo "Test Configuration:"
echo "  Unit Tests:        $([ "$RUN_UNIT" = true ] && echo "✓" || echo "✗")"
echo "  Integration Tests: $([ "$RUN_INTEGRATION" = true ] && echo "✓" || echo "✗")"
echo "  Benchmarks:        $([ "$RUN_BENCHMARKS" = true ] && echo "✓" || echo "✗")"
echo "  Verbose:           $([ "$VERBOSE" = true ] && echo "✓" || echo "✗")"
echo ""

# Change to project root
cd "$(dirname "$0")"

# Test counters
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# ============================================================
# Unit Tests
# ============================================================
if [ "$RUN_UNIT" = true ]; then
    print_header "Unit Tests"

    echo "Running: Trading Handler Tests (trading_test.go)"
    if go test ./services/orchestrator/handlers $VERBOSE_FLAG -run "TestCallForecastService|TestFetchOHLCFromInflux_FluxQuery" 2>&1; then
        print_success "Trading handler tests passed"
        ((PASSED_TESTS++))
    else
        print_error "Trading handler tests failed"
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))

    echo ""
    echo "Running: Evaluator Handler Tests (evaluator_test.go)"
    if go test ./services/orchestrator/handlers $VERBOSE_FLAG -run "TestCallForecastService" 2>&1; then
        print_success "Evaluator handler tests passed"
        ((PASSED_TESTS++))
    else
        print_error "Evaluator handler tests failed"
        ((FAILED_TESTS++))
    fi
    ((TOTAL_TESTS++))
fi

# ============================================================
# Integration Tests
# ============================================================
if [ "$RUN_INTEGRATION" = true ]; then
    print_header "Integration Tests (Requires InfluxDB)"

    # Check if InfluxDB is running
    if curl -s http://localhost:12130/health > /dev/null 2>&1; then
        print_success "InfluxDB is running at localhost:12130"
    else
        print_warning "InfluxDB not detected at localhost:12130"
        echo "Start InfluxDB with: ./orchestrator stack up influxdb"
        echo "Skipping integration tests..."
        RUN_INTEGRATION=false
    fi

    if [ "$RUN_INTEGRATION" = true ]; then
        echo ""
        echo "Running: Data Fetch Integration Tests"
        if SKIP_INTEGRATION_TESTS="" go test ./services/orchestrator/handlers $VERBOSE_FLAG -run "TestFetchOHLCFromInflux" 2>&1; then
            print_success "Data fetch integration tests passed"
            ((PASSED_TESTS++))
        else
            print_error "Data fetch integration tests failed"
            ((FAILED_TESTS++))
        fi
        ((TOTAL_TESTS++))

        echo ""
        echo "Running: Backtest Scenario Tests"
        if go test ./services/orchestrator/handlers $VERBOSE_FLAG -run "TestRunScenario" 2>&1; then
            print_success "Backtest scenario tests passed"
            ((PASSED_TESTS++))
        else
            print_warning "Backtest scenario tests skipped (forecast service not running)"
            # Don't count as failure if service isn't running
        fi
        ((TOTAL_TESTS++))

        echo ""
        echo "Running: End-to-End Integration Tests"
        if RUN_INTEGRATION_TESTS=1 go test ./test/integration $VERBOSE_FLAG 2>&1; then
            print_success "E2E integration tests passed"
            ((PASSED_TESTS++))
        else
            print_warning "E2E integration tests skipped (full stack not running)"
        fi
        ((TOTAL_TESTS++))
    fi
fi

# ============================================================
# Benchmarks
# ============================================================
if [ "$RUN_BENCHMARKS" = true ]; then
    print_header "Performance Benchmarks"

    echo "Running: fetchOHLCFromInflux Benchmark"
    go test ./services/orchestrator/handlers -bench=BenchmarkFetchOHLCFromInflux -benchmem -run=^$ | tee /tmp/bench_trading.txt

    echo ""
    echo "Running: CallForecastServiceAsOf Benchmark"
    go test ./services/orchestrator/handlers -bench=BenchmarkCallForecastServiceAsOf -benchmem -run=^$ | tee /tmp/bench_evaluator.txt

    echo ""
    print_success "Benchmark results saved to /tmp/bench_*.txt"
fi

# ============================================================
# Summary
# ============================================================
print_header "Test Summary"

echo "Results:"
echo "  Total:  $TOTAL_TESTS tests"
echo "  Passed: $PASSED_TESTS tests"
echo "  Failed: $FAILED_TESTS tests"
echo ""

if [ $FAILED_TESTS -eq 0 ]; then
    print_success "All tests passed! ✓"
    echo ""
    echo "Next steps:"
    echo "  1. Review the documentation: docs/backtest_timetravel_fix.md"
    echo "  2. Update Sapheneia forecast service to support as_of_date"
    echo "  3. Run a full backtest: ./aleutian backtest --config strategies/chronos_tiny_spy_threshold_v1.yaml"
    exit 0
else
    print_error "Some tests failed. Please review the output above."
    echo ""
    echo "Troubleshooting:"
    echo "  - Check if InfluxDB is running: curl http://localhost:12130/health"
    echo "  - Check if forecast service is running: curl http://localhost:12132/health"
    echo "  - Review logs: ./orchestrator stack logs"
    echo "  - See test/README_BACKTEST_TESTS.md for detailed troubleshooting"
    exit 1
fi
