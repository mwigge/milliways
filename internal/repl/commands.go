package repl

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type commandHandler func(ctx context.Context, r *REPL, args string) error

var commandHandlers = map[string]commandHandler{
	"switch":           handleSwitch,
	"stick":            handleStick,
	"back":             handleBack,
	"session":          handleSession,
	"history":          handleHistory,
	"summary":          handleSummary,
	"cost":             handleCost,
	"limit":            handleLimit,
	"openspec":         handleOpenspec,
	"repo":             handleRepo,
	"login":            handleLogin,
	"logout":           handleLogout,
	"auth":             handleAuth,
	"claude-reasoning":   handleClaudeReasoning,
	"claude-model":       handleClaudeModel,
	"minimax-reasoning":  handleMinimaxReasoning,
	"minimax-model":      handleMinimaxModel,
	"codex-review":     handleCodexReview,
	"codex-resume":     handleCodexResume,
	"codex-fork":       handleCodexFork,
	"codex-cloud":      handleCodexCloud,
	"codex-apply":      handleCodexApply,
	"codex-mcp":        handleCodexMCP,
	"codex-features":   handleCodexFeatures,
	"codex-model":      handleCodexModel,
	"codex-profile":    handleCodexProfile,
	"codex-sandbox":    handleCodexSandbox,
	"codex-approval":   handleCodexApproval,
	"codex-reasoning":  handleCodexReasoning,
	"codex-search":     handleCodexSearch,
	"codex-image":      handleCodexImage,
	"review-all":       handleReviewAll,
	"metrics":          handleMetrics,
	"logs":             handleLogs,
	"events":           handleEvents,
	"help":             handleHelp,
	"exit":             handleExit,
	// Runner shorthand aliases — /claude, /codex, etc. are equivalent to /switch <runner>
	"claude":  handleSwitch,
	"codex":   handleSwitch,
	"minimax": handleSwitch,
	"copilot": handleSwitch,
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
	r.loadRules()
	r.println(fmt.Sprintf("Switched to %s", RunnerAccentText(r.runner.Name(), r.runner.Name())))

	// Print runner-specific config so the user can verify settings on switch.
	switch runner := r.runner.(type) {
	case *MinimaxRunner:
		r.printMinimaxSettings(runner)
	case *ClaudeRunner:
		r.printClaudeSettings(runner)
	}

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
	sub, name := splitHead(args)

	switch sub {
	case "save":
		return handleSessionSave(r, name)
	case "load":
		return handleSessionLoad(r, name)
	case "list":
		return handleSessionList(r)
	case "clear":
		return handleSessionClear(r, name)
	case "":
		return handleSessionInfo(ctx, r)
	default:
		// Treat as a bare session name search (legacy substrate behaviour).
		return handleSessionSubstrateSearch(ctx, r, args)
	}
}

func handleSessionInfo(ctx context.Context, r *REPL) error {
	if r.session != nil {
		r.println(fmt.Sprintf("Current session: %s", r.session.conversationID))
		r.println(fmt.Sprintf("  Runner: %s", r.session.runnerName))
	} else {
		r.println("No active substrate session.")
	}

	if r.sessionStore != nil {
		runnerName := ""
		if r.runner != nil {
			runnerName = r.runner.Name()
		}
		r.println(fmt.Sprintf("  Runner: %s", runnerName))
		r.println(fmt.Sprintf("  Turns:  %d", len(r.turnBuffer)))
		if r.rules != "" {
			r.println(fmt.Sprintf("  Rules hash: %s", rulesHash(r.rules)[:12]+"..."))
		}
	}

	if r.substrate != nil {
		list, err := r.substrate.ConversationList(ctx)
		if err == nil && len(list) > 0 {
			r.println("")
			r.println("Stored substrate sessions:")
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

func handleSessionSave(r *REPL, name string) error {
	if r.sessionStore == nil {
		r.println("session store unavailable")
		return nil
	}
	if name == "" {
		return fmt.Errorf("usage: /session save <name>")
	}
	runnerName := ""
	if r.runner != nil {
		runnerName = r.runner.Name()
	}
	cwd, _ := os.Getwd()
	sess := PersistedSession{
		Version:    sessionVersion,
		SavedAt:    time.Now(),
		RunnerName: runnerName,
		RulesHash:  rulesHash(r.rules),
		WorkDir:    cwd,
		Turns:      r.turnBuffer,
	}
	if err := r.sessionStore.Save(name, sess); err != nil {
		return fmt.Errorf("saving session: %w", err)
	}
	r.println(fmt.Sprintf("session %q saved (%d turns)", name, len(r.turnBuffer)))
	return nil
}

func handleSessionLoad(r *REPL, name string) error {
	if r.sessionStore == nil {
		r.println("session store unavailable")
		return nil
	}
	if name == "" {
		return fmt.Errorf("usage: /session load <name>")
	}
	sess, err := r.sessionStore.Load(name)
	if err != nil {
		return fmt.Errorf("loading session: %w", err)
	}
	r.turnBuffer = sess.Turns
	r.println(fmt.Sprintf("session %q loaded (%d turns, saved %s)",
		name, len(sess.Turns), sess.SavedAt.Format("2006-01-02 15:04")))
	return nil
}

func handleSessionList(r *REPL) error {
	if r.sessionStore == nil {
		r.println("session store unavailable")
		return nil
	}
	metas, err := r.sessionStore.List()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}
	if len(metas) == 0 {
		r.println(MutedText("no saved sessions"))
		return nil
	}
	r.println(AccentColorText(r.scheme, "Saved sessions:"))
	r.println("")
	for _, m := range metas {
		age := time.Since(m.SavedAt).Round(time.Minute)
		r.println(fmt.Sprintf("  %-30s  %s  %2d turns  %s ago",
			m.Name, RunnerAccentText(m.Runner, m.Runner), m.Turns, age))
	}
	return nil
}

func handleSessionClear(r *REPL, args string) error {
	if args != "y" && args != "yes" {
		r.println(fmt.Sprintf("Clear %d turns? Type /session clear y to confirm.", len(r.turnBuffer)))
		return nil
	}
	r.turnBuffer = nil
	r.println("turn buffer cleared")
	return nil
}

func handleSessionSubstrateSearch(ctx context.Context, r *REPL, args string) error {
	if r.substrate == nil {
		return fmt.Errorf("session %q not found (mempalace not configured)", args)
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
			runnerName := ""
			if r.runner != nil {
				runnerName = r.runner.Name()
			}
			r.session = &replSession{
				conversationID: rec.ConversationID,
				runnerName:     runnerName,
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
		r.println(MutedText("no history yet"))
		return nil
	}
	scheme := r.scheme
	width := len(fmt.Sprintf("%d", len(r.history)))
	for i, h := range r.history {
		num := fmt.Sprintf("%*d", width, i+1)
		r.println(fmt.Sprintf("  %s  %s", MutedText(num), ColorText(scheme, h)))
	}
	r.println("")
	r.println(MutedText(fmt.Sprintf("  !N to re-run line N  !!  last  ctrl-r  search")))
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

func handleClaudeReasoning(ctx context.Context, r *REPL, args string) error {
	value := ClaudeReasoningMode(strings.ToLower(strings.TrimSpace(args)))
	if value == "" {
		value = ClaudeReasoningSummary
	}
	if value != ClaudeReasoningOff && value != ClaudeReasoningSummary && value != ClaudeReasoningVerbose {
		return fmt.Errorf("usage: /claude-reasoning [off|summary|verbose]")
	}
	cl, err := r.claudeRunner()
	if err != nil {
		return err
	}
	cl.SetReasoningMode(value)
	r.printClaudeSettings(cl)
	return nil
}

func handleClaudeModel(ctx context.Context, r *REPL, args string) error {
	cl, err := r.claudeRunner()
	if err != nil {
		return err
	}
	cl.SetModel(args)
	r.printClaudeSettings(cl)
	return nil
}

func (r *REPL) claudeRunner() (*ClaudeRunner, error) {
	runner, ok := r.runners["claude"]
	if !ok {
		return nil, fmt.Errorf("claude runner not registered")
	}
	cl, ok := runner.(*ClaudeRunner)
	if !ok {
		return nil, fmt.Errorf("claude runner does not support native settings")
	}
	return cl, nil
}

func (r *REPL) printClaudeSettings(cl *ClaudeRunner) {
	settings := cl.Settings()
	r.println(RunnerAccentText("claude", "Claude settings:"))
	r.println(fmt.Sprintf("  model:     %s", valueOrDefault(settings.Model, "default")))
	r.println(fmt.Sprintf("  reasoning: %s", string(settings.ReasoningMode)))
	if len(settings.AllowedTools) == 0 {
		r.println("  tools:     all")
		return
	}
	r.println("  tools:     " + strings.Join(settings.AllowedTools, ", "))
}

func handleMinimaxReasoning(ctx context.Context, r *REPL, args string) error {
	value := MinimaxReasoningMode(strings.ToLower(strings.TrimSpace(args)))
	if value == "" {
		value = MinimaxReasoningSummary
	}
	if value != MinimaxReasoningOff && value != MinimaxReasoningSummary && value != MinimaxReasoningVerbose {
		return fmt.Errorf("usage: /minimax-reasoning [off|summary|verbose]")
	}
	mm, err := r.minimaxRunner()
	if err != nil {
		return err
	}
	mm.SetReasoningMode(value)
	r.printMinimaxSettings(mm)
	return nil
}

func handleMinimaxModel(ctx context.Context, r *REPL, args string) error {
	mm, err := r.minimaxRunner()
	if err != nil {
		return err
	}
	mm.SetModel(args)
	r.printMinimaxSettings(mm)
	return nil
}

func (r *REPL) minimaxRunner() (*MinimaxRunner, error) {
	runner, ok := r.runners["minimax"]
	if !ok {
		return nil, fmt.Errorf("minimax runner not registered")
	}
	mm, ok := runner.(*MinimaxRunner)
	if !ok {
		return nil, fmt.Errorf("minimax runner does not support native settings")
	}
	return mm, nil
}

func (r *REPL) printMinimaxSettings(mm *MinimaxRunner) {
	settings := mm.Settings()
	r.println(RunnerAccentText("minimax", "MiniMax settings:"))
	r.println(fmt.Sprintf("  model:     %s", valueOrDefault(settings.Model, "default")))
	r.println(fmt.Sprintf("  reasoning: %s", string(settings.ReasoningMode)))
	r.println(fmt.Sprintf("  url:       %s", settings.URL))
	const doubledPath = "/text/chatcompletion_v2/text/chatcompletion_v2"
	if strings.Contains(settings.URL, doubledPath) {
		r.println(WarningText("  ! url looks doubled — set base_url: https://api.minimax.io/v1 in carte.yaml (no path suffix)"))
	}
}

func handleCodexReview(ctx context.Context, r *REPL, args string) error {
	cmdArgs := []string{"review"}
	if strings.TrimSpace(args) == "" {
		cmdArgs = append(cmdArgs, "--uncommitted")
	} else {
		cmdArgs = append(cmdArgs, strings.Fields(args)...)
	}
	return r.runCodexCommand(ctx, cmdArgs...)
}

func handleCodexResume(ctx context.Context, r *REPL, args string) error {
	cmdArgs := []string{"resume"}
	if strings.TrimSpace(args) == "" {
		cmdArgs = append(cmdArgs, "--last")
	} else {
		cmdArgs = append(cmdArgs, strings.Fields(args)...)
	}
	return r.runCodexCommand(ctx, cmdArgs...)
}

func handleCodexFork(ctx context.Context, r *REPL, args string) error {
	cmdArgs := []string{"fork"}
	if strings.TrimSpace(args) == "" {
		cmdArgs = append(cmdArgs, "--last")
	} else {
		cmdArgs = append(cmdArgs, strings.Fields(args)...)
	}
	return r.runCodexCommand(ctx, cmdArgs...)
}

func handleCodexCloud(ctx context.Context, r *REPL, args string) error {
	cmdArgs := []string{"cloud"}
	if strings.TrimSpace(args) == "" {
		cmdArgs = append(cmdArgs, "list")
	} else {
		cmdArgs = append(cmdArgs, strings.Fields(args)...)
	}
	return r.runCodexCommand(ctx, cmdArgs...)
}

func handleCodexApply(ctx context.Context, r *REPL, args string) error {
	if strings.TrimSpace(args) == "" {
		return fmt.Errorf("usage: /codex-apply <task-id>")
	}
	return r.runCodexCommand(ctx, append([]string{"apply"}, strings.Fields(args)...)...)
}

func handleCodexMCP(ctx context.Context, r *REPL, args string) error {
	cmdArgs := []string{"mcp"}
	if strings.TrimSpace(args) == "" {
		cmdArgs = append(cmdArgs, "list")
	} else {
		cmdArgs = append(cmdArgs, strings.Fields(args)...)
	}
	return r.runCodexCommand(ctx, cmdArgs...)
}

func handleCodexFeatures(ctx context.Context, r *REPL, args string) error {
	cmdArgs := []string{"features"}
	if strings.TrimSpace(args) == "" {
		cmdArgs = append(cmdArgs, "list")
	} else {
		cmdArgs = append(cmdArgs, strings.Fields(args)...)
	}
	return r.runCodexCommand(ctx, cmdArgs...)
}

func handleCodexModel(ctx context.Context, r *REPL, args string) error {
	codex, err := r.codexRunner()
	if err != nil {
		return err
	}
	codex.SetModel(args)
	r.printCodexSettings(codex)
	return nil
}

func handleCodexProfile(ctx context.Context, r *REPL, args string) error {
	codex, err := r.codexRunner()
	if err != nil {
		return err
	}
	codex.SetProfile(args)
	r.printCodexSettings(codex)
	return nil
}

func handleCodexSandbox(ctx context.Context, r *REPL, args string) error {
	value := strings.TrimSpace(args)
	if value != "" && value != "read-only" && value != "workspace-write" && value != "danger-full-access" {
		return fmt.Errorf("usage: /codex-sandbox [read-only|workspace-write|danger-full-access]")
	}
	codex, err := r.codexRunner()
	if err != nil {
		return err
	}
	codex.SetSandbox(value)
	r.printCodexSettings(codex)
	return nil
}

func handleCodexApproval(ctx context.Context, r *REPL, args string) error {
	value := strings.TrimSpace(args)
	if value != "" && value != "untrusted" && value != "on-request" && value != "never" {
		return fmt.Errorf("usage: /codex-approval [untrusted|on-request|never]")
	}
	codex, err := r.codexRunner()
	if err != nil {
		return err
	}
	codex.SetApproval(value)
	r.printCodexSettings(codex)
	return nil
}

func handleCodexReasoning(ctx context.Context, r *REPL, args string) error {
	value := CodexReasoningMode(strings.ToLower(strings.TrimSpace(args)))
	if value == "" {
		value = CodexReasoningVerbose
	}
	if value != CodexReasoningOff && value != CodexReasoningSummary && value != CodexReasoningVerbose {
		return fmt.Errorf("usage: /codex-reasoning [off|summary|verbose]")
	}
	codex, err := r.codexRunner()
	if err != nil {
		return err
	}
	codex.SetReasoningMode(value)
	r.printCodexSettings(codex)
	return nil
}

func handleCodexSearch(ctx context.Context, r *REPL, args string) error {
	value := strings.ToLower(strings.TrimSpace(args))
	if value != "on" && value != "off" {
		return fmt.Errorf("usage: /codex-search <on|off>")
	}
	codex, err := r.codexRunner()
	if err != nil {
		return err
	}
	codex.SetSearch(value == "on")
	r.printCodexSettings(codex)
	return nil
}

func handleCodexImage(ctx context.Context, r *REPL, args string) error {
	codex, err := r.codexRunner()
	if err != nil {
		return err
	}

	fields := strings.Fields(args)
	if len(fields) == 0 || fields[0] == "list" {
		r.printCodexSettings(codex)
		return nil
	}
	switch fields[0] {
	case "add":
		if len(fields) < 2 {
			return fmt.Errorf("usage: /codex-image add <path>")
		}
		codex.AddImage(strings.Join(fields[1:], " "))
	case "clear":
		codex.ClearImages()
	default:
		codex.AddImage(strings.Join(fields, " "))
	}
	r.printCodexSettings(codex)
	return nil
}

func (r *REPL) codexRunner() (*CodexRunner, error) {
	runner, ok := r.runners["codex"]
	if !ok {
		return nil, fmt.Errorf("codex runner not registered")
	}
	codex, ok := runner.(*CodexRunner)
	if !ok {
		return nil, fmt.Errorf("codex runner does not support native settings")
	}
	return codex, nil
}

func (r *REPL) runCodexCommand(ctx context.Context, args ...string) error {
	if len(args) == 0 {
		return nil
	}
	r.println(fmt.Sprintf("[%s] %s", RunnerAccentText("codex", "codex"), strings.Join(args, " ")))
	cmd := exec.CommandContext(ctx, "codex", args...)
	writer := &teeWriter{w: r.stdout, buf: new(bytes.Buffer), scheme: CodexScheme()}
	err := streamCmdOutput(ctx, cmd, writer)
	writer.Flush()
	return err
}

func (r *REPL) printCodexSettings(codex *CodexRunner) {
	settings := codex.Settings()
	r.println(RunnerAccentText("codex", "Codex settings:"))
	r.println(fmt.Sprintf("  model:    %s", valueOrDefault(settings.Model, "default")))
	r.println(fmt.Sprintf("  profile:  %s", valueOrDefault(settings.Profile, "default")))
	r.println(fmt.Sprintf("  sandbox:  %s", valueOrDefault(settings.Sandbox, "default")))
	r.println(fmt.Sprintf("  approval: %s", valueOrDefault(settings.Approval, "default")))
	r.println(fmt.Sprintf("  reasoning:%s", " "+string(settings.Reasoning)))
	r.println(fmt.Sprintf("  search:   %t", settings.Search))
	if len(settings.Images) == 0 {
		r.println("  images:   none")
		return
	}
	r.println("  images:")
	for _, image := range settings.Images {
		r.println(fmt.Sprintf("    %s", image))
	}
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func handleReviewAll(ctx context.Context, r *REPL, args string) error {
	target, err := resolveReviewTarget(args)
	if err != nil {
		return err
	}

	// Collect authenticated runners in stable order.
	var names []string
	for name := range r.runners {
		names = append(names, name)
	}
	sort.Strings(names)
	var activeRunners []Runner
	var activeNames []string
	for _, name := range names {
		runner := r.runners[name]
		ok, authErr := runner.AuthStatus()
		if authErr != nil || !ok {
			r.println(MutedText(fmt.Sprintf("  skip %s (not authenticated)", name)))
			continue
		}
		activeRunners = append(activeRunners, runner)
		activeNames = append(activeNames, name)
	}
	if len(activeRunners) == 0 {
		return fmt.Errorf("no authenticated runners available")
	}

	r.println(AccentColorText(r.scheme, fmt.Sprintf("review-all: %s", target.Label)))
	r.println(MutedText(fmt.Sprintf("  runners: %s", strings.Join(activeNames, ", "))))
	r.println("")

	sections := make(map[string]string, len(activeRunners))

	for i, runner := range activeRunners {
		name := activeNames[i]
		rscheme := SchemeForRunner(name)

		var prompt string
		if len(target.Repos) == 1 && filepath.IsAbs(target.Repos[0]) {
			prompt = buildReviewPrompt(target, "")
		} else {
			prompt = buildRemoteReviewPrompt(target.Repos)
		}

		req := DispatchRequest{
			Prompt:   prompt,
			History:  nil, // review is stateless
			Rules:    r.rules,
			ClientID: "repl/review-all/" + name,
		}

		r.println(fmt.Sprintf("  %s reviewing...", RunnerAccentText(name, name)))
		start := time.Now()
		output, execErr := collectReview(ctx, runner, req)
		dur := time.Since(start)

		if execErr != nil {
			r.println(fmt.Sprintf("  %s %s  %.1fs  %v",
				ColorText(rscheme, "✗"), RunnerAccentText(name, name), dur.Seconds(), execErr))
			sections[name] = fmt.Sprintf("_Error: %v_", execErr)
			continue
		}
		r.println(fmt.Sprintf("  %s %s  %.1fs  %d chars",
			ColorText(rscheme, "✓"), RunnerAccentText(name, name), dur.Seconds(), len(output)))
		sections[name] = output
	}

	// Write output file.
	reviewCwd, _ := os.Getwd()
	root := findGitRootFrom(reviewCwd)
	if root == "" {
		root = reviewCwd
	}
	path, writeErr := writeReviewFile(root, target.Label, sections, activeNames)
	if writeErr != nil {
		return fmt.Errorf("writing review: %w", writeErr)
	}
	r.println("")
	r.println(AccentColorText(r.scheme, "review saved: "+path))
	return nil
}

func handleMetrics(ctx context.Context, r *REPL, args string) error {
	scheme := r.scheme
	r.println(AccentColorText(scheme, "Session metrics"))
	r.println("")

	var totalCost float64
	var totalIn, totalOut, totalDispatches int

	for name, runner := range r.runners {
		rscheme := SchemeForRunner(name)
		q, err := runner.Quota()
		if err != nil || q == nil || q.Session == nil || q.Session.Dispatches == 0 {
			r.println(fmt.Sprintf("  %s  no dispatches", RunnerAccentText(name, name)))
			continue
		}
		s := q.Session
		var parts []string
		if s.CostUSD > 0 {
			parts = append(parts, fmt.Sprintf("$%.4f", s.CostUSD))
		}
		if s.InputTokens > 0 {
			parts = append(parts, fmt.Sprintf("%s↑", compactTokens(s.InputTokens)))
		}
		if s.OutputTokens > 0 {
			parts = append(parts, fmt.Sprintf("%s↓", compactTokens(s.OutputTokens)))
		}
		parts = append(parts, fmt.Sprintf("%d dispatches", s.Dispatches))
		r.println(fmt.Sprintf("  %s  %s",
			RunnerAccentText(name, name),
			AccentColorText(rscheme, strings.Join(parts, "  "))))
		totalCost += s.CostUSD
		totalIn += s.InputTokens
		totalOut += s.OutputTokens
		totalDispatches += s.Dispatches
	}

	if totalDispatches > 0 {
		r.println("")
		r.println(fmt.Sprintf("  %s  $%.4f  %s↑  %s↓  %d dispatches",
			MutedText("total"),
			totalCost,
			compactTokens(totalIn),
			compactTokens(totalOut),
			totalDispatches))
	}
	return nil
}

func handleLogs(ctx context.Context, r *REPL, args string) error {
	if r.logHandler == nil {
		r.println(MutedText("log buffer not available"))
		return nil
	}
	n := 50
	if args != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(args)); err == nil && v > 0 {
			n = v
		}
	}
	records := r.logHandler.Buffer().Recent(n)
	if len(records) == 0 {
		r.println(MutedText("no log entries"))
		return nil
	}
	scheme := r.scheme
	for _, rec := range records {
		ts := rec.At.Format("15:04:05")
		level := rec.Level.String()
		line := fmt.Sprintf("%s  %-5s  %s", ts, level, rec.Message)
		for _, k := range []string{"runner", "err", "prompt_len", "duration_ms", "path", "bytes"} {
			if v, ok := rec.Attrs[k]; ok {
				line += fmt.Sprintf("  %s=%v", k, v)
			}
		}
		switch rec.Level {
		case slog.LevelWarn, slog.LevelError:
			r.println(AccentColorText(scheme, line))
		default:
			r.println(MutedText(line))
		}
	}
	return nil
}

func handleEvents(ctx context.Context, r *REPL, args string) error {
	if len(r.turnBuffer) == 0 {
		r.println(MutedText("no events this session"))
		return nil
	}
	r.println(AccentColorText(r.scheme, fmt.Sprintf("Session events (%d turns)", len(r.turnBuffer))))
	r.println("")
	for _, t := range r.turnBuffer {
		ts := "??:??"
		if !t.At.IsZero() {
			ts = t.At.Format("15:04:05")
		}
		snippet := t.Text
		if len(snippet) > 120 {
			snippet = snippet[:120] + "…"
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		rscheme := SchemeForRunner(t.Runner)
		roleLabel := MutedText(fmt.Sprintf("%-9s", t.Role))
		runnerLabel := RunnerAccentText(t.Runner, t.Runner)
		r.println(fmt.Sprintf("  %s  %s  %s  %s",
			MutedText(ts),
			roleLabel,
			runnerLabel,
			ColorText(rscheme, snippet)))
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
	r.println("    /history         Show command history (!N re-run line N, !! last, ctrl-r search)")
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
	r.println("  Review:")
	r.println("    /review-all [branch]               Review current/named branch (all authenticated runners)")
	r.println("    /review-all openspec <name>        Review OpenSpec change")
	r.println("    /review-all <org>/<pattern>        Review matching repos (requires gh CLI)")
	r.println("")
	r.println("  Claude:")
	r.println("    /claude-reasoning [mode]  Set progress detail: off, summary, verbose (default: verbose)")
	r.println("    /claude-model <model>     Set model for /claude prompts")
	r.println("")
	r.println("  MiniMax:")
	r.println("    /minimax-reasoning [mode]  Set progress detail: off, summary, verbose (default: verbose)")
	r.println("    /minimax-model <model>     Set model for /minimax prompts")
	r.println("")
	r.println("  Codex:")
	r.println("    /codex-review [args]    Run codex review (default: --uncommitted)")
	r.println("    /codex-resume [args]    Resume Codex (default: --last)")
	r.println("    /codex-fork [args]      Fork Codex session (default: --last)")
	r.println("    /codex-cloud [args]     Run Codex Cloud command (default: list)")
	r.println("    /codex-apply <task-id>  Apply Codex task diff")
	r.println("    /codex-mcp [args]       Manage Codex MCP (default: list)")
	r.println("    /codex-features [args]  Inspect Codex features (default: list)")
	r.println("    /codex-model <model>    Set model for /codex prompts")
	r.println("    /codex-profile <name>   Set config profile for /codex prompts")
	r.println("    /codex-sandbox <mode>   Set sandbox for /codex prompts")
	r.println("    /codex-approval <mode>  Set approval policy for /codex prompts")
	r.println("    /codex-reasoning [mode] Set progress detail: off, summary, verbose")
	r.println("    /codex-search <on|off>  Toggle web search for /codex prompts")
	r.println("    /codex-image add|clear|list [path]  Attach images to /codex prompts")
	r.println("")
	r.println("  Observability:")
	r.println("    /metrics         Show per-runner cost and token usage")
	r.println("    /logs [N]        Show last N log entries (default 50)")
	r.println("    /events          Show conversation events this session")
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
