# EVM Wallet Bot

High-performance CLI tool for generating and managing millions of EVM-compatible wallets.  
Built in **Go**, backed by **PostgreSQL**, optimised for **Azure Ubuntu 24.04 LTS**.

**Supported chains:** Ethereum · BSC · Polygon · Arbitrum · Optimism · Base · any EVM chain

---

## Architecture

```
ethereum-wallet-bot/
├── cmd/            main.go              ← entry point
├── config/         config.go            ← .env loader
├── database/       db.go                ← connection pool
│                   migrations.go        ← auto schema
├── wallet/         generator.go         ← secp256k1 key gen
├── events/         events.go            ← event log system
├── core/           generator.go         ← parallel batch engine
│                   stats.go             ← statistics queries
├── cli/            menu.go              ← interactive menu
├── .env.example
├── install.sh                           ← one-shot Azure setup
└── go.mod
```

---

## Quick Start (local dev)

### Prerequisites

| Tool | Version |
|------|---------|
| Go | ≥ 1.22 |
| PostgreSQL | ≥ 15 (16 recommended) |

### 1 — Clone & configure

```bash
git clone https://github.com/youruser/ethereum-wallet-bot
cd ethereum-wallet-bot
cp .env.example .env
nano .env          # fill in DB credentials
```

### 2 — Build

```bash
go mod tidy
go build -ldflags="-s -w" -o evmwalletbot ./cmd
```

### 3 — Run

```bash
./evmwalletbot
```

The program will:
1. Connect to PostgreSQL
2. Auto-create the schema if missing
3. Show the interactive menu

---

## Azure Ubuntu 24.04 LTS — One-Shot Deployment

```bash
# Upload your project to the VPS first
scp -r ./ethereum-wallet-bot azureuser@<YOUR_IP>:~/

# SSH into the VPS
ssh azureuser@<YOUR_IP>

# Run the installer (installs Go, PostgreSQL, builds binary, tunes DB)
cd ~/ethereum-wallet-bot
chmod +x install.sh
./install.sh
```

The installer will:
- Install Go 1.22 system-wide
- Install and configure PostgreSQL 16
- Create an isolated DB user and password (auto-generated, written to `.env`)
- Apply PostgreSQL performance tuning for batch workloads
- Build the binary

### Keep it running with tmux

```bash
tmux new -s wallet
cd ~/ethereum-wallet-bot
./evmwalletbot
# Ctrl+B then D to detach
# tmux attach -t wallet to re-attach
```

---

## Usage — Interactive Menu

```
  ╔═══════════════════════════════════════════════════════╗
  ║         EVM  WALLET  MANAGER   v1.0                   ║
  ╚═══════════════════════════════════════════════════════╝

  ┌──────────────────────────────────────┐
  │   1   Generate wallets               │
  │   2   Show statistics                │
  │   3   Show wallet info               │
  │   4   Show recent events             │
  │   5   Exit                           │
  └──────────────────────────────────────┘
```

### Option 1 — Generate wallets

```
Enter number of wallet batches (1 batch = 1000 wallets): 5

[INFO] Generating 5000 wallets (5 batch(es) of 1000)

  Updating progress: 3500     / 5000      [████████████████████░░░░░░░░]   70.0%

[INFO] 5000 wallets successfully created in 2.14s  (2336 wallets/sec)
```

The progress line **rewrites itself in-place** — no flooding.

### Option 2 — Statistics

```
  ╔═════════════════════════════════════════════════╗
  ║              WALLET STATISTICS                  ║
  ╠═════════════════════════════════════════════════╣
  ║  Total wallets            : 125000              ║
  ║  Wallets created today    : 5000                ║
  ║  Unused wallets           : 124500              ║
  ║  Used wallets             : 500                 ║
  ╠═════════════════════════════════════════════════╣
  ║  Total events logged      : 125000              ║
  ║  Database size            : 48.2 MB             ║
  ╠═════════════════════════════════════════════════╣
  ║  Last wallet created      : 2026-03-05 14:22:10 ║
  ╚═════════════════════════════════════════════════╝
```

---

## Database Schema

### wallets

| Column      | Type        | Notes                        |
|-------------|-------------|------------------------------|
| id          | BIGSERIAL   | Primary key                  |
| address     | BYTEA       | 20 bytes — Ethereum address  |
| private_key | BYTEA       | 32 bytes — raw secp256k1     |
| created_at  | TIMESTAMPTZ | Auto-set on insert           |
| status      | SMALLINT    | 0=unused 1=used 2=reserved   |
| metadata    | JSONB       | Flexible extra data          |

Indexes: `address`, `status`, `created_at`

### wallet_events

| Column     | Type        | Notes                        |
|------------|-------------|------------------------------|
| id         | BIGSERIAL   | Primary key                  |
| wallet_id  | BIGINT      | FK → wallets(id)             |
| event_type | VARCHAR(64) | e.g. `wallet_created`        |
| event_data | JSONB       | Flexible structured payload  |
| created_at | TIMESTAMPTZ | Auto-set on insert           |

Indexes: `wallet_id`, `event_type`, `created_at`

---

## Environment Variables

| Variable    | Default     | Description                           |
|-------------|-------------|---------------------------------------|
| DB_HOST     | localhost   | PostgreSQL hostname                   |
| DB_PORT     | 5432        | PostgreSQL port                       |
| DB_USER     | postgres    | Database user                         |
| DB_PASSWORD | (empty)     | Database password                     |
| DB_NAME     | walletdb    | Database name                         |
| DB_SSLMODE  | disable     | `disable` / `require` / `verify-full` |
| WORKERS     | 16          | Parallel key-generation goroutines    |
| BATCH_SIZE  | 500         | Wallets per DB insert transaction     |
| LOG_LEVEL   | info        | `info` / `warn` / `error`            |

---

## Performance Notes

- Wallet generation uses **Go goroutines** — scales linearly with CPU cores.
- DB inserts use **multi-row INSERT … RETURNING id** — ~10× faster than individual inserts.
- Event logging runs in background goroutines — never blocks the generation pipeline.
- Connection pool tuned for 30 max open connections.
- PostgreSQL `shared_buffers` and `wal_buffers` tuned by `install.sh`.

### Expected throughput (Azure Standard B2s — 2 vCPU / 4 GB)

| Wallets   | Approx. time |
|-----------|-------------|
| 1,000     | ~0.5s        |
| 10,000    | ~2–3s        |
| 100,000   | ~20–30s      |
| 1,000,000 | ~4–6 min     |

---

## Security Notes

- Private keys are **never printed to logs or terminal**.
- Private keys are stored as raw 32-byte `BYTEA` in PostgreSQL.
- The PostgreSQL user created by `install.sh` has access **only to `walletdb`**.
- Port 5432 is **not opened in UFW** — PostgreSQL accepts local connections only.
- For production: enable PostgreSQL SSL (`DB_SSLMODE=require`) and restrict pg_hba.conf.

---

## Extending the Event System

Add new event types in `events/events.go`:

```go
const (
    FaucetClaim      EventType = "faucet_claim"
    InternalTransfer EventType = "internal_transfer"
    BalanceUpdated   EventType = "balance_updated"
)
```

Log a single event:

```go
events.Log(db, walletID, events.FaucetClaim, map[string]interface{}{
    "tx_hash": "0xabc...",
    "amount":  "0.01",
    "chain":   "polygon",
})
```

Log a batch event (high-performance):

```go
events.LogBatch(db, walletIDs, events.BalanceUpdated, map[string]interface{}{
    "source": "scanner",
})
```

---

## Credits

Key derivation logic inspired by [Planxnx/ethereum-wallet-generator](https://github.com/Planxnx/ethereum-wallet-generator).  
Cryptography provided by [go-ethereum](https://github.com/ethereum/go-ethereum).
