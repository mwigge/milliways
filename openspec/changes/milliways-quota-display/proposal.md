## Why

The TUI status bar shows every kitchen as `exhausted until 02:00` even when no quota limits are configured. The root cause is a bug in `pantry.ResetsAt()` — it always returns midnight UTC tomorrow, making every kitchen appear rate-limited.

Additionally, the status bar only shows exhausted/warning states — it never shows how many dispatches remain before a limit is hit, or whether usage is trending up or down.

## What Changes

1. **Bug fix**: `ResetsAt(kitchen, dailyLimit)` takes the configured limit as a parameter and returns zero time when unconstrained (no limit, no override).
2. **Remaining count**: `Remaining(kitchen, dailyLimit)` returns dispatches left until the daily limit.
3. **Trend indicator**: `Trend(kitchen)` returns a percentage comparing today's hourly dispatch rate vs yesterday's same window — `↑12%` means usage is trending up.
4. **KitchenState fields**: `Remaining int` and `Trend string` added to `KitchenState` struct.
5. **Rich status display**: Status bar shows `claude 12/50 ↑8%` (12 remaining of 50, trending up 8%) instead of `claude ✓`.

## Non-Goals

- No new quota enforcement logic — only display improvements
- No changes to `maitre.QuotaCheck` or `pantry.IsExhausted()`
- No changes to how quotas are stored
