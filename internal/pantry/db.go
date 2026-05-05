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

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// DB is the single shared database for all Milliways state.
// One SQLite file, one connection, multiple typed stores.
type DB struct {
	conn *sql.DB
	path string
}

// Open opens or creates the milliways.db file with WAL mode.
func Open(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	conn, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}
	conn.SetMaxOpenConns(1)

	if err := migrate(conn); err != nil {
		return nil, fmt.Errorf("migrating db: %w", err)
	}

	return &DB{conn: conn, path: dbPath}, nil
}

// Ledger returns the ledger store (dispatch history).
func (db *DB) Ledger() *LedgerStore { return &LedgerStore{db: db.conn} }

// Tickets returns the ticket store (async/detached dispatch tracking).
func (db *DB) Tickets() *TicketStore { return &TicketStore{db: db.conn} }

// Routing returns the routing store (learned kitchen preferences).
func (db *DB) Routing() *RoutingStore { return &RoutingStore{db: db.conn} }

// Quotas returns the quota store (usage tracking).
func (db *DB) Quotas() *QuotaStore { return &QuotaStore{db: db.conn} }

// GitGraph returns the git graph store (file churn and blame).
func (db *DB) GitGraph() *GitGraphStore { return &GitGraphStore{db: db.conn} }

// Quality returns the quality metrics store (complexity and coverage).
func (db *DB) Quality() *QualityStore { return &QualityStore{db: db.conn} }

// Deps returns the dependency store (packages, versions, CVEs).
func (db *DB) Deps() *DepStore { return &DepStore{db: db.conn} }

// RuntimeEvents returns the runtime event store.
func (db *DB) RuntimeEvents() *RuntimeEventStore { return &RuntimeEventStore{db: db.conn} }

// Checkpoints returns the checkpoint store.
func (db *DB) Checkpoints() *CheckpointStore { return &CheckpointStore{db: db.conn} }

// MemoryItems returns the durable memory item store.
func (db *DB) MemoryItems() *MemoryItemStore { return &MemoryItemStore{db: db.conn} }

// Parallel returns the parallel-group and slot store.
func (db *DB) Parallel() *ParallelStore { return &ParallelStore{db: db.conn} }

// Path returns the database file path.
func (db *DB) Path() string { return db.path }

// Close closes the database connection.
func (db *DB) Close() error { return db.conn.Close() }

// Ping verifies the database connection is still alive.
func (db *DB) Ping() error {
	return db.conn.Ping()
}

func migrate(conn *sql.DB) error {
	// Check current schema version
	var version int
	err := conn.QueryRow("SELECT COALESCE(MAX(version), 0) FROM mw_schema").Scan(&version)
	if err != nil {
		// Table doesn't exist yet — apply v1
		version = 0
	}

	if version < 1 {
		if _, err := conn.Exec(schemaV1); err != nil {
			return fmt.Errorf("applying schema v1: %w", err)
		}
	}
	if version < 2 {
		if _, err := conn.Exec(schemaV2); err != nil {
			return fmt.Errorf("applying schema v2: %w", err)
		}
	}
	if version < 3 {
		if _, err := conn.Exec(schemaV3); err != nil {
			return fmt.Errorf("applying schema v3: %w", err)
		}
	}
	if version < 4 {
		if _, err := conn.Exec(schemaV4); err != nil {
			return fmt.Errorf("applying schema v4: %w", err)
		}
	}
	if version < 5 {
		if _, err := conn.Exec(schemaV5); err != nil {
			return fmt.Errorf("applying schema v5: %w", err)
		}
	}
	if version < 6 {
		if _, err := conn.Exec(schemaV6); err != nil {
			return fmt.Errorf("applying schema v6: %w", err)
		}
	}

	return nil
}
