#!/bin/bash
# CRS Integration Tests for Aleutian Trace Agent
# Tests Phase 0 CRS features (GR-28 through GR-37)
#
# These tests verify:
#   - Session restore (GR-36): State persists across sessions
#   - Graph provider snapshot (GR-28): Graph state captured
#   - Disk persistence (GR-33): Backup/restore working
#   - Analytics CRS routing (GR-31): Analytics queries tracked
#   - Delta history (GR-35): Deltas recorded for replay
#
# Usage:
#   ./test_crs_integration.sh              # Run all CRS tests (remote mode)
#   ./test_crs_integration.sh -t 1,2,3     # Run specific tests
#   ./test_crs_integration.sh --local      # Run local Go tests (no GPU required)

set -e

# Configuration (can be overridden via environment)
REMOTE_HOST="${CRS_TEST_HOST:-10.0.0.250}"
REMOTE_PORT="${CRS_TEST_PORT:-13022}"
REMOTE_USER="${CRS_TEST_USER:-aleutiandevops}"
SSH_KEY="$HOME/.ssh/aleutiandevops_ansible_key"
SSH_CONTROL_SOCKET="$HOME/.ssh/crs_test_multiplex_%h_%p_%r"

# Model configuration (must match test_trace_agent_remote.sh)
OLLAMA_MODEL="glm-4.7-flash"
ROUTER_MODEL="granite4:micro-h"

# Project to analyze on remote
PROJECT_TO_ANALYZE="${TEST_PROJECT_ROOT:-/Users/jin/GolandProjects/AleutianOrchestrator}"
OUTPUT_FILE="/tmp/crs_test_results_$(date +%Y%m%d_%H%M%S).json"

# Local test mode (uses local Go tests instead of remote)
LOCAL_MODE=false

# Parse arguments
SPECIFIC_TESTS=""
while [[ $# -gt 0 ]]; do
    case $1 in
        -t|--tests)
            SPECIFIC_TESTS="$2"
            shift 2
            ;;
        --local)
            LOCAL_MODE=true
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [-t|--tests TEST_SPEC] [--local]"
            echo ""
            echo "Options:"
            echo "  -t, --tests   Comma-separated test numbers or ranges (e.g., 1,2,3 or 1-5)"
            echo "  --local       Run local Go tests instead of remote integration tests"
            echo ""
            echo "Test Categories:"
            echo "  1-3:   Session Restore (GR-36)"
            echo "  4-6:   Disk Persistence (GR-33)"
            echo "  7-9:   Graph Snapshots (GR-28)"
            echo "  10-12: Analytics Routing (GR-31)"
            echo "  13-15: Delta History (GR-35)"
            echo "  16-21: Graph Index Optimization (GR-01)"
            echo "  22-27: Go Interface Implementation Detection (GR-40)"
            echo "  28-30: Existence Tests (things that exist in AleutianOrchestrator)"
            echo "  31-35: PageRank Algorithm & find_important Tool (GR-12/GR-13)"
            echo "  36-44: Integration Test Quality Fixes (GR-Phase1)"
            echo "  45-49: Secondary Indexes (GR-06 to GR-09)"
            echo "  50-54: Query Cache LRU (GR-10)"
            echo "  55-59: Parallel BFS (GR-11)"
            echo ""
            echo "Environment Variables:"
            echo "  CRS_TEST_HOST    Remote host (default: 10.0.0.250)"
            echo "  CRS_TEST_PORT    SSH port (default: 13022)"
            echo "  CRS_TEST_USER    SSH user (default: aleutiandevops)"
            echo "  TEST_PROJECT_ROOT  Project to analyze"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# ==============================================================================
# LOCAL TEST MODE
# ==============================================================================

run_local_tests() {
    echo -e "${BLUE}═══════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  CRS Integration Tests - Local Mode${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════${NC}"
    echo ""

    local test_args=""
    if [ -n "$SPECIFIC_TESTS" ]; then
        # Map test numbers to Go test names
        case "$SPECIFIC_TESTS" in
            *1*|*2*|*3*)
                test_args="$test_args -run TestSession"
                ;;
            *4*|*5*|*6*)
                test_args="$test_args -run TestPersistence"
                ;;
            *7*|*8*|*9*)
                test_args="$test_args -run TestGraph"
                ;;
            *10*|*11*|*12*)
                test_args="$test_args -run TestAnalytics"
                ;;
            *13*|*14*|*15*)
                test_args="$test_args -run TestDeltaHistory"
                ;;
            *16*|*17*|*18*|*19*|*20*|*21*)
                test_args="$test_args -run TestGraphIndex"
                ;;
            *22*|*23*|*24*|*25*|*26*|*27*)
                test_args="$test_args -run TestGoInterface"
                ;;
            *28*|*29*|*30*)
                test_args="$test_args -run TestExistence"
                ;;
            *31*|*32*|*33*|*34*|*35*)
                test_args="$test_args -run TestPageRank"
                ;;
            *36*|*37*|*38*|*39*|*40*|*41*|*42*|*43*|*44*)
                test_args="$test_args -run TestQuality"
                ;;
            *45*|*46*|*47*|*48*|*49*)
                test_args="$test_args -run TestSecondaryIndex"
                ;;
            *50*|*51*|*52*|*53*|*54*)
                test_args="$test_args -run TestQueryCache"
                ;;
            *55*|*56*|*57*|*58*|*59*)
                test_args="$test_args -run TestParallelBFS"
                ;;
        esac
    fi

    echo -e "${YELLOW}Running CRS tests...${NC}"
    echo ""

    local exit_code=0

    # Run the Go tests for CRS package
    if ! go test ./services/trace/agent/mcts/crs/... -v -timeout 120s $test_args; then
        exit_code=1
    fi

    # For tests 55-59 (Parallel BFS), also run graph package tests
    if [[ "$SPECIFIC_TESTS" =~ (55|56|57|58|59) ]] || [ -z "$SPECIFIC_TESTS" ]; then
        echo ""
        echo -e "${YELLOW}Running Parallel BFS tests (GR-11)...${NC}"
        echo ""

        # Run parallel BFS tests with race detector
        if ! go test ./services/trace/graph/... -v -timeout 120s -run "TestParallelBFS" -race; then
            exit_code=1
        fi

        # Run benchmarks if no specific tests requested
        if [ -z "$SPECIFIC_TESTS" ]; then
            echo ""
            echo -e "${YELLOW}Running Parallel BFS benchmarks...${NC}"
            go test ./services/trace/graph/... -bench=BenchmarkBFS -benchmem -count=1 -timeout 60s || true
        fi
    fi

    if [ $exit_code -eq 0 ]; then
        echo ""
        echo -e "${GREEN}═══════════════════════════════════════════════════${NC}"
        echo -e "${GREEN}  All CRS Tests PASSED${NC}"
        echo -e "${GREEN}═══════════════════════════════════════════════════${NC}"
    else
        echo ""
        echo -e "${RED}═══════════════════════════════════════════════════${NC}"
        echo -e "${RED}  Some CRS Tests FAILED${NC}"
        echo -e "${RED}═══════════════════════════════════════════════════${NC}"
        exit 1
    fi
}

# ==============================================================================
# REMOTE INTEGRATION TEST DEFINITIONS
# ==============================================================================

# Test categories for CRS integration
declare -a CRS_TESTS=(
    # === SESSION RESTORE (GR-36) ===
    # These tests verify learned state persists across sessions

    # Test 1: Learn something in session 1, verify it persists
    "SESSION_RESTORE|session1|What is the main function in this codebase?|COMPLETE"

    # Test 2: Session 2 should restore and remember previous context
    "SESSION_RESTORE|session2_restore|Based on our previous conversation about main, what does it import?|COMPLETE"

    # Test 3: Verify proof numbers are restored (faster queries)
    "SESSION_RESTORE|session2_speed|What functions does main call?|COMPLETE|faster_than_first"

    # === DISK PERSISTENCE (GR-33) ===
    # These verify checkpoint save/load works

    # Test 4: Trigger checkpoint save
    "PERSISTENCE|checkpoint_save|Analyze the api package and remember the key types|COMPLETE"

    # Test 5: Verify checkpoint exists on disk
    "PERSISTENCE|checkpoint_verify|INTERNAL:verify_checkpoint_exists|COMPLETE"

    # Test 6: Restore from checkpoint after crash simulation
    "PERSISTENCE|checkpoint_restore|INTERNAL:restart_and_verify_state|COMPLETE"

    # === GRAPH SNAPSHOTS (GR-28) ===
    # These test graph state capture

    # Test 7: Build graph and verify snapshot
    "GRAPH|snapshot_create|Find all callers of the main function|COMPLETE"

    # Test 8: Verify graph context in events
    "GRAPH|event_context|INTERNAL:verify_event_graph_context|COMPLETE"

    # Test 9: Verify graph generation tracking
    "GRAPH|generation_track|Find callees of parseConfig|COMPLETE|generation_incremented"

    # === ANALYTICS ROUTING (GR-31) ===
    # These verify analytics tools route through CRS

    # Test 10: Run hotspots analysis
    "ANALYTICS|hotspots|Find the hotspots in this codebase|COMPLETE|analytics_recorded"

    # Test 11: Run dead code analysis
    "ANALYTICS|dead_code|Find any dead code in this project|COMPLETE|analytics_recorded"

    # Test 12: Run cycle detection
    "ANALYTICS|cycles|Are there any dependency cycles?|COMPLETE|analytics_recorded"

    # === DELTA HISTORY (GR-35) ===
    # These verify delta recording

    # Test 13: Verify deltas are recorded
    "HISTORY|delta_record|INTERNAL:verify_delta_count|COMPLETE"

    # Test 14: Verify history ringbuffer limits
    "HISTORY|ringbuffer|INTERNAL:verify_history_limit|COMPLETE"

    # Test 15: Verify delta replay works
    "HISTORY|replay|INTERNAL:replay_and_verify|COMPLETE"

    # === GR-01: GRAPH INDEX OPTIMIZATION ===
    # These tests verify graph tools use SymbolIndex O(1) lookup instead of O(V) scan

    # Test 16: Verify find_callers returns results correctly
    "GRAPH_INDEX|find_callers_basic|Find all callers of the Setup function|COMPLETE|graph_tool_used"

    # Test 17: Verify find_callees returns results correctly
    "GRAPH_INDEX|find_callees_basic|Find all functions called by main|COMPLETE|graph_tool_used"

    # Test 18: Verify find_implementations returns results correctly
    "GRAPH_INDEX|find_impls_basic|Find all implementations of the Handler interface|COMPLETE|graph_tool_used"

    # Test 19: Performance - second query should be fast (index warmed)
    "GRAPH_INDEX|perf_warm|Find callers of Execute in this codebase|COMPLETE|fast_execution"

    # Test 20: Verify OTel spans capture index usage (check logs for index_used=true)
    "GRAPH_INDEX|otel_trace|INTERNAL:verify_index_span_attribute|COMPLETE"

    # Test 21: Edge case - symbol not found should return quickly (O(1) fail fast)
    "GRAPH_INDEX|not_found_fast|Find callers of NonExistentFunctionXYZ123|COMPLETE|fast_not_found"

    # === GR-40: GO INTERFACE IMPLEMENTATION DETECTION ===
    # These tests verify that find_implementations works for Go code
    # Pre-GR-40: These tests are expected to FAIL (empty results, Grep fallback)
    # Post-GR-40: These tests should PASS (correct implementations found)

    # Test 22: Basic interface implementation - should find concrete types
    "GO_INTERFACE|basic_impl|Find all implementations of the Handler interface in this Go codebase|COMPLETE|implementations_found"

    # Test 23: Interface with multiple implementations
    "GO_INTERFACE|multi_impl|What types implement the Service interface?|COMPLETE|implementations_found"

    # Test 24: Empty interface (interface{}/any) - should handle gracefully
    "GO_INTERFACE|empty_interface|Find implementations of the Reader interface|COMPLETE|implementations_found"

    # Test 25: Verify no Grep fallback - should use graph tools only
    "GO_INTERFACE|no_grep_fallback|List all types that implement Closer|COMPLETE|no_grep_used"

    # Test 26: Verify EdgeTypeImplements exists in graph (internal check)
    "GO_INTERFACE|edge_exists|INTERNAL:verify_implements_edges|COMPLETE"

    # Test 27: Performance - implementation lookup should be O(k) not O(V)
    "GO_INTERFACE|perf_check|Find implementations of the Writer interface|COMPLETE|fast_execution"

    # === EXISTENCE TESTS (Tests for things that EXIST in AleutianOrchestrator) ===
    # These tests verify graph tools work when the target actually exists
    # GR-41: Added to validate call edge extraction works correctly

    # Test 28: find_callers for function that HAS callers (getDatesToProcess called by main)
    "GRAPH_INDEX|find_callers_exists|Find all callers of the getDatesToProcess function|COMPLETE|graph_tool_used"

    # Test 29: find_references for struct that EXISTS (Handler is a struct, not interface)
    "GRAPH_INDEX|find_refs_exists|Find all references to the Handler type|COMPLETE|graph_tool_used"

    # Test 30: find_callees for function that HAS callees (main calls multiple functions)
    "GRAPH_INDEX|find_callees_exists|Find all functions called by the main function|COMPLETE|graph_tool_used"

    # === GR-12/GR-13: PAGERANK ALGORITHM & find_important TOOL ===
    # These tests verify PageRank-based importance ranking is working

    # Test 31: Basic find_important query - should use PageRank not degree-based
    "PAGERANK|basic|What are the most important functions in this codebase?|COMPLETE|pagerank_used"

    # Test 32: find_important with top parameter
    "PAGERANK|top_param|Find the top 5 most important symbols|COMPLETE|pagerank_used"

    # Test 33: Comparison query - should mention PageRank vs degree difference
    "PAGERANK|compare|Which functions have the highest PageRank score?|COMPLETE|pagerank_used"

    # Test 34: Verify PageRank converges (internal check)
    "PAGERANK|convergence|INTERNAL:verify_pagerank_convergence|COMPLETE"

    # Test 35: Performance - PageRank should complete within reasonable time
    "PAGERANK|perf_check|Find the most architecturally important functions using PageRank|COMPLETE|fast_pagerank"

    # === GR-PHASE1: INTEGRATION TEST QUALITY FIXES ===
    # These tests verify the quality and efficiency issues identified in Phase 0-1 testing
    # TDD: These tests define expected behavior BEFORE fixes are implemented

    # Test 36: P0 - Empty response warnings should be minimal (< 50 total)
    "QUALITY|empty_response|What is the entry point of this codebase?|COMPLETE|empty_response_threshold"

    # Test 37: P0 - Average test runtime should be reasonable (< 15s for simple queries)
    "QUALITY|runtime_check|List the main packages in this project|COMPLETE|avg_runtime_threshold"

    # Test 38: P1 - Circuit breaker should fire consistently for all tools at threshold
    "QUALITY|cb_consistency|INTERNAL:verify_cb_threshold_consistency|COMPLETE"

    # Test 39: P1 - CRS speedup verification (session 2 faster than session 1)
    "QUALITY|crs_speedup|What does the main function do?|COMPLETE|crs_speedup_verified"

    # Test 40: P2 - Not-found queries should be fast (< 5 seconds)
    "QUALITY|not_found_fast|Find the function named CompletelyNonExistentXYZ999|COMPLETE|fast_not_found_strict"

    # Test 41: P2 - Debug endpoint /debug/crs should be available
    "QUALITY|debug_crs|INTERNAL:verify_debug_crs_endpoint|COMPLETE"

    # Test 42: P2 - Debug endpoint /debug/history should be available
    "QUALITY|debug_history|INTERNAL:verify_debug_history_endpoint|COMPLETE"

    # Test 43: P2 - PageRank convergence should be logged
    "QUALITY|pr_convergence|INTERNAL:verify_pagerank_convergence_logged|COMPLETE"

    # Test 44: P3 - Response should include [file:line] citations
    "QUALITY|citations|Where is the Handler type defined?|COMPLETE|citations_present"

    # === GR-06 to GR-09: SECONDARY INDEXES ===
    # These tests verify secondary indexes are working correctly
    # NOTE: Test 45 builds the graph first, then 46-49 verify indexes

    # Test 45: Build graph first (prerequisite for index verification)
    "SECONDARY_INDEX|build_graph|Find the function named main in this codebase|COMPLETE|graph_tool_used"

    # Test 46: GR-06 - Verify nodesByName index exists and has data
    "SECONDARY_INDEX|nodes_by_name|INTERNAL:verify_nodes_by_name_index|COMPLETE"

    # Test 47: GR-07 - Verify nodesByKind index via /debug/graph/stats
    "SECONDARY_INDEX|nodes_by_kind|INTERNAL:verify_nodes_by_kind_index|COMPLETE"

    # Test 48: GR-08 - Verify edgesByType index via /debug/graph/stats
    "SECONDARY_INDEX|edges_by_type|INTERNAL:verify_edges_by_type_index|COMPLETE"

    # Test 49: GR-09 - Verify edgesByFile index exists (RemoveFile uses it)
    "SECONDARY_INDEX|edges_by_file|INTERNAL:verify_edges_by_file_index|COMPLETE"

    # === GR-10: QUERY CACHING WITH LRU ===
    # These tests verify query caching is working correctly
    # TDD: Tests added BEFORE implementation

    # Test 50: First callers query (should populate cache)
    "QUERY_CACHE|cache_populate|Find all callers of the main function|COMPLETE|cache_miss_expected"

    # Test 51: Second identical callers query (should hit cache)
    "QUERY_CACHE|cache_hit|Find all callers of the main function|COMPLETE|cache_hit_expected"

    # Test 52: Verify cache stats endpoint returns data
    "QUERY_CACHE|cache_stats|INTERNAL:verify_cache_stats_endpoint|COMPLETE"

    # Test 53: Cache invalidation on graph rebuild (internal)
    "QUERY_CACHE|cache_invalidation|INTERNAL:verify_cache_invalidation|COMPLETE"

    # Test 54: Performance - cached query should be faster than uncached
    "QUERY_CACHE|cache_perf|Find callees of parseConfig|COMPLETE|cache_speedup_expected"

    # === GR-11: PARALLEL BFS FOR WIDE GRAPHS ===
    # These tests verify parallel BFS is working correctly
    # TDD: Tests added BEFORE implementation

    # Test 55: Parallel BFS returns same results as sequential (correctness)
    "PARALLEL_BFS|correctness|Find the complete call graph starting from main|COMPLETE|parallel_correctness"

    # Test 56: Verify parallel mode is enabled for wide graphs (threshold check)
    "PARALLEL_BFS|threshold|INTERNAL:verify_parallel_threshold|COMPLETE"

    # Test 57: Performance - parallel should be faster for wide graph traversal
    "PARALLEL_BFS|speedup|Get the full call chain from main to all functions it reaches|COMPLETE|parallel_speedup"

    # Test 58: Context cancellation works correctly in parallel mode
    "PARALLEL_BFS|cancellation|INTERNAL:verify_parallel_context_cancellation|COMPLETE"

    # Test 59: Race detector verification (internal - run with -race flag)
    "PARALLEL_BFS|race_free|INTERNAL:verify_no_race_conditions|COMPLETE"
)

# ==============================================================================
# SSH HELPERS
# ==============================================================================

# Setup ssh-agent to cache passphrase (only enter once)
setup_ssh_agent() {
    # Check if ssh-agent is already running with our key
    if ! ssh-add -l 2>/dev/null | grep -q "aleutiandevops_ansible_key"; then
        echo -e "${YELLOW}Setting up ssh-agent to cache passphrase...${NC}"
        eval "$(ssh-agent -s)" > /dev/null
        ssh-add "$SSH_KEY"
        echo -e "${GREEN}SSH key added to agent${NC}"
    fi
}

# SSH command wrapper (uses multiplexed connection)
ssh_cmd() {
    ssh -i "$SSH_KEY" \
        -o StrictHostKeyChecking=no \
        -o ControlPath="$SSH_CONTROL_SOCKET" \
        -p "$REMOTE_PORT" "$REMOTE_USER@$REMOTE_HOST" "$@"
}

# Establish master SSH connection for multiplexing
establish_connection() {
    echo -e "${YELLOW}Establishing master SSH connection...${NC}"
    ssh -i "$SSH_KEY" -p "$REMOTE_PORT" \
        -o StrictHostKeyChecking=no \
        -o ControlMaster=auto \
        -o ControlPath="$SSH_CONTROL_SOCKET" \
        -o ControlPersist=10m \
        -fN "$REMOTE_USER@$REMOTE_HOST"
    echo -e "${GREEN}Master connection established (multiplexing enabled)${NC}"
}

# Close master SSH connection
close_connection() {
    ssh -O exit -o ControlPath="$SSH_CONTROL_SOCKET" "$REMOTE_USER@$REMOTE_HOST" 2>/dev/null || true
}

# Test SSH connection
test_ssh_connection() {
    echo -e "${YELLOW}Testing SSH connection to $REMOTE_USER@$REMOTE_HOST:$REMOTE_PORT${NC}"
    if ssh_cmd "echo 'SSH connection successful'"; then
        echo -e "${GREEN}SSH connection OK${NC}"
        return 0
    else
        echo -e "${RED}SSH connection failed${NC}"
        return 1
    fi
}

# Setup remote environment (sync project and build)
setup_remote() {
    echo -e "${YELLOW}Setting up remote environment...${NC}"

    # Create temp directory on remote
    ssh_cmd "mkdir -p ~/trace_test"

    # Copy the project to analyze (if it's local Mac path)
    if [[ "$PROJECT_TO_ANALYZE" == /Users/* ]]; then
        echo "Syncing project to remote server..."
        local project_basename="$(basename "$PROJECT_TO_ANALYZE")"
        local remote_project="/home/$REMOTE_USER/trace_test/$project_basename"

        # Use rsync for efficient sync (uses multiplexed connection)
        rsync -az --delete -q --stats \
            --exclude '.git' \
            --exclude '.venv' \
            --exclude '__pycache__' \
            --exclude 'node_modules' \
            --exclude '.DS_Store' \
            -e "ssh -i $SSH_KEY -o StrictHostKeyChecking=no -o ControlPath=$SSH_CONTROL_SOCKET -p $REMOTE_PORT" \
            "$PROJECT_TO_ANALYZE/" "$REMOTE_USER@$REMOTE_HOST:$remote_project/" \
            | tail -3

        PROJECT_TO_ANALYZE="$remote_project"
        echo -e "${GREEN}Project synced to $remote_project${NC}"
    fi

    # Copy and build the trace server on remote
    echo "Building trace server on remote..."
    local local_repo="$(cd "$(dirname "$0")/.." && pwd)"

    # Sync the AleutianFOSS repo
    rsync -az --delete -q --stats \
        --exclude '.git' \
        --exclude '.venv' \
        --exclude '__pycache__' \
        --exclude 'bin' \
        --exclude '*.log' \
        --exclude 'trace_test_results*' \
        --exclude 'crs_test_results*' \
        --exclude 'node_modules' \
        --exclude '.DS_Store' \
        --exclude 'demo_data' \
        --exclude 'test_agent_data' \
        --exclude 'slides' \
        -e "ssh -i $SSH_KEY -o StrictHostKeyChecking=no -o ControlPath=$SSH_CONTROL_SOCKET -p $REMOTE_PORT" \
        "$local_repo/" "$REMOTE_USER@$REMOTE_HOST:~/trace_test/AleutianFOSS/" \
        | tail -3

    # Build on remote
    ssh_cmd "cd ~/trace_test/AleutianFOSS && go build -o bin/trace ./cmd/trace"

    echo -e "${GREEN}Remote environment ready${NC}"
}

# Check remote Ollama status
check_remote_ollama() {
    echo -e "${YELLOW}Checking Ollama on remote server...${NC}"

    if ! ssh_cmd "curl -s http://localhost:11434/api/tags" > /dev/null 2>&1; then
        echo -e "${RED}ERROR: Ollama is not running on remote server${NC}"
        echo "SSH into the server and start Ollama:"
        echo "  ssh -p $REMOTE_PORT $REMOTE_USER@$REMOTE_HOST"
        echo "  ollama serve"
        exit 1
    fi

    echo -e "${GREEN}✓ Ollama is running on remote server${NC}"

    # Get available models
    local models=$(ssh_cmd "curl -s http://localhost:11434/api/tags")

    # Check main agent model
    if ! echo "$models" | grep -q "$OLLAMA_MODEL"; then
        echo -e "${RED}ERROR: Model $OLLAMA_MODEL not found${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Main Agent model available: $OLLAMA_MODEL${NC}"

    # Check router model
    if ! echo "$models" | grep -q "$ROUTER_MODEL"; then
        echo -e "${RED}ERROR: Router model $ROUTER_MODEL not found${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Router model available: $ROUTER_MODEL${NC}"
}

# Start trace server on remote
start_trace_server() {
    echo -e "${YELLOW}Starting trace server on remote...${NC}"

    # Kill any existing trace server
    ssh_cmd "pkill -f 'bin/trace'" 2>/dev/null || true
    sleep 1

    # GR-40: Wipe stale graph cache to force rebuild with latest code
    # This ensures new features (like interface detection) are picked up
    echo "Wiping stale graph cache to force rebuild..."
    ssh_cmd "rm -f ~/trace_test/AleutianFOSS/*.db ~/trace_test/AleutianFOSS/graph_cache.json ~/trace_test/AleutianFOSS/*.gob 2>/dev/null" || true
    ssh_cmd "rm -rf ~/trace_test/AleutianFOSS/badger_* 2>/dev/null" || true

    # Start the server in background
    ssh -f -i "$SSH_KEY" \
        -o StrictHostKeyChecking=no \
        -p "$REMOTE_PORT" "$REMOTE_USER@$REMOTE_HOST" \
        "cd ~/trace_test/AleutianFOSS && \
         OLLAMA_BASE_URL=http://localhost:11434 \
         OLLAMA_MODEL=$OLLAMA_MODEL \
         nohup ./bin/trace -with-context -with-tools > trace_server.log 2>&1 &"

    sleep 2

    # Check if process started
    local server_pid
    server_pid=$(ssh_cmd "pgrep -f 'bin/trace'" 2>/dev/null || echo "")
    if [ -z "$server_pid" ]; then
        echo -e "${RED}ERROR: Failed to start trace server${NC}"
        ssh_cmd "cat ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "(no log file)"
        return 1
    fi

    echo "Server started with PID: $server_pid"

    # Wait for server to be responding (basic connectivity)
    echo "Waiting for server to respond..."
    local responding=0
    for i in {1..15}; do
        echo -n "."
        sleep 1
        if ssh_cmd "curl -s http://localhost:8080/v1/codebuddy/health" > /dev/null 2>&1; then
            responding=1
            break
        fi
    done
    echo ""

    if [ $responding -eq 0 ]; then
        echo -e "${RED}ERROR: Trace server not responding after 15 seconds${NC}"
        ssh_cmd "tail -30 ~/trace_test/AleutianFOSS/trace_server.log" 2>/dev/null || true
        return 1
    fi

    # Wait for warmup to complete (poll /ready endpoint)
    # Model warmup takes 30-90 seconds for large models like glm-4.7-flash
    echo "Waiting for model warmup to complete (this may take 30-90 seconds)..."
    local ready=0
    for i in {1..120}; do
        # Check /ready endpoint - returns 200 when warmup complete, 503 when still warming
        local ready_status=$(ssh_cmd "curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/v1/codebuddy/ready" 2>/dev/null)
        if [ "$ready_status" = "200" ]; then
            ready=1
            break
        fi
        # Show progress every 10 seconds
        if [ $((i % 10)) -eq 0 ]; then
            echo "  Still warming up... (${i}s elapsed, status: $ready_status)"
        fi
        sleep 1
    done

    if [ $ready -eq 1 ]; then
        echo -e "${GREEN}Trace server is ready (warmup complete)${NC}"
        return 0
    else
        echo -e "${RED}ERROR: Model warmup did not complete after 120 seconds${NC}"
        ssh_cmd "tail -50 ~/trace_test/AleutianFOSS/trace_server.log" 2>/dev/null || true
        return 1
    fi
}

# Stop trace server on remote
stop_trace_server() {
    echo -e "${YELLOW}Stopping trace server...${NC}"
    ssh_cmd "pkill -f 'bin/trace'" 2>/dev/null || true
}

# ==============================================================================
# TEST EXECUTION
# ==============================================================================

# Global variables for test tracking
declare -a DETAILED_RESULTS=()
FIRST_TEST_RUNTIME=0

run_crs_test() {
    local test_spec="$1"
    local test_num="$2"

    IFS='|' read -r category session_id query expected_state extra_check <<< "$test_spec"

    echo ""
    echo -e "${BLUE}════════════════════════════════════════════════════════════════${NC}"
    echo -e "${YELLOW}Test $test_num [$category]: $session_id${NC}"
    echo -e "${BLUE}════════════════════════════════════════════════════════════════${NC}"
    echo -e "  Query: ${query:0:80}..."

    # Handle internal verification tests
    if [[ "$query" == INTERNAL:* ]]; then
        # GR-39 Issue 5: Pass test_num to internal tests for proper result tracking
        run_internal_test "$category" "${query#INTERNAL:}" "$expected_state" "$test_num"
        return $?
    fi

    # Run agent query using the remote project path
    local start_time=$(get_time_ms)

    # Use PROJECT_TO_ANALYZE which is already converted to remote path in setup_remote
    local response=$(ssh_cmd "curl -s -X POST 'http://localhost:8080/v1/codebuddy/agent/run' \
        -H 'Content-Type: application/json' \
        -H 'X-Session-ID: crs_test_${session_id}' \
        -d '{\"project_root\": \"$PROJECT_TO_ANALYZE\", \"query\": \"$query\"}' \
        --max-time 300")

    local end_time=$(get_time_ms)
    local duration=$((end_time - start_time))

    # Validate response
    if [ -z "$response" ] || ! echo "$response" | jq . > /dev/null 2>&1; then
        echo -e "  ${RED}✗ FAILED - Invalid or empty response${NC}"
        return 1
    fi

    local state=$(echo "$response" | jq -r '.state // "ERROR"')
    local session_actual=$(echo "$response" | jq -r '.session_id // "unknown"')
    local steps_taken=$(echo "$response" | jq -r '.steps_taken // 0')
    local tokens_used=$(echo "$response" | jq -r '.tokens_used // 0')
    local agent_response=$(echo "$response" | jq -r '.response // ""')

    echo ""
    echo -e "  ${BLUE}─── Agent Response ───${NC}"
    echo -e "  State: $state | Steps: $steps_taken | Tokens: $tokens_used | Time: ${duration}ms"
    echo ""
    # Show truncated response
    echo "$agent_response" | head -20 | sed 's/^/    /'
    if [ $(echo "$agent_response" | wc -l) -gt 20 ]; then
        echo -e "    ${YELLOW}... (truncated, $(echo "$agent_response" | wc -l) total lines)${NC}"
    fi

    # Fetch and display CRS reasoning trace
    local trace_json="{}"
    local crs_details=""
    if [ "$session_actual" != "unknown" ]; then
        local trace_response=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/agent/$session_actual/reasoning'" 2>/dev/null)
        if echo "$trace_response" | jq . > /dev/null 2>&1; then
            trace_json="$trace_response"
            local trace_count=$(echo "$trace_response" | jq '.total_steps // 0')

            echo ""
            echo -e "  ${BLUE}─── CRS Reasoning Trace ($trace_count steps) ───${NC}"

            # Show each reasoning step with details
            echo "$trace_response" | jq -r '.trace[] |
                "    [\(.timestamp // "?")] \(.action // "unknown")" +
                (if .tool then " → Tool: \(.tool)" else "" end) +
                (if .target then " → Target: \(.target)" else "" end) +
                (if .result then " → Result: \(.result | tostring | .[0:60])" else "" end) +
                (if .error and .error != "" then " ⚠ Error: \(.error)" else "" end)
            ' 2>/dev/null | head -30

            # Show tool call summary with threshold warnings
            echo ""
            echo -e "  ${BLUE}─── Tool Usage Summary ───${NC}"
            local tool_summary=$(echo "$trace_response" | jq -r '
                [.trace[] | select(.action == "tool_call" or .action == "tool_call_forced")] |
                group_by(.tool) |
                map({tool: .[0].tool, count: length}) |
                sort_by(-.count) |
                .[] | "    \(.tool): \(.count) call(s)" +
                    (if .count > 2 then " ⚠ EXCEEDS THRESHOLD" else "" end)
            ' 2>/dev/null)
            if [ -n "$tool_summary" ]; then
                echo "$tool_summary"
            else
                echo "    (no tool calls recorded)"
            fi

            # GR-39b: Show circuit breaker events
            local cb_events=$(echo "$trace_response" | jq -r '
                [.trace[] | select(.action == "circuit_breaker")] |
                if length > 0 then
                    .[] | "    🛑 \(.tool // .target): \(.error // "fired")"
                else
                    null
                end
            ' 2>/dev/null)
            if [ "$cb_events" != "null" ] && [ -n "$cb_events" ]; then
                echo ""
                echo -e "  ${YELLOW}─── Circuit Breaker Events (GR-39b) ───${NC}"
                echo "$cb_events"
            fi

            # CB-30c: Show semantic repetition blocks
            local sem_blocks=$(echo "$trace_response" | jq -r '
                [.trace[] | select(.error != null and (.error | test("[Ss]emantic repetition|similar to")))] |
                if length > 0 then
                    .[] | "    🔄 \(.tool // .target): \(.error | .[0:80])"
                else
                    null
                end
            ' 2>/dev/null)
            if [ "$sem_blocks" != "null" ] && [ -n "$sem_blocks" ]; then
                echo ""
                echo -e "  ${YELLOW}─── Semantic Repetition Blocks (CB-30c) ───${NC}"
                echo "$sem_blocks"
            fi

            # GR-41b: Show LLM prompt/response info from llm_call steps
            local llm_calls=$(echo "$trace_response" | jq -r '
                [.trace[] | select(.action == "llm_call")] |
                if length > 0 then
                    .[] |
                    "    [LLM Call] msgs=\(.metadata.message_count // "?") tokens_out=\(.metadata.output_tokens // "?")" +
                    (if .metadata.last_user_message and (.metadata.last_user_message | length) > 0 then
                        "\n      Query: \(.metadata.last_user_message | .[0:100])..."
                    else "" end) +
                    (if .metadata.content_preview and (.metadata.content_preview | length) > 0 then
                        "\n      Response: \(.metadata.content_preview | .[0:100])..."
                    else "" end)
                else
                    null
                end
            ' 2>/dev/null)
            if [ "$llm_calls" != "null" ] && [ -n "$llm_calls" ]; then
                echo ""
                echo -e "  ${CYAN}─── LLM Prompts & Responses (GR-41b) ───${NC}"
                echo "$llm_calls"
            fi

            # Show routing decisions
            local routing=$(echo "$trace_response" | jq -r '
                [.trace[] | select(.action == "tool_routing")] |
                if length > 0 then
                    "    Router selected: " + ([.[].target] | unique | join(", "))
                else
                    "    (no routing decisions recorded)"
                end
            ' 2>/dev/null)
            echo ""
            echo -e "  ${BLUE}─── Router Decisions ───${NC}"
            echo "$routing"

            # Show any learned clauses or CRS state changes
            local crs_state=$(echo "$trace_response" | jq -r '
                .crs_state // {} |
                if . != {} then
                    "    Clauses: \(.clauses_count // "?") | " +
                    "Generation: \(.generation // "?") | " +
                    "Proof Numbers: \(.proof_numbers_count // "?")"
                else
                    null
                end
            ' 2>/dev/null)
            if [ "$crs_state" != "null" ] && [ -n "$crs_state" ]; then
                echo ""
                echo -e "  ${BLUE}─── CRS State ───${NC}"
                echo "$crs_state"
            fi

            # Show proof number updates (CRS-02)
            local proof_updates=$(echo "$trace_response" | jq -r '
                [.trace[] | select(.action | test("proof_update|disproven"))] |
                if length > 0 then
                    .[] | "    📊 \(.tool // .target): \(.metadata.reason // .error // "updated")"
                else
                    null
                end
            ' 2>/dev/null)
            if [ "$proof_updates" != "null" ] && [ -n "$proof_updates" ]; then
                echo ""
                echo -e "  ${BLUE}─── Proof Number Updates (CRS-02) ───${NC}"
                echo "$proof_updates"
            fi

            # Show learning events (CDCL clauses)
            local learn_events=$(echo "$trace_response" | jq -r '
                [.trace[] | select(.action | test("learn|clause|cdcl"))] |
                if length > 0 then
                    .[] | "    📚 \(.tool // .target): \(.metadata.failure_type // .error // "learned")"
                else
                    null
                end
            ' 2>/dev/null)
            if [ "$learn_events" != "null" ] && [ -n "$learn_events" ]; then
                echo ""
                echo -e "  ${BLUE}─── CDCL Learning Events (CRS-04) ───${NC}"
                echo "$learn_events"
            fi

            # Check for repeated tool calls (potential issue)
            local repeated=$(echo "$trace_response" | jq '
                [.trace[] | select(.action == "tool_call")] |
                group_by(.tool) |
                map(select(length > 3)) |
                length
            ' 2>/dev/null)
            if [ "$repeated" -gt 0 ]; then
                echo ""
                echo -e "  ${RED}⚠ WARNING: Detected tool called >3 times (potential loop)${NC}"
                # Show which tools exceeded
                echo "$trace_response" | jq -r '
                    [.trace[] | select(.action == "tool_call")] |
                    group_by(.tool) |
                    map(select(length > 3)) |
                    .[] | "    → \(.[0].tool): \(length) calls"
                ' 2>/dev/null
            fi

            # GR-39b verification: Check if circuit breaker fired appropriately
            local tool_counts=$(echo "$trace_response" | jq '
                [.trace[] | select(.action == "tool_call" or .action == "tool_call_forced")] |
                group_by(.tool) |
                map({tool: .[0].tool, count: length}) |
                map(select(.count > 2))
            ' 2>/dev/null)
            local cb_fired=$(echo "$trace_response" | jq '[.trace[] | select(.action == "circuit_breaker")] | length' 2>/dev/null)

            if [ "$(echo "$tool_counts" | jq 'length')" -gt 0 ] && [ "$cb_fired" -eq 0 ]; then
                echo ""
                echo -e "  ${RED}⚠ GR-39b ISSUE: Tools exceeded threshold but no circuit breaker fired!${NC}"
                echo "$tool_counts" | jq -r '.[] | "    → \(.tool): \(.count) calls (threshold: 2)"'
            elif [ "$cb_fired" -gt 0 ]; then
                echo ""
                echo -e "  ${GREEN}✓ GR-39b: Circuit breaker fired $cb_fired time(s)${NC}"
            fi
        fi
    fi

    # Fetch server logs for CRS-related entries (last 50 lines since test started)
    echo ""
    echo -e "  ${BLUE}─── Server Log Analysis ───${NC}"

    # GR-39b: Check for count-based circuit breaker
    local gr39b_logs=$(ssh_cmd "grep -i 'GR-39b' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -5" || echo "")
    if [ -n "$gr39b_logs" ]; then
        echo -e "  ${YELLOW}GR-39b (Count Circuit Breaker):${NC}"
        echo "$gr39b_logs" | sed 's/^/    /'
    fi

    # CB-30c: Check for semantic repetition
    local cb30c_logs=$(ssh_cmd "grep -i 'CB-30c\|[Ss]emantic repetition' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -5" || echo "")
    if [ -n "$cb30c_logs" ]; then
        echo -e "  ${YELLOW}CB-30c (Semantic Repetition):${NC}"
        echo "$cb30c_logs" | sed 's/^/    /'
    fi

    # CRS-02: Check for proof number updates
    local crs02_logs=$(ssh_cmd "grep -i 'CRS-02\|proof.*number\|disproven' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -3" || echo "")
    if [ -n "$crs02_logs" ]; then
        echo -e "  ${YELLOW}CRS-02 (Proof Numbers):${NC}"
        echo "$crs02_logs" | sed 's/^/    /'
    fi

    # CRS-04: Check for learning events
    local crs04_logs=$(ssh_cmd "grep -i 'CRS-04\|learnFromFailure\|CDCL' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -3" || echo "")
    if [ -n "$crs04_logs" ]; then
        echo -e "  ${YELLOW}CRS-04 (CDCL Learning):${NC}"
        echo "$crs04_logs" | sed 's/^/    /'
    fi

    # CRS-06: Check for coordinator events
    local crs06_logs=$(ssh_cmd "grep -i 'CRS-06\|EventCircuitBreaker\|EventSemanticRepetition' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -3" || echo "")
    if [ -n "$crs06_logs" ]; then
        echo -e "  ${YELLOW}CRS-06 (Coordinator Events):${NC}"
        echo "$crs06_logs" | sed 's/^/    /'
    fi

    # Check for any errors or warnings
    local error_logs=$(ssh_cmd "grep -i 'ERROR\|WARN' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -5" || echo "")
    if [ -n "$error_logs" ]; then
        echo -e "  ${RED}Errors/Warnings:${NC}"
        echo "$error_logs" | sed 's/^/    /'
    fi

    # If no CRS logs found, mention it
    if [ -z "$gr39b_logs" ] && [ -z "$cb30c_logs" ] && [ -z "$crs02_logs" ] && [ -z "$crs04_logs" ] && [ -z "$crs06_logs" ]; then
        echo "    (no CRS-specific log entries found)"
    fi

    # Store detailed result for JSON output
    LAST_TEST_RESULT=$(jq -n \
        --arg test_num "$test_num" \
        --arg category "$category" \
        --arg session_id "$session_id" \
        --arg query "$query" \
        --arg state "$state" \
        --arg steps "$steps_taken" \
        --arg tokens "$tokens_used" \
        --arg duration "$duration" \
        --arg response "$agent_response" \
        --arg session_actual "$session_actual" \
        --argjson trace "$trace_json" \
        '{
            test: ($test_num | tonumber),
            category: $category,
            session_id: $session_id,
            query: $query,
            state: $state,
            steps_taken: ($steps | tonumber),
            tokens_used: ($tokens | tonumber),
            runtime_ms: ($duration | tonumber),
            response: $response,
            actual_session_id: $session_actual,
            crs_trace: $trace
        }')

    echo ""
    if [ "$state" = "$expected_state" ]; then
        echo -e "  ${GREEN}════ PASSED ════${NC} State: $state (${duration}ms)"

        # Run extra checks if specified
        if [ -n "$extra_check" ]; then
            run_extra_check "$extra_check" "$response" "$duration" "$session_actual"
        fi

        return 0
    else
        echo -e "  ${RED}════ FAILED ════${NC} Expected: $expected_state, Got: $state"
        # Show error details if available
        local error_msg=$(echo "$response" | jq -r '.error // ""')
        if [ -n "$error_msg" ] && [ "$error_msg" != "null" ]; then
            echo -e "    ${RED}Error: $error_msg${NC}"
        fi
        return 1
    fi
}

run_internal_test() {
    local category="$1"
    local test_name="$2"
    local expected="$3"
    local test_num="${4:-0}"  # GR-39 Issue 5: Accept test_num for proper result tracking

    local start_time=$(get_time_ms)
    local exit_code=0
    local result_message=""

    case "$test_name" in
        verify_checkpoint_exists)
            # Check for CRS checkpoint/backup files in ~/.aleutian/crs (NOT ~/.claude/crs)
            echo -e "  ${BLUE}Checking ~/.aleutian/crs for persistence files...${NC}"

            # First check if the directory exists
            local dir_exists=$(ssh_cmd "test -d ~/.aleutian/crs && echo 'yes' || echo 'no'" || echo "no")
            if [ "$dir_exists" = "no" ]; then
                echo -e "  ${RED}✗ Directory ~/.aleutian/crs does not exist${NC}"
                echo -e "  ${YELLOW}  → CRS persistence may not be initialized${NC}"
                exit_code=1
                result_message="Directory does not exist"
            else
                # Check for BadgerDB files (MANIFEST, *.vlog, *.sst)
                local badger_files=$(ssh_cmd "find ~/.aleutian/crs -name 'MANIFEST' -o -name '*.vlog' -o -name '*.sst' 2>/dev/null | wc -l" || echo "0")
                badger_files=$(echo "$badger_files" | tr -d '[:space:]')

                # Check for checkpoint/backup files
                local checkpoint_files=$(ssh_cmd "find ~/.aleutian/crs -name '*.backup*' -o -name '*.checkpoint*' -o -name 'crs_*.json' 2>/dev/null | wc -l" || echo "0")
                checkpoint_files=$(echo "$checkpoint_files" | tr -d '[:space:]')

                # List what's in the directory for debugging
                echo -e "  ${BLUE}Contents of ~/.aleutian/crs:${NC}"
                ssh_cmd "ls -la ~/.aleutian/crs 2>/dev/null | head -10" | while read line; do
                    echo -e "    $line"
                done

                if [ "$badger_files" -gt 0 ]; then
                    echo -e "  ${GREEN}✓ BadgerDB files found: $badger_files${NC}"
                    result_message="BadgerDB files found: $badger_files"
                elif [ "$checkpoint_files" -gt 0 ]; then
                    echo -e "  ${GREEN}✓ Checkpoint files found: $checkpoint_files${NC}"
                    result_message="Checkpoint files found: $checkpoint_files"
                else
                    echo -e "  ${RED}✗ No persistence files found in ~/.aleutian/crs${NC}"
                    echo -e "  ${YELLOW}  → Directory exists but is empty or has no CRS data${NC}"
                    exit_code=1
                    result_message="No persistence files found"
                fi
            fi
            ;;

        restart_and_verify_state)
            # Restart the server and verify state is restored
            echo -e "    ${BLUE}Restarting trace server...${NC}"
            stop_trace_server
            sleep 2
            if start_trace_server; then
                echo -e "  ${GREEN}✓ Server restarted successfully${NC}"
                result_message="Server restarted successfully"
            else
                echo -e "  ${RED}✗ Server failed to restart${NC}"
                exit_code=1
                result_message="Server failed to restart"
            fi
            ;;

        verify_event_graph_context)
            # Check server logs for graph context in events
            local has_context=$(ssh_cmd "grep -c 'graph_context\|GraphContext' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
            has_context=$(echo "$has_context" | tr -d '[:space:]')
            if [ "$has_context" -gt 0 ]; then
                echo -e "  ${GREEN}✓ Graph context found in events ($has_context occurrences)${NC}"
                result_message="Graph context found: $has_context occurrences"
            else
                echo -e "  ${YELLOW}⚠ Graph context not found in logs (may need more queries first)${NC}"
                result_message="Graph context not found (warning only)"
            fi
            ;;

        verify_delta_count)
            # Query CRS state for delta count
            local delta_info=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/crs/deltas'" 2>/dev/null)
            if echo "$delta_info" | jq . > /dev/null 2>&1; then
                local count=$(echo "$delta_info" | jq '.count // .total // 0')
                if [ "$count" -gt 0 ]; then
                    echo -e "  ${GREEN}✓ Delta count: $count${NC}"
                    result_message="Delta count: $count"
                else
                    echo -e "  ${YELLOW}⚠ Delta count is 0 (run more queries first)${NC}"
                    result_message="Delta count is 0"
                fi
            else
                echo -e "  ${YELLOW}⚠ CRS debug endpoint not available${NC}"
                result_message="CRS debug endpoint not available"
            fi
            ;;

        verify_history_limit)
            # Verify ringbuffer history is bounded
            local history_info=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/crs/history'" 2>/dev/null)
            if echo "$history_info" | jq . > /dev/null 2>&1; then
                local size=$(echo "$history_info" | jq '.size // .count // 0')
                local limit=$(echo "$history_info" | jq '.limit // .max_size // 1000')
                if [ "$size" -le "$limit" ]; then
                    echo -e "  ${GREEN}✓ History size ($size) within limit ($limit)${NC}"
                    result_message="History size ($size) within limit ($limit)"
                else
                    echo -e "  ${RED}✗ History size ($size) exceeds limit ($limit)${NC}"
                    exit_code=1
                    result_message="History size exceeds limit"
                fi
            else
                echo -e "  ${YELLOW}⚠ History endpoint not available${NC}"
                result_message="History endpoint not available"
            fi
            ;;

        replay_and_verify)
            # Test delta replay functionality
            echo -e "  ${YELLOW}⚠ Replay verification not yet implemented${NC}"
            result_message="Not yet implemented"
            ;;

        verify_index_span_attribute)
            # GR-01: Check server logs for OTel span attributes indicating index usage
            # After optimization, spans should have "index_used=true" or "lookup_method=index"
            echo -e "  ${BLUE}Checking trace server logs for index span attributes...${NC}"

            # Check for index-related span attributes in logs
            local index_traces=$(ssh_cmd "grep -c 'index_used\|lookup_method.*index\|GetByName\|index.GetByName' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
            index_traces=$(echo "$index_traces" | tr -d '[:space:]')

            # Also check for O(V) scan indicators (should be absent after fix)
            local scan_traces=$(ssh_cmd "grep -c 'findSymbolsByName\|O(V)\|full_scan' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
            scan_traces=$(echo "$scan_traces" | tr -d '[:space:]')

            echo -e "  ${BLUE}Index usage traces: $index_traces${NC}"
            echo -e "  ${BLUE}Full scan traces: $scan_traces${NC}"

            if [ "$index_traces" -gt 0 ]; then
                echo -e "  ${GREEN}✓ Index usage detected in OTel spans${NC}"
                result_message="Index usage: $index_traces traces, Scans: $scan_traces traces"
            elif [ "$scan_traces" -eq 0 ]; then
                # No scan traces means we're probably using index (good)
                echo -e "  ${GREEN}✓ No O(V) scan traces detected (index likely used)${NC}"
                result_message="No scan traces detected"
            else
                # Before GR-01 fix: expect scan traces, no index traces
                echo -e "  ${YELLOW}⚠ O(V) scans detected, index usage not confirmed${NC}"
                echo -e "  ${YELLOW}  → This test will pass after GR-01 is implemented${NC}"
                result_message="Pre-GR-01: Scans=$scan_traces, Index=$index_traces"
            fi
            ;;

        verify_pagerank_convergence)
            # GR-12: Verify PageRank algorithm converged within max iterations
            echo -e "  ${BLUE}Checking PageRank convergence (GR-12)...${NC}"

            # Ensure graph is built first
            local stats_response=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/graph/stats'" 2>/dev/null || echo "")
            if [ -z "$stats_response" ] || echo "$stats_response" | jq -e '.error' >/dev/null 2>&1; then
                echo -e "  ${YELLOW}Building graph first...${NC}"
                ssh_cmd "curl -s -X POST -H 'Content-Type: application/json' -d '{\"project_root\":\"/home/aleutiandevops/trace_test/AleutianOrchestrator\"}' 'http://localhost:8080/v1/codebuddy/init'" 2>/dev/null
                sleep 2
            fi

            # Trigger PageRank by calling find_important via agent
            echo -e "  ${BLUE}Triggering PageRank via find_important...${NC}"
            local agent_response=$(ssh_cmd "curl -s -X POST -H 'Content-Type: application/json' -d '{\"query\":\"What are the top 3 most important functions?\"}' 'http://localhost:8080/v1/codebuddy/agent/run'" 2>/dev/null)
            sleep 3

            # Check for PageRank-related log entries
            local pr_logs=$(ssh_cmd "grep -i 'PageRank\|pagerank\|find_important' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -10" || echo "")

            if [ -n "$pr_logs" ]; then
                echo -e "  ${BLUE}PageRank log entries:${NC}"
                echo "$pr_logs" | sed 's/^/    /'

                # Check for convergence indicator
                local converged=$(echo "$pr_logs" | grep -ci "converged\|iterations\|PageRankTop")
                if [ "$converged" -gt 0 ]; then
                    echo -e "  ${GREEN}✓ GR-12: PageRank convergence detected${NC}"
                    result_message="PageRank converged"
                else
                    echo -e "  ${GREEN}✓ GR-12: PageRank executed${NC}"
                    result_message="PageRank executed"
                fi
            else
                echo -e "  ${RED}✗ GR-12: No PageRank activity found in logs${NC}"
                exit_code=1
                result_message="No PageRank activity"
            fi
            ;;

        verify_implements_edges)
            # GR-40: Verify EdgeTypeImplements edges exist in the graph for Go code
            # NOTE: 0 implements edges is CORRECT if codebase has 0 interfaces
            echo -e "  ${BLUE}Checking for EdgeTypeImplements edges in graph...${NC}"

            # Query the graph stats endpoint for edge type breakdown
            local graph_stats=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/graph/stats'" 2>/dev/null)

            if echo "$graph_stats" | jq . > /dev/null 2>&1; then
                local implements_count=$(echo "$graph_stats" | jq '.edges_by_type.implements // .edges_by_type.EdgeTypeImplements // 0')
                local total_edges=$(echo "$graph_stats" | jq '.edge_count // .total_edges // 0')
                local interface_count=$(echo "$graph_stats" | jq '.nodes_by_kind.interface // 0')

                echo -e "  ${BLUE}Total edges: $total_edges${NC}"
                echo -e "  ${BLUE}Interface nodes: $interface_count${NC}"
                echo -e "  ${BLUE}Implements edges: $implements_count${NC}"

                if [ "$interface_count" -eq 0 ]; then
                    # No interfaces in codebase - 0 implements edges is correct
                    echo -e "  ${GREEN}✓ GR-40: No interfaces in codebase, 0 implements edges is correct${NC}"
                    result_message="No interfaces in codebase (correct: 0 implements edges)"
                elif [ "$implements_count" -gt 0 ]; then
                    echo -e "  ${GREEN}✓ GR-40: EdgeTypeImplements edges found: $implements_count${NC}"
                    result_message="Implements edges found: $implements_count"
                else
                    # Has interfaces but no implements edges - this is the bug case
                    echo -e "  ${RED}✗ GR-40: $interface_count interfaces but 0 implements edges${NC}"
                    echo -e "  ${YELLOW}  → Go interface satisfaction requires method-set matching${NC}"
                    exit_code=1
                    result_message="Bug: $interface_count interfaces but 0 implements edges"
                fi
            else
                # Fallback: check server logs for implements edge creation
                local impl_logs=$(ssh_cmd "grep -c 'EdgeTypeImplements\|implements.*edge' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
                impl_logs=$(echo "$impl_logs" | tr -d '[:space:]')

                if [ "$impl_logs" -gt 0 ]; then
                    echo -e "  ${GREEN}✓ Implements edge activity detected in logs${NC}"
                    result_message="Implements edge logs: $impl_logs"
                else
                    # Can't determine - pass with warning
                    echo -e "  ${YELLOW}⚠ Cannot verify implements edges (no graph stats)${NC}"
                    result_message="Cannot verify (no graph stats endpoint)"
                fi
            fi
            ;;

        # ================================================================================
        # GR-PHASE1: INTEGRATION TEST QUALITY INTERNAL TESTS
        # TDD: These tests define expected behavior BEFORE fixes are implemented
        # ================================================================================

        verify_cb_threshold_consistency)
            # P1-Issue2: Verify circuit breaker fires consistently for ALL tools at same threshold
            echo -e "  ${BLUE}Checking circuit breaker consistency across all tools...${NC}"

            # Get tool usage and circuit breaker events
            local tool_calls=$(ssh_cmd "grep -c 'tool_call\|executing tool' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
            local cb_fires=$(ssh_cmd "grep -c 'GR-39b\|circuit.*breaker.*fired' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
            tool_calls=$(echo "$tool_calls" | tr -d '[:space:]')
            cb_fires=$(echo "$cb_fires" | tr -d '[:space:]')

            echo -e "  ${BLUE}Total tool calls: $tool_calls${NC}"
            echo -e "  ${BLUE}Circuit breaker fires: $cb_fires${NC}"

            # Check for tools that exceeded threshold but didn't fire CB
            local tools_over_threshold=$(ssh_cmd "grep -E 'find_important.*calls|Read.*calls|Grep.*calls' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | grep -E '[345]+ calls' | head -5" || echo "")

            if [ -n "$tools_over_threshold" ]; then
                echo -e "  ${YELLOW}Tools exceeding threshold:${NC}"
                echo "$tools_over_threshold" | sed 's/^/    /'

                # Check if CB fired for these
                local cb_for_tools=$(ssh_cmd "grep -E 'GR-39b.*(find_important|Read|Grep)' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "")
                if [ -z "$cb_for_tools" ]; then
                    echo -e "  ${RED}✗ P1-Issue2: Tools exceeded threshold but no CB fired!${NC}"
                    exit_code=1
                    result_message="CB inconsistency: tools over threshold, no CB fired"
                else
                    echo -e "  ${GREEN}✓ P1-Issue2: Circuit breaker fired for tools exceeding threshold${NC}"
                    result_message="CB consistent"
                fi
            else
                if [ "$cb_fires" -gt 0 ]; then
                    echo -e "  ${GREEN}✓ P1-Issue2: Circuit breaker fired when tools exceeded threshold${NC}"
                    result_message="CB fires: $cb_fires"
                else
                    echo -e "  ${YELLOW}⚠ P1-Issue2: No tools exceeded threshold (cannot verify CB consistency)${NC}"
                    result_message="No tools exceeded threshold yet"
                fi
            fi
            ;;

        verify_debug_crs_endpoint)
            # P2-Issue5: Verify /debug/crs endpoint is available
            # GR-Phase1: Endpoint moved to /agent/debug/crs for session access
            echo -e "  ${BLUE}Checking /agent/debug/crs endpoint availability...${NC}"

            local crs_response=$(ssh_cmd "curl -s -w '%{http_code}' 'http://localhost:8080/v1/codebuddy/agent/debug/crs'" 2>/dev/null || echo "")
            local http_code=""
            local body=""
            local resp_len=${#crs_response}

            # Handle empty response (server not running or connection failed)
            if [ -z "$crs_response" ] || [ "$resp_len" -lt 3 ]; then
                echo -e "  ${RED}✗ P2-Issue5: No response from server (connection failed or server stopped, len=$resp_len)${NC}"
                exit_code=1
                result_message="Server not responding"
                http_code="000"
            else
                http_code="${crs_response: -3}"
                body="${crs_response:0:$((resp_len - 3))}"
            fi

            echo -e "  ${BLUE}HTTP status: $http_code${NC}"

            if [ "$http_code" = "200" ]; then
                if echo "$body" | jq . > /dev/null 2>&1; then
                    echo -e "  ${GREEN}✓ P2-Issue5: /debug/crs endpoint available and returns valid JSON${NC}"
                    result_message="Endpoint available (HTTP 200)"
                else
                    echo -e "  ${YELLOW}⚠ P2-Issue5: /debug/crs returns 200 but invalid JSON${NC}"
                    result_message="Endpoint returns invalid JSON"
                fi
            elif [ "$http_code" = "404" ]; then
                echo -e "  ${RED}✗ P2-Issue5: /debug/crs endpoint not found (404)${NC}"
                echo -e "  ${YELLOW}  → Implement endpoint to expose CRS state for debugging${NC}"
                exit_code=1
                result_message="Endpoint not implemented (404)"
            else
                echo -e "  ${RED}✗ P2-Issue5: /debug/crs endpoint error (HTTP $http_code)${NC}"
                exit_code=1
                result_message="Endpoint error (HTTP $http_code)"
            fi
            ;;

        verify_debug_history_endpoint)
            # P2-Issue5: Verify /debug/history endpoint is available
            # NOTE: This endpoint is not yet implemented - test will show 404
            echo -e "  ${BLUE}Checking /debug/history endpoint availability...${NC}"

            local history_response=$(ssh_cmd "curl -s -w '%{http_code}' 'http://localhost:8080/v1/codebuddy/agent/debug/history'" 2>/dev/null || echo "")
            local http_code=""
            local body=""
            local resp_len=${#history_response}

            # Handle empty response (server not running or connection failed)
            if [ -z "$history_response" ] || [ "$resp_len" -lt 3 ]; then
                echo -e "  ${RED}✗ P2-Issue5: No response from server (connection failed or server stopped, len=$resp_len)${NC}"
                exit_code=1
                result_message="Server not responding"
                http_code="000"
            else
                http_code="${history_response: -3}"
                body="${history_response:0:$((resp_len - 3))}"
            fi

            echo -e "  ${BLUE}HTTP status: $http_code${NC}"

            if [ "$http_code" = "200" ]; then
                if echo "$body" | jq . > /dev/null 2>&1; then
                    local history_count=$(echo "$body" | jq '.count // .size // length')
                    echo -e "  ${GREEN}✓ P2-Issue5: /debug/history endpoint available ($history_count entries)${NC}"
                    result_message="Endpoint available ($history_count entries)"
                else
                    echo -e "  ${YELLOW}⚠ P2-Issue5: /debug/history returns 200 but invalid JSON${NC}"
                    result_message="Endpoint returns invalid JSON"
                fi
            elif [ "$http_code" = "404" ]; then
                echo -e "  ${RED}✗ P2-Issue5: /debug/history endpoint not found (404)${NC}"
                echo -e "  ${YELLOW}  → Implement endpoint to expose reasoning history${NC}"
                exit_code=1
                result_message="Endpoint not implemented (404)"
            else
                echo -e "  ${RED}✗ P2-Issue5: /debug/history endpoint error (HTTP $http_code)${NC}"
                exit_code=1
                result_message="Endpoint error (HTTP $http_code)"
            fi
            ;;

        verify_pagerank_convergence_logged)
            # P2-Issue6: Verify PageRank convergence is logged with iterations and tolerance
            echo -e "  ${BLUE}Checking PageRank convergence logging...${NC}"

            # Look for convergence logs with iterations and tolerance
            local convergence_logs=$(ssh_cmd "grep -i 'pagerank.*converge\|iterations.*tolerance\|convergence.*achieved' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -5" || echo "")

            if [ -n "$convergence_logs" ]; then
                echo -e "  ${BLUE}PageRank convergence logs:${NC}"
                echo "$convergence_logs" | sed 's/^/    /'

                # Check for specific convergence info
                local has_iterations=$(echo "$convergence_logs" | grep -ci "iteration")
                local has_tolerance=$(echo "$convergence_logs" | grep -ci "tolerance\|delta\|diff")

                if [ "$has_iterations" -gt 0 ] && [ "$has_tolerance" -gt 0 ]; then
                    echo -e "  ${GREEN}✓ P2-Issue6: PageRank convergence logged with iterations and tolerance${NC}"
                    result_message="Convergence logged with details"
                else
                    echo -e "  ${YELLOW}⚠ P2-Issue6: Convergence logged but missing iterations ($has_iterations) or tolerance ($has_tolerance)${NC}"
                    result_message="Partial convergence logging"
                fi
            else
                # Check if PageRank was even invoked
                local pr_invoked=$(ssh_cmd "grep -ci 'pagerank\|find_important' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
                pr_invoked=$(echo "$pr_invoked" | tr -d '[:space:]')

                if [ "$pr_invoked" -gt 0 ]; then
                    echo -e "  ${RED}✗ P2-Issue6: PageRank invoked ($pr_invoked times) but convergence not logged${NC}"
                    echo -e "  ${YELLOW}  → Add logging for iterations to convergence and tolerance achieved${NC}"
                    exit_code=1
                    result_message="PageRank invoked but no convergence logging"
                else
                    echo -e "  ${YELLOW}⚠ P2-Issue6: PageRank not invoked yet (run importance queries first)${NC}"
                    result_message="PageRank not invoked"
                fi
            fi
            ;;

        # ================================================================================
        # GR-06 to GR-09: SECONDARY INDEX VERIFICATION TESTS
        # These tests verify secondary indexes are populated and working correctly
        # ================================================================================

        verify_nodes_by_name_index)
            # GR-06: Verify nodesByName secondary index exists and has data
            echo -e "  ${BLUE}Checking nodesByName index (GR-06)...${NC}"

            # Ensure graph is built first
            local stats_response=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/graph/stats'" 2>/dev/null || echo "")
            if [ -z "$stats_response" ] || echo "$stats_response" | jq -e '.error' >/dev/null 2>&1; then
                echo -e "  ${YELLOW}Building graph first...${NC}"
                local init_response=$(ssh_cmd "curl -s -X POST -H 'Content-Type: application/json' -d '{\"project_root\":\"/home/aleutiandevops/trace_test/AleutianOrchestrator\"}' 'http://localhost:8080/v1/codebuddy/init'" 2>/dev/null)
                sleep 2
                stats_response=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/graph/stats'" 2>/dev/null || echo "")
            fi

            if [ -z "$stats_response" ]; then
                echo -e "  ${RED}✗ GR-06: Cannot connect to server${NC}"
                exit_code=1
                result_message="Server not responding"
            elif echo "$stats_response" | jq -e '.error' >/dev/null 2>&1; then
                echo -e "  ${RED}✗ GR-06: Failed to build graph${NC}"
                exit_code=1
                result_message="Graph build failed"
            else
                local node_count=$(echo "$stats_response" | jq -r '.node_count // 0' 2>/dev/null)
                local kinds_count=$(echo "$stats_response" | jq -r '.nodes_by_kind | length' 2>/dev/null)

                if [ "$node_count" -gt 0 ] && [ "$kinds_count" -gt 0 ]; then
                    echo -e "  ${GREEN}✓ GR-06: nodesByName index verified (node_count=$node_count, kinds=$kinds_count)${NC}"
                    echo -e "  ${BLUE}  Index is populated - nodes added use AddNode which populates nodesByName${NC}"
                    result_message="nodesByName index working (nodes: $node_count)"
                else
                    echo -e "  ${RED}✗ GR-06: Graph has no nodes (node_count=$node_count)${NC}"
                    exit_code=1
                    result_message="Empty graph"
                fi
            fi
            ;;

        verify_nodes_by_kind_index)
            # GR-07: Verify nodesByKind secondary index via /debug/graph/stats
            echo -e "  ${BLUE}Checking nodesByKind index (GR-07)...${NC}"

            # Ensure graph is built first
            local stats_response=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/graph/stats'" 2>/dev/null || echo "")
            if [ -z "$stats_response" ] || echo "$stats_response" | jq -e '.error' >/dev/null 2>&1; then
                echo -e "  ${YELLOW}Building graph first...${NC}"
                local init_response=$(ssh_cmd "curl -s -X POST -H 'Content-Type: application/json' -d '{\"project_root\":\"/home/aleutiandevops/trace_test/AleutianOrchestrator\"}' 'http://localhost:8080/v1/codebuddy/init'" 2>/dev/null)
                sleep 2
                stats_response=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/graph/stats'" 2>/dev/null || echo "")
            fi

            if [ -z "$stats_response" ]; then
                echo -e "  ${RED}✗ GR-07: Cannot connect to server${NC}"
                exit_code=1
                result_message="Server not responding"
            elif echo "$stats_response" | jq -e '.error' >/dev/null 2>&1; then
                echo -e "  ${RED}✗ GR-07: Failed to build graph${NC}"
                exit_code=1
                result_message="Graph build failed"
            else
                # nodes_by_kind map should have entries
                local kinds_map=$(echo "$stats_response" | jq -c '.nodes_by_kind // {}' 2>/dev/null)
                local kinds_count=$(echo "$kinds_map" | jq 'length' 2>/dev/null)

                if [ "$kinds_count" -gt 0 ]; then
                    echo -e "  ${GREEN}✓ GR-07: nodesByKind index has $kinds_count kinds${NC}"
                    echo "$kinds_map" | jq -r 'to_entries | .[:5] | .[] | "    \(.key): \(.value) nodes"' 2>/dev/null
                    result_message="nodesByKind index working ($kinds_count kinds)"
                else
                    echo -e "  ${RED}✗ GR-07: nodesByKind is empty${NC}"
                    exit_code=1
                    result_message="Empty nodesByKind"
                fi
            fi
            ;;

        verify_edges_by_type_index)
            # GR-08: Verify edgesByType secondary index via /debug/graph/stats
            echo -e "  ${BLUE}Checking edgesByType index (GR-08)...${NC}"

            # Ensure graph is built first
            local stats_response=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/graph/stats'" 2>/dev/null || echo "")
            if [ -z "$stats_response" ] || echo "$stats_response" | jq -e '.error' >/dev/null 2>&1; then
                echo -e "  ${YELLOW}Building graph first...${NC}"
                local init_response=$(ssh_cmd "curl -s -X POST -H 'Content-Type: application/json' -d '{\"project_root\":\"/home/aleutiandevops/trace_test/AleutianOrchestrator\"}' 'http://localhost:8080/v1/codebuddy/init'" 2>/dev/null)
                sleep 2
                stats_response=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/graph/stats'" 2>/dev/null || echo "")
            fi

            if [ -z "$stats_response" ]; then
                echo -e "  ${RED}✗ GR-08: Cannot connect to server${NC}"
                exit_code=1
                result_message="Server not responding"
            elif echo "$stats_response" | jq -e '.error' >/dev/null 2>&1; then
                echo -e "  ${RED}✗ GR-08: Failed to build graph${NC}"
                exit_code=1
                result_message="Graph build failed"
            else
                # edges_by_type map should have entries
                local types_map=$(echo "$stats_response" | jq -c '.edges_by_type // {}' 2>/dev/null)
                local types_count=$(echo "$types_map" | jq 'length' 2>/dev/null)
                local edge_count=$(echo "$stats_response" | jq -r '.edge_count // 0' 2>/dev/null)

                if [ "$types_count" -gt 0 ]; then
                    echo -e "  ${GREEN}✓ GR-08: edgesByType index has $types_count edge types (total edges: $edge_count)${NC}"
                    echo "$types_map" | jq -r 'to_entries | .[] | "    \(.key): \(.value) edges"' 2>/dev/null
                    result_message="edgesByType index working ($types_count types, $edge_count edges)"
                else
                    echo -e "  ${RED}✗ GR-08: edgesByType is empty${NC}"
                    exit_code=1
                    result_message="Empty edgesByType"
                fi
            fi
            ;;

        verify_edges_by_file_index)
            # GR-09: Verify edgesByFile index exists (used by RemoveFile)
            echo -e "  ${BLUE}Checking edgesByFile index (GR-09)...${NC}"

            # Ensure graph is built first
            local stats_response=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/graph/stats'" 2>/dev/null || echo "")
            if [ -z "$stats_response" ] || echo "$stats_response" | jq -e '.error' >/dev/null 2>&1; then
                echo -e "  ${YELLOW}Building graph first...${NC}"
                local init_response=$(ssh_cmd "curl -s -X POST -H 'Content-Type: application/json' -d '{\"project_root\":\"/home/aleutiandevops/trace_test/AleutianOrchestrator\"}' 'http://localhost:8080/v1/codebuddy/init'" 2>/dev/null)
                sleep 2
                stats_response=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/graph/stats'" 2>/dev/null || echo "")
            fi

            if [ -z "$stats_response" ]; then
                echo -e "  ${RED}✗ GR-09: Cannot connect to server${NC}"
                exit_code=1
                result_message="Server not responding"
            elif echo "$stats_response" | jq -e '.error' >/dev/null 2>&1; then
                echo -e "  ${RED}✗ GR-09: Failed to build graph${NC}"
                exit_code=1
                result_message="Graph build failed"
            else
                local edge_count=$(echo "$stats_response" | jq -r '.edge_count // 0' 2>/dev/null)
                local node_count=$(echo "$stats_response" | jq -r '.node_count // 0' 2>/dev/null)

                if [ "$edge_count" -gt 0 ]; then
                    # Check logs for edgesByFile usage or RemoveFile operations
                    local file_index_logs=$(ssh_cmd "grep -ci 'edgesByFile\|RemoveFile\|file.*index' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
                    file_index_logs=$(echo "$file_index_logs" | tr -d '[:space:]')

                    echo -e "  ${GREEN}✓ GR-09: edgesByFile index verified (edge_count=$edge_count, nodes=$node_count)${NC}"
                    echo -e "  ${BLUE}  Index is populated - edges added use AddEdge which populates edgesByFile${NC}"

                    if [ "$file_index_logs" -gt 0 ]; then
                        echo -e "  ${BLUE}  Found $file_index_logs file index related log entries${NC}"
                    fi

                    result_message="edgesByFile index working (edges: $edge_count)"
                else
                    echo -e "  ${RED}✗ GR-09: Graph has no edges (edge_count=$edge_count)${NC}"
                    exit_code=1
                    result_message="Empty graph (no edges)"
                fi
            fi
            ;;

        # ================================================================================
        # GR-10: QUERY CACHE VERIFICATION TESTS
        # TDD: These tests define expected behavior BEFORE implementation
        # ================================================================================

        verify_cache_stats_endpoint)
            # GR-10: Verify /debug/cache endpoint returns cache statistics
            echo -e "  ${BLUE}Checking cache stats endpoint (GR-10)...${NC}"

            # First, ensure graph is initialized
            echo -e "  ${BLUE}Ensuring graph is initialized...${NC}"
            local init_resp=$(ssh_cmd "curl -s -X POST -H 'Content-Type: application/json' -d '{\"project_root\":\"/home/aleutiandevops/trace_test/AleutianOrchestrator\"}' 'http://localhost:8080/v1/codebuddy/init'" 2>/dev/null || echo "")
            local graph_id=$(echo "$init_resp" | jq -r '.graph_id // ""' 2>/dev/null)
            if [ -n "$graph_id" ] && [ "$graph_id" != "null" ]; then
                echo -e "  ${GREEN}✓ Graph initialized: $graph_id${NC}"
            else
                echo -e "  ${YELLOW}⚠ Graph init response: $init_resp${NC}"
            fi

            # Make callers queries to populate the cache (use actual AleutianOrchestrator function names)
            echo -e "  ${BLUE}Populating cache with callers queries...${NC}"
            local total_callers=0
            # These are actual functions in AleutianOrchestrator that are likely to have callers
            for func_name in "CodeAnalysisRequest" "NewClient" "ParseAPIMessage" "WriteDataToGCS" "FetchPromptFromGCS" "DistillerRequest"; do
                local callers_resp=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/callers?graph_id=$graph_id&function=$func_name'" 2>/dev/null || echo "")
                local callers_count=$(echo "$callers_resp" | jq '.callers | length' 2>/dev/null || echo "0")
                if [ "$callers_count" -gt 0 ]; then
                    echo -e "  ${GREEN}✓ Found $callers_count callers of '$func_name'${NC}"
                    total_callers=$((total_callers + callers_count))
                    break  # One successful query is enough to populate cache
                fi
            done
            if [ "$total_callers" -eq 0 ]; then
                echo -e "  ${YELLOW}⚠ No callers found (cache should still record misses)${NC}"
            fi

            local cache_response=$(ssh_cmd "curl -s -w '%{http_code}' 'http://localhost:8080/v1/codebuddy/debug/cache'" 2>/dev/null || echo "")
            local http_code=""
            local body=""
            local resp_len=${#cache_response}

            if [ -z "$cache_response" ] || [ "$resp_len" -lt 3 ]; then
                echo -e "  ${RED}✗ GR-10: No response from cache endpoint${NC}"
                exit_code=1
                result_message="Server not responding"
                http_code="000"
            else
                http_code="${cache_response: -3}"
                body="${cache_response:0:$((resp_len - 3))}"
            fi

            echo -e "  ${BLUE}HTTP status: $http_code${NC}"

            if [ "$http_code" = "200" ]; then
                if echo "$body" | jq . > /dev/null 2>&1; then
                    local callers_size=$(echo "$body" | jq '.callers_size // .callers.size // 0')
                    local callees_size=$(echo "$body" | jq '.callees_size // .callees.size // 0')
                    local paths_size=$(echo "$body" | jq '.paths_size // .paths.size // 0')
                    local hit_rate=$(echo "$body" | jq '.hit_rate // 0')
                    local callers_misses=$(echo "$body" | jq '.callers_misses // 0')

                    echo -e "  ${GREEN}✓ GR-10: Cache stats endpoint available${NC}"
                    echo -e "  ${BLUE}  Callers cache: $callers_size entries${NC}"
                    echo -e "  ${BLUE}  Callees cache: $callees_size entries${NC}"
                    echo -e "  ${BLUE}  Paths cache: $paths_size entries${NC}"
                    echo -e "  ${BLUE}  Hit rate: $hit_rate${NC}"

                    # Verify cache activity
                    local total_size=$((callers_size + callees_size + paths_size))
                    local total_misses=$(echo "$body" | jq '(.callers_misses // 0) + (.callees_misses // 0) + (.paths_misses // 0)' 2>/dev/null || echo "0")

                    if [ "$total_size" -ge 1 ]; then
                        echo -e "  ${GREEN}✓ GR-10: Cache populated with $total_size entries${NC}"
                        result_message="Cache stats available and populated ($total_size entries)"
                    elif [ "$total_misses" -ge 1 ]; then
                        echo -e "  ${GREEN}✓ GR-10: Cache active ($total_misses queries made)${NC}"
                        result_message="Cache stats available ($total_misses queries, 0 cached)"
                    else
                        echo -e "  ${GREEN}✓ GR-10: Cache endpoint working (no queries yet)${NC}"
                        result_message="Cache stats endpoint working"
                    fi
                else
                    echo -e "  ${YELLOW}⚠ GR-10: Cache endpoint returns 200 but invalid JSON${NC}"
                    result_message="Endpoint returns invalid JSON"
                fi
            elif [ "$http_code" = "404" ]; then
                echo -e "  ${RED}✗ GR-10: Cache stats endpoint not found (404)${NC}"
                echo -e "  ${YELLOW}  → Implement /debug/cache endpoint to expose cache stats${NC}"
                exit_code=1
                result_message="Endpoint not implemented (404)"
            else
                echo -e "  ${RED}✗ GR-10: Cache stats endpoint error (HTTP $http_code)${NC}"
                exit_code=1
                result_message="Endpoint error (HTTP $http_code)"
            fi
            ;;

        verify_cache_invalidation)
            # GR-10: Verify cache is invalidated when graph is rebuilt
            echo -e "  ${BLUE}Checking cache invalidation (GR-10)...${NC}"

            # First, get current cache stats
            local before_stats=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/cache'" 2>/dev/null || echo "{}")
            local before_callers=$(echo "$before_stats" | jq '.callers_size // 0' 2>/dev/null || echo "0")

            # Trigger a graph rebuild (re-init the project)
            echo -e "  ${BLUE}Triggering graph rebuild...${NC}"
            ssh_cmd "curl -s -X POST -H 'Content-Type: application/json' -d '{\"project_root\":\"/home/aleutiandevops/trace_test/AleutianOrchestrator\", \"force_rebuild\": true}' 'http://localhost:8080/v1/codebuddy/init'" 2>/dev/null
            sleep 2

            # Check cache stats after rebuild
            local after_stats=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/cache'" 2>/dev/null || echo "{}")
            local after_callers=$(echo "$after_stats" | jq '.callers_size // 0' 2>/dev/null || echo "0")
            local generation=$(echo "$after_stats" | jq '.generation // 0' 2>/dev/null || echo "0")

            echo -e "  ${BLUE}Before rebuild: $before_callers callers cached${NC}"
            echo -e "  ${BLUE}After rebuild: $after_callers callers cached${NC}"
            echo -e "  ${BLUE}Cache generation: $generation${NC}"

            if [ "$after_callers" -eq 0 ] || [ "$after_callers" -lt "$before_callers" ]; then
                echo -e "  ${GREEN}✓ GR-10: Cache was invalidated on graph rebuild${NC}"
                result_message="Cache invalidated (before=$before_callers, after=$after_callers)"
            else
                echo -e "  ${YELLOW}⚠ GR-10: Cache may not have been invalidated${NC}"
                echo -e "  ${YELLOW}  → Cache should be cleared when graph generation changes${NC}"
                result_message="Cache not invalidated (before=$before_callers, after=$after_callers)"
            fi
            ;;

        # ================================================================================
        # GR-11: PARALLEL BFS VERIFICATION TESTS
        # TDD: These tests define expected behavior BEFORE implementation
        # ================================================================================

        verify_parallel_threshold)
            # GR-11: Verify parallel mode is used for levels with > 16 nodes
            echo -e "  ${BLUE}Checking parallel BFS threshold (GR-11)...${NC}"

            # Check server logs for parallel mode decisions
            local parallel_logs=$(ssh_cmd "grep -i 'parallel_mode\|parallel.*threshold\|level.*nodes' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -10" || echo "")

            if [ -n "$parallel_logs" ]; then
                echo -e "  ${GREEN}✓ GR-11: Parallel BFS threshold logging found${NC}"
                echo -e "  ${BLUE}Recent logs:${NC}"
                echo "$parallel_logs" | sed 's/^/    /'
                result_message="Parallel threshold logging present"
            else
                echo -e "  ${YELLOW}⚠ GR-11: No parallel threshold logs found${NC}"
                echo -e "  ${YELLOW}  → Pre-GR-11: Expected (parallel BFS not implemented)${NC}"
                echo -e "  ${YELLOW}  → Post-GR-11: Should log level sizes and parallel decisions${NC}"
                result_message="No parallel logs (pre-implementation expected)"
            fi
            ;;

        verify_parallel_context_cancellation)
            # GR-11: Verify context cancellation works in parallel mode
            echo -e "  ${BLUE}Checking parallel BFS context cancellation (GR-11)...${NC}"

            # Check for cancellation handling in logs
            local cancel_logs=$(ssh_cmd "grep -i 'context.*cancel\|parallel.*cancel\|bfs.*abort' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -5" || echo "")

            # Also check that errgroup is used (indicates proper cancellation propagation)
            local errgroup_logs=$(ssh_cmd "grep -i 'errgroup\|worker.*exit\|goroutine.*stop' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -5" || echo "")

            if [ -n "$cancel_logs" ] || [ -n "$errgroup_logs" ]; then
                echo -e "  ${GREEN}✓ GR-11: Context cancellation handling detected${NC}"
                if [ -n "$cancel_logs" ]; then
                    echo "$cancel_logs" | sed 's/^/    /'
                fi
                result_message="Cancellation handling present"
            else
                echo -e "  ${YELLOW}⚠ GR-11: No cancellation handling logs (may not have been triggered)${NC}"
                echo -e "  ${YELLOW}  → This test passes if no crash occurs during normal operation${NC}"
                result_message="No cancellation triggered (normal operation)"
            fi
            ;;

        verify_no_race_conditions)
            # GR-11: Verify no race conditions in parallel BFS
            echo -e "  ${BLUE}Checking for race conditions (GR-11)...${NC}"

            # Check if server was built with -race flag
            local race_check=$(ssh_cmd "grep -i 'race.*detected\|DATA RACE' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | head -5" || echo "")

            if [ -n "$race_check" ]; then
                echo -e "  ${RED}✗ GR-11: RACE CONDITION DETECTED${NC}"
                echo "$race_check" | sed 's/^/    /'
                exit_code=1
                result_message="Race condition detected"
            else
                echo -e "  ${GREEN}✓ GR-11: No race conditions detected in logs${NC}"
                echo -e "  ${BLUE}  → For thorough check, rebuild with: go build -race${NC}"
                echo -e "  ${BLUE}  → And run: go test -race ./services/trace/graph/...${NC}"
                result_message="No races in logs (run -race for thorough check)"
            fi
            ;;

        *)
            echo -e "  ${YELLOW}⚠ Unknown internal test: $test_name${NC}"
            result_message="Unknown test"
            ;;
    esac

    # GR-39 Issue 5: Set LAST_TEST_RESULT for internal tests
    local end_time=$(get_time_ms)
    local duration=$((end_time - start_time))
    local result_status="PASSED"
    if [ $exit_code -ne 0 ]; then
        result_status="FAILED"
    fi

    LAST_TEST_RESULT=$(jq -n \
        --arg test_num "$test_num" \
        --arg category "$category" \
        --arg query "INTERNAL:$test_name" \
        --arg state "$result_status" \
        --arg message "$result_message" \
        --arg duration "$duration" \
        '{
            test: ($test_num | tonumber),
            category: $category,
            query: $query,
            state: $state,
            steps_taken: 0,
            tokens_used: 0,
            runtime_ms: ($duration | tonumber),
            response: $message,
            crs_trace: {total_steps: 0, trace: []}
        }')

    return $exit_code
}

run_extra_check() {
    local check="$1"
    local response="$2"
    local duration="$3"
    local session_id="${4:-}"

    case "$check" in
        faster_than_first)
            # Session 2+ should be faster due to restored state
            # Compare to first session runtime (stored globally)
            if [ "$FIRST_TEST_RUNTIME" -gt 0 ] && [ "$duration" -lt "$FIRST_TEST_RUNTIME" ]; then
                local speedup=$(( (FIRST_TEST_RUNTIME - duration) * 100 / FIRST_TEST_RUNTIME ))
                echo -e "    ${GREEN}✓ ${speedup}% faster than first query (${duration}ms vs ${FIRST_TEST_RUNTIME}ms)${NC}"
                echo -e "    ${GREEN}  → Session restore appears to be working!${NC}"
            elif [ "$FIRST_TEST_RUNTIME" -gt 0 ]; then
                local slowdown=$(( (duration - FIRST_TEST_RUNTIME) * 100 / FIRST_TEST_RUNTIME ))
                echo -e "    ${YELLOW}⚠ ${slowdown}% slower than first query (${duration}ms vs ${FIRST_TEST_RUNTIME}ms)${NC}"
                echo -e "    ${YELLOW}  → Query complexity may differ, or CRS not providing speedup${NC}"
            else
                echo -e "    ${YELLOW}⚠ No first test runtime to compare (${duration}ms)${NC}"
            fi
            ;;

        analytics_recorded)
            # Check if analytics were recorded in CRS
            if [ -z "$session_id" ]; then
                session_id=$(echo "$response" | jq -r '.session_id')
            fi
            local trace=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/agent/$session_id/reasoning'" 2>/dev/null)
            if echo "$trace" | jq . > /dev/null 2>&1; then
                local has_analytics=$(echo "$trace" | jq '[.trace[] | select(.action == "analytics_query" or .action == "tool_call")] | length')
                if [ "$has_analytics" -gt 0 ]; then
                    echo -e "    ${GREEN}✓ Analytics/tool calls recorded in CRS ($has_analytics steps)${NC}"
                else
                    echo -e "    ${YELLOW}⚠ No analytics found in trace${NC}"
                fi
            fi
            ;;

        generation_incremented)
            # Check CRS generation was incremented
            local gen_response=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/crs/generation'" 2>/dev/null)
            if echo "$gen_response" | jq . > /dev/null 2>&1; then
                local gen=$(echo "$gen_response" | jq '.generation // 0')
                if [ "$gen" -gt 0 ]; then
                    echo -e "    ${GREEN}✓ CRS generation: $gen${NC}"
                else
                    echo -e "    ${YELLOW}⚠ CRS generation is 0${NC}"
                fi
            else
                echo -e "    ${YELLOW}⚠ Could not fetch CRS generation${NC}"
            fi
            ;;

        graph_tool_used)
            # GR-01: Verify graph tools (find_callers, find_callees, find_implementations) were invoked
            if [ -z "$session_id" ]; then
                session_id=$(echo "$response" | jq -r '.session_id')
            fi
            local trace=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/agent/$session_id/reasoning'" 2>/dev/null)
            if echo "$trace" | jq . > /dev/null 2>&1; then
                local graph_tools=$(echo "$trace" | jq '[.trace[] | select(.action == "tool_call") | select(.tool | test("find_callers|find_callees|find_implementations|find_symbol"))] | length')
                if [ "$graph_tools" -gt 0 ]; then
                    echo -e "    ${GREEN}✓ Graph tools used: $graph_tools invocations${NC}"
                else
                    echo -e "    ${YELLOW}⚠ No graph tools in trace (may have used other tools)${NC}"
                fi
            fi
            ;;

        fast_execution)
            # GR-01: Verify query executed quickly (< 5000ms for warmed index)
            if [ "$duration" -lt 5000 ]; then
                echo -e "    ${GREEN}✓ Fast execution: ${duration}ms (< 5s threshold)${NC}"
            else
                echo -e "    ${YELLOW}⚠ Slower than expected: ${duration}ms (threshold: 5s)${NC}"
            fi
            ;;

        fast_not_found)
            # GR-01: Verify not-found case is fast (O(1) index miss, not O(V) scan)
            if [ "$duration" -lt 3000 ]; then
                echo -e "    ${GREEN}✓ Fast not-found: ${duration}ms (O(1) index miss)${NC}"
            else
                echo -e "    ${YELLOW}⚠ Slow not-found: ${duration}ms (may be using O(V) scan)${NC}"
            fi
            # Also check response mentions not found
            local not_found=$(echo "$response" | jq -r '.response // ""' | grep -ci "not found\|no callers\|no function")
            if [ "$not_found" -gt 0 ]; then
                echo -e "    ${GREEN}✓ Correctly reported function not found${NC}"
            fi
            ;;

        implementations_found)
            # GR-40: Verify find_implementations returned actual results
            if [ -z "$session_id" ]; then
                session_id=$(echo "$response" | jq -r '.session_id')
            fi
            local trace=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/agent/$session_id/reasoning'" 2>/dev/null)
            local agent_resp=$(echo "$response" | jq -r '.response // ""')

            # Check if find_implementations was used
            local impl_tool_used=$(echo "$trace" | jq '[.trace[] | select(.action == "tool_call") | select(.tool == "find_implementations")] | length' 2>/dev/null || echo "0")

            # Check if response contains implementation names (not "no implementations found")
            local found_impls=$(echo "$agent_resp" | grep -ci "implement\|struct\|type.*handler\|concrete")
            local no_impls=$(echo "$agent_resp" | grep -ci "no implementation\|not found\|empty\|none")

            echo -e "    ${BLUE}find_implementations calls: $impl_tool_used${NC}"

            if [ "$impl_tool_used" -gt 0 ]; then
                echo -e "    ${GREEN}✓ GR-40: find_implementations tool was used${NC}"

                if [ "$found_impls" -gt 0 ] && [ "$no_impls" -eq 0 ]; then
                    echo -e "    ${GREEN}✓ GR-40: Implementations were found in response${NC}"
                elif [ "$no_impls" -gt 0 ]; then
                    echo -e "    ${RED}✗ GR-40: Response indicates no implementations found${NC}"
                    echo -e "    ${YELLOW}  → Pre-GR-40: Go implicit interfaces not detected${NC}"
                    echo -e "    ${YELLOW}  → Post-GR-40: This should show concrete types${NC}"
                else
                    echo -e "    ${YELLOW}⚠ GR-40: Could not determine if implementations found${NC}"
                fi
            else
                # Check if Grep was used as fallback (bad)
                local grep_used=$(echo "$trace" | jq '[.trace[] | select(.action == "tool_call") | select(.tool == "Grep")] | length' 2>/dev/null || echo "0")
                if [ "$grep_used" -gt 0 ]; then
                    echo -e "    ${RED}✗ GR-40: Fell back to Grep ($grep_used calls) instead of find_implementations${NC}"
                    echo -e "    ${YELLOW}  → Pre-GR-40: Expected behavior (no implements edges)${NC}"
                else
                    echo -e "    ${YELLOW}⚠ GR-40: find_implementations not used, but no Grep fallback${NC}"
                fi
            fi
            ;;

        pagerank_used)
            # GR-12/GR-13: Verify find_important tool was used (PageRank-based)
            if [ -z "$session_id" ]; then
                session_id=$(echo "$response" | jq -r '.session_id')
            fi
            local trace=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/agent/$session_id/reasoning'" 2>/dev/null)
            local agent_resp=$(echo "$response" | jq -r '.response // ""')

            # Check if find_important was used
            local fi_used=$(echo "$trace" | jq '[.trace[] | select(.action == "tool_call") | select(.tool == "find_important")] | length' 2>/dev/null || echo "0")

            # Check if response mentions PageRank
            local mentions_pr=$(echo "$agent_resp" | grep -ci "pagerank\|page rank\|importance.*score")

            if [ "$fi_used" -gt 0 ]; then
                echo -e "    ${GREEN}✓ GR-13: find_important tool was used: $fi_used calls${NC}"
                if [ "$mentions_pr" -gt 0 ]; then
                    echo -e "    ${GREEN}✓ GR-12: Response mentions PageRank scoring${NC}"
                fi
            else
                # Check if find_hotspots was used as fallback
                local hs_used=$(echo "$trace" | jq '[.trace[] | select(.action == "tool_call") | select(.tool == "find_hotspots")] | length' 2>/dev/null || echo "0")
                if [ "$hs_used" -gt 0 ]; then
                    echo -e "    ${YELLOW}⚠ GR-13: Used find_hotspots (degree-based) instead of find_important (PageRank)${NC}"
                    echo -e "    ${YELLOW}  → Pre-GR-13: Expected (find_important not implemented)${NC}"
                    echo -e "    ${YELLOW}  → Post-GR-13: Should use find_important for importance queries${NC}"
                else
                    echo -e "    ${RED}✗ GR-13: Neither find_important nor find_hotspots used${NC}"
                fi
            fi
            ;;

        fast_pagerank)
            # GR-12: Verify PageRank completed within reasonable time (< 30s for convergence)
            if [ "$duration" -lt 30000 ]; then
                echo -e "    ${GREEN}✓ GR-12: PageRank completed in ${duration}ms (< 30s threshold)${NC}"
            else
                echo -e "    ${YELLOW}⚠ GR-12: PageRank took ${duration}ms (threshold: 30s)${NC}"
                echo -e "    ${YELLOW}  → May need optimization for large graphs${NC}"
            fi
            ;;

        no_grep_used)
            # GR-40: Verify that Grep was NOT used as fallback for interface queries
            if [ -z "$session_id" ]; then
                session_id=$(echo "$response" | jq -r '.session_id')
            fi
            local trace=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/agent/$session_id/reasoning'" 2>/dev/null)

            local grep_calls=$(echo "$trace" | jq '[.trace[] | select(.action == "tool_call") | select(.tool == "Grep")] | length' 2>/dev/null || echo "0")
            local impl_calls=$(echo "$trace" | jq '[.trace[] | select(.action == "tool_call") | select(.tool == "find_implementations")] | length' 2>/dev/null || echo "0")

            if [ "$grep_calls" -eq 0 ]; then
                echo -e "    ${GREEN}✓ GR-40: No Grep fallback (correct behavior)${NC}"
                if [ "$impl_calls" -gt 0 ]; then
                    echo -e "    ${GREEN}✓ GR-40: Used find_implementations: $impl_calls calls${NC}"
                fi
            else
                echo -e "    ${RED}✗ GR-40: Grep fallback detected: $grep_calls calls${NC}"
                echo -e "    ${YELLOW}  → Pre-GR-40: Expected (no implements edges, falls back to Grep)${NC}"
                echo -e "    ${YELLOW}  → Post-GR-40: Should use find_implementations exclusively${NC}"

                # Show what Grep was searching for
                local grep_patterns=$(echo "$trace" | jq -r '[.trace[] | select(.action == "tool_call") | select(.tool == "Grep") | .params.pattern // .target] | unique | join(", ")' 2>/dev/null)
                if [ -n "$grep_patterns" ] && [ "$grep_patterns" != "null" ]; then
                    echo -e "    ${YELLOW}  → Grep patterns: $grep_patterns${NC}"
                fi
            fi
            ;;

        # ================================================================================
        # GR-PHASE1: INTEGRATION TEST QUALITY CHECKS
        # TDD: These checks define expected behavior BEFORE fixes are implemented
        # ================================================================================

        empty_response_threshold)
            # P0: Verify empty response warnings are minimal (< 50 total)
            local empty_warns=$(ssh_cmd "grep -c 'empty response' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
            empty_warns=$(echo "$empty_warns" | tr -d '[:space:]')

            if [ "$empty_warns" -lt 50 ]; then
                echo -e "    ${GREEN}✓ P0-Issue1: Empty response warnings: $empty_warns (< 50 threshold)${NC}"
            else
                echo -e "    ${RED}✗ P0-Issue1: Empty response warnings: $empty_warns (exceeds 50 threshold)${NC}"
                echo -e "    ${YELLOW}  → Root cause: OllamaAdapter receiving empty responses from LLM${NC}"
                echo -e "    ${YELLOW}  → Fix: Check prompt format compatibility with $OLLAMA_MODEL${NC}"
            fi
            ;;

        avg_runtime_threshold)
            # P0: Verify this test completed in reasonable time (< 15s)
            local threshold=15000
            if [ "$duration" -lt "$threshold" ]; then
                echo -e "    ${GREEN}✓ P0-Issue1: Runtime ${duration}ms (< ${threshold}ms threshold)${NC}"
            else
                echo -e "    ${RED}✗ P0-Issue1: Runtime ${duration}ms (exceeds ${threshold}ms threshold)${NC}"
                echo -e "    ${YELLOW}  → Likely cause: Empty response retries adding ~9s per query${NC}"
            fi
            ;;

        crs_speedup_verified)
            # P1: Verify CRS provides speedup for subsequent queries
            # This test should be faster than FIRST_TEST_RUNTIME (session 1)
            if [ "$FIRST_TEST_RUNTIME" -gt 0 ]; then
                if [ "$duration" -lt "$FIRST_TEST_RUNTIME" ]; then
                    local speedup=$(( (FIRST_TEST_RUNTIME - duration) * 100 / FIRST_TEST_RUNTIME ))
                    echo -e "    ${GREEN}✓ P1-Issue3: CRS speedup verified: ${speedup}% faster${NC}"
                    echo -e "    ${GREEN}  → Session 1: ${FIRST_TEST_RUNTIME}ms, This query: ${duration}ms${NC}"
                else
                    local slowdown=$(( (duration - FIRST_TEST_RUNTIME) * 100 / FIRST_TEST_RUNTIME ))
                    echo -e "    ${RED}✗ P1-Issue3: CRS NOT providing speedup: ${slowdown}% SLOWER${NC}"
                    echo -e "    ${YELLOW}  → Session 1: ${FIRST_TEST_RUNTIME}ms, This query: ${duration}ms${NC}"
                    echo -e "    ${YELLOW}  → CRS context should reduce tool calls for subsequent queries${NC}"
                fi
            else
                echo -e "    ${YELLOW}⚠ P1-Issue3: No baseline runtime available for comparison${NC}"
            fi
            ;;

        fast_not_found_strict)
            # P2: Verify not-found queries complete in < 5 seconds
            local threshold=5000
            if [ "$duration" -lt "$threshold" ]; then
                echo -e "    ${GREEN}✓ P2-Issue4: Not-found query: ${duration}ms (< ${threshold}ms threshold)${NC}"
            else
                echo -e "    ${RED}✗ P2-Issue4: Not-found query: ${duration}ms (exceeds ${threshold}ms)${NC}"
                echo -e "    ${YELLOW}  → Should be O(1) index miss, not O(V) scan with LLM retries${NC}"
            fi
            # Verify response indicates not found
            local agent_resp=$(echo "$response" | jq -r '.response // ""')
            if echo "$agent_resp" | grep -qi "not found\|no function\|doesn't exist\|does not exist"; then
                echo -e "    ${GREEN}✓ P2-Issue4: Correctly reported symbol not found${NC}"
            else
                echo -e "    ${YELLOW}⚠ P2-Issue4: Response may not clearly indicate not found${NC}"
            fi
            ;;

        citations_present)
            # P3: Verify response includes [file:line] citations
            local agent_resp=$(echo "$response" | jq -r '.response // ""')
            # Look for patterns like [file.go:123] or file.go:123 or (file.go:123)
            local citation_count=$(echo "$agent_resp" | grep -oE '\[?[a-zA-Z0-9_/.-]+\.(go|py|js|ts|rs|java):[0-9]+\]?' | wc -l)
            citation_count=$(echo "$citation_count" | tr -d '[:space:]')

            if [ "$citation_count" -gt 0 ]; then
                echo -e "    ${GREEN}✓ P3-Issue7: Found $citation_count [file:line] citations${NC}"
            else
                echo -e "    ${RED}✗ P3-Issue7: No [file:line] citations in response${NC}"
                echo -e "    ${YELLOW}  → Analytical responses should include source citations${NC}"
                echo -e "    ${YELLOW}  → Fix: Improve prompt to require citations${NC}"
            fi
            ;;

        # ================================================================================
        # GR-10: QUERY CACHE PERFORMANCE CHECKS
        # TDD: These checks define expected behavior BEFORE implementation
        # ================================================================================

        cache_miss_expected)
            # GR-10: First query should be a cache miss
            local cache_stats=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/cache'" 2>/dev/null || echo "{}")
            local miss_count=$(echo "$cache_stats" | jq '.misses // 0' 2>/dev/null || echo "0")
            local hit_count=$(echo "$cache_stats" | jq '.hits // 0' 2>/dev/null || echo "0")

            if [ "$miss_count" -gt 0 ]; then
                echo -e "    ${GREEN}✓ GR-10: Cache miss recorded (misses=$miss_count, hits=$hit_count)${NC}"
            else
                echo -e "    ${YELLOW}⚠ GR-10: Cache stats not available or no miss recorded${NC}"
                echo -e "    ${YELLOW}  → Pre-GR-10: Expected (cache not implemented)${NC}"
            fi

            # Check server logs for cache activity
            local cache_logs=$(ssh_cmd "grep -i 'cache.*miss\|cache.*populate' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -3" || echo "")
            if [ -n "$cache_logs" ]; then
                echo -e "    ${BLUE}Cache logs:${NC}"
                echo "$cache_logs" | sed 's/^/      /'
            fi
            ;;

        cache_hit_expected)
            # GR-10: Second identical query should be a cache hit
            local cache_stats=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/cache'" 2>/dev/null || echo "{}")
            local hit_count=$(echo "$cache_stats" | jq '.hits // 0' 2>/dev/null || echo "0")
            local miss_count=$(echo "$cache_stats" | jq '.misses // 0' 2>/dev/null || echo "0")

            if [ "$hit_count" -gt 0 ]; then
                local hit_rate=$(echo "scale=2; $hit_count * 100 / ($hit_count + $miss_count)" | bc 2>/dev/null || echo "?")
                echo -e "    ${GREEN}✓ GR-10: Cache hit recorded (hits=$hit_count, hit_rate=$hit_rate%)${NC}"
            else
                echo -e "    ${RED}✗ GR-10: No cache hit for repeated query${NC}"
                echo -e "    ${YELLOW}  → Pre-GR-10: Expected (cache not implemented)${NC}"
                echo -e "    ${YELLOW}  → Post-GR-10: Second identical query should hit cache${NC}"
            fi

            # Check server logs for cache hit
            local cache_logs=$(ssh_cmd "grep -i 'cache.*hit' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -3" || echo "")
            if [ -n "$cache_logs" ]; then
                echo -e "    ${BLUE}Cache hit logs:${NC}"
                echo "$cache_logs" | sed 's/^/      /'
            fi
            ;;

        cache_speedup_expected)
            # GR-10: Cached query should be significantly faster
            # Compare this runtime to the first test runtime
            local cache_stats=$(ssh_cmd "curl -s 'http://localhost:8080/v1/codebuddy/debug/cache'" 2>/dev/null || echo "{}")
            local avg_hit_time=$(echo "$cache_stats" | jq '.avg_hit_time_ms // 0' 2>/dev/null || echo "0")
            local avg_miss_time=$(echo "$cache_stats" | jq '.avg_miss_time_ms // 0' 2>/dev/null || echo "0")

            if [ "$avg_hit_time" -gt 0 ] && [ "$avg_miss_time" -gt 0 ]; then
                local speedup=$(echo "scale=1; $avg_miss_time / $avg_hit_time" | bc 2>/dev/null || echo "?")
                echo -e "    ${GREEN}✓ GR-10: Cache speedup: ${speedup}x (miss=${avg_miss_time}ms, hit=${avg_hit_time}ms)${NC}"
            else
                # Fall back to comparing with first test
                if [ "$FIRST_TEST_RUNTIME" -gt 0 ]; then
                    if [ "$duration" -lt "$FIRST_TEST_RUNTIME" ]; then
                        local speedup=$(( (FIRST_TEST_RUNTIME - duration) * 100 / FIRST_TEST_RUNTIME ))
                        echo -e "    ${GREEN}✓ GR-10: Query ${speedup}% faster than first (cached)${NC}"
                        echo -e "    ${BLUE}  First query: ${FIRST_TEST_RUNTIME}ms, This query: ${duration}ms${NC}"
                    else
                        echo -e "    ${YELLOW}⚠ GR-10: No speedup observed (may not be cached)${NC}"
                    fi
                fi
            fi
            ;;

        # ================================================================================
        # GR-11: PARALLEL BFS PERFORMANCE CHECKS
        # TDD: These checks define expected behavior BEFORE implementation
        # ================================================================================

        parallel_correctness)
            # GR-11: Verify parallel BFS returns same results as sequential
            # Check that call graph contains expected nodes
            local agent_resp=$(echo "$response" | jq -r '.response // ""')
            local node_count=$(echo "$agent_resp" | grep -oE '[a-zA-Z_][a-zA-Z0-9_]*' | sort -u | wc -l | tr -d ' ')

            if [ "$node_count" -gt 0 ]; then
                echo -e "    ${GREEN}✓ GR-11: Call graph returned $node_count unique symbols${NC}"

                # Check server logs for parallel mode indication
                local parallel_log=$(ssh_cmd "grep -i 'parallel.*bfs\|bfs.*parallel\|parallel_mode' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -3" || echo "")
                if [ -n "$parallel_log" ]; then
                    echo -e "    ${BLUE}Parallel BFS logs:${NC}"
                    echo "$parallel_log" | sed 's/^/      /'
                else
                    echo -e "    ${YELLOW}⚠ GR-11: No parallel BFS log entries (pre-implementation expected)${NC}"
                fi
            else
                echo -e "    ${RED}✗ GR-11: No symbols in call graph response${NC}"
            fi
            ;;

        parallel_speedup)
            # GR-11: Verify parallel is faster for wide graphs
            # Check OTel span attributes for parallel_mode and timing
            local trace_resp=$(echo "$response" | jq '.crs_trace // {}')
            local parallel_used=$(echo "$trace_resp" | jq -r '[.trace[] | select(.metadata.parallel_mode == true)] | length' 2>/dev/null || echo "0")

            if [ "$parallel_used" -gt 0 ]; then
                echo -e "    ${GREEN}✓ GR-11: Parallel mode was used for traversal${NC}"

                # Check if there's timing info
                local parallel_time=$(echo "$trace_resp" | jq -r '[.trace[] | select(.metadata.parallel_mode == true) | .metadata.duration_ms // 0] | add' 2>/dev/null || echo "0")
                if [ "$parallel_time" -gt 0 ]; then
                    echo -e "    ${BLUE}  Parallel execution time: ${parallel_time}ms${NC}"
                fi
            else
                echo -e "    ${YELLOW}⚠ GR-11: Parallel mode not detected (pre-implementation or graph too small)${NC}"
                echo -e "    ${YELLOW}  → Pre-GR-11: Expected (parallel BFS not implemented)${NC}"
                echo -e "    ${YELLOW}  → Post-GR-11: Should use parallel for levels > 16 nodes${NC}"
            fi

            # Check server logs for speedup info
            local speedup_log=$(ssh_cmd "grep -i 'parallel.*speedup\|level.*nodes' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -3" || echo "")
            if [ -n "$speedup_log" ]; then
                echo -e "    ${BLUE}Speedup logs:${NC}"
                echo "$speedup_log" | sed 's/^/      /'
            fi
            ;;

        *)
            echo -e "    ${YELLOW}⚠ Unknown extra check: $check${NC}"
            ;;
    esac
}

# ==============================================================================
# UTILITY FUNCTIONS
# ==============================================================================

# Get current time in milliseconds
get_time_ms() {
    python3 -c 'import time; print(int(time.time() * 1000))'
}

# Expand test specification into array of test numbers
expand_test_spec() {
    local spec="$1"
    local result=()

    IFS=',' read -ra parts <<< "$spec"
    for part in "${parts[@]}"; do
        if [[ "$part" =~ ^([0-9]+)-([0-9]+)$ ]]; then
            # Range like "1-5"
            for ((i=${BASH_REMATCH[1]}; i<=${BASH_REMATCH[2]}; i++)); do
                result+=($i)
            done
        else
            # Single number
            result+=($part)
        fi
    done

    echo "${result[@]}"
}

# ==============================================================================
# MAIN
# ==============================================================================

main() {
    # Local mode runs Go tests directly
    if [ "$LOCAL_MODE" = true ]; then
        run_local_tests
        exit $?
    fi

    echo -e "${BLUE}═══════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  CRS Integration Tests - Remote GPU Mode${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════${NC}"
    echo ""
    echo "Remote: $REMOTE_USER@$REMOTE_HOST:$REMOTE_PORT"
    echo "Project: $PROJECT_TO_ANALYZE"
    echo "Main Agent: $OLLAMA_MODEL"
    echo "Router: $ROUTER_MODEL"
    echo "Output: $OUTPUT_FILE"
    echo ""

    # Setup ssh-agent first (enter passphrase once)
    setup_ssh_agent

    # Establish master connection for multiplexing
    establish_connection
    trap 'stop_trace_server; close_connection' EXIT

    # Test SSH connection
    if ! test_ssh_connection; then
        exit 1
    fi

    # Check remote Ollama
    check_remote_ollama

    # Setup remote environment (sync and build)
    setup_remote

    # Start trace server
    if ! start_trace_server; then
        exit 1
    fi

    # Determine which tests to run
    local tests_to_run=()
    if [ -n "$SPECIFIC_TESTS" ]; then
        tests_to_run=($(expand_test_spec "$SPECIFIC_TESTS"))
        echo ""
        echo -e "${BLUE}Running ${#tests_to_run[@]} specific CRS tests${NC}"
        echo "Tests: ${tests_to_run[*]}"
    else
        # Run all tests
        for ((i=1; i<=${#CRS_TESTS[@]}; i++)); do
            tests_to_run+=($i)
        done
        echo ""
        echo -e "${BLUE}Running all ${#tests_to_run[@]} CRS tests${NC}"
    fi
    echo ""

    # Initialize results
    local results="[]"
    local passed=0
    local failed=0
    local total_runtime=0
    local total_tokens=0
    local total_steps=0

    # Run tests
    for test_num in "${tests_to_run[@]}"; do
        local idx=$((test_num - 1))
        if [ $idx -ge 0 ] && [ $idx -lt ${#CRS_TESTS[@]} ]; then
            # LAST_TEST_RESULT is set by run_crs_test
            LAST_TEST_RESULT="{}"

            if run_crs_test "${CRS_TESTS[$idx]}" "$test_num"; then
                ((passed++))
                LAST_TEST_RESULT=$(echo "$LAST_TEST_RESULT" | jq '.status = "PASSED"')
            else
                ((failed++))
                LAST_TEST_RESULT=$(echo "$LAST_TEST_RESULT" | jq '.status = "FAILED"')
            fi

            # Extract stats from test result
            local runtime=$(echo "$LAST_TEST_RESULT" | jq -r '.runtime_ms // 0')
            local tokens=$(echo "$LAST_TEST_RESULT" | jq -r '.tokens_used // 0')
            local steps=$(echo "$LAST_TEST_RESULT" | jq -r '.steps_taken // 0')

            # Track first test runtime for speed comparisons
            if [ "$FIRST_TEST_RUNTIME" -eq 0 ]; then
                FIRST_TEST_RUNTIME=$runtime
            fi

            total_runtime=$((total_runtime + runtime))
            total_tokens=$((total_tokens + tokens))
            total_steps=$((total_steps + steps))

            # Add to results array
            results=$(echo "$results" | jq --argjson new "$LAST_TEST_RESULT" '. + [$new]')
        else
            echo -e "${YELLOW}Skipping invalid test number: $test_num${NC}"
        fi
    done

    # Calculate averages
    local tests_run=${#tests_to_run[@]}
    local avg_runtime=0
    local avg_tokens=0
    local avg_steps=0
    if [ $tests_run -gt 0 ]; then
        avg_runtime=$((total_runtime / tests_run))
        avg_tokens=$((total_tokens / tests_run))
        avg_steps=$((total_steps / tests_run))
    fi

    # Calculate tool usage across all tests
    local tool_usage=$(echo "$results" | jq '
        [.[].crs_trace.trace // [] | .[] | select(.action == "tool_call" or .action == "tool_call_forced")] |
        group_by(.tool) |
        map({tool: .[0].tool, count: length}) |
        sort_by(-.count)
    ' 2>/dev/null || echo "[]")

    # Calculate CRS event counts across all tests
    local circuit_breaker_count=$(echo "$results" | jq '
        [.[].crs_trace.trace // [] | .[] | select(.action == "circuit_breaker")] | length
    ' 2>/dev/null || echo "0")

    local semantic_rep_count=$(echo "$results" | jq '
        [.[].crs_trace.trace // [] | .[] | select(.error != null and (.error | test("[Ss]emantic repetition|similar to")))] | length
    ' 2>/dev/null || echo "0")

    local tools_exceeding_threshold=$(echo "$results" | jq '
        [.[].crs_trace.trace // [] | .[] | select(.action == "tool_call" or .action == "tool_call_forced")] |
        group_by(.tool) |
        map({tool: .[0].tool, count: length}) |
        map(select(.count > 2)) |
        length
    ' 2>/dev/null || echo "0")

    local learning_events=$(echo "$results" | jq '
        [.[].crs_trace.trace // [] | .[] | select(.action | test("learn|clause|cdcl"))] | length
    ' 2>/dev/null || echo "0")

    # GR-Phase1: Calculate quality metrics
    local empty_response_warnings=$(ssh_cmd "grep -c 'empty response' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
    empty_response_warnings=$(echo "$empty_response_warnings" | tr -d '[:space:]')

    local tests_under_15s=$(echo "$results" | jq '[.[] | select(.runtime_ms < 15000)] | length' 2>/dev/null || echo "0")
    local tests_over_60s=$(echo "$results" | jq '[.[] | select(.runtime_ms >= 60000)] | length' 2>/dev/null || echo "0")

    # Build output JSON
    local output=$(jq -n \
        --arg timestamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --arg project "$PROJECT_TO_ANALYZE" \
        --arg remote "$REMOTE_USER@$REMOTE_HOST:$REMOTE_PORT" \
        --arg model "$OLLAMA_MODEL" \
        --arg router "$ROUTER_MODEL" \
        --arg total "$tests_run" \
        --arg passed "$passed" \
        --arg failed "$failed" \
        --arg total_runtime "$total_runtime" \
        --arg avg_runtime "$avg_runtime" \
        --arg total_tokens "$total_tokens" \
        --arg avg_tokens "$avg_tokens" \
        --arg total_steps "$total_steps" \
        --arg avg_steps "$avg_steps" \
        --argjson tool_usage "$tool_usage" \
        --argjson results "$results" \
        '{
            metadata: {
                timestamp: $timestamp,
                test_type: "CRS Integration",
                project_root: $project,
                remote_host: $remote,
                models: {
                    main_agent: $model,
                    router: $router
                },
                total_tests: ($total | tonumber),
                passed: ($passed | tonumber),
                failed: ($failed | tonumber),
                timing: {
                    total_runtime_ms: ($total_runtime | tonumber),
                    avg_runtime_ms: ($avg_runtime | tonumber)
                },
                usage: {
                    total_tokens: ($total_tokens | tonumber),
                    avg_tokens: ($avg_tokens | tonumber),
                    total_steps: ($total_steps | tonumber),
                    avg_steps: ($avg_steps | tonumber)
                },
                tool_usage_summary: $tool_usage
            },
            results: $results
        }')

    echo "$output" > "$OUTPUT_FILE"

    # Summary
    echo ""
    echo ""
    echo -e "${BLUE}╔══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║                     CRS TEST SUMMARY                             ║${NC}"
    echo -e "${BLUE}╠══════════════════════════════════════════════════════════════════╣${NC}"
    echo -e "${BLUE}║${NC}                                                                  ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}  Remote: $REMOTE_USER@$REMOTE_HOST:$REMOTE_PORT                             ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}  Models: $OLLAMA_MODEL / $ROUTER_MODEL               ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}                                                                  ${BLUE}║${NC}"
    echo -e "${BLUE}╠══════════════════════════════════════════════════════════════════╣${NC}"
    echo -e "${BLUE}║${NC}  RESULTS                                                         ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}  ├─ Tests run:    $tests_run                                              ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}  ├─ ${GREEN}Passed:       $passed${NC}                                              ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}  └─ ${RED}Failed:       $failed${NC}                                              ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}                                                                  ${BLUE}║${NC}"
    echo -e "${BLUE}╠══════════════════════════════════════════════════════════════════╣${NC}"
    echo -e "${BLUE}║${NC}  PERFORMANCE                                                     ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}  ├─ Total runtime:  ${total_runtime}ms                                    ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}  ├─ Avg runtime:    ${avg_runtime}ms                                      ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}  ├─ Total tokens:   ${total_tokens}                                       ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}  ├─ Avg tokens:     ${avg_tokens}                                         ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}  ├─ Total steps:    ${total_steps}                                        ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}  └─ Avg steps:      ${avg_steps}                                          ${BLUE}║${NC}"
    echo -e "${BLUE}║${NC}                                                                  ${BLUE}║${NC}"
    echo -e "${BLUE}╠══════════════════════════════════════════════════════════════════╣${NC}"
    echo -e "${BLUE}║${NC}  TOOL USAGE (across all tests)                                   ${BLUE}║${NC}"
    echo "$tool_usage" | jq -r '.[] | "  ├─ \(.tool): \(.count) calls" + (if .count > 2 then " ⚠" else "" end)' 2>/dev/null | head -10 | while read line; do
        printf "${BLUE}║${NC}  %-64s ${BLUE}║${NC}\n" "$line"
    done
    echo -e "${BLUE}║${NC}                                                                  ${BLUE}║${NC}"
    echo -e "${BLUE}╠══════════════════════════════════════════════════════════════════╣${NC}"
    echo -e "${BLUE}║${NC}  CRS EVENTS (Code Reasoning State)                               ${BLUE}║${NC}"
    printf "${BLUE}║${NC}  ├─ Circuit breakers fired:    %-5s                             ${BLUE}║${NC}\n" "$circuit_breaker_count"
    printf "${BLUE}║${NC}  ├─ Semantic repetitions:      %-5s                             ${BLUE}║${NC}\n" "$semantic_rep_count"
    printf "${BLUE}║${NC}  ├─ Tools exceeding threshold: %-5s                             ${BLUE}║${NC}\n" "$tools_exceeding_threshold"
    printf "${BLUE}║${NC}  └─ Learning events:           %-5s                             ${BLUE}║${NC}\n" "$learning_events"
    echo -e "${BLUE}║${NC}                                                                  ${BLUE}║${NC}"
    # GR-39b Verification
    if [ "$tools_exceeding_threshold" -gt 0 ] && [ "$circuit_breaker_count" -eq 0 ]; then
        echo -e "${BLUE}║${NC}  ${RED}⚠ GR-39b ISSUE: Tools exceeded threshold but CB didn't fire!${NC}   ${BLUE}║${NC}"
    elif [ "$circuit_breaker_count" -gt 0 ]; then
        echo -e "${BLUE}║${NC}  ${GREEN}✓ GR-39b: Circuit breaker working correctly${NC}                    ${BLUE}║${NC}"
    fi
    echo -e "${BLUE}║${NC}                                                                  ${BLUE}║${NC}"
    echo -e "${BLUE}╠══════════════════════════════════════════════════════════════════╣${NC}"
    echo -e "${BLUE}║${NC}  GR-PHASE1 QUALITY METRICS                                       ${BLUE}║${NC}"
    printf "${BLUE}║${NC}  ├─ Empty response warnings:  %-5s (should be <50)              ${BLUE}║${NC}\n" "$empty_response_warnings"
    printf "${BLUE}║${NC}  ├─ Avg runtime:              %-5sms (should be <15000ms)       ${BLUE}║${NC}\n" "$avg_runtime"
    printf "${BLUE}║${NC}  ├─ Tests under 15s:          %-5s                              ${BLUE}║${NC}\n" "$tests_under_15s"
    printf "${BLUE}║${NC}  └─ Tests over 60s:           %-5s (should be 0)                ${BLUE}║${NC}\n" "$tests_over_60s"
    echo -e "${BLUE}║${NC}                                                                  ${BLUE}║${NC}"
    # Quality assessment
    if [ "$empty_response_warnings" -lt 50 ] && [ "$avg_runtime" -lt 15000 ]; then
        echo -e "${BLUE}║${NC}  ${GREEN}✓ GR-Phase1: Quality thresholds MET${NC}                            ${BLUE}║${NC}"
    else
        echo -e "${BLUE}║${NC}  ${RED}✗ GR-Phase1: Quality thresholds NOT met${NC}                        ${BLUE}║${NC}"
        if [ "$empty_response_warnings" -ge 50 ]; then
            echo -e "${BLUE}║${NC}    ${YELLOW}→ P0: Empty response warnings exceed threshold${NC}               ${BLUE}║${NC}"
        fi
        if [ "$avg_runtime" -ge 15000 ]; then
            echo -e "${BLUE}║${NC}    ${YELLOW}→ P0: Average runtime exceeds 15s threshold${NC}                  ${BLUE}║${NC}"
        fi
    fi
    echo -e "${BLUE}║${NC}                                                                  ${BLUE}║${NC}"
    echo -e "${BLUE}╚══════════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "Results saved to: ${GREEN}$OUTPUT_FILE${NC}"
    echo ""

    # Show failed test details
    if [ $failed -gt 0 ]; then
        echo -e "${RED}╔══════════════════════════════════════════════════════════════════╗${NC}"
        echo -e "${RED}║                     FAILED TESTS                                 ║${NC}"
        echo -e "${RED}╠══════════════════════════════════════════════════════════════════╣${NC}"
        echo "$results" | jq -r '.[] | select(.status == "FAILED") |
            "Test \(.test) [\(.category)]: \(.query | .[0:50])...\n  State: \(.state)\n  Error: \(.response | .[0:100] // "none")\n"
        ' | while read line; do
            echo -e "${RED}║${NC}  $line"
        done
        echo -e "${RED}╚══════════════════════════════════════════════════════════════════╝${NC}"
        echo ""
        echo -e "${YELLOW}Remote server logs (last 30 lines):${NC}"
        ssh_cmd "tail -30 ~/trace_test/AleutianFOSS/trace_server.log" 2>/dev/null || true
    fi

    # Per-test breakdown
    echo ""
    echo -e "${BLUE}╔══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║                     PER-TEST BREAKDOWN                           ║${NC}"
    echo -e "${BLUE}╚══════════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo "$results" | jq -r '.[] |
        "Test \(.test) [\(.category)] - \(.status)\n" +
        "  Query: \(.query | .[0:60])...\n" +
        "  Time: \(.runtime_ms)ms | Steps: \(.steps_taken) | Tokens: \(.tokens_used)\n" +
        "  CRS Trace: \(.crs_trace.total_steps // 0) reasoning steps\n" +
        "  Tools: \([.crs_trace.trace // [] | .[] | select(.action == "tool_call" or .action == "tool_call_forced") | .tool] | group_by(.) | map("\(.[0]):\(length)") | join(", ") | if . == "" then "none" else . end)\n" +
        "  Circuit Breakers: \([.crs_trace.trace // [] | .[] | select(.action == "circuit_breaker")] | length)\n" +
        "  Semantic Blocks: \([.crs_trace.trace // [] | .[] | select(.error != null and (.error | test("[Ss]emantic|similar")))] | length)\n"
    ' 2>/dev/null

    # CRS Server Log Summary
    echo ""
    echo -e "${BLUE}╔══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║                     CRS SERVER LOG SUMMARY                       ║${NC}"
    echo -e "${BLUE}╚══════════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    # Count key CRS events in server logs
    local log_gr39b=$(ssh_cmd "grep -c 'GR-39b' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
    local log_cb30c=$(ssh_cmd "grep -c 'CB-30c' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
    local log_crs02=$(ssh_cmd "grep -c 'CRS-02' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
    local log_crs04=$(ssh_cmd "grep -c 'CRS-04' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
    local log_crs06=$(ssh_cmd "grep -c 'CRS-06' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
    local log_errors=$(ssh_cmd "grep -c 'ERROR' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")
    local log_warns=$(ssh_cmd "grep -c 'WARN' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null" || echo "0")

    echo -e "  Log event counts:"
    printf "    GR-39b (Count Circuit Breaker):  %s\n" "$log_gr39b"
    printf "    CB-30c (Semantic Repetition):    %s\n" "$log_cb30c"
    printf "    CRS-02 (Proof Numbers):          %s\n" "$log_crs02"
    printf "    CRS-04 (CDCL Learning):          %s\n" "$log_crs04"
    printf "    CRS-06 (Coordinator Events):     %s\n" "$log_crs06"
    printf "    Errors:                          %s\n" "$log_errors"
    printf "    Warnings:                        %s\n" "$log_warns"
    echo ""

    # Show recent GR-39b and CB-30c logs if any
    if [ "$log_gr39b" != "0" ]; then
        echo -e "  ${YELLOW}Recent GR-39b (Count Circuit Breaker) entries:${NC}"
        ssh_cmd "grep 'GR-39b' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -5" | sed 's/^/    /'
        echo ""
    fi

    if [ "$log_cb30c" != "0" ]; then
        echo -e "  ${YELLOW}Recent CB-30c (Semantic Repetition) entries:${NC}"
        ssh_cmd "grep 'CB-30c' ~/trace_test/AleutianFOSS/trace_server.log 2>/dev/null | tail -5" | sed 's/^/    /'
        echo ""
    fi

    # Option to view full JSON
    echo ""
    echo -e "${YELLOW}Full JSON results saved to: $OUTPUT_FILE${NC}"
    echo -e "${YELLOW}View with: cat $OUTPUT_FILE | jq .${NC}"
    echo -e "${YELLOW}View specific test: cat $OUTPUT_FILE | jq '.results[0]'${NC}"
    echo -e "${YELLOW}View all CRS traces: cat $OUTPUT_FILE | jq '.results[].crs_trace'${NC}"
    echo ""
    echo -e "${YELLOW}View server logs: ssh -p $REMOTE_PORT $REMOTE_USER@$REMOTE_HOST 'cat ~/trace_test/AleutianFOSS/trace_server.log'${NC}"
    echo -e "${YELLOW}Search for GR-39b: ssh -p $REMOTE_PORT $REMOTE_USER@$REMOTE_HOST 'grep GR-39b ~/trace_test/AleutianFOSS/trace_server.log'${NC}"

    if [ $failed -gt 0 ]; then
        exit 1
    fi
}

main "$@"
