package pantry

import (
	"database/sql"
	"fmt"
)

// LedgerStore provides access to the mw_ledger table.
type LedgerStore struct {
	db *sql.DB
}

// LedgerEntry represents one dispatch record.
type LedgerEntry struct {
	ID           int64
	Timestamp    string
	TaskHash     string
	TaskType     string
	Kitchen      string
	Station      string
	File         string
	DurationSec  float64
	ExitCode     int
	CostEstUSD   float64
	Outcome      string
	SessionID    string
	ParentID     *int64
	DispatchMode string
}

// Insert writes a ledger entry to the database.
func (s *LedgerStore) Insert(e LedgerEntry) (int64, error) {
	result, err := s.db.Exec(
		`INSERT INTO mw_ledger (ts, task_hash, task_type, kitchen, station, file, duration_s, exit_code, cost_est_usd, outcome, session_id, parent_id, dispatch_mode)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Timestamp, e.TaskHash, e.TaskType, e.Kitchen, e.Station, e.File,
		e.DurationSec, e.ExitCode, e.CostEstUSD, e.Outcome,
		e.SessionID, e.ParentID, e.DispatchMode,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting ledger entry: %w", err)
	}
	return result.LastInsertId()
}

// KitchenStats holds aggregated statistics for one kitchen.
type KitchenStats struct {
	Kitchen      string
	Dispatches   int
	Successes    int
	SuccessRate  float64
	TotalSeconds float64
}

// Stats returns aggregated statistics per kitchen.
func (s *LedgerStore) Stats() ([]KitchenStats, error) {
	rows, err := s.db.Query(`
		SELECT kitchen,
		       COUNT(*) as dispatches,
		       SUM(CASE WHEN outcome = 'success' THEN 1 ELSE 0 END) as successes,
		       SUM(duration_s) as total_seconds
		FROM mw_ledger
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

// StatsDB returns the underlying database connection for complex analytical queries.
func (s *LedgerStore) StatsDB() *sql.DB {
	return s.db
}

// Total returns the total number of ledger entries.
func (s *LedgerStore) Total() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM mw_ledger").Scan(&count)
	return count, err
}
