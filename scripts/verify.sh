#!/bin/bash
set -euo pipefail

echo "=== Running verification loop ==="
echo ""

echo "1. Checking formatting..."
UNFORMATTED=$(gofmt -l $(go list -f '{{.Dir}}' -find ./...) || true)
if [ -n "$UNFORMATTED" ]; then
    echo "ERROR: Unformatted files:"
    echo "$UNFORMATTED"
    exit 1
fi
echo "✓ All files formatted"
echo ""

echo "2. Building..."
go build ./... || exit 1
echo "✓ Build successful"
echo ""

echo "3. Running go vet..."
go vet ./... || exit 1
echo "✓ Vet passed"
echo ""

echo "4. Cross-compiling Windows binary (no CGo)..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /tmp/evmwalletbot.exe ./cmd
echo "✓ Windows cross-compile successful"
echo ""

echo "6. Checking for carriage returns in .go files..."
CR_FILES=$(find . -name '*.go' -not -path './.git/*' -print0 | xargs -0 grep -l $'\r' 2>/dev/null || true)
if [ -n "$CR_FILES" ]; then
    echo "ERROR: Found carriage returns in .go files:"
    echo "$CR_FILES"
    exit 1
fi
echo "✓ No carriage returns"
echo ""

echo "7. Running tests with race detector..."
go test ./... -race -count=1 || exit 1
echo "✓ All tests passed"
echo ""

echo "=== ✓ ALL CHECKS PASSED ==="
exit 0
