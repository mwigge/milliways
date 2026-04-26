package repl

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type commandHandler func(ctx context.Context, r *REPL, args string) error

var commandHandlers = map[string]commandHandler{
	"switch":   handleSwitch,
	"stick":    handleStick,
	"back":     handleBack,
	"session":  handleSession,
	"history":  handleHistory,
	"summary":  handleSummary,
	"cost":     handleCost,
	"limit":    handleLimit,
	"openspec": handleOpenspec,
	"repo":     handleRepo,
	"login":    handleLogin,
	"logout":   handleLogout,
	"auth":     handleAuth,
	"help":     handleHelp,
	"exit":     handleExit,
	// Runner shorthand aliases — /claude, /codex, etc. are equivalent to /switch <runner>
	"claude":   handleSwitch,
	"codex":    handleSwitch,
	"minimax":  handleSwitch,
	"copilot":  handleSwitch,
}

func handleSwitch(ctx context.Context, r *REPL, args string) error {
	if args == "" {
		r.println("Available runners:")
		for name := range r.runners {
			r.println(fmt.Sprintf("  %s", name))
		}
		return nil
	}

	if r.runner != nil {
		r.checkpointConversation(ctx, fmt.Sprintf("switch_to_%s", args))
	}

	if err := r.SetRunner(args); err != nil {
		return err
	}
	r.println(fmt.Sprintf("Switched to %s", RunnerAccentText(r.runner.Name(), r.runner.Name())))
	return nil
}

func handleStick(ctx context.Context, r *REPL, args string) error {
	r.println("Sticky mode enabled (current runner persists until /stick or /switch)")
	return nil
}

func handleBack(ctx context.Context, r *REPL, args string) error {
	if r.prev == nil {
		return fmt.Errorf("no previous runner")
	}
	r.runner, r.prev = r.prev, r.runner
	r.println(fmt.Sprintf("Reverted to %s", RunnerAccentText(r.runner.Name(), r.runner.Name())))
	return nil
}

func handleSession(ctx context.Context, r *REPL, args string) error {
	if args == "" {
		if r.session != nil {
			r.println(fmt.Sprintf("Current session: %s", r.session.conversationID))
			r.println(fmt.Sprintf("  Runner: %s", r.session.runnerName))
		} else {
			r.println("No active session.")
		}
		if r.substrate != nil {
			list, err := r.substrate.ConversationList(ctx)
			if err == nil && len(list) > 0 {
				r.println("")
				r.println("Stored sessions:")
				for _, s := range list {
					age := ""
					if !s.UpdatedAt.IsZero() {
						age = fmt.Sprintf(" (%s ago)", time.Since(s.UpdatedAt).Round(time.Second))
					}
					r.println(fmt.Sprintf("  %s%s [%s]", s.ConversationID, age, s.Status))
				}
			}
		}
		return nil
	}

	if r.substrate == nil {
		return fmt.Errorf("mempalace not configured")
	}

	list, err := r.substrate.ConversationList(ctx)
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	for _, s := range list {
		if strings.Contains(strings.ToLower(s.ConversationID), strings.ToLower(args)) ||
			strings.Contains(strings.ToLower(s.BlockID), strings.ToLower(args)) {
			rec, err := r.substrate.ConversationGet(ctx, s.ConversationID)
			if err != nil {
				continue
			}
			r.session = &replSession{
				conversationID: rec.ConversationID,
				runnerName:     r.runner.Name(),
			}
			r.println(fmt.Sprintf("Resumed session: %s", rec.ConversationID))
			r.println(fmt.Sprintf("  Status: %s", rec.Status))
			r.println(fmt.Sprintf("  Turns: %d", len(rec.Transcript)))
			return nil
		}
	}

	return fmt.Errorf("session %q not found", args)
}

func handleHistory(ctx context.Context, r *REPL, args string) error {
	if len(r.history) == 0 {
		r.println("No history yet.")
		return nil
	}
	r.println("History:")
	for i, h := range r.history {
		r.println(fmt.Sprintf("  %d: %s", i+1, h))
	}
	return nil
}

func handleSummary(ctx context.Context, r *REPL, args string) error {
	r.println("Session summary:")
	if r.runner != nil {
		r.println(fmt.Sprintf("  Runner: %s", RunnerAccentText(r.runner.Name(), r.runner.Name())))
	}
	r.println(fmt.Sprintf("  Commands: %d", len(r.history)))
	return nil
}

func handleCost(ctx context.Context, r *REPL, args string) error {
	r.println("Session cost: $0.00 (tracking not yet implemented)")
	return nil
}

func handleLimit(ctx context.Context, r *REPL, args string) error {
	for name, runner := range r.runners {
		r.println(fmt.Sprintf("%s:", RunnerAccentText(name, name)))

		var quota *QuotaInfo
		var err error

		if r.getQuota != nil {
			quota, err = r.getQuota(name)
		}
		if err != nil || quota == nil {
			quota, err = runner.Quota()
		}
		if err != nil || quota == nil {
			r.println("  unknown")
			continue
		}
		if quota.Day != nil {
			r.println(fmt.Sprintf("  day:   %s", formatQuotaPeriod(quota.Day)))
		}
		if quota.Week != nil {
			r.println(fmt.Sprintf("  week:  %s", formatQuotaPeriod(quota.Week)))
		}
		if quota.Month != nil {
			r.println(fmt.Sprintf("  month: %s", formatQuotaPeriod(quota.Month)))
		}
	}
	return nil
}

func formatQuotaPeriod(q *QuotaPeriod) string {
	if q == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d / %d [%s]", q.Used, q.Limit, q.Resets)
}

func handleOpenspec(ctx context.Context, r *REPL, args string) error {
	if args != "" {
		return handleOpenspecSwitch(ctx, r, args)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	changeDir := findOpenSpecChange(cwd)
	if changeDir == "" {
		r.println("No active OpenSpec change found.")
		if r.currentChange != "" {
			r.println(fmt.Sprintf("(hint: /openspec %s to restore)", r.currentChange))
		}
		return nil
	}

	proposalPath := filepath.Join(changeDir, "proposal.md")
	proposalBytes, err := os.ReadFile(proposalPath)
	if err != nil {
		r.println(fmt.Sprintf("OpenSpec change: %s", changeDir))
		return nil
	}

	proposal := string(proposalBytes)
	lines := strings.Split(proposal, "\n")
	var title string
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			title = strings.TrimPrefix(line, "# ")
			break
		}
	}

	changeName := filepath.Base(changeDir)
	r.println(fmt.Sprintf("OpenSpec change: %s", changeName))
	r.currentChange = changeName
	if title != "" {
		r.println(fmt.Sprintf("  %s", title))
	}

	tasksPath := filepath.Join(changeDir, "tasks.md")
	if _, err := os.Stat(tasksPath); err == nil {
		tasksBytes, _ := os.ReadFile(tasksPath)
		tasksLines := strings.Split(string(tasksBytes), "\n")
		total := 0
		done := 0
		for _, line := range tasksLines {
			if strings.HasPrefix(strings.TrimSpace(line), "- [x]") {
				done++
				total++
			} else if strings.HasPrefix(strings.TrimSpace(line), "- [ ]") {
				total++
			}
		}
		if total > 0 {
			r.println(fmt.Sprintf("  Tasks: %d / %d done", done, total))
		}
	}

	return nil
}

func handleOpenspecSwitch(ctx context.Context, r *REPL, name string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	changeDir := findOpenSpecChangeByName(cwd, name)
	if changeDir == "" {
		r.println(fmt.Sprintf("OpenSpec change %q not found.", name))
		return nil
	}

	proposalPath := filepath.Join(changeDir, "proposal.md")
	proposalBytes, err := os.ReadFile(proposalPath)
	if err != nil {
		r.println(fmt.Sprintf("OpenSpec change: %s", changeDir))
		return nil
	}

	proposal := string(proposalBytes)
	lines := strings.Split(proposal, "\n")
	var title string
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			title = strings.TrimPrefix(line, "# ")
			break
		}
	}

	changeName := filepath.Base(changeDir)
	r.println(fmt.Sprintf("Switched to OpenSpec change: %s", changeName))
	r.currentChange = changeName
	if title != "" {
		r.println(fmt.Sprintf("  %s", title))
	}

	tasksPath := filepath.Join(changeDir, "tasks.md")
	if _, err := os.Stat(tasksPath); err == nil {
		tasksBytes, _ := os.ReadFile(tasksPath)
		tasksLines := strings.Split(string(tasksBytes), "\n")
		total := 0
		done := 0
		for _, line := range tasksLines {
			if strings.HasPrefix(strings.TrimSpace(line), "- [x]") {
				done++
				total++
			} else if strings.HasPrefix(strings.TrimSpace(line), "- [ ]") {
				total++
			}
		}
		if total > 0 {
			r.println(fmt.Sprintf("  Tasks: %d / %d done", done, total))
		}
	}

	return nil
}

func findOpenSpecChange(dir string) string {
	for {
		changesDir := filepath.Join(dir, "openspec", "changes")
		entries, err := os.ReadDir(changesDir)
		if err == nil {
			var newest string
			var newestTime int64
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				path := filepath.Join(changesDir, entry.Name())
				info, err := entry.Info()
				if err != nil {
					continue
				}
				if info.ModTime().Unix() > newestTime {
					newestTime = info.ModTime().Unix()
					newest = path
				}
			}
			if newest != "" {
				return newest
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func findOpenSpecChangeByName(dir, name string) string {
	nameLower := strings.ToLower(name)
	for {
		changesDir := filepath.Join(dir, "openspec", "changes")
		entries, err := os.ReadDir(changesDir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				if strings.ToLower(entry.Name()) == nameLower {
					return filepath.Join(changesDir, entry.Name())
				}
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func handleRepo(ctx context.Context, r *REPL, args string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	repoPath := findGitRoot(cwd)
	if repoPath == "" {
		r.println("Not in a git repository.")
		return nil
	}

	repoName := filepath.Base(repoPath)

	branch, _, _ := runGitCmd(repoPath, []string{"rev-parse", "--abbrev-ref", "HEAD"})
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "HEAD"
	}

	commit, _, _ := runGitCmd(repoPath, []string{"log", "-1", "--format=%h %s"})
	commit = strings.TrimSpace(commit)
	if commit == "" {
		commit = "none"
	}

	status, _, _ := runGitCmd(repoPath, []string{"status", "--porcelain"})
	statusLines := strings.Split(strings.TrimSpace(status), "\n")
	clean := true
	for _, line := range statusLines {
		if strings.TrimSpace(line) != "" {
			clean = false
			break
		}
	}

	relPath, _ := filepath.Rel(repoPath, cwd)

	r.println(fmt.Sprintf("Repo:     %s", repoName))
	r.println(fmt.Sprintf("Branch:   %s", RunnerAccentText("claude", branch)))
	r.println(fmt.Sprintf("Commit:   %s", MutedText(commit)))
	if clean {
		r.println(fmt.Sprintf("Status:   %s", PrimaryText("clean")))
	} else {
		r.println(fmt.Sprintf("Status:   %s", WarningText("dirty")))
	}
	if relPath != "." {
		r.println(fmt.Sprintf("Path:     %s", relPath))
	}

	return nil
}

func cwd() string {
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return ""
}

func findGitRoot(dir string) string {
	for {
		gitDir := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func runGitCmd(dir string, args []string) (stdout, stderr string, exitCode int) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), string(ee.Stderr), ee.ExitCode()
		}
		return "", err.Error(), 1
	}
	return string(out), "", 0
}

func handleLogin(ctx context.Context, r *REPL, args string) error {
	if r.runner == nil {
		return fmt.Errorf("no runner selected")
	}
	return r.runner.Login()
}

func handleLogout(ctx context.Context, r *REPL, args string) error {
	if r.runner == nil {
		return fmt.Errorf("no runner selected")
	}
	return r.runner.Logout()
}

func handleAuth(ctx context.Context, r *REPL, args string) error {
	r.println("Auth status:")
	for name, runner := range r.runners {
		status, err := runner.AuthStatus()
		if err != nil {
			r.println(fmt.Sprintf("  %s: error", RunnerAccentText(name, name)))
			continue
		}
		if status {
			r.println(fmt.Sprintf("  %s: authenticated", RunnerAccentText(name, name)))
		} else {
			r.println(fmt.Sprintf("  %s: not authenticated", RunnerAccentText(name, name)))
		}
	}
	return nil
}

func handleHelp(ctx context.Context, r *REPL, args string) error {
	r.println("Available commands:")
	r.println("")
	r.println("  Routing:")
	r.println("    /switch <runner>  Switch to another runner")
	r.println("    /stick           Keep current runner until released")
	r.println("    /back            Reverse the most recent switch")
	r.println("")
	r.println("  Session:")
	r.println("    /session [name]  Show or name the session")
	r.println("    /history         Show command history")
	r.println("    /summary         Show session statistics")
	r.println("    /cost            Show session cost")
	r.println("")
	r.println("  Quotas:")
	r.println("    /limit           Show runner quotas")
	r.println("")
	r.println("  Context:")
	r.println("    /openspec        Show current OpenSpec change")
	r.println("    /repo            Show current git repository")
	r.println("")
	r.println("  Auth:")
	r.println("    /login           Login to current runner")
	r.println("    /logout          Logout from current runner")
	r.println("    /auth            Show auth status for all runners")
	r.println("")
	r.println("  System:")
	r.println("    /help            Show this help")
	r.println("    /exit            Exit the REPL")
	r.println("")
	r.println("  Bash:")
	r.println("    !<command>       Run a bash command")
	return nil
}

func handleExit(ctx context.Context, r *REPL, args string) error {
	r.println("Goodbye!")
	return nil
}