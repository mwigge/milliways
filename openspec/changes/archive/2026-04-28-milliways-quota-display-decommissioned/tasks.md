# Tasks — milliways-quota-display

## QD-1: Fix ResetsAt() and add Remaining() + Trend() [1 SP]

> All in `internal/pantry/quotas.go`

### QD-1.1 — Fix ResetsAt signature and logic

Change:
```go
func (s *QuotaStore) ResetsAt(kitchen string) (time.Time, error)
```
To:
```go
func (s *QuotaStore) ResetsAt(kitchen string, dailyLimit int) (time.Time, error)
```

New logic:
1. Check `mw_quota_overrides` → if valid override, return it
2. If `dailyLimit <= 0` → return `time.Time{}` (zero = unconstrained)
3. Else → return midnight UTC tomorrow

### QD-1.2 — Add Remaining(kitchen, dailyLimit) int

```go
// Remaining returns dispatches left until daily limit is reached.
// Returns -1 if dailyLimit is 0 (unlimited).
func (s *QuotaStore) Remaining(kitchen string, dailyLimit int) (int, error)
```

Logic: `max(0, dailyLimit - DailyDispatches(kitchen))`; return `-1` if `dailyLimit <= 0`.

### QD-1.3 — Add Trend(kitchen) string

```go
// Trend returns "↑N%", "↓N%", "±0%", or "".
func (s *QuotaStore) Trend(kitchen string) (string, error)
```

Use a single SQL query to compare today vs yesterday dispatch counts over the same elapsed window.
Format: `↑12%` if `ratio > 0.05`, `↓8%` if `ratio < -0.05`, `±0%` otherwise.
Return `""` if either window has 0 dispatches (insufficient data).

### QD-1.4 — Add quotas_test.go tests

Table-driven tests for `Remaining()` and `Trend()`:
- `Remaining`: unlimited kitchen → -1; partial usage → correct remaining; exhausted → 0
- `Trend`: no data → ""; trending up → `↑N%`; trending down → `↓N%`; flat → `±0%`
- `ResetsAt`: with override → override time; unlimited → zero time; limited → midnight

## QD-2: Wire Remaining + Trend into buildKitchenStates [0.5 SP]

> `cmd/milliways/commands.go`

### QD-2.1 — Update buildKitchenStates call site

In `buildKitchenStates()` where `ResetsAt` is called, pass the daily limit:
```go
if resetsAt, err := pdb.Quotas().ResetsAt(name, cfg.Kitchens[name].DailyLimit); err == nil ...
```

### QD-2.2 — Populate Remaining and Trend fields

In `buildKitchenStates()`, after computing `state.Status` and `state.ResetsAt`:
```go
state.Remaining, _ = pdb.Quotas().Remaining(name, cfg.Kitchens[name].DailyLimit)
state.Trend, _ = pdb.Quotas().Trend(name)
```

### QD-2.3 — Go vet / build

`go vet ./...` and `go build ./...` must pass.

## QD-3: KitchenState fields + renderStatusBar update [0.5 SP]

### QD-3.1 — Add Remaining and Trend to KitchenState

In `internal/tui/state.go`:
```go
type KitchenState struct {
    Name       string
    Status     string
    ResetsAt   string
    UsageRatio float64
    Remaining  int    // -1 if unlimited
    Trend      string // "↑N%", "↓N%", "±0%", or ""
}
```

### QD-3.2 — Update renderStatusBar display logic

In `internal/tui/view.go`, update `renderStatusBar()`. Display rules:

| Condition | Display |
|-----------|---------|
| `Status = "ready"` + `Remaining = -1` | `claude ✓` |
| `Status = "ready"` + `Remaining >= 0` | `claude 12/50 ↑8%` |
| `Status = "warning"` | `claude ⚠ 85% (12 left ↑8%)` |
| `Status = "exhausted"` | `claude ✗ (02:00)` (unchanged) |
| `Status = "ready"` + `Remaining >= 0` + `Trend != ""` | append `Trend` to remaining display |
| `Remaining = 0` | `claude 0/50 ↓5%` (at limit but not yet exhausted) |

### QD-3.3 — Update test fixtures

Update all `KitchenState` literals in test files to include `Remaining` and `Trend`:
- `internal/tui/switch_command_test.go`
- `internal/tui/run_targets_test.go`
- `internal/tui/dispatch_state_test.go`

### QD-3.4 — Add view tests for renderStatusBar

Table-driven tests in `internal/tui/view_test.go` covering all display cases above.

## QD-4: Integration + cleanup [0.5 SP]

- [ ] QD-4.1 `go test ./...` passes
- [ ] QD-4.2 `go vet ./...` passes
- [ ] QD-4.3 `go build ./...` passes
- [ ] QD-4.4 `go test -race ./...` passes

- [ ] 🍽️ **Service check** — The kitchen has a window. You can see what's cooking without leaving the table.
