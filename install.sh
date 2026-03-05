#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
#  install.sh  —  EVM Wallet Bot  —  Azure Ubuntu 24.04 LTS deployment
# ─────────────────────────────────────────────────────────────────────────────
#  Run as a non-root user with sudo privileges.
#  Usage:  chmod +x install.sh && ./install.sh
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

# BUG FIX #8 — use the directory where install.sh lives, not a hardcoded path.
# Previously the script assumed the project was always at $HOME/ethereum-wallet-bot,
# which silently failed if it was unpacked anywhere else.
APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GO_VERSION="1.22.4"
DB_NAME="walletdb"
DB_USER="walletuser"
SERVICE_NAME="evmwalletbot"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()    { echo -e "${CYAN}[INFO]${NC}  $*"; }
success() { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error()   { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

echo ""
echo "  ╔══════════════════════════════════════════════╗"
echo "  ║   EVM Wallet Bot  —  Installation Script     ║"
echo "  ║   Azure Ubuntu 24.04 LTS                     ║"
echo "  ╚══════════════════════════════════════════════╝"
echo ""

# ── 1. System update ──────────────────────────────────────────────────────────
info "Updating system packages..."
sudo apt-get update -qq
sudo apt-get upgrade -y -qq
sudo apt-get install -y -qq curl wget git build-essential ca-certificates gnupg lsb-release
success "System packages updated"

# ── 2. Install Go ─────────────────────────────────────────────────────────────
if command -v go &>/dev/null; then
    INSTALLED_GO=$(go version | awk '{print $3}' | sed 's/go//')
    info "Go already installed: v${INSTALLED_GO}"
else
    info "Installing Go ${GO_VERSION}..."
    cd /tmp
    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
    rm "go${GO_VERSION}.linux-amd64.tar.gz"

    # Add to PATH permanently
    if ! grep -q '/usr/local/go/bin' "$HOME/.profile"; then
        echo 'export PATH=$PATH:/usr/local/go/bin' >> "$HOME/.profile"
        echo 'export GOPATH=$HOME/go' >> "$HOME/.profile"
        echo 'export PATH=$PATH:$GOPATH/bin' >> "$HOME/.profile"
    fi
    export PATH=$PATH:/usr/local/go/bin
    success "Go ${GO_VERSION} installed"
fi

# ── 3. Install PostgreSQL 16 ──────────────────────────────────────────────────
if ! command -v psql &>/dev/null; then
    info "Installing PostgreSQL 16..."
    curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
        | sudo gpg --dearmor -o /etc/apt/trusted.gpg.d/postgresql.gpg
    echo "deb [signed-by=/etc/apt/trusted.gpg.d/postgresql.gpg] \
        https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" \
        | sudo tee /etc/apt/sources.list.d/pgdg.list > /dev/null
    sudo apt-get update -qq
    sudo apt-get install -y -qq postgresql-16
    sudo systemctl enable --now postgresql
    success "PostgreSQL 16 installed and started"
else
    info "PostgreSQL already installed"
fi

# ── 4. Create database & user ─────────────────────────────────────────────────
info "Setting up PostgreSQL database and user..."

# BUG FIX #1 — only generate a new password on first-time install.
# If .env already exists we read the password from it instead of generating
# a new one.  Re-generating a password on every run would:
#   (a) change the DB role password,  (b) overwrite .env, and
#   (c) make the existing database inaccessible with the old credentials.
if [[ -f "${APP_DIR}/.env" ]]; then
    warn ".env already exists — preserving existing credentials."
    # Source the existing .env to extract DB_PASSWORD for psql operations below.
    DB_PASS=$(grep '^DB_PASSWORD=' "${APP_DIR}/.env" | cut -d= -f2-)
    SKIP_ENV_WRITE=true
else
    DB_PASS=$(openssl rand -base64 24 | tr -dc 'A-Za-z0-9' | head -c 32)
    SKIP_ENV_WRITE=false
fi

sudo -u postgres psql -c "
DO \$\$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = '${DB_USER}') THEN
        -- First install: create the role with the generated password.
        CREATE ROLE ${DB_USER} LOGIN PASSWORD '${DB_PASS}';
    END IF;
    -- Do NOT alter an existing role's password on reinstall.
    -- That would break existing connections that use the old .env credentials.
END
\$\$;
" 2>/dev/null

sudo -u postgres psql -c "
CREATE DATABASE ${DB_NAME} OWNER ${DB_USER};
" 2>/dev/null || warn "Database may already exist — skipping creation"

sudo -u postgres psql -c "GRANT ALL PRIVILEGES ON DATABASE ${DB_NAME} TO ${DB_USER};" 2>/dev/null
success "Database '${DB_NAME}' and user '${DB_USER}' configured"

# ── 5. Write .env file (first install only) ───────────────────────────────────
if [[ "${SKIP_ENV_WRITE}" == "false" ]]; then
    info "Writing .env configuration (first install)..."
    cd "${APP_DIR}"
    cat > .env <<ENV
DB_HOST=localhost
DB_PORT=5432
DB_USER=${DB_USER}
DB_PASSWORD=${DB_PASS}
DB_NAME=${DB_NAME}
DB_SSLMODE=disable
WORKERS=16
BATCH_SIZE=500
LOG_LEVEL=info
ENV
    success ".env written"
else
    info ".env preserved — skipping write."
fi

# ── 6. Build the binary ───────────────────────────────────────────────────────
info "Downloading Go dependencies and building..."
cd "${APP_DIR}"
export PATH=$PATH:/usr/local/go/bin
go mod tidy
go build -ldflags="-s -w" -o evmwalletbot ./cmd
success "Binary built: ${APP_DIR}/evmwalletbot"

# ── 7. Systemd service (optional background mode) ─────────────────────────────
# NOTE: The bot is interactive (requires a TTY).  The systemd unit below is
# provided for reference only — use tmux/screen to keep it running.
cat > /tmp/${SERVICE_NAME}.service <<UNIT
[Unit]
Description=EVM Wallet Bot
After=network.target postgresql.service

[Service]
Type=simple
User=${USER}
WorkingDirectory=${APP_DIR}
ExecStart=${APP_DIR}/evmwalletbot
Restart=on-failure
StandardInput=tty
TTYPath=/dev/tty1

[Install]
WantedBy=multi-user.target
UNIT

# ── 8. Firewall: block external PostgreSQL access ─────────────────────────────
if command -v ufw &>/dev/null; then
    info "Configuring UFW firewall..."
    sudo ufw allow OpenSSH
    sudo ufw --force enable
    # PostgreSQL (5432) is NOT opened externally — keep it local only
    success "Firewall configured (SSH open, PostgreSQL local-only)"
fi

# ── 9. Performance tuning for PostgreSQL on Azure ─────────────────────────────
info "Applying PostgreSQL performance settings..."
PG_CONF=$(sudo -u postgres psql -t -c "SHOW config_file;" 2>/dev/null | tr -d ' ')
if [[ -n "$PG_CONF" ]]; then
    sudo tee -a "${PG_CONF}" > /dev/null <<PGCONF

# EVM Wallet Bot tuning — appended by install.sh
max_connections = 100
shared_buffers = 256MB
effective_cache_size = 768MB
maintenance_work_mem = 64MB
checkpoint_completion_target = 0.9
wal_buffers = 16MB
default_statistics_target = 100
random_page_cost = 1.1
effective_io_concurrency = 200
work_mem = 4MB
min_wal_size = 1GB
max_wal_size = 4GB
PGCONF
    sudo systemctl restart postgresql
    success "PostgreSQL tuned for batch workloads"
fi

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo "  ╔══════════════════════════════════════════════════════════╗"
echo "  ║   Installation complete!                                 ║"
echo "  ╠══════════════════════════════════════════════════════════╣"
echo "  ║   Binary  : ${APP_DIR}/evmwalletbot"
echo "  ║   Config  : ${APP_DIR}/.env"
echo "  ║   DB user : ${DB_USER}  /  DB name : ${DB_NAME}"
echo "  ║                                                          ║"
echo "  ║   Run with:  cd ${APP_DIR} && ./evmwalletbot             ║"
echo "  ║   Or keep session alive:  tmux new -s wallet             ║"
echo "  ╚══════════════════════════════════════════════════════════╝"
echo ""
