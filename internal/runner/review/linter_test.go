package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stubRunner returns a runCmd stub that returns the given output and exit code.
func stubRunner(output []byte, exitCode int) func(ctx context.Context, dir, name string, args ...string) ([]byte, int, error) {
	return func(ctx context.Context, dir, name string, args ...string) ([]byte, int, error) {
		return output, exitCode, nil
	}
}

// blockingRunner returns a runCmd stub that blocks until the context is cancelled.
func blockingRunner() func(ctx context.Context, dir, name string, args ...string) ([]byte, int, error) {
	return func(ctx context.Context, dir, name string, args ...string) ([]byte, int, error) {
		<-ctx.Done()
		return nil, -1, ctx.Err()
	}
}

// goRepo creates a temporary directory with a go.mod file so AutoLinter detects Go.
func goRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module smoke.test\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return dir
}

// rustRepo creates a temporary directory with a Cargo.toml so AutoLinter detects Rust.
func rustRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\"\n"), 0o644); err != nil {
		t.Fatalf("write Cargo.toml: %v", err)
	}
	return dir
}

// ---- Tests ----

func TestAutoLinter_GoRepo_BuildSuccess(t *testing.T) {
	t.Parallel()

	dir := goRepo(t)
	runner := stubRunner([]byte(""), 0)
	linter := NewLinterWithRunner(runner)

	findings, err := linter.Run(context.Background(), dir)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("Run() = %d findings, want 0 on success; got: %v", len(findings), findings)
	}
}

func TestAutoLinter_GoRepo_BuildFailure(t *testing.T) {
	t.Parallel()

	dir := goRepo(t)
	output := []byte("foo.go:10:5: undefined: bar\n")
	runner := stubRunner(output, 1)
	linter := NewLinterWithRunner(runner)

	findings, err := linter.Run(context.Background(), dir)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("Run() = 0 findings, want at least 1")
	}
	f := findings[0]
	if f.File != "foo.go" {
		t.Errorf("Finding.File = %q, want %q", f.File, "foo.go")
	}
	if f.Line != 10 {
		t.Errorf("Finding.Line = %d, want 10", f.Line)
	}
	if f.Severity != SeverityHigh {
		t.Errorf("Finding.Severity = %q, want %q", f.Severity, SeverityHigh)
	}
	if f.Reason == "" {
		t.Error("Finding.Reason is empty, want error message")
	}
}

func TestAutoLinter_NoManifest_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	// Empty dir — no go.mod, Cargo.toml, pyproject.toml, etc.
	dir := t.TempDir()
	runner := stubRunner([]byte(""), 0)
	linter := NewLinterWithRunner(runner)

	findings, err := linter.Run(context.Background(), dir)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("Run() = %d findings, want 0 when no manifest found", len(findings))
	}
}

func TestAutoLinter_RustRepo_CargoCheckError(t *testing.T) {
	t.Parallel()

	dir := rustRepo(t)
	output := []byte("error[E0308]: mismatched types\n --> src/main.rs:5:10\n")
	runner := stubRunner(output, 1)
	linter := NewLinterWithRunner(runner)

	findings, err := linter.Run(context.Background(), dir)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("Run() = 0 findings, want at least 1 for Rust error")
	}
	f := findings[0]
	if f.Severity != SeverityHigh {
		t.Errorf("Finding.Severity = %q, want %q", f.Severity, SeverityHigh)
	}
}

func TestAutoLinter_TimeoutHandled(t *testing.T) {
	t.Parallel()

	dir := goRepo(t)
	linter := NewLinterWithRunner(blockingRunner())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := linter.Run(ctx, dir)
	if err == nil {
		t.Fatal("Run() expected error on context timeout, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() error = %v, want context.DeadlineExceeded in chain", err)
	}
}

func TestAutoLinter_GoRepo_UnparsableOutputFallback(t *testing.T) {
	t.Parallel()

	// Non-zero exit with output that can't be parsed as Go diagnostic lines
	// → should return one generic SeverityHigh finding.
	dir := goRepo(t)
	output := []byte("build/lint failed: some unknown error\n")
	runner := stubRunner(output, 2)
	linter := NewLinterWithRunner(runner)

	findings, err := linter.Run(context.Background(), dir)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("Run() = 0 findings, want at least 1 fallback finding")
	}
	if findings[0].Severity != SeverityHigh {
		t.Errorf("finding.Severity = %q, want HIGH", findings[0].Severity)
	}
}
