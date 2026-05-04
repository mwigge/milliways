//go:build ignore

// architect_smoke_main.go is a standalone smoke program that verifies the
// ArchitectEditor two-phase flow using stub GroupClients. Run via:
//
//	go run architect_smoke_main.go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mwigge/milliways/internal/runner/review"
)

// smokeArchitectClient stubs the architect model, returning a single finding
// whose Reason contains a numbered plan step.
type smokeArchitectClient struct{}

func (s *smokeArchitectClient) ReviewGroup(_ context.Context, _ review.Group, _ review.PriorContext) ([]review.Finding, error) {
	return []review.Finding{
		{
			Severity: review.SeverityHigh,
			Reason:   "1. Edit main.go: add context propagation\n2. Edit handler.go: fix error handling",
		},
	}, nil
}

// smokeEditorClient stubs the editor model, returning one finding per call.
type smokeEditorClient struct {
	callCount int
}

func (s *smokeEditorClient) ReviewGroup(_ context.Context, g review.Group, _ review.PriorContext) ([]review.Finding, error) {
	s.callCount++
	return []review.Finding{
		{
			Severity: review.SeverityLow,
			File:     g.Dir,
			Reason:   fmt.Sprintf("editor applied step for %s", g.Dir),
		},
	}, nil
}

func main() {
	// Create a temporary repo directory with stub files.
	dir, err := os.MkdirTemp("", "architect-smoke-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mkdtemp: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir) //nolint:errcheck

	for _, name := range []string{"main.go", "handler.go"} {
		if writeErr := os.WriteFile(dir+"/"+name, []byte("package main\n"), 0o644); writeErr != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", name, writeErr)
			os.Exit(1)
		}
	}

	tracker := review.NewContextTracker(dir)
	for _, name := range []string{"main.go", "handler.go"} {
		if addErr := tracker.Add(dir + "/" + name); addErr != nil {
			fmt.Fprintf(os.Stderr, "tracker.Add %s: %v\n", name, addErr)
			os.Exit(1)
		}
	}

	architectClient := &smokeArchitectClient{}
	editorClient := &smokeEditorClient{}

	ae := review.NewArchitectEditor(architectClient, editorClient)
	findings, plan, execErr := ae.Execute(context.Background(), dir, "refactor for correctness", tracker)
	if execErr != nil {
		fmt.Fprintf(os.Stderr, "Execute: %v\n", execErr)
		os.Exit(1)
	}

	if len(plan.Steps) == 0 {
		fmt.Fprintln(os.Stderr, "FAIL: plan has no steps")
		os.Exit(1)
	}
	if len(findings) == 0 {
		fmt.Fprintln(os.Stderr, "FAIL: no findings returned")
		os.Exit(1)
	}

	fmt.Printf("OK: plan steps=%d findings=%d\n", len(plan.Steps), len(findings))
}
