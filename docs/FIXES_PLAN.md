# Code Quality Fixes - Implementation Plan

## Executive Summary

Analysis of the codebase revealed **1 critical compile error** and **8 additional issues** ranging from high to low severity. This plan outlines the fixes needed to achieve a production-ready state.

**Current Score: 855/1000** → **Target Score: 950+/1000**

---

## 🔴 Critical Issues (Blocks Compilation)

### Issue #1: Missing Context Parameter in GetStats Call

**Location:** `cmd/main.go:113`

**Problem:**
```go
s, err := core.GetStats(pool)  // ❌ Missing ctx parameter
```

**Function Signature:**
```go
func GetStats(ctx context.Context, pool *pgxpool.Pool) (*Stats, error)
```

**Fix:**
```go
s, err := core.GetStats(ctx, pool)  // ✅ Add ctx parameter
```

**Impact:** Application will not compile

**Effort:** 1 minute

---

## 🟠 High Severity Issues

### Issue #2: Inefficient Trigger Strategy

**Location:** `database/migrations.go` - trigger functions

**Problem:**
Current triggers recalculate entire table counts on every INSERT/UPDATE/DELETE:

```sql
UPDATE system_stats SET
    total_wallets = (SELECT COUNT(*) FROM wallets),
    unused_wallets = (SELECT COUNT(*) FROM wallets WHERE used = false),
    used_wallets = (SELECT COUNT(*) FROM wallets WHERE used = true)
```

**Impact:**
- At 1M wallets: ~100ms per operation
- At 10M wallets: ~1s per operation
- At 50M wallets: ~5s+ per operation
- Defeats the O(1) stats optimization goal

**Solution:**
Replace with increment/decrement counters:

```sql
-- On INSERT
UPDATE system_stats SET
    total_wallets = total_wallets + 1,
    unused_wallets = unused_wallets + 1;

-- On UPDATE (when used changes from false to true)
UPDATE system_stats SET
    unused_wallets = unused_wallets - 1,
    used_wallets = used_wallets + 1;

-- On DELETE
UPDATE system_stats SET
    total_wallets = total_wallets - 1,
    unused_wallets = unused_wallets - CASE WHEN OLD.used = false THEN 1 ELSE 0 END,
    used_wallets = used_wallets - CASE WHEN OLD.used = true THEN 1 ELSE 0 END;
```

**Benefits:**
- O(1) performance regardless of table size
- Consistent with the original optimization goal
- Scales to billions of records

**Effort:** 30 minutes

---

### Issue #3: Database Name Sanitization

**Location:** `database/db.go:EnsureDatabase()`

**Current Approach:**
```go
dbName := strings.ReplaceAll(cfg.DBName, "'", "''")
```

**Problem:**
- Prevents SQL injection but doesn't validate format
- Allows potentially problematic characters
- Could cause issues with database tools

**Solution:**
Add strict validation with regex:

```go
func validateDatabaseName(name string) error {
    // PostgreSQL identifier rules: alphanumeric + underscore, max 63 chars
    if !regexp.MustCompile(`^[a-zA-Z0-9_]{1,63}$`).MatchString(name) {
        return fmt.Errorf("invalid database name: must contain only letters, numbers, and underscores (max 63 chars)")
    }
    return nil
}
```

**Benefits:**
- Prevents edge cases
- Clearer error messages
- Follows PostgreSQL best practices

**Effort:** 15 minutes

---

### Issue #4: Context Ignored in Wallet Info

**Location:** `cli/menu.go` (likely in wallet info display function)

**Problem:**
```go
ctx := context.Background()  // ❌ Replaces incoming context
```

**Impact:**
- Shutdown signals ignored
- Graceful shutdown doesn't work for this operation
- Could cause hanging on exit

**Solution:**
```go
// Use the context passed from main
// ctx parameter should be passed through function chain
```

**Effort:** 10 minutes (need to verify exact location)

---

### Issue #5: Hardcoded Pool Configuration

**Location:** `database/db.go:Connect()`

**Current:**
```go
poolConfig.MaxConns = 30
poolConfig.MinConns = 5
```

**Problem:**
- Not configurable per environment
- Different workloads need different settings
- Production vs development needs differ

**Solution:**
Add to `config/config.go`:

```go
type Config struct {
    // ... existing fields ...
    DBMaxConns int `env:"DB_MAX_CONNS" envDefault:"30"`
    DBMinConns int `env:"DB_MIN_CONNS" envDefault:"5"`
}
```

Update `.env.example`:
```
DB_MAX_CONNS=30
DB_MIN_CONNS=5
```

**Effort:** 15 minutes

---

## 🟡 Medium Severity Issues

### Issue #6: Unnecessary Semaphore

**Location:** `wallet/generator.go` or `core/generator.go`

**Problem:**
Generator already limits concurrency with:
- Worker pool pattern
- Buffered channel (`walletCh`)

Additional semaphore adds complexity without benefit:
```go
sem := make(chan struct{}, workers)
```

**Solution:**
- Review the actual implementation
- If semaphore is redundant, remove it
- If it serves a purpose, document why

**Effort:** 20 minutes (investigation + fix)

---

### Issue #7: Large Function Refactoring

**Location:** `core/generator.go` or `wallet/generator.go`

**Problem:**
`GenerateWallets()` function is becoming very large and handles multiple concerns:
- Worker pool management
- Progress tracking
- Database insertion
- Error handling
- Statistics updates

**Solution:**
Split into focused functions:

```
generator.go      → Main orchestration
workers.go        → Worker pool management
insert.go         → Batch insertion logic
progress.go       → Progress tracking and display
```

**Benefits:**
- Easier to test individual components
- Better code organization
- Simpler to modify specific behaviors

**Effort:** 1-2 hours

---

### Issue #8: Magic Numbers

**Locations:** Multiple files

**Examples:**
```go
1000              // Batch size
500               // Progress update interval
30                // Max connections
5                 // Min connections
200*time.Millisecond  // Retry delay
5*time.Minute     // Health check interval
```

**Solution:**
Move to `config/config.go` or create constants file:

```go
const (
    DefaultBatchSize = 1000
    DefaultProgressInterval = 500
    DefaultRetryDelay = 200 * time.Millisecond
    DefaultHealthCheckInterval = 5 * time.Minute
)
```

**Effort:** 30 minutes

---

## 📋 Implementation Order

### Phase 1: Critical Fix (Required for Compilation)
1. ✅ Fix GetStats context parameter

### Phase 2: High Priority (Performance & Security)
2. Fix trigger strategy (biggest performance impact)
3. Add database name validation
4. Fix context handling in wallet info
5. Make pool configuration configurable

### Phase 3: Code Quality (Maintainability)
6. Review/remove unnecessary semaphore
7. Refactor large functions
8. Extract magic numbers to constants

### Phase 4: Documentation
9. Update IMPLEMENTATION_SUMMARY.md
10. Document trigger optimization approach
11. Update README with new config options

---

## Estimated Timeline

| Phase | Effort | Priority |
|-------|--------|----------|
| Phase 1 | 1 min | CRITICAL |
| Phase 2 | 1.5 hours | HIGH |
| Phase 3 | 2.5 hours | MEDIUM |
| Phase 4 | 30 min | LOW |
| **Total** | **~4.5 hours** | - |

---

## Testing Strategy

After each fix:

1. **Compile Check:** `go build ./cmd`
2. **Unit Tests:** Run existing tests
3. **Integration Test:** 
   - Generate 1000 wallets
   - Check stats accuracy
   - Verify graceful shutdown
4. **Performance Test:**
   - Generate 100k wallets
   - Monitor trigger performance
   - Check memory usage

---

## Risk Assessment

| Issue | Risk if Not Fixed | Mitigation |
|-------|-------------------|------------|
| #1 Compile Error | Application won't run | MUST FIX |
| #2 Trigger Strategy | Performance degrades with scale | HIGH - Fix before production |
| #3 DB Name Validation | Potential injection edge cases | MEDIUM - Current code mostly safe |
| #4 Context Handling | Hanging on shutdown | MEDIUM - Affects UX |
| #5 Pool Config | Suboptimal performance | LOW - Defaults are reasonable |
| #6 Semaphore | Code complexity | LOW - No functional impact |
| #7 Large Function | Maintenance difficulty | LOW - Works correctly |
| #8 Magic Numbers | Configuration inflexibility | LOW - Nice to have |

---

## Success Criteria

- ✅ Application compiles without errors
- ✅ All tests pass
- ✅ Stats queries remain O(1) at any scale
- ✅ Graceful shutdown works in all scenarios
- ✅ Configuration is flexible and documented
- ✅ Code is maintainable and well-organized

---

## Next Steps

1. **Review this plan** - Confirm priorities and approach
2. **Switch to code mode** - Begin implementation
3. **Test incrementally** - Verify each fix works
4. **Update documentation** - Keep docs in sync with code

---

**Ready to proceed?** 

Switch to `code` mode to begin implementation, or provide feedback on this plan.
