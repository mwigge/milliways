package pantry

import (
	"database/sql"
	"fmt"
	"time"
)

// QuotaStore provides access to the mw_quotas table.
type QuotaStore struct {
	db *sql.DB
}

// Increment records a dispatch for quota tracking.
func (s *QuotaStore) Increment(kitchen string, durationSec float64, failed bool) error {
	date := time.Now().UTC().Format("2006-01-02")
	failureInc := 0
	if failed {
		failureInc = 1
	}

	_, err := s.db.Exec(`
		INSERT INTO mw_quotas (kitchen, date, dispatches, total_sec, failures)
		VALUES (?, ?, 1, ?, ?)
		ON CONFLICT(kitchen, date) DO UPDATE SET
			dispatches = dispatches + 1,
			total_sec = total_sec + ?,
			failures = failures + ?
	`, kitchen, date, durationSec, failureInc, durationSec, failureInc)
	if err != nil {
		return fmt.Errorf("incrementing quota: %w", err)
	}
	return nil
}

// DailyDispatches returns the number of dispatches for a kitchen today.
func (s *QuotaStore) DailyDispatches(kitchen string) (int, error) {
	date := time.Now().UTC().Format("2006-01-02")
	var count int
	err := s.db.QueryRow(
		"SELECT COALESCE(dispatches, 0) FROM mw_quotas WHERE kitchen = ? AND date = ?",
		kitchen, date,
	).Scan(&count)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("querying daily dispatches: %w", err)
	}
	return count, nil
}
