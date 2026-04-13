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

INSERT OR IGNORE INTO mw_schema (version) VALUES (1);
`
