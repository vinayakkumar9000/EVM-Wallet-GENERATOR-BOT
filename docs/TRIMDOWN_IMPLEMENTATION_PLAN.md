# EVM Wallet Generator Trimdown Implementation Plan

## Overview
This plan details the complete trimdown of the EVM wallet generator to focus ONLY on wallet generation, vanity generation, stats, config/tuning, and benchmark. All database management, lookup, and monitoring tools will be removed.

## Phase 1: Remove First-Run Tips

### 1.1 Remove from config/config.go
**File:** `config/config.go`

**Changes:**
- Remove `ShowFirstRunTips bool` field from Config struct (line ~28)
- Remove `showFirstRunTips := getEnv("SHOW_FIRST_RUN_TIPS", "true") == "true"` (line ~96)
- Remove `ShowFirstRunTips: showFirstRunTips,` from Config initialization (line ~115)
- Remove ShowFirstRunTips validation/references if any

### 1.2 Remove from cli/menu.go
**File:** `cli/menu.go`

**Changes:**
- Delete lines 29-30: `if cfg.ShowFirstRunTips { showFirstRunTips(reader, cfg) }`
- Delete entire `showFirstRunTips` function (lines ~1267-1308)
- Remove `toggleFirstRunTips` function (lines ~752-763)
- Remove case "8" from handleConfigMenu that calls toggleFirstRunTips
- Update config menu display to remove "First-run tips" option
- Renumber config menu options (currently 1-9, will become 1-8)

## Phase 2: Remove Database Management Menu

### 2.1 Remove handleDatabaseMenu and all helpers
**File:** `cli/menu.go`

**Functions to DELETE:**
- `handleDatabaseMenu` (lines ~173-217)
- `handleDatabaseHealth` (lines ~594-598)
- `showPoolStatus` (lines ~1031-1062)
- `recordHealthSnapshot` (lines ~1064-1096)
- `showMaintenanceRecommendations` (lines ~1098-1143)
- `watchPoolStatusLive` (lines ~1145-1179)
- `watchWalletStatsLive` (lines ~1181-1215)

**Changes to Run function:**
- Remove case "4": `handleDatabaseMenu(ctx, store, reader)` from main menu switch

## Phase 3: Remove Wallet Lookup Menu

### 3.1 Remove lookup functions
**File:** `cli/menu.go`

**Functions to DELETE:**
- `handleLookupMenu` (lines ~169-171)
- `handleWalletInfo` (lines ~561-592)

**Changes to Run function:**
- Remove case "3": `handleLookupMenu(ctx, store, reader)` from main menu switch

## Phase 4: Update Main Menu

### 4.1 Renumber menu options
**File:** `cli/menu.go`

**Current menu (9 options):**
1. Generate wallets (G)
2. Statistics (S)
3. Wallet lookup (L) ← DELETE
4. Database & monitoring (D) ← DELETE
5. Benchmark / tuning (B)
6. Configuration (C)
8. Help (H)
9. Vanity address (V)
0. Exit (Q)

**New menu (6 options):**
1. Generate wallets (G)
2. Vanity generation (V)
3. Statistics (S)
4. Configuration / tuning (C)
5. Benchmark / tuning (B)
6. Help (H)
0. Exit (Q)

**Changes to printMenu function (lines ~437-465):**
```go
// Update menu display
│   1 [G]   Generate wallets               (batch hint)
│   2 [V]   Vanity generation
│   3 [S]   Statistics
│   4 [C]   Configuration / tuning         (workers hint)
│   5 [B]   Benchmark / tuning
│   6 [H]   Help
│   0 [Q]   Exit
```

**Changes to normalizeMenuChoice function (lines ~429-435):**
```go
shortcuts := map[string]string{
    "g": "1", // Generate
    "v": "2", // Vanity
    "s": "3", // Statistics
    "c": "4", // Configuration
    "b": "5", // Benchmark
    "h": "6", // Help
    "q": "0", // Quit
    "x": "0", // Alternative quit
}
```

**Changes to Run function switch statement (lines ~48-73):**
```go
switch choice {
case "1":
    handleGenerateMenu(ctx, store, cfg, reader)
case "2":
    handleVanityMenu(ctx, store, cfg, reader)
case "3":
    handleStatsMenu(ctx, store, reader)
case "4":
    handleConfigMenu(cfg, reader)
case "5":
    handleBenchmarkMenu(ctx, store, cfg, reader)
case "6":
    handleHelpMenu(reader)
case "0":
    fmt.Println(core.Info("\n[INFO] Goodbye."))
    return
default:
    fmt.Println(core.Warning("\n[WARN] Invalid option — please choose 1-6 or 0."))
}
```

## Phase 5: Update Help Menu

### 5.1 Remove deleted tool help entries
**File:** `cli/menu.go`

**Changes to handleHelpMenu function (lines ~357-380):**
- Remove case "4": `showDatabaseHelp()` 
- Renumber remaining options (1-5 becomes 1-4)

**Update menu display:**
```go
│   1   Generation modes
│   2   Batch size guide
│   3   Workers guide
│   4   Settings guide
│   5   Back
```

**Functions to DELETE:**
- `showDatabaseHelp` (lines ~1249-1265)

**Update switch statement:**
```go
switch strings.TrimSpace(readLine(reader)) {
case "1":
    showGenerationHelp()
case "2":
    showBatchSizeHelp()
case "3":
    showWorkersHelp()
case "4":
    showSettingsHelp()
case "5":
    return
default:
    fmt.Println(core.Warning("\n[WARN] Invalid option — please choose 1 to 5."))
}
```

## Phase 6: Vanity Generation Changes

### 6.1 Create separate vanity database support
**File:** `storage/sqlite/sqlite.go`

**Add new function:**
```go
// OpenVanityDB opens or creates the vanity.db database
func OpenVanityDB(dataDir string) (*Store, error) {
    dbPath := filepath.Join(dataDir, "vanity.db")
    // Same logic as Open() but for vanity.db
    // Return a separate Store instance
}
```

### 6.2 Modify vanity generation to use separate DB
**File:** `core/vanity.go`

**Changes to GenerateVanityWallets function:**
1. Open separate vanity.db at start
2. Remove any pool monitoring/status display during generation
3. Modify displayMatch to:
   - Show full address (not truncated)
   - Show full private key with 0x prefix (all 64 hex chars)
   - Only display first 5 matches
4. After 5 matches displayed, show: "... (N total matches saved to vanity.db)"
5. All matches still saved to vanity.db
6. Ensure main wallets.db receives ZERO wallets

**Specific changes:**
- Line ~67: Add vanity DB opening logic
- Lines ~140-155 (displayMatch): Show full keys, limit to 5 displays
- Lines ~157-170 (saveVanityMatches): Save to vanity.db instead of main store
- Remove any pool monitoring calls

### 6.3 Update vanity match display
**File:** `core/vanity.go`

**Modify displayMatch function (lines ~140-155):**
```go
func displayMatch(match *VanityMatch, current, target int, displayLimit int) {
    // Only display if within limit (first 5)
    if current > displayLimit {
        return
    }
    
    addr := match.Wallet.AddressHex() // Full address with 0x
    privKey := "0x" + match.Wallet.PrivateKeyHex() // Full 64 hex chars with 0x
    
    fmt.Print("\r" + clearLine())
    fmt.Printf("\n  %s MATCH %d/%d\n", Success("✓"), current, target)
    fmt.Printf("    address      %s\n", Info("%s", addr))
    fmt.Printf("    private key  %s\n", Info("%s", privKey))
    fmt.Printf("    attempts     %s   ·   elapsed %s\n\n",
        FormatNumber(int(match.Attempts)),
        match.Elapsed.Round(time.Millisecond))
    
    // After 5th match, show summary
    if current == displayLimit && target > displayLimit {
        fmt.Printf("  ... (%d total matches will be saved to vanity.db)\n\n", target)
    }
}
```

## Phase 7: Verify Benchmark Stores No Wallets

### 7.1 Audit benchmark functions
**File:** `cli/menu.go`

**Functions to verify:**
- `runSmallBenchmark` (lines ~1217-1247)
- `compareWorkerCounts` (lines ~1249-1283)
- `compareBatchSizes` (lines ~1285-1319)

**Verification:**
- These functions call `core.GenerateWallets()` which saves to store
- **PROBLEM:** Benchmark currently DOES save wallets!
- **FIX NEEDED:** Create a no-op/mock storage for benchmarks

### 7.2 Create benchmark-specific generation
**File:** `core/generator.go`

**Add new function:**
```go
// GenerateWalletsBenchmark generates wallets for benchmarking without storage
func GenerateWalletsBenchmark(ctx context.Context, cfg *config.Config, totalWallets int) error {
    // Same logic as GenerateWallets but skip all storage operations
    // Only measure generation speed, no DB inserts
}
```

**Update benchmark functions to use GenerateWalletsBenchmark instead of GenerateWallets**

## Phase 8: Configuration Menu Updates

### 8.1 Remove first-run tips toggle
**File:** `cli/menu.go`

**Changes to handleConfigMenu (lines ~337-355):**
- Remove "First-run tips" display line
- Remove case "8": `toggleFirstRunTips(cfg)`
- Renumber case "9" to "8" (reset settings)
- Update menu display to show options 1-8 instead of 1-9

**Update menu display:**
```go
│   1   Show current settings
│   2   Workers
│   3   Batch size
│   4   Logging (enable/disable)
│   5   Pool monitor interval
│   6   Pool warning threshold
│   7   UI mode (full/minimal)
│   8   Reset session settings
│   0   Back
```

## Phase 9: Clean Up Unused Imports

After all deletions, check for unused imports in:
- `cli/menu.go`
- `config/config.go`
- `core/vanity.go`

Run `gofmt -s -w .` to clean up.

## Phase 10: Testing & Verification

### 10.1 Build verification
```bash
gofmt -l .            # Should output nothing
go build ./...        # Should exit 0
go vet ./...          # Should exit 0
go test ./... -race -count=1  # Should pass
```

### 10.2 Carriage return check
```bash
# PowerShell
Get-ChildItem -Recurse -Filter "*.go" | Select-String -Pattern "`r" | Select-Object Path, LineNumber
```

### 10.3 Smoke tests
1. **Launch test:** Run app, verify goes straight to main menu (no welcome box)
2. **Menu test:** Verify menu shows only: Generate, Vanity, Stats, Config, Benchmark, Help, Quit
3. **Vanity test:** Run vanity search that finds >5 matches
   - Verify terminal prints exactly 5 full address+key pairs
   - Verify message shows total saved
   - Verify vanity.db created
   - Verify wallets.db unchanged (row count same)
4. **Benchmark test:** Run benchmark/tuning
   - Verify wallets.db and vanity.db row counts unchanged
5. **Normal generate test:** Run normal Generate
   - Verify wallets.db updated
   - Verify .txt export still works

## Implementation Order

1. Phase 1: Remove first-run tips (config + menu)
2. Phase 2: Remove database management menu
3. Phase 3: Remove wallet lookup menu
4. Phase 4: Update main menu (renumber, update shortcuts)
5. Phase 5: Update help menu
6. Phase 8: Update config menu
7. Phase 7: Fix benchmark to not store wallets
8. Phase 6: Vanity generation changes (separate DB, limit output)
9. Phase 9: Clean up imports
10. Phase 10: Testing & verification

## Files Modified Summary

1. `config/config.go` - Remove ShowFirstRunTips field
2. `cli/menu.go` - Major changes (remove functions, update menus)
3. `core/vanity.go` - Separate DB, limit output to 5 matches
4. `core/generator.go` - Add benchmark-only generation function
5. `storage/sqlite/sqlite.go` - Add vanity DB support

## Success Criteria

✅ No first-run tips dialogue
✅ Main menu shows only 6 options + quit
✅ No database management menu
✅ No wallet lookup menu
✅ No monitoring tools
✅ Vanity uses separate vanity.db
✅ Vanity shows max 5 matches in terminal (full keys)
✅ Benchmark stores no wallets
✅ All tests pass
✅ No formatting issues
✅ Smoke tests pass
