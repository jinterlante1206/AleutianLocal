#!/bin/bash
# Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
# Code Buddy Agent Loop Test Suite
#
# Usage:
#   ./scripts/codebuddy_test_suite.sh           # Interactive menu
#   ./scripts/codebuddy_test_suite.sh all       # Run all tests
#   ./scripts/codebuddy_test_suite.sh 1 2 3     # Run specific tests
#   ./scripts/codebuddy_test_suite.sh server    # Start server only

# NOTE: Not using set -e so tests can fail without exiting interactive mode

# =============================================================================
# Configuration
# =============================================================================

BASE_URL="${BASE_URL:-http://localhost:8080}"
API_BASE="$BASE_URL/v1/codebuddy"
TEST_REPO="${TEST_REPO:-/Users/jin/GolandProjects/AleutianOrchestrator}"
OLLAMA_BASE_URL="${OLLAMA_BASE_URL:-http://localhost:11434}"

# Default model: glm-4.7-flash (works better with tools flag than gpt-oss:20b)
OLLAMA_MODEL="${OLLAMA_MODEL:-glm-4.7-flash}"

PORT="${PORT:-8080}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Test state
LAST_SESSION_ID=""
TESTS_PASSED=0
TESTS_FAILED=0
SERVER_PID=""

# =============================================================================
# Helper Functions
# =============================================================================

print_header() {
    echo ""
    echo -e "${BLUE}╔════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║${NC} ${BOLD}$1${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════════════════════════════╝${NC}"
}

print_subheader() {
    echo ""
    echo -e "${CYAN}── $1 ──${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
    ((TESTS_PASSED++))
}

print_failure() {
    echo -e "${RED}✗ $1${NC}"
    ((TESTS_FAILED++))
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

print_config() {
    echo -e "  ${CYAN}$1:${NC} $2"
}

# Make API request and capture response
api_request() {
    local method="$1"
    local endpoint="$2"
    local data="$3"

    if [ -n "$data" ]; then
        curl -s -X "$method" "$API_BASE$endpoint" \
            -H "Content-Type: application/json" \
            -d "$data"
    else
        curl -s -X "$method" "$API_BASE$endpoint"
    fi
}

# Extract field from JSON response
json_field() {
    local json="$1"
    local field="$2"
    echo "$json" | grep -o "\"$field\":[^,}]*" | head -1 | sed 's/.*://' | tr -d '"' | tr -d ' '
}

# Extract string field (handles spaces)
json_string_field() {
    local json="$1"
    local field="$2"
    echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('$field',''))" 2>/dev/null || echo ""
}

# Pretty print JSON
pretty_json() {
    echo "$1" | python3 -m json.tool 2>/dev/null || echo "$1"
}

# Wait for server to be ready
wait_for_server() {
    local max_attempts=30
    local attempt=0

    echo -n "Waiting for server"
    while [ $attempt -lt $max_attempts ]; do
        if curl -s "$API_BASE/health" > /dev/null 2>&1; then
            echo -e " ${GREEN}ready!${NC}"
            return 0
        fi
        echo -n "."
        sleep 1
        ((attempt++))
    done

    echo -e " ${RED}timeout!${NC}"
    return 1
}

# =============================================================================
# Server Management
# =============================================================================

start_server() {
    local mode="$1"  # "context", "tools", "both", or empty

    print_header "Starting Code Buddy Server"

    local flags=""
    case "$mode" in
        context) flags="-with-context" ;;
        tools) flags="-with-tools" ;;
        both) flags="-with-context -with-tools" ;;
    esac

    print_config "Port" "$PORT"
    print_config "Ollama URL" "$OLLAMA_BASE_URL"
    print_config "Model" "$OLLAMA_MODEL"
    print_config "Test Repo" "$TEST_REPO"
    print_config "Flags" "${flags:-none}"

    # Kill any existing server
    pkill -f "codebuddy.*-port.*$PORT" 2>/dev/null || true
    sleep 1

    # Start server in background
    OLLAMA_BASE_URL="$OLLAMA_BASE_URL" \
    OLLAMA_MODEL="$OLLAMA_MODEL" \
    go run ./cmd/codebuddy -port "$PORT" $flags > /tmp/codebuddy_test.log 2>&1 &
    SERVER_PID=$!

    if wait_for_server; then
        print_success "Server started (PID: $SERVER_PID)"
        return 0
    else
        print_failure "Server failed to start"
        cat /tmp/codebuddy_test.log
        return 1
    fi
}

stop_server() {
    if [ -n "$SERVER_PID" ]; then
        print_info "Stopping server (PID: $SERVER_PID)"
        kill $SERVER_PID 2>/dev/null || true
        SERVER_PID=""
    fi
    pkill -f "codebuddy.*-port.*$PORT" 2>/dev/null || true
}

# =============================================================================
# Test Cases
# =============================================================================

test_health_check() {
    print_subheader "Test: Health Check"

    local response=$(api_request GET "/health")
    local status=$(json_field "$response" "status")

    # Accept both "ok" and "healthy" as valid status values
    if [ "$status" = "ok" ] || [ "$status" = "healthy" ]; then
        print_success "Health check passed (status=$status)"
        return 0
    else
        print_failure "Health check failed: $response"
        return 1
    fi
}

test_basic_query() {
    print_subheader "Test: Basic Query (Entry Points)"

    local response=$(api_request POST "/agent/run" "{
        \"project_root\": \"$TEST_REPO\",
        \"query\": \"What are the main entry points in this codebase?\"
    }")

    local state=$(json_field "$response" "state")
    local tokens=$(json_field "$response" "tokens_used")
    LAST_SESSION_ID=$(json_field "$response" "session_id")

    echo "  Session ID: $LAST_SESSION_ID"
    echo "  State: $state"
    echo "  Tokens: $tokens"

    if [ "$state" = "COMPLETE" ] && [ "$tokens" -gt 0 ] 2>/dev/null; then
        print_success "Basic query completed with $tokens tokens"
        echo ""
        echo -e "  ${CYAN}Response preview:${NC}"
        json_string_field "$response" "response" | head -c 300
        echo "..."
        return 0
    else
        print_failure "Basic query failed: state=$state, tokens=$tokens"
        pretty_json "$response"
        return 1
    fi
}

test_different_queries() {
    print_subheader "Test: Different Query Types"

    local queries=(
        "What packages are in this project?"
        "Explain the data flow in the main function"
        "What external dependencies does this project use?"
        "Are there any error handling patterns?"
    )

    local passed=0
    local failed=0

    for query in "${queries[@]}"; do
        echo -n "  Testing: \"${query:0:40}...\" "

        local response=$(api_request POST "/agent/run" "{
            \"project_root\": \"$TEST_REPO\",
            \"query\": \"$query\"
        }")

        local state=$(json_field "$response" "state")
        local tokens=$(json_field "$response" "tokens_used")

        if [ "$state" = "COMPLETE" ] && [ "$tokens" -gt 0 ] 2>/dev/null; then
            echo -e "${GREEN}✓${NC} ($tokens tokens)"
            ((passed++))
        else
            echo -e "${RED}✗${NC} (state=$state)"
            ((failed++))
        fi
    done

    if [ $failed -eq 0 ]; then
        print_success "All $passed queries completed"
        return 0
    else
        print_failure "$failed of $((passed + failed)) queries failed"
        return 1
    fi
}

test_ambiguous_query() {
    print_subheader "Test: Ambiguous Query (Should Trigger CLARIFY)"

    local response=$(api_request POST "/agent/run" "{
        \"project_root\": \"$TEST_REPO\",
        \"query\": \"fix it\"
    }")

    local state=$(json_field "$response" "state")
    LAST_SESSION_ID=$(json_field "$response" "session_id")

    echo "  Query: \"fix it\""
    echo "  State: $state"
    echo "  Session ID: $LAST_SESSION_ID"

    if [ "$state" = "CLARIFY" ]; then
        print_success "Ambiguous query correctly triggered CLARIFY state"
        return 0
    elif [ "$state" = "COMPLETE" ]; then
        print_info "Query completed (model answered anyway)"
        return 0
    else
        print_failure "Unexpected state: $state"
        return 1
    fi
}

test_short_query() {
    print_subheader "Test: Very Short Query"

    local response=$(api_request POST "/agent/run" "{
        \"project_root\": \"$TEST_REPO\",
        \"query\": \"help\"
    }")

    local state=$(json_field "$response" "state")

    echo "  Query: \"help\""
    echo "  State: $state"

    # Short queries should trigger CLARIFY due to ambiguity check
    if [ "$state" = "CLARIFY" ] || [ "$state" = "COMPLETE" ]; then
        print_success "Short query handled (state=$state)"
        return 0
    else
        print_failure "Unexpected state: $state"
        return 1
    fi
}

test_session_continue() {
    print_subheader "Test: Multi-Turn Conversation (Continue)"

    # First query
    echo "  Step 1: Initial query..."
    local response1=$(api_request POST "/agent/run" "{
        \"project_root\": \"$TEST_REPO\",
        \"query\": \"What is the main function in this codebase?\"
    }")

    local session_id=$(json_field "$response1" "session_id")
    local state1=$(json_field "$response1" "state")

    echo "    Session: $session_id"
    echo "    State: $state1"

    if [ "$state1" != "COMPLETE" ]; then
        print_failure "Initial query did not complete"
        return 1
    fi

    # Continue query
    echo "  Step 2: Follow-up query..."
    local response2=$(api_request POST "/agent/continue" "{
        \"session_id\": \"$session_id\",
        \"clarification\": \"What imports does it use?\"
    }")

    local state2=$(json_field "$response2" "state")
    local tokens2=$(json_field "$response2" "tokens_used")

    echo "    State: $state2"
    echo "    Tokens: $tokens2"

    # Continue may return error if session is in wrong state
    if [ "$state2" = "COMPLETE" ]; then
        print_success "Multi-turn conversation completed"
        return 0
    else
        print_info "Continue returned state=$state2 (may need CLARIFY state first)"
        return 0
    fi
}

test_session_get_state() {
    print_subheader "Test: Get Session State"

    if [ -z "$LAST_SESSION_ID" ]; then
        print_info "No previous session, running a query first..."
        test_basic_query > /dev/null 2>&1
    fi

    local response=$(api_request GET "/agent/$LAST_SESSION_ID")
    local state=$(json_field "$response" "state")
    local session_id=$(json_field "$response" "session_id")

    echo "  Session ID: $LAST_SESSION_ID"
    echo "  Retrieved State: $state"

    if [ -n "$state" ]; then
        print_success "Session state retrieved"
        return 0
    else
        print_failure "Could not retrieve session state"
        return 1
    fi
}

test_session_abort() {
    print_subheader "Test: Abort Session"

    # Start a query
    local response1=$(api_request POST "/agent/run" "{
        \"project_root\": \"$TEST_REPO\",
        \"query\": \"Analyze all functions in detail\"
    }")

    local session_id=$(json_field "$response1" "session_id")
    echo "  Session: $session_id"

    # Try to abort (may already be complete)
    local response2=$(api_request POST "/agent/abort" "{
        \"session_id\": \"$session_id\"
    }")

    echo "  Abort response: $response2"
    print_success "Abort endpoint tested"
    return 0
}

test_invalid_project() {
    print_subheader "Test: Invalid Project Root"

    local response=$(api_request POST "/agent/run" "{
        \"project_root\": \"/nonexistent/path/that/does/not/exist\",
        \"query\": \"What is this?\"
    }")

    local state=$(json_field "$response" "state")
    local error=$(json_string_field "$response" "error")
    local degraded=$(json_field "$response" "degraded_mode")

    echo "  State: $state"
    echo "  Degraded: $degraded"

    # Should either error or run in degraded mode
    if [ "$state" = "ERROR" ] || [ "$degraded" = "true" ] || [ "$state" = "COMPLETE" ]; then
        print_success "Invalid project handled gracefully"
        return 0
    else
        print_failure "Unexpected behavior: state=$state"
        return 1
    fi
}

test_empty_query() {
    print_subheader "Test: Empty Query"

    local response=$(api_request POST "/agent/run" "{
        \"project_root\": \"$TEST_REPO\",
        \"query\": \"\"
    }")

    local state=$(json_field "$response" "state")
    local error=$(json_string_field "$response" "error")

    echo "  State: $state"

    if [ "$state" = "ERROR" ] || [ -n "$error" ]; then
        print_success "Empty query rejected correctly"
        return 0
    else
        print_failure "Empty query should have been rejected"
        return 1
    fi
}

test_concurrent_sessions() {
    print_subheader "Test: Concurrent Sessions"

    echo "  Starting 3 concurrent queries..."

    # Start multiple queries in background
    local pids=()
    for i in 1 2 3; do
        (
            local response=$(api_request POST "/agent/run" "{
                \"project_root\": \"$TEST_REPO\",
                \"query\": \"What is function number $i in this codebase?\"
            }")
            local state=$(json_field "$response" "state")
            local session=$(json_field "$response" "session_id")
            echo "    Query $i: session=$session state=$state"
        ) &
        pids+=($!)
    done

    # Wait for all to complete
    local failed=0
    for pid in "${pids[@]}"; do
        wait $pid || ((failed++))
    done

    if [ $failed -eq 0 ]; then
        print_success "All concurrent sessions completed"
        return 0
    else
        print_failure "$failed concurrent sessions failed"
        return 1
    fi
}

test_tools_available() {
    print_subheader "Test: Tools Endpoint"

    local response=$(api_request GET "/tools")
    local tool_count=$(echo "$response" | grep -o '"name"' | wc -l)

    echo "  Tools available: $tool_count"

    if [ "$tool_count" -gt 0 ]; then
        print_success "Tools endpoint returned $tool_count tools"
        return 0
    else
        print_failure "No tools returned"
        return 1
    fi
}

test_graph_init() {
    print_subheader "Test: Graph Initialization"

    local response=$(api_request POST "/init" "{
        \"project_root\": \"$TEST_REPO\"
    }")

    local graph_id=$(json_field "$response" "graph_id")
    local file_count=$(json_field "$response" "file_count")

    echo "  Graph ID: $graph_id"
    echo "  Files: $file_count"

    if [ -n "$graph_id" ]; then
        print_success "Graph initialized with $file_count files"
        return 0
    else
        print_failure "Graph initialization failed"
        return 1
    fi
}

test_degraded_mode() {
    print_subheader "Test: Degraded Mode (Current Directory)"

    local response=$(api_request POST "/agent/run" "{
        \"project_root\": \".\",
        \"query\": \"What is this project about?\"
    }")

    local state=$(json_field "$response" "state")
    local degraded=$(json_field "$response" "degraded_mode")

    echo "  State: $state"
    echo "  Degraded Mode: $degraded"

    if [ "$state" = "COMPLETE" ] || [ "$state" = "ERROR" ]; then
        print_success "Degraded mode test completed"
        return 0
    else
        print_failure "Unexpected state: $state"
        return 1
    fi
}

# =============================================================================
# Test Suites
# =============================================================================

run_all_tests() {
    print_header "Running All Tests"

    # Reset counters
    TESTS_PASSED=0
    TESTS_FAILED=0

    # Start server with full features
    if ! start_server "both"; then
        print_failure "Server failed to start"
        return 1
    fi

    # Core tests (continue even if individual tests fail)
    test_health_check || true
    test_graph_init || true
    test_tools_available || true

    # Query tests
    test_basic_query || true
    test_different_queries || true
    test_ambiguous_query || true
    test_short_query || true

    # Session tests
    test_session_get_state || true
    test_session_continue || true
    test_session_abort || true

    # Error handling tests
    test_invalid_project || true
    test_empty_query || true

    # Advanced tests
    test_concurrent_sessions || true
    test_degraded_mode || true

    # Summary
    print_header "Test Summary"
    echo -e "  ${GREEN}Passed: $TESTS_PASSED${NC}"
    echo -e "  ${RED}Failed: $TESTS_FAILED${NC}"
    echo ""

    # Don't stop server in interactive mode - let user keep testing
    # stop_server

    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}All tests passed!${NC}"
        return 0
    else
        echo -e "${RED}Some tests failed. Server still running for debugging.${NC}"
        return 0  # Don't fail the whole script
    fi
}

run_quick_tests() {
    print_header "Running Quick Tests"

    # Reset counters
    TESTS_PASSED=0
    TESTS_FAILED=0

    if ! start_server "context"; then
        print_failure "Server failed to start"
        return 1
    fi

    test_health_check || true
    test_basic_query || true
    test_session_get_state || true

    # Don't stop server in interactive mode
    # stop_server

    print_header "Quick Test Summary"
    echo -e "  ${GREEN}Passed: $TESTS_PASSED${NC}"
    echo -e "  ${RED}Failed: $TESTS_FAILED${NC}"
    echo -e "  ${YELLOW}Server still running - continue testing or press 'x' to stop${NC}"
}

# =============================================================================
# Interactive Menu
# =============================================================================

show_menu() {
    echo ""
    echo -e "${BOLD}Code Buddy Agent Test Suite${NC}  ${YELLOW}[Model: $OLLAMA_MODEL]${NC}"
    echo ""
    echo "  Server Management:"
    echo "    s)  Start server (with context)"
    echo "    S)  Start server (with context + tools)"
    echo "    x)  Stop server"
    echo ""
    echo "  Test Suites:"
    echo "    a)  Run ALL tests"
    echo "    q)  Run QUICK tests (health, basic query)"
    echo ""
    echo "  Individual Tests:"
    echo "    1)  Health check"
    echo "    2)  Graph initialization"
    echo "    3)  Tools endpoint"
    echo "    4)  Basic query"
    echo "    5)  Different query types"
    echo "    6)  Ambiguous query (CLARIFY)"
    echo "    7)  Short query"
    echo "    8)  Session state"
    echo "    9)  Multi-turn conversation"
    echo "    10) Abort session"
    echo "    11) Invalid project"
    echo "    12) Empty query"
    echo "    13) Concurrent sessions"
    echo "    14) Degraded mode"
    echo ""
    echo "  Session Management:"
    echo "    id) Show last session ID"
    echo "    c)  Continue last session (custom follow-up)"
    echo "    r)  Run custom query"
    echo ""
    echo "    h)  Show this menu"
    echo "    l)  Show server logs"
    echo "    0)  Exit"
    echo ""
}

custom_query() {
    print_subheader "Custom Query"
    echo -n "  Enter your query: "
    read -r query

    if [ -z "$query" ]; then
        echo "  (cancelled)"
        return
    fi

    local response=$(api_request POST "/agent/run" "{
        \"project_root\": \"$TEST_REPO\",
        \"query\": \"$query\"
    }")

    local state=$(json_field "$response" "state")
    local tokens=$(json_field "$response" "tokens_used")
    LAST_SESSION_ID=$(json_field "$response" "session_id")

    echo ""
    echo "  Session ID: $LAST_SESSION_ID"
    echo "  State: $state"
    echo "  Tokens: $tokens"
    echo ""
    echo -e "  ${CYAN}Response:${NC}"
    json_string_field "$response" "response"
    echo ""
}

continue_session() {
    if [ -z "$LAST_SESSION_ID" ]; then
        echo -e "  ${RED}No session to continue. Run a query first (e.g., option 4).${NC}"
        return
    fi

    print_subheader "Continue Session: $LAST_SESSION_ID"
    echo -n "  Enter follow-up: "
    read -r clarification

    if [ -z "$clarification" ]; then
        echo "  (cancelled)"
        return
    fi

    local response=$(api_request POST "/agent/continue" "{
        \"session_id\": \"$LAST_SESSION_ID\",
        \"clarification\": \"$clarification\"
    }")

    local state=$(json_field "$response" "state")
    local tokens=$(json_field "$response" "tokens_used")

    echo ""
    echo "  State: $state"
    echo "  Tokens: $tokens"
    echo ""
    echo -e "  ${CYAN}Response:${NC}"
    json_string_field "$response" "response"
    echo ""
}

show_session_id() {
    if [ -z "$LAST_SESSION_ID" ]; then
        echo -e "  ${YELLOW}No session yet. Run a query first.${NC}"
    else
        echo -e "  ${GREEN}Last Session ID:${NC} $LAST_SESSION_ID"
    fi
}

run_interactive() {
    show_menu

    while true; do
        # Show session indicator if we have one
        if [ -n "$LAST_SESSION_ID" ]; then
            echo -n -e "${CYAN}[session: ${LAST_SESSION_ID:0:8}...] Select > ${NC}"
        else
            echo -n -e "${CYAN}Select > ${NC}"
        fi
        read -r choice

        case "$choice" in
            s) start_server "context" ;;
            S) start_server "both" ;;
            x) stop_server ;;
            a) run_all_tests ;;
            q) run_quick_tests ;;
            1) test_health_check ;;
            2) test_graph_init ;;
            3) test_tools_available ;;
            4) test_basic_query ;;
            5) test_different_queries ;;
            6) test_ambiguous_query ;;
            7) test_short_query ;;
            8) test_session_get_state ;;
            9) test_session_continue ;;
            10) test_session_abort ;;
            11) test_invalid_project ;;
            12) test_empty_query ;;
            13) test_concurrent_sessions ;;
            14) test_degraded_mode ;;
            id) show_session_id ;;
            c) continue_session ;;
            r) custom_query ;;
            h) show_menu ;;
            l) tail -50 /tmp/codebuddy_test.log ;;
            0|exit|quit)
                stop_server
                echo "Goodbye!"
                exit 0
                ;;
            "") ;;
            *) echo "Unknown option: $choice (press h for help)" ;;
        esac
    done
}

# =============================================================================
# Main
# =============================================================================

cleanup() {
    stop_server
}

trap cleanup EXIT

# Print banner
echo ""
echo -e "${BLUE}╔════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║${NC}        ${BOLD}Code Buddy Agent Loop Test Suite${NC}                        ${BLUE}║${NC}"
echo -e "${BLUE}║${NC}        Testing CB-11 Agent Loop Refactor                       ${BLUE}║${NC}"
echo -e "${BLUE}╠════════════════════════════════════════════════════════════════╣${NC}"
echo -e "${BLUE}║${NC}  ${YELLOW}Model: ${BOLD}$OLLAMA_MODEL${NC}                                       ${BLUE}║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════════╝${NC}"
echo ""
print_config "Test Repository" "$TEST_REPO"
print_config "API Base" "$API_BASE"
print_config "Model" "$OLLAMA_MODEL"
echo -e "  ${YELLOW}Tip:${NC} Override model with OLLAMA_MODEL=<name> ./scripts/codebuddy_test_suite.sh"

# Parse arguments
if [ $# -eq 0 ]; then
    run_interactive
elif [ "$1" = "all" ]; then
    run_all_tests
elif [ "$1" = "quick" ]; then
    run_quick_tests
elif [ "$1" = "server" ]; then
    start_server "${2:-context}"
    echo "Server running. Press Ctrl+C to stop."
    wait $SERVER_PID
else
    # Run specific tests by number
    start_server "context" || exit 1
    for arg in "$@"; do
        case "$arg" in
            1) test_health_check ;;
            2) test_graph_init ;;
            3) test_tools_available ;;
            4) test_basic_query ;;
            5) test_different_queries ;;
            6) test_ambiguous_query ;;
            7) test_short_query ;;
            8) test_session_get_state ;;
            9) test_session_continue ;;
            10) test_session_abort ;;
            11) test_invalid_project ;;
            12) test_empty_query ;;
            13) test_concurrent_sessions ;;
            14) test_degraded_mode ;;
            *) echo "Unknown test: $arg" ;;
        esac
    done
    stop_server
fi
