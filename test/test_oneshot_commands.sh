#!/bin/bash
# Celeste One-Shot Commands Test Framework
# Tests all CLI commands without entering TUI mode

set -e  # Exit on error

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Binary location
CELESTE="${CELESTE_BIN:-./celeste}"

# Test result tracking
declare -a FAILED_TESTS

# Print test header
print_header() {
    echo "═══════════════════════════════════════════════════════════════"
    echo "  CELESTE ONE-SHOT COMMANDS TEST SUITE"
    echo "═══════════════════════════════════════════════════════════════"
    echo ""
    echo "Testing binary: $CELESTE"
    echo ""
}

# Test runner function
run_test() {
    local test_name="$1"
    local command="$2"
    local expected_pattern="$3"

    TESTS_RUN=$((TESTS_RUN + 1))
    echo -n "[$TESTS_RUN] Testing: $test_name ... "

    # Run command and capture output
    output=$($command 2>&1) || true

    # Check if output matches expected pattern
    if echo "$output" | grep -qi "$expected_pattern"; then
        echo -e "${GREEN}PASS${NC}"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    else
        echo -e "${RED}FAIL${NC}"
        echo "  Expected pattern: $expected_pattern"
        echo "  Got output: ${output:0:100}..."
        TESTS_FAILED=$((TESTS_FAILED + 1))
        FAILED_TESTS+=("$test_name")
        return 1
    fi
}

# Print section header
section() {
    echo ""
    echo "─────────────────────────────────────────────────────────────"
    echo "  $1"
    echo "─────────────────────────────────────────────────────────────"
}

# Main test execution
main() {
    print_header

    # Check if binary exists
    if [ ! -f "$CELESTE" ]; then
        echo -e "${RED}ERROR: Binary not found at $CELESTE${NC}"
        echo "Build it first with: go build -o celeste cmd/celeste/*.go"
        exit 1
    fi

    # Test: Version
    section "VERSION & HELP COMMANDS"
    run_test "version command" "$CELESTE version" "Celeste CLI"
    run_test "version flag" "$CELESTE --version" "Celeste CLI"
    run_test "help command" "$CELESTE help" "Usage"

    # Test: Config
    section "CONFIG COMMANDS"
    run_test "config --show" "$CELESTE config --show" "base_url\|Configuration"
    run_test "config --list" "$CELESTE config --list" "Available\|config"

    # Test: Skills
    section "SKILLS COMMANDS"
    run_test "skills --list" "$CELESTE skills --list" "Available Skills"
    run_test "skills list (no flag)" "$CELESTE skills" "Available Skills"
    run_test "skills --info generate_uuid" "$CELESTE skills --info generate_uuid" "SKILL:\|Status:"
    run_test "skills --reload" "$CELESTE skills --reload" "Reloaded\|skills"

    # Test: Providers
    section "PROVIDERS COMMANDS"
    run_test "providers list" "$CELESTE providers" "AI PROVIDERS\|openai\|grok"
    run_test "providers --tools" "$CELESTE providers --tools" "TOOL-CAPABLE\|openai"
    run_test "providers info openai" "$CELESTE providers info openai" "PROVIDER: OPENAI\|Function Calling"
    run_test "providers current" "$CELESTE providers current" "PROVIDER:\|OpenAI Compatible"
    run_test "providers info grok" "$CELESTE providers info grok" "PROVIDER: GROK"
    run_test "providers info anthropic" "$CELESTE providers info anthropic" "PROVIDER: ANTHROPIC"

    # Test: Sessions (read-only tests)
    section "SESSION COMMANDS"
    run_test "session --list" "$CELESTE session --list" "Saved Sessions\|No sessions"

    # Test: Context/Stats (may not have data yet)
    section "CONTEXT & STATS COMMANDS"
    run_test "context command" "$CELESTE context" "context\|No active sessions"
    run_test "stats command" "$CELESTE stats" "ANテL\|sessions\|No sessions"

    # Test: Skill execution (safe skills only)
    section "SKILL EXECUTION"
    run_test "skill generate_uuid" "$CELESTE skill generate_uuid" "uuid"
    run_test "skill generate_password" "$CELESTE skill generate_password" "password"
    run_test "skill generate_password --length 32" "$CELESTE skill generate_password --length 32" "password"

    # Print summary
    echo ""
    echo "═══════════════════════════════════════════════════════════════"
    echo "  TEST SUMMARY"
    echo "═══════════════════════════════════════════════════════════════"
    echo ""
    echo "Tests Run:    $TESTS_RUN"
    echo -e "Tests Passed: ${GREEN}$TESTS_PASSED${NC}"
    echo -e "Tests Failed: ${RED}$TESTS_FAILED${NC}"

    if [ $TESTS_FAILED -gt 0 ]; then
        echo ""
        echo "Failed tests:"
        for test in "${FAILED_TESTS[@]}"; do
            echo "  - $test"
        done
        echo ""
        exit 1
    else
        echo ""
        echo -e "${GREEN}All tests passed!${NC}"
        echo ""
        exit 0
    fi
}

# Run tests
main "$@"
