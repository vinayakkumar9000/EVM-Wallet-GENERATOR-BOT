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
PAPA9000/
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
git clone https://github.com/vinayakkumar9000/PAPA9000.git
cd PAPA9000
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
git clone https://github.com/vinayakkumar9000/PAPA9000.git
cd PAPA9000
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
cd ~/PAPA9000
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
git clone https://github.com/vinayakkumar9000/PAPA9000.git
cd PAPA9000
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

## Usage

### Non-Interactive Mode (CLI Flags)

For automation and scripting, use command-line flags to generate wallets without the interactive menu:

```bash
# Show help
./evmwalletbot --help

# Show version
./evmwalletbot --version

# Generate 1000 wallets
./evmwalletbot --count 1000

# Generate with export to CSV
./evmwalletbot --count 5000 --export-mode combined --export-dir ./output

# Generate with paired export (address.txt + privatekey.txt)
./evmwalletbot --count 10000 --export-mode paired --export-dir ./exports
```

**Available Flags:**
- `--count N` — Generate N wallets and exit (non-interactive mode)
- `--export-mode MODE` — Export mode: `paired`, `key-only`, `address-only`, `combined`
- `--export-dir PATH` — Export directory path (default: `./exports`)
- `--version` — Show version and exit
- `--help` — Show help and exit

### Interactive Menu

The EVM Wallet Manager features a comprehensive interactive menu system with organized submenus for all operations.

### Main Menu

```
  ╔═══════════════════════════════════════════════════════╗
  ║         EVM  WALLET  MANAGER   v1.0                   ║
  ║         Multi-chain  ·  PostgreSQL  ·  Go             ║
  ╚═══════════════════════════════════════════════════════╝

  ┌──────────────────────────────────────┐
  │   1   Generate wallets               │
  │   2   Statistics                     │
  │   3   Wallet lookup                  │
  │   4   Database tools                 │
  │   5   Monitoring                     │
  │   6   Benchmark / tuning             │
  │   7   Configuration                  │
  │   8   Help                           │
  │   9   Exit                           │
  └──────────────────────────────────────┘
```

### 1. Generate Wallets Submenu

Generate wallets with preview and confirmation for large runs.

```
  ┌────────────────────────────────────────────┐
  │              GENERATE WALLETS              │
  │   1   Generate by wallet count             │
  │   2   Generate by batch count              │
  │   3   Preview run settings                 │
  │   4   Generation settings                  │
  │   5   Back                                 │
  └────────────────────────────────────────────┘
```

**Features:**
- Generate exact number of wallets or by batch count
- Preview shows workers, batch size, database, and logging status
- Automatic confirmation prompt for runs >10,000 wallets
- Real-time progress display with throughput metrics

**Example:**
```
Enter number of wallets to generate: 50000

  ┌──────────────────────────────────────────────────────┐
  │                  RUN PREVIEW                         │
  ├──────────────────────────────────────────────────────┤
  │  Wallets        : 50000                              │
  │  Mode           : 100 batches × 500 wallets          │
  │  Workers        : 16                                 │
  │  Batch size     : 500                                │
  │  Insert batches : 100                                │
  │  Database       : walletdb                           │
  │  Logging        : enabled                            │
  └──────────────────────────────────────────────────────┘

  ⚠️  Large generation run: 50000 wallets
  Continue? [y/N]: y

[INFO] Starting wallet generation
[INFO] Generating 50000 wallets
[INFO] Generation finished — all 50000 wallets stored successfully.
```

### 2. Statistics Submenu

View wallet statistics with live watch mode and database size information.

```
  ┌────────────────────────────────────────────┐
  │                STATISTICS                  │
  │   1   Show current stats                   │
  │   2   Watch stats live                     │
  │   3   Database size                        │
  │   4   Back                                 │
  └────────────────────────────────────────────┘
```

**Features:**
- Current statistics snapshot
- Live watch mode (auto-refresh every 5 seconds)
- Database size breakdown (total, tables, indexes)
- Press Enter to stop live watch

### 3. Wallet Lookup Submenu

Look up wallet details by ID or address.

```
  ┌────────────────────────────────────────────┐
  │              WALLET LOOKUP                 │
  │   1   Lookup by wallet ID                  │
  │   2   Lookup by address                    │
  │   3   Back                                 │
  └────────────────────────────────────────────┘
```

### 4. Database Tools Submenu

Comprehensive database management and monitoring.

```
  ┌────────────────────────────────────────────┐
  │              DATABASE TOOLS                │
  │   1   Health check                         │
  │   2   Connection pool status               │
  │   3   Record health snapshot               │
  │   4   Maintenance recommendations          │
  │   5   Back                                 │
  └────────────────────────────────────────────┘
```

**Features:**
- Database connectivity and health checks
- Real-time connection pool statistics
- Save health snapshots to timestamped files
- Automated maintenance recommendations based on database size

**Example - Pool Status:**
```
  ┌──────────────────────────────────────────────────────┐
  │            CONNECTION POOL STATUS                    │
  ├──────────────────────────────────────────────────────┤
  │  Total connections    : 8                            │
  │  Idle connections     : 5                            │
  │  Acquired connections : 3                            │
  │  Max connections      : 30                           │
  │  Usage                : 10.0%                        │
  └──────────────────────────────────────────────────────┘
```

### 5. Monitoring Submenu

Real-time monitoring with live watch modes.

```
  ┌────────────────────────────────────────────┐
  │               MONITORING                   │
  │   1   Pool status (once)                   │
  │   2   Watch pool status (live)             │
  │   3   Watch wallet stats (live)            │
  │   4   Set refresh interval (5s)            │
  │   5   Back                                 │
  └────────────────────────────────────────────┘
```

**Features:**
- One-time pool status snapshot
- Live watch modes with configurable refresh interval (1-60 seconds)
- Auto-refresh with screen clearing for clean display
- Press Enter to stop watching

### 6. Benchmark / Tuning Submenu

Performance testing and optimization tools.

```
  ┌────────────────────────────────────────────┐
  │           BENCHMARK / TUNING               │
  │   1   Estimate current settings            │
  │   2   Run small benchmark (1000 wallets)   │
  │   3   Compare worker counts                │
  │   4   Compare batch sizes                  │
  │   5   Back                                 │
  └────────────────────────────────────────────┘
```

**Features:**
- Estimate completion times for different run sizes
- Run actual benchmarks to measure system performance
- Compare different worker counts (4, 8, 16, 32)
- Compare different batch sizes (100, 250, 500, 1000)
- Get recommendations based on results

**Example - Estimation:**
```
  ┌──────────────────────────────────────────────────────┐
  │           PERFORMANCE ESTIMATION                     │
  ├──────────────────────────────────────────────────────┤
  │  Current settings:                                   │
  │    Workers     : 16                                  │
  │    Batch size  : 500                                 │
  │    Estimated   : ~10000 wallets/second               │
  └──────────────────────────────────────────────────────┘

  Estimated completion times:
    Small run (10,000)       : 1.0 seconds
    Medium run (100,000)     : 10.0 seconds
    Large run (1,000,000)    : 1.7 minutes
    Very large run (10,000,000) : 16.7 minutes
```

### 7. Configuration Submenu

Runtime configuration without editing .env file.

```
  ┌──────────────────────────────────────┐
  │             CONFIGURATION            │
  ├──────────────────────────────────────┤
  │  Database       : walletdb           │
  │  User           : postgres           │
  │  Host           : localhost          │
  │  Port           : 5432               │
  │  Max conns      : 30                 │
  │  Min conns      : 5                  │
  │  Workers        : 16                 │
  │  Batch size     : 500 wallets        │
  │  Logging        : enabled            │
  │  Pool monitor   : 30 s               │
  │  Pool threshold : 0.80               │
  ├──────────────────────────────────────┤
  │   1   Show current settings          │
  │   2   Workers                        │
  │   3   Batch size                     │
  │   4   Logging (enable/disable)       │
  │   5   Pool monitor interval          │
  │   6   Pool warning threshold         │
  │   7   Reset session settings         │
  │   8   Back                           │
  └──────────────────────────────────────┘
```

**Features:**
- Modify all runtime settings without restarting
- Changes apply to current session only
- Reset to .env defaults anytime
- Input validation with helpful error messages
- Recommended values shown for each setting

**Configurable Settings:**
- **Workers (1-100):** Parallel wallet generators, default 16
- **Batch Size (1-1000):** Wallets per transaction, default 500
- **Logging:** Enable/disable batch completion logs
- **Pool Monitor Interval (0-300s):** Pool stats frequency, 0 to disable
- **Pool Warning Threshold (0.1-1.0):** Connection usage alert level

### 8. Help Submenu

Comprehensive help system with topic-specific guides.

```
  ┌────────────────────────────────────────────┐
  │                  HELP                      │
  │   1   Generation modes                     │
  │   2   Batch size guide                     │
  │   3   Workers guide                        │
  │   4   Database guide                       │
  │   5   Settings guide                       │
  │   6   Back                                 │
  └────────────────────────────────────────────┘
```

**Features:**
- Detailed explanations for each topic
- Best practices and recommendations
- Trade-offs and optimization tips
- Examples and use cases
- Press Enter to return to menu

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

### Core Configuration

| Variable    | Default     | Description                           |
|-------------|-------------|---------------------------------------|
| DB_HOST     | localhost   | PostgreSQL hostname                   |
| DB_PORT     | 5432        | PostgreSQL port                       |
| DB_USER     | postgres    | Database user                         |
| DB_PASSWORD | (empty)     | Database password                     |
| DB_NAME     | walletdb    | Database name                         |
| DB_SSLMODE  | disable     | `disable` / `require` / `verify-full` |
| DB_MAX_CONNS | 30         | Maximum connection pool size          |
| DB_MIN_CONNS | 5          | Minimum idle connections              |
| WORKERS     | 16          | Parallel key-generation goroutines    |
| BATCH_SIZE  | 500         | Wallets per DB insert transaction     |
| LOG_LEVEL   | info        | `info` / `warn` / `error`            |
| ENABLE_LOGGING | true     | Enable batch completion logs          |

### Export Configuration

| Variable              | Default  | Description                                    |
|-----------------------|----------|------------------------------------------------|
| EXPORT_ENABLED        | false    | Enable plaintext file export                   |
| EXPORT_MODE           | paired   | Export mode: `paired`, `key-only`, `address-only`, `combined` |
| EXPORT_DIR            | ./exports | Output directory for export files             |
| EXPORT_OVERWRITE      | false    | Overwrite existing files (false = append mode) |
| EXPORT_ADDRESS_PREFIX | true     | Add 0x prefix to addresses                     |
| EXPORT_KEY_PREFIX     | true     | Add 0x prefix to private keys                  |
| EXPORT_USE_CHECKSUM   | true     | Use EIP-55 checksum for addresses              |

**Export Modes:**
- **paired**: Separate `address.txt` and `privatekey.txt` files with line-for-line correspondence
- **key-only**: Only `privatekey.txt` (for importing to wallets)
- **address-only**: Only `address.txt` (for monitoring/airdrops)
- **combined**: Single `wallets.csv` with both columns (for spreadsheets)

**Example Export Configuration:**
```bash
# Enable export with paired mode
EXPORT_ENABLED=true
EXPORT_MODE=paired
EXPORT_DIR=./my_wallets
EXPORT_OVERWRITE=false
EXPORT_USE_CHECKSUM=true
```

### Monitoring Configuration

| Variable              | Default | Description                                    |
|-----------------------|---------|------------------------------------------------|
| POOL_MONITOR_INTERVAL | 30      | Pool stats log interval in seconds (0 = disable) |
| POOL_WARNING_THRESHOLD | 0.8    | Warn when pool usage exceeds this ratio (0.0-1.0) |
| UI_MODE               | full    | Display mode: `full` or `minimal`              |
| SHOW_FIRST_RUN_TIPS   | true    | Show tips on first run                         |

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

- Private keys are **never printed to logs or terminal** (except during export operations).
- Private keys are stored as raw 32-byte `BYTEA` in PostgreSQL.
- The PostgreSQL user created by `install.sh` has access **only to `walletdb`**.
- Port 5432 is **not opened in UFW** — PostgreSQL accepts local connections only.
- For production: enable PostgreSQL SSL (`DB_SSLMODE=require`) and restrict pg_hba.conf.

### Export Security

When using the export feature:
- **Exported files contain plaintext private keys** — treat them as highly sensitive
- Store export files in encrypted volumes or secure locations
- Use `EXPORT_OVERWRITE=false` (append mode) to avoid accidental data loss
- Delete export files immediately after importing to destination wallets
- Never commit export files to version control (already in `.gitignore`)
- Consider using `address-only` mode when private keys are not needed
- Verify exported wallets using the built-in `VerifyExportedWallet` function

**Recommended Export Workflow:**
1. Generate wallets to database (secure storage)
2. Export only when needed for specific operations
3. Import to destination immediately
4. Securely delete export files
5. Verify import success before deleting database records

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
