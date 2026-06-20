#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "=== Zero-setup smoke test (no Postgres, no .env) ==="

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

export CGO_ENABLED=0
go build -o "$TMPDIR/evmwalletbot" ./cmd

OUT="$TMPDIR/out"
mkdir -p "$OUT"

echo "Running non-interactive generation..."
"$TMPDIR/evmwalletbot" --count 5 --export-mode paired --export-dir "$OUT"

ADDR_LINES=$(wc -l < "$OUT/address.txt" | tr -d ' ')
KEY_LINES=$(wc -l < "$OUT/privatekey.txt" | tr -d ' ')

if [ "$ADDR_LINES" != "5" ] || [ "$KEY_LINES" != "5" ]; then
    echo "ERROR: expected 5 lines in export files, got address=$ADDR_LINES key=$KEY_LINES"
    exit 1
fi

if ! find "$TMPDIR" -name 'wallets.db' | grep -q .; then
    echo "ERROR: wallets.db was not created"
    exit 1
fi

echo "Running interactive menu launch check..."
printf '0\n' | "$TMPDIR/evmwalletbot" | grep -q "Schema ready (sqlite backend)"

echo "=== ✓ SMOKE TEST PASSED ==="
