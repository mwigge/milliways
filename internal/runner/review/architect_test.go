package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// architectStubClient is a test double for GroupClient used in architect tests.
type architectStubClient struct {
	findings []Finding
	err      error
	calls    int
}

func (s *architectStubClient) ReviewGroup(_ context.Context, _ Group, _ PriorContext) ([]Finding, error) {
	s.calls++
	return s.findings, s.err
}

// architectMultiStubClient returns different findings per call index.
type architectMultiStubClient struct {
	responses []architectStubResponse
	calls     int
}

type architectStubResponse struct {
	findings []Finding
	err      error
}

func (m *architectMultiStubClient) ReviewGroup(_ context.Context, _ Group, _ PriorContext) ([]Finding, error) {
	idx := m.calls
	m.calls++
	if idx < len(m.responses) {
		return m.responses[idx].findings, m.responses[idx].err
	}
	return nil, nil
}

// makeTracker returns a ContextTracker with the given real files added.
func makeTracker(t *testing.T, files map[string]string) ContextTracker {
	t.Helper()
	dir := t.TempDir()
	tracker := NewContextTracker(dir)
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if err := tracker.Add(p); err != nil {
			t.Fatalf("tracker.Add %s: %v", name, err)
		}
	}
	return tracker
}

// ---- ArchitectEditor tests ----

func TestArchitectEditor_Execute_TwoPhases(t *testing.T) {
	t.Parallel()

	architectClient := &architectStubClient{
		findings: []Finding{
			{Severity: SeverityHigh, Reason: "1. Edit foo.go: fix error handling"},
		},
	}
	editorFinding := Finding{Severity: SeverityHigh, File: "foo.go", Reason: "error not checked"}
	editorClient := &architectStubClient{
		findings: []Finding{editorFinding},
	}

	ae := NewArchitectEditor(architectClient, editorClient)
	tracker := makeTracker(t, map[string]string{"foo.go": "package main\n"})

	findings, plan, err := ae.Execute(context.Background(), t.TempDir(), "fix error handling", tracker)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if len(plan.Steps) != 1 {
		t.Errorf("plan.Steps len = %d, want 1", len(plan.Steps))
	}
	if len(findings) == 0 {
		t.Error("Execute() returned no findings, want at least 1")
	}
	if architectClient.calls != 1 {
		t.Errorf("architect called %d times, want 1", architectClient.calls)
	}
	if editorClient.calls != 1 {
		t.Errorf("editor called %d times, want 1", editorClient.calls)
	}
}

func TestArchitectEditor_Execute_MultipleSteps(t *testing.T) {
	t.Parallel()

	architectClient := &architectStubClient{
		findings: []Finding{
			{
				Severity: SeverityHigh,
				Reason:   "1. Edit a.go: add context\n2. Edit b.go: remove global state\n3. Edit c.go: fix typo",
			},
		},
	}
	editorClient := &architectStubClient{
		findings: []Finding{{Severity: SeverityLow, Reason: "done"}},
	}

	ae := NewArchitectEditor(architectClient, editorClient)
	tracker := makeTracker(t, map[string]string{
		"a.go": "package a\n",
		"b.go": "package b\n",
		"c.go": "package c\n",
	})

	_, plan, err := ae.Execute(context.Background(), t.TempDir(), "refactor", tracker)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if len(plan.Steps) != 3 {
		t.Errorf("plan.Steps len = %d, want 3", len(plan.Steps))
	}
	if editorClient.calls != 3 {
		t.Errorf("editor called %d times, want 3", editorClient.calls)
	}
}

func TestArchitectEditor_Execute_ArchitectError(t *testing.T) {
	t.Parallel()

	sentinelErr := errors.New("architect unavailable")
	architectClient := &architectStubClient{err: sentinelErr}
	editorClient := &architectStubClient{}

	ae := NewArchitectEditor(architectClient, editorClient)
	tracker := makeTracker(t, map[string]string{"x.go": "package x\n"})

	_, _, err := ae.Execute(context.Background(), t.TempDir(), "task", tracker)
	if err == nil {
		t.Fatal("Execute() expected error when architect fails, got nil")
	}
	if !errors.Is(err, sentinelErr) {
		t.Errorf("Execute() error = %v, want chain containing sentinelErr", err)
	}
	if editorClient.calls != 0 {
		t.Errorf("editor called %d times, want 0 (architect failed)", editorClient.calls)
	}
}

func TestArchitectEditor_Execute_EditorError_ContinuesOtherSteps(t *testing.T) {
	t.Parallel()

	sentinelErr := errors.New("editor step 2 failed")

	architectClient := &architectStubClient{
		findings: []Finding{
			{
				Severity: SeverityHigh,
				Reason:   "1. Edit a.go: step one\n2. Edit b.go: step two\n3. Edit c.go: step three",
			},
		},
	}
	editorClient := &architectMultiStubClient{
		responses: []architectStubResponse{
			{findings: []Finding{{Severity: SeverityLow, Reason: "step1 done"}}},
			{err: sentinelErr},
			{findings: []Finding{{Severity: SeverityLow, Reason: "step3 done"}}},
		},
	}

	ae := NewArchitectEditor(architectClient, editorClient)
	tracker := makeTracker(t, map[string]string{
		"a.go": "package a\n",
		"b.go": "package b\n",
		"c.go": "package c\n",
	})

	findings, plan, err := ae.Execute(context.Background(), t.TempDir(), "task", tracker)
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	if len(plan.Steps) != 3 {
		t.Errorf("plan.Steps len = %d, want 3", len(plan.Steps))
	}
	if editorClient.calls != 3 {
		t.Errorf("editor called %d times, want 3 (must continue past error)", editorClient.calls)
	}

	// Verify step 1 and step 3 findings are present.
	var reasons []string
	for _, f := range findings {
		reasons = append(reasons, f.Reason)
	}
	found1 := false
	found3 := false
	for _, r := range reasons {
		if r == "step1 done" {
			found1 = true
		}
		if r == "step3 done" {
			found3 = true
		}
	}
	if !found1 {
		t.Error("step1 finding missing from results")
	}
	if !found3 {
		t.Error("step3 finding missing from results")
	}
}

// ---- parseArchitectPlan tests ----

func TestParseArchitectPlan_NumberedList(t *testing.T) {
	t.Parallel()

	content := "1. Edit a.go: do X\n2. Edit b.go: do Y"
	plan := parseArchitectPlan(content)

	if len(plan.Steps) != 2 {
		t.Fatalf("Steps len = %d, want 2", len(plan.Steps))
	}
	if plan.Steps[0].Description == "" {
		t.Error("Steps[0].Description is empty")
	}
	if plan.Steps[1].Description == "" {
		t.Error("Steps[1].Description is empty")
	}
}

func TestParseArchitectPlan_EmptyResponse(t *testing.T) {
	t.Parallel()

	plan := parseArchitectPlan("")
	if len(plan.Steps) != 0 {
		t.Errorf("Steps len = %d, want 0 for empty response", len(plan.Steps))
	}
}

func TestParseArchitectPlan_ExtractsFilePath(t *testing.T) {
	t.Parallel()

	content := "1. Edit internal/server/single.go: fix timeout"
	plan := parseArchitectPlan(content)

	if len(plan.Steps) != 1 {
		t.Fatalf("Steps len = %d, want 1", len(plan.Steps))
	}
	if plan.Steps[0].File != "internal/server/single.go" {
		t.Errorf("Steps[0].File = %q, want %q", plan.Steps[0].File, "internal/server/single.go")
	}
}
