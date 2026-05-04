package review

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNoCommits is returned by Undo when the repository has no commits to revert.
var ErrNoCommits = errors.New("no commits to undo")

// ErrNotARepo is returned when the target path is not inside a git repository.
var ErrNotARepo = errors.New("not a git repository")

// GitIntegration wraps git operations scoped to a repository root.
// All methods are no-ops when the repo has no git directory (.git absent).
type GitIntegration interface {
	// IsRepo returns true if repoPath is inside a git repository.
	IsRepo() bool
	// Dirty returns true if the working tree has uncommitted changes.
	Dirty() (bool, error)
	// Diff returns the unified diff of unstaged+staged changes. Empty string if clean.
	Diff() (string, error)
	// CommitAll stages all changes under repoPath and creates a commit.
	CommitAll(msg string) error
	// Undo reverts the most recent commit (git reset HEAD~1 --mixed).
	// Returns ErrNoCommits if there is nothing to undo.
	Undo() error
	// Log returns the N most recent commit subjects.
	Log(n int) ([]string, error)
}

// ExecGitIntegration implements GitIntegration by shelling out to the git(1)
// binary. All git invocations are scoped to RepoPath via -C.
type ExecGitIntegration struct {
	// RepoPath is the working directory root passed to every git -C invocation.
	RepoPath string
	// runCmd is the command executor; injected in tests, defaults to exec-based runner.
	runCmd func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewGitIntegration returns a GitIntegration backed by the real git binary.
func NewGitIntegration(repoPath string) GitIntegration {
	return NewGitIntegrationWithRunner(repoPath, defaultRunCmd)
}

// NewGitIntegrationWithRunner returns a GitIntegration that uses the provided
// runCmd for all subprocess calls. Intended for unit testing.
func NewGitIntegrationWithRunner(
	repoPath string,
	runCmd func(ctx context.Context, name string, args ...string) ([]byte, error),
) GitIntegration {
	return &ExecGitIntegration{
		RepoPath: repoPath,
		runCmd:   runCmd,
	}
}

// defaultRunCmd executes name with args and returns combined stdout output.
// Stderr is discarded; callers receive the non-zero exit error from exec.
func defaultRunCmd(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

// IsRepo returns true when `git -C repoPath rev-parse --git-dir` exits zero.
func (g *ExecGitIntegration) IsRepo() bool {
	_, err := g.runCmd(context.Background(), "git", "-C", g.RepoPath, "rev-parse", "--git-dir")
	return err == nil
}

// Dirty returns true when `git status --porcelain` produces any output.
func (g *ExecGitIntegration) Dirty() (bool, error) {
	out, err := g.runCmd(context.Background(), "git", "-C", g.RepoPath, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// Diff returns the unified diff produced by `git diff HEAD`.
// An empty string is returned when the working tree is clean.
func (g *ExecGitIntegration) Diff() (string, error) {
	out, err := g.runCmd(context.Background(), "git", "-C", g.RepoPath, "diff", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	return string(out), nil
}

// CommitAll stages all changes with `git add -A` and commits them.
// It returns nil — not an error — when the working tree is already clean.
func (g *ExecGitIntegration) CommitAll(msg string) error {
	// Check whether there is anything to commit before calling git add.
	dirty, err := g.Dirty()
	if err != nil {
		return fmt.Errorf("commit all dirty check: %w", err)
	}
	if !dirty {
		return nil
	}

	if _, err := g.runCmd(context.Background(), "git", "-C", g.RepoPath, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	if _, err := g.runCmd(context.Background(), "git", "-C", g.RepoPath,
		"commit", "-m", msg, "--no-gpg-sign"); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	return nil
}

// Undo reverts the most recent commit with `git reset --mixed HEAD~1`.
// Returns ErrNoCommits when the repository history is empty or the current
// commit has no parent (root commit).
func (g *ExecGitIntegration) Undo() error {
	out, err := g.runCmd(context.Background(), "git", "-C", g.RepoPath, "log", "-1", "--oneline")
	if err != nil {
		// git exits 128 on an empty repo ("does not have any commits yet").
		return ErrNoCommits
	}
	if strings.TrimSpace(string(out)) == "" {
		return ErrNoCommits
	}

	if _, err := g.runCmd(context.Background(), "git", "-C", g.RepoPath,
		"reset", "--mixed", "HEAD~1"); err != nil {
		return fmt.Errorf("git reset: %w", err)
	}
	return nil
}

// Log returns the subjects of the N most recent commits in newest-first order.
func (g *ExecGitIntegration) Log(n int) ([]string, error) {
	out, err := g.runCmd(context.Background(), "git", "-C", g.RepoPath,
		"log", fmt.Sprintf("-n%d", n), "--pretty=%s")
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	raw := strings.TrimRight(string(out), "\n")
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}
