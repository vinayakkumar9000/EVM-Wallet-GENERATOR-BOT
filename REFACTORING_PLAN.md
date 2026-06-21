# Repository Refactoring Plan: Ultra-Compact Structure

## Current Structure Analysis

### Current File Count: ~25+ files
```
D:\bob\evm wallet generator\
├── main.go                    (entry point)
├── go.mod, go.sum            (dependencies)
├── README.md, LICENSE        (docs)
├── .gitignore                (git config)
├── vanity.db                 (runtime data)
├── internal/                 (10 Go files)
│   ├── config.go            (configuration)
│   ├── database.go          (DB interface)
│   ├── generator.go         (wallet generation engine)
│   ├── menu.go              (CLI menu system)
│   ├── postgres.go          (PostgreSQL backend)
│   ├── sqlite.go            (SQLite backend)
│   ├── storage.go           (storage interface)
│   ├── ui.go                (terminal UI/colors)
│   ├── verify.go            (wallet verification)
│   └── wallet.go            (wallet operations)
├── .bob/                     (Bob Shell files - DELETE)
└── .git/                     (Git history - DELETE)
```

---

## Target Structure: 7 Core Files

```
/
├── main.go                   (single entry point - KEEP)
├── go.mod                    (dependencies - KEEP)
├── go.sum                    (checksums - KEEP)
├── README.md                 (minimal docs - REWRITE)
├── LICENSE                   (legal - KEEP)
└── data/
    ├── wallets.db           (SQLite data - runtime created)
    ├── vanity.db            (vanity data - runtime created)
    └── src/
        ├── core.go          (MERGE: wallet.go + generator.go + verify.go)
        ├── storage.go       (MERGE: storage.go + database.go + sqlite.go + postgres.go)
        ├── config.go        (KEEP: config.go)
        └── ui.go            (MERGE: ui.go + menu.go)
```

**File Reduction: 25+ files → 7 core files (72% reduction)**

---

## Detailed Consolidation Plan

### Phase 1: File Merging Strategy

#### 1. **data/src/core.go** (Merge 3 files)
**Source files:**
- `internal/wallet.go` (wallet generation, HD wallets, vanity, export)
- `internal/generator.go` (parallel generation engine, benchmarks)
- `internal/verify.go` (wallet verification)

**Rationale:** All core wallet operations in one file
- Wallet generation (random + HD)
- Vanity address matching
- Export functionality
- Verification logic
- Benchmark utilities

**Size estimate:** ~2,500 lines

---

#### 2. **data/src/storage.go** (Merge 4 files)
**Source files:**
- `internal/storage.go` (interface definition)
- `internal/database.go` (common DB operations)
- `internal/sqlite.go` (SQLite implementation)
- `internal/postgres.go` (PostgreSQL implementation)

**Rationale:** All storage backends in one file
- Storage interface
- SQLite implementation (default)
- PostgreSQL implementation (optional)
- Health monitoring
- Connection pooling

**Size estimate:** ~1,800 lines

---

#### 3. **data/src/config.go** (Keep as-is)
**Source file:**
- `internal/config.go`

**Rationale:** Configuration is self-contained
- Environment variable loading
- Validation logic
- No dependencies on other internal files

**Size estimate:** ~250 lines

---

#### 4. **data/src/ui.go** (Merge 2 files)
**Source files:**
- `internal/ui.go` (terminal UI, colors, progress)
- `internal/menu.go` (interactive CLI menu)

**Rationale:** All UI/UX in one file
- Color/ANSI handling
- Progress tracking
- Menu system
- User interaction

**Size estimate:** ~1,500 lines

---

### Phase 2: Import Path Updates

#### Current imports:
```go
import "evmwalletbot/internal"
```

#### New imports:
```go
import "evmwalletbot/data/src"
```

**Files requiring updates:**
- `main.go` (change all `internal.` to `src.`)

---

### Phase 3: File Actions

#### KEEP (5 files)
```
✓ main.go              - Entry point
✓ go.mod               - Dependencies
✓ go.sum               - Checksums
✓ LICENSE              - Legal
✓ README.md            - Rewrite to minimal version
```

#### MERGE → data/src/ (4 consolidated files)
```
→ data/src/core.go     - wallet.go + generator.go + verify.go
→ data/src/storage.go  - storage.go + database.go + sqlite.go + postgres.go
→ data/src/config.go   - config.go (moved as-is)
→ data/src/ui.go       - ui.go + menu.go
```

#### DELETE (development/tooling files)
```
✗ .bob/                - Bob Shell internal files
✗ .git/                - Git history (optional, user decides)
✗ .gitignore           - Not needed for production utility
✗ vanity.db            - Runtime-created, not source
```

#### RUNTIME-CREATED (not in source)
```
○ data/wallets.db      - Created on first run
○ data/vanity.db       - Created when using vanity feature
```

---

## Migration Steps

### Step 1: Create new directory structure
```bash
mkdir -p data/src
```

### Step 2: Merge files into data/src/

**core.go:**
```bash
# Merge wallet operations
cat internal/wallet.go internal/generator.go internal/verify.go > data/src/core.go
# Update package declaration: package internal → package src
sed -i 's/package internal/package src/g' data/src/core.go
```

**storage.go:**
```bash
# Merge storage backends
cat internal/storage.go internal/database.go internal/sqlite.go internal/postgres.go > data/src/storage.go
sed -i 's/package internal/package src/g' data/src/storage.go
```

**config.go:**
```bash
# Move config as-is
cp internal/config.go data/src/config.go
sed -i 's/package internal/package src/g' data/src/config.go
```

**ui.go:**
```bash
# Merge UI components
cat internal/ui.go internal/menu.go > data/src/ui.go
sed -i 's/package internal/package src/g' data/src/ui.go
```

### Step 3: Update main.go imports
```bash
# Change import path
sed -i 's|evmwalletbot/internal|evmwalletbot/data/src|g' main.go
# Change all internal. references to src.
sed -i 's/internal\./src./g' main.go
```

### Step 4: Update go.mod module path (optional)
```go
// If renaming module:
module evmwalletbot  // Keep as-is for simplicity
```

### Step 5: Remove old files
```bash
rm -rf internal/
rm -rf .bob/
rm -f .gitignore
rm -f vanity.db  # Will be recreated in data/
```

### Step 6: Test build
```bash
go build -o evmwalletbot.exe
```

---

## New Minimal README.md

```markdown
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
```

---

## Final Directory Tree

```
/
├── main.go                   # Application entry point
├── go.mod                    # Go module definition
├── go.sum                    # Dependency checksums
├── README.md                 # Minimal documentation
├── LICENSE                   # License file
└── data/
    ├── wallets.db           # SQLite database (created at runtime)
    ├── vanity.db            # Vanity wallets (created at runtime)
    └── src/
        ├── core.go          # Wallet operations (2,500 lines)
        ├── storage.go       # Storage backends (1,800 lines)
        ├── config.go        # Configuration (250 lines)
        └── ui.go            # User interface (1,500 lines)
```

**Total source files: 9 files (5 root + 4 in data/src/)**

---

## Startup Command

### After Refactoring

```bash
# Build
go build -o evmwalletbot.exe

# Run interactive mode
./evmwalletbot.exe

# Run with flags
./evmwalletbot.exe -count 1000 -export-mode paired -export-dir ./output
```

**No changes to user-facing commands!** All functionality preserved.

---

## File Count Reduction

### Before Refactoring
- **Source files**: 13 (main.go + 10 internal/*.go + go.mod + go.sum)
- **Documentation**: 2 (README.md + LICENSE)
- **Configuration**: 1 (.gitignore)
- **Development**: 2+ (.bob/, .git/)
- **Runtime data**: 1 (vanity.db)
- **Total tracked**: ~19+ files

### After Refactoring
- **Source files**: 9 (main.go + 4 data/src/*.go + go.mod + go.sum + README.md + LICENSE)
- **Runtime data**: 2 (data/wallets.db + data/vanity.db - created at runtime)
- **Total tracked**: 9 files

### Reduction
- **Source files**: 13 → 9 (31% reduction)
- **Total complexity**: 19+ → 9 (53% reduction)
- **Directory depth**: 2 levels → 2 levels (maintained)
- **Package structure**: Simplified (internal → data/src)

---

## Benefits

### 1. **Simplicity**
- Single `data/src/` package instead of `internal/`
- 4 consolidated files instead of 10 fragmented files
- Easier to navigate and understand

### 2. **Portability**
- Fewer files to distribute
- Clear separation: code in `data/src/`, data in `data/`
- Single binary + data folder = complete application

### 3. **Maintainability**
- Related code grouped together (e.g., all storage in one file)
- Reduced import complexity
- Easier to find functionality

### 4. **Production-Ready**
- No development artifacts (.bob/, .git/)
- Minimal documentation (only essentials)
- Clean, professional structure

---

## Verification Checklist

After refactoring, verify:

- [ ] `go build` succeeds without errors
- [ ] `./evmwalletbot.exe` launches interactive menu
- [ ] Generate 100 wallets successfully
- [ ] Vanity generation works
- [ ] Export functionality works
- [ ] PostgreSQL mode works (if configured)
- [ ] All CLI flags work as documented
- [ ] Database files created in `data/` directory

---

## Rollback Plan

If issues occur:

1. **Keep backup**: `cp -r internal/ internal.backup/`
2. **Test thoroughly** before deleting `internal/`
3. **Rollback**: `rm -rf data/src/ && mv internal.backup/ internal/`

---

## Notes

### What's Preserved
✓ 100% functionality (all features work identically)
✓ All CLI commands and flags
✓ Database compatibility (SQLite + PostgreSQL)
✓ Export formats
✓ Vanity generation
✓ HD wallet support
✓ Performance characteristics

### What's Changed
- Package name: `internal` → `src`
- Import path: `evmwalletbot/internal` → `evmwalletbot/data/src`
- File organization: 10 files → 4 consolidated files
- Directory structure: `internal/` → `data/src/`
- README: Comprehensive → Minimal (essentials only)

### What's Removed
- Development files (.bob/, .git/)
- Git configuration (.gitignore)
- Runtime databases from source (moved to data/)

---

## Implementation Timeline

**Estimated time: 2-3 hours**

1. **Preparation** (30 min)
   - Backup current repository
   - Create new directory structure
   - Review merge strategy

2. **File Merging** (60 min)
   - Merge core.go (wallet + generator + verify)
   - Merge storage.go (storage + database + sqlite + postgres)
   - Move config.go
   - Merge ui.go (ui + menu)

3. **Import Updates** (30 min)
   - Update main.go imports
   - Update package declarations
   - Fix any cross-references

4. **Testing** (30 min)
   - Build verification
   - Functional testing
   - Performance validation

5. **Cleanup** (30 min)
   - Remove old files
   - Update README
   - Final verification

---

## Success Criteria

✅ Repository has ≤10 source files
✅ All code in `data/src/` directory
✅ Single entry point (`main.go`)
✅ Minimal README (installation + run + config only)
✅ 100% functionality preserved
✅ No breaking changes to user commands
✅ Clean, production-ready structure
