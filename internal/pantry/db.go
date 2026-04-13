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

	if err := migrate(conn); err != nil {
		return nil, fmt.Errorf("migrating db: %w", err)
	}

	return &DB{conn: conn, path: dbPath}, nil
}

// Ledger returns the ledger store (dispatch history).
func (db *DB) Ledger() *LedgerStore { return &LedgerStore{db: db.conn} }

// Routing returns the routing store (learned kitchen preferences).
func (db *DB) Routing() *RoutingStore { return &RoutingStore{db: db.conn} }

// Quotas returns the quota store (usage tracking).
func (db *DB) Quotas() *QuotaStore { return &QuotaStore{db: db.conn} }

// GitGraph returns the git graph store (file churn and blame).
func (db *DB) GitGraph() *GitGraphStore { return &GitGraphStore{db: db.conn} }

// Quality returns the quality metrics store (complexity and coverage).
func (db *DB) Quality() *QualityStore { return &QualityStore{db: db.conn} }

// Path returns the database file path.
func (db *DB) Path() string { return db.path }

// Close closes the database connection.
func (db *DB) Close() error { return db.conn.Close() }

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

	return nil
}
