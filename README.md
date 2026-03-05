# EVM Wallet Bot

High-performance CLI tool for generating and managing millions of EVM-compatible wallets.  
Built in **Go**, backed by **PostgreSQL**, optimised for **Ubuntu/Debian Linux**.

**Supported chains:** Ethereum · BSC · Polygon · Arbitrum · Optimism · Base · any EVM chain

---

## Table of Contents

1. [Architecture](#architecture)
2. [Step-by-Step Installation Guide](#step-by-step-installation-guide)
   - [Linux (Ubuntu/Debian)](#linux-ubuntudebian)
   - [VPS Deployment](#vps-deployment-azure-aws-digitalocean-etc)
   - [Local PC (Windows WSL / macOS)](#local-pc-windows-wsl--macos)
3. [Usage](#usage--interactive-menu)
4. [Configuration](#environment-variables)
5. [Database Schema](#database-schema)
6. [Performance Notes](#performance-notes)
7. [Security Notes](#security-notes)
8. [Extending the Event System](#extending-the-event-system)

---

## Architecture

```
EVM-Wallet-GENERATOR-BOT/
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
├── install.sh                           ← one-shot setup script
└── go.mod
```

---

## Step-by-Step Installation Guide

### Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | ≥ 1.22 | Compiling and running the application |
| PostgreSQL | ≥ 15 (16 recommended) | Database for storing wallets |
| Git | Any | Cloning the repository |

---

### Linux (Ubuntu/Debian)

Follow these steps to install and run the EVM Wallet Bot on a fresh Linux system.

#### Step 1: Update System Packages

```bash
sudo apt update && sudo apt upgrade -y
```

#### Step 2: Install Required Dependencies

```bash
sudo apt install -y curl wget git build-essential ca-certificates gnupg lsb-release
```

#### Step 3: Install Go (version 1.22+)

```bash
# Download Go
cd /tmp
wget https://go.dev/dl/go1.22.4.linux-amd64.tar.gz

# Extract and install
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.22.4.linux-amd64.tar.gz

# Add Go to PATH (add to ~/.bashrc for persistence)
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
echo 'export GOPATH=$HOME/go' >> ~/.bashrc
echo 'export PATH=$PATH:$GOPATH/bin' >> ~/.bashrc
source ~/.bashrc

# Verify installation
go version
```

#### Step 4: Install PostgreSQL 16

```bash
# Add PostgreSQL repository
curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo gpg --dearmor -o /etc/apt/trusted.gpg.d/postgresql.gpg
echo "deb [signed-by=/etc/apt/trusted.gpg.d/postgresql.gpg] https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" | sudo tee /etc/apt/sources.list.d/pgdg.list > /dev/null

# Install PostgreSQL
sudo apt update
sudo apt install -y postgresql-16

# Start and enable PostgreSQL
sudo systemctl enable --now postgresql

# Verify installation
psql --version
```

#### Step 5: Clone the Repository

```bash
cd ~
git clone https://github.com/vinayakkumar9000/EVM-Wallet-GENERATOR-BOT.git
cd EVM-Wallet-GENERATOR-BOT
```

#### Step 6: Configure the Database

```bash
# Generate a secure password
DB_PASS=$(openssl rand -base64 24 | tr -dc 'A-Za-z0-9' | head -c 32)
echo "Generated password: $DB_PASS"

# Create database user and database
sudo -u postgres psql <<EOF
CREATE ROLE walletuser LOGIN PASSWORD '$DB_PASS';
CREATE DATABASE walletdb OWNER walletuser;
GRANT ALL PRIVILEGES ON DATABASE walletdb TO walletuser;
EOF
```

#### Step 7: Configure Environment Variables

```bash
# Copy example config
cp .env.example .env

# Edit the .env file with your database credentials
nano .env
```

Update the `.env` file with:
```
DB_HOST=localhost
DB_PORT=5432
DB_USER=walletuser
DB_PASSWORD=YOUR_GENERATED_PASSWORD
DB_NAME=walletdb
DB_SSLMODE=disable
WORKERS=16
BATCH_SIZE=500
LOG_LEVEL=info
```

#### Step 8: Build the Application

```bash
# Download dependencies
go mod tidy

# Build the binary
go build -ldflags="-s -w" -o evmwalletbot ./cmd
```

#### Step 9: Run the Application

```bash
./evmwalletbot
```

**The program will:**
1. Connect to PostgreSQL
2. Auto-create the schema if missing
3. Show the interactive menu

---

### VPS Deployment (Azure, AWS, DigitalOcean, etc.)

For VPS deployment, you can use the automated installer script.

#### Step 1: Connect to Your VPS

```bash
ssh your_username@YOUR_VPS_IP
```

#### Step 2: Clone the Repository

```bash
cd ~
git clone https://github.com/vinayakkumar9000/EVM-Wallet-GENERATOR-BOT.git
cd EVM-Wallet-GENERATOR-BOT
```

#### Step 3: Run the Automated Installer

```bash
chmod +x install.sh
./install.sh
```

**The installer will automatically:**
- Update system packages
- Install Go 1.22
- Install and configure PostgreSQL 16
- Create database user and database
- Generate secure credentials and write to `.env`
- Apply PostgreSQL performance tuning
- Build the binary

#### Step 4: Run the Application

```bash
./evmwalletbot
```

#### Step 5: Keep Running with tmux (Recommended)

```bash
# Install tmux if not present
sudo apt install -y tmux

# Create a new tmux session
tmux new -s wallet

# Run the application
cd ~/EVM-Wallet-GENERATOR-BOT
./evmwalletbot

# Detach from tmux: Press Ctrl+B, then D
# Re-attach later: tmux attach -t wallet
```

---

### Local PC (Windows WSL / macOS)

#### Windows (using WSL2)

```bash
# 1. Install WSL2 (run in PowerShell as Administrator)
wsl --install -d Ubuntu

# 2. Open Ubuntu WSL and follow the Linux installation steps above
```

#### macOS

```bash
# 1. Install Homebrew (if not installed)
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# 2. Install Go
brew install go

# 3. Install PostgreSQL
brew install postgresql@16
brew services start postgresql@16

# 4. Clone and setup
git clone https://github.com/vinayakkumar9000/EVM-Wallet-GENERATOR-BOT.git
cd EVM-Wallet-GENERATOR-BOT
cp .env.example .env

# 5. Create database (macOS uses your username by default)
createdb walletdb

# 6. Edit .env with your credentials
nano .env

# 7. Build and run
go mod tidy
go build -ldflags="-s -w" -o evmwalletbot ./cmd
./evmwalletbot
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
