package main

import (
	"strings"
	"testing"
	"time"
)

// TestRenderSnapshot_TableDriven verifies the per-agent text frame
// formatter handles the realistic permutations: populated fields,
// missing fields (dashes), long session ids (truncated), and zero
// counts (still rendered with the "(—)" placeholder so the layout is
// stable as runners progressively wire metrics).
func TestRenderSnapshot_TableDriven(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 34, 56, 0, time.UTC)

	tests := []struct {
		name         string
		snap         snapshotView
		mustContain  []string
		mustNotMatch []string
	}{
		{
			name: "empty agent — dashes for model and session",
			snap: snapshotView{AgentID: "claude"},
			mustContain: []string{
				"/context — claude",
				"12:34:56",
				"model:       —",
				"session:     —",
				"uptime:      0s",
				"in:       0↑",
				"out:      0↓",
				"cached:   0",
				"tools:       0 (—)",
				"mcp:         0 (—)",
				"files:       0 (—)",
				"cost:        $0.0000",
				"errors_5m:   0",
			},
		},
		{
			name: "populated agent — model, tokens, files",
			snap: snapshotView{
				AgentID:   "codex",
				Model:     "gpt-4o",
				SessionID: "h-42",
				UptimeS:   125.7,
				Tokens:    tokensView{Input: 1234, Output: 567, Cached: 89},
				Tools:     []toolView{{Name: "Read"}, {Name: "Edit"}},
				FilesInContext: []fileView{
					{Path: "main.go", Bytes: 1024},
				},
				CostUSD:  0.1234,
				Errors5m: 2,
			},
			mustContain: []string{
				"/context — codex",
				"model:       gpt-4o",
				"session:     h-42",
				"uptime:      125s",
				"in:       1234↑",
				"out:      567↓",
				"cached:   89",
				"tools:       2 (—)",
				"files:       1 (—)",
				"cost:        $0.1234",
				"errors_5m:   2",
			},
		},
		{
			name: "long session id is truncated to 8 chars",
			snap: snapshotView{
				AgentID:   "minimax",
				SessionID: "0123456789abcdef",
			},
			mustContain: []string{
				"session:     01234567",
			},
			mustNotMatch: []string{
				"0123456789abcdef",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := renderSnapshot(tc.snap, now)
			for _, want := range tc.mustContain {
				if !strings.Contains(got, want) {
					t.Errorf("frame missing %q.\n--- frame ---\n%s", want, got)
				}
			}
			for _, deny := range tc.mustNotMatch {
				if strings.Contains(got, deny) {
					t.Errorf("frame unexpectedly contains %q.\n--- frame ---\n%s", deny, got)
				}
			}
		})
	}
}

// TestRenderAggregate_HeaderAndPerAgentRows verifies the aggregate frame
// emits the totals header and one mini-card row per agent, and that
// dashes back-fill for empty model/session.
func TestRenderAggregate_HeaderAndPerAgentRows(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 9, 0, 0, 0, time.UTC)
	agg := aggregateView{
		Totals: totalsView{
			TokensIn:     100,
			TokensOut:    50,
			Cached:       10,
			CostUSD:      0.0042,
			ActiveAgents: 2,
			Errors5m:     1,
		},
		Agents: []snapshotView{
			{AgentID: "claude", Model: "claude-3-5-sonnet", SessionID: "h-1", Tokens: tokensView{Input: 100}},
			{AgentID: "codex"},
		},
	}
	got := renderAggregate(agg, now)
	want := []string{
		"/context — totals",
		"09:00:00",
		"active:      2 agent(s)",
		"tokens in:   100↑",
		"tokens out:  50↓",
		"cached:      10",
		"cost:        $0.0042",
		"errors_5m:   1",
		"· claude",
		"model=claude-3-5-sonnet",
		"sess=h-1",
		"· codex   ",
		"model=—",
		"sess=—",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("aggregate frame missing %q.\n--- frame ---\n%s", w, got)
		}
	}
}

// TestDash_EmptyVsPopulated documents the dash() helper used throughout
// the formatter. The em-dash is the convention (not "-" or "n/a") to
// match Claude Code's `/context` visual idiom.
func TestDash_EmptyVsPopulated(t *testing.T) {
	t.Parallel()
	if got := dash(""); got != "—" {
		t.Errorf("dash(\"\") = %q, want em-dash", got)
	}
	if got := dash("foo"); got != "foo" {
		t.Errorf("dash(\"foo\") = %q, want passthrough", got)
	}
}

// TestShortSession_Truncation documents the 8-char session truncation
// rule. Used by both per-agent and aggregate frames.
func TestShortSession_Truncation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"short", "short"},
		{"01234567", "01234567"},
		{"0123456789abcdef", "01234567"},
	}
	for _, c := range cases {
		if got := shortSession(c.in); got != c.want {
			t.Errorf("shortSession(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
