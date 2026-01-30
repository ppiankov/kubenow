#!/bin/bash
# kubenow v2.0 Verification Script
# Tests all commands, error handling, and edge cases

KUBENOW="./bin/kubenow"
PASSED=0
FAILED=0

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test result tracking
test_pass() {
    echo -e "${GREEN}✓${NC} $1"
    ((PASSED++))
}

test_fail() {
    echo -e "${RED}✗${NC} $1"
    ((FAILED++))
}

test_skip() {
    echo -e "${YELLOW}⊘${NC} $1 (skipped)"
}

echo "========================================="
echo "kubenow v2.0 Verification Suite"
echo "========================================="
echo ""

# Build first
echo "Building kubenow..."
make build > /dev/null 2>&1
if [ $? -eq 0 ]; then
    test_pass "Build succeeded"
else
    test_fail "Build failed"
    exit 1
fi
echo ""

# 1. Version Tests
echo "=== Version Tests ==="
if $KUBENOW version > /dev/null 2>&1; then
    VERSION=$($KUBENOW version | head -1)
    if [[ $VERSION == *"0.1.0"* ]]; then
        test_pass "Version command works (v0.1.0)"
    else
        test_fail "Version mismatch: $VERSION"
    fi
else
    test_fail "Version command failed"
fi
echo ""

# 2. Help Tests
echo "=== Help Tests ==="
if $KUBENOW --help > /dev/null 2>&1; then
    test_pass "Root help command works"
else
    test_fail "Root help command failed"
fi

if $KUBENOW analyze --help > /dev/null 2>&1; then
    test_pass "Analyze help command works"
else
    test_fail "Analyze help command failed"
fi

if $KUBENOW analyze requests-skew --help > /dev/null 2>&1; then
    test_pass "Requests-skew help command works"
else
    test_fail "Requests-skew help command failed"
fi

if $KUBENOW analyze node-footprint --help > /dev/null 2>&1; then
    test_pass "Node-footprint help command works"
else
    test_fail "Node-footprint help command failed"
fi

if $KUBENOW incident --help > /dev/null 2>&1; then
    test_pass "Incident help command works"
else
    test_fail "Incident help command failed"
fi
echo ""

# 3. Error Handling Tests
echo "=== Error Handling Tests ==="

# Missing required flags
$KUBENOW analyze requests-skew > /dev/null 2>&1
EXIT_CODE=$?
if [ $EXIT_CODE -ne 0 ]; then
    test_pass "Requests-skew fails without prometheus-url (exit $EXIT_CODE)"
else
    test_fail "Requests-skew should fail without prometheus-url"
fi

$KUBENOW analyze node-footprint > /dev/null 2>&1
EXIT_CODE=$?
if [ $EXIT_CODE -ne 0 ]; then
    test_pass "Node-footprint fails without prometheus-url (exit $EXIT_CODE)"
else
    test_fail "Node-footprint should fail without prometheus-url"
fi

$KUBENOW incident > /dev/null 2>&1
EXIT_CODE=$?
if [ $EXIT_CODE -ne 0 ]; then
    test_pass "Incident fails without LLM flags (exit $EXIT_CODE)"
else
    test_fail "Incident should fail without LLM flags"
fi

# Invalid output format
$KUBENOW analyze requests-skew --prometheus-url http://localhost:9090 --output invalid > /dev/null 2>&1
EXIT_CODE=$?
if [ $EXIT_CODE -ne 0 ]; then
    test_pass "Requests-skew fails with invalid output format (exit $EXIT_CODE)"
else
    test_fail "Requests-skew should fail with invalid output format"
fi
echo ""

# 4. Command Structure Tests
echo "=== Command Structure Tests (Breaking Changes) ==="

# Old syntax should NOT work
$KUBENOW --mode incident > /dev/null 2>&1
EXIT_CODE=$?
if [ $EXIT_CODE -ne 0 ]; then
    test_pass "Old syntax '--mode incident' correctly rejected"
else
    test_fail "Old syntax should be rejected"
fi

# New syntax structure check
if $KUBENOW incident --help > /dev/null 2>&1 && \
   $KUBENOW pod --help > /dev/null 2>&1 && \
   $KUBENOW teamlead --help > /dev/null 2>&1 && \
   $KUBENOW compliance --help > /dev/null 2>&1 && \
   $KUBENOW chaos --help > /dev/null 2>&1; then
    test_pass "All LLM subcommands exist"
else
    test_fail "Some LLM subcommands missing"
fi

if $KUBENOW analyze requests-skew --help > /dev/null 2>&1 && \
   $KUBENOW analyze node-footprint --help > /dev/null 2>&1; then
    test_pass "All analyze subcommands exist"
else
    test_fail "Some analyze subcommands missing"
fi
echo ""

# 5. Documentation Tests
echo "=== Documentation Tests ==="

if [ -f "README.md" ]; then
    if grep -q "v0.1.0\|Version 0.1.0" README.md; then
        test_pass "README.md updated for v0.1.0"
    else
        test_fail "README.md missing v0.1.0 version reference"
    fi
else
    test_fail "README.md not found"
fi

if [ -f "CONTRIBUTING.md" ]; then
    test_pass "CONTRIBUTING.md exists"
else
    test_fail "CONTRIBUTING.md not found"
fi

if [ -f "CHANGELOG.md" ]; then
    if grep -q "\[0.1.0\]" CHANGELOG.md; then
        test_pass "CHANGELOG.md has v0.1.0 entry"
    else
        test_fail "CHANGELOG.md missing v0.1.0 entry"
    fi
else
    test_fail "CHANGELOG.md not found"
fi

if [ -f "docs/architecture.md" ]; then
    test_pass "Architecture docs exist"
else
    test_fail "Architecture docs not found"
fi
echo ""

# 6. CI/CD Configuration Tests
echo "=== CI/CD Configuration Tests ==="

if [ -f ".github/workflows/ci.yml" ]; then
    test_pass "CI workflow exists"
else
    test_fail "CI workflow not found"
fi

if [ -f ".github/workflows/release.yml" ]; then
    test_pass "Release workflow exists"
else
    test_fail "Release workflow not found"
fi

if [ -f ".goreleaser.yml" ]; then
    test_pass "GoReleaser config exists"
else
    test_fail "GoReleaser config not found"
fi

if [ -f "Makefile" ]; then
    if grep -q "build" Makefile && \
       grep -q "test" Makefile && \
       grep -q "lint" Makefile; then
        test_pass "Makefile has essential targets"
    else
        test_fail "Makefile missing targets"
    fi
else
    test_fail "Makefile not found"
fi
echo ""

# 7. Test Coverage
echo "=== Test Coverage ==="
go test ./... > /dev/null 2>&1
if [ $? -eq 0 ]; then
    test_pass "All unit tests pass"
else
    test_fail "Some unit tests failed"
fi

# Check coverage
COVERAGE=$(go test -coverprofile=coverage.tmp ./internal/analyzer ./internal/metrics 2>/dev/null | grep "coverage:" | awk '{sum+=$5; count++} END {print sum/count}')
rm -f coverage.tmp
if [ -n "$COVERAGE" ]; then
    test_pass "Test coverage generated"
else
    test_skip "Coverage calculation"
fi
echo ""

# 8. Flag Validation Tests
echo "=== Flag Validation Tests ==="

# Check that global flags work before subcommands
$KUBENOW --namespace test incident --help > /dev/null 2>&1
if [ $? -eq 0 ]; then
    test_pass "Global flags work before subcommands"
else
    test_fail "Global flags should work before subcommands"
fi

# Check verbose flag
$KUBENOW --verbose analyze requests-skew --help > /dev/null 2>&1
if [ $? -eq 0 ]; then
    test_pass "Verbose flag works"
else
    test_fail "Verbose flag failed"
fi
echo ""

# 9. Code Quality Tests
echo "=== Code Quality Tests ==="

# Check formatting
UNFORMATTED=$(gofmt -l . | wc -l)
if [ "$UNFORMATTED" -eq 0 ]; then
    test_pass "All code properly formatted"
else
    test_fail "$UNFORMATTED files need formatting"
fi

# Check go vet
go vet ./... > /dev/null 2>&1
if [ $? -eq 0 ]; then
    test_pass "Go vet passes"
else
    test_fail "Go vet found issues"
fi
echo ""

# Summary
echo "========================================="
echo "Verification Summary"
echo "========================================="
echo -e "Passed: ${GREEN}$PASSED${NC}"
echo -e "Failed: ${RED}$FAILED${NC}"
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}✓ All tests passed! Ready for release.${NC}"
    exit 0
else
    echo -e "${RED}✗ Some tests failed. Review and fix before release.${NC}"
    exit 1
fi
