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
	"math"
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

// IsExhausted checks if a kitchen has exceeded its daily limit or been externally rate-limited.
// Returns false if dailyLimit is 0 (unlimited) and no external override exists.
func (s *QuotaStore) IsExhausted(kitchen string, dailyLimit int) (bool, error) {
	// Check external rate-limit override first
	var resetsAtStr string
	err := s.db.QueryRow(
		"SELECT resets_at FROM mw_quota_overrides WHERE kitchen = ?",
		kitchen,
	).Scan(&resetsAtStr)
	if err == nil {
		resetsAt, parseErr := time.Parse(time.RFC3339, resetsAtStr)
		if parseErr == nil && time.Now().Before(resetsAt) {
			return true, nil
		}
		// Override expired — clean it up
		_, _ = s.db.Exec("DELETE FROM mw_quota_overrides WHERE kitchen = ?", kitchen)
	}

	// Check daily limit
	if dailyLimit <= 0 {
		return false, nil
	}

	dispatches, err := s.DailyDispatches(kitchen)
	if err != nil {
		return false, err
	}

	return dispatches >= dailyLimit, nil
}

// MarkExhausted records that a kitchen has been externally rate-limited.
// resetsAt is the time the kitchen reported it will accept requests again.
func (s *QuotaStore) MarkExhausted(kitchen string, resetsAt time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO mw_quota_overrides (kitchen, resets_at)
		VALUES (?, ?)
		ON CONFLICT(kitchen) DO UPDATE SET resets_at = ?
	`, kitchen, resetsAt.Format(time.RFC3339), resetsAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("marking kitchen exhausted: %w", err)
	}
	return nil
}

// ResetsAt returns the time when a kitchen's quota resets.
// For externally rate-limited kitchens, returns the override time.
// For daily-limit kitchens, returns midnight UTC of the next day.
// Returns a zero time when the kitchen has no configured daily limit.
func (s *QuotaStore) ResetsAt(kitchen string, dailyLimit int) (time.Time, error) {
	// Check external override first
	var resetsAtStr string
	err := s.db.QueryRow(
		"SELECT resets_at FROM mw_quota_overrides WHERE kitchen = ?",
		kitchen,
	).Scan(&resetsAtStr)
	if err == nil {
		resetsAt, parseErr := time.Parse(time.RFC3339, resetsAtStr)
		if parseErr == nil && time.Now().Before(resetsAt) {
			return resetsAt, nil
		}
		_, _ = s.db.Exec("DELETE FROM mw_quota_overrides WHERE kitchen = ?", kitchen)
	}

	if dailyLimit <= 0 {
		return time.Time{}, nil
	}

	// Default: midnight UTC tomorrow
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return midnight, nil
}

// Remaining returns dispatches left until daily limit is reached.
// Returns -1 when the kitchen has no configured daily limit.
func (s *QuotaStore) Remaining(kitchen string, dailyLimit int) (int, error) {
	if dailyLimit <= 0 {
		return -1, nil
	}

	dispatches, err := s.DailyDispatches(kitchen)
	if err != nil {
		return 0, err
	}

	remaining := dailyLimit - dispatches
	if remaining < 0 {
		remaining = 0
	}

	return remaining, nil
}

// Trend compares today's dispatch rate to yesterday's over the same elapsed UTC window.
// It returns "↑N%", "↓N%", "±0%", "↑new", "↓new", or "" when there is no data.
func (s *QuotaStore) Trend(kitchen string) (string, error) {
	now := time.Now().UTC()
	todayDate := now.Format("2006-01-02")
	yesterdayDate := now.AddDate(0, 0, -1).Format("2006-01-02")
	currentHour := now.Hour()

	var todayDispatches int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM mw_ledger
		WHERE kitchen = ?
		  AND date(ts) = ?
		  AND CAST(strftime('%H', ts) AS INTEGER) < ?
	`, kitchen, todayDate, currentHour).Scan(&todayDispatches)
	if err != nil {
		return "", fmt.Errorf("querying today's dispatch trend: %w", err)
	}

	var yesterdayDispatches int
	err = s.db.QueryRow(`
		SELECT COUNT(*)
		FROM mw_ledger
		WHERE kitchen = ?
		  AND date(ts) = ?
		  AND CAST(strftime('%H', ts) AS INTEGER) < ?
	`, kitchen, yesterdayDate, currentHour).Scan(&yesterdayDispatches)
	if err != nil {
		return "", fmt.Errorf("querying yesterday's dispatch trend: %w", err)
	}

	if todayDispatches == 0 && yesterdayDispatches == 0 {
		return "", nil
	}
	if yesterdayDispatches == 0 {
		return "↑new", nil
	}
	if todayDispatches == 0 {
		return "↓new", nil
	}

	ratio := float64(todayDispatches) / float64(yesterdayDispatches)
	pctChange := (ratio - 1.0) * 100

	switch {
	case pctChange > 5:
		return fmt.Sprintf("↑%.0f%%", pctChange), nil
	case pctChange < -5:
		return fmt.Sprintf("↓%.0f%%", -pctChange), nil
	default:
		return "±0%", nil
	}
}

// FiveHourDispatches returns the number of dispatches for a kitchen in the last 5 hours.
func (s *QuotaStore) FiveHourDispatches(kitchen string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM mw_ledger WHERE kitchen = ? AND julianday(ts) >= julianday('now','-5 hours')`,
		kitchen,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("querying five-hour dispatches: %w", err)
	}
	return count, nil
}

// WeeklyDispatches returns the number of dispatches for a kitchen in the last 7 days.
func (s *QuotaStore) WeeklyDispatches(kitchen string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM mw_ledger WHERE kitchen = ? AND julianday(ts) >= julianday('now','-7 days')`,
		kitchen,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("querying weekly dispatches: %w", err)
	}
	return count, nil
}

// MonthlyDispatches returns the number of dispatches for a kitchen since the start of the current month.
func (s *QuotaStore) MonthlyDispatches(kitchen string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM mw_ledger WHERE kitchen = ? AND julianday(ts) >= julianday(date('now','start of month'))`,
		kitchen,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("querying monthly dispatches: %w", err)
	}
	return count, nil
}

// UsageRatio returns the dispatches/dailyLimit ratio (0.0-1.0).
// Returns 0.0 if dailyLimit is 0 (unlimited).
func (s *QuotaStore) UsageRatio(kitchen string, dailyLimit int) (float64, error) {
	if dailyLimit <= 0 {
		return 0.0, nil
	}

	dispatches, err := s.DailyDispatches(kitchen)
	if err != nil {
		return 0.0, err
	}

	return math.Min(float64(dispatches)/float64(dailyLimit), 1.0), nil
}
