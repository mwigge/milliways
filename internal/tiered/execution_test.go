package tiered

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testRepository(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "allowed.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "allowed.txt")
	run("commit", "-qm", "base")
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	revision, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return root, string(revision[:len(revision)-1])
}

func envelope(root, revision, alignment string) ExecutionEnvelope {
	return ExecutionEnvelope{
		Schema: EnvelopeSchema, TaskID: "task-1", RepositoryRoot: root,
		BaseRevision: revision, Language: "go", Objective: "change allowed file",
		AllowedPaths: []string{"allowed.txt"}, ForbiddenPaths: []string{".git"},
		Acceptance:      []AcceptanceCommand{{Kind: "test", Program: "git", Args: []string{"diff", "--check"}}},
		MaxChangedFiles: 1, MaxTokens: 1000, MaxRetries: 1, WallTimeSeconds: 5,
		AlignmentLabel: alignment, ResponseSchema: ResultSchema,
	}
}

func TestApplyEditsAndVerifyResult(t *testing.T) {
	root, revision := testRepository(t)
	env := envelope(root, revision, "safety-tuned")
	if err := ApplyEdits(env, []EditOperation{{Path: "allowed.txt", Content: []byte("after\n")}}); err != nil {
		t.Fatal(err)
	}
	result, err := VerifiedResult(context.Background(), env, "updated")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "verified" || len(result.ChangedFiles) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestScopeViolationIsRejectedBeforeWriting(t *testing.T) {
	root, revision := testRepository(t)
	env := envelope(root, revision, "uncensored")
	err := ApplyEdits(env, []EditOperation{{Path: "forbidden.txt", Content: []byte("no\n")}})
	if err == nil {
		t.Fatal("expected scope rejection")
	}
	if _, statErr := os.Stat(filepath.Join(root, "forbidden.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("forbidden file was written: %v", statErr)
	}
}

func TestStaleRevisionIsRejected(t *testing.T) {
	root, revision := testRepository(t)
	env := envelope(root, revision+"bad", "")
	if err := ValidateEnvelope(env); err == nil {
		t.Fatal("expected stale revision rejection")
	}
}

func TestForbiddenGitPathIsRejectedWithReason(t *testing.T) {
	root, revision := testRepository(t)
	env := envelope(root, revision, "")
	err := ApplyEdits(env, []EditOperation{{Path: ".git/config", Content: []byte("bad\n")}})
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("expected forbidden path rejection, got %v", err)
	}
}

func TestSymlinkEscapeIsRejected(t *testing.T) {
	root, revision := testRepository(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	env := envelope(root, revision, "")
	env.AllowedPaths = []string{"link/escape.txt"}
	err := ApplyEdits(env, []EditOperation{{Path: "link/escape.txt", Content: []byte("no\n")}})
	if err == nil {
		t.Fatal("expected symlink escape rejection")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("file was written outside repository root: %v", statErr)
	}
}

func TestMaxChangedFilesBudgetIsEnforced(t *testing.T) {
	root, revision := testRepository(t)
	env := envelope(root, revision, "")
	env.AllowedPaths = []string{"allowed.txt", "other.txt"}
	env.MaxChangedFiles = 1
	err := ApplyEdits(env, []EditOperation{
		{Path: "allowed.txt", Content: []byte("after\n")},
		{Path: "other.txt", Content: []byte("new\n")},
	})
	if err == nil || !strings.Contains(err.Error(), "budget exceeded") {
		t.Fatalf("expected changed-file budget rejection, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "other.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("file was written despite budget rejection: %v", statErr)
	}
}
