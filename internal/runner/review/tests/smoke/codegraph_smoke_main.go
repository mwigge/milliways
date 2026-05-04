//go:build ignore

// codegraph_smoke_main.go is a standalone program used by smoke_codegraph.sh.
// It exercises NewCodeGraphClient with a bad socket path and verifies that
// IsIndexed returns false without panicking, confirming graceful fallback.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mwigge/milliways/internal/runner/review"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: codegraph_smoke_test <socketPath>")
		os.Exit(1)
	}
	socketPath := os.Args[1]

	client := review.NewCodeGraphClient(socketPath)
	mc, ok := client.(*review.MCPCodeGraphClient)
	if !ok {
		fmt.Fprintln(os.Stderr, "FAIL: NewCodeGraphClient did not return *MCPCodeGraphClient")
		os.Exit(1)
	}

	ctx := context.Background()

	indexed := mc.IsIndexed(ctx)
	if indexed {
		fmt.Fprintln(os.Stderr, "FAIL: IsIndexed() = true for unavailable socket, want false")
		os.Exit(1)
	}

	_, err := client.Files(ctx, "/repo")
	if err == nil {
		fmt.Fprintln(os.Stderr, "FAIL: Files() on bad socket returned nil error, want error")
		os.Exit(1)
	}

	_, err = client.Impact(ctx, "main", 2)
	if err == nil {
		fmt.Fprintln(os.Stderr, "FAIL: Impact() on bad socket returned nil error, want error")
		os.Exit(1)
	}

	fmt.Println("OK: CodeGraph client fails gracefully when socket unavailable")
}
