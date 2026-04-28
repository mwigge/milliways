## Current Behavior

```
pantry.ResetsAt(kitchen string) time.Time
```
Always returns midnight UTC tomorrow, even when:
- No `dailyLimit` is configured
- No entry exists in `mw_quota_overrides`

Result: every kitchen shows `exhausted until 02:00` in the TUI status bar.

## Design

### 1. `ResetsAt(kitchen, dailyLimit)` — Bug Fix

Change signature to accept `dailyLimit int` so it knows whether the kitchen has a constraint:

```go
// ResetsAt returns when a kitchen's quota resets.
// Returns zero time if: no daily limit is configured AND no external override exists.
func (s *QuotaStore) ResetsAt(kitchen string, dailyLimit int) (time.Time, error)
```

Logic:
1. Check `mw_quota_overrides` for external rate-limit override → return parsed `resetsAt`
2. If `dailyLimit <= 0` → return `time.Time{}` (zero time = unconstrained)
3. Otherwise → return midnight UTC tomorrow

**Callers updated**:
- `cmd/milliways/commands.go`: `pdb.Quotas().ResetsAt(name, cfg.Kitchens[name].DailyLimit)`
- Any other callers of `ResetsAt` must now pass the limit

### 2. `Remaining(kitchen, dailyLimit) int`

Returns how many dispatches are left before the daily limit is hit.

```go
// Remaining returns dispatches left until daily limit is reached.
// Returns -1 if dailyLimit is 0 (unlimited).
func (s *QuotaStore) Remaining(kitchen string, dailyLimit int) (int, error)
```

Logic:
- If `dailyLimit <= 0` → return `-1`
- `remaining = dailyLimit - DailyDispatches(kitchen)`
- Return `max(0, remaining)`

### 3. `Trend(kitchen) string`

Compares today's hourly dispatch rate to yesterday's same window. Returns `↑N%`, `↓N%`, or `±0%`.

```go
// Trend returns a percentage string comparing today's dispatch rate to yesterday's
// same window. Returns "" if there is no data for comparison.
func (s *QuotaStore) Trend(kitchen string) (string, error)
```

Logic:
1. Query `mw_quotas` for today and yesterday, grouped by hour
2. Sum dispatches for today's elapsed hours vs yesterday's same hours
3. `ratio = todayRate / yesterdayRate - 1`
4. Format as `↑N%` if ratio > 0.05, `↓N%` if ratio < -0.05, `±0%` otherwise
5. Return `""` if either window has 0 dispatches (insufficient data)

SQL approach (single query):
```sql
WITH today AS (
    SELECT COALESCE(SUM(dispatches), 0) as d
    FROM mw_quotas
    WHERE kitchen = ? AND date = ?
      AND CAST(strftime('%H', ts) AS INTEGER) < CAST(strftime('%H', 'now') AS INTEGER)
),
yesterday AS (
    SELECT COALESCE(SUM(dispatches), 0) as d
    FROM mw_quotas
    WHERE kitchen = ? AND date = date('now', '-1 day')
      AND CAST(strftime('%H', ts) AS INTEGER) >= CAST(strftime('%H', 'now') AS INTEGER)
)
SELECT d FROM today;
```

### 4. `KitchenState` Fields

```go
type KitchenState struct {
    Name       string
    Status     string  // "ready", "exhausted", "warning", "not-installed", "disabled"
    ResetsAt   string  // "HH:MM" for exhausted kitchens
    UsageRatio float64 // 0.0-1.0 for warning display
    Remaining  int     // dispatches left until limit, -1 if unlimited
    Trend      string  // "↑N%", "↓N%", "±0%", or ""
}
```

### 5. TUI Status Bar Display

Changes to `renderStatusBar()` in `internal/tui/view.go`:

| Status | Current | New |
|--------|---------|-----|
| ready, unlimited | `claude ✓` | `claude ✓` |
| ready, limited | `claude ✓` | `claude 12/50 ↑8%` |
| warning | `claude ⚠ 85%` | `claude ⚠ 85% (12 left ↑8%)` |
| exhausted | `claude ✗ (02:00)` | `claude ✗ (02:00)` |
| not-installed | _(hidden)_ | _(hidden)_ |

Format for limited-but-ready: `{name} {remaining}/{limit} {trend}`
- `claude 12/50 ↑8%`
- `opencode 50/50 ↓5%` (near limit)
- `gemini -1/-1` → never shown (unlimited = no remaining display)

### 6. Call Chain

```
buildKitchenStates (cmd/milliways/commands.go)
  └── pdb.Quotas().Remaining(name, limit)   → int
  └── pdb.Quotas().Trend(name)              → string
  └── pdb.Quotas().ResetsAt(name, limit)   → time.Time

SetKitchenStates (tui/app.go)
  └── KitchenState{Remaining, Trend} stored in model

renderStatusBar (tui/view.go)
  └── format Remaining + Trend into status string
```

## Files Changed

| File | Change |
|------|--------|
| `internal/pantry/quotas.go` | Add `Remaining()`, fix `ResetsAt()`, add `Trend()` |
| `internal/pantry/quotas_test.go` | Add tests for all three methods |
| `cmd/milliways/commands.go` | Pass `dailyLimit` to `ResetsAt`; populate `Remaining` and `Trend` |
| `internal/tui/state.go` | Add `Remaining int` and `Trend string` to `KitchenState` |
| `internal/tui/view.go` | Update `renderStatusBar()` to show remaining + trend |
| `internal/tui/switch_command_test.go` | Update test fixtures with new `KitchenState` fields |
| `internal/tui/run_targets_test.go` | Update test fixtures with new `KitchenState` fields |
| `internal/tui/dispatch_state_test.go` | Update test fixtures with new `KitchenState` fields |
| `internal/tui/view_test.go` | Update `renderStatusBar` tests |
