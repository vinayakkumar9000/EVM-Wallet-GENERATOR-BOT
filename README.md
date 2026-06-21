# EVM Wallet Generator

High-performance CLI tool for generating Ethereum-compatible wallets.

**Supported chains:** Ethereum · BSC · Polygon · Arbitrum · Optimism · Base

---

## Quick Start

### Build
```bash
go build -o evmwalletbot.exe
```

### Run
```bash
# Interactive mode (default)
./evmwalletbot.exe

# Generate 1000 wallets
./evmwalletbot.exe -count 1000

# Generate with export
./evmwalletbot.exe -count 1000 -export-mode paired -export-dir ./output
```

---

## Configuration

All settings are optional. Defaults work with embedded SQLite (zero setup).

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `STORAGE` | `sqlite` | Storage backend: `sqlite` or `postgres` |
| `WORKERS` | `16` | Parallel wallet generators |
| `BATCH_SIZE` | `500` | Wallets per database batch |
| `EXPORT_ENABLED` | `false` | Enable file export |
| `EXPORT_MODE` | `paired` | Export format: `paired`, `key-only`, `address-only`, `combined`, `json`, `keystore` |
| `EXPORT_DIR` | `./exports` | Export output directory |

### PostgreSQL (Optional)

Set `STORAGE=postgres` and configure:
- `DB_HOST` (default: `localhost`)
- `DB_PORT` (default: `5432`)
- `DB_USER` (default: `postgres`)
- `DB_PASSWORD` (required)
- `DB_NAME` (default: `walletdb`)

---

## Features

- **Zero Setup**: Embedded SQLite, single binary
- **High Performance**: 5,000-10,000 wallets/sec
- **Vanity Generation**: Custom address patterns
- **HD Wallets**: BIP-39/BIP-44 seed phrase support
- **Export**: Multiple formats (text, CSV, JSON, keystore)
- **Multi-Backend**: SQLite (default) or PostgreSQL

---

## Security

⚠️ **CRITICAL**: Private keys are stored in plaintext in the database.
- Protect `data/wallets.db` and `data/vanity.db` files
- Export files contain plaintext keys - treat as highly sensitive
- Use strong passwords for keystore exports
- Never commit `.env` files with credentials

---

## License

See LICENSE file.