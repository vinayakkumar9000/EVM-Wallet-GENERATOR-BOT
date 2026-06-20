# EVM Wallet Bot

High-performance CLI tool for generating and managing EVM-compatible wallets.  
Built in **Go** — **zero setup by default** (embedded SQLite, single binary).

**Supported chains:** Ethereum · BSC · Polygon · Arbitrum · Optimism · Base · any EVM chain

---

## Quick Start (zero setup)

Download or build **one file** and run it. No PostgreSQL, Docker, or `.env` required.

```powershell
# Windows — build a portable .exe (no CGo)
go build -o evmwalletbot.exe ./cmd
.\evmwalletbot.exe
```

```bash
# Linux / macOS
go build -o evmwalletbot ./cmd
./evmwalletbot
```

The app automatically:

- Creates an embedded SQLite database (`wallets.db`) next to the executable, or under your user config directory if that folder is not writable
- Runs schema migrations on first launch
- Opens the interactive menu (Generate, Stats, Lookup, and more)

**Non-interactive example** (generate 5 wallets + export):

```bash
./evmwalletbot --count 5 --export-mode paired --export-dir ./out
```

Creates `./out/address.txt` and `./out/privatekey.txt` plus the embedded database.

---

## Optional: PostgreSQL mode

For multi-user or server deployments, enable PostgreSQL explicitly:

```bash
# Environment
export STORAGE=postgres          # or DB_ENABLED=true
export DB_HOST=localhost
export DB_USER=postgres
export DB_PASSWORD=secret
export DB_NAME=walletdb

./evmwalletbot -storage postgres
```

If PostgreSQL is requested but unreachable, the app **warns and falls back to embedded SQLite** — it does not exit.

---

## Table of Contents

1. [Architecture](#architecture)
2. [Usage](#usage)
3. [Configuration](#configuration)
4. [Verification](#verification)
5. [Security Notes](#security-notes)

---

## Architecture

```
evmwalletbot/
├── cmd/              main.go, storage_factory.go   ← entry point
├── config/           config.go                    ← .env / env (optional)
├── storage/          interface.go                 ← pluggable storage
│   ├── sqlite/       embedded default (modernc.org/sqlite, pure Go)
│   └── postgres/     optional external backend
├── wallet/           generator.go                 ← secp256k1 key gen
├── core/             generator.go, stats.go       ← parallel engine
├── cli/              menu.go                      ← interactive menu
└── scripts/verify.sh                              ← CI / local checks
```

**Default storage:** embedded SQLite (`STORAGE=sqlite`).  
**Optional storage:** PostgreSQL (`STORAGE=postgres` or `DB_ENABLED=true`).

---

## Usage

### Flags

| Flag | Description |
|------|-------------|
| `--count N` | Generate N wallets and exit |
| `--export-mode MODE` | `paired`, `key-only`, `address-only`, `combined` |
| `--export-dir PATH` | Export directory (enables export when set with mode) |
| `--storage TYPE` | `sqlite` (default) or `postgres` |
| `--data-dir PATH` | SQLite data directory (auto-created) |
| `--version` | Show version |
| `--help` | Show help |

### Interactive menu

Run `./evmwalletbot` with no flags. Main options:

- **Generate** — parallel wallet generation with progress
- **Statistics** — counts, storage size, live watch
- **Lookup** — find wallet by ID
- **Database & monitoring** — health check, pool status (PostgreSQL only)
- **Benchmark / tuning** — worker and batch comparisons
- **Vanity** — pattern-matched addresses

---

## Configuration

All settings are optional. Defaults work with no `.env` file.

| Variable | Default | Description |
|----------|---------|-------------|
| `STORAGE` | `sqlite` | `sqlite` or `postgres` |
| `DB_ENABLED` | `false` | Set `true` to opt into PostgreSQL |
| `WALLET_DATA_DIR` | (auto) | SQLite database directory |
| `WORKERS` | `16` | Parallel generators |
| `BATCH_SIZE` | `500` | Wallets per storage batch |
| `EXPORT_ENABLED` | `false` | Enable file export |
| `EXPORT_MODE` | `paired` | Export format |
| `EXPORT_DIR` | `./exports` | Export output path |

PostgreSQL-only variables (`DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, pool settings) are validated only when `STORAGE=postgres`.

---

## Verification

```bash
bash scripts/verify.sh
```

Runs `gofmt`, `go build`, `go vet`, Windows cross-compile, carriage-return check, and `go test -race`.

---

## Security Notes

- Private keys are never printed to logs (except during explicit export).
- Embedded SQLite stores keys locally in `wallets.db` — back up and protect this file.
- Export files contain plaintext keys — treat as highly sensitive.
- PostgreSQL mode: use SSL and restrict network access in production.

---

## Credits

Key derivation inspired by [Planxnx/ethereum-wallet-generator](https://github.com/Planxnx/ethereum-wallet-generator).  
Cryptography: [go-ethereum](https://github.com/ethereum/go-ethereum).
