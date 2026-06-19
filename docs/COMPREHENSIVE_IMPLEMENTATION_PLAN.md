# Comprehensive Implementation Plan - EVM Wallet AI

## Status: Phase 1 Complete ✓

### Phase 1: Compilation Fixes - COMPLETED
- ✓ Fixed broken `ctx` parameter at line 1149-1150
- ✓ Fixed printf misuse in core.Info/Error/Warning calls
- ✓ Removed duplicate formatNumber function
- ✓ All files formatted with gofmt
- ✓ `go build ./...` - exits 0
- ✓ `go vet ./...` - exits 0
- ✓ `go test ./...` - all tests pass
- ✓ No bare carriage returns in .go files

---

## Phase 2: Full Quality Pass

### 2.1 Create Self-Verification Script
**Priority: CRITICAL - Must be done first**

Create `scripts/verify.sh`:
```bash
#!/bin/bash
set -euo pipefail

echo "=== Running Full Verification Loop ==="
echo ""

echo "1. Checking code formatting..."
UNFORMATTED=$(gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
    echo "ERROR: The following files are not formatted:"
    echo "$UNFORMATTED"
    exit 1
fi
echo "✓ All files properly formatted"

echo ""
echo "2. Building all packages..."
go build ./...
echo "✓ Build successful"

echo ""
echo "3. Running go vet..."
go vet ./...
echo "✓ Vet passed"

echo ""
echo "4. Checking for carriage returns in .go files..."
if grep -rl $'\r' --include='*.go' . 2>/dev/null; then
    echo "ERROR: Found carriage returns in .go files"
    exit 1
fi
echo "✓ No carriage returns found"

echo ""
echo "5. Running tests with race detector..."
# Set up ephemeral Postgres for tests
export TEST_DB_HOST=localhost
export TEST_DB_PORT=5432
export TEST_DB_NAME=walletdb_test
export TEST_DB_USER=postgres
export TEST_DB_PASSWORD=test

# Start ephemeral Postgres (if not already running)
if ! docker ps | grep -q postgres-test; then
    echo "Starting ephemeral Postgres..."
    docker run --rm -d \
        --name postgres-test \
        -e POSTGRES_PASSWORD=test \
        -e POSTGRES_DB=walletdb_test \
        -p 5432:5432 \
        postgres:16
    sleep 3
fi

# Run tests
go test ./... -race -count=1

# Cleanup
docker stop postgres-test 2>/dev/null || true

echo ""
echo "=== ✓ ALL VERIFICATION CHECKS PASSED ==="
```

**Tasks:**
- [ ] Create scripts/ directory
- [ ] Write verify.sh script
- [ ] Make it executable (chmod +x)
- [ ] Test the script end-to-end
- [ ] Add Docker fallback for systems without Docker

### 2.2 Eliminate Duplicate Code
**Files to audit:**
- [ ] Check `events` package - either fully integrate or delete
- [ ] Verify no duplicate spinnerFrames/formatNumber in core
- [ ] Search for any other duplicate functions across packages
- [ ] Consolidate Generate vs GenerateInto key-gen paths

**Implementation:**
```bash
# Search for duplicate function signatures
rg "^func \w+\(" --no-heading | sort | uniq -d
```

### 2.3 Error Handling Audit
**Critical areas:**
- [ ] Database operations: tx.Rollback, rows.Err, rows.Close
- [ ] File operations: file.Close, writer.Flush
- [ ] Context cancellation: proper cleanup on Ctrl+C
- [ ] No naked panics in normal flow (only for truly unrecoverable errors)

**Pattern to apply:**
```go
// Before
tx.Rollback()

// After
if err := tx.Rollback(); err != nil {
    log.Printf("rollback failed: %v", err)
}
```

### 2.4 Concurrency & Race Conditions
**Tasks:**
- [ ] Run `go test -race` on all packages
- [ ] Fix any data races found
- [ ] Verify goroutine cleanup on context cancellation
- [ ] Test Postgres pool shutdown (no connection leaks)
- [ ] Add context timeout tests

**Test scenarios:**
1. Generate wallets, Ctrl+C mid-run → clean shutdown
2. Multiple concurrent generations → no races
3. Pool exhaustion → graceful degradation

### 2.5 Input Validation
**All user input points:**
- [ ] Wallet count (positive integer, reasonable max)
- [ ] Batch size (1-1000, PostgreSQL COPY limit)
- [ ] Worker count (1-100, CPU-aware recommendation)
- [ ] Vanity patterns (hex validation, length limits)
- [ ] Wallet ID lookup (positive integer)
- [ ] Configuration values (ranges, types)

**Pattern:**
```go
func validateWalletCount(input string) (int, error) {
    count, err := strconv.Atoi(input)
    if err != nil {
        return 0, fmt.Errorf("invalid number: %w", err)
    }
    if count < 1 {
        return 0, fmt.Errorf("count must be positive")
    }
    if count > 100_000_000 {
        return 0, fmt.Errorf("count too large (max 100M)")
    }
    return count, nil
}
```

### 2.6 Manual Menu Testing
**Test each menu path:**
- [ ] 1. Generate wallets (count mode)
- [ ] 1. Generate wallets (batch mode)
- [ ] 1. Generate wallets (settings)
- [ ] 2. Statistics (view)
- [ ] 2. Statistics (watch live)
- [ ] 2. Statistics (database size)
- [ ] 3. Wallet lookup (valid ID)
- [ ] 3. Wallet lookup (invalid ID)
- [ ] 4. Database & monitoring (all 7 options)
- [ ] 5. Benchmark (all 4 options)
- [ ] 6. Configuration (all 9 options)
- [ ] 8. Help (all 5 pages)
- [ ] 9. Vanity address (prefix only)
- [ ] 9. Vanity address (suffix only)
- [ ] 9. Vanity address (both)
- [ ] 9. Vanity address (checksum mode)
- [ ] 0. Exit

**Create test harness:**
```bash
# scripts/test_menu.sh
echo "1" | go run cmd/main.go  # Test generate menu
echo "2" | go run cmd/main.go  # Test stats menu
# etc.
```

### 2.7 Code Quality
**Tools to run:**
- [ ] `gofmt -w .` (already done)
- [ ] `go vet ./...` (already done)
- [ ] `golangci-lint run` (if available)
- [ ] `staticcheck ./...` (if available)

**Manual review:**
- [ ] All exported functions have doc comments
- [ ] Package-level doc comments present
- [ ] Clear, descriptive variable names
- [ ] No magic numbers (use named constants)
- [ ] Consistent error message format

### 2.8 Repository Hygiene
**Move to /docs:**
- [ ] CLEANUP_PLAN.md
- [ ] CLI_REDESIGN_IMPLEMENTATION_PLAN.md
- [ ] FINAL_SUMMARY.md
- [ ] FIXES_APPLIED.md
- [ ] FIXES_PLAN.md
- [ ] IMPLEMENTATION_SUMMARY.md
- [ ] IMPROVEMENTS_ROADMAP.md
- [ ] MENU_ENHANCEMENT_PLAN.md
- [ ] PHASE1_CHANGES.md
- [ ] PHASE2_CHANGES.md
- [ ] PHASE3_CHANGES.md
- [ ] TIER2_IMPLEMENTATION_SUMMARY.md
- [ ] VANITY_IMPLEMENTATION_PLAN.md
- [ ] CLI_MENU_FIX_PLAN.md (just created)
- [ ] COMPREHENSIVE_IMPLEMENTATION_PLAN.md (this file)

**Keep in root:**
- README.md
- LICENSE
- .gitignore
- .env.example

### 2.9 Test Coverage
**Add/expand tests for:**
- [ ] Vanity pattern matching (prefix, suffix, both)
- [ ] Vanity difficulty calculation
- [ ] Vanity time estimation
- [ ] Config parsing and validation
- [ ] Plaintext exporter (Phase 3)
- [ ] Progress bar rendering
- [ ] Color output gating (TTY detection)

**Target coverage:**
- Core logic: 80%+
- Wallet generation: 90%+
- Vanity matching: 95%+

---

## Phase 3: Plaintext Export Feature

### 3.1 Configuration Schema
**Add to config/config.go:**
```go
type ExportConfig struct {
    Enabled       bool   `env:"EXPORT_ENABLED" envDefault:"false"`
    Mode          string `env:"EXPORT_MODE" envDefault:"paired"`
    OutputDir     string `env:"EXPORT_DIR" envDefault:"./exports"`
    Overwrite     bool   `env:"EXPORT_OVERWRITE" envDefault:"false"`
    AddressPrefix bool   `env:"EXPORT_ADDRESS_PREFIX" envDefault:"true"`
    KeyPrefix     bool   `env:"EXPORT_KEY_PREFIX" envDefault:"true"`
    UseChecksum   bool   `env:"EXPORT_USE_CHECKSUM" envDefault:"true"`
}

// Modes: "paired", "key-only", "address-only", "combined"
```

### 3.2 Exporter Implementation
**Create wallet/exporter.go:**
```go
type Exporter struct {
    config      ExportConfig
    addressFile *os.File
    keyFile     *os.File
    csvFile     *os.File
    writer      *bufio.Writer
    mu          sync.Mutex
}

func NewExporter(cfg ExportConfig) (*Exporter, error)
func (e *Exporter) Export(address, privateKey string) error
func (e *Exporter) Flush() error
func (e *Exporter) Close() error
```

**Features:**
- [ ] Buffered writes (flush every 1000 wallets or on signal)
- [ ] Atomic file operations (write to .tmp, rename on success)
- [ ] Line-for-line sync for paired mode
- [ ] EIP-55 checksum for addresses
- [ ] Optional 0x prefix
- [ ] CSV header for combined mode

### 3.3 Integration Points
**Modify core/generator.go:**
- [ ] Add optional Exporter parameter
- [ ] Call exporter.Export() after DB insert
- [ ] Handle exporter errors gracefully
- [ ] Flush on completion and Ctrl+C

**Menu integration:**
- [ ] Add export toggle to Configuration menu
- [ ] Show export status in status strip
- [ ] Display export file paths in completion summary

### 3.4 Verification Tests
**Create wallet/exporter_test.go:**
```go
func TestExportPaired(t *testing.T)
func TestExportKeyOnly(t *testing.T)
func TestExportAddressOnly(t *testing.T)
func TestExportCombined(t *testing.T)
func TestExportVerifyChecksum(t *testing.T)
func TestExportAtomicWrite(t *testing.T)
```

**Cross-verification:**
- [ ] Read exported private key
- [ ] Derive address from key using go-ethereum
- [ ] Compare with exported address
- [ ] Verify EIP-55 checksum

### 3.5 Documentation
**Update README.md:**
- [ ] Export configuration options
- [ ] Export modes explanation
- [ ] File format examples
- [ ] Security warnings (private key handling)

---

## Phase 4: CLI UI/UX Polish

### 4.1 Live Progress Display
**Enhance core/progress.go:**
```go
type ProgressBar struct {
    total       int
    current     int
    startTime   time.Time
    lastUpdate  time.Time
    spinner     *Spinner
    peakRate    float64
    mu          sync.Mutex
}

func (p *ProgressBar) Update(current int)
func (p *ProgressBar) Render() string
func (p *ProgressBar) Complete()
```

**Display format:**
```
[⠋] Generating wallets... ████████████░░░░░░░░ 60% (60,000/100,000)
    Speed: 8,234 w/s | Peak: 9,102 w/s | ETA: 4.8s
```

**Features:**
- [ ] Spinner animation (8 frames)
- [ ] Progress bar (20 chars)
- [ ] Percentage
- [ ] Current/total count
- [ ] Current speed (rolling average)
- [ ] Peak speed
- [ ] ETA calculation
- [ ] Redraw in place with \r (no scrolling)

### 4.2 Vanity Progress
**Enhance core/vanity.go:**
```go
type VanityProgress struct {
    tried       uint64
    found       int
    target      int
    startTime   time.Time
    hitRate     float64
    difficulty  float64
}

func (v *VanityProgress) Render() string
```

**Display format:**
```
[⠙] Searching for vanity address... Found: 3/10
    Tried: 1,234,567 | Hit rate: 0.00024% | Difficulty: 1:416,667
    Speed: 45,678 tries/s | ETA: 3m 12s
```

### 4.3 Color Hierarchy
**Already implemented in core/colors.go, verify:**
- [ ] Success (green): completions, found items
- [ ] Info (cyan): status messages
- [ ] Warning (yellow): non-critical issues
- [ ] Error (red): failures
- [ ] Hint (dim gray): secondary info
- [ ] Highlight (bold): emphasis

**TTY detection:**
- [ ] Check `term.IsTerminal(os.Stdout.Fd())`
- [ ] Honor NO_COLOR environment variable
- [ ] Add --no-color flag
- [ ] Fallback to plain text when piped

### 4.4 Status Strip
**Enhance cli/status.go:**
```go
type StatusStrip struct {
    walletCount    int64
    lastRunSpeed   float64
    lastRunTime    time.Time
    exportEnabled  bool
    exportPath     string
}

func (s *StatusStrip) Render()
```

**Display format:**
```
┌────────────────────────────────────────────────────────┐
│ Wallets: 1,234,567 | Last run: 8,234 w/s (2m ago)      │
│ Export: enabled → ./exports/                           │
└────────────────────────────────────────────────────────┘
```

### 4.5 Menu Flattening
**Already done, verify:**
- [ ] Main menu: 9 options (no deep nesting)
- [ ] Letter shortcuts work (G, S, L, D, B, C, H, V, Q)
- [ ] Inline validation (no menu reprint on error)
- [ ] Friendly error messages

### 4.6 Completion Summary
**Enhance showCompletionSummary():**
```
╔══════════════════════════════════════════════════════════════╗
║                    GENERATION COMPLETE                       ║
╠══════════════════════════════════════════════════════════════╣
║                                                              ║
║  ✓ Successfully generated and stored all wallets             ║
║                                                              ║
║  Summary:                                                    ║
║    Wallets generated : 100,000                               ║
║    Time elapsed      : 12.3s                                 ║
║    Average speed     : 8,130 wallets/sec                     ║
║    Peak speed        : 9,102 wallets/sec                     ║
║    Workers used      : 16                                    ║
║    Batch size        : 500                                   ║
║                                                              ║
║  Storage:                                                    ║
║    Database          : walletdb (100,000 rows)               ║
║    Export files      : ./exports/address.txt                 ║
║                        ./exports/privatekey.txt              ║
║                                                              ║
╚══════════════════════════════════════════════════════════════╝
```

### 4.7 Non-Interactive Mode
**Add flags to cmd/main.go:**
```go
var (
    noInteractive = flag.Bool("non-interactive", false, "Run without UI")
    generateCount = flag.Int("generate", 0, "Generate N wallets and exit")
    vanityPrefix  = flag.String("vanity-prefix", "", "Vanity prefix pattern")
    vanitySuffix  = flag.String("vanity-suffix", "", "Vanity suffix pattern")
    vanityCount   = flag.Int("vanity-count", 1, "Number of vanity wallets")
    noColor       = flag.Bool("no-color", false, "Disable color output")
)
```

**Usage:**
```bash
# Generate 10,000 wallets non-interactively
./evmwalletbot --non-interactive --generate 10000

# Generate 5 vanity wallets with prefix "dead"
./evmwalletbot --non-interactive --vanity-prefix dead --vanity-count 5
```

---

## Definition of Done Checklist

### Build & Test
- [ ] `gofmt -l .` → empty
- [ ] `go build ./...` → exit 0, no output
- [ ] `go vet ./...` → exit 0
- [ ] `go test ./... -race -count=1` → ALL pass
- [ ] No .go file contains \r
- [ ] `scripts/verify.sh` passes 3 times consecutively

### Functionality
- [ ] Every main menu action runs without panic
- [ ] Plaintext export works in all 4 modes
- [ ] Exported addresses verified against private keys
- [ ] Vanity generation works (prefix, suffix, both, checksum)
- [ ] Live progress renders correctly in TTY
- [ ] Plain fallback works when piped
- [ ] Non-interactive mode works
- [ ] Ctrl+C cleanup is graceful (no leaks)

### Code Quality
- [ ] No duplicate code
- [ ] All errors properly handled
- [ ] No data races
- [ ] Input validation everywhere
- [ ] Exported symbols documented
- [ ] Test coverage >80% for core logic

### Documentation
- [ ] README updated with:
  - [ ] Build/run instructions
  - [ ] Plaintext export usage
  - [ ] Vanity usage + difficulty formula
  - [ ] New flags (--non-interactive, --no-color, etc.)
  - [ ] Configuration options
- [ ] All .md files moved to /docs except README + LICENSE
- [ ] CHANGELOG.md created with all changes

### Git Hygiene
- [ ] Each commit builds + tests clean
- [ ] Commit messages follow conventional format
- [ ] No broken intermediate commits
- [ ] PR description includes:
  - [ ] Summary of bugs fixed (major → smallest)
  - [ ] Refactoring rationale
  - [ ] New features description
  - [ ] Pasted green verify.sh output

---

## Implementation Order

### Sprint 1: Foundation (Days 1-2)
1. Create verify.sh script
2. Run initial verification
3. Fix any issues found
4. Set up ephemeral Postgres for tests

### Sprint 2: Quality Pass (Days 3-5)
1. Eliminate duplicate code
2. Error handling audit
3. Fix race conditions
4. Input validation
5. Manual menu testing
6. Code quality tools
7. Move docs to /docs
8. Expand test coverage

### Sprint 3: Plaintext Export (Days 6-7)
1. Configuration schema
2. Exporter implementation
3. Integration with generator
4. Menu integration
5. Verification tests
6. Documentation

### Sprint 4: UI/UX Polish (Days 8-9)
1. Live progress display
2. Vanity progress
3. Status strip
4. Completion summary
5. Non-interactive mode
6. TTY detection and color gating

### Sprint 5: Final Verification (Day 10)
1. Run verify.sh 3x consecutively
2. Manual testing of all features
3. Update README
4. Create CHANGELOG
5. Prepare PR

---

## Success Metrics

- **Zero compilation errors** across all commits
- **Zero test failures** in CI/CD
- **Zero data races** detected
- **Zero panics** in normal operation
- **100% menu coverage** (all paths tested)
- **>80% code coverage** for core logic
- **<1s startup time** for CLI
- **>5000 wallets/sec** generation speed (16 workers)
- **Clean verify.sh output** 3x in a row

---

## Risk Mitigation

### Risk: Breaking existing functionality
**Mitigation:** Run verify.sh after EVERY change, no exceptions

### Risk: Database connection leaks
**Mitigation:** Add connection pool monitoring tests, verify cleanup on Ctrl+C

### Risk: File corruption in export
**Mitigation:** Atomic writes (.tmp + rename), buffered I/O with explicit flush

### Risk: Race conditions in concurrent generation
**Mitigation:** Run all tests with -race flag, add stress tests

### Risk: Poor UX in non-TTY environments
**Mitigation:** Comprehensive TTY detection, fallback to plain output

---

## Notes

- **Ponytail mode active:** Prefer stdlib, avoid new dependencies, delete over add
- **No browser/JS/WASM:** CLI only, as specified
- **Postgres is primary:** File export is optional/supplementary
- **Backward compatibility:** Existing .env configs must still work
- **Security:** Never log private keys, warn about plaintext export risks
