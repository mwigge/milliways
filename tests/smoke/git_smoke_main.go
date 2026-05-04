// git_smoke_main exercises NewGitIntegration end-to-end against a real git
// repository prepared by smoke_git.sh. Exit code is non-zero on failure.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mwigge/milliways/internal/runner/review"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: git_smoke_test <repoPath>")
		os.Exit(1)
	}
	repoPath := os.Args[1]

	gi := review.NewGitIntegration(repoPath)

	// 1. IsRepo must return true.
	if !gi.IsRepo() {
		fatal("IsRepo() = false, want true")
	}
	logf("IsRepo: OK")

	// 2. Initially dirty (file.go written but not committed).
	dirty, err := gi.Dirty()
	mustNil("Dirty()", err)
	if !dirty {
		fatal("Dirty() = false after writing file.go, want true")
	}
	logf("Dirty (initial): OK")

	// 3. CommitAll creates the first commit.
	if err := gi.CommitAll("smoke: initial commit"); err != nil {
		fatal("CommitAll(): %v", err)
	}
	logf("CommitAll (first): OK")

	// 4. Log should now contain the initial commit.
	msgs, err := gi.Log(1)
	mustNil("Log()", err)
	if len(msgs) == 0 || msgs[0] != "smoke: initial commit" {
		fatal("Log(1) = %v, want [\"smoke: initial commit\"]", msgs)
	}
	logf("Log: OK")

	// 5. Dirty should be false after committing.
	dirty, err = gi.Dirty()
	mustNil("Dirty() after commit", err)
	if dirty {
		fatal("Dirty() = true after commit, want false")
	}
	logf("Dirty (after commit): OK")

	// 6. Modify file to produce a diff.
	if err := appendLine(repoPath, "file.go", "// modified\n"); err != nil {
		fatal("appendLine: %v", err)
	}
	diff, err := gi.Diff()
	mustNil("Diff()", err)
	if diff == "" {
		fatal("Diff() = \"\", want non-empty after modifying file.go")
	}
	logf("Diff: OK")

	// 7. Commit the change so we have a parent for Undo.
	if err := gi.CommitAll("smoke: second commit"); err != nil {
		fatal("CommitAll() second: %v", err)
	}
	logf("CommitAll (second): OK")

	// 8. Undo reverts the second commit; tree becomes dirty.
	if err := gi.Undo(); err != nil {
		fatal("Undo(): %v", err)
	}
	dirty, err = gi.Dirty()
	mustNil("Dirty() after Undo", err)
	if !dirty {
		fatal("Dirty() = false after Undo, want true")
	}
	logf("Undo: OK")
}

// appendLine appends text to path.
func appendLine(dir, name, line string) error {
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // best-effort close
	_, err = fmt.Fprint(f, line)
	return err
}

func mustNil(label string, err error) {
	if err != nil {
		fatal("%s: %v", label, err)
	}
}

func logf(format string, args ...any) {
	fmt.Printf("[smoke] "+format+"\n", args...)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[smoke FAIL] "+format+"\n", args...)
	os.Exit(1)
}

// configureGit sets git identity inside repoPath so commits succeed in CI.
func init() {
	if len(os.Args) < 2 {
		return
	}
	repoPath := os.Args[1]
	for _, kv := range [][2]string{
		{"user.email", "smoke@test"},
		{"user.name", "Smoke"},
	} {
		out, err := exec.Command("git", "-C", repoPath, "config", kv[0], kv[1]).CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "git config %s: %v\n%s", kv[0], err, out)
			os.Exit(1)
		}
	}
}
