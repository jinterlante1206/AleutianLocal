#!/bin/bash
# Aleutian Model Compatibility Test Script
# Run on Ubuntu RTX 5090 server to verify model support
#
# Usage:
#   ./scripts/test_models.sh                    # Test all models
#   ./scripts/test_models.sh chronos-t5-tiny    # Test specific model
#   ./scripts/test_models.sh --list             # List available models

set -e

# Configuration
FORECAST_URL="${FORECAST_URL:-http://localhost:8000}"
RESULTS_FILE="model_test_results.json"

# Test data (synthetic OHLC-like data)
TEST_DATA="[100.5, 101.2, 99.8, 102.3, 101.5, 103.1, 102.8, 104.2, 103.5, 105.0, 104.8, 106.2, 105.5, 107.1, 106.8]"

# Models to test (in priority order)
MODELS=(
    # Verified (sanity check)
    "chronos-t5-tiny"

    # Priority for testing
    "timesfm-1-0"
    "moirai-1-1-small"
    "moment-small"
    "lag-llama"
    "granite-ttm-r1"
)

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${NC}[INFO] $1${NC}"
}

log_success() {
    echo -e "${GREEN}[PASS] $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}[WARN] $1${NC}"
}

log_error() {
    echo -e "${RED}[FAIL] $1${NC}"
}

# Check if forecast service is running
check_service() {
    log_info "Checking forecast service at $FORECAST_URL..."
    if curl -sf "$FORECAST_URL/health" > /dev/null 2>&1; then
        log_success "Forecast service is healthy"
        return 0
    else
        log_error "Forecast service is not reachable at $FORECAST_URL"
        return 1
    fi
}

# List available models
list_models() {
    log_info "Fetching available models..."
    curl -s "$FORECAST_URL/v1/models" | jq '.'
}

# Test a single model
test_model() {
    local model=$1
    log_info "Testing model: $model"

    # Capture start time
    local start_time=$(date +%s.%N)

    # Make forecast request
    local response=$(curl -s -w "\n%{http_code}" -X POST "$FORECAST_URL/v1/timeseries/forecast" \
        -H "Content-Type: application/json" \
        -d "{\"model\": \"$model\", \"data\": $TEST_DATA, \"horizon\": 5}")

    # Extract HTTP status code (last line) and body (everything else)
    local http_code=$(echo "$response" | tail -n1)
    local body=$(echo "$response" | sed '$d')

    # Capture end time
    local end_time=$(date +%s.%N)
    local duration=$(echo "$end_time - $start_time" | bc)

    # Check result
    if [ "$http_code" -eq 200 ]; then
        # Validate response has forecast array
        if echo "$body" | jq -e '.forecast' > /dev/null 2>&1; then
            local forecast_len=$(echo "$body" | jq '.forecast | length')
            log_success "$model: OK (${duration}s, $forecast_len points)"

            # Check VRAM usage
            if command -v nvidia-smi &> /dev/null; then
                local vram=$(nvidia-smi --query-gpu=memory.used --format=csv,noheader,nounits | head -1)
                log_info "  VRAM usage: ${vram} MB"
            fi

            echo "{\"model\": \"$model\", \"status\": \"verified\", \"duration_s\": $duration, \"http_code\": $http_code}" >> "$RESULTS_FILE.tmp"
            return 0
        else
            log_error "$model: Invalid response format"
            echo "{\"model\": \"$model\", \"status\": \"broken\", \"error\": \"invalid_response\", \"http_code\": $http_code}" >> "$RESULTS_FILE.tmp"
            return 1
        fi
    elif [ "$http_code" -eq 400 ]; then
        local error=$(echo "$body" | jq -r '.detail // .error // "unknown"')
        if [[ "$error" == *"broken"* ]]; then
            log_warning "$model: Marked as broken"
            echo "{\"model\": \"$model\", \"status\": \"broken\", \"error\": \"$error\", \"http_code\": $http_code}" >> "$RESULTS_FILE.tmp"
        else
            log_error "$model: Bad request - $error"
            echo "{\"model\": \"$model\", \"status\": \"error\", \"error\": \"$error\", \"http_code\": $http_code}" >> "$RESULTS_FILE.tmp"
        fi
        return 1
    elif [ "$http_code" -eq 501 ]; then
        log_warning "$model: Not implemented"
        echo "{\"model\": \"$model\", \"status\": \"unimplemented\", \"http_code\": $http_code}" >> "$RESULTS_FILE.tmp"
        return 1
    else
        log_error "$model: HTTP $http_code"
        echo "{\"model\": \"$model\", \"status\": \"error\", \"http_code\": $http_code}" >> "$RESULTS_FILE.tmp"
        return 1
    fi
}

# Run tests
run_tests() {
    local models_to_test=("$@")

    # Initialize results file
    rm -f "$RESULTS_FILE.tmp"
    echo "[" > "$RESULTS_FILE.tmp"

    local passed=0
    local failed=0
    local total=${#models_to_test[@]}

    for model in "${models_to_test[@]}"; do
        if test_model "$model"; then
            ((passed++))
        else
            ((failed++))
        fi
        echo "," >> "$RESULTS_FILE.tmp"
    done

    # Finalize JSON (remove trailing comma)
    sed -i '$ s/,$//' "$RESULTS_FILE.tmp" 2>/dev/null || sed -i '' '$ s/,$//' "$RESULTS_FILE.tmp"
    echo "]" >> "$RESULTS_FILE.tmp"
    mv "$RESULTS_FILE.tmp" "$RESULTS_FILE"

    # Summary
    echo ""
    echo "=========================================="
    echo "Test Summary"
    echo "=========================================="
    echo -e "Total:  $total"
    echo -e "${GREEN}Passed: $passed${NC}"
    echo -e "${RED}Failed: $failed${NC}"
    echo ""
    echo "Results saved to: $RESULTS_FILE"

    # Show system info
    if command -v nvidia-smi &> /dev/null; then
        echo ""
        echo "GPU Info:"
        nvidia-smi --query-gpu=name,memory.total,memory.used,memory.free --format=csv
    fi
}

# Main
main() {
    echo "=========================================="
    echo "Aleutian Model Compatibility Test"
    echo "=========================================="
    echo ""

    # Handle arguments
    if [ "$1" == "--list" ]; then
        check_service && list_models
        exit 0
    fi

    # Check service first
    if ! check_service; then
        echo ""
        echo "Please start the forecast service first:"
        echo "  cd services/forecast"
        echo "  podman build -f Dockerfile.gpu -t aleutian-forecast:gpu ."
        echo "  podman run --gpus all -p 8000:8000 aleutian-forecast:gpu"
        exit 1
    fi

    echo ""

    # Determine which models to test
    if [ $# -gt 0 ]; then
        run_tests "$@"
    else
        run_tests "${MODELS[@]}"
    fi
}

main "$@"
