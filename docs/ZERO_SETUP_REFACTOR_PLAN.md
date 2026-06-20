# Zero-Setup Refactor Plan - Single Binary with Embedded Storage

## Problem Statement

Current state (commit 97c680b):
- **BLOCKS on missing PostgreSQL** - exits with error if Postgres not running
- Requires external database setup, .env configuration
- Not a true single-file, zero-setup application

```
[ERROR] Database setup failed: cannot reach PostgreSQL server (localhost:5432)
```

## Goal

Transform into a **true single-binary, zero-setup application**:
- Run ONE executable → EVERYTHING works
- NO external services (no Postgres install required)
- NO configuration files (no .env required)
- NO manual setup steps
- Works on fresh machine out of the box

## Architecture Changes

### 1. Storage Interface Abstraction

Create `storage/interface.go` with all database operations:

```go
package storage

import "context"

type Storage interface {
    // Wallet operations
    SaveWallets(ctx context.Context, wallets []*wallet.Wallet) ([]int64, error)
    GetWalletByID(ctx context.Context, id int64) (*WalletRecord, error)
    GetWalletByAddress(ctx context.Context, address []byte) (*WalletRecord, error)
    CountWallets(ctx context.Context) (int64, error)
    
    // Statistics
    GetStats(ctx context.Context) (*Stats, error)
    
    // Health & monitoring
    HealthCheck(ctx context.Context) error
    GetPoolStats() *PoolStats // Returns nil for non-pooled backends
    
    // Lifecycle
    Close() error
    Migrate(ctx context.Context) error
}

type WalletRecord struct {
    ID         int64
    Address    []byte
    PrivateKey []byte
    CreatedAt  time.Time
    Status     int
    Metadata   map[string]interface{}
}

type Stats struct {
    TotalWallets   int64
    UnusedWallets  int64
    UsedWallets    int64
    ReservedWallets int64
    OldestWallet   time.Time
    NewestWallet   time.Time
}

type PoolStats struct {
    TotalConns    int32
    IdleConns     int32
    AcquiredConns int32
    MaxConns      int32
}
```

### 2. Implementation: Embedded SQLite (Default)

**Package:** `storage/sqlite`

**Library:** `modernc.org/sqlite` (pure Go, no CGo, cross-platform)

**Features:**
- Single file database (e.g., `wallets.db`)
- Auto-create database file on first run
- Auto-run migrations on startup
- Same schema as Postgres (wallets table with indexes)
- Full SQL query support for stats, lookups, monitoring

**File Location Strategy:**
```go
// Priority order:
1. --data-dir flag if provided
2. WALLET_DATA_DIR env var if set
3. Next to executable: filepath.Dir(os.Executable())
4. Fallback: os.UserConfigDir() + "/evmwalletbot"
```

**Auto-create directories:**
```go
dataDir := determineDataDir()
if err := os.MkdirAll(dataDir, 0755); err != nil {
    return fmt.Errorf("create data directory: %w", err)
}
dbPath := filepath.Join(dataDir, "wallets.db")
```

**Schema Migration:**
```go
func (s *SQLiteStorage) Migrate(ctx context.Context) error {
    // Create wallets table if not exists
    // Create indexes
    // Same structure as Postgres version
}
```

**Batch Insert Optimization:**
```go
// Use SQLite's batch insert with transaction
// INSERT INTO wallets (address, private_key, created_at, status) VALUES (?, ?, ?, ?), ...
// Commit transaction after batch
```

### 3. Implementation: PostgreSQL (Optional)

**Package:** `storage/postgres`

**Activation:** Only when explicitly enabled:
- `STORAGE=postgres` in .env
- `--storage postgres` flag
- `DB_ENABLED=true` in .env

**Behavior:**
- If enabled but unreachable → **WARN and FALLBACK to SQLite**
- Never crash/exit on connection failure
- Log warning: "PostgreSQL unavailable, using embedded storage"

**Wrapper around existing code:**
```go
type PostgresStorage struct {
    pool *pgxpool.Pool
}

func NewPostgresStorage(cfg *config.Config) (*PostgresStorage, error) {
    // Existing database.Connect() logic
    // Return error if unreachable (caller handles fallback)
}
```

### 4. Configuration Changes

**Make .env fully optional:**

```go
// config/config.go
type Config struct {
    // Storage configuration
    StorageType      string // "sqlite" (default) | "postgres"
    DataDir          string // Auto-determined if empty
    
    // PostgreSQL (only used if StorageType=postgres)
    DBHost           string
    DBPort           int
    DBUser           string
    DBPassword       string
    DBName           string
    // ... existing DB fields
    
    // Generation settings (with defaults)
    Workers          int    // Default: runtime.NumCPU()
    BatchSize        int    // Default: 500
    
    // Export settings (with defaults)
    ExportEnabled    bool   // Default: false
    ExportMode       string // Default: "paired"
    ExportDir        string // Default: "./exports"
    // ... existing export fields
}

func Load() (*Config, error) {
    // Try to load .env (best effort, no error if missing)
    _ = godotenv.Load()
    
    // Provide safe defaults for everything
    cfg := &Config{
        StorageType: getEnv("STORAGE", "sqlite"),
        DataDir:     getEnv("WALLET_DATA_DIR", ""), // Auto-determined
        Workers:     getEnvInt("WORKERS", runtime.NumCPU()),
        BatchSize:   getEnvInt("BATCH_SIZE", 500),
        // ... all fields with defaults
    }
    
    // Only validate Postgres fields if StorageType=postgres
    if cfg.StorageType == "postgres" {
        if err := cfg.ValidatePostgres(); err != nil {
            return nil, err
        }
    }
    
    return cfg, nil
}
```

### 5. Main Entry Point Changes

**cmd/main.go refactor:**

```go
func main() {
    // Parse flags
    var (
        storageType = flag.String("storage", "", "Storage backend: sqlite|postgres")
        dataDir     = flag.String("data-dir", "", "Data directory path")
        generateCount = flag.Int("count", 0, "Generate N wallets (non-interactive)")
        // ... existing flags
    )
    flag.Parse()
    
    // Load config (never fails, uses defaults)
    cfg, err := config.Load()
    if err != nil {
        log.Printf("[WARN] Config load error: %v, using defaults", err)
        cfg = config.DefaultConfig()
    }
    
    // Override with flags
    if *storageType != "" {
        cfg.StorageType = *storageType
    }
    if *dataDir != "" {
        cfg.DataDir = *dataDir
    }
    
    // Initialize storage (NEVER exits on failure)
    store, err := initStorage(cfg)
    if err != nil {
        log.Fatalf("[FATAL] Storage initialization failed: %v", err)
    }
    defer store.Close()
    
    // Run migrations
    if err := store.Migrate(ctx); err != nil {
        log.Printf("[WARN] Migration error: %v", err)
    }
    
    // Show stats if data exists
    if stats, err := store.GetStats(ctx); err == nil && stats.TotalWallets > 0 {
        log.Printf("[INFO] Loaded %d existing wallets", stats.TotalWallets)
        core.PrintStats(stats)
    }
    
    // Non-interactive mode
    if *generateCount > 0 {
        if err := core.GenerateWallets(ctx, store, cfg, *generateCount); err != nil {
            log.Fatalf("[ERROR] Generation failed: %v", err)
        }
        return
    }
    
    // Interactive mode
    cli.Run(ctx, store, cfg)
}

func initStorage(cfg *config.Config) (storage.Storage, error) {
    switch cfg.StorageType {
    case "postgres":
        // Try Postgres first
        pgStore, err := postgres.NewPostgresStorage(cfg)
        if err != nil {
            log.Printf("[WARN] PostgreSQL unavailable: %v", err)
            log.Printf("[INFO] Falling back to embedded storage")
            return sqlite.NewSQLiteStorage(cfg)
        }
        log.Printf("[INFO] Using PostgreSQL storage")
        return pgStore, nil
        
    case "sqlite", "":
        log.Printf("[INFO] Using embedded SQLite storage")
        return sqlite.NewSQLiteStorage(cfg)
        
    default:
        return nil, fmt.Errorf("unknown storage type: %s", cfg.StorageType)
    }
}
```

**Remove Postgres-specific code from main:**
- Remove `database.EnsureDatabase()` call
- Remove `database.Connect()` call
- Remove connection pool monitoring goroutine (move to postgres storage)
- Remove all `os.Exit(1)` on database errors

### 6. Update All Consumers

**Files to update:**

1. **core/generator.go**
   - Change signature: `func GenerateWallets(ctx context.Context, store storage.Storage, cfg *config.Config, totalWallets int) error`
   - Replace `pool *pgxpool.Pool` with `store storage.Storage`
   - Replace `insertWalletBatchCopy(pool, batch)` with `store.SaveWallets(ctx, batch)`

2. **core/stats.go**
   - Change signature: `func GetStats(ctx context.Context, store storage.Storage) (*storage.Stats, error)`
   - Replace direct SQL queries with `store.GetStats(ctx)`

3. **cli/menu.go**
   - Change signature: `func Run(ctx context.Context, store storage.Storage, cfg *config.Config)`
   - Update all menu handlers to use `store` interface
   - Update pool monitoring to check `store.GetPoolStats()` (returns nil for SQLite)

4. **cli/status.go**
   - Update to use `storage.Storage` interface

### 7. Cross-Platform Considerations

**Windows-specific:**
- Use `filepath.Join()` for all paths (not `/`)
- Use `os.MkdirAll()` to create directories
- Test with PowerShell and cmd.exe

**Build command:**
```bash
# No CGo required with modernc.org/sqlite
GOOS=windows GOARCH=amd64 go build -o evmwalletbot.exe ./cmd
```

**File paths:**
```go
// Good (cross-platform)
dbPath := filepath.Join(dataDir, "wallets.db")
exportPath := filepath.Join(cfg.ExportDir, "address.txt")

// Bad (Unix-only)
dbPath := dataDir + "/wallets.db"
```

## Implementation Steps

### Phase 1: Storage Interface & SQLite Implementation

1. Create `storage/interface.go` with Storage interface
2. Create `storage/sqlite/sqlite.go` with SQLite implementation
3. Add `modernc.org/sqlite` dependency: `go get modernc.org/sqlite`
4. Implement all interface methods for SQLite
5. Add schema migration for SQLite
6. Add unit tests for SQLite storage

### Phase 2: Postgres Wrapper

1. Create `storage/postgres/postgres.go`
2. Wrap existing `database/` package code
3. Implement Storage interface for Postgres
4. Add connection pool monitoring to Postgres storage
5. Add unit tests (gated behind env var)

### Phase 3: Refactor Consumers

1. Update `core/generator.go` to use Storage interface
2. Update `core/stats.go` to use Storage interface
3. Update `cli/menu.go` to use Storage interface
4. Update `cli/status.go` to use Storage interface
5. Remove direct `*pgxpool.Pool` dependencies

### Phase 4: Main Entry Point

1. Update `cmd/main.go` with storage initialization
2. Add CLI flags (--storage, --data-dir)
3. Implement fallback logic (Postgres → SQLite)
4. Remove database.EnsureDatabase() and database.Connect()
5. Remove os.Exit(1) on database errors

### Phase 5: Configuration

1. Update `config/config.go` with storage fields
2. Make all fields have safe defaults
3. Make .env optional (best-effort load)
4. Add StorageType and DataDir fields
5. Update validation to be storage-specific

### Phase 6: Testing & Verification

1. Create `scripts/verify-zero-setup.sh` for smoke tests
2. Test on fresh machine with no Postgres
3. Test non-interactive mode: `./evmwalletbot --count 5`
4. Test interactive mode with embedded storage
5. Test Postgres fallback behavior
6. Test Windows cross-compilation
7. Run full verification suite 3x

### Phase 7: Documentation

1. Update README with "Quick Start" section
2. Document zero-setup default behavior
3. Document optional Postgres mode
4. Update .env.example with storage configuration
5. Add troubleshooting section

## Verification Checklist

### Build & Test
- [ ] `gofmt -l .` returns empty
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] `go test ./... -race -count=1` passes
- [ ] `GOOS=windows GOARCH=amd64 go build -o evmwalletbot.exe ./cmd` succeeds
- [ ] No carriage returns in .go files: `! grep -rl $'\r' --include='*.go' .`

### Zero-Setup Smoke Test
- [ ] Fresh machine, NO Postgres, NO .env
- [ ] Run: `./evmwalletbot --count 5 --export-mode paired --export-dir ./out`
- [ ] Exits with code 0
- [ ] Creates `./out/address.txt` and `./out/privatekey.txt` with 5 lines
- [ ] Creates embedded DB file automatically
- [ ] No connection errors in output

### Interactive Mode Test
- [ ] Run: `./evmwalletbot` (no args)
- [ ] Menu launches successfully
- [ ] Generate wallets works
- [ ] Statistics menu works
- [ ] Wallet lookup works
- [ ] All features work with embedded storage

### Postgres Fallback Test
- [ ] Set `STORAGE=postgres` in .env
- [ ] Ensure Postgres is NOT running
- [ ] Run: `./evmwalletbot`
- [ ] Logs warning about Postgres unavailable
- [ ] Falls back to SQLite
- [ ] Menu launches successfully
- [ ] All features work

### Cross-Platform Test
- [ ] Build on Linux: `go build -o evmwalletbot ./cmd`
- [ ] Build for Windows: `GOOS=windows GOARCH=amd64 go build -o evmwalletbot.exe ./cmd`
- [ ] Test .exe on Windows machine
- [ ] Verify paths use `filepath.Join()`
- [ ] Verify directories auto-created

## File Structure After Refactor

```
evmwalletbot/
├── cmd/
│   └── main.go                    # Updated: storage init, no Postgres dependency
├── storage/
│   ├── interface.go               # NEW: Storage interface
│   ├── sqlite/
│   │   ├── sqlite.go              # NEW: SQLite implementation
│   │   ├── sqlite_test.go         # NEW: SQLite tests
│   │   └── migrations.go          # NEW: SQLite schema
│   └── postgres/
│       ├── postgres.go            # NEW: Postgres wrapper
│       └── postgres_test.go       # NEW: Postgres tests (gated)
├── database/                      # KEEP: Used by postgres storage
│   ├── db.go
│   └── migrations.go
├── core/
│   ├── generator.go               # UPDATED: Use storage.Storage
│   └── stats.go                   # UPDATED: Use storage.Storage
├── cli/
│   ├── menu.go                    # UPDATED: Use storage.Storage
│   └── status.go                  # UPDATED: Use storage.Storage
├── config/
│   └── config.go                  # UPDATED: Add storage fields, defaults
├── wallet/                        # UNCHANGED
├── scripts/
│   ├── verify.bat                 # UPDATED: Add zero-setup test
│   ├── verify.sh                  # UPDATED: Add zero-setup test
│   └── verify-zero-setup.sh       # NEW: Smoke test script
├── docs/
│   └── ZERO_SETUP_REFACTOR_PLAN.md # NEW: This document
├── .env.example                   # UPDATED: Add storage config
├── README.md                      # UPDATED: Quick start section
└── go.mod                         # UPDATED: Add modernc.org/sqlite
```

## Dependencies

**Add:**
```bash
go get modernc.org/sqlite
```

**Keep existing:**
- github.com/ethereum/go-ethereum (crypto)
- github.com/jackc/pgx/v5 (Postgres, optional)
- github.com/joho/godotenv (config, optional)

## Success Criteria

### Must Have
1. ✅ Single binary runs on fresh machine with NO external dependencies
2. ✅ NO Postgres required for default operation
3. ✅ NO .env required for default operation
4. ✅ All menu features work (generate, stats, lookup, monitoring)
5. ✅ Data persists across runs (embedded DB)
6. ✅ Export feature works (plaintext files)
7. ✅ Cross-compiles to Windows .exe with no CGo
8. ✅ Postgres is optional (opt-in only)
9. ✅ Graceful fallback if Postgres unavailable
10. ✅ All tests pass (including zero-setup smoke test)

### Nice to Have
- Migration tool to convert SQLite → Postgres
- Performance comparison (SQLite vs Postgres)
- Backup/restore commands for embedded DB
- Compression for embedded DB file

## Timeline Estimate

- **Phase 1-2:** Storage interface & implementations (4-6 hours)
- **Phase 3-4:** Refactor consumers & main (3-4 hours)
- **Phase 5:** Configuration updates (1-2 hours)
- **Phase 6:** Testing & verification (2-3 hours)
- **Phase 7:** Documentation (1-2 hours)

**Total:** 11-17 hours of focused development

## Risks & Mitigations

### Risk: SQLite performance vs Postgres
**Mitigation:** 
- Use transactions for batch inserts
- Add indexes on address, status, created_at
- Benchmark both backends
- Document performance characteristics

### Risk: Breaking existing Postgres users
**Mitigation:**
- Keep Postgres fully functional
- Make it opt-in with clear documentation
- Provide migration path
- Test both backends thoroughly

### Risk: Cross-platform path issues
**Mitigation:**
- Use `filepath` package consistently
- Test on Windows, Linux, macOS
- Auto-create directories with proper permissions
- Handle path separators correctly

### Risk: Data location confusion
**Mitigation:**
- Clear logging of data directory location
- Document data location strategy
- Add `--data-dir` flag for explicit control
- Show data path in stats/help menu

## Next Steps

1. Review and approve this plan
2. Create feature branch: `feature/zero-setup-refactor`
3. Implement Phase 1 (Storage interface & SQLite)
4. Run verification after each phase
5. Create PR with full test results
6. Update documentation
7. Release new version with zero-setup capability

## Questions for Review

1. Is `modernc.org/sqlite` acceptable? (pure Go, no CGo, cross-platform)
2. Should we support migration from SQLite → Postgres?
3. Should we add a `--migrate` command for data export/import?
4. What should be the default data directory location?
5. Should we compress the embedded DB file?
6. Should we add backup/restore commands?

---

**Status:** Plan ready for review and implementation
**Target:** Single binary, zero-setup, works everywhere
**Fallback:** Postgres optional, graceful degradation
