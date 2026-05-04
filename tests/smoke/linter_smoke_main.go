//go:build ignore

// linter_smoke_main.go is a standalone program used by smoke_linter.sh.
// It calls NewLinter().Run() on the fixture repo path supplied as the first
// argument and asserts that at least one SeverityHigh finding is returned.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mwigge/milliways/internal/runner/review"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: linter_smoke_test <repoPath>")
		os.Exit(1)
	}
	repoPath := os.Args[1]

	linter := review.NewLinter()
	findings, err := linter.Run(context.Background(), repoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Run() error: %v\n", err)
		os.Exit(1)
	}

	var highCount int
	for _, f := range findings {
		if f.Severity == review.SeverityHigh {
			highCount++
		}
	}

	if highCount == 0 {
		fmt.Fprintf(os.Stderr, "FAIL: expected at least one SeverityHigh finding, got %d findings\n", len(findings))
		for _, f := range findings {
			fmt.Fprintf(os.Stderr, "  finding: severity=%s file=%s line=%d reason=%s\n", f.Severity, f.File, f.Line, f.Reason)
		}
		os.Exit(1)
	}

	fmt.Printf("OK: %d SeverityHigh finding(s)\n", highCount)
	for _, f := range findings {
		fmt.Printf("  [%s] %s:%d %s\n", f.Severity, f.File, f.Line, f.Reason)
	}
}
