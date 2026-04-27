## ADDED Requirements

### Requirement: Five-tier metrics retention

The daemon SHALL persist metric samples in five tiers with the following bucket sizes and retention windows:

| Tier    | Bucket size | Retention window     |
|---------|-------------|----------------------|
| raw     | 1 second    | 60 minutes           |
| hourly  | 1 hour      | 24 hours             |
| daily   | 1 day       | 7 days               |
| weekly  | 1 week      | 28 days (~4 weeks)   |
| monthly | 1 month     | 12 months (~365 days) |

#### Scenario: Sample arrives and is stored at raw tier

- **WHEN** a metric sample is observed (e.g., `tokens_in` increment)
- **THEN** the daemon SHALL persist a row at the `raw` tier within 1s
- **AND** the row SHALL be visible to `metrics.rollup.get({tier: "raw"})` queries

#### Scenario: Sample older than 60 minutes is demoted

- **WHEN** the rollup scheduler ticks and finds a `raw` sample older than 60 minutes
- **THEN** the sample SHALL be aggregated into the corresponding `hourly` bucket
- **AND** the `raw` row SHALL be deleted

#### Scenario: Older buckets cascade through tiers

- **WHEN** the scheduler ticks and finds an `hourly` bucket older than 24 hours
- **THEN** that bucket SHALL be aggregated into the corresponding `daily` bucket and removed
- **AND** the same cascade SHALL apply for `daily` → `weekly` and `weekly` → `monthly`
- **AND** `monthly` buckets older than 12 months SHALL be deleted

### Requirement: SQLite as the storage layer

Metrics SHALL be persisted in a SQLite database at `${state}/metrics.db`. The daemon SHALL create the file on first start and migrate the schema as needed.

#### Scenario: Schema migration on daemon upgrade

- **WHEN** a newer daemon version starts against an existing `metrics.db` with an older schema version
- **THEN** the daemon SHALL apply forward migrations transactionally
- **AND** SHALL log each migration applied
- **AND** SHALL refuse to start if migration fails (no silent data loss)

#### Scenario: Concurrent reads do not block the rollup scheduler

- **WHEN** a cockpit client calls `metrics.rollup.get` while the rollup scheduler is mid-tick
- **THEN** the read SHALL return current data without waiting for the scheduler
- **AND** SHALL never observe a partially-aggregated bucket — neither cross-tier (a row demoted out of `raw` but not yet committed to `hourly`) nor within-tier (a half-built aggregate)
- **AND** the entire demotion cascade across all five tiers SHALL run in a single SQLite write transaction so this guarantee is enforced by the database, not by application-level coordination

### Requirement: Rollup scheduler runs every minute

A daemon-internal scheduler SHALL tick once per minute and perform the demotion cascade. The scheduler SHALL be resilient to skipped ticks (e.g., laptop sleep): on each tick it processes *all* eligible samples, not just those from the last minute.

#### Scenario: Laptop sleeps for two hours

- **WHEN** the daemon resumes after a sleep gap of 2 hours
- **THEN** the next scheduler tick SHALL demote all `raw` samples older than 60min into the correct hourly buckets
- **AND** SHALL also demote any hourly buckets that crossed the 24h boundary
- **AND** the cockpit SHALL see consistent data after one tick

### Requirement: Aggregation rules per metric kind

The daemon SHALL classify every tracked metric as one of `counter`, `histogram`, or `gauge` and apply the matching aggregation when demoting between tiers:

- **Counter** (e.g., `tokens_in`, `tokens_out`, `cost_usd`, `dispatch_count`, `error_count`): higher tier value = `SUM` of lower-tier bucket values.
- **Histogram** (e.g., `dispatch_latency_ms`): each bucket retains `count`, `sum`, `min`, `max`, `p50`, `p95`, `p99`. Higher-tier percentiles are computed by `count`-weighted averaging across constituent buckets and SHALL be documented as approximate.
- **Gauge** (e.g., `mcp_servers_connected`, `active_agents`): higher tier value = `count`-weighted mean of lower-tier bucket means.

#### Scenario: Counter rolls up correctly

- **WHEN** sixty `raw` samples of `tokens_in` over an hour sum to 12,500
- **THEN** the resulting `hourly` bucket SHALL have value 12,500
- **AND** the `daily` bucket spanning that hour SHALL include 12,500 in its sum

#### Scenario: Histogram percentiles labelled approximate

- **WHEN** a client calls `metrics.rollup.get` for a histogram metric at any tier above `raw`
- **THEN** the response SHALL include `approximate: true` for the percentile fields
- **AND** the `count`, `sum`, `min`, `max` fields SHALL be exact (lossless aggregation)
- **AND** the documentation SHALL state that rolled-up percentiles can misrank by 10–30% on long-tailed distributions across heterogeneous buckets — they are suitable for trend comparison, NOT for SLO-violation detection. Only the `raw` and `hourly` tiers are SLO-grade.

### Requirement: Calendar-month boundaries use a configured timezone

Calendar-month boundaries SHALL use a single configured timezone. The default SHALL be the user's local timezone (so "this month" matches the user's intuition), configurable in `milliways.lua` as `milliways.metrics.timezone = "UTC"` for users who prefer UTC bucketing.

#### Scenario: Local timezone bucketing

- **WHEN** the daemon is started with the default config in a host whose local timezone is `Europe/Stockholm`
- **THEN** monthly bucket `ts` values SHALL align to midnight `Europe/Stockholm` on the 1st of each month
- **AND** the year-on-year comparison SHALL produce buckets that match the user's calendar perception

#### Scenario: Timezone change between runs

- **WHEN** a user travels and the host's local timezone changes
- **AND** `milliways.metrics.timezone` is unset (defaulting to local)
- **THEN** new buckets SHALL use the new local timezone
- **AND** existing buckets SHALL retain their original `ts` (no retroactive rebucketing)
- **AND** the daemon SHALL log a warning that comparison across the timezone change may show a one-time discontinuity

### Requirement: `metrics.rollup.get` RPC

The daemon SHALL expose a JSON-RPC method `metrics.rollup.get` for cockpit clients to query historical buckets:

```
metrics.rollup.get({
  metric: string,           // e.g., "tokens_in"
  tier: "raw"|"hourly"|"daily"|"weekly"|"monthly",
  range: { from: ISO8601, to: ISO8601 }?,  // defaults to full retention window
  agent_id: string?,        // optional filter
}) → {
  metric, tier, kind, buckets: [
    { ts: ISO8601, value: number, count: number, ... }
  ],
  approximate: bool         // true for histogram percentiles above raw tier
}
```

#### Scenario: Query last 24 hours of token throughput

- **WHEN** a client calls `metrics.rollup.get({metric: "tokens_in", tier: "hourly"})`
- **THEN** the response SHALL contain up to 24 buckets covering the last 24 hours
- **AND** each bucket SHALL include `ts` (start of hour) and `value` (sum)

#### Scenario: Comparison helper

- **WHEN** a client calls `metrics.rollup.get({metric: "cost_usd", tier: "daily", range: {from: -7d, to: now}})`
- **THEN** the response SHALL contain 7 daily buckets
- **AND** the cockpit SHALL be able to compute "today vs 7-day average" client-side from the result

#### Scenario: Year-on-year query

- **WHEN** a client calls `metrics.rollup.get({metric: "cost_usd", tier: "monthly", range: {from: -12m, to: now}})`
- **THEN** the response SHALL contain up to 12 monthly buckets
- **AND** a follow-up comparison overlay SHALL be able to render "this month vs same month last year" given enough history

### Requirement: Comparison UI is out of scope this change

The `/context` and observability cockpits SHIPPED in this change SHALL NOT include comparison overlays. The data is collected from MVP so a follow-up change can implement comparison views without backfilling.

#### Scenario: Follow-up change can build comparison overlay

- **WHEN** a future change adds a "this hour vs same hour yesterday" overlay
- **THEN** that change SHALL only need to add UI code; it SHALL find usable data already present in `metrics.db`
