# CLI Menu Fix Plan

## Root Cause Analysis

### Issue 1: Broken Line at Line 1149-1150
**Location:** `showMaintenanceRecommendations` function
**Problem:** The parameter `ctx` is split across two lines:
```go
err := pool.QueryRow(ct
x, `SELECT COUNT(*) FROM wallets`).Scan(&walletCount)
```

**Should be:**
```go
err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallets`).Scan(&walletCount)
```

**Cause:** Bare carriage return (`\r`) between "ct" and "x" causing Go parser to see incomplete statement.

### Issue 2: Backtick Count
**Current:** 60 backticks (EVEN count)
**Status:** ✓ Backticks are balanced - no unterminated raw string literal found

### Issue 3: Line Endings
**Search Result:** Only one `\r` found in the file at line 704 in the `readLine` function:
```go
return strings.TrimRight(line, "\r\n")
```
This is **intentional** and correct - it's part of the string literal for trimming line endings.

## Compilation Error

```
cli\menu.go:1149:25: syntax error: unexpected newline in argument list; possibly missing comma or )
```

This error points directly to line 1149 where `ctx` is broken.

## Fix Strategy

### Step 1: Fix the Broken ctx Parameter
**File:** `cli/menu.go`
**Line:** 1149-1150
**Action:** Join the split lines to restore `ctx` as a single parameter

**Before:**
```go
err := pool.QueryRow(ct
x, `SELECT COUNT(*) FROM wallets`).Scan(&walletCount)
```

**After:**
```go
err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallets`).Scan(&walletCount)
```

### Step 2: Verify No Other Split Parameters
Search for other potential line breaks in function calls that might have similar issues.

### Step 3: Run gofmt
After fixing, run `gofmt -w cli/menu.go` to ensure proper formatting.

### Step 4: Verification
1. `go build ./...` - must exit 0
2. `go vet ./...` - must exit 0
3. `gofmt -l .` - must print nothing
4. `go test ./...` - all tests pass
5. Verify no bare `\r` characters remain (except in string literals)

## Implementation Notes

- The task description mentioned 63 bare carriage returns, but current search only found the intentional one in `readLine`
- The task mentioned odd backtick count (68), but current count is 60 (even)
- **This suggests the file may have been partially fixed already, but line 1149 still has the critical break**

## Next Steps

1. Switch to `code` mode (plan mode cannot edit .go files)
2. Apply the fix to line 1149-1150
3. Run all verification commands
4. Confirm compilation success
