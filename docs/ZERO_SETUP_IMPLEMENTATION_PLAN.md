# Zero-Setup Refactor Implementation Plan

## Executive Summary

Transform the EVM wallet generator from a PostgreSQL-dependent application into a true single-file, zero-setup tool that works out-of-the-box on any machine. The default backend will be embedded SQLite (pure Go, no CGo), with PostgreSQL as an optional opt-in feature.

**Current State:** Application requires external PostgreSQL server and exits on startup if unavailable.

**Target State:** Single executable that launches immediately with embedded SQLite, requiring zero external dependencies or configuration.

---

## 1. Current State Analysis

### 1.1 PostgreSQL Dependencies

**cmd/main.go (lines 115-125):**
```go
// BLOCKING: Exits if Postgres unavailable
database.EnsureDatabase(ctx, cfg)  // Line ~115
pool, err := database.Connect(ctx, cfg)  // Line ~125
if err != nil {
    os.Exit(1)  // FATAL EXIT
}
```

**Direct pgxpool.Pool Usage:**
- `cli.Run(ctx, pool, cfg)` - CLI menu system
- `core.GenerateWallets(ctx, pool, cfg, count)` - Wallet generation
- `core.GetStats(ctx, pool)` - Statistics queries
- All menu handlers in `cli/menu.go`

**Pool Monitoring (lines 135-165):**
```go
go func() {
    ticker := time.NewTicker(...)
    stats := pool.Stat()  // PostgreSQL-specific
    // Monitoring goroutine
}()
```

### 1.2 Storage Interface Status

**GOOD NEWS:** `storage/interface.go` already exists with complete abstraction:
- `Storage` interface with all required methods
- `storage/sqlite/sqlite.go` - Embedded SQLite implementation (pure Go)
- `storage/postgres/postgres.go` - PostgreSQL implementation

**Current Issue:** The interface exists but is NOT used. All code still directly uses `*pgxpool.Pool`.

---

## 2. Implementation Strategy

### 2.1 Storage Factory Pattern

**Create:** `storage/factory.go`

```go
package storage

import (
    "context"
    "fmt"
    "log"
    
    "evmwalletbot/config"
    "evmwalletbot/storage/postgres"
    "evmwalletbot/storage/sqlite"
)

// NewStorage creates a storage backend based on configuration.
// Returns embedded SQLite by default, PostgreSQL only if explicitly enabled.
func NewStorage(ctx context.Context, cfg *config.Config) (Storage, error) {
    switch cfg.StorageType {
    case "postgres":
        log.Println("[INFO] Using PostgreSQL storage (opt-in)")
        store, err := postgres.NewPostgresStorage(ctx, cfg)
        if err != nil {
            log.Printf("[WARN] PostgreSQL unavailable: %v", err)
            log.Println("[INFO] Falling back to embedded SQLite storage")
            return newSQLiteFallback(cfg)
        }
        return store, nil
        
    case "sqlite", "":
        log.Println("[INFO] Using embedded SQLite storage (zero-setup)")
        return sqlite.NewSQLiteStorage(cfg.DataDir)
        
    default:
        return nil, fmt.Errorf("unknown storage type: %s", cfg.StorageType)
    }
}

func newSQLiteFallback(cfg *config.Config) (Storage, error) {
    return sqlite.NewSQLiteStorage(cfg.DataDir)
}
```

### 2.2 Configuration Updates

**config/config.go - Already has:**
- `StorageType string` (default: "sqlite")
- `DataDir string` (auto-determined if empty)

**Add CLI Flags in cmd/main.go:**
```go
var (
    storageType = flag.String("storage", "", "Storage backend: sqlite (default) or postgres")
    dataDir     = flag.String("data-dir", "", "Data directory for SQLite (auto-determined if empty)")
    // ... existing flags
)

// Override config with flags
if *storageType != "" {
    cfg.StorageType = *storageType
}
if *dataDir != "" {
    cfg.DataDir = *dataDir
}
```

---

## 3. File-by-File Modification Plan

### 3.1 cmd/main.go - Complete Refactor

**BEFORE (lines 115-165):**
```go
// Unconditional Postgres connection
database.EnsureDatabase(ctx, cfg)
pool, err := database.Connect(ctx, cfg)
if err != nil {
    os.Exit(1)  // FATAL
}
defer pool.Close()

// Pool monitoring goroutine
go func() {
    stats := pool.Stat()  // PostgreSQL-specific
    // ...
}()

database.Migrate(pool)
s, err := core.GetStats(ctx, pool)
core.GenerateWallets(ctx, pool, cfg, *generateCount)
cli.Run(ctx, pool, cfg)
```

**AFTER:**
```go
// Create storage backend (never exits, falls back to SQLite)
store, err := storage.NewStorage(ctx, cfg)
if err != nil {
    fmt.Fprintf(os.Stderr, "[ERROR] Storage initialization failed: %v\n", err)
    os.Exit(1)
}
defer store.Close()

// Run schema migrations
log.Println("[INFO] Verifying storage schema...")
if err := store.Migrate(ctx); err != nil {
    fmt.Fprintf(os.Stderr, "[ERROR] Schema migration failed: %v\n", err)
    os.Exit(1)
}
log.Println("[INFO] Schema ready")

// Pool monitoring (only for PostgreSQL backend)
if store.StorageType() == "postgres" && cfg.PoolMonitorInterval > 0 {
    go monitorConnectionPool(ctx, store, cfg)
}

// Auto-display stats
s, err := store.GetStats(ctx)
if err == nil && s.TotalWallets > 0 {
    log.Printf("[INFO] Existing database found — %d wallets loaded\n", s.TotalWallets)
    core.PrintStats(s)
}

// Non-interactive mode
if *generateCount > 0 {
    log.Printf("[INFO] Non-interactive mode: generating %d wallets", *generateCount)
    if err := core.GenerateWallets(ctx, store, cfg, *generateCount); err != nil {
        fmt.Fprintf(os.Stderr, "[ERROR] Generation failed: %v\n", err)
        os.Exit(1)
    }
    log.Println("[INFO] Generation complete, exiting")
    os.Exit(0)
}

// Launch interactive CLI
cli.Run(ctx, store, cfg)
```

**Add Helper Function:**
```go
func monitorConnectionPool(ctx context.Context, store storage.Storage, cfg *config.Config) {
    ticker := time.NewTicker(time.Duration(cfg.PoolMonitorInterval) * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            stats := store.GetPoolStats()
            if stats == nil {
                return // No pool stats (SQLite)
            }
            
            if cfg.EnableLogging {
                log.Printf("[POOL] Connections: Total=%d Idle=%d Acquired=%d",
                    stats.TotalConns, stats.IdleConns, stats.AcquiredConns)
            }
            
            if stats.Usage() > cfg.PoolWarningThreshold {
                log.Printf("[WARN] Connection pool usage high: %d/%d (%.0f%%)",
                    stats.AcquiredConns, stats.MaxConns, stats.Usage()*100)
            }
        case <-ctx.Done():
            return
        }
    }
}
```

### 3.2 cli/menu.go - Interface Conversion

**Change Function Signatures:**
```go
// BEFORE
func Run(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config)
func handleGenerateMenu(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, reader *bufio.Reader)
func handleStatsMenu(ctx context.Context, pool *pgxpool.Pool, reader *bufio.Reader)
// ... all handlers

// AFTER
func Run(ctx context.Context, store storage.Storage, cfg *config.Config)
func handleGenerateMenu(ctx context.Context, store storage.Storage, cfg *config.Config, reader *bufio.Reader)
func handleStatsMenu(ctx context.Context, store storage.Storage, reader *bufio.Reader)
// ... all handlers
```

**Update Function Bodies:**
```go
// BEFORE
func handleStats(ctx context.Context, pool *pgxpool.Pool) {
    s, err := core.GetStats(ctx, pool)
    // ...
}

// AFTER
func handleStats(ctx context.Context, store storage.Storage) {
    s, err := store.GetStats(ctx)
    // ...
}
```

**Pool-Specific Functions (conditional):**
```go
func showPoolStatus(store storage.Storage) {
    stats := store.GetPoolStats()
    if stats == nil {
        fmt.Println(core.Info("\n[INFO] Connection pooling not available for this storage backend"))
        return
    }
    
    // Display pool stats
    fmt.Printf("  Total connections: %d\n", stats.TotalConns)
    // ...
}
```

### 3.3 core/generator.go - Interface Conversion

**Change Function Signature:**
```go
// BEFORE
func GenerateWallets(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, totalWallets int) error

// AFTER
func GenerateWallets(ctx context.Context, store storage.Storage, cfg *config.Config, totalWallets int) error
```

**Update Storage Calls:**
```go
// BEFORE
ids, err := insertWalletBatchCopy(ctx, pool, batch)

// AFTER
ids, err := store.SaveWallets(ctx, batch)
```

### 3.4 core/stats.go - Interface Conversion

**Change Function Signature:**
```go
// BEFORE
func GetStats(ctx context.Context, pool *pgxpool.Pool) (*Stats, error)

// AFTER - REMOVE THIS FUNCTION
// Use store.GetStats(ctx) directly, which returns storage.Stats
```

**Update PrintStats:**
```go
// BEFORE
func PrintStats(s *Stats)

// AFTER
func PrintStats(s *storage.Stats) {
    // Update field names to match storage.Stats
    fmt.Printf("Total wallets: %d\n", s.TotalWallets)
    fmt.Printf("Unused: %d\n", s.UnusedWallets)
    // ...
}
```

### 3.5 core/vanity.go - Interface Conversion

**Change Function Signature:**
```go
// BEFORE
func GenerateVanityWallets(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, vanityConfig VanityConfig) error

// AFTER
func GenerateVanityWallets(ctx context.Context, store storage.Storage, cfg *config.Config, vanityConfig VanityConfig) error
```

### 3.6 storage/sqlite/sqlite.go - Enhancements

**Current Issues:**
1. Missing proper error handling for data directory creation
2. No logging of database file location
3. Stats query doesn't match PostgreSQL schema

**Fixes Needed:**
```go
func NewSQLiteStorage(dataDir string) (*SQLiteStorage, error) {
    if dataDir == "" {
        var err error
        dataDir, err = determineDataDir()
        if err != nil {
            return nil, fmt.Errorf("determine data directory: %w", err)
        }
    }

    // Create data directory with proper permissions
    if err := os.MkdirAll(dataDir, 0755); err != nil {
        return nil, fmt.Errorf("create data directory %s: %w", dataDir, err)
    }

    dbPath := filepath.Join(dataDir, "wallets.db")
    
    // Log database location for user awareness
    log.Printf("[INFO] SQLite database: %s", dbPath)
    
    // ... rest of implementation
}
```

**Update GetStats to match storage.Stats:**
```go
func (s *SQLiteStorage) GetStats(ctx context.Context) (*storage.Stats, error) {
    stats := &storage.Stats{}
    
    // Get total count
    err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM wallets").Scan(&stats.TotalWallets)
    if err != nil {
        return nil, fmt.Errorf("count total wallets: %w", err)
    }
    
    if stats.TotalWallets == 0 {
        return stats, nil
    }
    
    // Get counts by status
    err = s.db.QueryRowContext(ctx, `
        SELECT 
            COUNT(CASE WHEN status = 0 THEN 1 END) as unused,
            COUNT(CASE WHEN status = 1 THEN 1 END) as used,
            COUNT(CASE WHEN status = 2 THEN 1 END) as reserved
        FROM wallets
    `).Scan(&stats.UnusedWallets, &stats.UsedWallets, &stats.ReservedWallets)
    if err != nil {
        return nil, fmt.Errorf("count by status: %w", err)
    }
    
    // Get timestamps
    err = s.db.QueryRowContext(ctx, `
        SELECT MIN(created_at), MAX(created_at) FROM wallets
    `).Scan(&stats.OldestWallet, &stats.NewestWallet)
    if err != nil {
        return nil, fmt.Errorf("get timestamps: %w", err)
    }
    
    return stats, nil
}
```

### 3.7 storage/postgres/postgres.go - Enhancements

**Add Fallback-Friendly Constructor:**
```go
// NewPostgresStorage creates a new PostgreSQL storage backend.
// Returns an error if the database is unreachable (caller should fallback).
func NewPostgresStorage(ctx context.Context, cfg *config.Config) (*PostgresStorage, error) {
    // Validate PostgreSQL-specific config
    if err := validatePostgresConfig(cfg); err != nil {
        return nil, fmt.Errorf("invalid postgres config: %w", err)
    }
    
    // Try to ensure database exists
    if err := database.EnsureDatabase(ctx, cfg); err != nil {
        return nil, fmt.Errorf("ensure database: %w", err)
    }
    
    // Try to connect
    pool, err := database.Connect(ctx, cfg)
    if err != nil {
        return nil, fmt.Errorf("connect to database: %w", err)
    }
    
    return &PostgresStorage{pool: pool}, nil
}

func validatePostgresConfig(cfg *config.Config) error {
    if cfg.DBHost == "" {
        return fmt.Errorf("DB_HOST cannot be empty")
    }
    if cfg.DBUser == "" {
        return fmt.Errorf("DB_USER cannot be empty")
    }
    if cfg.DBName == "" {
        return fmt.Errorf("DB_NAME cannot be empty")
    }
    return nil
}
```

---

## 4. Verification Strategy

### 4.1 Verification Scripts

**scripts/verify.sh (Linux/macOS):**
```bash
#!/usr/bin/env bash
set -euo pipefail

echo "=== EVM Wallet Generator - Verification Suite ==="
echo

# 1. Code formatting
echo "1. Checking code formatting..."
if [ -n "$(gofmt -l .)" ]; then
    echo "❌ FAIL: Code not formatted. Run: gofmt -w ."
    gofmt -l .
    exit 1
fi
echo "✓ PASS: Code is properly formatted"
echo

# 2. Build all packages
echo "2. Building all packages..."
if ! go build ./...; then
    echo "❌ FAIL: Build failed"
    exit 1
fi
echo "✓ PASS: All packages build successfully"
echo

# 3. Vet all packages
echo "3. Running go vet..."
if ! go vet ./...; then
    echo "❌ FAIL: go vet found issues"
    exit 1
fi
echo "✓ PASS: go vet clean"
echo

# 4. Cross-compile for Windows
echo "4. Cross-compiling for Windows..."
if ! GOOS=windows GOARCH=amd64 go build -o /tmp/evmwalletbot.exe ./cmd; then
    echo "❌ FAIL: Windows cross-compile failed"
    exit 1
fi
echo "✓ PASS: Windows executable built successfully"
echo

# 5. Run tests with race detector
echo "5. Running tests with race detector..."
if ! go test ./... -race -count=1; then
    echo "❌ FAIL: Tests failed"
    exit 1
fi
echo "✓ PASS: All tests passed"
echo

# 6. Check for carriage returns in Go files
echo "6. Checking for carriage returns in .go files..."
if grep -rl $'\r' --include='*.go' . 2>/dev/null; then
    echo "❌ FAIL: Found carriage returns in Go files"
    exit 1
fi
echo "✓ PASS: No carriage returns found"
echo

echo "=== ✓ ALL CHECKS PASSED ==="
```

**scripts/verify.bat (Windows):**
```batch
@echo off
setlocal enabledelayedexpansion

echo === EVM Wallet Generator - Verification Suite ===
echo.

REM 1. Code formatting
echo 1. Checking code formatting...
gofmt -l . > nul 2>&1
if %errorlevel% neq 0 (
    echo X FAIL: Code not formatted
    gofmt -l .
    exit /b 1
)
echo √ PASS: Code is properly formatted
echo.

REM 2. Build all packages
echo 2. Building all packages...
go build ./...
if %errorlevel% neq 0 (
    echo X FAIL: Build failed
    exit /b 1
)
echo √ PASS: All packages build successfully
echo.

REM 3. Vet all packages
echo 3. Running go vet...
go vet ./...
if %errorlevel% neq 0 (
    echo X FAIL: go vet found issues
    exit /b 1
)
echo √ PASS: go vet clean
echo.

REM 4. Cross-compile for Windows
echo 4. Building Windows executable...
go build -o evmwalletbot.exe ./cmd
if %errorlevel% neq 0 (
    echo X FAIL: Windows build failed
    exit /b 1
)
echo √ PASS: Windows executable built successfully
echo.

REM 5. Run tests
echo 5. Running tests...
go test ./... -count=1
if %errorlevel% neq 0 (
    echo X FAIL: Tests failed
    exit /b 1
)
echo √ PASS: All tests passed
echo.

echo === √ ALL CHECKS PASSED ===
```

### 4.2 Smoke Test Scenarios

**Test 1: Zero-Setup Launch (Critical)**
```bash
# Clean environment - no Postgres, no .env
rm -f .env
docker stop postgres 2>/dev/null || true

# Build and run
go build -o evmwalletbot ./cmd
./evmwalletbot --count 5 --export-mode paired --export-dir ./test-output

# Expected:
# - Exits 0
# - Creates SQLite database automatically
# - Creates ./test-output/address.txt with 5 lines
# - Creates ./test-output/privatekey.txt with 5 lines
# - No connection errors
# - Logs show "Using embedded SQLite storage"
```

**Test 2: Interactive Menu (Zero-Setup)**
```bash
# Pipe menu commands
echo -e "1\n5\n0" | ./evmwalletbot

# Expected:
# - Menu launches successfully
# - Generate 5 wallets works
# - Stats menu shows correct count
# - No crashes or errors
```

**Test 3: PostgreSQL Opt-In**
```bash
# Start Postgres
docker run -d --name test-pg -e POSTGRES_PASSWORD=test -p 5432:5432 postgres:15

# Create .env with Postgres config
cat > .env << EOF
STORAGE=postgres
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=test
DB_NAME=walletdb
EOF

# Run
./evmwalletbot --count 5

# Expected:
# - Logs show "Using PostgreSQL storage (opt-in)"
# - Connects to Postgres successfully
# - Generates 5 wallets
# - Exits 0
```

**Test 4: PostgreSQL Fallback**
```bash
# .env with Postgres but server not running
cat > .env << EOF
STORAGE=postgres
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=wrong
DB_NAME=walletdb
EOF

# Run
./evmwalletbot --count 5

# Expected:
# - Logs show "PostgreSQL unavailable: ..."
# - Logs show "Falling back to embedded SQLite storage"
# - Generates 5 wallets successfully
# - Exits 0 (NO CRASH)
```

**Test 5: CLI Flags Override**
```bash
# Override storage type via flag
./evmwalletbot --storage sqlite --data-dir ./custom-data --count 5

# Expected:
# - Uses SQLite despite any .env settings
# - Creates database in ./custom-data/wallets.db
# - Generates 5 wallets
# - Exits 0
```

**Test 6: Cross-Platform Build**
```bash
# Build for multiple platforms
GOOS=linux GOARCH=amd64 go build -o evmwalletbot-linux ./cmd
GOOS=windows GOARCH=amd64 go build -o evmwalletbot.exe ./cmd
GOOS=darwin GOARCH=amd64 go build -o evmwalletbot-mac ./cmd

# Verify no CGo dependencies
ldd evmwalletbot-linux 2>&1 | grep -i "not a dynamic"

# Expected:
# - All builds succeed
# - No CGo dependencies
# - Single static binary for each platform
```

---

## 5. Migration Strategy

### 5.1 Phased Rollout

**Phase 1: Storage Factory (Non-Breaking)**
- Add `storage/factory.go`
- Add CLI flags to `cmd/main.go`
- Update `config/config.go` defaults
- **Test:** Verify existing Postgres mode still works

**Phase 2: cmd/main.go Refactor**
- Replace `database.Connect()` with `storage.NewStorage()`
- Remove unconditional `os.Exit(1)` on DB failure
- Add fallback logic
- Update pool monitoring to be conditional
- **Test:** Verify zero-setup mode works

**Phase 3: CLI Package Refactor**
- Update all function signatures in `cli/menu.go`
- Replace `pool` with `store` throughout
- Make pool-specific functions conditional
- **Test:** Verify all menu options work with SQLite

**Phase 4: Core Package Refactor**
- Update `core/generator.go`
- Update `core/stats.go`
- Update `core/vanity.go`
- **Test:** Verify generation and stats work with both backends

**Phase 5: Storage Backend Polish**
- Enhance `storage/sqlite/sqlite.go` error handling
- Add logging for database location
- Ensure schema compatibility
- **Test:** Run full test suite

**Phase 6: Documentation & Scripts**
- Create verification scripts
- Update README
- Add smoke tests to CI
- **Test:** Run all verification checks

### 5.2 Rollback Strategy

**Git Strategy:**
- Each phase is a separate commit
- Tag each phase: `v1.1.0-phase1`, `v1.1.0-phase2`, etc.
- If a phase fails, revert to previous tag

**Testing Checkpoints:**
- After each phase, run: `./scripts/verify.sh`
- If verification fails, do NOT proceed to next phase
- Fix issues before continuing

**Backward Compatibility:**
- Existing `.env` files with Postgres config continue to work
- Default behavior changes from "require Postgres" to "use SQLite"
- Users can opt-in to Postgres with `STORAGE=postgres`

---

## 6. README Updates

### 6.1 New Quick Start Section

```markdown
## Quick Start (Zero Setup)

**Download and run - that's it!**

### Windows
```powershell
# Download the latest release
# Double-click evmwalletbot.exe or run in PowerShell:
.\evmwalletbot.exe
```

### Linux/macOS
```bash
# Download the latest release
chmod +x evmwalletbot
./evmwalletbot
```

**No PostgreSQL required!** The application uses an embedded SQLite database that's created automatically.

### Generate Wallets (Non-Interactive)
```bash
# Generate 1000 wallets and export to files
./evmwalletbot --count 1000 --export-mode paired --export-dir ./output
```

### Data Location
- **Windows:** Next to the executable or `%APPDATA%\evmwalletbot\`
- **Linux/macOS:** Next to the executable or `~/.config/evmwalletbot/`
- **Custom:** Use `--data-dir /path/to/data`

---

## Advanced: PostgreSQL Backend (Optional)

For high-performance scenarios or existing PostgreSQL infrastructure:

### 1. Create `.env` file:
```env
STORAGE=postgres
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=your_password
DB_NAME=walletdb
```

### 2. Start PostgreSQL:
```bash
docker run -d \
  --name wallet-postgres \
  -e POSTGRES_PASSWORD=your_password \
  -p 5432:5432 \
  postgres:15
```

### 3. Run the application:
```bash
./evmwalletbot
```

**Fallback:** If PostgreSQL is unavailable, the application automatically falls back to SQLite.

---

## Configuration

### Storage Options

| Flag | Environment | Default | Description |
|------|-------------|---------|-------------|
| `--storage` | `STORAGE` | `sqlite` | Backend: `sqlite` or `postgres` |
| `--data-dir` | `WALLET_DATA_DIR` | Auto | SQLite database directory |

### CLI Flags

```bash
./evmwalletbot [flags]

Flags:
  --count int           Generate N wallets and exit (non-interactive)
  --export-mode string  Export mode: paired, key-only, address-only, combined
  --export-dir string   Export directory path
  --storage string      Storage backend: sqlite (default) or postgres
  --data-dir string     Data directory for SQLite
  --version            Show version and exit
  --help               Show help and exit
```
```

### 6.2 Update Features Section

```markdown
## Features

- ✅ **Zero Setup** - Single executable, no external dependencies
- ✅ **Embedded Database** - SQLite included, no server required
- ✅ **Optional PostgreSQL** - Opt-in for high-performance scenarios
- ✅ **Cross-Platform** - Windows, Linux, macOS (single binary)
- ✅ **No CGo** - Pure Go, easy cross-compilation
- ✅ **Automatic Fallback** - Gracefully handles unavailable backends
- ✅ **Parallel Generation** - Multi-threaded wallet creation
- ✅ **Interactive CLI** - User-friendly menu system
- ✅ **Batch Export** - Multiple export formats
- ✅ **Vanity Addresses** - Custom prefix/suffix patterns
```

---

## 7. Definition of Done Checklist

### Code Quality
- [ ] `gofmt -l .` returns empty (all code formatted)
- [ ] `go build ./...` exits 0 (all packages build)
- [ ] `go vet ./...` exits 0 (no vet issues)
- [ ] `go test ./... -race -count=1` passes (all tests pass)
- [ ] No carriage returns in `.go` files
- [ ] Windows cross-compile succeeds: `GOOS=windows GOARCH=amd64 go build -o evmwalletbot.exe ./cmd`

### Zero-Setup Requirements
- [ ] Fresh machine, NO Postgres, NO .env → binary launches menu (never exits on DB)
- [ ] Generate wallets works with embedded SQLite out-of-the-box
- [ ] Stats menu shows correct data from SQLite
- [ ] Lookup menu works with SQLite
- [ ] Data files auto-created in writable path
- [ ] Folders created automatically (no manual mkdir)

### PostgreSQL Opt-In
- [ ] `STORAGE=postgres` in .env enables PostgreSQL mode
- [ ] PostgreSQL mode works when server is available
- [ ] PostgreSQL unavailable → warns and falls back to SQLite (no crash)
- [ ] Pool monitoring only runs for PostgreSQL backend
- [ ] All menu features work with both backends

### Cross-Platform
- [ ] Windows .exe builds with no CGo dependencies
- [ ] Linux binary builds and runs
- [ ] macOS binary builds and runs
- [ ] All paths use `filepath` (not hardcoded `/`)
- [ ] Directories created with `os.MkdirAll`

### Documentation
- [ ] README "Quick start" = run one file, zero setup
- [ ] PostgreSQL documented as optional opt-in feature
- [ ] CLI flags documented (`--storage`, `--data-dir`)
- [ ] Data location documented for each OS
- [ ] Export options documented

### Testing
- [ ] Smoke test 1 passes: Zero-setup non-interactive generation
- [ ] Smoke test 2 passes: Zero-setup interactive menu
- [ ] Smoke test 3 passes: PostgreSQL opt-in mode
- [ ] Smoke test 4 passes: PostgreSQL fallback to SQLite
- [ ] Smoke test 5 passes: CLI flags override config
- [ ] Smoke test 6 passes: Cross-platform builds
- [ ] `scripts/verify.sh` passes 3 times in a row
- [ ] `scripts/verify.bat` passes on Windows

### Git Hygiene
- [ ] Every commit in the series builds clean
- [ ] No broken intermediate commits
- [ ] Each phase tagged for easy rollback
- [ ] Commit messages follow conventional format

---

## 8. Risk Mitigation

### Risk 1: SQLite Performance vs PostgreSQL
**Mitigation:** 
- Use WAL mode for better concurrency
- Batch inserts with transactions
- Benchmark both backends
- Document performance characteristics

### Risk 2: Schema Compatibility
**Mitigation:**
- Use same schema for both backends
- Test migrations on both
- Ensure storage.Stats matches both

### Risk 3: Data Directory Permissions
**Mitigation:**
- Try executable directory first
- Fallback to user config directory
- Clear error messages on permission issues
- Document data locations

### Risk 4: Windows Path Handling
**Mitigation:**
- Use `filepath.Join` everywhere
- Test on actual Windows machine
- Use `filepath.Separator` not `/`
- Handle both `\` and `/` in user input

### Risk 5: Breaking Existing Users
**Mitigation:**
- Existing `.env` files continue to work
- PostgreSQL mode is opt-in, not removed
- Fallback ensures no data loss
- Clear migration guide in release notes

---

## 9. Success Metrics

### Quantitative
- [ ] Zero-setup smoke test passes on 3 different machines
- [ ] Application starts in <1 second (vs current ~3-5 seconds)
- [ ] Binary size <20MB (single file)
- [ ] Memory usage <50MB for embedded mode
- [ ] Generation speed within 10% of PostgreSQL mode

### Qualitative
- [ ] User can download and run without reading docs
- [ ] No "database connection failed" errors on first run
- [ ] Clear feedback about which backend is being used
- [ ] Graceful fallback with helpful messages
- [ ] Professional error messages (no stack traces to user)

---

## 10. Timeline Estimate

**Phase 1:** Storage Factory (2 hours)
- Create factory.go
- Add CLI flags
- Test with existing Postgres mode

**Phase 2:** cmd/main.go Refactor (3 hours)
- Replace database.Connect
- Add fallback logic
- Update pool monitoring
- Test zero-setup mode

**Phase 3:** CLI Package Refactor (4 hours)
- Update all function signatures
- Replace pool with store
- Make pool functions conditional
- Test all menu options

**Phase 4:** Core Package Refactor (2 hours)
- Update generator.go
- Update stats.go
- Update vanity.go
- Test generation and stats

**Phase 5:** Storage Backend Polish (2 hours)
- Enhance SQLite error handling
- Add logging
- Ensure schema compatibility
- Test both backends

**Phase 6:** Documentation & Scripts (2 hours)
- Create verification scripts
- Update README
- Write smoke tests
- Final testing

**Total Estimated Time:** 15 hours

**Buffer for Issues:** +5 hours

**Total with Buffer:** 20 hours

---

## 11. Post-Implementation Tasks

### Immediate (Before Release)
- [ ] Run full verification suite 3 times
- [ ] Test on Windows, Linux, macOS
- [ ] Create release binaries for all platforms
- [ ] Write release notes with migration guide
- [ ] Update CHANGELOG.md

### Short-Term (Within 1 Week)
- [ ] Monitor GitHub issues for user feedback
- [ ] Add telemetry for backend usage (opt-in)
- [ ] Create video tutorial for zero-setup
- [ ] Update documentation with FAQs

### Long-Term (Within 1 Month)
- [ ] Benchmark SQLite vs PostgreSQL performance
- [ ] Consider adding more storage backends (MySQL, etc.)
- [ ] Optimize SQLite for very large datasets
- [ ] Add backup/restore functionality

---

## Appendix A: File Modification Summary

| File | Changes | Lines Changed | Risk |
|------|---------|---------------|------|
| `cmd/main.go` | Complete refactor | ~100 | High |
| `cli/menu.go` | Interface conversion | ~50 | Medium |
| `core/generator.go` | Interface conversion | ~20 | Low |
| `core/stats.go` | Interface conversion | ~30 | Low |
| `core/vanity.go` | Interface conversion | ~10 | Low |
| `storage/factory.go` | New file | ~60 | Low |
| `storage/sqlite/sqlite.go` | Enhancements | ~30 | Low |
| `storage/postgres/postgres.go` | Enhancements | ~20 | Low |
| `config/config.go` | Minor updates | ~5 | Low |
| `scripts/verify.sh` | New file | ~80 | N/A |
| `scripts/verify.bat` | New file | ~60 | N/A |
| `README.md` | Major rewrite | ~200 | N/A |

**Total Lines Changed:** ~665 lines
**New Files:** 3
**Modified Files:** 9

---

## Appendix B: Testing Matrix

| Test Scenario | SQLite | PostgreSQL | Expected Result |
|---------------|--------|------------|-----------------|
| No .env, no Postgres | ✓ | - | Launch with SQLite |
| .env with STORAGE=sqlite | ✓ | - | Use SQLite |
| .env with STORAGE=postgres, Postgres running | - | ✓ | Use PostgreSQL |
| .env with STORAGE=postgres, Postgres down | ✓ | - | Fallback to SQLite |
| --storage sqlite flag | ✓ | - | Override to SQLite |
| --storage postgres flag, Postgres running | - | ✓ | Override to PostgreSQL |
| --storage postgres flag, Postgres down | ✓ | - | Fallback to SQLite |
| Generate 1000 wallets | ✓ | ✓ | Success both |
| Stats menu | ✓ | ✓ | Correct data both |
| Lookup menu | ✓ | ✓ | Find wallets both |
| Export to files | ✓ | ✓ | Files created both |
| Vanity generation | ✓ | ✓ | Success both |
| Pool monitoring | N/A | ✓ | Only for PostgreSQL |
| Cross-compile Windows | ✓ | ✓ | Single .exe |
| Cross-compile Linux | ✓ | ✓ | Single binary |
| Cross-compile macOS | ✓ | ✓ | Single binary |

---

## Appendix C: Error Message Standards

### Good Error Messages (User-Friendly)
```
[INFO] Using embedded SQLite storage (zero-setup)
[INFO] SQLite database: C:\Users\user\AppData\Roaming\evmwalletbot\wallets.db
[WARN] PostgreSQL unavailable: connection refused (localhost:5432)
[INFO] Falling back to embedded SQLite storage
[ERROR] Cannot create data directory: permission denied
        → Try running with --data-dir flag to specify a writable location
```

### Bad Error Messages (Avoid)
```
panic: runtime error: invalid memory address
database/sql: connection refused
Error: pq: password authentication failed for user "postgres"
```

### Guidelines
- Always prefix with severity: `[INFO]`, `[WARN]`, `[ERROR]`
- Explain what happened in plain English
- Suggest a solution when possible
- Never show stack traces to end users
- Log technical details to file, show friendly message to user

---

## Appendix D: Performance Benchmarks (Target)

| Operation | SQLite (Target) | PostgreSQL (Current) | Acceptable? |
|-----------|-----------------|----------------------|-------------|
| Startup time | <1s | ~3-5s | ✓ Better |
| Generate 1K wallets | ~2s | ~1.5s | ✓ Within 10% |
| Generate 10K wallets | ~15s | ~12s | ✓ Within 10% |
| Generate 100K wallets | ~2.5min | ~2min | ✓ Within 10% |
| Stats query | <100ms | <50ms | ✓ Acceptable |
| Lookup by ID | <10ms | <5ms | ✓ Acceptable |
| Memory usage | <50MB | ~100MB | ✓ Better |
| Binary size | <20MB | N/A | ✓ Acceptable |

**Note:** These are targets. Actual performance will be measured during implementation.

---

## End of Implementation Plan

**Next Steps:**
1. Review this plan with team/stakeholders
2. Get approval to proceed
3. Create feature branch: `feature/zero-setup-refactor`
4. Begin Phase 1: Storage Factory
5. Commit after each phase with verification passing
6. Create PR when all phases complete

**Questions/Concerns:**
- Raise any concerns about the approach
- Suggest improvements to the plan
- Identify any missing requirements
- Discuss timeline and resource allocation
