# Ultra-Compact Repository Refactoring Plan

**Date:** 2026-06-20  
**Objective:** Reduce repository to absolute minimum structure with single entry point

---

## Current State (Post-Cleanup)

```
evmwalletbot/
├── .gitignore
├── go.mod
├── go.sum
├── LICENSE
├── README.md
├── cmd/
│   ├── main.go
│   ├── storage_factory.go
│   └── verify.go
├── config/
│   └── config.go
├── cli/
│   ├── menu.go
│   ├── hd.go
│   ├── import.go
│   ├── status.go
│   └── vanity_menu.go
├── core/
│   ├── generator.go
│   ├── stats.go
│   ├── vanity.go
│   ├── progress.go
│   ├── benchmark.go
│   ├── maintenance.go
│   ├── retry.go
│   ├── colors.go
│   ├── constants.go
│   └── box.go
├── wallet/
│   ├── generator.go
│   ├── exporter.go
│   ├── hd.go
│   ├── import.go
│   └── vanity.go
├── storage/
│   ├── interface.go
│   ├── label.go
│   ├── sqlite/
│   │   ├── sqlite.go
│   │   ├── vanity.go
│   │   └── vanity_resume.go
│   └── postgres/
│       └── postgres.go
└── database/
    ├── db.go
    └── migrations.go
```

**Current:** 27 files in 9 directories

---

## Target Ultra-Compact Structure

```
evmwalletbot/
├── main.go                    ← Single entry point (merged from cmd/)
├── README.md                  ← Minimal user guide
├── LICENSE                    ← Legal requirement
├── .gitignore                 ← Git configuration
├── go.mod                     ← Module definition
├── go.sum                     ← Dependency checksums
└── data/
    └── src/
        ├── config.go          ← Configuration (from config/)
        ├── storage.go         ← Storage factory (from cmd/storage_factory.go)
        ├── verify.go          ← Verification (from cmd/verify.go)
        ├── menu.go            ← CLI menu (merged from cli/)
        ├── generator.go       ← Core generation (merged from core/)
        ├── wallet.go          ← Wallet operations (merged from wallet/)
        ├── sqlite.go          ← SQLite storage (merged from storage/sqlite/)
        ├── postgres.go        ← PostgreSQL storage (from storage/postgres/)
        ├── database.go        ← Database utilities (merged from database/)
        └── ui.go              ← UI utilities (merged colors, box, progress)
```

**Target:** 16 files in 2 directories (41% reduction from current)

---

## Analysis: Go Module Constraints

### Critical Issue: Go Package Structure

Go has strict requirements for package organization:

1. **Import paths are tied to directory structure**
   - Current: `import "evmwalletbot/cli"`
   - Proposed: `import "evmwalletbot/data/src"`

2. **Package names must match directory names**
   - Current: `package cli` in `cli/` directory
   - Proposed: `package src` in `data/src/` directory

3. **All files in same directory must have same package name**
   - Cannot mix `package main` with `package src` in same directory

### Recommended Approach

**Option A: Flat Structure (Most Compact)**
```
evmwalletbot/
├── main.go              ← package main (entry point)
├── config.go            ← package main
├── storage.go           ← package main
├── menu.go              ← package main
├── generator.go         ← package main
├── wallet.go            ← package main
├── database.go          ← package main
├── ui.go                ← package main
├── README.md
├── LICENSE
├── .gitignore
├── go.mod
└── go.sum
```
**Pros:** Simplest possible structure, no imports needed  
**Cons:** All code in `package main`, harder to test individual components

**Option B: Single Internal Package (Balanced)**
```
evmwalletbot/
├── main.go              ← package main (entry point only)
├── README.md
├── LICENSE
├── .gitignore
├── go.mod
├── go.sum
└── internal/
    ├── config.go        ← package internal
    ├── storage.go       ← package internal
    ├── menu.go          ← package internal
    ├── generator.go     ← package internal
    ├── wallet.go        ← package internal
    ├── database.go      ← package internal
    └── ui.go            ← package internal
```
**Pros:** Clean separation, testable, standard Go practice  
**Cons:** One extra directory level

**Option C: Data/Src Structure (As Requested)**
```
evmwalletbot/
├── main.go              ← package main
├── README.md
├── LICENSE
├── .gitignore
├── go.mod
├── go.sum
└── data/
    └── src/
        ├── config.go    ← package src
        ├── storage.go   ← package src
        ├── menu.go      ← package src
        ├── generator.go ← package src
        ├── wallet.go    ← package src
        ├── database.go  ← package src
        └── ui.go        ← package src
```
**Pros:** Matches requested structure  
**Cons:** Unusual for Go, `data/src` implies data files not source code

---

## Recommended: Option B (Single Internal Package)

This is the most idiomatic Go approach that achieves maximum compactness while maintaining code quality.

---

## File Consolidation Plan

### 1. Entry Point (main.go)
**Merge:** `cmd/main.go` → `main.go`
- Keep main() function
- Keep signal handling
- Keep startup logic
- Remove: Nothing (this is the entry point)

### 2. Configuration (internal/config.go)
**Merge:** `config/config.go` → `internal/config.go`
- Keep all configuration loading
- Keep environment variable parsing
- Keep defaults

### 3. Storage Factory (internal/storage.go)
**Merge:**
- `cmd/storage_factory.go` → `internal/storage.go`
- `storage/interface.go` → `internal/storage.go`
- `storage/label.go` → `internal/storage.go`

**Result:** Single file with:
- Storage interface definition
- Storage factory function
- Label management
- ~150 lines total

### 4. SQLite Storage (internal/sqlite.go)
**Merge:**
- `storage/sqlite/sqlite.go` → `internal/sqlite.go`
- `storage/sqlite/vanity.go` → `internal/sqlite.go`
- `storage/sqlite/vanity_resume.go` → `internal/sqlite.go`

**Result:** Single SQLite implementation file (~400 lines)

### 5. PostgreSQL Storage (internal/postgres.go)
**Keep:** `storage/postgres/postgres.go` → `internal/postgres.go`
- No merge needed (already single file)

### 6. Database Utilities (internal/database.go)
**Merge:**
- `database/db.go` → `internal/database.go`
- `database/migrations.go` → `internal/database.go`

**Result:** Database utilities + migrations (~200 lines)

### 7. CLI Menu (internal/menu.go)
**Merge:**
- `cli/menu.go` → `internal/menu.go`
- `cli/hd.go` → `internal/menu.go`
- `cli/import.go` → `internal/menu.go`
- `cli/status.go` → `internal/menu.go`
- `cli/vanity_menu.go` → `internal/menu.go`

**Result:** All menu functions in one file (~600 lines)

### 8. Core Generator (internal/generator.go)
**Merge:**
- `core/generator.go` → `internal/generator.go`
- `core/stats.go` → `internal/generator.go`
- `core/vanity.go` → `internal/generator.go`
- `core/benchmark.go` → `internal/generator.go`
- `core/maintenance.go` → `internal/generator.go`
- `core/retry.go` → `internal/generator.go`

**Result:** All generation logic (~700 lines)

### 9. Wallet Operations (internal/wallet.go)
**Merge:**
- `wallet/generator.go` → `internal/wallet.go`
- `wallet/exporter.go` → `internal/wallet.go`
- `wallet/hd.go` → `internal/wallet.go`
- `wallet/import.go` → `internal/wallet.go`
- `wallet/vanity.go` → `internal/wallet.go`

**Result:** All wallet operations (~500 lines)

### 10. UI Utilities (internal/ui.go)
**Merge:**
- `core/colors.go` → `internal/ui.go`
- `core/box.go` → `internal/ui.go`
- `core/progress.go` → `internal/ui.go`
- `core/constants.go` → `internal/ui.go`
- `cmd/verify.go` → `internal/ui.go` (verification is UI-related)

**Result:** All UI/display utilities (~300 lines)

---

## Import Path Updates

### Before (Current)
```go
import (
    "evmwalletbot/cli"
    "evmwalletbot/config"
    "evmwalletbot/core"
    "evmwalletbot/storage"
    "evmwalletbot/wallet"
    "evmwalletbot/database"
)
```

### After (Ultra-Compact)
```go
import (
    "evmwalletbot/internal"
)
```

All functions accessed via `internal.FunctionName()`

---

## Migration Steps

### Phase 1: Create Structure
1. Create `internal/` directory
2. Keep root files (main.go will be created)

### Phase 2: Merge Files
1. Merge `config/` → `internal/config.go`
2. Merge `storage/` → `internal/storage.go`, `internal/sqlite.go`, `internal/postgres.go`
3. Merge `database/` → `internal/database.go`
4. Merge `cli/` → `internal/menu.go`
5. Merge `core/` → `internal/generator.go`, `internal/ui.go`
6. Merge `wallet/` → `internal/wallet.go`
7. Merge `cmd/` → `main.go` (root), `internal/storage.go`, `internal/ui.go`

### Phase 3: Update Imports
1. Update `main.go` to import `evmwalletbot/internal`
2. Update all function calls to use `internal.` prefix
3. Update package declarations to `package internal`

### Phase 4: Cleanup
1. Delete old directories: `cmd/`, `config/`, `cli/`, `core/`, `wallet/`, `storage/`, `database/`
2. Verify build: `go build -o evmwalletbot main.go`
3. Test functionality

---

## Files to Keep (10 files)

### Root Level (6 files)
```
main.go          ← Consolidated entry point
README.md        ← Minimal user guide
LICENSE          ← Legal requirement
.gitignore       ← Git configuration
go.mod           ← Module definition
go.sum           ← Dependency checksums
```

### Internal Package (10 files)
```
internal/
├── config.go      ← Configuration management
├── storage.go     ← Storage interface + factory
├── sqlite.go      ← SQLite implementation
├── postgres.go    ← PostgreSQL implementation
├── database.go    ← Database utilities + migrations
├── menu.go        ← All CLI menus
├── generator.go   ← Core generation logic
├── wallet.go      ← Wallet operations
├── ui.go          ← UI utilities
└── verify.go      ← Verification logic
```

**Total: 16 files in 2 directories**

---

## Files to Delete (11 files)

All original source directories will be deleted after merging:

```
cmd/main.go                    → merged into main.go
cmd/storage_factory.go         → merged into internal/storage.go
cmd/verify.go                  → merged into internal/ui.go
config/config.go               → merged into internal/config.go
cli/menu.go                    → merged into internal/menu.go
cli/hd.go                      → merged into internal/menu.go
cli/import.go                  → merged into internal/menu.go
cli/status.go                  → merged into internal/menu.go
cli/vanity_menu.go             → merged into internal/menu.go
core/generator.go              → merged into internal/generator.go
core/stats.go                  → merged into internal/generator.go
core/vanity.go                 → merged into internal/generator.go
core/progress.go               → merged into internal/ui.go
core/benchmark.go              → merged into internal/generator.go
core/maintenance.go            → merged into internal/generator.go
core/retry.go                  → merged into internal/generator.go
core/colors.go                 → merged into internal/ui.go
core/constants.go              → merged into internal/ui.go
core/box.go                    → merged into internal/ui.go
wallet/generator.go            → merged into internal/wallet.go
wallet/exporter.go             → merged into internal/wallet.go
wallet/hd.go                   → merged into internal/wallet.go
wallet/import.go               → merged into internal/wallet.go
wallet/vanity.go               → merged into internal/wallet.go
storage/interface.go           → merged into internal/storage.go
storage/label.go               → merged into internal/storage.go
storage/sqlite/sqlite.go       → merged into internal/sqlite.go
storage/sqlite/vanity.go       → merged into internal/sqlite.go
storage/sqlite/vanity_resume.go → merged into internal/sqlite.go
storage/postgres/postgres.go   → moved to internal/postgres.go
database/db.go                 → merged into internal/database.go
database/migrations.go         → merged into internal/database.go
```

**Directories to delete:** `cmd/`, `config/`, `cli/`, `core/`, `wallet/`, `storage/`, `database/`

---

## Merge Summary

| Target File | Source Files | Lines (Est.) |
|-------------|--------------|--------------|
| `main.go` | `cmd/main.go` | 150 |
| `internal/config.go` | `config/config.go` | 200 |
| `internal/storage.go` | `cmd/storage_factory.go`, `storage/interface.go`, `storage/label.go` | 150 |
| `internal/sqlite.go` | `storage/sqlite/*.go` (3 files) | 400 |
| `internal/postgres.go` | `storage/postgres/postgres.go` | 300 |
| `internal/database.go` | `database/*.go` (2 files) | 200 |
| `internal/menu.go` | `cli/*.go` (5 files) | 600 |
| `internal/generator.go` | `core/generator.go`, `core/stats.go`, `core/vanity.go`, `core/benchmark.go`, `core/maintenance.go`, `core/retry.go` | 700 |
| `internal/wallet.go` | `wallet/*.go` (5 files) | 500 |
| `internal/ui.go` | `core/colors.go`, `core/box.go`, `core/progress.go`, `core/constants.go`, `cmd/verify.go` | 300 |

**Total:** ~3,500 lines across 10 files (vs 32 files currently)

---

## Updated Startup Commands

### Build
```bash
go build -o evmwalletbot main.go
```

### Run
```bash
./evmwalletbot
```

### With Flags
```bash
./evmwalletbot --count 1000 --export-mode paired --export-dir ./output
```

No changes to user-facing commands!

---

## Minimal README.md

```markdown
# EVM Wallet Generator

High-performance CLI tool for generating EVM-compatible wallets.

## Installation

```bash
go build -o evmwalletbot main.go
```

## Usage

```bash
# Interactive mode
./evmwalletbot

# Generate 1000 wallets
./evmwalletbot --count 1000

# Generate and export
./evmwalletbot --count 1000 --export-mode paired --export-dir ./output
```

## Configuration

Optional environment variables:

- `STORAGE` - `sqlite` (default) or `postgres`
- `WORKERS` - Parallel generators (default: 16)
- `BATCH_SIZE` - Wallets per batch (default: 500)
- `EXPORT_ENABLED` - Enable file export (default: false)
- `EXPORT_MODE` - `paired`, `key-only`, `address-only`, `combined`
- `EXPORT_DIR` - Export directory (default: ./exports)

PostgreSQL mode (optional):
- `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`

## License

See LICENSE file.
```

---

## Impact Analysis

### File Count Reduction
- **Before Cleanup:** 50+ files
- **After Cleanup:** 27 files (46% reduction)
- **After Ultra-Compact:** 16 files (68% reduction from original, 41% from current)

### Directory Depth Reduction
- **Before:** 4 levels (root → storage → sqlite → files)
- **After:** 2 levels (root → internal → files)

### Import Complexity Reduction
- **Before:** 7 different import paths
- **After:** 1 import path (`evmwalletbot/internal`)

### Maintenance Benefits
- Fewer files to navigate
- Single package for all business logic
- Easier to understand code flow
- Simpler testing setup
- Faster builds (fewer packages)

---

## Risks & Considerations

### Potential Issues
1. **Large files** - Some merged files will be 500-700 lines
   - Mitigation: Use clear section comments, logical grouping

2. **Testing** - All code in one package
   - Mitigation: Use `internal` package (still testable)

3. **Code organization** - Less granular separation
   - Mitigation: Use clear function naming, comments

4. **Git history** - File moves lose history
   - Mitigation: Document merge in commit message

### Benefits Outweigh Risks
- ✅ Dramatically simpler structure
- ✅ Easier for users to understand
- ✅ Faster builds
- ✅ Reduced cognitive load
- ✅ Still fully functional
- ✅ Still testable

---

## Verification Checklist

After refactoring:

- [ ] `go build main.go` succeeds
- [ ] `go mod tidy` runs without errors
- [ ] `./evmwalletbot` launches interactive menu
- [ ] `./evmwalletbot --count 5` generates 5 wallets
- [ ] SQLite storage works (default)
- [ ] PostgreSQL storage works (with env vars)
- [ ] Export functionality works
- [ ] Vanity generation works
- [ ] HD wallet generation works
- [ ] Import functionality works

---

## Conclusion

This ultra-compact refactoring reduces the repository from **27 files in 9 directories** to **16 files in 2 directories** while preserving 100% of functionality.

The final structure is:
- ✅ Minimal (16 files total)
- ✅ Flat (2 directory levels max)
- ✅ Idiomatic (follows Go conventions)
- ✅ Maintainable (clear organization)
- ✅ Production-ready (fully functional)

**Recommended:** Proceed with Option B (Single Internal Package) for best balance of compactness and code quality.
