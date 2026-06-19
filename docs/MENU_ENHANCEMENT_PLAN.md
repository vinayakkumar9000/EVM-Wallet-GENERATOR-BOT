# EVM Wallet Manager - Menu Enhancement Plan

## Current State Analysis

### Existing Menu Structure
```
Main Menu (9 options)
├── 1) Generate wallets (submenu with 5 options)
│   ├── 1) Generate by wallet count
│   ├── 2) Generate by batch count
│   ├── 3) Change generation settings
│   ├── 4) Preview current generation settings
│   └── 5) Back
├── 2) Statistics (single action)
├── 3) Wallet lookup (single action)
├── 4) Database tools (single action - health check only)
├── 5) Monitoring (single action - recent events)
├── 6) Benchmark / tuning (single action - prints message)
├── 7) Configuration (submenu with 2 options)
│   ├── 1) Update batch size
│   └── 2) Back
├── 8) Help (single action - prints text)
└── 9) Exit
```

### Identified Gaps

1. **Configuration Menu** - Only allows batch size changes, missing:
   - Workers configuration
   - Logging toggle
   - Pool monitor interval
   - Pool warning threshold
   - Database connection settings
   - Session settings reset

2. **Generation Flow** - Missing:
   - Run preview before execution
   - Confirmation prompt for large runs
   - Better validation and error messages

3. **Statistics** - Single action, missing:
   - Live watch mode
   - Database size info
   - Refresh options

4. **Database Tools** - Single action, missing:
   - Connection pool status
   - Health snapshot recording
   - Maintenance recommendations

5. **Monitoring** - Single action, missing:
   - Pool status display
   - Live watch modes
   - Configurable refresh interval

6. **Benchmark/Tuning** - Just prints message, missing:
   - Estimation tools
   - Small benchmark runner
   - Worker/batch comparison tools

7. **Help** - Single text dump, missing:
   - Organized help pages
   - Topic-specific help
   - Interactive navigation

---

## Proposed Menu Architecture

### Complete Menu Tree
```
EVM Wallet Manager
├── 1) Generate wallets
│   ├── 1) Generate by wallet count
│   ├── 2) Generate by batch count
│   ├── 3) Preview run settings
│   ├── 4) Generation settings
│   └── 5) Back
│
├── 2) Statistics
│   ├── 1) Show current stats
│   ├── 2) Watch stats live (auto-refresh)
│   ├── 3) Database size
│   └── 4) Back
│
├── 3) Wallet lookup
│   ├── 1) Lookup by wallet ID
│   ├── 2) Lookup by address
│   └── 3) Back
│
├── 4) Database tools
│   ├── 1) Health check
│   ├── 2) Connection pool status
│   ├── 3) Record health snapshot
│   ├── 4) Maintenance recommendations
│   └── 5) Back
│
├── 5) Monitoring
│   ├── 1) Pool status (once)
│   ├── 2) Watch pool status (live)
│   ├── 3) Watch wallet stats (live)
│   ├── 4) Set refresh interval
│   └── 5) Back
│
├── 6) Benchmark / tuning
│   ├── 1) Estimate current settings
│   ├── 2) Run small benchmark (1000 wallets)
│   ├── 3) Compare worker counts
│   ├── 4) Compare batch sizes
│   └── 5) Back
│
├── 7) Configuration
│   ├── 1) Show current settings
│   ├── 2) Workers
│   ├── 3) Batch size
│   ├── 4) Logging (enable/disable)
│   ├── 5) Pool monitor interval
│   ├── 6) Pool warning threshold
│   ├── 7) Confirmation prompts (enable/disable)
│   ├── 8) Reset session settings
│   └── 9) Back
│
├── 8) Help
│   ├── 1) Generation modes
│   ├── 2) Batch size guide
│   ├── 3) Workers guide
│   ├── 4) Database guide
│   ├── 5) Settings guide
│   └── 6) Back
│
└── 9) Exit
```

---

## Implementation Phases

### Phase 1: Enhanced Configuration Menu ⭐ HIGH PRIORITY
**Goal:** Allow runtime configuration without editing .env

**Changes to `cli/menu.go`:**
1. Expand `handleConfigMenu()` to show all settings
2. Add handlers for each setting type:
   - `changeWorkers()` - Update cfg.Workers
   - `changeBatchSize()` - Already exists, keep it
   - `toggleLogging()` - Update cfg.EnableLogging
   - `changePoolMonitorInterval()` - Update cfg.PoolMonitorInterval
   - `changePoolWarningThreshold()` - Update cfg.PoolWarningThreshold
   - `resetSessionSettings()` - Reload from .env

**New Functions:**
```go
func handleConfigMenu(cfg *config.Config, reader *bufio.Reader) {
    // Show all settings with 9 options
}

func changeWorkers(cfg *config.Config, reader *bufio.Reader) {
    // Prompt for new worker count (1-100)
    // Validate and update cfg.Workers
}

func toggleLogging(cfg *config.Config) {
    // Toggle cfg.EnableLogging
}

func changePoolMonitorInterval(cfg *config.Config, reader *bufio.Reader) {
    // Prompt for interval in seconds (0 to disable)
}

func changePoolWarningThreshold(cfg *config.Config, reader *bufio.Reader) {
    // Prompt for threshold (0.0-1.0)
}

func resetSessionSettings(cfg *config.Config) {
    // Reload original values from .env
}
```

**Success Criteria:**
- ✅ All config options accessible via menu
- ✅ Changes apply to current session only
- ✅ Validation prevents invalid values
- ✅ Reset option restores .env defaults

---

### Phase 2: Generation Preview & Confirmation ⭐ HIGH PRIORITY
**Goal:** Show preview before large generation runs

**Changes to `cli/menu.go`:**
1. Add `previewGenerationRun()` function
2. Add confirmation prompt in `generateWallets()`
3. Make confirmation optional via config

**New Functions:**
```go
func previewGenerationRun(cfg *config.Config, total int) {
    // Display:
    // - Total wallets
    // - Number of batches
    // - Workers
    // - Batch size
    // - Database name
    // - Logging status
}

func confirmGeneration(reader *bufio.Reader, total int) bool {
    // Prompt: "Continue? [y/N]: "
    // Return true if user confirms
}
```

**Modified Functions:**
```go
func generateWallets(ctx, pool, cfg, total) {
    // 1. Show preview
    previewGenerationRun(cfg, total)
    
    // 2. Ask for confirmation if total > threshold (e.g., 10000)
    if total > 10000 && !confirmGeneration(reader, total) {
        return
    }
    
    // 3. Proceed with generation
}
```

**Success Criteria:**
- ✅ Preview shows all relevant settings
- ✅ Confirmation required for large runs (>10k wallets)
- ✅ User can cancel before generation starts
- ✅ Confirmation can be disabled in config

---

### Phase 3: Statistics Submenu
**Goal:** Add live watch and database size info

**Changes to `cli/menu.go`:**
1. Convert `handleStatsMenu()` to submenu with 4 options
2. Add live watch functionality
3. Add database size query

**New Functions:**
```go
func handleStatsMenu(ctx, pool, reader) {
    // Show submenu with 4 options
}

func watchStatsLive(ctx, pool, interval) {
    // Auto-refresh stats every N seconds
    // Press any key to stop
}

func showDatabaseSize(ctx, pool) {
    // Query: SELECT pg_size_pretty(pg_database_size('walletdb'))
    // Show table sizes, index sizes
}
```

**Success Criteria:**
- ✅ Stats submenu with 4 options
- ✅ Live watch mode with configurable interval
- ✅ Database size information
- ✅ Clean exit from watch mode

---

### Phase 4: Database Tools Submenu
**Goal:** Comprehensive database management

**Changes to `cli/menu.go`:**
1. Convert `handleDatabaseMenu()` to submenu with 5 options
2. Add pool status display
3. Add health snapshot recording
4. Add maintenance recommendations

**New Functions:**
```go
func handleDatabaseMenu(ctx, pool, reader) {
    // Show submenu with 5 options
}

func showPoolStatus(pool) {
    // Display pool.Stat() information
    // - Total connections
    // - Idle connections
    // - Acquired connections
    // - Max connections
}

func recordHealthSnapshot(ctx, pool) {
    // Run health check and save to file
    // Format: health_snapshot_YYYYMMDD_HHMMSS.txt
}

func showMaintenanceRecommendations(ctx, pool) {
    // Analyze database and suggest:
    // - VACUUM if needed
    // - Index rebuilds
    // - Statistics updates
}
```

**Success Criteria:**
- ✅ Database submenu with 5 options
- ✅ Pool status shows real-time info
- ✅ Health snapshots saved to files
- ✅ Maintenance recommendations actionable

---

### Phase 5: Monitoring Submenu
**Goal:** Real-time monitoring with live updates

**Changes to `cli/menu.go`:**
1. Convert `handleMonitoringMenu()` to submenu with 5 options
2. Add live watch modes
3. Add configurable refresh interval

**New Functions:**
```go
func handleMonitoringMenu(ctx, pool, cfg, reader) {
    // Show submenu with 5 options
}

func watchPoolStatus(ctx, pool, interval) {
    // Auto-refresh pool status
    // Press any key to stop
}

func watchWalletStats(ctx, pool, interval) {
    // Auto-refresh wallet statistics
    // Press any key to stop
}

func setRefreshInterval(cfg, reader) {
    // Prompt for interval (1-60 seconds)
    // Update cfg.MonitorRefreshInterval
}
```

**Success Criteria:**
- ✅ Monitoring submenu with 5 options
- ✅ Live watch modes work correctly
- ✅ Configurable refresh interval
- ✅ Clean exit from watch modes

---

### Phase 6: Benchmark/Tuning Submenu
**Goal:** Performance testing and optimization

**Changes to `cli/menu.go`:**
1. Convert `handleBenchmarkMenu()` to submenu with 5 options
2. Add estimation calculator
3. Add small benchmark runner
4. Add comparison tools

**New Functions:**
```go
func handleBenchmarkMenu(ctx, pool, cfg, reader) {
    // Show submenu with 5 options
}

func estimateSettings(cfg) {
    // Calculate estimated time for different scenarios
    // Based on current workers and batch size
}

func runSmallBenchmark(ctx, pool, cfg) {
    // Generate 1000 wallets
    // Measure time and throughput
    // Display results
}

func compareWorkerCounts(ctx, pool, cfg) {
    // Test with different worker counts
    // Show performance comparison
}

func compareBatchSizes(ctx, pool, cfg) {
    // Test with different batch sizes
    // Show performance comparison
}
```

**Success Criteria:**
- ✅ Benchmark submenu with 5 options
- ✅ Estimation shows realistic times
- ✅ Small benchmark runs safely
- ✅ Comparisons provide actionable insights

---

### Phase 7: Help Submenu
**Goal:** Organized, topic-specific help

**Changes to `cli/menu.go`:**
1. Convert `handleHelpMenu()` to submenu with 6 options
2. Add detailed help pages for each topic

**New Functions:**
```go
func handleHelpMenu(reader) {
    // Show submenu with 6 options
}

func showGenerationHelp() {
    // Explain generation modes
    // Best practices
    // Examples
}

func showBatchSizeHelp() {
    // Explain batch size impact
    // Recommendations
    // Trade-offs
}

func showWorkersHelp() {
    // Explain worker scaling
    // CPU considerations
    // Optimal settings
}

func showDatabaseHelp() {
    // Database setup
    // Connection tuning
    // Maintenance
}

func showSettingsHelp() {
    // All configuration options
    // When to change them
    // Default values
}
```

**Success Criteria:**
- ✅ Help submenu with 6 options
- ✅ Each help page is clear and actionable
- ✅ Examples provided where relevant
- ✅ Easy navigation between topics

---

### Phase 8: Error Messages & Validation
**Goal:** Clear, actionable error messages

**Changes throughout `cli/menu.go`:**
1. Improve all input validation
2. Add specific error messages
3. Add recovery suggestions

**Improvements:**
```go
// Before:
fmt.Println("[ERROR] Invalid input")

// After:
fmt.Println("[ERROR] Invalid worker count. Please enter a number between 1 and 100.")
fmt.Println("        Current value: 16")
fmt.Println("        Recommended: 8-32 (based on CPU cores)")
```

**Success Criteria:**
- ✅ All errors include specific reason
- ✅ All errors include current value
- ✅ All errors include valid range/format
- ✅ All errors include recovery suggestion

---

### Phase 9: Testing & Edge Cases
**Goal:** Robust menu system

**Test Cases:**
1. Invalid input handling
2. Context cancellation during operations
3. Database connection loss
4. Large number inputs (overflow)
5. Concurrent menu operations
6. Watch mode interruption
7. Configuration validation
8. Help navigation

**Success Criteria:**
- ✅ All edge cases handled gracefully
- ✅ No panics or crashes
- ✅ Clean shutdown on Ctrl+C
- ✅ State consistency maintained

---

### Phase 10: Documentation
**Goal:** Complete usage documentation

**Deliverables:**
1. Update README.md with menu structure
2. Create MENU_GUIDE.md with detailed usage
3. Add inline comments for complex logic
4. Document configuration options

**Success Criteria:**
- ✅ README shows menu tree
- ✅ MENU_GUIDE covers all features
- ✅ Code comments explain non-obvious logic
- ✅ Configuration options documented

---

## Design Principles

### 1. Keep Main Menu Clean
- Maximum 9 options on main menu
- Advanced features nested in submenus
- Common tasks easily accessible

### 2. Number-Based Navigation
- All options numbered 1-9
- Consistent "Back" option
- No complex key combinations

### 3. Progressive Disclosure
- Show simple options first
- Advanced features in submenus
- Help always available

### 4. Fail-Fast Validation
- Validate input immediately
- Show specific error messages
- Suggest valid alternatives

### 5. Non-Destructive Operations
- Confirmation for large operations
- Preview before execution
- Session-only configuration changes

### 6. Terminal-Friendly
- No external dependencies
- Works in any terminal
- Clean text-based UI

---

## Implementation Order

### Sprint 1 (High Priority)
1. Phase 1: Enhanced Configuration Menu
2. Phase 2: Generation Preview & Confirmation
3. Phase 8: Error Messages & Validation (partial)

### Sprint 2 (Medium Priority)
4. Phase 3: Statistics Submenu
5. Phase 4: Database Tools Submenu
6. Phase 5: Monitoring Submenu

### Sprint 3 (Lower Priority)
7. Phase 6: Benchmark/Tuning Submenu
8. Phase 7: Help Submenu
9. Phase 8: Error Messages & Validation (complete)

### Sprint 4 (Polish)
10. Phase 9: Testing & Edge Cases
11. Phase 10: Documentation

---

## Success Metrics

### User Experience
- ✅ All features accessible via menu
- ✅ No need to edit .env for common tasks
- ✅ Clear feedback for all actions
- ✅ Intuitive navigation

### Code Quality
- ✅ No code duplication
- ✅ Clear function names
- ✅ Comprehensive error handling
- ✅ Well-documented

### Performance
- ✅ Menu navigation instant (<100ms)
- ✅ Live watch modes responsive
- ✅ No memory leaks in watch modes
- ✅ Clean shutdown

---

## Risk Mitigation

### Risk: Breaking Existing Functionality
**Mitigation:** 
- Implement incrementally
- Test each phase thoroughly
- Keep existing functions working
- Add new functions alongside old ones

### Risk: Complex State Management
**Mitigation:**
- Session config separate from .env
- Clear reset mechanism
- Document state changes
- Validate all state transitions

### Risk: User Confusion
**Mitigation:**
- Consistent menu structure
- Clear option labels
- Comprehensive help system
- Examples in help pages

---

## Next Steps

1. ✅ Review and approve this plan
2. ⏳ Switch to code mode
3. ⏳ Implement Phase 1 (Configuration Menu)
4. ⏳ Test Phase 1 thoroughly
5. ⏳ Proceed to Phase 2

---

**Status:** Plan Complete - Ready for Implementation
**Estimated Effort:** 4 sprints (8-12 hours total)
**Priority:** High - Improves user experience significantly
