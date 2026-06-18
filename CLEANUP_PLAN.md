# Code Cleanup Plan - Final Optimizations

## Current Score: 920/1000 → Target: 950/1000

Based on deep code review, the following cleanup tasks will improve code quality from 920 to 950.

---

## 1. Remove Unused Event Types ✅ READY

**File**: `events/events.go`

**Problem**: Event types defined but never used in actual code:
- `BalanceReceived`
- `TransactionSent`
- `RotationComplete`
- `FaucetClaim`
- `BalanceUpdated`

**Action**: Keep only `WalletCreated` (or remove entirely since we use batch logging now)

**Impact**: Cleaner API, less confusion

---

## 2. Pass Context Instead of Background() ✅ READY

**Files**:
- `events/events.go` - `Log()`, `LogBatch()`, `GetRecent()`
- `core/maintenance.go` - `CollectHealthMetrics()`, `RecordHealthMetrics()`
- `core/stats.go` - `GetStats()`
- `database/db.go` - `EnsureDatabase()`

**Problem**: Functions create `context.Background()` internally, preventing proper cancellation

**Action**: Add `ctx context.Context` as first parameter

**Impact**: Proper shutdown propagation, better resource management

---

## 3. Dynamic Pool Warmup ✅ READY

**File**: `core/generator.go`

**Current**:
```go
for i := 0; i < 1000; i++ {
    walletPool.Put(...)
}
```

**Problem**: Fixed 1000 objects regardless of workload

**Action**: Use dynamic sizing:
```go
warmupSize := cfg.Workers * 32
if warmupSize > 1000 {
    warmupSize = 1000
}
for i := 0; i < warmupSize; i++ {
    walletPool.Put(...)
}
```

**Impact**: Better memory usage for small workloads

---

## 4. Remove UNIQUE Constraint on private_key ✅ READY

**File**: `database/migrations.go`

**Current**:
```sql
private_key BYTEA NOT NULL UNIQUE
```

**Problem**: 
- Collision probability: 1/2^256 (essentially impossible)
- Wastes disk space
- Slows inserts
- Unnecessary index

**Action**: Remove UNIQUE, keep only on address:
```sql
private_key BYTEA NOT NULL
```

**Impact**: Faster inserts, smaller indexes

---

## 5. Remove Unused EventWorkers Config ✅ READY

**File**: `config/config.go`

**Problem**: `EventWorkers` field added but event system removed

**Verification**:
```bash
grep -R "EventWorkers" .
# Should only find definition, no usage
```

**Action**: Remove field and env var parsing

**Impact**: Cleaner config

---

## 6. Remove "Recent Events" Menu Option ✅ READY

**File**: `cli/menu.go`

**Problem**: Menu option 4 shows events, but we moved to batch logging

**Action**: 
- Remove `handleRecentEvents()` function
- Remove menu option 4
- Renumber remaining options

**Impact**: Simpler CLI, matches architecture

---

## 7. Add Basic Test Coverage 🔄 FUTURE

**Files to create**:
- `wallet/generator_test.go`
- `core/generator_test.go`
- `database/migrations_test.go`

**Tests needed**:
```go
// wallet/generator_test.go
func TestGenerate(t *testing.T)
func TestGenerateInto(t *testing.T)
func BenchmarkGenerate(b *testing.B)

// core/generator_test.go
func TestGenerateWallets(t *testing.T)
func BenchmarkGenerateWallets(b *testing.B)

// database/migrations_test.go
func TestMigrate(t *testing.T)
```

**Impact**: Confidence in changes, regression prevention

---

## Implementation Order

### Phase 1: Quick Wins (30 min)
1. Remove unused event types
2. Remove EventWorkers config
3. Remove "Recent Events" menu

### Phase 2: Context Propagation (45 min)
4. Add context parameters to all functions
5. Update all call sites

### Phase 3: Performance Tuning (20 min)
6. Dynamic pool warmup
7. Remove private_key UNIQUE constraint

### Phase 4: Testing (2-3 hours)
8. Add test files
9. Write basic tests
10. Add benchmarks

---

## Expected Score Progression

| Phase | Score | Improvement |
|-------|-------|-------------|
| Current | 920 | Baseline |
| After Phase 1 | 930 | +10 (cleanup) |
| After Phase 2 | 940 | +10 (architecture) |
| After Phase 3 | 945 | +5 (performance) |
| After Phase 4 | 950+ | +5+ (quality) |

---

## Risk Assessment

### Low Risk (Safe to implement immediately):
- Remove unused event types ✅
- Remove EventWorkers config ✅
- Remove menu option ✅
- Dynamic pool warmup ✅

### Medium Risk (Test thoroughly):
- Context propagation (many call sites)
- Remove UNIQUE constraint (schema change)

### High Risk (Requires extensive testing):
- Test coverage (new code, needs validation)

---

## Rollback Plan

All changes are in Git. If issues arise:

```bash
# Rollback to current state
git reset --hard 90f710d

# Or rollback specific file
git checkout 90f710d -- path/to/file
```

---

## Success Criteria

✅ Code compiles without errors
✅ All existing functionality works
✅ No new bugs introduced
✅ Score improves to 950/1000
✅ Codebase is simpler and cleaner

---

**Status**: Ready for implementation
**Estimated Time**: 4-5 hours total
**Priority**: High (final polish before 1.0 release)
