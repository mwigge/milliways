package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readRepoFile(t *testing.T, elems ...string) string {
	t.Helper()

	path := filepath.Join(append([]string{repoRoot(t)}, elems...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}

	return string(data)
}

func TestSmoke_NvimContextScenarioScriptContainsRepresentativeBundle(t *testing.T) {
	t.Parallel()

	script := readRepoFile(t, "testdata", "smoke", "scenarios", "nvim-context.sh")

	for _, want := range []string{
		"go build",
		"milliways",
		"schema_version",
		"\"1\"",
		"buffer",
		"cursor",
		"lsp",
		"diagnostics",
		"unused variable",
		"git",
		"project",
		"recent_files",
		"--context-stdin",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("scenario script missing %q", want)
		}
	}

	for _, want := range []string{"error", "invalid", "rm -f", "mktemp"} {
		if !strings.Contains(strings.ToLower(script), strings.ToLower(want)) {
			t.Fatalf("scenario script missing assertion/cleanup fragment %q", want)
		}
	}
}

func TestSmokeHarness_InvokesNvimContextScenario(t *testing.T) {
	t.Parallel()

	smokeScript := readRepoFile(t, "scripts", "smoke.sh")

	for _, want := range []string{
		"nvim-context",
		"nvim-context.sh",
	} {
		if !strings.Contains(smokeScript, want) {
			t.Fatalf("scripts/smoke.sh missing %q", want)
		}
	}
}

func TestSmoke_NvimContextScenarioExecutesWithoutParseErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repo := repoRoot(t)
	scriptPath := filepath.Join(repo, "testdata", "smoke", "scenarios", "nvim-context.sh")
	cmd := exec.CommandContext(ctx, "bash", scriptPath)
	cmd.Dir = repo
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("scenario exceeded 10s timeout\noutput:\n%s", string(output))
	}
	if err != nil {
		t.Fatalf("scenario failed: %v\noutput:\n%s", err, string(output))
	}

	lowerOutput := strings.ToLower(string(output))
	for _, bad := range []string{"error", "invalid"} {
		if strings.Contains(lowerOutput, bad) {
			t.Fatalf("scenario output unexpectedly contains %q\noutput:\n%s", bad, string(output))
		}
	}
}
