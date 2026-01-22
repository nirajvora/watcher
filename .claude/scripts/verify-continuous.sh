#!/bin/bash
# Continuous Verification Script
# Runs verification loop until all criteria pass
#
# Usage: ./verify-continuous.sh [max_iterations]

set -e

MAX_ITERATIONS=${1:-10}
ITERATION=0
ALL_PASSED=false

echo "=========================================="
echo "  DEX Arbitrage System Verification"
echo "=========================================="
echo ""
echo "Max iterations: $MAX_ITERATIONS"
echo "Starting verification loop..."
echo ""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

check_tests() {
    echo "Running tests..."
    if go test -v -race ./... 2>&1 | tee /tmp/test-output.txt; then
        echo -e "${GREEN}✓ Tests passed${NC}"
        return 0
    else
        echo -e "${RED}✗ Tests failed${NC}"
        return 1
    fi
}

check_coverage() {
    echo "Checking coverage..."
    go test -coverprofile=/tmp/coverage.out ./... 2>/dev/null
    coverage=$(go tool cover -func=/tmp/coverage.out | grep total | awk '{print $3}' | sed 's/%//')
    
    if (( $(echo "$coverage >= 80" | bc -l) )); then
        echo -e "${GREEN}✓ Coverage: ${coverage}% (≥80%)${NC}"
        return 0
    else
        echo -e "${RED}✗ Coverage: ${coverage}% (<80%)${NC}"
        return 1
    fi
}

check_build() {
    echo "Checking build..."
    if go build ./... 2>&1; then
        echo -e "${GREEN}✓ Build successful${NC}"
        return 0
    else
        echo -e "${RED}✗ Build failed${NC}"
        return 1
    fi
}

check_race() {
    echo "Checking for race conditions..."
    if go test -race ./... 2>&1 | grep -q "race detected"; then
        echo -e "${RED}✗ Race condition detected${NC}"
        return 1
    else
        echo -e "${GREEN}✓ No race conditions${NC}"
        return 0
    fi
}

check_lint() {
    echo "Running linter..."
    if command -v golangci-lint &> /dev/null; then
        if golangci-lint run ./... 2>&1; then
            echo -e "${GREEN}✓ Lint passed${NC}"
            return 0
        else
            echo -e "${YELLOW}⚠ Lint warnings${NC}"
            return 0  # Don't fail on lint warnings
        fi
    else
        echo -e "${YELLOW}⚠ golangci-lint not installed, skipping${NC}"
        return 0
    fi
}

run_verification() {
    echo ""
    echo "=========================================="
    echo "  Iteration $((ITERATION + 1))/$MAX_ITERATIONS"
    echo "=========================================="
    echo ""
    
    local failed=0
    
    check_build || ((failed++))
    check_tests || ((failed++))
    check_coverage || ((failed++))
    check_race || ((failed++))
    check_lint || ((failed++))
    
    echo ""
    if [ $failed -eq 0 ]; then
        echo -e "${GREEN}=========================================="
        echo "  ALL CHECKS PASSED!"
        echo "==========================================${NC}"
        return 0
    else
        echo -e "${RED}=========================================="
        echo "  $failed CHECK(S) FAILED"
        echo "==========================================${NC}"
        return 1
    fi
}

# Main loop
while [ $ITERATION -lt $MAX_ITERATIONS ]; do
    if run_verification; then
        ALL_PASSED=true
        break
    fi
    
    ((ITERATION++))
    
    if [ $ITERATION -lt $MAX_ITERATIONS ]; then
        echo ""
        echo -e "${YELLOW}Waiting 5 seconds before next iteration...${NC}"
        echo "Press Ctrl+C to stop"
        sleep 5
    fi
done

echo ""
if [ "$ALL_PASSED" = true ]; then
    echo -e "${GREEN}=========================================="
    echo "  VERIFICATION COMPLETE"
    echo "  All criteria met after $((ITERATION + 1)) iteration(s)"
    echo "==========================================${NC}"
    exit 0
else
    echo -e "${RED}=========================================="
    echo "  VERIFICATION INCOMPLETE"
    echo "  Max iterations ($MAX_ITERATIONS) reached"
    echo "  Manual intervention required"
    echo "==========================================${NC}"
    exit 1
fi
