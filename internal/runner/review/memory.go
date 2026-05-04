package review

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

// NoopMemory is a Memory implementation that discards all calls. Used when
// MemPalace is unavailable or memory is disabled.
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

// callerFn is the function type used to call MemPalace MCP methods.
type callerFn func(ctx context.Context, method string, params any) (json.RawMessage, error)

// MemPalaceMemory is a Memory implementation backed by the MemPalace MCP
// server. All errors from MCP calls are swallowed — memory is best-effort.
type MemPalaceMemory struct {
	call callerFn
}

// NewMemPalaceMemoryWithCaller returns a MemPalaceMemory using the provided
// call function. Intended for production use (pass a real MCP dispatcher) and
// tests (pass a stub).
func NewMemPalaceMemoryWithCaller(fn callerFn) *MemPalaceMemory {
	return &MemPalaceMemory{call: fn}
}

// LoadPrior queries the MemPalace knowledge graph for prior findings in the
// repo. Returns an empty PriorContext (not an error) when MCP is unavailable.
func (m *MemPalaceMemory) LoadPrior(ctx context.Context, repoPath string) (PriorContext, error) {
	params := map[string]any{
		"query": fmt.Sprintf("findings for %s", filepath.Base(repoPath)),
	}
	raw, err := m.call(ctx, "mcp__mempalace__mempalace_kg_query", params)
	if err != nil {
		// MCP unavailable — return empty context silently.
		return PriorContext{}, nil
	}

	type triple struct {
		Subject   string `json:"subject"`
		Predicate string `json:"predicate"`
		Object    string `json:"object"`
	}
	var triples []triple
	if err := json.Unmarshal(raw, &triples); err != nil {
		return PriorContext{}, nil
	}

	findings := make([]Finding, 0, len(triples))
	for _, t := range triples {
		findings = append(findings, Finding{
			Symbol: t.Subject,
			Reason: t.Object,
		})
	}
	return PriorContext{Findings: findings}, nil
}

// StoreFindings stores each finding in the MemPalace knowledge graph after
// checking for duplicates. Errors are swallowed.
func (m *MemPalaceMemory) StoreFindings(ctx context.Context, _ string, group Group, findings []Finding) error {
	for _, f := range findings {
		// Check for duplicates before adding.
		dupParams := map[string]any{
			"subject":   f.Symbol,
			"predicate": "has_issue",
			"object":    f.Reason,
		}
		dupRaw, err := m.call(ctx, "mcp__mempalace__mempalace_check_duplicate", dupParams)
		if err != nil {
			// MCP unavailable — skip this finding.
			continue
		}

		var dupResp struct {
			IsDuplicate bool    `json:"is_duplicate"`
			Similarity  float64 `json:"similarity"`
		}
		if err := json.Unmarshal(dupRaw, &dupResp); err == nil && dupResp.IsDuplicate {
			// Skip duplicate finding.
			continue
		}

		// Store the finding.
		addParams := map[string]any{
			"subject":   f.Symbol,
			"predicate": "has_issue",
			"object":    f.Reason,
		}
		if _, err := m.call(ctx, "mcp__mempalace__mempalace_kg_add", addParams); err != nil {
			// Non-fatal — continue with remaining findings.
			continue
		}
	}

	// Record that this group was reviewed.
	relParams := map[string]any{
		"subject":   group.Dir,
		"predicate": "reviewed_group",
		"object":    fmt.Sprintf("reviewed at %s", time.Now().UTC().Format(time.RFC3339)),
	}
	_, _ = m.call(ctx, "mcp__mempalace__mempalace_kg_add", relParams) //nolint:errcheck // best-effort

	return nil
}

// LogSession writes a diary entry and a knowledge graph node for the completed
// session.
func (m *MemPalaceMemory) LogSession(ctx context.Context, repoPath, model, summary string) error {
	diaryParams := map[string]any{
		"content": fmt.Sprintf("Review of %s with %s: %s", filepath.Base(repoPath), model, summary),
	}
	if _, err := m.call(ctx, "mcp__mempalace__mempalace_diary_write", diaryParams); err != nil {
		return nil // best-effort
	}

	kgParams := map[string]any{
		"subject":   filepath.Base(repoPath),
		"predicate": "last_reviewed",
		"object":    time.Now().UTC().Format(time.RFC3339),
	}
	_, _ = m.call(ctx, "mcp__mempalace__mempalace_kg_add", kgParams) //nolint:errcheck // best-effort

	return nil
}
