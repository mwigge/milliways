package pantry

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// TicketStore provides access to the mw_tickets table.
type TicketStore struct {
	db *sql.DB
}

// Ticket tracks an async or detached dispatch.
type Ticket struct {
	ID          string
	Kitchen     string
	Prompt      string
	Mode        string // "async", "detached"
	PID         int
	Status      string // "running", "complete", "failed", "timeout"
	OutputPath  string
	StartedAt   string
	CompletedAt string
	ExitCode    int
	LedgerID    *int64
}

// Create inserts a new ticket and returns its ID.
func (s *TicketStore) Create(kitchen, prompt, mode string, pid int, outputPath string) (string, error) {
	id := generateTicketID()

	// Prompt is truncated for display in `milliways tickets` — callers should
	// avoid putting sensitive data in prompts (same as shell history).
	_, err := s.db.Exec(`
		INSERT INTO mw_tickets (id, kitchen, prompt, mode, pid, status, output_path, started_at)
		VALUES (?, ?, ?, ?, ?, 'running', ?, ?)
	`, id, kitchen, truncatePrompt(prompt, 100), mode, pid, outputPath, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return "", fmt.Errorf("creating ticket: %w", err)
	}
	return id, nil
}

// Get returns a ticket by ID.
func (s *TicketStore) Get(id string) (*Ticket, error) {
	var t Ticket
	err := s.db.QueryRow(`
		SELECT id, kitchen, prompt, mode, COALESCE(pid, 0), status, COALESCE(output_path, ''),
		       started_at, COALESCE(completed_at, ''), COALESCE(exit_code, 0), ledger_id
		FROM mw_tickets WHERE id = ?
	`, id).Scan(&t.ID, &t.Kitchen, &t.Prompt, &t.Mode, &t.PID, &t.Status,
		&t.OutputPath, &t.StartedAt, &t.CompletedAt, &t.ExitCode, &t.LedgerID)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting ticket: %w", err)
	}
	return &t, nil
}

// List returns all tickets, optionally filtered by status.
func (s *TicketStore) List(statusFilter string) ([]Ticket, error) {
	query := "SELECT id, kitchen, prompt, mode, COALESCE(pid, 0), status, COALESCE(output_path, ''), started_at, COALESCE(completed_at, ''), COALESCE(exit_code, 0), ledger_id FROM mw_tickets"
	var args []any

	if statusFilter != "" {
		query += " WHERE status = ?"
		args = append(args, statusFilter)
	}
	query += " ORDER BY started_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing tickets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tickets []Ticket
	for rows.Next() {
		var t Ticket
		if err := rows.Scan(&t.ID, &t.Kitchen, &t.Prompt, &t.Mode, &t.PID, &t.Status,
			&t.OutputPath, &t.StartedAt, &t.CompletedAt, &t.ExitCode, &t.LedgerID); err != nil {
			return nil, fmt.Errorf("scanning ticket: %w", err)
		}
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}

// UpdateStatus marks a ticket as complete or failed.
func (s *TicketStore) UpdateStatus(id, status string, exitCode int, ledgerID *int64) error {
	_, err := s.db.Exec(`
		UPDATE mw_tickets SET status = ?, exit_code = ?, completed_at = ?, ledger_id = ?
		WHERE id = ?
	`, status, exitCode, time.Now().UTC().Format(time.RFC3339), ledgerID, id)
	if err != nil {
		return fmt.Errorf("updating ticket: %w", err)
	}
	return nil
}

func generateTicketID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "mw-" + hex.EncodeToString(b)
}

func truncatePrompt(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
