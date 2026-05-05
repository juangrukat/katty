#!/usr/bin/env bash
# Katty-Go CI script — run all quality checks
# Usage: ./scripts/ci.sh [--fix]

set -euo pipefail
cd "$(dirname "$0")/.."

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass()  { echo -e "${GREEN}PASS${NC} $1"; }
fail()  { echo -e "${RED}FAIL${NC} $1"; exit 1; }
info()  { echo -e "${YELLOW}....${NC} $1"; }

FIX=false
if [[ "${1:-}" == "--fix" ]]; then
    FIX=true
    shift
fi

info "Katty-Go CI — $(date '+%Y-%m-%d %H:%M')"

# ── 1. Formatting ──
info "1/7 gofmt"
if $FIX; then
    gofmt -w . 2>&1
fi
if [[ -z "$(gofmt -l .)" ]]; then
    pass "gofmt — clean"
else
    fail "gofmt — unformatted files: $(gofmt -l .)"
fi

# ── 2. go vet ──
info "2/7 go vet"
go vet ./... && pass "go vet" || fail "go vet"

# ── 3. staticcheck ──
info "3/7 staticcheck"
STATICCHECK=""
if command -v staticcheck &>/dev/null; then
    STATICCHECK=staticcheck
elif [[ -x "$HOME/go/bin/staticcheck" ]]; then
    STATICCHECK="$HOME/go/bin/staticcheck"
fi
if [[ -n "$STATICCHECK" ]]; then
    $STATICCHECK ./... && pass "staticcheck" || fail "staticcheck"
else
    info "staticcheck not found — skipping (install: go install honnef.co/go/tools/cmd/staticcheck@latest)"
fi

# ── 4. golangci-lint (if installed) ──
info "4/7 golangci-lint"
if command -v golangci-lint &>/dev/null; then
    golangci-lint run ./... && pass "golangci-lint" || fail "golangci-lint"
else
    info "golangci-lint not found — skipping (install: brew install golangci-lint)"
fi

# ── 5. Unit tests + race + coverage ──
info "5/7 go test -race -cover"
go test -count=1 -race -cover ./... 2>&1 | tee /tmp/katty-test.out
if [[ ${PIPESTATUS[0]} -eq 0 ]]; then
    pass "go test -race -cover"
else
    fail "go test — failures detected"
fi

# ── 6. Fuzz (smoke: seed corpus only, fast) ──
info "6/7 fuzz smoke (seed corpus)"
fuzz_pkgs=$(go list ./... | while read pkg; do
    test_files=$(go list -f '{{.TestGoFiles}}' "$pkg" 2>/dev/null)
    if [[ "$test_files" == *"fuzz_test.go"* ]]; then echo "$pkg"; fi
done)
if [[ -z "$fuzz_pkgs" ]]; then
    pass "fuzz smoke — no fuzz tests found"
else
    for pkg in $fuzz_pkgs; do
        # Extract fuzz test names and run each one separately
        fuzz_names=$(go test -list='Fuzz.*' "$pkg" 2>/dev/null | grep '^Fuzz')
        for name in $fuzz_names; do
            go test -run='^$' -fuzz="^${name}$" -fuzztime=2s "$pkg" 2>&1 | tail -1
        done
    done
    pass "fuzz smoke"
fi

# ── 7. Benchmarks (smoke) ──
info "7/7 bench smoke"
go test -bench=. -benchtime=100ms ./... 2>&1 | grep -E '(Benchmark|ok )' | head -20
pass "bench smoke"

echo ""
echo -e "${GREEN}═══ All checks passed ═══${NC}"
