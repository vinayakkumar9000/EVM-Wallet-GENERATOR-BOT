# CLI UI/UX Redesign — Implementation Plan

**Project:** EVM Wallet Generator  
**Version:** 2.0 UI Overhaul  
**Status:** Planning Phase  
**Date:** 2026-06-19

---

## Executive Summary

This document provides a detailed implementation plan for redesigning the CLI interface of the EVM Wallet Generator. The redesign focuses on **live feedback**, **visual hierarchy**, **simplified navigation**, and **professional polish** while maintaining backward compatibility and respecting the "lazy senior dev" philosophy.

**Key Goals:**
1. Live progress visualization during wallet generation
2. Color-coded visual hierarchy with TTY detection
3. Flattened menu structure (9 items → 6 items)
4. Letter shortcuts alongside number navigation
5. Status-aware dashboard on home screen
6. Toggle-style settings interface

---

## Current State Analysis

### Existing Infrastructure (Assets)

✅ **Already Available:**
- `core/colors.go` — Full ANSI color support with TTY detection and NO_COLOR compliance
- `core/progress.go` — Progress tracking infrastructure (needs verification)
- `core/generator.go` — Atomic counters (`confirmedCount`) for real-time progress
- `cli/menu.go` — Complete menu system with 9 top-level items + 7 submenus
- Screen clearing capability via `ClearScreenIfEnabled()`
- Color functions: `Success()`, `Error()`, `Warning()`, `Info()`, `Hint()`, `Highlight()`

✅ **Existing Patterns:**
- Box-drawing characters for menus (┌─┐│└┘)
- Structured menu handlers with reader pattern
- Configuration preview displays
- Batch-level logging (not per-wallet)
- Retry logic with exponential backoff

### Current Pain Points

❌ **Missing or Inadequate:**
1. **No live progress bar** — Generation prints batch logs, no in-place updates
2. **No spinner animation** — Static output during long operations
3. **No speed/ETA display** — Users can't estimate completion time
4. **Deep menu nesting** — 7 submenus require multiple "Back" selections
5. **Number-only navigation** — No letter shortcuts (G/V/B/S/Q)
6. **No status strip** — Home screen doesn't show current state (wallet count, last run)
7. **Static settings display** — No toggle UI, just text-based prompts
8. **Menu reprinting on errors** — Validation errors reprint entire menu

---

## Implementation Strategy

### Phase Structure

We'll implement in **3 tiers** matching the priority levels from the design document:

- **Tier 1** (Highest Impact) — Live progress, colors, screen clearing, status strip
- **Tier 2** (Navigation) — Letter shortcuts, menu flattening, inline hints
- **Tier 3** (Polish) — Toggles, inline validation, confirmations, first-run tips

### Development Approach

**Lazy Senior Dev Principles:**
- Use stdlib where possible (no new dependencies for Tier 1-2)
- Reuse existing `colors.go` infrastructure
- Build on existing atomic counters in `generator.go`
- Keep changes surgical and focused
- Add `ponytail:` comments for intentional simplifications

**Optional Enhancement Path:**
- Tier 1-2 can be done with pure stdlib
- Tier 3 polish can optionally use Bubble Tea if desired
- Provide both paths in the implementation guide

---

## Tier 1: Highest Impact Changes

### 1.1 Live Progress Bar System

**Goal:** Real-time visual feedback during wallet generation with spinner, bar, %, speed, ETA.

**Current State:**
```go
// core/generator.go (lines ~50-70)
var confirmedCount atomic.Int64
// Progress updates via batch logging only
if cfg.EnableLogging {
    log.Printf("[INFO] Batch %d complete: %d wallets inserted", batchNum, len(ids))
}
```

**Target State:**
```
⠹  ████████████████████████████░░░░░░░░░░░░   68%   683 / 1,000

    speed    18,204 /s          peak     20,180 /s
    elapsed  0.04s              eta      0.02s
    written  ./wallets/address.txt  +  privatekey.txt
```

**Implementation Plan:**

**File:** `core/progress.go` (create or enhance)

```go
package core

import (
    "fmt"
    "strings"
    "sync/atomic"
    "time"
)

// ProgressTracker manages live progress display
type ProgressTracker struct {
    total       int
    startTime   time.Time
    spinFrames  []rune
    spinIndex   int
    peakRate    float64
    lastCount   int
    lastTime    time.Time
}

func NewProgressTracker(total int) *ProgressTracker {
    return &ProgressTracker{
        total:      total,
        startTime:  time.Now(),
        spinFrames: []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"),
        lastTime:   time.Now(),
    }
}

// Render updates the progress display in-place
func (p *ProgressTracker) Render(current int) {
    if !IsColorEnabled() {
        // Fallback: print every 10% milestone
        pct := float64(current) / float64(p.total) * 100
        if int(pct)%10 == 0 && current > p.lastCount {
            fmt.Printf("Progress: %d/%d (%.0f%%)\n", current, p.total, pct)
            p.lastCount = current
        }
        return
    }

    elapsed := time.Since(p.startTime).Seconds()
    rate := float64(current) / elapsed
    if rate > p.peakRate {
        p.peakRate = rate
    }

    remaining := p.total - current
    eta := 0.0
    if rate > 0 {
        eta = float64(remaining) / rate
    }

    frac := float64(current) / float64(p.total)
    bar := progressBar(frac, 40)
    spin := p.spinFrames[p.spinIndex%len(p.spinFrames)]
    p.spinIndex++

    // Clear line and redraw
    fmt.Printf("\r\033[K   %c  %s  %3.0f%%   %s / %s\n",
        spin, bar, frac*100,
        formatNumber(current), formatNumber(p.total))
    fmt.Printf("\r\033[K       speed    %s /s          peak     %s /s\n",
        formatNumber(int(rate)), formatNumber(int(p.peakRate)))
    fmt.Printf("\r\033[K       elapsed  %.2fs              eta      %.2fs\n",
        elapsed, eta)
    
    // Move cursor back up 3 lines for next update
    fmt.Print("\033[3A")
}

// Finish displays final summary
func (p *ProgressTracker) Finish(final int) {
    // Move cursor down 3 lines to clear the progress area
    fmt.Print("\033[3B")
    fmt.Print("\r\033[K")
    
    elapsed := time.Since(p.startTime)
    avgRate := float64(final) / elapsed.Seconds()
    
    fmt.Printf("\n%s\n", Success("✓ Generation complete"))
    fmt.Printf("  %s wallets in %s (avg %s /s, peak %s /s)\n\n",
        formatNumber(final),
        elapsed.Round(time.Millisecond),
        formatNumber(int(avgRate)),
        formatNumber(int(p.peakRate)))
}

func progressBar(frac float64, width int) string {
    if frac < 0 { frac = 0 }
    if frac > 1 { frac = 1 }
    fill := int(frac * float64(width))
    return strings.Repeat("█", fill) + strings.Repeat("░", width-fill)
}

func formatNumber(n int) string {
    s := fmt.Sprintf("%d", n)
    if len(s) <= 3 {
        return s
    }
    var result []byte
    for i, c := range s {
        if i > 0 && (len(s)-i)%3 == 0 {
            result = append(result, ',')
        }
        result = append(result, byte(c))
    }
    return string(result)
}
```

**Integration Points:**

1. **In `core/generator.go`** (around line 50):
```go
// Replace the progress goroutine section
tracker := NewProgressTracker(totalWallets)
progressDone := make(chan struct{})

go func() {
    ticker := time.NewTicker(120 * time.Millisecond)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            tracker.Render(int(confirmedCount.Load()))
        case <-progressDone:
            tracker.Render(int(confirmedCount.Load()))
            return
        case <-ctx.Done():
            return
        }
    }
}()
```

2. **At end of generation** (after final batch):
```go
close(progressDone)
time.Sleep(150 * time.Millisecond) // Let final render complete
tracker.Finish(int(confirmedCount.Load()))
```

**Testing:**
- Run with 1,000 wallets (fast, visible updates)
- Run with 100,000 wallets (sustained progress)
- Test with `NO_COLOR=1` (should fall back to milestone printing)
- Test with piped output: `./evmwalletbot | tee log.txt`

---

### 1.2 Status Strip on Home Screen

**Goal:** Show current state at a glance before the main menu.

**Target State:**
```
   READY   ·   12,480 wallets in DB   ·   last run 18.2k/s
```

**Implementation Plan:**

**File:** `cli/status.go` (create new file)

```go
package cli

import (
    "context"
    "fmt"
    "time"
    
    "github.com/jackc/pgx/v5/pgxpool"
    "evmwalletbot/config"
    "evmwalletbot/core"
)

// StatusStrip displays current system state
type StatusStrip struct {
    WalletCount int64
    LastRunRate int
    DBName      string
}

func GetStatusStrip(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) (*StatusStrip, error) {
    var count int64
    err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM wallets").Scan(&count)
    if err != nil {
        return nil, err
    }
    
    // TODO: Store last run rate in a simple cache or config
    // For now, return 0 (will show "no recent runs")
    
    return &StatusStrip{
        WalletCount: count,
        LastRunRate: 0, // Placeholder
        DBName:      cfg.DBName,
    }, nil
}

func (s *StatusStrip) Render() {
    if !core.IsColorEnabled() {
        fmt.Printf("Status: %d wallets in %s\n", s.WalletCount, s.DBName)
        return
    }
    
    status := core.Success("READY")
    walletInfo := fmt.Sprintf("%s wallets in %s", 
        formatNumber(int(s.WalletCount)), s.DBName)
    
    lastRun := core.Hint("no recent runs")
    if s.LastRunRate > 0 {
        lastRun = fmt.Sprintf("last run %s/s", formatNumber(s.LastRunRate))
    }
    
    fmt.Printf("\n   %s   ·   %s   ·   %s\n\n", status, walletInfo, lastRun)
}

func formatNumber(n int) string {
    s := fmt.Sprintf("%d", n)
    if len(s) <= 3 {
        return s
    }
    var result []byte
    for i, c := range s {
        if i > 0 && (len(s)-i)%3 == 0 {
            result = append(result, ',')
        }
        result = append(result, byte(c))
    }
    return string(result)
}
```

**Integration Point:**

**In `cli/menu.go`** (in the `Run()` function, before `printMenu()`):
```go
// Add after printBanner() and before printMenu()
if strip, err := GetStatusStrip(ctx, pool, cfg); err == nil {
    strip.Render()
}
```

---

### 1.3 Enhanced Color Usage

**Current State:** Colors exist but underutilized in menus.

**Target State:** Visual hierarchy with colors:
- Green: Success states, active items
- Yellow: Warnings, important notices
- Red: Errors, critical items
- Cyan: Info, hints
- Dim: Secondary text, hints
- Bold: Focus, emphasis

**Implementation Plan:**

**File:** `cli/menu.go` (enhance `printMenu()`)

```go
func printMenu() {
    title := core.Highlight("MAIN MENU")
    
    fmt.Printf(`
  ┌──────────────────────────────────────┐
  │   %s                        │
  ├──────────────────────────────────────┤
  │   %s   Generate wallets               │
  │   %s   Statistics                     │
  │   %s   Wallet lookup                  │
  │   %s   Database tools                 │
  │   %s   Monitoring                     │
  │   %s   Benchmark / tuning             │
  │   %s   Configuration                  │
  │   %s   Help                           │
  │   %s   Exit                           │
  └──────────────────────────────────────┘
  %s `,
        title,
        core.Success("1"),
        core.Info("2"),
        core.Info("3"),
        core.Info("4"),
        core.Info("5"),
        core.Info("6"),
        core.Info("7"),
        core.Info("8"),
        core.Warning("9"),
        core.Hint("Select option:"))
}
```

**Apply to all submenus** — Use color functions consistently:
- Menu titles: `Highlight()`
- Action items: `Success()` for primary, `Info()` for secondary
- Back/Exit: `Warning()`
- Prompts: `Hint()`

---

### 1.4 Screen Clearing Strategy

**Current State:** Menus stack and scroll away.

**Target State:** Clear screen between menu transitions.

**Implementation:** Already exists! Just use consistently.

**In `cli/menu.go`** (in `Run()` function):
```go
// Already present:
core.ClearScreenIfEnabled()
printBanner()

// Ensure it's called before each menu transition
switch choice {
case "1":
    core.ClearScreenIfEnabled() // Add if missing
    handleGenerateMenu(ctx, pool, cfg, reader)
// ... etc
}
```

**Verify:** All menu handlers should call `ClearScreenIfEnabled()` at entry.

---

## Tier 2: Navigation & Comprehension

### 2.1 Letter Shortcuts

**Goal:** Allow `G`, `V`, `B`, `S`, `Q` alongside `1`, `2`, etc.

**Implementation Plan:**

**File:** `cli/menu.go` (enhance input handling)

```go
// Add a mapping function
func normalizeMenuChoice(input string) string {
    input = strings.ToLower(strings.TrimSpace(input))
    
    // Letter shortcuts for main menu
    shortcuts := map[string]string{
        "g": "1", // Generate
        "s": "2", // Statistics
        "l": "3", // Lookup
        "d": "4", // Database
        "m": "5", // Monitoring
        "b": "6", // Benchmark
        "c": "7", // Configuration
        "h": "8", // Help
        "q": "9", // Quit
        "x": "9", // Alternative quit
    }
    
    if mapped, ok := shortcuts[input]; ok {
        return mapped
    }
    return input
}

// In Run() function:
choice := normalizeMenuChoice(readLine(reader))
```

**Update Menu Display:**
```go
│   1 [G]   Generate wallets               │
│   2 [S]   Statistics                     │
│   3 [L]   Wallet lookup                  │
│   4 [D]   Database tools                 │
│   5 [M]   Monitoring                     │
│   6 [B]   Benchmark / tuning             │
│   7 [C]   Configuration                  │
│   8 [H]   Help                           │
│   9 [Q]   Exit                           │
```

---

### 2.2 Menu Flattening

**Current Structure:** 9 top-level + 7 submenus (16 total screens)

**Target Structure:** 6 top-level items (reduce by 3)

**Proposed Flattening:**

**Before:**
1. Generate → submenu (5 items)
2. Statistics → submenu (4 items)
3. Lookup → direct action
4. Database → submenu (5 items)
5. Monitoring → submenu (5 items)
6. Benchmark → submenu (5 items)
7. Configuration → submenu (8 items)
8. Help → submenu (6 items)
9. Exit

**After:**
1. **Generate** [G] → Direct action with inline prompts
2. **Statistics** [S] → Direct display with live watch option
3. **Database** [D] → Combined tools + monitoring
4. **Settings** [C] → Configuration with inline toggles
5. **Help** [H] → Single page with sections
6. **Quit** [Q]

**Implementation Strategy:**

**Generate Menu** — Simplify to inline prompts:
```go
func handleGenerateMenu(...) {
    fmt.Print("\n  Enter wallet count (or 'b' for batch mode): ")
    input := strings.TrimSpace(readLine(reader))
    
    if input == "b" || input == "batch" {
        // Batch mode
        fmt.Printf("  Enter batch count (1 batch = %d wallets): ", cfg.BatchSize)
        // ... handle batch input
    } else {
        // Direct count
        count, ok := promptPositiveInt(reader, input)
        if ok {
            generateWallets(ctx, pool, cfg, count)
        }
    }
}
```

**Statistics** — Show immediately with watch option:
```go
func handleStatsMenu(...) {
    handleStats(ctx, pool) // Show stats immediately
    
    fmt.Print("\n  [W] Watch live   [R] Refresh   [Enter] Back: ")
    choice := strings.ToLower(strings.TrimSpace(readLine(reader)))
    
    if choice == "w" {
        watchStatsLive(ctx, pool)
    } else if choice == "r" {
        handleStatsMenu(ctx, pool, reader) // Recursive refresh
    }
}
```

**Database + Monitoring** — Combine into one menu:
```go
func handleDatabaseMenu(...) {
    fmt.Print(`
  ┌────────────────────────────────────────────┐
  │           DATABASE & MONITORING            │
  │   1   Health check                         │
  │   2   Connection pool status               │
  │   3   Watch pool live                      │
  │   4   Maintenance recommendations          │
  │   5   Back                                 │
  └────────────────────────────────────────────┘
  Select option: `)
}
```

---

### 2.3 Inline Configuration Hints

**Goal:** Show current settings beside menu items.

**Target State:**
```
│   1 [G]   Generate wallets           count 1,000 · paired       │
│   7 [C]   Settings                   8 workers · checksum on    │
```

**Implementation:**

```go
func printMenu(cfg *config.Config, strip *StatusStrip) {
    genHint := core.Hint(fmt.Sprintf("count %s · batch %d", 
        formatNumber(1000), cfg.BatchSize))
    settingsHint := core.Hint(fmt.Sprintf("%d workers · batch %d",
        cfg.Workers, cfg.BatchSize))
    
    fmt.Printf(`
  │   1 [G]   Generate wallets           %s │
  │   7 [C]   Settings                   %s │
    `, genHint, settingsHint)
}
```

---

## Tier 3: Polish & Refinement

### 3.1 Toggle-Style Settings

**Goal:** Visual toggles instead of text prompts.

**Target State:**
```
│   Checksum EIP55  [▣ on ]                                      │
│   0x on keys      [▢ off]                                      │
```

**Implementation:**

```go
func renderToggle(enabled bool) string {
    if enabled {
        return core.Success("[▣ on ]")
    }
    return core.Hint("[▢ off]")
}

func handleConfigMenu(cfg *config.Config, reader *bufio.Reader) {
    fmt.Printf(`
  │   Checksum EIP55  %s                                      │
  │   0x on keys      %s                                      │
  │   Logging         %s                                      │
    `,
        renderToggle(true),  // Assuming checksum is always on
        renderToggle(false), // Assuming 0x prefix is off
        renderToggle(cfg.EnableLogging))
}
```

---

### 3.2 Inline Validation

**Goal:** Show errors inline without reprinting menu.

**Current:**
```go
fmt.Println(core.Error("\n[ERROR] Invalid option — please choose 1 to 9."))
// Menu gets reprinted, pushing error up
```

**Target:**
```go
func printInlineError(msg string) {
    fmt.Printf("\r\033[K  %s %s\n", core.Error("✗"), msg)
    fmt.Print("  Select option: ")
}

// Usage:
default:
    printInlineError("Invalid option — please choose 1 to 9")
    choice = strings.TrimSpace(readLine(reader))
    // Continue without reprinting menu
```

---

### 3.3 Completion Summary Screen

**Goal:** Professional summary after generation completes.

**Target State:**
```
╭─ ✓ DONE ──────────────────────────────────────────────────────╮
│   1,000 wallets generated in 0.05s                             │
│                                                                │
│     average  18,204 /s        peak     20,180 /s               │
│     workers  8                heap     1.1 MiB                 │
│                                                                │
│   [G] generate more   [⏎] back to menu                         │
╰────────────────────────────────────────────────────────────────╯
```

**Implementation:**

```go
func showCompletionSummary(total int, elapsed time.Duration, peakRate float64, cfg *config.Config) {
    avgRate := float64(total) / elapsed.Seconds()
    
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    heapMB := float64(m.Alloc) / 1024 / 1024
    
    fmt.Printf(`
  ╭─ %s ──────────────────────────────────────────────────────╮
  │   %s wallets generated in %s                             │
  │                                                                │
  │     average  %s /s        peak     %s /s               │
  │     workers  %-16d  heap     %.1f MiB                 │
  │                                                                │
  │   %s generate more   %s back to menu                         │
  ╰────────────────────────────────────────────────────────────────╯
`,
        core.Success("✓ DONE"),
        formatNumber(total),
        elapsed.Round(time.Millisecond),
        formatNumber(int(avgRate)),
        formatNumber(int(peakRate)),
        cfg.Workers,
        heapMB,
        core.Hint("[G]"),
        core.Hint("[⏎]"))
}
```

---

### 3.4 First-Run Tips

**Goal:** Show helpful tip on first launch.

**Implementation:**

```go
// In cli/menu.go, add to Run() function
func Run(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) {
    reader := bufio.NewReader(os.Stdin)
    
    core.ClearScreenIfEnabled()
    printBanner()
    
    // Check for first run marker
    if _, err := os.Stat(".bob-first-run"); os.IsNotExist(err) {
        showFirstRunTip()
        os.Create(".bob-first-run") // Create marker
    }
    
    // ... rest of function
}

func showFirstRunTip() {
    fmt.Printf(`
  %s
  
  Quick tips:
    • Use letter shortcuts: [G] generate, [S] stats, [Q] quit
    • Press Ctrl+C during generation to stop safely
    • Check [H] Help for detailed guides
    
  Press Enter to continue...
`, core.Info("👋 Welcome to EVM Wallet Generator!"))
    
    bufio.NewReader(os.Stdin).ReadString('\n')
}
```

---

## Fallback Behavior (Non-TTY)

### Detection

Already implemented in `core/colors.go`:
```go
func shouldEnableColors() bool {
    if os.Getenv("NO_COLOR") != "" {
        return false
    }
    if !term.IsTerminal(int(os.Stdout.Fd())) {
        return false
    }
    return true
}
```

### Fallback Strategy

**When `!IsColorEnabled()`:**

1. **Progress Bar** → Milestone printing:
```go
if !IsColorEnabled() {
    // Print every 10% milestone
    pct := float64(current) / float64(p.total) * 100
    if int(pct)%10 == 0 && current > p.lastCount {
        fmt.Printf("Progress: %d/%d (%.0f%%)\n", current, p.total, pct)
        p.lastCount = current
    }
    return
}
```

2. **Status Strip** → Plain text:
```go
if !IsColorEnabled() {
    fmt.Printf("Status: %d wallets in %s\n", s.WalletCount, s.DBName)
    return
}
```

3. **Menus** → No colors, no box drawing (optional):
```go
if !IsColorEnabled() {
    fmt.Print(`
MAIN MENU
1. Generate wallets
2. Statistics
...
`)
    return
}
```

4. **No Screen Clearing** — Already handled by `ClearScreenIfEnabled()`

---

## Testing Strategy

### Manual Testing Checklist

**Tier 1:**
- [ ] Live progress bar displays correctly (1,000 wallets)
- [ ] Spinner animates smoothly
- [ ] Speed and ETA update in real-time
- [ ] Status strip shows correct wallet count
- [ ] Colors display correctly in terminal
- [ ] `NO_COLOR=1` disables colors
- [ ] Piped output works: `./evmwalletbot | tee log.txt`
- [ ] Screen clearing works between menus

**Tier 2:**
- [ ] Letter shortcuts work (G, S, L, D, M, B, C, H, Q)
- [ ] Flattened menus reduce navigation depth
- [ ] Inline hints display current settings
- [ ] Combined Database+Monitoring menu works

**Tier 3:**
- [ ] Toggle UI displays correctly
- [ ] Inline validation doesn't reprint menu
- [ ] Completion summary shows all stats
- [ ] First-run tip appears once

### Automated Testing

**Unit Tests:**
```go
// core/progress_test.go
func TestProgressBar(t *testing.T) {
    tests := []struct {
        frac     float64
        width    int
        expected string
    }{
        {0.0, 10, "░░░░░░░░░░"},
        {0.5, 10, "█████░░░░░"},
        {1.0, 10, "██████████"},
    }
    
    for _, tt := range tests {
        result := progressBar(tt.frac, tt.width)
        if result != tt.expected {
            t.Errorf("progressBar(%.1f, %d) = %s, want %s",
                tt.frac, tt.width, result, tt.expected)
        }
    }
}
```

---

## Rollout Plan

### Phase 1: Foundation (Week 1)
- [ ] Implement `core/progress.go` with live progress bar
- [ ] Integrate progress tracker into `core/generator.go`
- [ ] Add status strip to home screen
- [ ] Enhance color usage in main menu
- [ ] Test with various terminal emulators

### Phase 2: Navigation (Week 2)
- [ ] Add letter shortcut mapping
- [ ] Flatten Generate menu to inline prompts
- [ ] Flatten Statistics menu to direct display
- [ ] Combine Database + Monitoring menus
- [ ] Add inline configuration hints

### Phase 3: Polish (Week 3)
- [ ] Implement toggle-style settings UI
- [ ] Add inline validation (no menu reprints)
- [ ] Create completion summary screen
- [ ] Add first-run tips
- [ ] Comprehensive testing and bug fixes

### Phase 4: Documentation (Week 4)
- [ ] Update README with new UI screenshots
- [ ] Create user guide for new features
- [ ] Document keyboard shortcuts
- [ ] Add troubleshooting section for TTY issues

---

## Optional: Bubble Tea Integration

If you decide to use Bubble Tea for Tier 3 polish, here's the integration path:

### Dependencies
```bash
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
```

### Model Structure
```go
type model struct {
    menu        string // "main", "generate", "settings"
    cursor      int
    choices     []string
    selected    map[int]struct{}
    cfg         *config.Config
    pool        *pgxpool.Pool
}

func (m model) Init() tea.Cmd {
    return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "ctrl+c", "q":
            return m, tea.Quit
        case "up", "k":
            if m.cursor > 0 {
                m.cursor--
            }
        case "down", "j":
            if m.cursor < len(m.choices)-1 {
                m.cursor++
            }
        case "enter", " ":
            // Handle selection
        }
    }
    return m, nil
}

func (m model) View() string {
    // Render menu with lipgloss styling
}
```

**Pros:**
- Professional arrow-key navigation
- Smooth animations
- Consistent styling
- Built-in input handling

**Cons:**
- New dependency (violates "lazy" principle for simple cases)
- Learning curve
- Overkill for basic menus

**Recommendation:** Start with stdlib (Tier 1-2), evaluate Bubble Tea for Tier 3 if desired.

---

## Success Criteria

### Tier 1 Complete When:
- ✅ Live progress bar displays during generation
- ✅ Status strip shows on home screen
- ✅ Colors enhance visual hierarchy
- ✅ Screen clears between menus
- ✅ Fallback works for non-TTY environments

### Tier 2 Complete When:
- ✅ Letter shortcuts work for all main menu items
- ✅ Menu depth reduced from 16 screens to ~8 screens
- ✅ Inline hints show current settings
- ✅ Navigation feels faster and more intuitive

### Tier 3 Complete When:
- ✅ Toggle UI implemented for boolean settings
- ✅ Inline validation doesn't disrupt flow
- ✅ Completion summary provides professional closure
- ✅ First-run experience is welcoming

---

## Risk Mitigation

### Risk: Terminal Compatibility Issues
**Mitigation:** Comprehensive TTY detection and fallback behavior

### Risk: Performance Impact of Live Updates
**Mitigation:** Update interval of 120ms (8 FPS) is negligible

### Risk: Breaking Existing Scripts
**Mitigation:** Respect `NO_COLOR` and provide `--plain` flag

### Risk: Scope Creep
**Mitigation:** Strict tier boundaries, optional Tier 3

---

## Appendix: ANSI Reference

### Cursor Control
```
\033[H        Move to home (0,0)
\033[{n}A     Move up n lines
\033[{n}B     Move down n lines
\033[{n}C     Move right n columns
\033[{n}D     Move left n columns
```

### Screen Control
```
\033[2J       Clear entire screen
\033[K        Clear from cursor to end of line
\r            Carriage return (move to start of line)
```

### Cursor Visibility
```
\033[?25l     Hide cursor
\033[?25h     Show cursor
```

### Color Codes
```
\033[0m       Reset all attributes
\033[1m       Bold
\033[2m       Dim
\033[31m      Red
\033[32m      Green
\033[33m      Yellow
\033[34m      Blue
\033[36m      Cyan
\033[90m      Gray
```

---

## Conclusion

This implementation plan provides a clear, phased approach to redesigning the CLI UI/UX while respecting the "lazy senior dev" philosophy. The plan:

1. **Leverages existing infrastructure** (colors.go, atomic counters)
2. **Uses stdlib first** (no new dependencies for Tier 1-2)
3. **Provides clear integration points** (specific files and line numbers)
4. **Includes fallback behavior** (non-TTY environments)
5. **Defines success criteria** (testable outcomes)
6. **Offers optional enhancement path** (Bubble Tea for Tier 3)

**Next Steps:**
1. Review and approve this plan
2. Create feature branch: `feature/cli-redesign`
3. Implement Tier 1 (highest impact)
4. Test thoroughly with various terminals
5. Iterate based on feedback
6. Proceed to Tier 2 and 3 as desired

**Estimated Effort:**
- Tier 1: 2-3 days
- Tier 2: 2-3 days
- Tier 3: 3-4 days
- Total: 1-2 weeks for complete redesign

The redesign will transform the CLI from a functional but plain interface into a modern, professional tool that provides excellent user experience while maintaining backward compatibility and respecting terminal limitations.
