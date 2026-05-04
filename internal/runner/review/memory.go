package review

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// defaultSocketPath is the default Unix socket path for the MemPalace daemon.
const defaultSocketPath = ".local/state/milliways/sock"

// NoopMemory is a Memory implementation that discards all calls. Used when
// --no-memory flag is set or MemPalace is unavailable.
type NoopMemory struct{}

// LoadPrior returns an empty PriorContext.
func (NoopMemory) LoadPrior(_ context.Context, _ string) (PriorContext, error) {
	return PriorContext{}, nil
}

// StoreFindings is a no-op.
func (NoopMemory) StoreFindings(_ context.Context, _ string, _ Group, _ []Finding) error {
	return nil
}

// LogSession is a no-op.
func (NoopMemory) LogSession(_ context.Context, _, _, _ string) error {
	return nil
}

// callerFn is the function type used to call MemPalace MCP tools over the
// daemon socket.
type callerFn func(ctx context.Context, method string, params any) (json.RawMessage, error)

// MemPalaceMemory calls the MemPalace MCP tools through a thin JSON-RPC client
// over the daemon Unix socket. All MCP errors are logged at Warn and swallowed
// — memory is best-effort and must never abort a review.
type MemPalaceMemory struct {
	socketPath string
	call       callerFn
}

// NewMemPalaceMemory returns a Memory backed by the MemPalace daemon at
// socketPath. When socketPath is empty the default
// ~/.local/state/milliways/sock path is used.
func NewMemPalaceMemory(socketPath string) Memory {
	if socketPath == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			socketPath = filepath.Join(home, defaultSocketPath)
		}
	}
	m := &MemPalaceMemory{socketPath: socketPath}
	m.call = m.socketCall
	return m
}

// NewMemPalaceMemoryWithCaller returns a MemPalaceMemory that uses fn instead
// of the real Unix socket. Used in tests.
func NewMemPalaceMemoryWithCaller(fn callerFn) Memory {
	return &MemPalaceMemory{call: fn}
}

// LoadPrior queries the MemPalace knowledge graph for prior findings associated
// with repoPath (direction: outgoing, predicate: has_issue). If the MCP call
// fails it returns an empty PriorContext without error — memory is optional.
func (m *MemPalaceMemory) LoadPrior(ctx context.Context, repoPath string) (PriorContext, error) {
	raw, err := m.call(ctx, "mcp__mempalace__mempalace_kg_query", map[string]any{
		"entity":    repoPath,
		"direction": "outgoing",
	})
	if err != nil {
		// Socket unavailable or palace empty — return empty context silently.
		return PriorContext{}, nil
	}

	type triple struct {
		Subject   string `json:"subject"`
		Predicate string `json:"predicate"`
		Object    string `json:"object"`
	}
	var triples []triple
	if err := json.Unmarshal(raw, &triples); err != nil {
		// Unparseable response — treat as empty without error.
		return PriorContext{}, nil
	}

	findings := make([]Finding, 0, len(triples))
	for _, t := range triples {
		if t.Predicate == "has_issue" {
			findings = append(findings, Finding{
				Symbol: t.Subject,
				Reason: t.Object,
			})
		}
	}
	return PriorContext{Findings: findings}, nil
}

// StoreFindings persists each finding to the MemPalace knowledge graph.
// For each finding:
//  1. check_duplicate is called with the finding's Reason content.
//  2. If similarity > 0.85, the finding is skipped as a duplicate.
//  3. Otherwise kg_add is called with subject=Symbol, predicate="has_issue".
//
// After findings, a reviewed_group relation is always recorded for the group.
// All MCP errors are logged at Warn and never returned.
func (m *MemPalaceMemory) StoreFindings(ctx context.Context, repoPath string, group Group, findings []Finding) error {
	for _, f := range findings {
		// Step 1: duplicate check.
		dupRaw, err := m.call(ctx, "mcp__mempalace__mempalace_check_duplicate", map[string]any{
			"content": f.Reason,
		})
		if err != nil {
			slog.Warn("mempalace check_duplicate failed", "error", err, "reason", f.Reason)
			continue
		}

		var dup struct {
			IsDuplicate bool    `json:"is_duplicate"`
			Similarity  float64 `json:"similarity"`
		}
		if parseErr := json.Unmarshal(dupRaw, &dup); parseErr != nil {
			slog.Warn("mempalace check_duplicate parse failed", "error", parseErr)
			continue
		}
		if dup.Similarity > 0.85 {
			continue // duplicate — skip storage
		}

		// Step 2: store the finding.
		symbol := f.Symbol
		if symbol == "" {
			symbol = f.File
		}
		_, err = m.call(ctx, "mcp__mempalace__mempalace_kg_add", map[string]any{
			"subject":       symbol,
			"predicate":     "has_issue",
			"object":        f.Reason,
			"source_closet": repoPath,
		})
		if err != nil {
			slog.Warn("mempalace kg_add finding failed", "error", err, "symbol", symbol)
		}
	}

	// Always record the reviewed_group relation, regardless of per-finding errors.
	_, err := m.call(ctx, "mcp__mempalace__mempalace_kg_add", map[string]any{
		"subject":   repoPath,
		"predicate": "reviewed_group",
		"object":    group.Dir,
	})
	if err != nil {
		slog.Warn("mempalace kg_add reviewed_group failed", "error", err, "dir", group.Dir)
	}

	return nil
}

// LogSession records a diary entry for the completed review session and
// updates the last_reviewed triple in the knowledge graph.
// Both calls are best-effort; errors are logged and not returned.
func (m *MemPalaceMemory) LogSession(ctx context.Context, repoPath, model, summary string) error {
	topic := filepath.Base(repoPath)

	_, err := m.call(ctx, "mcp__mempalace__mempalace_diary_write", map[string]any{
		"agent_name": "ReviewRunner",
		"entry":      fmt.Sprintf("[%s] %s", model, summary),
		"topic":      topic,
	})
	if err != nil {
		slog.Warn("mempalace diary_write failed", "error", err, "topic", topic)
	}

	_, err = m.call(ctx, "mcp__mempalace__mempalace_kg_add", map[string]any{
		"subject":   repoPath,
		"predicate": "last_reviewed",
		"object":    time.Now().Format(time.RFC3339),
	})
	if err != nil {
		slog.Warn("mempalace kg_add last_reviewed failed", "error", err)
	}

	return nil
}

// socketCall is the real transport. For v1 this is a stub that always returns
// an error; the real Unix socket transport will be wired in a follow-up.
// Callers that depend on this path (NewMemPalaceMemory) will fall back
// gracefully because all errors are treated as non-fatal by Memory consumers.
func (m *MemPalaceMemory) socketCall(_ context.Context, _ string, _ any) (json.RawMessage, error) {
	return nil, fmt.Errorf("mempalace socket transport not implemented (socket: %s)", m.socketPath)
}
