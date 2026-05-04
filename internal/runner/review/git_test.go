package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo initialises a bare git repo in dir with a configured identity.
// It returns the repo path for convenience.
func initTestRepo(t *testing.T, dir string) string {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", dir)
	run("-C", dir, "config", "user.email", "test@example.com")
	run("-C", dir, "config", "user.name", "Test")
	return dir
}

// commitFile writes a file and commits it in the given repo.
func commitFile(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("-C", dir, "add", "-A")
	run("-C", dir, "commit", "-m", msg, "--no-gpg-sign")
}

// --- IsRepo ---

func TestExecGitIntegration_IsRepo_True(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t, t.TempDir())
	gi := NewGitIntegration(dir)
	if !gi.IsRepo() {
		t.Error("IsRepo() = false, want true for initialised git repo")
	}
}

func TestExecGitIntegration_IsRepo_False(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // no git init
	gi := NewGitIntegration(dir)
	if gi.IsRepo() {
		t.Error("IsRepo() = true, want false for plain directory")
	}
}

// --- Dirty ---

func TestExecGitIntegration_Dirty_Clean(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t, t.TempDir())
	commitFile(t, dir, "hello.go", "package main\n", "initial")

	gi := NewGitIntegration(dir)
	dirty, err := gi.Dirty()
	if err != nil {
		t.Fatalf("Dirty() error = %v", err)
	}
	if dirty {
		t.Error("Dirty() = true, want false for clean working tree")
	}
}

func TestExecGitIntegration_Dirty_Uncommitted(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t, t.TempDir())
	// Write a file but do NOT commit.
	if err := os.WriteFile(filepath.Join(dir, "dirty.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	gi := NewGitIntegration(dir)
	dirty, err := gi.Dirty()
	if err != nil {
		t.Fatalf("Dirty() error = %v", err)
	}
	if !dirty {
		t.Error("Dirty() = false, want true for uncommitted file")
	}
}

// --- CommitAll ---

func TestExecGitIntegration_CommitAll_CreatesCommit(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t, t.TempDir())
	// Write a file and let CommitAll do the staging.
	if err := os.WriteFile(filepath.Join(dir, "newfile.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	gi := NewGitIntegration(dir)
	if err := gi.CommitAll("feat: add newfile"); err != nil {
		t.Fatalf("CommitAll() error = %v", err)
	}

	msgs, err := gi.Log(1)
	if err != nil {
		t.Fatalf("Log() error = %v", err)
	}
	if len(msgs) == 0 || msgs[0] != "feat: add newfile" {
		t.Errorf("Log(1) = %v, want [\"feat: add newfile\"]", msgs)
	}
}

func TestExecGitIntegration_CommitAll_NothingToCommit(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t, t.TempDir())
	commitFile(t, dir, "stable.go", "package main\n", "base")

	gi := NewGitIntegration(dir)
	// Working tree is clean — CommitAll must return nil, not an error.
	if err := gi.CommitAll("should not create commit"); err != nil {
		t.Errorf("CommitAll() on clean tree = %v, want nil", err)
	}
}

// --- Undo ---

func TestExecGitIntegration_Undo_RevertsLastCommit(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t, t.TempDir())
	// An initial commit is required so that HEAD~1 is valid after the second commit.
	commitFile(t, dir, "base.go", "package main\n", "initial")
	commitFile(t, dir, "undo_me.go", "package main\n", "to be undone")

	gi := NewGitIntegration(dir)
	if err := gi.Undo(); err != nil {
		t.Fatalf("Undo() error = %v", err)
	}

	// After undo the file should be unstaged (working tree dirty).
	dirty, err := gi.Dirty()
	if err != nil {
		t.Fatalf("Dirty() after Undo error = %v", err)
	}
	if !dirty {
		t.Error("Dirty() = false after Undo, want true (file should be unstaged)")
	}
}

func TestExecGitIntegration_Undo_NoCommits(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t, t.TempDir())
	// Repo has no commits.
	gi := NewGitIntegration(dir)
	err := gi.Undo()
	if !errors.Is(err, ErrNoCommits) {
		t.Errorf("Undo() error = %v, want ErrNoCommits", err)
	}
}

// --- Diff ---

func TestExecGitIntegration_Diff_ShowsChanges(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t, t.TempDir())
	commitFile(t, dir, "base.go", "package main\n", "base commit")

	// Modify the file without staging/committing.
	if err := os.WriteFile(filepath.Join(dir, "base.go"), []byte("package main\n\nvar x = 1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	gi := NewGitIntegration(dir)
	diff, err := gi.Diff()
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if diff == "" {
		t.Error("Diff() = \"\", want non-empty diff for modified file")
	}
}

// --- Log ---

func TestExecGitIntegration_Log_ReturnsMessages(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t, t.TempDir())
	commitFile(t, dir, "a.go", "package a\n", "first commit")
	commitFile(t, dir, "b.go", "package b\n", "second commit")

	gi := NewGitIntegration(dir)
	msgs, err := gi.Log(2)
	if err != nil {
		t.Fatalf("Log() error = %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("Log(2) returned %d entries, want 2", len(msgs))
	}
	if msgs[0] != "second commit" {
		t.Errorf("msgs[0] = %q, want %q", msgs[0], "second commit")
	}
	if msgs[1] != "first commit" {
		t.Errorf("msgs[1] = %q, want %q", msgs[1], "first commit")
	}
}

// --- Unit tests with injected runCmd stub ---

func TestExecGitIntegration_IsRepo_ErrorExitReturnsFalse(t *testing.T) {
	t.Parallel()
	gi := NewGitIntegrationWithRunner("/some/path", func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, fmt.Errorf("exit status 128")
	})
	if gi.IsRepo() {
		t.Error("IsRepo() = true, want false when command fails")
	}
}

func TestExecGitIntegration_Dirty_RunCmdError(t *testing.T) {
	t.Parallel()
	gi := NewGitIntegrationWithRunner("/some/path", func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "status") {
			return nil, fmt.Errorf("exit status 128")
		}
		return nil, nil
	})
	_, err := gi.Dirty()
	if err == nil {
		t.Error("Dirty() error = nil, want error when git status fails")
	}
}

func TestExecGitIntegration_Diff_RunCmdError(t *testing.T) {
	t.Parallel()
	gi := NewGitIntegrationWithRunner("/some/path", func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, fmt.Errorf("exit status 1")
	})
	_, err := gi.Diff()
	if err == nil {
		t.Error("Diff() error = nil, want error when git diff fails")
	}
}

func TestExecGitIntegration_CommitAll_AddFailureReturnsError(t *testing.T) {
	t.Parallel()
	calls := 0
	gi := NewGitIntegrationWithRunner("/some/path", func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls++
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "status --porcelain"):
			return []byte("M dirty.go\n"), nil // dirty so CommitAll will proceed
		case strings.Contains(joined, "add -A"):
			return nil, fmt.Errorf("exit status 1")
		default:
			return nil, nil
		}
	})
	err := gi.CommitAll("msg")
	if err == nil {
		t.Error("CommitAll() = nil, want error when git add fails")
	}
}

func TestExecGitIntegration_Undo_LogRunCmdError(t *testing.T) {
	t.Parallel()
	gi := NewGitIntegrationWithRunner("/some/path", func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "log") {
			return nil, fmt.Errorf("exit status 128")
		}
		return nil, nil
	})
	err := gi.Undo()
	if err == nil {
		t.Error("Undo() = nil, want error when git log fails")
	}
}

func TestExecGitIntegration_Log_RunCmdError(t *testing.T) {
	t.Parallel()
	gi := NewGitIntegrationWithRunner("/some/path", func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, fmt.Errorf("exit status 128")
	})
	_, err := gi.Log(5)
	if err == nil {
		t.Error("Log() = nil, want error when git log fails")
	}
}

func TestExecGitIntegration_ErrNotARepo_IsNotRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gi := NewGitIntegration(dir)
	_, err := gi.Dirty()
	// Not a repo: git returns non-zero but we wrap it, so the error must be non-nil.
	// The exact sentinel isn't tested here — just that an error is returned.
	if err == nil {
		t.Error("Dirty() on non-repo = nil, want an error")
	}
}
