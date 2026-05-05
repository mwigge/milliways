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

package parallel

import (
	"context"
	"strings"
	"testing"
)

// stubMPClient is a test double for MPClient.
type stubMPClient struct {
	triples []KGTriple
	err     error
	// captures the last KGQuery call arguments for assertions
	lastSubjectPrefix string
	lastPredicate     string
	lastFilters       map[string]string
}

func (s *stubMPClient) KGQuery(_ context.Context, subjectPrefix, predicate string, filters map[string]string) ([]KGTriple, error) {
	s.lastSubjectPrefix = subjectPrefix
	s.lastPredicate = predicate
	s.lastFilters = filters
	return s.triples, s.err
}

func (s *stubMPClient) KGAdd(_ context.Context, _, _, _ string, _ map[string]string) error {
	return nil
}

// ---------------------------------------------------------------------------
// jaccardSimilarity
// ---------------------------------------------------------------------------

func TestJaccardSimilarity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    string
		b    string
		want float64
	}{
		{
			name: "identical strings",
			a:    "missing input validation on token parameter",
			b:    "missing input validation on token parameter",
			want: 1.0,
		},
		{
			name: "disjoint strings",
			a:    "alpha beta gamma",
			b:    "delta epsilon zeta",
			want: 0.0,
		},
		{
			name: "empty a",
			a:    "",
			b:    "something here",
			want: 0.0,
		},
		{
			name: "empty b",
			a:    "something here",
			b:    "",
			want: 0.0,
		},
		{
			name: "both empty",
			a:    "",
			b:    "",
			want: 0.0,
		},
		{
			name: "partial overlap three tokens shared of five unique",
			// a tokens: {foo, bar, baz}  b tokens: {foo, bar, qux}
			// intersection: {foo, bar} = 2, union: {foo, bar, baz, qux} = 4
			a:    "foo bar baz",
			b:    "foo bar qux",
			want: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := jaccardSimilarity(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("jaccardSimilarity(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// deduplicateFindings
// ---------------------------------------------------------------------------

func TestDeduplicateFindings(t *testing.T) {
	t.Parallel()

	t.Run("two similar findings merged", func(t *testing.T) {
		t.Parallel()
		// High overlap — should merge into one entry with both sources.
		triples := []KGTriple{
			{
				Subject:   "internal/server/auth.go",
				Predicate: "has_finding",
				Object:    "missing input validation on token parameter",
				Properties:     map[string]string{"source": "claude"},
			},
			{
				Subject:   "internal/server/auth.go",
				Predicate: "has_finding",
				Object:    "missing input validation on the token parameter here",
				Properties:     map[string]string{"source": "codex"},
			},
		}
		got := deduplicateFindings(triples)
		if len(got) != 1 {
			t.Fatalf("expected 1 merged finding, got %d", len(got))
		}
		if len(got[0].sources) != 2 {
			t.Errorf("expected 2 sources, got %d: %v", len(got[0].sources), got[0].sources)
		}
	})

	t.Run("two different findings kept separate", func(t *testing.T) {
		t.Parallel()
		triples := []KGTriple{
			{
				Subject:   "internal/server/auth.go",
				Predicate: "has_finding",
				Object:    "missing input validation on token parameter",
				Properties:     map[string]string{"source": "claude"},
			},
			{
				Subject:   "internal/server/auth.go",
				Predicate: "has_finding",
				Object:    "goroutine leak detected on context cancellation path",
				Properties:     map[string]string{"source": "codex"},
			},
		}
		got := deduplicateFindings(triples)
		if len(got) != 2 {
			t.Errorf("expected 2 separate findings, got %d", len(got))
		}
	})

	t.Run("longer description kept on merge", func(t *testing.T) {
		t.Parallel()
		// Use descriptions with high token overlap so Jaccard >= 0.65.
		// Short: {missing, input, validation, on, token, parameter} = 6 tokens
		// Long:  {missing, input, validation, on, token, parameter, endpoint, check} = 8 tokens
		// intersection = 6, union = 8, Jaccard = 6/8 = 0.75 — above threshold.
		triples := []KGTriple{
			{
				Subject:   "internal/server/auth.go",
				Predicate: "has_finding",
				Object:    "missing input validation on token parameter",
				Properties:     map[string]string{"source": "claude"},
			},
			{
				Subject:   "internal/server/auth.go",
				Predicate: "has_finding",
				Object:    "missing input validation on token parameter endpoint check",
				Properties:     map[string]string{"source": "codex"},
			},
		}
		got := deduplicateFindings(triples)
		if len(got) != 1 {
			t.Fatalf("expected 1 merged finding, got %d", len(got))
		}
		if got[0].description != "missing input validation on token parameter endpoint check" {
			t.Errorf("expected longer description to be kept, got %q", got[0].description)
		}
	})

	t.Run("empty input returns empty output", func(t *testing.T) {
		t.Parallel()
		got := deduplicateFindings(nil)
		if len(got) != 0 {
			t.Errorf("expected empty result for nil input, got %d entries", len(got))
		}
	})
}

// ---------------------------------------------------------------------------
// Aggregate
// ---------------------------------------------------------------------------

func TestAggregate_ThreeProvidersSameFile(t *testing.T) {
	t.Parallel()

	groupID := "grp-abc123"
	// All three descriptions share high token overlap (Jaccard >= 0.65) so
	// they should be merged into a single finding with three sources.
	triples := []KGTriple{
		{
			Subject:   "internal/server/auth.go",
			Predicate: "has_finding",
			Object:    "missing input validation on token parameter request endpoint",
			Properties:     map[string]string{"source": "claude", "group_id": groupID},
		},
		{
			Subject:   "internal/server/auth.go",
			Predicate: "has_finding",
			Object:    "missing input validation on token parameter request endpoint here",
			Properties:     map[string]string{"source": "codex", "group_id": groupID},
		},
		{
			Subject:   "internal/server/auth.go",
			Predicate: "has_finding",
			Object:    "missing input validation on token parameter request endpoint check",
			Properties:     map[string]string{"source": "local", "group_id": groupID},
		},
	}

	mp := &stubMPClient{triples: triples}
	summary, err := Aggregate(context.Background(), groupID, mp)
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	if summary.GroupID != groupID {
		t.Errorf("GroupID = %q, want %q", summary.GroupID, groupID)
	}
	if len(summary.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(summary.Findings))
	}
	if summary.Findings[0].Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %v, want HIGH", summary.Findings[0].Confidence)
	}
	if summary.HighCount != 1 {
		t.Errorf("HighCount = %d, want 1", summary.HighCount)
	}
	if summary.MediumCount != 0 {
		t.Errorf("MediumCount = %d, want 0", summary.MediumCount)
	}
	if summary.LowCount != 0 {
		t.Errorf("LowCount = %d, want 0", summary.LowCount)
	}

	// Verify the KGQuery was called with correct filters
	if mp.lastFilters["group_id"] != groupID {
		t.Errorf("KGQuery called with filters %v, want group_id=%q", mp.lastFilters, groupID)
	}
}

func TestAggregate_ZeroTriples(t *testing.T) {
	t.Parallel()

	mp := &stubMPClient{triples: []KGTriple{}}
	summary, err := Aggregate(context.Background(), "grp-empty", mp)
	if err != nil {
		t.Fatalf("Aggregate returned error for zero triples: %v", err)
	}
	if len(summary.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(summary.Findings))
	}
	if summary.HighCount != 0 || summary.MediumCount != 0 || summary.LowCount != 0 {
		t.Errorf("expected all counts zero, got H=%d M=%d L=%d",
			summary.HighCount, summary.MediumCount, summary.LowCount)
	}
}

func TestAggregate_KGQueryError_ReturnEmptySummary(t *testing.T) {
	t.Parallel()

	mp := &stubMPClient{err: errKGQueryFailed}
	summary, err := Aggregate(context.Background(), "grp-err", mp)
	// Error from KGQuery should NOT surface as an error from Aggregate.
	if err != nil {
		t.Fatalf("Aggregate should swallow KGQuery errors, got: %v", err)
	}
	if len(summary.Findings) != 0 {
		t.Errorf("expected empty summary on KGQuery error, got %d findings", len(summary.Findings))
	}
}

func TestAggregate_ConfidenceBuckets(t *testing.T) {
	t.Parallel()

	groupID := "grp-buckets"
	triples := []KGTriple{
		// File A — 3 distinct sources → HIGH
		{Subject: "a.go", Predicate: "has_finding", Object: "alpha beta gamma delta epsilon zeta eta theta iota kappa", Properties: map[string]string{"source": "claude"}},
		{Subject: "a.go", Predicate: "has_finding", Object: "alpha beta gamma delta epsilon zeta eta theta iota kappa!", Properties: map[string]string{"source": "codex"}},
		{Subject: "a.go", Predicate: "has_finding", Object: "alpha beta gamma delta epsilon zeta eta theta iota kappa!!", Properties: map[string]string{"source": "local"}},
		// File B — 2 distinct sources → MEDIUM
		{Subject: "b.go", Predicate: "has_finding", Object: "goroutine leak on shutdown pathway context handler", Properties: map[string]string{"source": "claude"}},
		{Subject: "b.go", Predicate: "has_finding", Object: "goroutine leak on the shutdown pathway context handler", Properties: map[string]string{"source": "codex"}},
		// File C — 1 source → LOW
		{Subject: "c.go", Predicate: "has_finding", Object: "sql injection risk in query builder function", Properties: map[string]string{"source": "local"}},
	}

	mp := &stubMPClient{triples: triples}
	summary, err := Aggregate(context.Background(), groupID, mp)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	if summary.HighCount != 1 {
		t.Errorf("HighCount = %d, want 1", summary.HighCount)
	}
	if summary.MediumCount != 1 {
		t.Errorf("MediumCount = %d, want 1", summary.MediumCount)
	}
	if summary.LowCount != 1 {
		t.Errorf("LowCount = %d, want 1", summary.LowCount)
	}
}

func TestAggregate_SortOrder(t *testing.T) {
	t.Parallel()

	groupID := "grp-sort"
	// z.go comes after a.go alphabetically; within z.go we have LOW then HIGH order in input
	// but output should be HIGH before LOW.
	triples := []KGTriple{
		{Subject: "z.go", Predicate: "has_finding", Object: "single source finding alone here now", Properties: map[string]string{"source": "local"}},
		{Subject: "a.go", Predicate: "has_finding", Object: "alpha beta gamma delta epsilon zeta eta theta iota kappa", Properties: map[string]string{"source": "claude"}},
		{Subject: "z.go", Predicate: "has_finding", Object: "alpha beta gamma delta epsilon zeta eta theta iota kappa", Properties: map[string]string{"source": "claude"}},
		{Subject: "a.go", Predicate: "has_finding", Object: "alpha beta gamma delta epsilon zeta eta theta iota kappa!", Properties: map[string]string{"source": "codex"}},
		{Subject: "z.go", Predicate: "has_finding", Object: "alpha beta gamma delta epsilon zeta eta theta iota kappa!", Properties: map[string]string{"source": "codex"}},
		{Subject: "a.go", Predicate: "has_finding", Object: "alpha beta gamma delta epsilon zeta eta theta iota kappa!!", Properties: map[string]string{"source": "local"}},
		{Subject: "z.go", Predicate: "has_finding", Object: "alpha beta gamma delta epsilon zeta eta theta iota kappa!!", Properties: map[string]string{"source": "local"}},
	}

	mp := &stubMPClient{triples: triples}
	summary, err := Aggregate(context.Background(), groupID, mp)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	if len(summary.Findings) < 2 {
		t.Fatalf("expected at least 2 findings, got %d", len(summary.Findings))
	}

	// First finding must be from a.go (alphabetically before z.go)
	if summary.Findings[0].File != "a.go" {
		t.Errorf("first finding file = %q, want %q", summary.Findings[0].File, "a.go")
	}

	// Within z.go, HIGH should come before LOW
	var zFindings []AggregatedFinding
	for _, f := range summary.Findings {
		if f.File == "z.go" {
			zFindings = append(zFindings, f)
		}
	}
	if len(zFindings) >= 2 {
		if zFindings[0].Confidence != ConfidenceHigh {
			t.Errorf("within z.go, first finding confidence = %v, want HIGH", zFindings[0].Confidence)
		}
	}
}

// ---------------------------------------------------------------------------
// RenderSummary
// ---------------------------------------------------------------------------

func TestRenderSummary_SeparatorWidth(t *testing.T) {
	t.Parallel()

	s := Summary{
		GroupID:  "abcdef1234567890",
		Findings: []AggregatedFinding{},
	}
	output := RenderSummary(s)
	lines := strings.Split(output, "\n")

	// Find separator lines (lines that are all ─ or start with ──)
	for _, line := range lines {
		if strings.HasPrefix(line, "──") && !strings.Contains(line, "consensus") {
			// Bottom separator should be exactly 68 chars
			if len([]rune(line)) != 68 {
				t.Errorf("separator line length = %d, want 68: %q", len([]rune(line)), line)
			}
		}
	}
}

func TestRenderSummary_HeaderContainsGroupIDPrefix(t *testing.T) {
	t.Parallel()

	s := Summary{
		GroupID:  "abcdef1234567890",
		Findings: []AggregatedFinding{},
	}
	output := RenderSummary(s)
	if !strings.Contains(output, "abcdef12") {
		t.Errorf("header does not contain first 8 chars of GroupID: %q", output)
	}
	// Should NOT contain full ID beyond 8 chars in the header line
	lines := strings.Split(output, "\n")
	headerLine := lines[0]
	if strings.Contains(headerLine, "abcdef1234567890") {
		t.Errorf("header should truncate GroupID to 8 chars, got: %q", headerLine)
	}
}

func TestRenderSummary_NoFindings(t *testing.T) {
	t.Parallel()

	s := Summary{GroupID: "testgroup"}
	output := RenderSummary(s)
	if !strings.Contains(output, "no structured findings") {
		t.Errorf("expected no-findings message, got: %q", output)
	}
}

func TestRenderSummary_PartialFlag(t *testing.T) {
	t.Parallel()

	s := Summary{
		GroupID: "testgroup",
		Partial: true,
		Findings: []AggregatedFinding{
			{
				File:        "pkg/foo/bar.go",
				Description: "some issue found here in the code",
				Confidence:  ConfidenceLow,
				Sources:     []string{"local"},
			},
		},
		LowCount: 1,
	}
	output := RenderSummary(s)
	if !strings.Contains(output, "partial") {
		t.Errorf("expected partial flag in output, got: %q", output)
	}
}

func TestRenderSummary_FindingFormat(t *testing.T) {
	t.Parallel()

	s := Summary{
		GroupID: "abc12345xyz",
		Findings: []AggregatedFinding{
			{
				File:        "internal/server/auth.go",
				Description: "missing input validation on token parameter",
				Confidence:  ConfidenceHigh,
				Sources:     []string{"claude", "codex", "local"},
			},
			{
				File:        "internal/server/auth.go",
				Description: "error not wrapped loses stack trace at L142",
				Confidence:  ConfidenceMedium,
				Sources:     []string{"claude", "codex"},
			},
			{
				File:        "internal/server/handler.go",
				Description: "goroutine leak on context cancellation",
				Confidence:  ConfidenceLow,
				Sources:     []string{"local"},
			},
		},
		HighCount:   1,
		MediumCount: 1,
		LowCount:    1,
	}
	output := RenderSummary(s)

	// File headers must appear
	if !strings.Contains(output, "internal/server/auth.go") {
		t.Errorf("missing file header in output: %q", output)
	}
	if !strings.Contains(output, "internal/server/handler.go") {
		t.Errorf("missing file header in output: %q", output)
	}

	// Confidence labels
	if !strings.Contains(output, "[HIGH]") {
		t.Errorf("missing [HIGH] label: %q", output)
	}
	if !strings.Contains(output, "[MEDIUM]") {
		t.Errorf("missing [MEDIUM] label: %q", output)
	}
	if !strings.Contains(output, "[LOW]") {
		t.Errorf("missing [LOW] label: %q", output)
	}

	// Source list in parens, alphabetically sorted
	if !strings.Contains(output, "(claude, codex, local)") {
		t.Errorf("missing sorted source list: %q", output)
	}

	// Summary line
	if !strings.Contains(output, "1 HIGH") {
		t.Errorf("missing HIGH count in summary: %q", output)
	}
	if !strings.Contains(output, "1 MEDIUM") {
		t.Errorf("missing MEDIUM count in summary: %q", output)
	}
	if !strings.Contains(output, "1 LOW") {
		t.Errorf("missing LOW count in summary: %q", output)
	}
}

func TestRenderSummary_SourcesSortedAlphabetically(t *testing.T) {
	t.Parallel()

	s := Summary{
		GroupID: "sorttest1",
		Findings: []AggregatedFinding{
			{
				File:        "pkg/foo.go",
				Description: "some finding with multiple sources listed",
				Confidence:  ConfidenceHigh,
				Sources:     []string{"local", "claude", "codex"}, // intentionally unsorted
			},
		},
		HighCount: 1,
	}
	output := RenderSummary(s)
	if !strings.Contains(output, "(claude, codex, local)") {
		t.Errorf("sources not sorted alphabetically in output: %q", output)
	}
}

// ---------------------------------------------------------------------------
// ShouldAutoTrigger
// ---------------------------------------------------------------------------

func TestShouldAutoTrigger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		group Group
		want  bool
	}{
		{
			name: "all done",
			group: Group{
				ID: "g1",
				Slots: []SlotRecord{
					{Status: SlotDone},
					{Status: SlotDone},
				},
			},
			want: true,
		},
		{
			name: "all error",
			group: Group{
				ID: "g2",
				Slots: []SlotRecord{
					{Status: SlotError},
					{Status: SlotError},
				},
			},
			want: true,
		},
		{
			name: "mixed done and error",
			group: Group{
				ID: "g3",
				Slots: []SlotRecord{
					{Status: SlotDone},
					{Status: SlotError},
				},
			},
			want: true,
		},
		{
			name: "one still running",
			group: Group{
				ID: "g4",
				Slots: []SlotRecord{
					{Status: SlotDone},
					{Status: SlotRunning},
				},
			},
			want: false,
		},
		{
			name: "all running",
			group: Group{
				ID: "g5",
				Slots: []SlotRecord{
					{Status: SlotRunning},
					{Status: SlotRunning},
				},
			},
			want: false,
		},
		{
			name:  "empty slots",
			group: Group{ID: "g6", Slots: []SlotRecord{}},
			want:  false,
		},
		{
			name:  "nil slots",
			group: Group{ID: "g7"},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ShouldAutoTrigger(tt.group)
			if got != tt.want {
				t.Errorf("ShouldAutoTrigger(%v) = %v, want %v", tt.group.ID, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ConsensusAggregator struct
// ---------------------------------------------------------------------------

func TestConsensusAggregator_DelegatesToPackageLevel(t *testing.T) {
	t.Parallel()

	groupID := "grp-struct-test"
	triples := []KGTriple{
		{
			Subject:   "pkg/foo.go",
			Predicate: "has_finding",
			Object:    "some finding description for the test case",
			Properties:     map[string]string{"source": "local"},
		},
	}

	mp := &stubMPClient{triples: triples}
	ca := &ConsensusAggregator{MP: mp}

	summary, err := ca.Aggregate(context.Background(), groupID)
	if err != nil {
		t.Fatalf("ConsensusAggregator.Aggregate error: %v", err)
	}
	if summary.GroupID != groupID {
		t.Errorf("GroupID = %q, want %q", summary.GroupID, groupID)
	}
	if len(summary.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(summary.Findings))
	}
}

// sentinel used in error test — declared here to avoid magic strings.
var errKGQueryFailed = &testKGQueryError{msg: "kg query failed"}

type testKGQueryError struct{ msg string }

func (e *testKGQueryError) Error() string { return e.msg }
