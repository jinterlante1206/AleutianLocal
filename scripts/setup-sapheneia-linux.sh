#!/bin/bash
# =============================================================================
# Aleutian + Sapheneia Integration Setup Script for Linux
# =============================================================================
# This script automates the setup of AleutianFOSS with Sapheneia integration
# on a Linux server (tested on Ubuntu 22.04/24.04).
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/AleutianAI/AleutianFOSS/main/scripts/setup-sapheneia-linux.sh | bash
#   # OR
#   ./scripts/setup-sapheneia-linux.sh
#
# What this script does:
#   1. Checks and installs prerequisites (podman, go, gh)
#   2. Guides through GitHub authentication
#   3. Clones Sapheneia and AleutianFOSS repos
#   4. Configures environment variables (~/.bashrc)
#   5. Sets up Sapheneia services
#   6. Builds and starts AleutianFOSS
#   7. Verifies everything is working
# =============================================================================

set -e  # Exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration
PROJECTS_DIR="${PROJECTS_DIR:-$HOME/projects}"
SAPHENEIA_BRANCH="${SAPHENEIA_BRANCH:-aleutian_merge}"
ALEUTIAN_BRANCH="${ALEUTIAN_BRANCH:-main}"

# =============================================================================
# Helper Functions
# =============================================================================

print_header() {
    echo ""
    echo -e "${CYAN}========================================${NC}"
    echo -e "${CYAN}  $1${NC}"
    echo -e "${CYAN}========================================${NC}"
    echo ""
}

print_step() {
    echo -e "${BLUE}➤ $1${NC}"
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

print_info() {
    echo -e "  $1"
}

check_command() {
    if command -v "$1" &> /dev/null; then
        return 0
    else
        return 1
    fi
}

wait_for_service() {
    local url=$1
    local name=$2
    local max_attempts=${3:-30}
    local attempt=1

    print_step "Waiting for $name to be ready..."
    while [ $attempt -le $max_attempts ]; do
        if curl -s "$url" > /dev/null 2>&1; then
            print_success "$name is ready"
            return 0
        fi
        echo -n "."
        sleep 2
        attempt=$((attempt + 1))
    done
    echo ""
    print_error "$name failed to start after $max_attempts attempts"
    return 1
}

# =============================================================================
# Prerequisite Checks
# =============================================================================

check_prerequisites() {
    print_header "Checking Prerequisites"

    local missing_deps=()

    # Check Git
    if check_command git; then
        print_success "Git installed: $(git --version)"
    else
        missing_deps+=("git")
    fi

    # Check Podman
    if check_command podman; then
        print_success "Podman installed: $(podman --version | head -1)"
    else
        missing_deps+=("podman")
    fi

    # Check podman-compose
    if check_command podman-compose; then
        print_success "podman-compose installed"
    else
        missing_deps+=("podman-compose")
    fi

    # Check Go
    if check_command go; then
        print_success "Go installed: $(go version)"
    else
        missing_deps+=("golang")
    fi

    # Check GitHub CLI
    if check_command gh; then
        print_success "GitHub CLI installed: $(gh --version | head -1)"
    else
        missing_deps+=("gh")
    fi

    # Check NVIDIA GPU (optional)
    if check_command nvidia-smi; then
        print_success "NVIDIA GPU detected"
        nvidia-smi --query-gpu=name,memory.total --format=csv,noheader
        HAS_GPU=true
    else
        print_warning "No NVIDIA GPU detected (will use CPU)"
        HAS_GPU=false
    fi

    # Install missing dependencies
    if [ ${#missing_deps[@]} -gt 0 ]; then
        print_header "Installing Missing Dependencies"

        # Detect package manager
        if check_command apt; then
            print_step "Detected Debian/Ubuntu system"
            sudo apt update

            for dep in "${missing_deps[@]}"; do
                case $dep in
                    podman)
                        print_step "Installing Podman..."
                        sudo apt install -y podman
                        ;;
                    podman-compose)
                        print_step "Installing podman-compose..."
                        sudo apt install -y podman-compose || pip3 install podman-compose
                        ;;
                    golang)
                        print_step "Installing Go..."
                        sudo apt install -y golang-go
                        ;;
                    gh)
                        print_step "Installing GitHub CLI..."
                        sudo apt install -y gh || {
                            # Fallback to official repo
                            curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
                            echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null
                            sudo apt update && sudo apt install -y gh
                        }
                        ;;
                    git)
                        print_step "Installing Git..."
                        sudo apt install -y git
                        ;;
                esac
            done
        elif check_command dnf; then
            print_step "Detected Fedora/RHEL system"
            for dep in "${missing_deps[@]}"; do
                case $dep in
                    podman) sudo dnf install -y podman ;;
                    podman-compose) pip3 install podman-compose ;;
                    golang) sudo dnf install -y golang ;;
                    gh) sudo dnf install -y gh ;;
                    git) sudo dnf install -y git ;;
                esac
            done
        else
            print_error "Unsupported package manager. Please install manually: ${missing_deps[*]}"
            exit 1
        fi

        print_success "Dependencies installed"
    fi
}

# =============================================================================
# GitHub Authentication
# =============================================================================

setup_github_auth() {
    print_header "GitHub Authentication"

    # Check if already authenticated
    if gh auth status &> /dev/null; then
        print_success "Already authenticated with GitHub"
        gh auth status
        return 0
    fi

    print_warning "GitHub authentication required"
    echo ""
    print_info "You need to authenticate with GitHub to clone private repos."
    print_info "The easiest method is device code authentication:"
    echo ""
    echo -e "${YELLOW}Run this command and follow the prompts:${NC}"
    echo ""
    echo "    gh auth login"
    echo ""
    print_info "Select: GitHub.com → HTTPS → Yes (authenticate) → Login with browser"
    echo ""

    read -p "Press Enter after you've completed GitHub authentication..."

    # Verify
    if gh auth status &> /dev/null; then
        print_success "GitHub authentication verified"
    else
        print_error "GitHub authentication failed. Please run 'gh auth login' manually."
        exit 1
    fi
}

# =============================================================================
# Clone Repositories
# =============================================================================

clone_repositories() {
    print_header "Cloning Repositories"

    mkdir -p "$PROJECTS_DIR"
    cd "$PROJECTS_DIR"

    # Clone Sapheneia
    if [ -d "sapheneia" ]; then
        print_success "Sapheneia repo already exists"
        cd sapheneia
        git fetch origin
        git checkout "$SAPHENEIA_BRANCH" || git checkout -b "$SAPHENEIA_BRANCH" "origin/$SAPHENEIA_BRANCH"
        git pull origin "$SAPHENEIA_BRANCH" || true
        cd ..
    else
        print_step "Cloning Sapheneia..."
        gh repo clone Sapheneia/sapheneia
        cd sapheneia
        git checkout "$SAPHENEIA_BRANCH"
        cd ..
    fi

    # Clone AleutianFOSS
    if [ -d "AleutianFOSS" ]; then
        print_success "AleutianFOSS repo already exists"
        cd AleutianFOSS
        git fetch origin
        git checkout "$ALEUTIAN_BRANCH"
        git pull origin "$ALEUTIAN_BRANCH" || true
        cd ..
    else
        print_step "Cloning AleutianFOSS..."
        gh repo clone AleutianAI/AleutianFOSS
        cd AleutianFOSS
        git checkout "$ALEUTIAN_BRANCH"
        cd ..
    fi

    print_success "Repositories cloned"
}

# =============================================================================
# Environment Configuration
# =============================================================================

configure_environment() {
    print_header "Configuring Environment"

    # Create directories
    print_step "Creating required directories..."
    mkdir -p ~/models_cache
    mkdir -p ~/simulations

    # Configure shell environment
    BASHRC="$HOME/.bashrc"
    MARKER="# === Aleutian/Sapheneia Configuration ==="

    if grep -q "$MARKER" "$BASHRC" 2>/dev/null; then
        print_warning "Aleutian configuration already exists in ~/.bashrc"
        print_info "To reconfigure, remove the Aleutian section from ~/.bashrc and re-run this script"
    else
        print_step "Adding environment variables to ~/.bashrc..."

        cat >> "$BASHRC" << 'EOF'

# === Aleutian/Sapheneia Configuration ===
# Added by setup-sapheneia-linux.sh

# Sapheneia Integration
export ORCHESTRATOR_URL=http://localhost:12700
export SAPHENEIA_API_KEY=default_trading_api_key_please_change
export SAPHENEIA_ORCHESTRATION_URL=http://localhost:12700

# InfluxDB (for direct queries)
export INFLUXDB_URL=http://localhost:12130
export INFLUXDB_TOKEN=aleutian-dev-token-2026
export INFLUXDB_ORG=aleutian-finance

# Project paths
export ALEUTIAN_HOME=$HOME/projects/AleutianFOSS
export SAPHENEIA_HOME=$HOME/projects/sapheneia

# Convenience aliases
alias aleutian-eval='aleutian evaluate run --api-version unified'
alias aleutian-status='podman ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"'
alias sapheneia-logs='cd $SAPHENEIA_HOME && podman-compose logs -f'
alias aleutian-logs='cd $ALEUTIAN_HOME && podman-compose logs -f orchestrator'

# === End Aleutian/Sapheneia Configuration ===
EOF

        print_success "Environment variables added to ~/.bashrc"
    fi

    # Source the updated bashrc for this session
    export ORCHESTRATOR_URL=http://localhost:12700
    export SAPHENEIA_API_KEY=default_trading_api_key_please_change
    export SAPHENEIA_ORCHESTRATION_URL=http://localhost:12700
    export INFLUXDB_URL=http://localhost:12130
    export INFLUXDB_TOKEN=aleutian-dev-token-2026
    export INFLUXDB_ORG=aleutian-finance
    export ALEUTIAN_HOME="$PROJECTS_DIR/AleutianFOSS"
    export SAPHENEIA_HOME="$PROJECTS_DIR/sapheneia"

    print_success "Environment configured"
}

# =============================================================================
# Setup Sapheneia
# =============================================================================

setup_sapheneia() {
    print_header "Setting Up Sapheneia"

    cd "$PROJECTS_DIR/sapheneia"

    # Create shared network
    print_step "Creating shared network..."
    podman network create aleutian-shared 2>/dev/null || print_warning "Network already exists"

    # Configure .env
    print_step "Configuring Sapheneia .env..."
    if [ -f .env.template ]; then
        cp .env.template .env
    elif [ ! -f .env ]; then
        print_error ".env.template not found and no .env exists"
        exit 1
    fi

    # Update .env settings
    sed -i "s|MODELS_CACHE_PATH=.*|MODELS_CACHE_PATH=$HOME/models_cache|" .env
    sed -i "s|SIMULATIONS_ROOT=.*|SIMULATIONS_ROOT=$HOME/simulations|" .env
    sed -i "s|INFLUXDB_TOKEN=.*|INFLUXDB_TOKEN=aleutian-dev-token-2026|" .env

    # Enable GPU if available
    if [ "$HAS_GPU" = true ]; then
        print_step "Enabling GPU support..."
        sed -i "s|DEVICE=cpu|DEVICE=cuda:0|" .env
    fi

    # Verify configuration
    print_step "Verifying Sapheneia configuration..."
    grep -E "MODELS_CACHE_PATH|SIMULATIONS_ROOT|DEVICE|INFLUXDB_TOKEN" .env

    # Start Sapheneia services
    print_step "Starting Sapheneia services..."
    podman-compose up -d forecast forecast-chronos-t5-tiny trading data

    # Wait for services
    wait_for_service "http://localhost:12700/health" "Sapheneia Forecast" 60
    wait_for_service "http://localhost:12710/health" "Chronos Service" 60

    print_success "Sapheneia setup complete"
}

# =============================================================================
# Setup AleutianFOSS
# =============================================================================

setup_aleutian() {
    print_header "Setting Up AleutianFOSS"

    cd "$PROJECTS_DIR/AleutianFOSS"

    # Build CLI
    print_step "Building Aleutian CLI..."
    go build -o aleutian ./cmd/aleutian

    # Install to system path
    print_step "Installing Aleutian CLI..."
    sudo cp aleutian /usr/local/bin/

    # Verify installation
    if check_command aleutian; then
        print_success "Aleutian CLI installed: $(aleutian --version 2>/dev/null || echo 'installed')"
    else
        print_error "Aleutian CLI installation failed"
        exit 1
    fi

    # Build orchestrator container
    print_step "Building Aleutian orchestrator..."
    podman-compose build orchestrator

    # Start Aleutian stack
    print_step "Starting Aleutian stack (Sapheneia mode)..."
    aleutian stack start --forecast-mode sapheneia --skip-model-check

    # Wait for orchestrator
    sleep 10

    # Restart sapheneia-data to connect to shared network
    print_step "Reconnecting Sapheneia data service..."
    cd "$PROJECTS_DIR/sapheneia"
    podman restart sapheneia-data 2>/dev/null || true

    print_success "AleutianFOSS setup complete"
}

# =============================================================================
# Initialize Models
# =============================================================================

initialize_models() {
    print_header "Initializing Forecast Models"

    print_step "Initializing Chronos model..."

    local response
    response=$(curl -s -X POST http://localhost:12710/forecast/v1/chronos/initialization \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer default_trading_api_key_please_change" \
        -d '{}')

    if echo "$response" | grep -q "error"; then
        print_warning "Model initialization returned: $response"
    else
        print_success "Chronos model initialized"
    fi

    # Test prediction
    print_step "Testing forecast endpoint..."
    response=$(curl -s -X POST http://localhost:12700/orchestration/v1/predict \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer default_trading_api_key_please_change" \
        -d '{"ticker":"SPY","model":"amazon/chronos-t5-tiny","context":[100,101,102,103,104],"prediction_length":5}')

    if echo "$response" | grep -q "predictions"; then
        print_success "Forecast endpoint working"
    else
        print_warning "Forecast test response: $response"
    fi
}

# =============================================================================
# Verification
# =============================================================================

verify_setup() {
    print_header "Verifying Setup"

    echo ""
    print_step "Checking running containers..."
    podman ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

    echo ""
    print_step "Testing service health..."

    local services=(
        "http://localhost:12700/health|Sapheneia Forecast"
        "http://localhost:12710/health|Chronos Service"
        "http://localhost:12130/health|InfluxDB"
    )

    local all_healthy=true
    for service in "${services[@]}"; do
        IFS='|' read -r url name <<< "$service"
        if curl -s "$url" > /dev/null 2>&1; then
            print_success "$name: healthy"
        else
            print_error "$name: not responding"
            all_healthy=false
        fi
    done

    echo ""
    if [ "$all_healthy" = true ]; then
        print_success "All services are healthy!"
    else
        print_warning "Some services are not responding. Check logs with:"
        print_info "  podman logs <container-name>"
    fi
}

# =============================================================================
# Print Summary
# =============================================================================

print_summary() {
    print_header "Setup Complete!"

    echo -e "${GREEN}"
    cat << 'EOF'
    ___    __          __  _                ________  __________
   /   |  / /__  __  _/ /_(_)___ _____     / ____/ / / / ____/ /
  / /| | / / _ \/ / / / __/ / __ `/ __ \   / /_  / / / / __  / /
 / ___ |/ /  __/ /_/ / /_/ / /_/ / / / /  / __/ / /_/ / /_/ / /
/_/  |_/_/\___/\__,_/\__/_/\__,_/_/ /_/  /_/    \____/\____/_/

EOF
    echo -e "${NC}"

    echo "Your Aleutian + Sapheneia integration is ready!"
    echo ""
    echo -e "${CYAN}Quick Start Commands:${NC}"
    echo ""
    echo "  # Run an evaluation"
    echo "  aleutian evaluate run --config strategies/spy_threshold_v1.yaml --api-version unified"
    echo ""
    echo "  # Export results"
    echo "  aleutian evaluate export <run_id>"
    echo ""
    echo "  # Check status"
    echo "  aleutian-status"
    echo ""
    echo -e "${CYAN}Service URLs:${NC}"
    echo ""
    echo "  Aleutian Orchestrator:  http://localhost:12700"
    echo "  Sapheneia Orchestration: http://localhost:12210"
    echo "  Chronos Forecast:       http://localhost:12710"
    echo "  InfluxDB:               http://localhost:12130"
    echo ""
    echo -e "${CYAN}Environment Variables (already configured):${NC}"
    echo ""
    echo "  ORCHESTRATOR_URL=$ORCHESTRATOR_URL"
    echo "  SAPHENEIA_API_KEY=$SAPHENEIA_API_KEY"
    echo ""
    echo -e "${YELLOW}NOTE: Run 'source ~/.bashrc' or open a new terminal to use the aliases.${NC}"
    echo ""
}

# =============================================================================
# Main
# =============================================================================

main() {
    print_header "Aleutian + Sapheneia Setup Script"

    echo "This script will set up AleutianFOSS with Sapheneia integration."
    echo "It will:"
    echo "  1. Check and install prerequisites"
    echo "  2. Guide through GitHub authentication"
    echo "  3. Clone required repositories"
    echo "  4. Configure environment variables"
    echo "  5. Start all services"
    echo ""

    read -p "Continue? [Y/n] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]] && [[ ! -z $REPLY ]]; then
        echo "Setup cancelled."
        exit 0
    fi

    check_prerequisites
    setup_github_auth
    clone_repositories
    configure_environment
    setup_sapheneia
    setup_aleutian
    initialize_models
    verify_setup
    print_summary
}

# Run main function
main "$@"
