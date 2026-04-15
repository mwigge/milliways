package pantry

import (
	"database/sql"
	"fmt"
	"strings"
)

// LedgerStore provides access to the mw_ledger table.
type LedgerStore struct {
	db *sql.DB
}

// LedgerEntry represents one dispatch record.
type LedgerEntry struct {
	ID             int64
	Timestamp      string
	TaskHash       string
	TaskType       string
	Kitchen        string
	Station        string
	File           string
	DurationSec    float64
	ExitCode       int
	CostEstUSD     float64
	Outcome        string
	SessionID      string
	ParentID       *int64
	DispatchMode   string
	ConversationID string
	SegmentID      string
	SegmentIndex   int
	EndReason      string
}

// Insert writes a ledger entry to the database.
func (s *LedgerStore) Insert(e LedgerEntry) (int64, error) {
	result, err := s.db.Exec(
		`INSERT INTO mw_ledger (ts, task_hash, task_type, kitchen, station, file, duration_s, exit_code, cost_est_usd, outcome, session_id, parent_id, dispatch_mode, conversation_id, segment_id, segment_index, end_reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Timestamp, e.TaskHash, e.TaskType, e.Kitchen, e.Station, e.File,
		e.DurationSec, e.ExitCode, e.CostEstUSD, e.Outcome,
		e.SessionID, e.ParentID, e.DispatchMode,
		e.ConversationID, e.SegmentID, e.SegmentIndex, e.EndReason,
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

// TaskKitchenStat holds per task-type/kitchen aggregated statistics.
type TaskKitchenStat struct {
	TaskType   string
	Kitchen    string
	Dispatches int
	Successes  int
	Rate       float64
}

// FailoverChain summarizes a multi-segment conversation lineage.
type FailoverChain struct {
	ConversationID string
	Segments       int
	Failovers      int
	Providers      string
}

// TieredStats returns per task-type/kitchen success rates for tiered-CLI analysis.
func (s *LedgerStore) TieredStats() ([]TaskKitchenStat, error) {
	rows, err := s.db.Query(`
		SELECT task_type, kitchen,
		       COUNT(*) as dispatches,
		       SUM(CASE WHEN outcome = 'success' THEN 1 ELSE 0 END) as successes
		FROM mw_ledger
		WHERE task_type != ''
		GROUP BY task_type, kitchen
		ORDER BY task_type, successes DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("querying tiered stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var stats []TaskKitchenStat
	for rows.Next() {
		var ts TaskKitchenStat
		if err := rows.Scan(&ts.TaskType, &ts.Kitchen, &ts.Dispatches, &ts.Successes); err != nil {
			return nil, fmt.Errorf("scanning tiered stats: %w", err)
		}
		if ts.Dispatches > 0 {
			ts.Rate = float64(ts.Successes) / float64(ts.Dispatches) * 100
		}
		stats = append(stats, ts)
	}
	return stats, rows.Err()
}

// Total returns the total number of ledger entries.
func (s *LedgerStore) Total() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM mw_ledger").Scan(&count)
	return count, err
}

// Last returns the most recent ledger entry, or nil if the ledger is empty.
func (s *LedgerStore) Last() (*LedgerEntry, error) {
	var e LedgerEntry
	err := s.db.QueryRow(`
		SELECT id, ts, task_hash, task_type, kitchen, station, file,
		       duration_s, exit_code, cost_est_usd, outcome,
		       COALESCE(session_id, ''), parent_id, COALESCE(dispatch_mode, 'sync'),
		       COALESCE(conversation_id, ''), COALESCE(segment_id, ''), COALESCE(segment_index, 0), COALESCE(end_reason, '')
		FROM mw_ledger ORDER BY id DESC LIMIT 1
	`).Scan(&e.ID, &e.Timestamp, &e.TaskHash, &e.TaskType, &e.Kitchen,
		&e.Station, &e.File, &e.DurationSec, &e.ExitCode, &e.CostEstUSD,
		&e.Outcome, &e.SessionID, &e.ParentID, &e.DispatchMode,
		&e.ConversationID, &e.SegmentID, &e.SegmentIndex, &e.EndReason)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// FailoverChains returns recent multi-segment conversation chains.
func (s *LedgerStore) FailoverChains(limit int) ([]FailoverChain, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.Query(`
		SELECT conversation_id, COUNT(*) as segments,
		       SUM(CASE WHEN end_reason = 'provider exhausted' THEN 1 ELSE 0 END) as failovers
		FROM mw_ledger
		WHERE conversation_id != ''
		GROUP BY conversation_id
		HAVING COUNT(*) > 1
		ORDER BY MAX(id) DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("querying failover chains: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var chains []FailoverChain
	var ids []string
	for rows.Next() {
		var chain FailoverChain
		if err := rows.Scan(&chain.ConversationID, &chain.Segments, &chain.Failovers); err != nil {
			return nil, fmt.Errorf("scanning failover chain: %w", err)
		}
		chains = append(chains, chain)
		ids = append(ids, chain.ConversationID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()

	for i, conversationID := range ids {
		providers, err := s.providersForConversation(conversationID)
		if err != nil {
			return nil, err
		}
		chains[i].Providers = strings.Join(providers, " -> ")
	}
	return chains, nil
}

func (s *LedgerStore) providersForConversation(conversationID string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT kitchen
		FROM mw_ledger
		WHERE conversation_id = ?
		ORDER BY CASE WHEN segment_index > 0 THEN segment_index ELSE id END ASC, id ASC
	`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("querying providers for conversation: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var providers []string
	for rows.Next() {
		var provider string
		if err := rows.Scan(&provider); err != nil {
			return nil, fmt.Errorf("scanning provider chain: %w", err)
		}
		providers = append(providers, provider)
	}
	return providers, rows.Err()
}

// UpdateOutcome updates the outcome of a specific ledger entry.
func (s *LedgerStore) UpdateOutcome(id int64, outcome string) error {
	_, err := s.db.Exec("UPDATE mw_ledger SET outcome = ? WHERE id = ?", outcome, id)
	if err != nil {
		return fmt.Errorf("updating outcome: %w", err)
	}
	return nil
}
