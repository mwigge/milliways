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

import "fmt"

// ListRecent returns the n most-recent tickets ordered by started_at DESC.
// If n <= 0, all tickets are returned.
func (s *TicketStore) ListRecent(n int) ([]Ticket, error) {
	query := `
		SELECT id, kitchen, prompt, mode, COALESCE(pid, 0), status,
		       COALESCE(output_path, ''), started_at,
		       COALESCE(completed_at, ''), COALESCE(exit_code, 0), ledger_id
		FROM mw_tickets
		ORDER BY started_at DESC`
	var args []any

	if n > 0 {
		query += " LIMIT ?"
		args = append(args, n)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing recent tickets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tickets []Ticket
	for rows.Next() {
		var t Ticket
		if err := rows.Scan(
			&t.ID, &t.Kitchen, &t.Prompt, &t.Mode, &t.PID, &t.Status,
			&t.OutputPath, &t.StartedAt, &t.CompletedAt, &t.ExitCode, &t.LedgerID,
		); err != nil {
			return nil, fmt.Errorf("scanning ticket: %w", err)
		}
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}

// CountByStatus returns the count of tickets for each distinct status.
func (s *TicketStore) CountByStatus() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM mw_tickets GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("counting tickets by status: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scanning status count: %w", err)
		}
		counts[status] = count
	}
	return counts, rows.Err()
}
