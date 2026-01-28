#!/bin/bash
# =============================================================================
# Quick Start Script for Aleutian + Sapheneia Stack
# =============================================================================
# Use this script after initial setup to start/restart all services.
#
# Usage:
#   ./scripts/start-sapheneia-stack.sh [--rebuild]
#
# Options:
#   --rebuild    Force rebuild of containers
# =============================================================================

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
PROJECTS_DIR="${PROJECTS_DIR:-$HOME/projects}"
REBUILD=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --rebuild) REBUILD=true; shift ;;
        *) shift ;;
    esac
done

echo -e "${BLUE}Starting Aleutian + Sapheneia Stack...${NC}"
echo ""

# Ensure environment is set
export ORCHESTRATOR_URL="${ORCHESTRATOR_URL:-http://localhost:12700}"
export SAPHENEIA_API_KEY="${SAPHENEIA_API_KEY:-default_trading_api_key_please_change}"

# 1. Start Sapheneia
echo -e "${BLUE}[1/4] Starting Sapheneia services...${NC}"
cd "$PROJECTS_DIR/sapheneia"

if [ "$REBUILD" = true ]; then
    podman-compose build
fi

podman-compose up -d forecast forecast-chronos-t5-tiny trading data
sleep 5

# 2. Start AleutianFOSS
echo -e "${BLUE}[2/4] Starting AleutianFOSS services...${NC}"
cd "$PROJECTS_DIR/AleutianFOSS"

if [ "$REBUILD" = true ]; then
    go build -o aleutian ./cmd/aleutian
    sudo cp aleutian /usr/local/bin/
    podman-compose build orchestrator
fi

aleutian stack start --forecast-mode sapheneia --skip-model-check

# 3. Reconnect shared services
echo -e "${BLUE}[3/4] Reconnecting services...${NC}"
sleep 5
cd "$PROJECTS_DIR/sapheneia"
podman restart sapheneia-data 2>/dev/null || true

# 4. Health check
echo -e "${BLUE}[4/4] Checking service health...${NC}"
sleep 10

check_health() {
    local url=$1
    local name=$2
    if curl -s "$url" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ $name${NC}"
        return 0
    else
        echo -e "${YELLOW}⚠ $name (may still be starting)${NC}"
        return 1
    fi
}

echo ""
check_health "http://localhost:12700/health" "Sapheneia Forecast (12700)"
check_health "http://localhost:12710/health" "Chronos Service (12710)"
check_health "http://localhost:12130/health" "InfluxDB (12130)"

echo ""
echo -e "${GREEN}Stack is running!${NC}"
echo ""
echo "Quick commands:"
echo "  aleutian evaluate run --config strategies/spy_threshold_v1.yaml --api-version unified"
echo "  podman ps --format 'table {{.Names}}\t{{.Status}}'"
