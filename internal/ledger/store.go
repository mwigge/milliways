package ledger

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

const createTableSQL = `
CREATE TABLE IF NOT EXISTS ledger (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	ts          TEXT NOT NULL,
	task_hash   TEXT NOT NULL,
	task_type   TEXT NOT NULL DEFAULT '',
	kitchen     TEXT NOT NULL,
	station     TEXT NOT NULL DEFAULT '',
	file        TEXT NOT NULL DEFAULT '',
	duration_s  REAL NOT NULL DEFAULT 0,
	exit_code   INTEGER NOT NULL DEFAULT 0,
	cost_est_usd REAL NOT NULL DEFAULT 0,
	outcome     TEXT NOT NULL DEFAULT 'success'
);

CREATE INDEX IF NOT EXISTS idx_ledger_kitchen ON ledger(kitchen);
CREATE INDEX IF NOT EXISTS idx_ledger_task_type ON ledger(task_type);
CREATE INDEX IF NOT EXISTS idx_ledger_outcome ON ledger(outcome);
`

// Store provides SQLite-backed ledger persistence.
type Store struct {
	db *sql.DB
}

// OpenStore opens or creates the SQLite ledger database.
func OpenStore(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("creating ledger db directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("opening ledger db: %w", err)
	}

	if _, err := db.Exec(createTableSQL); err != nil {
		return nil, fmt.Errorf("creating ledger table: %w", err)
	}

	return &Store{db: db}, nil
}

// Insert writes a ledger entry to SQLite.
func (s *Store) Insert(e Entry) error {
	_, err := s.db.Exec(
		`INSERT INTO ledger (ts, task_hash, task_type, kitchen, station, file, duration_s, exit_code, cost_est_usd, outcome)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Timestamp, e.TaskHash, e.TaskType, e.Kitchen, e.Station, e.File,
		e.DurationSec, e.ExitCode, e.CostEstUSD, e.Outcome,
	)
	if err != nil {
		return fmt.Errorf("inserting ledger entry: %w", err)
	}
	return nil
}

// KitchenStats returns dispatch count and success count per kitchen.
type KitchenStats struct {
	Kitchen      string
	Dispatches   int
	Successes    int
	SuccessRate  float64
	TotalSeconds float64
}

// Stats returns aggregated statistics per kitchen.
func (s *Store) Stats() ([]KitchenStats, error) {
	rows, err := s.db.Query(`
		SELECT kitchen,
		       COUNT(*) as dispatches,
		       SUM(CASE WHEN outcome = 'success' THEN 1 ELSE 0 END) as successes,
		       SUM(duration_s) as total_seconds
		FROM ledger
		GROUP BY kitchen
		ORDER BY dispatches DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("querying ledger stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var stats []KitchenStats
	for rows.Next() {
		var ks KitchenStats
		if err := rows.Scan(&ks.Kitchen, &ks.Dispatches, &ks.Successes, &ks.TotalSeconds); err != nil {
			return nil, fmt.Errorf("scanning ledger stats: %w", err)
		}
		if ks.Dispatches > 0 {
			ks.SuccessRate = float64(ks.Successes) / float64(ks.Dispatches) * 100
		}
		stats = append(stats, ks)
	}
	return stats, rows.Err()
}

// Total returns the total number of ledger entries.
func (s *Store) Total() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM ledger").Scan(&count)
	return count, err
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
