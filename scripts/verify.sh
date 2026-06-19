#!/bin/bash
set -euo pipefail

echo "=== Running verification loop ==="
echo ""

echo "1. Checking formatting..."
UNFORMATTED=$(gofmt -l . || true)
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

echo "4. Checking for carriage returns in .go files..."
if grep -rl $'\r' --include='*.go' . 2>/dev/null; then
    echo "ERROR: Found carriage returns in .go files"
    exit 1
fi
echo "✓ No carriage returns"
echo ""

echo "5. Running tests with race detector..."
go test ./... -race -count=1 || exit 1
echo "✓ All tests passed"
echo ""

echo ""
echo "=== ✓ ALL CHECKS PASSED ==="
exit 0