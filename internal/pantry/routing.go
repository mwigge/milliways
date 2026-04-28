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
	"time"
)

// RoutingStore provides access to the mw_routing table (learned preferences).
type RoutingStore struct {
	db *sql.DB
}

// RecordOutcome updates routing scores after a dispatch.
// Uses INSERT...ON CONFLICT DO UPDATE to avoid TOCTOU races.
func (s *RoutingStore) RecordOutcome(taskType, fileProfile, kitchen string, success bool, duration float64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	successInc, failureInc := 0, 0
	if success {
		successInc = 1
	} else {
		failureInc = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO mw_routing (task_type, file_profile, kitchen, success_count, failure_count, avg_duration, last_used)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_type, file_profile, kitchen) DO UPDATE SET
			success_count = success_count + ?,
			failure_count = failure_count + ?,
			avg_duration  = (avg_duration * (success_count + failure_count) + ?) / (success_count + failure_count + 1),
			last_used     = ?
	`, taskType, fileProfile, kitchen, successInc, failureInc, duration, now,
		successInc, failureInc, duration, now)
	if err != nil {
		return fmt.Errorf("recording outcome: %w", err)
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
