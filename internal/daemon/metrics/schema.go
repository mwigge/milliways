// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metrics

// schemaV1 is the initial schema for ${state}/metrics.db.
//
// Five tiers — raw, hourly, daily, weekly, monthly — each with the same
// shape. Counter metrics use `value`; histogram metrics use
// (count, sum, min, max, p50, p95, p99); gauges are stored as their
// running mean (`value`) plus `count` so higher-tier roll-ups can take
// a count-weighted average.
//
// `ts` is unix-seconds, bucket-aligned per tier (raw=1s, hourly=hour,
// daily=midnight local, weekly=Mon midnight local, monthly=1st-of-month
// midnight local).
//
// Indexes target the dominant query: `metric, tier, [agent_id,] ts`.
const schemaV1 = `
CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS samples_raw (
    metric    TEXT    NOT NULL,
    agent_id  TEXT    NOT NULL DEFAULT '',
    ts        INTEGER NOT NULL,
    value     REAL    NOT NULL DEFAULT 0,
    count     INTEGER NOT NULL DEFAULT 0,
    sum       REAL    NOT NULL DEFAULT 0,
    min       REAL    NOT NULL DEFAULT 0,
    max       REAL    NOT NULL DEFAULT 0,
    p50       REAL    NOT NULL DEFAULT 0,
    p95       REAL    NOT NULL DEFAULT 0,
    p99       REAL    NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_samples_raw_metric_agent_ts
    ON samples_raw(metric, agent_id, ts);

CREATE TABLE IF NOT EXISTS samples_hourly (
    metric    TEXT    NOT NULL,
    agent_id  TEXT    NOT NULL DEFAULT '',
    ts        INTEGER NOT NULL,
    value     REAL    NOT NULL DEFAULT 0,
    count     INTEGER NOT NULL DEFAULT 0,
    sum       REAL    NOT NULL DEFAULT 0,
    min       REAL    NOT NULL DEFAULT 0,
    max       REAL    NOT NULL DEFAULT 0,
    p50       REAL    NOT NULL DEFAULT 0,
    p95       REAL    NOT NULL DEFAULT 0,
    p99       REAL    NOT NULL DEFAULT 0,
    PRIMARY KEY (metric, agent_id, ts)
);
CREATE INDEX IF NOT EXISTS idx_samples_hourly_metric_agent_ts
    ON samples_hourly(metric, agent_id, ts);

CREATE TABLE IF NOT EXISTS samples_daily (
    metric    TEXT    NOT NULL,
    agent_id  TEXT    NOT NULL DEFAULT '',
    ts        INTEGER NOT NULL,
    value     REAL    NOT NULL DEFAULT 0,
    count     INTEGER NOT NULL DEFAULT 0,
    sum       REAL    NOT NULL DEFAULT 0,
    min       REAL    NOT NULL DEFAULT 0,
    max       REAL    NOT NULL DEFAULT 0,
    p50       REAL    NOT NULL DEFAULT 0,
    p95       REAL    NOT NULL DEFAULT 0,
    p99       REAL    NOT NULL DEFAULT 0,
    PRIMARY KEY (metric, agent_id, ts)
);
CREATE INDEX IF NOT EXISTS idx_samples_daily_metric_agent_ts
    ON samples_daily(metric, agent_id, ts);

CREATE TABLE IF NOT EXISTS samples_weekly (
    metric    TEXT    NOT NULL,
    agent_id  TEXT    NOT NULL DEFAULT '',
    ts        INTEGER NOT NULL,
    value     REAL    NOT NULL DEFAULT 0,
    count     INTEGER NOT NULL DEFAULT 0,
    sum       REAL    NOT NULL DEFAULT 0,
    min       REAL    NOT NULL DEFAULT 0,
    max       REAL    NOT NULL DEFAULT 0,
    p50       REAL    NOT NULL DEFAULT 0,
    p95       REAL    NOT NULL DEFAULT 0,
    p99       REAL    NOT NULL DEFAULT 0,
    PRIMARY KEY (metric, agent_id, ts)
);
CREATE INDEX IF NOT EXISTS idx_samples_weekly_metric_agent_ts
    ON samples_weekly(metric, agent_id, ts);

CREATE TABLE IF NOT EXISTS samples_monthly (
    metric    TEXT    NOT NULL,
    agent_id  TEXT    NOT NULL DEFAULT '',
    ts        INTEGER NOT NULL,
    value     REAL    NOT NULL DEFAULT 0,
    count     INTEGER NOT NULL DEFAULT 0,
    sum       REAL    NOT NULL DEFAULT 0,
    min       REAL    NOT NULL DEFAULT 0,
    max       REAL    NOT NULL DEFAULT 0,
    p50       REAL    NOT NULL DEFAULT 0,
    p95       REAL    NOT NULL DEFAULT 0,
    p99       REAL    NOT NULL DEFAULT 0,
    PRIMARY KEY (metric, agent_id, ts)
);
CREATE INDEX IF NOT EXISTS idx_samples_monthly_metric_agent_ts
    ON samples_monthly(metric, agent_id, ts);
`

// migrations is the ordered list of forward-only schema migrations.
// Adding a migration: append a new entry. Never edit an applied one.
var migrations = []migration{
	{version: 1, sql: schemaV1},
}

type migration struct {
	version int
	sql     string
}
