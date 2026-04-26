package repl

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ReviewTarget describes what is being reviewed.
type ReviewTarget struct {
	Label string   // human-readable, used in file header
	Repos []string // absolute paths to local repos, or "org/name" for remote repos
	Branch string  // branch to diff (empty = current HEAD)
	Base   string  // base branch (main/master)
	Diff   string  // pre-loaded diff text
	Extra  string  // OpenSpec artifacts or other context injected after diff
}

// resolveReviewTarget parses /review-all args into a ReviewTarget.
// args examples: "", "feature/auth", "openspec my-change", "org/chaostooling*"
func resolveReviewTarget(args string) (ReviewTarget, error) {
	args = strings.TrimSpace(args)
	cwd, _ := os.Getwd()
	root := findGitRootFrom(cwd)
	if root == "" {
		root = cwd
	}

	// openspec <name>
	if rest, ok := cutPrefix(args, "openspec "); ok {
		return resolveOpenspecTarget(root, strings.TrimSpace(rest))
	}

	// org/<pattern> — multi-repo via gh CLI
	if strings.Contains(args, "/") || strings.Contains(args, "*") {
		return resolveRepoPattern(args)
	}

	// branch name or empty
	branch := args
	base := detectBaseBranch(root)
	diff, err := gitDiff(root, branch, base)
	if err != nil {
		return ReviewTarget{}, fmt.Errorf("git diff: %w", err)
	}
	label := branch
	if label == "" {
		label = currentBranch(root)
	}
	return ReviewTarget{
		Label:  label + " vs " + base,
		Repos:  []string{root},
		Branch: branch,
		Base:   base,
		Diff:   diff,
	}, nil
}

func resolveOpenspecTarget(root, name string) (ReviewTarget, error) {
	changeDir := findOpenSpecChangeByName(root, name)
	if changeDir == "" {
		return ReviewTarget{}, fmt.Errorf("openspec change %q not found", name)
	}
	var extra strings.Builder
	for _, f := range []string{"proposal.md", "tasks.md"} {
		data, err := os.ReadFile(filepath.Join(changeDir, f))
		if err == nil {
			extra.WriteString("### " + f + "\n\n")
			extra.Write(data)
			extra.WriteString("\n\n")
		}
	}
	base := detectBaseBranch(root)
	diff, _ := gitDiff(root, "", base)
	return ReviewTarget{
		Label: "openspec/" + name,
		Repos: []string{root},
		Base:  base,
		Diff:  diff,
		Extra: extra.String(),
	}, nil
}

func resolveRepoPattern(pattern string) (ReviewTarget, error) {
	parts := strings.SplitN(pattern, "/", 2)
	if len(parts) != 2 {
		return ReviewTarget{}, fmt.Errorf("pattern must be org/glob (e.g. myorg/service*)")
	}
	org, glob := parts[0], parts[1]
	out, err := runCmd("", "gh", "repo", "list", org, "--limit", "100", "--json", "nameWithOwner", "--jq", ".[].nameWithOwner")
	if err != nil {
		return ReviewTarget{}, fmt.Errorf("gh repo list: %w", err)
	}
	var repos []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name := line
		if idx := strings.Index(line, "/"); idx >= 0 {
			name = line[idx+1:]
		}
		matched, _ := filepath.Match(glob, name)
		if !matched {
			matched, _ = filepath.Match(glob, line)
		}
		if matched {
			repos = append(repos, line)
		}
	}
	if len(repos) == 0 {
		return ReviewTarget{}, fmt.Errorf("no repos matched %q", pattern)
	}
	return ReviewTarget{
		Label: pattern,
		Repos: repos, // "org/name" strings, not local paths
	}, nil
}

// buildReviewPrompt constructs the prompt sent to each runner.
func buildReviewPrompt(target ReviewTarget, repoLabel string) string {
	var b strings.Builder
	b.WriteString("Please review the following code change. Your review should cover:\n")
	b.WriteString("1. Does the code do what is expected and correct?\n")
	b.WriteString("2. Are there bugs, edge cases, security issues, or missing error handling?\n")
	b.WriteString("3. Is the implementation clean and idiomatic?\n")
	if target.Extra != "" {
		b.WriteString("4. Does it match the OpenSpec specification?\n")
	}
	b.WriteString("\nBe specific. Reference line numbers or function names where relevant.\n\n")

	if repoLabel != "" {
		b.WriteString("## Repository: " + repoLabel + "\n\n")
	}

	if target.Extra != "" {
		b.WriteString("## Specification\n\n")
		b.WriteString(target.Extra)
	}

	if target.Diff != "" {
		diff := target.Diff
		if len(diff) > 40000 {
			diff = diff[:40000] + "\n\n[diff truncated — focus on what is shown]"
		}
		b.WriteString("## Code Changes\n\n```diff\n")
		b.WriteString(diff)
		b.WriteString("\n```\n")
	}

	return b.String()
}

// buildRemoteReviewPrompt constructs a review prompt for remote repos fetched via gh CLI.
func buildRemoteReviewPrompt(repos []string, rules string) string {
	var b strings.Builder
	b.WriteString("Please review the following open pull requests/branches. For each, check correctness, bugs, and code quality.\n\n")
	for _, repo := range repos {
		b.WriteString("## Repo: " + repo + "\n\n")
		prs, err := runCmd("", "gh", "pr", "list", "--repo", repo, "--state", "open",
			"--json", "number,title,headRefName",
			"--jq", ".[] | \"#\\(.number) \\(.title) [\\(.headRefName)]\"")
		if err != nil || strings.TrimSpace(prs) == "" {
			b.WriteString("_No open PRs._\n\n")
			continue
		}
		b.WriteString("Open PRs:\n" + strings.TrimSpace(prs) + "\n\n")
		// Fetch diff for first open PR only (to stay within context limits).
		prNum := extractFirstPRNumber(prs)
		if prNum != "" {
			diff, diffErr := runCmd("", "gh", "pr", "diff", "--repo", repo, prNum)
			if diffErr == nil && len(diff) > 0 {
				if len(diff) > 20000 {
					diff = diff[:20000] + "\n[truncated]"
				}
				b.WriteString("```diff\n" + diff + "\n```\n\n")
			}
		}
	}
	return b.String()
}

func extractFirstPRNumber(ghOutput string) string {
	lines := strings.SplitN(strings.TrimSpace(ghOutput), "\n", 2)
	if len(lines) == 0 {
		return ""
	}
	first := lines[0]
	if strings.HasPrefix(first, "#") {
		parts := strings.Fields(first)
		if len(parts) > 0 {
			return strings.TrimPrefix(parts[0], "#")
		}
	}
	return ""
}

// collectReview runs a single runner and returns its plain-text output.
func collectReview(ctx context.Context, runner Runner, req DispatchRequest) (string, error) {
	var buf bytes.Buffer
	err := runner.Execute(ctx, req, &buf)
	raw := buf.String()
	// Strip ANSI escape codes for the file output.
	clean := ansiPattern.ReplaceAllString(raw, "")
	return strings.TrimSpace(clean), err
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[mKHJABCDsuhr]`)

// writeReviewFile writes the combined review markdown to docs_local/reviews/.
func writeReviewFile(root, label string, sections map[string]string, runnerOrder []string) (string, error) {
	dir := filepath.Join(root, "docs_local", "reviews")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating review dir: %w", err)
	}
	ts := time.Now().Format("2006-01-02T15-04-05")
	slug := slugify(label)
	if slug != "" {
		slug = "-" + slug
	}
	path := filepath.Join(dir, "review-"+ts+slug+".md")

	var b strings.Builder
	b.WriteString("# Code Review\n\n")
	b.WriteString("**Date:** " + time.Now().Format("2006-01-02 15:04:05") + "\n")
	b.WriteString("**Target:** " + label + "\n")
	b.WriteString("**Reviewers:** " + strings.Join(runnerOrder, ", ") + "\n\n")
	b.WriteString("---\n\n")

	for _, name := range runnerOrder {
		output, ok := sections[name]
		b.WriteString("## Review by " + name + "\n\n")
		if !ok || output == "" {
			b.WriteString("_No output._\n\n")
		} else {
			b.WriteString(output + "\n\n")
		}
		b.WriteString("---\n\n")
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("writing review file: %w", err)
	}
	return path, nil
}

func findGitRootFrom(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func detectBaseBranch(root string) string {
	for _, candidate := range []string{"main", "master"} {
		out, err := runCmd(root, "git", "rev-parse", "--verify", candidate)
		if err == nil && strings.TrimSpace(out) != "" {
			return candidate
		}
	}
	return "main"
}

func currentBranch(root string) string {
	out, err := runCmd(root, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "HEAD"
	}
	return strings.TrimSpace(out)
}

func gitDiff(root, branch, base string) (string, error) {
	var args []string
	if branch == "" {
		args = []string{"diff", base + "...HEAD"}
	} else {
		args = []string{"diff", base + "..." + branch}
	}
	return runCmd(root, "git", args...)
}

func runCmd(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	return string(out), err
}

func cutPrefix(s, prefix string) (string, bool) {
	if strings.HasPrefix(s, prefix) {
		return s[len(prefix):], true
	}
	return s, false
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("/", "-", " ", "-", "*", "").Replace(s)
	var b strings.Builder
	for _, r := range s {
		if r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}

