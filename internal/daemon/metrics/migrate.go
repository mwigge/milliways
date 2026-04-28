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

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// runMigrations applies any unapplied migrations in order. Forward-only:
// a partial failure rolls back the in-flight migration and returns an
// error so the caller refuses to start.
func runMigrations(conn *sql.DB) error {
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
        version    INTEGER PRIMARY KEY,
        applied_at INTEGER NOT NULL
    )`); err != nil {
		return fmt.Errorf("bootstrap schema_version: %w", err)
	}

	current, err := currentVersion(conn)
	if err != nil {
		return fmt.Errorf("read schema_version: %w", err)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if err := applyMigration(conn, m); err != nil {
			return fmt.Errorf("apply migration v%d: %w", m.version, err)
		}
		slog.Info("metrics migration applied", "version", m.version)
	}
	return nil
}

func currentVersion(conn *sql.DB) (int, error) {
	var v sql.NullInt64
	row := conn.QueryRow("SELECT MAX(version) FROM schema_version")
	if err := row.Scan(&v); err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}

func applyMigration(conn *sql.DB, m migration) error {
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if _, err := tx.Exec(m.sql); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("exec sql: %w", err)
	}
	if _, err := tx.Exec(
		"INSERT INTO schema_version(version, applied_at) VALUES (?, ?)",
		m.version, time.Now().Unix(),
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// SchemaVersion returns the highest applied schema version.
func (s *Store) SchemaVersion() (int, error) {
	return currentVersion(s.conn)
}
