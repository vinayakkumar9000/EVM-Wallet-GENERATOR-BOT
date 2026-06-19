# Vanity Wallet Generation Implementation Plan

## Overview
Add CLI-based vanity EVM wallet generation with prefix/suffix matching, difficulty estimation, and live progress tracking.

## Architecture

### 1. Vanity Matching Logic (wallet/vanity.go)

```go
// Core matching function
func MatchesVanity(addr string, prefix, suffix string, checksum bool) bool
```

**Features:**
- Strip "0x" prefix from address
- Case-insensitive mode: lowercase both pattern and address
- Case-sensitive mode: match against EIP-55 checksummed address
- Validate hex characters [0-9a-fA-F]

**Helper Functions:**
- `IsValidHexPattern(pattern string) bool` - Validate hex input
- `CalculateDifficulty(prefix, suffix string, checksum bool) float64`
- `EstimateTime(difficulty float64, speed float64) (time50, time99 time.Duration)`

### 2. Vanity Generation Engine (core/vanity.go)

**Reuse existing patterns:**
- Worker pool from `GenerateWallets()`
- Atomic counters for attempts and matches
- Context cancellation for graceful shutdown
- Wallet pool for object reuse

**New Components:**
```go
type VanityConfig struct {
    Prefix      string
    Suffix      string
    Checksum    bool
    TargetCount int
}

func GenerateVanityWallets(ctx context.Context, pool *pgxpool.Pool, 
    cfg *config.Config, vanity VanityConfig) error
```

**Flow:**
1. Validate prefix/suffix patterns
2. Calculate difficulty
3. Run 1-second calibration to measure speed
4. Display pre-flight panel with estimates
5. Spawn workers that generate and test matches
6. Save matches to database using existing COPY mechanism
7. Display live progress with spinner

### 3. CLI Integration (cli/menu.go)

**Main Menu Addition:**
- Add option "9" for "Vanity address generation"
- Update quit to option "10" (or "0")

**Vanity Menu Flow:**
```
handleVanityMenu(ctx, pool, cfg, reader)
  ├─ Prompt for prefix (optional, validate hex)
  ├─ Prompt for suffix (optional, validate hex)
  ├─ Prompt for checksum mode (y/N)
  ├─ Prompt for wallet count (required, positive int)
  ├─ Validate: at least one of prefix/suffix must be set
  ├─ Calculate and display difficulty panel
  ├─ Confirm to proceed (especially for high difficulty)
  └─ Call GenerateVanityWallets()
```

**Input Validation:**
- Re-prompt on invalid hex (don't crash)
- Clear error messages
- Allow empty input for optional fields

### 4. Difficulty Calculations

**Formula:**
```
characters = len(prefix) + len(suffix)
base_difficulty = 16^characters

if checksum:
    alpha_count = count of [a-fA-F] in pattern
    difficulty = base_difficulty * 2^alpha_count
```

**Probability:**
```
P(success after K attempts) = 1 - (1 - 1/difficulty)^K
50% attempts ≈ difficulty * ln(2)
99% attempts ≈ difficulty * ln(100)
```

**Speed Calibration:**
- Generate wallets for 1 second
- Count attempts
- Calculate addresses/second
- Use for time estimates

### 5. Live Progress Display

**Format:**
```
⠹  tried 412,905  ·  found 1/3  ·  18.2k/s  ·  P≈47%
```

**Components:**
- Spinner: ⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏ (10 frames)
- Attempts counter (formatted with commas)
- Matches found / target
- Current speed (k/s format)
- Rolling probability percentage

**Update frequency:** ~8 FPS (120ms interval)

### 6. Match Display

**On each match found:**
```
✓ MATCH  0xDeAd...12bEEF
  private key  0x3a1f...c9
  attempts     412,905   ·   elapsed 22.6s
```

**Storage:**
- Use existing `insertWalletBatchCopy()` mechanism
- Batch matches (e.g., every 10 matches or on completion)
- Mark with special status or add vanity metadata

### 7. Pre-Flight Panel

**Display before starting:**
```
╔══════════════════════════════════════════════════════════════╗
║                   VANITY GENERATION PREVIEW                  ║
╠══════════════════════════════════════════════════════════════╣
║  Pattern        : 0xdead……beef   (case-insensitive)         ║
║  Difficulty     : 16,777,216                                 ║
║  Calibrating... : ~18,200 addr/s                             ║
║                                                              ║
║  Time Estimates:                                             ║
║    50% chance   : ~16 min                                    ║
║    99% chance   : ~1h 45m                                    ║
║                                                              ║
║  Workers        : 16                                         ║
║  Target matches : 3                                          ║
╚══════════════════════════════════════════════════════════════╝

⚠️  This may take a while. Continue? [y/N]:
```

## Implementation Order

1. **Phase 1: Core Logic**
   - Create `wallet/vanity.go` with matching and validation
   - Add unit tests for `MatchesVanity()`
   - Test case-sensitive and case-insensitive modes

2. **Phase 2: Generation Engine**
   - Create `core/vanity.go` with worker pool
   - Implement difficulty calculations
   - Add speed calibration
   - Test with simple patterns (1-2 chars)

3. **Phase 3: Progress & Display**
   - Implement live progress spinner
   - Add match display formatting
   - Test progress updates

4. **Phase 4: CLI Integration**
   - Add vanity menu option
   - Implement input prompts with validation
   - Add pre-flight panel
   - Test full flow

5. **Phase 5: Polish**
   - Add graceful cancellation (Ctrl+C)
   - Optimize for performance
   - Add comprehensive error handling
   - Update documentation

## Testing Strategy

**Unit Tests:**
- `MatchesVanity()` with various patterns
- Hex validation
- Difficulty calculations
- Probability formulas

**Integration Tests:**
- Generate vanity wallet with 1-char prefix
- Generate with suffix
- Generate with both prefix and suffix
- Test checksum mode
- Test cancellation

**Performance Tests:**
- Measure speed with different worker counts
- Verify no memory leaks with long runs
- Test with high difficulty patterns

## Edge Cases

1. **Empty prefix AND suffix** → Fall back to normal generation
2. **Invalid hex characters** → Re-prompt with clear error
3. **Very high difficulty** → Warn user, allow cancel
4. **Context cancellation** → Save partial results, clean shutdown
5. **Database errors** → Retry with backoff, don't lose matches
6. **Zero matches found** → Handle gracefully, show attempts made

## Code Style Consistency

- Follow existing patterns from `core/generator.go`
- Use `ponytail:` comments for optimizations
- Reuse `walletPool` for object pooling
- Use atomic counters for thread-safe stats
- Match existing error handling style
- Use same progress rendering approach

## Success Criteria Checklist

- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes
- [ ] Menu shows vanity option
- [ ] Prompts for prefix (optional), suffix (optional), checksum, count
- [ ] Validates hex input, re-prompts on error
- [ ] Displays difficulty and time estimates before starting
- [ ] Shows live progress with spinner
- [ ] Displays each match with address and private key
- [ ] Saves matches to database
- [ ] Empty prefix+suffix falls back to normal generation
- [ ] Unit tests for MatchesVanity pass
- [ ] Graceful Ctrl+C handling
