package pantry

import (
	"database/sql"
	"fmt"
	"time"
)

// RoutingStore provides access to the mw_routing table (learned preferences).
type RoutingStore struct {
	db *sql.DB
}

// RecordOutcome updates routing scores after a dispatch.
// Computes running average in Go to avoid SQLite SET clause evaluation order ambiguity (B5).
func (s *RoutingStore) RecordOutcome(taskType, fileProfile, kitchen string, success bool, duration float64) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Read current state
	var curSuccess, curFailure int
	var curAvg float64
	err := s.db.QueryRow(`
		SELECT COALESCE(success_count, 0), COALESCE(failure_count, 0), COALESCE(avg_duration, 0)
		FROM mw_routing WHERE task_type = ? AND file_profile = ? AND kitchen = ?
	`, taskType, fileProfile, kitchen).Scan(&curSuccess, &curFailure, &curAvg)

	if err != nil {
		// Row doesn't exist — insert fresh
		successVal, failureVal := 0, 0
		if success {
			successVal = 1
		} else {
			failureVal = 1
		}
		_, insertErr := s.db.Exec(`
			INSERT INTO mw_routing (task_type, file_profile, kitchen, success_count, failure_count, avg_duration, last_used)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, taskType, fileProfile, kitchen, successVal, failureVal, duration, now)
		if insertErr != nil {
			return fmt.Errorf("inserting routing: %w", insertErr)
		}
		return nil
	}

	// Compute new values in Go (avoids SQL evaluation order issues)
	if success {
		curSuccess++
	} else {
		curFailure++
	}
	total := curSuccess + curFailure
	newAvg := duration
	if total > 1 {
		newAvg = (curAvg*float64(total-1) + duration) / float64(total)
	}

	_, err = s.db.Exec(`
		UPDATE mw_routing SET success_count = ?, failure_count = ?, avg_duration = ?, last_used = ?
		WHERE task_type = ? AND file_profile = ? AND kitchen = ?
	`, curSuccess, curFailure, newAvg, now, taskType, fileProfile, kitchen)
	if err != nil {
		return fmt.Errorf("updating routing: %w", err)
	}
	return nil
}

// BestKitchen returns the kitchen with the highest success rate for a given task type.
// Returns empty string if fewer than minDataPoints entries exist.
func (s *RoutingStore) BestKitchen(taskType, fileProfile string, minDataPoints int) (string, float64, error) {
	var kitchen string
	var rate float64

	err := s.db.QueryRow(`
		SELECT kitchen,
		       CASE WHEN (success_count + failure_count) > 0
		            THEN CAST(success_count AS REAL) / (success_count + failure_count) * 100
		            ELSE 0 END as success_rate
		FROM mw_routing
		WHERE task_type = ? AND file_profile = ?
		  AND (success_count + failure_count) >= ?
		ORDER BY success_rate DESC, avg_duration ASC
		LIMIT 1
	`, taskType, fileProfile, minDataPoints).Scan(&kitchen, &rate)

	if err == sql.ErrNoRows {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("querying best kitchen: %w", err)
	}
	return kitchen, rate, nil
}
