#!/bin/bash
# Test runner script for CelesteCLI Docker testing
# Runs all test binaries and generates reports

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Directories
TEST_DIR="/app/tests"
REPORT_DIR="/app/reports"
FIXTURES_DIR="/app/fixtures"

# Create report directory
mkdir -p "$REPORT_DIR"

# Test results tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

echo "════════════════════════════════════════════════════════════════"
echo "  CelesteCLI Test Suite"
echo "════════════════════════════════════════════════════════════════"
echo ""

# Check if mock API is available
echo "🔍 Checking mock API server..."
if curl -s http://mock-api:8080/health > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Mock API server is healthy${NC}"
else
    echo -e "${YELLOW}⚠ Warning: Mock API server not reachable${NC}"
fi
echo ""

# Function to run a test binary
run_test() {
    local test_name=$1
    local test_binary=$2

    echo "────────────────────────────────────────────────────────────────"
    echo "📦 Running: $test_name"
    echo "────────────────────────────────────────────────────────────────"

    if [ ! -f "$test_binary" ]; then
        echo -e "${YELLOW}⚠ Test binary not found: $test_binary${NC}"
        echo ""
        return 0
    fi

    # Run test with verbose output and JSON format
    set +e
    "$test_binary" -test.v -test.timeout=30s > "$REPORT_DIR/${test_name}.log" 2>&1
    TEST_EXIT_CODE=$?
    set -e

    # Parse results
    if [ $TEST_EXIT_CODE -eq 0 ]; then
        echo -e "${GREEN}✓ PASS${NC} - $test_name"
        PASSED_TESTS=$((PASSED_TESTS + 1))
    else
        echo -e "${RED}✗ FAIL${NC} - $test_name (exit code: $TEST_EXIT_CODE)"
        FAILED_TESTS=$((FAILED_TESTS + 1))

        # Show last 20 lines of failure
        echo ""
        echo "Last 20 lines of output:"
        tail -n 20 "$REPORT_DIR/${test_name}.log"
    fi

    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    echo ""
}

# Run all test binaries
echo "🚀 Starting test execution..."
echo ""

# Skills tests
run_test "skills" "$TEST_DIR/skills_test"

# Config tests
run_test "config" "$TEST_DIR/config_test"

# LLM tests
run_test "llm" "$TEST_DIR/llm_test"

# TUI tests (if they exist)
run_test "tui" "$TEST_DIR/tui_test"

# Print summary
echo "════════════════════════════════════════════════════════════════"
echo "  Test Summary"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "Total Tests:  $TOTAL_TESTS"
echo -e "${GREEN}Passed:       $PASSED_TESTS${NC}"
echo -e "${RED}Failed:       $FAILED_TESTS${NC}"
echo ""

# Calculate pass rate
if [ $TOTAL_TESTS -gt 0 ]; then
    PASS_RATE=$((PASSED_TESTS * 100 / TOTAL_TESTS))
    echo "Pass Rate:    ${PASS_RATE}%"
else
    echo "Pass Rate:    N/A (no tests found)"
fi

echo ""
echo "📁 Test logs saved to: $REPORT_DIR"
echo ""

# Generate JSON report
cat > "$REPORT_DIR/summary.json" <<EOF
{
  "total": $TOTAL_TESTS,
  "passed": $PASSED_TESTS,
  "failed": $FAILED_TESTS,
  "pass_rate": $PASS_RATE,
  "timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "environment": {
    "mock_api": "http://mock-api:8080",
    "fixtures_dir": "$FIXTURES_DIR"
  }
}
EOF

echo "✅ Test summary written to: $REPORT_DIR/summary.json"
echo ""

# Exit with failure if any tests failed
if [ $FAILED_TESTS -gt 0 ]; then
    echo -e "${RED}════════════════════════════════════════════════════════════════${NC}"
    echo -e "${RED}  TEST SUITE FAILED - $FAILED_TESTS test(s) failed${NC}"
    echo -e "${RED}════════════════════════════════════════════════════════════════${NC}"
    exit 1
else
    echo -e "${GREEN}════════════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}  ALL TESTS PASSED ✓${NC}"
    echo -e "${GREEN}════════════════════════════════════════════════════════════════${NC}"
    exit 0
fi
