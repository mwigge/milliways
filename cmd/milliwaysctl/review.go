package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mwigge/milliways/internal/runner/review"
)

// runLocalReviewCode implements `milliwaysctl local review-code`.
func runLocalReviewCode(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review-code", flag.ContinueOnError)
	fs.SetOutput(stderr)

	model := fs.String("model", "", "override loaded model alias (default: auto-detect from endpoint)")
	out := fs.String("out", "", "write final report to file (default: stdout)")
	resume := fs.Bool("resume", false, "continue from an existing scratch file")
	noMemory := fs.Bool("no-memory", false, "skip MemPalace read/write")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "usage: milliwaysctl local review-code <repo-path> [flags]")
		fs.PrintDefaults()
		return 2
	}

	repoPath := fs.Arg(0)
	if _, err := os.Stat(repoPath); err != nil {
		fmt.Fprintf(stderr, "review-code: %s: %v\n", repoPath, err)
		return 1
	}

	endpoint := strings.TrimRight(os.Getenv("MILLIWAYS_LOCAL_ENDPOINT"), "/")
	if endpoint == "" {
		endpoint = "http://localhost:8765/v1"
	}
	socketPath := defaultSocket()

	cfg := review.Config{
		RepoPath:   repoPath,
		Endpoint:   endpoint,
		ModelAlias: *model,
		OutPath:    *out,
		Resume:     *resume,
		NoMemory:   *noMemory,
		SocketPath: socketPath,
	}

	runner, err := review.New(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "review-code: %v\n", err)
		return 1
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	fmt.Fprintf(stderr, "reviewing %s …\n", repoPath)
	result, err := runner.Run(ctx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "review-code: %v\n", err)
		return 1
	}

	report := buildReport(result)

	if *out != "" {
		if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
			fmt.Fprintf(stderr, "review-code: write %s: %v\n", *out, err)
			return 1
		}
		fmt.Fprintf(stderr, "report written to %s\n", *out)
	} else {
		fmt.Fprint(stdout, report)
	}

	fmt.Fprintf(stderr, "\ngroups: %d  findings: %d  model: %s\n",
		len(result.Groups), len(result.Findings), result.Model)
	return 0
}

func buildReport(result review.ReviewResult) string {
	var b strings.Builder
	b.WriteString(result.Summary)
	if result.Summary != "" && !strings.HasSuffix(result.Summary, "\n") {
		b.WriteByte('\n')
	}
	return b.String()
}
