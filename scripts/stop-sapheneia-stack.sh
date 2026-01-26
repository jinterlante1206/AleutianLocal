#!/bin/bash
# =============================================================================
# Stop Script for Aleutian + Sapheneia Stack
# =============================================================================
# Gracefully stops all services.
#
# Usage:
#   ./scripts/stop-sapheneia-stack.sh
# =============================================================================

set -e

PROJECTS_DIR="${PROJECTS_DIR:-$HOME/projects}"

echo "Stopping Aleutian + Sapheneia Stack..."

# Stop AleutianFOSS
echo "[1/2] Stopping AleutianFOSS..."
cd "$PROJECTS_DIR/AleutianFOSS"
aleutian stack stop 2>/dev/null || podman-compose down

# Stop Sapheneia
echo "[2/2] Stopping Sapheneia..."
cd "$PROJECTS_DIR/sapheneia"
podman-compose down

echo ""
echo "All services stopped."
echo "Run './scripts/start-sapheneia-stack.sh' to start again."
