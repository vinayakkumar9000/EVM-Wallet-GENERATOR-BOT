# Comprehensive Fix and Enhancement Plan for EVM Wallet Generator

## Executive Summary

This document outlines a systematic approach to fix compilation errors, improve code quality, add new features, and establish a robust verification process for the EVM wallet generator CLI application.

## Current State Analysis

### Confirmed Issues

1. **Printf Misuse (39 instances)**: Color helper functions (`core.Info`, `core.Error`, `core.Warning`, `core.Success`) accept only a single string but are being used with `fmt.Printf` and `fmt.Sprintf` combinations
2. **Duplicate Function**: `formatNumber` exists in both `cli/status.go:59` and `cli/menu.go:1447`
3. **Documentation Clutter**: ~15 process markdown files in root directory should be in `/docs`
4. **Missing Features**: Plaintext export and enhanced CLI UI/UX

### Architecture Overview

```
evmwalletbot/
├── cmd/main.go           # Entry point
├── cli/                  # Interactive menu system
│   ├── menu.go          # Main menu logic (1703 lines)
│   ├── status.go        # Status display
│   └── menu_test.go     # Menu tests
├── core/                # Core utilities
│   ├── colors.go        # ANSI color helpers (TTY-aware)
│   ├── constants.go     # Constants
│   ├── generator.go     # Wallet generation
│   ├── maintenance.go   # DB maintenance
│   ├── progress.go      # Progress display
│   ├── stats.go         # Statistics
│   ├── vanity.go        # Vanity address search
│   └── retry.go         # Retry logic
├── wallet/              # Wallet operations
│   ├── generator.go     # Core generation
│   ├── vanity.go        # Vanity matching
│   └── exporter.go      # Export functionality
├── database/            # Postgres integration
│   ├── db.go           # Connection pool
│   └── migrations.go   # Schema migrations
├── config/             # Configuration
│   └── config.go       # Config struct
└── events/             # Event system (partially integrated)
    └── events.go
```

## Phase 1: Make It Compile

### 1.1 Fix Color Function Signatures

**Problem**: Color helpers take `string` but are used as printf wrappers.

**Solution**: Make them variadic in `core/colors.go`:

```go
// Before:
func Info(text string) string {
    return Colorize(Cyan, text)
}

// After:
func Info(format string, a ...any) string {
    if len(a) == 0 {
        return Colorize(Cyan, format)
    }
    return Colorize(Cyan, fmt.Sprintf(format, a...))
}
```

Apply to: `Info`, `Error`, `Warning`, `Success`, `Hint`, `Highlight`

**Call Site Updates** (39 instances in cli/menu.go):
- Change `fmt.Printf(core.Info(fmt.Sprintf(...)))` → `fmt.Print(core.Info(...))`
- Change `fmt.Printf(core.Error(fmt.Sprintf(...)))` → `fmt.Print(core.Error(...))`
- Similar for Warning, Success

**Affected Lines** (from search results):
- Lines: 124, 473, 480, 507, 511, 537, 542, 557, 588, and others

### 1.2 Consolidate Duplicate Functions

**formatNumber Duplication**:
- Keep: `cli/status.go:59` (package-level, single source)
- Delete: `cli/menu.go:1447`
- Update: All callers in menu.go to use the status.go version

**Verification**: Search for other duplicates:
- `spinnerFrames` (should be consolidated in core/)
- Any other utility functions

### 1.3 Scan for Split Identifiers

**Method**: Search patterns:
- Identifiers ending at line boundary: `\w+$` followed by continuation
- String literals split across lines
- Function calls with arguments on next line without proper continuation

**Note**: Initial investigation shows no actual split `ctx` identifier in current code. The issue may have been fixed or is in a different location.

### 1.4 Verification Loop

Create `scripts/verify.sh`:

```bash
#!/bin/bash
set -euo pipefail

echo "=== Running verification loop ==="

echo "1. Checking formatting..."
UNFORMATTED=$(gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
    echo "ERROR: Unformatted files:"
    echo "$UNFORMATTED"
    exit 1
fi
echo "✓ All files formatted"

echo "2. Building..."
go build ./... || exit 1
echo "✓ Build successful"

echo "3. Running go vet..."
go vet ./... || exit 1
echo "✓ Vet passed"

echo "4. Checking for carriage returns..."
if grep -rl $'\r' --include='*.go' . 2>/dev/null; then
    echo "ERROR: Found carriage returns in .go files"
    exit 1
fi
echo "✓ No carriage returns"

echo "5. Running tests with race detector..."
go test ./... -race -count=1 || exit 1
echo "✓ All tests passed"

echo ""
echo "=== ✓ ALL CHECKS PASSED ==="
```

Windows version `scripts/verify.bat`:

```batch
@echo off
setlocal enabledelayedexpansion

echo === Running verification loop ===

echo 1. Checking formatting...
gofmt -l . > temp_fmt.txt
set /p UNFORMATTED=<temp_fmt.txt
if not "!UNFORMATTED!"=="" (
    echo ERROR: Unformatted files:
    type temp_fmt.txt
    del temp_fmt.txt
    exit /b 1
)
del temp_fmt.txt
echo ✓ All files formatted

echo 2. Building...
go build ./...
if errorlevel 1 exit /b 1
echo ✓ Build successful

echo 3. Running go vet...
go vet ./...
if errorlevel 1 exit /b 1
echo ✓ Vet passed

echo 4. Running tests with race detector...
go test ./... -race -count=1
if errorlevel 1 exit /b 1
echo ✓ All tests passed

echo.
echo === ✓ ALL CHECKS PASSED ===
```

## Phase 2: Full Quality Pass

### 2.1 Code Quality Improvements

#### Eliminate Duplicates and Dead Code

**Events Package Audit**:
- Current state: Defined but not fully integrated
- Decision tree:
  1. If used in generation flow → Complete integration
  2. If not used → Delete package
  3. If partially used → Document why and complete or remove

**Key Generation Consolidation**:
- Analyze `wallet.Generate` vs `wallet.GenerateInto`
- Benchmark both approaches
- Keep the more efficient one or merge if complementary
- Document the decision

#### Error Handling Audit

**Ignored Errors to Fix**:
```go
// Pattern to search:
_ = tx.Rollback()
_ = rows.Close()
_ = file.Close()
_ = rows.Err()
```

**Fix Pattern**:
```go
// Before:
defer tx.Rollback()

// After:
defer func() {
    if err := tx.Rollback(); err != nil && err != pgx.ErrTxClosed {
        log.Printf("rollback error: %v", err)
    }
}()
```

#### Concurrency Safety

**Race Detector Workflow**:
1. Run `go test -race ./...`
2. Document each race condition found
3. Fix with proper synchronization (mutex, channels, atomic)
4. Re-run until clean

**Graceful Shutdown**:
- Ensure all goroutines respect context cancellation
- Close Postgres pool cleanly
- Flush any buffered writers
- No leaked goroutines (verify with pprof if needed)

#### Input Validation

**Validation Points**:
- Wallet count: positive integer, reasonable max (e.g., 1M)
- Worker count: 1 to runtime.NumCPU()*2
- Hex patterns: valid hex chars, proper length
- File paths: valid, writable, no path traversal
- Batch sizes: positive, reasonable

**Validation Pattern**:
```go
func validateWalletCount(input string) (int, error) {
    count, err := strconv.Atoi(input)
    if err != nil {
        return 0, fmt.Errorf("invalid number: %w", err)
    }
    if count <= 0 {
        return 0, fmt.Errorf("count must be positive")
    }
    if count > 10_000_000 {
        return 0, fmt.Errorf("count too large (max 10M)")
    }
    return count, nil
}
```

### 2.2 Testing Strategy

#### Unit Tests to Add/Expand

1. **Vanity Matching** (`wallet/vanity_test.go`):
   - Test pattern matching (prefix, suffix, contains)
   - Test difficulty calculation
   - Test case sensitivity
   - Edge cases (empty pattern, invalid hex)

2. **Difficulty/Time Math** (`core/vanity_test.go`):
   - Verify difficulty formula: `16^n` for n-char pattern
   - Test time estimation accuracy
   - Test with various worker counts

3. **Config Parsing** (`config/config_test.go`):
   - Test loading from .env
   - Test defaults
   - Test validation
   - Test invalid values

4. **Plaintext Exporter** (`wallet/exporter_test.go`):
   - Test all export modes
   - Test file creation/append
   - Test EIP-55 checksum
   - Test paired file synchronization

#### Integration Tests

Create `cli/menu_integration_test.go`:
- Test each menu action with mocked input
- Verify no panics
- Verify correct output format
- Use ephemeral Postgres for DB operations

### 2.3 Repository Hygiene

**Move Process Docs to /docs**:
```
Root → /docs:
- FIXES_APPLIED.md
- FIXES_PLAN.md
- PHASE1_CHANGES.md
- PHASE2_CHANGES.md
- PHASE3_CHANGES.md
- CLI_MENU_FIX_PLAN.md
- CLI_REDESIGN_IMPLEMENTATION_PLAN.md
- COMPREHENSIVE_IMPLEMENTATION_PLAN.md
- IMPLEMENTATION_SUMMARY.md
- IMPROVEMENTS_ROADMAP.md
- MENU_ENHANCEMENT_PLAN.md
- TIER2_IMPLEMENTATION_SUMMARY.md
- VANITY_IMPLEMENTATION_PLAN.md
- FINAL_SUMMARY.md
- CLEANUP_PLAN.md
```

Keep in root:
- README.md
- LICENSE
- .gitignore
- .env.example

## Phase 3: Plaintext Export Feature

### 3.1 Configuration Structure

Add to `config/config.go`:

```go
type ExportConfig struct {
    Enabled       bool   `env:"EXPORT_ENABLED" envDefault:"false"`
    Mode          string `env:"EXPORT_MODE" envDefault:"paired"`
    OutputDir     string `env:"EXPORT_DIR" envDefault:"./exports"`
    Overwrite     bool   `env:"EXPORT_OVERWRITE" envDefault:"false"`
    AddressPrefix bool   `env:"EXPORT_0X_PREFIX" envDefault:"true"`
    EIP55Checksum bool   `env:"EXPORT_EIP55" envDefault:"true"`
}

type Config struct {
    // ... existing fields ...
    Export ExportConfig
}
```

### 3.2 Export Modes

**Mode Definitions**:

1. **Paired** (default):
   - `addresses.txt`: One address per line
   - `private_keys.txt`: One key per line (same order)
   - Synchronized writes (buffered)

2. **Key-Only**:
   - `private_keys.txt`: One key per line

3. **Address-Only**:
   - `addresses.txt`: One address per line

4. **Combined CSV**:
   - `wallets.csv`: Header + rows of `address,private_key`

### 3.3 Implementation

Create `wallet/exporter.go`:

```go
package wallet

import (
    "bufio"
    "encoding/csv"
    "fmt"
    "os"
    "path/filepath"
    "sync"
)

type Exporter struct {
    mode          string
    outputDir     string
    overwrite     bool
    addressPrefix bool
    eip55         bool
    
    addressFile   *os.File
    keyFile       *os.File
    csvWriter     *csv.Writer
    
    addressWriter *bufio.Writer
    keyWriter     *bufio.Writer
    
    mu            sync.Mutex
}

func NewExporter(cfg ExportConfig) (*Exporter, error) {
    // Create output directory
    // Open files based on mode
    // Initialize buffered writers
    // Return exporter
}

func (e *Exporter) Export(address, privateKey string) error {
    e.mu.Lock()
    defer e.mu.Unlock()
    
    // Format address (EIP-55, 0x prefix)
    // Write based on mode
    // Return error if any
}

func (e *Exporter) Flush() error {
    // Flush all buffered writers
}

func (e *Exporter) Close() error {
    // Flush and close all files
}
```

### 3.4 Integration Points

**In Generation Loop** (`core/generator.go`):
```go
if cfg.Export.Enabled {
    if err := exporter.Export(address, privateKey); err != nil {
        // Log error but continue generation
    }
}
```

**Menu Integration** (`cli/menu.go`):
- Add export configuration submenu
- Toggle enable/disable
- Select mode
- Configure options
- Show export status in completion summary

### 3.5 Verification Test

Create `wallet/exporter_test.go`:

```go
func TestExportVerification(t *testing.T) {
    // Generate test wallet
    // Export to temp directory
    // Read back address and key
    // Derive address from private key
    // Verify EIP-55 checksum matches
    // Verify address matches original
}
```

## Phase 4: CLI UI/UX Polish

### 4.1 Live Progress Display

**Components**:
- Spinner animation (rotating chars)
- Progress bar (visual representation)
- Percentage complete
- Current speed (wallets/sec)
- Peak speed
- ETA (estimated time remaining)

**Implementation** (`core/progress.go`):

```go
type ProgressDisplay struct {
    total       int64
    current     int64
    startTime   time.Time
    lastUpdate  time.Time
    peakSpeed   float64
    spinnerIdx  int
    
    mu          sync.Mutex
}

func (p *ProgressDisplay) Update(current int64) {
    p.mu.Lock()
    defer p.mu.Unlock()
    
    p.current = current
    elapsed := time.Since(p.startTime).Seconds()
    speed := float64(current) / elapsed
    
    if speed > p.peakSpeed {
        p.peakSpeed = speed
    }
    
    // Calculate ETA
    remaining := p.total - current
    eta := time.Duration(float64(remaining)/speed) * time.Second
    
    // Render progress line with \r (carriage return)
    fmt.Printf("\r%s [%s] %d%% | %s/s | Peak: %s/s | ETA: %s",
        spinner[p.spinnerIdx],
        progressBar(current, total),
        percentage,
        formatSpeed(speed),
        formatSpeed(p.peakSpeed),
        formatDuration(eta))
    
    p.spinnerIdx = (p.spinnerIdx + 1) % len(spinner)
}

func (p *ProgressDisplay) Finish() {
    fmt.Println() // New line after progress
}
```

### 4.2 Color Hierarchy

**Already Implemented** in `core/colors.go`:
- TTY detection via `golang.org/x/term`
- NO_COLOR environment variable support
- Automatic fallback to plain text when piped

**Enhancement**: Add `--no-color` flag:

```go
// In cmd/main.go
var noColor bool
flag.BoolVar(&noColor, "no-color", false, "Disable colored output")

if noColor {
    os.Setenv("NO_COLOR", "1")
}
```

### 4.3 Menu Improvements

**Current Issues**:
- Deep nesting (main → submenu → sub-submenu)
- No keyboard shortcuts
- Full menu reprint on invalid input

**Improvements**:

1. **Flatten Structure**:
   ```
   Main Menu:
   1. Generate Wallets
   2. Vanity Search
   3. View Statistics
   4. Lookup Wallet
   5. Database Tools
   6. Configuration
   7. Help
   8. Exit
   
   (Remove monitoring, benchmark submenus - integrate into main)
   ```

2. **Letter Shortcuts**:
   ```
   [G]enerate, [V]anity, [S]tats, [L]ookup, [D]atabase, [C]onfig, [H]elp, [Q]uit
   ```

3. **Inline Validation**:
   ```go
   func promptWithValidation(prompt string, validator func(string) error) string {
       for {
           fmt.Print(prompt)
           input := readLine()
           if err := validator(input); err != nil {
               fmt.Printf(core.Error("✗ %s\n"), err)
               continue // Don't reprint menu
           }
           return input
       }
   }
   ```

### 4.4 Status Strip

**Implementation** (`cli/status.go`):

```go
type StatusStrip struct {
    WalletCount    int64
    OutputTarget   string
    LastRunSpeed   float64
    LastRunTime    time.Time
}

func (s *StatusStrip) Render() {
    if !core.IsColorEnabled() {
        return // Skip in non-TTY
    }
    
    fmt.Printf(core.Dim("─────────────────────────────────────────\n"))
    fmt.Printf(core.Dim("Wallets: %s | Target: %s | Last: %s @ %s/s\n"),
        core.FormatNumber(s.WalletCount),
        s.OutputTarget,
        s.LastRunTime.Format("15:04"),
        formatSpeed(s.LastRunSpeed))
    fmt.Printf(core.Dim("─────────────────────────────────────────\n"))
}
```

### 4.5 Completion Summary

**Display After Generation**:

```
╔════════════════════════════════════════╗
║       Generation Complete! ✓           ║
╠════════════════════════════════════════╣
║ Wallets Generated: 10,000              ║
║ Time Elapsed:      2m 34s              ║
║ Average Speed:     65.4 wallets/sec    ║
║ Peak Speed:        89.2 wallets/sec    ║
║                                        ║
║ Saved to Database: ✓                   ║
║ Exported to Files: ✓                   ║
║   - addresses.txt                      ║
║   - private_keys.txt                   ║
╚════════════════════════════════════════╝
```

### 4.6 Non-Interactive Mode

**Flag Support**:
```bash
# Generate 1000 wallets non-interactively
./evmwalletbot --generate 1000 --no-interactive

# Vanity search
./evmwalletbot --vanity "dead" --workers 8 --no-interactive
```

**Implementation**:
```go
var (
    generateCount int
    vanityPattern string
    noInteractive bool
)

flag.IntVar(&generateCount, "generate", 0, "Generate N wallets")
flag.StringVar(&vanityPattern, "vanity", "", "Search for vanity pattern")
flag.BoolVar(&noInteractive, "no-interactive", false, "Non-interactive mode")

if noInteractive {
    if generateCount > 0 {
        runGeneration(generateCount)
        os.Exit(0)
    }
    if vanityPattern != "" {
        runVanitySearch(vanityPattern)
        os.Exit(0)
    }
}
```

## Phase 5: Verification & Documentation

### 5.1 Ephemeral Postgres Setup

**Docker Approach** (add to `scripts/verify.sh`):

```bash
# Start ephemeral Postgres
echo "Starting test database..."
CONTAINER_ID=$(docker run -d --rm \
    -e POSTGRES_PASSWORD=test \
    -e POSTGRES_DB=walletdb \
    -p 5432:5432 \
    postgres:16)

# Wait for ready
sleep 3

# Export connection string for tests
export DATABASE_URL="postgres://postgres:test@localhost:5432/walletdb?sslmode=disable"

# Run migrations
go run cmd/main.go --migrate || {
    docker stop $CONTAINER_ID
    exit 1
}

# Run tests
go test ./... -race -count=1 || {
    docker stop $CONTAINER_ID
    exit 1
}

# Cleanup
docker stop $CONTAINER_ID
```

**Alternative: testcontainers-go**:

```go
// In database/db_test.go
func setupTestDB(t *testing.T) *pgxpool.Pool {
    ctx := context.Background()
    
    req := testcontainers.ContainerRequest{
        Image:        "postgres:16",
        ExposedPorts: []string{"5432/tcp"},
        Env: map[string]string{
            "POSTGRES_PASSWORD": "test",
            "POSTGRES_DB":       "walletdb",
        },
        WaitingFor: wait.ForLog("database system is ready"),
    }
    
    container, err := testcontainers.GenericContainer(ctx, req)
    require.NoError(t, err)
    
    t.Cleanup(func() {
        container.Terminate(ctx)
    })
    
    // Get connection string and return pool
}
```

### 5.2 README Updates

**Sections to Add/Update**:

1. **Quick Start**:
   ```markdown
   ## Quick Start
   
   ```bash
   # Clone repository
   git clone https://github.com/vinayakkumar9000/evm-wallet-ai
   cd evm-wallet-ai
   
   # Set up environment
   cp .env.example .env
   # Edit .env with your Postgres credentials
   
   # Build
   go build -o evmwalletbot cmd/main.go
   
   # Run
   ./evmwalletbot
   ```
   ```

2. **Plaintext Export**:
   ```markdown
   ## Plaintext Export
   
   Export wallets to text files alongside database storage:
   
   ```bash
   # Enable in .env
   EXPORT_ENABLED=true
   EXPORT_MODE=paired  # paired, key-only, address-only, combined
   EXPORT_DIR=./exports
   EXPORT_0X_PREFIX=true
   EXPORT_EIP55=true
   ```
   
   **Modes**:
   - `paired`: Separate files for addresses and keys (line-synced)
   - `key-only`: Private keys only
   - `address-only`: Addresses only
   - `combined`: CSV with both columns
   ```

3. **Vanity Search**:
   ```markdown
   ## Vanity Address Search
   
   Find addresses matching specific patterns:
   
   ```bash
   # Interactive
   ./evmwalletbot
   # Select option 2 (Vanity Search)
   
   # Non-interactive
   ./evmwalletbot --vanity "dead" --workers 8
   ```
   
   **Difficulty Formula**:
   - Prefix/suffix: `16^n` where n = pattern length
   - Contains: `16^(n-1)` (easier)
   - Case-sensitive: `22^n` (harder)
   
   **Example**: Finding "dead" prefix takes ~65,536 attempts on average
   ```

4. **Flags**:
   ```markdown
   ## Command-Line Flags
   
   ```
   --generate N          Generate N wallets non-interactively
   --vanity PATTERN      Search for vanity pattern
   --workers N           Number of worker goroutines (default: CPU count)
   --no-interactive      Non-interactive mode (for scripts/CI)
   --no-color            Disable colored output
   --migrate             Run database migrations and exit
   ```
   ```

### 5.3 Commit Strategy

**Commit Series** (each must build + test clean):

1. `fix: make color functions variadic and update call sites`
2. `refactor: consolidate duplicate formatNumber function`
3. `chore: move process docs to /docs directory`
4. `fix: improve error handling (tx.Rollback, rows.Err, file.Close)`
5. `fix: add input validation for all user inputs`
6. `test: add unit tests for vanity matching and difficulty calculation`
7. `test: add config parsing tests`
8. `feat: add plaintext export with multiple modes`
9. `test: add exporter verification tests`
10. `feat: implement live progress display with spinner and ETA`
11. `feat: add --no-color flag and improve TTY detection`
12. `refactor: flatten menu structure and add keyboard shortcuts`
13. `feat: add completion summary panel`
14. `feat: add non-interactive mode with flags`
15. `docs: update README with new features and usage examples`
16. `chore: add verification scripts for CI/CD`

### 5.4 PR Summary Template

```markdown
# EVM Wallet Generator: Comprehensive Fixes and Enhancements

## Summary

This PR fixes all compilation errors, improves code quality, adds plaintext export, and polishes the CLI UI/UX. All changes have been verified through automated testing.

## Bugs Fixed

### Major
- **Printf Misuse (39 instances)**: Made color helper functions variadic to properly support formatted output
- **Duplicate Functions**: Consolidated `formatNumber` into single source of truth
- **Error Handling**: Fixed ignored errors in transaction rollbacks, row iteration, and file operations
- **Race Conditions**: Fixed data races detected by `go test -race`

### Minor
- Input validation for all user-provided values
- Graceful shutdown on Ctrl+C with proper cleanup
- Memory leaks in goroutine management

## Refactorings

- **Events Package**: [Completed integration / Removed as unused]
- **Key Generation**: Consolidated Generate/GenerateInto into single optimized path
- **Menu Structure**: Flattened deep nesting, added keyboard shortcuts
- **Repository Hygiene**: Moved 15 process docs to /docs directory

## New Features

### 1. Plaintext Export
- Four export modes: paired, key-only, address-only, combined CSV
- Configurable output directory, overwrite/append, 0x prefix, EIP-55 checksum
- Works with or without database storage
- Verified: addresses derive correctly from private keys

### 2. Enhanced CLI UI/UX
- Live progress display: spinner + bar + %/speed/peak/ETA
- Color hierarchy with TTY detection and NO_COLOR support
- Status strip showing wallet count, target, last run stats
- Completion summary panel with detailed statistics
- Non-interactive mode for scripts/CI (`--generate`, `--vanity`, `--no-interactive`)

## Verification Output

```
=== Running verification loop ===
1. Checking formatting...
✓ All files formatted

2. Building...
✓ Build successful

3. Running go vet...
✓ Vet passed

4. Checking for carriage returns...
✓ No carriage returns

5. Running tests with race detector...
✓ All tests passed (23 tests, 0 failures)

=== ✓ ALL CHECKS PASSED ===
```

**Verification run 3 times consecutively: ALL PASSED**

## Testing

- Unit tests: 23 tests covering vanity matching, difficulty calculation, config parsing, exporter
- Integration tests: All menu actions tested end-to-end
- Race detector: Clean (no data races)
- Manual testing: Each menu option exercised with various inputs

## Documentation

- README updated with build instructions, plaintext export usage, vanity search guide
- All process docs moved to /docs
- Code comments added for exported functions
- Difficulty formula documented

## Breaking Changes

None. All changes are backward compatible.

## Migration Guide

No migration needed. Existing databases and configurations work as-is.
New features are opt-in via configuration.
```

## Definition of Done Checklist

- [ ] `gofmt -l .` returns empty
- [ ] `go build ./...` exits 0 with no output
- [ ] `go vet ./...` exits 0
- [ ] `go test ./... -race -count=1` all pass against ephemeral Postgres
- [ ] No .go file contains carriage return (\r)
- [ ] Every main-menu action runs without panic
- [ ] Plaintext export works in all modes
- [ ] Addresses verified against private keys (EIP-55)
- [ ] UI/UX: live progress + colors in TTY, plain when piped
- [ ] Repo root has only README.md + LICENSE among .md files
- [ ] README updated with all new features
- [ ] Each commit builds + tests clean
- [ ] Verification script runs clean 3 consecutive times

## Timeline Estimate

- Phase 1 (Compilation): 2-3 hours
- Phase 2 (Quality): 4-6 hours
- Phase 3 (Export): 3-4 hours
- Phase 4 (UI/UX): 4-5 hours
- Phase 5 (Verification): 2-3 hours

**Total**: 15-21 hours of focused development

## Risk Mitigation

1. **Database Tests**: Use ephemeral Postgres to avoid state pollution
2. **Backward Compatibility**: All new features opt-in via config
3. **Incremental Commits**: Each commit builds and tests clean
4. **Verification Loop**: Automated checks prevent regressions
5. **Manual Testing**: Exercise each menu path before declaring done

## Next Steps

1. Review and approve this plan
2. Switch to `code` or `advanced` mode for implementation
3. Execute phases sequentially
4. Run verification loop after each phase
5. Open PR with summary and verification output
