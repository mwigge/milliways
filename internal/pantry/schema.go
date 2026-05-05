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

package pantry

// schemaV1 is the initial database schema for milliways.db.
// All tables prefixed mw_ to prevent collision with attached databases.
const schemaV1 = `
CREATE TABLE IF NOT EXISTS mw_schema (
    version     INTEGER PRIMARY KEY,
    applied_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS mw_ledger (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ts            TEXT NOT NULL,
    task_hash     TEXT NOT NULL,
    task_type     TEXT NOT NULL DEFAULT '',
    kitchen       TEXT NOT NULL,
    station       TEXT NOT NULL DEFAULT '',
    file          TEXT NOT NULL DEFAULT '',
    duration_s    REAL NOT NULL DEFAULT 0,
    exit_code     INTEGER NOT NULL DEFAULT 0,
    cost_est_usd  REAL NOT NULL DEFAULT 0,
    outcome       TEXT NOT NULL DEFAULT 'success',
    session_id    TEXT,
    parent_id     INTEGER,
    dispatch_mode TEXT DEFAULT 'sync'
);
CREATE INDEX IF NOT EXISTS idx_mw_ledger_kitchen ON mw_ledger(kitchen);
CREATE INDEX IF NOT EXISTS idx_mw_ledger_outcome ON mw_ledger(outcome);
CREATE INDEX IF NOT EXISTS idx_mw_ledger_ts ON mw_ledger(ts);

CREATE TABLE IF NOT EXISTS mw_tickets (
    id           TEXT PRIMARY KEY,
    kitchen      TEXT NOT NULL,
    prompt       TEXT NOT NULL,
    mode         TEXT NOT NULL,
    pid          INTEGER,
    status       TEXT NOT NULL DEFAULT 'running',
    output_path  TEXT,
    started_at   TEXT NOT NULL,
    completed_at TEXT,
    exit_code    INTEGER,
    ledger_id    INTEGER REFERENCES mw_ledger(id)
);

CREATE TABLE IF NOT EXISTS mw_gitgraph (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    repo         TEXT NOT NULL,
    file_path    TEXT NOT NULL,
    churn_30d    INTEGER NOT NULL DEFAULT 0,
    churn_90d    INTEGER NOT NULL DEFAULT 0,
    authors_30d  INTEGER NOT NULL DEFAULT 0,
    last_author  TEXT,
    last_changed TEXT,
    stability    TEXT NOT NULL DEFAULT 'unknown',
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(repo, file_path)
);
CREATE INDEX IF NOT EXISTS idx_mw_gitgraph_stability ON mw_gitgraph(stability);

CREATE TABLE IF NOT EXISTS mw_quality (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    repo                  TEXT NOT NULL,
    file_path             TEXT NOT NULL,
    function_name         TEXT,
    cyclomatic_complexity INTEGER,
    cognitive_complexity  INTEGER,
    coverage_pct          REAL,
    smell_count           INTEGER NOT NULL DEFAULT 0,
    updated_at            TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(repo, file_path, function_name)
);

CREATE TABLE IF NOT EXISTS mw_deps (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    repo           TEXT NOT NULL,
    package        TEXT NOT NULL,
    version        TEXT NOT NULL,
    latest_version TEXT,
    cve_ids        TEXT,
    lock_file      TEXT,
    updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(repo, package, lock_file)
);

CREATE TABLE IF NOT EXISTS mw_routing (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    task_type     TEXT NOT NULL,
    file_profile  TEXT NOT NULL DEFAULT '',
    kitchen       TEXT NOT NULL,
    success_count INTEGER NOT NULL DEFAULT 0,
    failure_count INTEGER NOT NULL DEFAULT 0,
    avg_duration  REAL NOT NULL DEFAULT 0,
    last_used     TEXT,
    UNIQUE(task_type, file_profile, kitchen)
);

CREATE TABLE IF NOT EXISTS mw_quotas (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kitchen    TEXT NOT NULL,
    date       TEXT NOT NULL,
    dispatches INTEGER NOT NULL DEFAULT 0,
    total_sec  REAL NOT NULL DEFAULT 0,
    failures   INTEGER NOT NULL DEFAULT 0,
    UNIQUE(kitchen, date)
);

CREATE TABLE IF NOT EXISTS mw_quota_overrides (
    kitchen   TEXT PRIMARY KEY,
    resets_at TEXT NOT NULL
);

INSERT OR IGNORE INTO mw_schema (version) VALUES (1);
`

const schemaV2 = `
ALTER TABLE mw_ledger ADD COLUMN conversation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE mw_ledger ADD COLUMN segment_id TEXT NOT NULL DEFAULT '';
ALTER TABLE mw_ledger ADD COLUMN segment_index INTEGER NOT NULL DEFAULT 0;
ALTER TABLE mw_ledger ADD COLUMN end_reason TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_mw_ledger_conversation_id ON mw_ledger(conversation_id);

INSERT OR IGNORE INTO mw_schema (version) VALUES (2);
`

const schemaV3 = `
CREATE TABLE IF NOT EXISTS mw_runtime_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id TEXT NOT NULL DEFAULT '',
    block_id        TEXT NOT NULL DEFAULT '',
    segment_id      TEXT NOT NULL DEFAULT '',
    kind            TEXT NOT NULL,
    provider        TEXT NOT NULL DEFAULT '',
    text            TEXT NOT NULL DEFAULT '',
    at              TEXT NOT NULL,
    fields_json     TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_mw_runtime_events_conversation_id ON mw_runtime_events(conversation_id);
CREATE INDEX IF NOT EXISTS idx_mw_runtime_events_kind ON mw_runtime_events(kind);

INSERT OR IGNORE INTO mw_schema (version) VALUES (3);
`

const schemaV4 = `
CREATE TABLE IF NOT EXISTS mw_checkpoints (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id TEXT NOT NULL,
    checkpoint_id   TEXT NOT NULL,
    block_id        TEXT NOT NULL DEFAULT '',
    segment_id      TEXT NOT NULL DEFAULT '',
    provider        TEXT NOT NULL DEFAULT '',
    reason          TEXT NOT NULL DEFAULT '',
    taken_at        TEXT NOT NULL,
    snapshot_json   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_mw_checkpoints_conversation_id ON mw_checkpoints(conversation_id);
CREATE INDEX IF NOT EXISTS idx_mw_checkpoints_taken_at ON mw_checkpoints(taken_at);

INSERT OR IGNORE INTO mw_schema (version) VALUES (4);
`

const schemaV5 = `
CREATE TABLE IF NOT EXISTS mw_memory_items (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id TEXT NOT NULL DEFAULT '',
    memory_type    TEXT NOT NULL,
    source_kind    TEXT NOT NULL DEFAULT '',
    scope          TEXT NOT NULL DEFAULT '',
    text           TEXT NOT NULL,
    confidence     REAL NOT NULL DEFAULT 0,
    status         TEXT NOT NULL DEFAULT 'active',
    valid_until    TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    invalidated_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_mw_memory_items_type_status ON mw_memory_items(memory_type, status);
CREATE INDEX IF NOT EXISTS idx_mw_memory_items_conversation_id ON mw_memory_items(conversation_id);

INSERT OR IGNORE INTO mw_schema (version) VALUES (5);
`

const schemaV6 = `
CREATE TABLE IF NOT EXISTS mw_parallel_groups (
    id          TEXT PRIMARY KEY,
    prompt      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'running',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_mw_parallel_groups_created_at ON mw_parallel_groups(created_at);
CREATE INDEX IF NOT EXISTS idx_mw_parallel_groups_status ON mw_parallel_groups(status);

CREATE TABLE IF NOT EXISTS mw_parallel_slots (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id     TEXT NOT NULL REFERENCES mw_parallel_groups(id),
    handle       INTEGER NOT NULL,
    provider     TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'running',
    started_at   TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT NOT NULL DEFAULT '',
    tokens_in    INTEGER NOT NULL DEFAULT 0,
    tokens_out   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_mw_parallel_slots_group_id ON mw_parallel_slots(group_id);

INSERT OR IGNORE INTO mw_schema (version) VALUES (6);
`

const schemaV7 = `
CREATE TABLE IF NOT EXISTS mw_security_findings (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    cve_id            TEXT NOT NULL,
    package_name      TEXT NOT NULL,
    installed_version TEXT NOT NULL,
    fixed_in_version  TEXT NOT NULL DEFAULT '',
    severity          TEXT NOT NULL DEFAULT '',
    ecosystem         TEXT NOT NULL DEFAULT '',
    summary           TEXT NOT NULL DEFAULT '',
    scan_source       TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'active',
    first_seen        TEXT NOT NULL DEFAULT (datetime('now')),
    last_seen         TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(cve_id, package_name, installed_version, ecosystem)
);

CREATE INDEX IF NOT EXISTS idx_mw_security_findings_status ON mw_security_findings(status);
CREATE INDEX IF NOT EXISTS idx_mw_security_findings_severity ON mw_security_findings(severity);
CREATE INDEX IF NOT EXISTS idx_mw_security_findings_cve_id ON mw_security_findings(cve_id);

CREATE TABLE IF NOT EXISTS mw_security_accepted_risks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    cve_id       TEXT NOT NULL,
    package_name TEXT NOT NULL,
    reason       TEXT NOT NULL DEFAULT '',
    accepted_at  TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at   TEXT NOT NULL,
    UNIQUE(cve_id, package_name)
);

CREATE INDEX IF NOT EXISTS idx_mw_security_accepted_risks_expires_at ON mw_security_accepted_risks(expires_at);

INSERT OR IGNORE INTO mw_schema (version) VALUES (7);
`
