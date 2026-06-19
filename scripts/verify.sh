#!/bin/bash
set -euo pipefail

echo "=== Running Full Verification Loop ==="
echo ""

echo "1. Checking code formatting..."
UNFORMATTED=$(gofmt -l . 2>/dev/null || true)
if [ -n "$UNFORMATTED" ]; then
    echo "ERROR: The following files are not formatted:"
    echo "$UNFORMATTED"
    exit 1
fi
echo "✓ All files properly formatted"

echo ""
echo "2. Building all packages..."
if ! go build ./... 2>&1; then
    echo "ERROR: Build failed"
    exit 1
fi
echo "✓ Build successful"

echo ""
echo "3. Running go vet..."
if ! go vet ./... 2>&1; then
    echo "ERROR: Vet failed"
    exit 1
fi
echo "✓ Vet passed"

echo ""
echo "4. Checking for carriage returns in .go files..."
if grep -rl $'\r' --include='*.go' . 2>/dev/null; then
    echo "ERROR: Found carriage returns in .go files"
    exit 1
fi
echo "✓ No carriage returns found"

echo ""
echo "5. Running tests with race detector..."
# Check if Docker is available
if command -v docker &> /dev/null; then
    # Set up ephemeral Postgres for tests
    export TEST_DB_HOST=localhost
    export TEST_DB_PORT=5432
    export TEST_DB_NAME=walletdb_test
    export TEST_DB_USER=postgres
    export TEST_DB_PASSWORD=test

    # Check if postgres-test container is already running
    if docker ps --format '{{.Names}}' | grep -q '^postgres-test$'; then
        echo "Using existing postgres-test container..."
    else
        echo "Starting ephemeral Postgres..."
        docker run --rm -d \
            --name postgres-test \
            -e POSTGRES_PASSWORD=test \
            -e POSTGRES_DB=walletdb_test \
            -p 5432:5432 \
            postgres:16 >/dev/null 2>&1
        
        # Wait for Postgres to be ready
        echo "Waiting for Postgres to be ready..."
        for i in {1..30}; do
            if docker exec postgres-test pg_isready -U postgres >/dev/null 2>&1; then
                break
            fi
            sleep 1
        done
    fi

    # Run tests
    if ! go test ./... -race -count=1 2>&1; then
        echo "ERROR: Tests failed"
        docker stop postgres-test 2>/dev/null || true
        exit 1
    fi

    # Cleanup
    docker stop postgres-test 2>/dev/null || true
else
    echo "WARNING: Docker not available, running tests without ephemeral Postgres"
    if ! go test ./... -race -count=1 2>&1; then
        echo "ERROR: Tests failed"
        exit 1
    fi
fi

echo ""
echo "=== ✓ ALL VERIFICATION CHECKS PASSED ==="
