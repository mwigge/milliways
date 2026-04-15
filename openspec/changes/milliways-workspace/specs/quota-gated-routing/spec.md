# Spec: quota-gated-routing

## Overview

The sommelier MUST check kitchen quotas before routing. Exhausted kitchens MUST be skipped with a clear reason. The user MUST see quota state at all times.

## Requirements

### Configuration

- carte.yaml MUST support per-kitchen `daily_limit` (int, dispatches per day, 0 = unlimited)
- carte.yaml MUST support per-kitchen `daily_minutes` (float, total dispatch minutes per day, 0 = unlimited)
- carte.yaml MUST support per-kitchen `warn_threshold` (float, 0.0-1.0, default 0.8)

### Quota checking

- The sommelier MUST check `QuotaStore.IsExhausted()` for every candidate kitchen before selecting it
- When a kitchen is exhausted, it MUST be skipped and the next candidate checked
- The Decision.Reason MUST explain the skip: `"kitchen exhausted (N/N today, resets HH:MM) → fallback other_kitchen"`
- If ALL kitchens are exhausted, the sommelier MUST return a Decision with empty Kitchen and reason explaining the situation

### Auto-detection from adapters

- When the TUI receives EventRateLimit with Status "exhausted", it MUST call `QuotaStore.MarkExhausted(kitchen, resetsAt)`
- The rate-limit-detected exhaustion MUST take precedence over manual daily_limit counting
- When `resetsAt` has passed, the kitchen MUST be considered available again

### Warning threshold

- When a kitchen's usage ratio exceeds `warn_threshold`, the status bar MUST show a yellow warning
- The warning MUST include the current usage (e.g., "claude ⚠ 42/50")
- Tier 1 feedback in the process map MUST note when routing to a kitchen near its limit

### Status bar

- The TUI MUST display a status bar showing all kitchen states
- Ready kitchens: name in green with ✓
- Exhausted kitchens: name in red with ✗ and `(resets HH:MM)`
- Warning kitchens: name in yellow with ⚠ and usage fraction
- Not-installed kitchens: omitted from status bar

### Failover behaviour (Option C)

- When a kitchen is rate-limited mid-dispatch, the current dispatch MUST run to completion or failure
- The quota store MUST be updated with the exhaustion state after the dispatch completes
- The NEXT dispatch MUST route around the exhausted kitchen
- This spec only governs pre-dispatch quota gating. Mid-stream provider switching is defined by `provider-continuity` and may continue the same block after exhaustion.
