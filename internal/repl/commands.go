// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package repl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
	"switch":            handleSwitch,
	"stick":             handleStick,
	"back":              handleBack,
	"session":           handleSession,
	"history":           handleHistory,
	"summary":           handleSummary,
	"cost":              handleCost,
	"limit":             handleLimit,
	"openspec":          handleOpenspec,
	"repo":              handleRepo,
	"login":             handleLogin,
	"logout":            handleLogout,
	"auth":              handleAuth,
	"model":             handleModel,
	"claude-reasoning":  handleClaudeReasoning,
	"claude-model":      handleClaudeModel,
	"minimax-reasoning": handleMinimaxReasoning,
	"minimax-model":     handleMinimaxModel,
	"codex-review":      handleCodexReview,
	"codex-resume":      handleCodexResume,
	"codex-fork":        handleCodexFork,
	"codex-cloud":       handleCodexCloud,
	"codex-apply":       handleCodexApply,
	"codex-mcp":         handleCodexMCP,
	"codex-features":    handleCodexFeatures,
	"codex-model":       handleCodexModel,
	"codex-profile":     handleCodexProfile,
	"codex-sandbox":     handleCodexSandbox,
	"codex-approval":    handleCodexApproval,
	"codex-reasoning":   handleCodexReasoning,
	"codex-search":      handleCodexSearch,
	"codex-image":       handleCodexImage,
	"apply":             handleApply,
	"image":             handleImage,
	"pptx":              handlePptx,
	"drawio":            handleDrawio,
	"review-all":        handleReviewAll,
	"metrics":           handleMetrics,
	"logs":              handleLogs,
	"events":            handleEvents,
	"help":              handleHelp,
	"exit":              handleExit,
	// OpenSpec commands
	"opsx:list":     handleOpsxList,
	"opsx:status":   handleOpsxStatus,
	"opsx:show":     handleOpsxShow,
	"opsx:apply":    handleOpsxApply,
	"opsx:explore":  handleOpsxExplore,
	"opsx:archive":  handleOpsxArchive,
	"opsx:validate": handleOpsxValidate,
	// Runner shorthand aliases — /claude, /minimax, etc. are equivalent to /switch <runner>
	"claude":  func(ctx context.Context, r *REPL, _ string) error { return handleSwitch(ctx, r, "claude") },
	"codex":   func(ctx context.Context, r *REPL, _ string) error { return handleSwitch(ctx, r, "codex") },
	"minimax": func(ctx context.Context, r *REPL, _ string) error { return handleSwitch(ctx, r, "minimax") },
	"copilot": func(ctx context.Context, r *REPL, _ string) error { return handleSwitch(ctx, r, "copilot") },
	"pool":    func(ctx context.Context, r *REPL, _ string) error { return handleSwitch(ctx, r, "pool") },
	"gemini":  func(ctx context.Context, r *REPL, _ string) error { return handleSwitch(ctx, r, "gemini") },
	"local":   func(ctx context.Context, r *REPL, _ string) error { return handleSwitch(ctx, r, "local") },
	// Pool-specific commands
	"pool-model": handlePoolModel,
	"pool-mode":  handlePoolMode,
	// Gemini-specific commands
	"gemini-model": handleGeminiModel,
	// Local-specific commands
	"local-model":      handleLocalModel,
	"local-models":     handleLocalModels,
	"local-endpoint":   handleLocalEndpoint,
	"local-temp":       handleLocalTemp,
	"local-max-tokens": handleLocalMaxTokens,
	"local-hot":        handleLocalHot,
	// Local-bootstrap aliases — dispatch to `milliwaysctl local <verb>` so the
	// legacy --repl line-reader has the same UX as the milliways-term Leader+/
	// palette. Multiple word orderings registered for muscle memory.
	"install-local-server":  localCtlAlias("install-server"),
	"local-install-server":  localCtlAlias("install-server"),
	"local-server-install":  localCtlAlias("install-server"),
	"install-local-swap":    localCtlAlias("install-swap"),
	"local-install-swap":    localCtlAlias("install-swap"),
	"local-swap-install":    localCtlAlias("install-swap"),
	"list-local-models":     localCtlAlias("list-models"),
	"local-list-models":     localCtlAlias("list-models"),
	"switch-local-server":   localCtlAlias("switch-server"),
	"local-switch-server":   localCtlAlias("switch-server"),
	"download-local-model":  localCtlAlias("download-model"),
	"local-download-model":  localCtlAlias("download-model"),
	"setup-local-model":     localCtlAlias("setup-model"),
	"local-setup-model":     localCtlAlias("setup-model"),
	// /models — list available models for the active runner (local-first).
	"models": handleModelsContextual,
	// ? — show milliways shortcuts reference
	"?": handleShortcuts,
	// Rotation ring and takeover
	"takeover-ring": handleTakeoverRing,
	"takeover":      handleTakeover,
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
	case *ClaudeRunner:
		r.printClaudeSettings(runner)
	case *CodexRunner:
		r.printCodexSettings(runner)
	case *MinimaxRunner:
		r.printMinimaxSettings(runner)
	case *CopilotRunner:
		r.printCopilotSettings(runner)
	case *PoolRunner:
		r.printPoolSettings(runner)
	case *GeminiRunner:
		r.printGeminiSettings(runner)
	case *LocalRunner:
		r.printLocalSettings(runner)
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

// modelCompletionContext returns the candidate model IDs, the partial text
// typed after the command, and the full line prefix up to the partial word —
// for tab-completion of model IDs.
//
// Returns (nil, "", "") when the line does not match a model command.
func modelCompletionContext(r *REPL, line string) (ids []string, partial, linePrefix string) {
	// Commands that accept a model ID argument.
	type cmdSpec struct {
		cmd     string // e.g. "/model "
		catalog func() []string
	}

	claudeIDs := func() []string {
		out := make([]string, 0, len(ClaudeModelCatalog))
		for _, e := range ClaudeModelCatalog {
			out = append(out, e.ID)
		}
		return out
	}
	codexIDs := func() []string {
		out := make([]string, 0, len(CodexModelCatalog))
		for _, e := range CodexModelCatalog {
			out = append(out, e.ID)
		}
		return out
	}
	minimaxIDs := func() []string {
		out := make([]string, 0, len(MinimaxModelCatalog))
		for _, e := range MinimaxModelCatalog {
			out = append(out, e.ID)
		}
		return out
	}

	// /model uses the active runner's catalog.
	runnerIDsForModel := func() []string {
		if r.runner == nil {
			return nil
		}
		switch r.runner.(type) {
		case *ClaudeRunner:
			return claudeIDs()
		case *CodexRunner:
			return codexIDs()
		case *MinimaxRunner:
			return minimaxIDs()
		}
		return nil
	}

	specs := []cmdSpec{
		{"/model ", runnerIDsForModel},
		{"/claude-model ", claudeIDs},
		{"/codex-model ", codexIDs},
		{"/minimax-model ", minimaxIDs},
	}

	for _, spec := range specs {
		if !strings.HasPrefix(line, spec.cmd) {
			continue
		}
		partial = line[len(spec.cmd):]
		catalog := spec.catalog()
		if catalog == nil {
			return nil, "", ""
		}
		return catalog, partial, spec.cmd
	}
	return nil, "", ""
}

func handleModel(ctx context.Context, r *REPL, args string) error {
	if r.runner == nil {
		return fmt.Errorf("no runner selected")
	}
	args = strings.TrimSpace(args)
	switch runner := r.runner.(type) {
	case *ClaudeRunner:
		if args == "" {
			picked := r.pickClaudeModel(runner)
			if picked != "" {
				runner.SetModel(picked)
				r.printClaudeSettings(runner)
			} else {
				r.printClaudeModelCatalog(runner)
			}
			return nil
		}
		runner.SetModel(args)
		r.printClaudeSettings(runner)
	case *CodexRunner:
		if args == "" {
			picked := r.pickCodexModel(runner)
			if picked != "" {
				runner.SetModel(picked)
				r.printCodexSettings(runner)
			} else {
				r.printCodexModelCatalog(runner)
			}
			return nil
		}
		runner.SetModel(args)
		r.printCodexSettings(runner)
	case *MinimaxRunner:
		return handleMinimaxModel(ctx, r, args)
	case *PoolRunner:
		return handlePoolModel(ctx, r, args)
	case *GeminiRunner:
		return handleGeminiModel(ctx, r, args)
	case *LocalRunner:
		return handleLocalModel(ctx, r, args)
	case *CopilotRunner:
		r.println(MutedText("copilot: model selection is managed by GitHub — not configurable here"))
	default:
		r.println(MutedText(fmt.Sprintf("%s: /model not supported", r.runner.Name())))
	}
	return nil
}

func (r *REPL) printClaudeModelCatalog(cl *ClaudeRunner) {
	current := cl.Settings().Model
	r.println(RunnerAccentText("claude", "Claude models:"))
	r.println("")
	for _, e := range ClaudeModelCatalog {
		marker := "  "
		if e.ID == current {
			marker = "* "
		}
		note := ""
		if e.Note != "" {
			note = "  " + MutedText(e.Note)
		}
		r.println(fmt.Sprintf("  %s%-34s%s", marker, e.ID, note))
	}
	r.println("")
	r.println(MutedText("  /model <id> to switch"))
}

func (r *REPL) printCodexModelCatalog(codex *CodexRunner) {
	current := codex.Settings().Model
	r.println(RunnerAccentText("codex", "Codex models:"))
	r.println("")
	for _, e := range CodexModelCatalog {
		marker := "  "
		if e.ID == current {
			marker = "* "
		}
		note := ""
		if e.Note != "" {
			note = "  " + MutedText(e.Note)
		}
		r.println(fmt.Sprintf("  %s%-20s%s", marker, e.ID, note))
	}
	r.println("")
	r.println(MutedText("  /model <id> to switch"))
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
	if strings.TrimSpace(args) == "" {
		picked := r.pickMinimaxModel(mm)
		if picked != "" {
			mm.SetModel(picked)
			r.printMinimaxSettings(mm)
		} else {
			r.printMinimaxCatalog()
		}
		return nil
	}
	mm.SetModel(args)
	r.printMinimaxSettings(mm)
	return nil
}

func (r *REPL) printMinimaxCatalog() {
	r.println(RunnerAccentText("minimax", "MiniMax models:"))
	r.println("")
	kinds := []MinimaxModelKind{MinimaxKindChat, MinimaxKindImage, MinimaxKindMusic, MinimaxKindLyrics}
	for _, kind := range kinds {
		r.println(fmt.Sprintf("  %s:", string(kind)))
		for _, e := range MinimaxModelCatalog {
			if e.Kind != kind {
				continue
			}
			note := ""
			if e.Note != "" {
				note = "  " + MutedText(e.Note)
			}
			r.println(fmt.Sprintf("    %-26s%s", e.ID, note))
		}
		r.println("")
	}
	r.println(MutedText("  /minimax-model <id> to switch"))
}

// pickClaudeModel opens the interactive picker for Claude models.
// Returns the selected model ID, or "" if the picker was cancelled or stdin is
// not a terminal (caller should fall back to printing the catalog).
func (r *REPL) pickClaudeModel(cl *ClaudeRunner) string {
	ids := make([]string, 0, len(ClaudeModelCatalog))
	for _, e := range ClaudeModelCatalog {
		ids = append(ids, e.ID)
	}
	return pickFromList(r.stdout, ids, cl.Settings().Model)
}

// pickCodexModel opens the interactive picker for Codex models.
func (r *REPL) pickCodexModel(codex *CodexRunner) string {
	ids := make([]string, 0, len(CodexModelCatalog))
	for _, e := range CodexModelCatalog {
		ids = append(ids, e.ID)
	}
	return pickFromList(r.stdout, ids, codex.Settings().Model)
}

// pickMinimaxModel opens the interactive picker for MiniMax models.
func (r *REPL) pickMinimaxModel(mm *MinimaxRunner) string {
	ids := make([]string, 0, len(MinimaxModelCatalog))
	for _, e := range MinimaxModelCatalog {
		ids = append(ids, e.ID)
	}
	return pickFromList(r.stdout, ids, mm.Settings().Model)
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
	r.println(fmt.Sprintf("  kind:      %s", string(settings.Kind)))
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

// handleImage manages the pending-attachment queue for multi-runner image input.
//
// Usage:
//
//	/image <path>    — load image and add to queue
//	/image list      — list pending images
//	/image clear     — clear pending images
//	/image           — same as /image list
func handleImage(ctx context.Context, r *REPL, args string) error {
	args = strings.TrimSpace(args)

	switch {
	case args == "" || args == "list":
		if len(r.pendingAttachments) == 0 {
			r.println(MutedText("no pending images"))
			return nil
		}
		for i, a := range r.pendingAttachments {
			r.println(fmt.Sprintf("  [%d] %s (%s)", i, a.FilePath, a.MimeType))
		}
		return nil

	case args == "clear":
		n := len(r.pendingAttachments)
		r.pendingAttachments = nil
		r.println(fmt.Sprintf("cleared %d image(s)", n))
		return nil

	default:
		a, err := LoadImageAttachment(args)
		if err != nil {
			return err
		}
		r.pendingAttachments = append(r.pendingAttachments, a)
		r.println(fmt.Sprintf("queued: %s (%s)", args, a.MimeType))
		return nil
	}
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
	binary := resolveCodexBinary()
	if codex, err := r.codexRunner(); err == nil && codex.binary != "" {
		binary = codex.binary
	}
	cmd := exec.CommandContext(ctx, binary, args...)
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

func (r *REPL) printCopilotSettings(c *CopilotRunner) {
	r.println(RunnerAccentText("copilot", "Copilot settings:"))
	r.println(fmt.Sprintf("  binary:  %s", c.binary))
}

func handlePoolModel(ctx context.Context, r *REPL, args string) error {
	p, ok := r.runner.(*PoolRunner)
	if !ok {
		return fmt.Errorf("not on pool runner — use /pool first")
	}
	if args == "" {
		model := p.model
		if model == "" {
			model = "(default)"
		}
		r.println(fmt.Sprintf("pool model: %s", model))
		return nil
	}
	p.SetModel(args)
	r.println(fmt.Sprintf("pool model set to %s", args))
	return nil
}

func handlePoolMode(ctx context.Context, r *REPL, args string) error {
	p, ok := r.runner.(*PoolRunner)
	if !ok {
		return fmt.Errorf("not on pool runner — use /pool first")
	}
	if args == "" {
		mode := p.mode
		if mode == "" {
			mode = "(default)"
		}
		r.println(fmt.Sprintf("pool mode: %s", mode))
		return nil
	}
	p.SetMode(args)
	r.println(fmt.Sprintf("pool mode set to %s", args))
	return nil
}

func (r *REPL) printPoolSettings(p *PoolRunner) {
	r.println(RunnerAccentText("pool", "Pool settings:"))
	r.println(fmt.Sprintf("  binary:  %s", p.binary))
	if p.model != "" {
		r.println(fmt.Sprintf("  model:   %s", p.model))
	}
	if p.mode != "" {
		r.println(fmt.Sprintf("  mode:    %s", p.mode))
	}
}

func (r *REPL) printGeminiSettings(g *GeminiRunner) {
	r.println(RunnerAccentText("gemini", "Gemini settings:"))
	r.println(fmt.Sprintf("  binary:  %s", g.binary))
	if g.model != "" {
		r.println(fmt.Sprintf("  model:   %s", g.model))
	}
}

func handleGeminiModel(ctx context.Context, r *REPL, args string) error {
	g, ok := r.runner.(*GeminiRunner)
	if !ok {
		return fmt.Errorf("not on gemini runner — use /gemini first")
	}
	if args == "" {
		model := g.model
		if model == "" {
			model = "(default)"
		}
		r.println(fmt.Sprintf("gemini model: %s", model))
		return nil
	}
	g.SetModel(args)
	r.println(fmt.Sprintf("gemini model set to %s", args))
	return nil
}

// handleShortcuts prints the milliways key shortcuts and command reference.
func handleShortcuts(_ context.Context, r *REPL, _ string) error {
	lines := []string{
		"milliways shortcuts:",
		"  ?               Show this shortcuts reference",
		"  /help           Full command list",
		"  /switch <r>     Switch runner  (or just /<runner>)",
		"  /claude         Switch to claude",
		"  /codex          Switch to codex",
		"  /copilot        Switch to copilot",
		"  /gemini         Switch to gemini",
		"  /minimax        Switch to minimax",
		"  /pool           Switch to pool",
		"  /model          List/set model for current runner",
		"  /takeover [r]   Handoff with briefing injection",
		"  /takeover-ring  Configure rotation ring",
		"  /session        Session management",
		"  /history        Show conversation history",
		"  /cost           Show session cost",
		"  /login <r>      Authenticate a runner",
		"  !<cmd>          Run a shell command",
		"  /exit           Quit milliways",
	}
	for _, l := range lines {
		r.println(l)
	}
	return nil
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
	r.println("    /claude           Switch to claude (shorthand for /switch claude)")
	r.println("    /codex            Switch to codex (shorthand for /switch codex)")
	r.println("    /minimax          Switch to minimax (shorthand for /switch minimax)")
	r.println("    /copilot          Switch to copilot (shorthand for /switch copilot)")
	r.println("    /stick            Keep current runner until released")
	r.println("    /back             Reverse the most recent switch")
	r.println("    /model            List models for the current runner")
	r.println("    /model <id>       Set model for the current runner")
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
	r.println("  OpenSpec:")
	r.println("    /opsx:list               List active changes")
	r.println("    /opsx:status [name]      Show task completion (default: current change)")
	r.println("    /opsx:show <name>        Show change detail")
	r.println("    /opsx:apply <name>       Fetch instructions and dispatch to current runner")
	r.println("    /opsx:explore [name]     Explore change — think/investigate, no implementation")
	r.println("    /opsx:archive <name>     Archive a completed change")
	r.println("    /opsx:validate <name>    Validate a change")
	r.println("")
	r.println("  Images:")
	r.println("    /image <path>    Load image and add to pending queue (sent with next prompt)")
	r.println("    /image list      List pending images")
	r.println("    /image clear     Clear pending images")
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
	r.println("    /minimax-model             List all available models (chat, image, music, lyrics)")
	r.println("    /minimax-model <model>     Set model — routes to the correct endpoint automatically")
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
	r.println("  Artifacts:")
	r.println("    /pptx <topic>    Generate a PowerPoint presentation (python-pptx, saved to cwd)")
	r.println("    /drawio <topic>  Generate a draw.io diagram XML (saved to cwd)")
	r.println("    /apply           Extract fenced code blocks from last AI response and write to files")
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
	r.println("    /exit            Exit")
	r.println("")
	r.println("  Bash:")
	r.println("    !<command>       Run a bash command")
	return nil
}

func handleApply(ctx context.Context, r *REPL, args string) error {
	text := r.lastAssistantText()
	if text == "" {
		r.println(MutedText("no assistant response in current session"))
		return nil
	}

	blocks := ExtractCodeBlocks(text)
	if len(blocks) == 0 {
		r.println(MutedText("no code blocks found in last response"))
		return nil
	}

	applied := 0
	for _, block := range blocks {
		label := block.FilePath
		if label == "" {
			label = "(no path)"
		}
		r.println(fmt.Sprintf("[%d] %s %s  (%d bytes)",
			block.Index, block.Lang, label, len(block.Content)))

		// Print first 3 lines of content as preview.
		previewLines := strings.SplitN(block.Content, "\n", 5)
		for i, pl := range previewLines {
			if i >= 3 {
				r.println("  ...")
				break
			}
			r.println("  " + pl)
		}

		response, err := r.input.Prompt("  apply? [y/N/path]: ")
		if err != nil {
			// EOF or abort — stop processing.
			break
		}
		response = strings.TrimSpace(response)

		var path string
		switch {
		case response == "y" || response == "Y":
			if block.FilePath == "" {
				r.println(MutedText("  no path inferred — skipping (use /apply and type a path)"))
				continue
			}
			path = block.FilePath
		case response == "" || response == "n" || response == "N":
			continue
		default:
			// Treat as a file path.
			path = response
		}

		if applyErr := ApplyCodeBlock(block, path); applyErr != nil {
			r.println(ErrorText(fmt.Sprintf("  error: %v", applyErr)))
		} else {
			r.println(PrimaryText(fmt.Sprintf("  written: %s", path)))
			applied++
		}
	}

	r.println(fmt.Sprintf("applied %d / %d blocks", applied, len(blocks)))
	return nil
}

func handleExit(ctx context.Context, r *REPL, args string) error {
	r.println("Goodbye!")
	return nil
}

// handleTakeoverRing manages the rotation ring configuration.
//
// /takeover-ring                       — show current ring state
// /takeover-ring claude,codex,minimax  — set ring from comma-separated runners
// /takeover-ring off|clear             — remove ring
func handleTakeoverRing(ctx context.Context, r *REPL, args string) error {
	args = strings.TrimSpace(args)

	switch args {
	case "":
		// Show current ring state.
		if r.ring == nil {
			r.println("No rotation ring configured")
			return nil
		}
		// Build display: "claude → codex → minimax (pos: codex)"
		posName := r.ring.Runners[r.ring.Pos%len(r.ring.Runners)]
		r.println(fmt.Sprintf("Rotation ring: %s (pos: %s)",
			strings.Join(r.ring.Runners, " → "), posName))
		return nil

	case "off", "clear":
		r.ring = nil
		r.println("Rotation ring cleared")
		return nil

	default:
		// Parse comma-separated runner names.
		parts := strings.Split(args, ",")
		runners := make([]string, 0, len(parts))
		for _, p := range parts {
			name := strings.TrimSpace(p)
			if name == "" {
				continue
			}
			runners = append(runners, name)
		}

		if len(runners) < 2 {
			r.println("Rotation ring must have at least 2 runners")
			return nil
		}

		// Validate each runner name.
		for _, name := range runners {
			if _, ok := r.runners[name]; !ok {
				r.println(fmt.Sprintf("Unknown runner: %s", name))
				return nil
			}
		}

		r.ring = &RingConfig{Runners: runners, Pos: 0}
		// Print the ring with a wrap-around indicator: "claude → codex → minimax → claude"
		display := strings.Join(runners, " → ") + " → " + runners[0]
		r.println(fmt.Sprintf("Rotation ring set: %s", display))
		return nil
	}
}

// handleTakeover switches the active runner and injects a handoff briefing as
// the first turn of the new runner's context.
//
// Usage:
//
//	/takeover <runner>  — switch to the named runner
//	/takeover           — advance the ring (if configured); error otherwise
func handleTakeover(ctx context.Context, r *REPL, args string) error {
	args = strings.TrimSpace(args)

	from := ""
	if r.runner != nil {
		from = r.runner.Name()
	}

	var to string

	switch {
	case args != "":
		// Explicit target runner supplied — validate it.
		if _, ok := r.runners[args]; !ok {
			return fmt.Errorf("Unknown runner: %s", args)
		}
		if args == from {
			return fmt.Errorf("Already on %s — use a different runner", from)
		}
		to = args

	case r.ring != nil:
		// No explicit target; ring is configured — advance to next runner.
		next, newPos, err := nextRingRunner(r.ring, r.runnerAvailable)
		if err != nil {
			return fmt.Errorf("ring exhausted: %w", err)
		}
		r.ring.Pos = newPos
		to = next

	default:
		return fmt.Errorf("No target runner — use /takeover <runner> or configure a ring with /takeover-ring")
	}

	// Generate briefing from transcript + turn buffer.
	currentCwd, _ := os.Getwd()
	briefing := GenerateBriefing(r.TranscriptPath(), r.turnBuffer, currentCwd)

	// Prepend briefing as a synthetic user turn so the new runner has context.
	synthetic := ConversationTurn{
		Role:   "user",
		Text:   fmt.Sprintf("[TAKEOVER from %s → %s]\n%s", from, to, briefing),
		Runner: from,
		At:     time.Now(),
	}
	r.turnBuffer = append([]ConversationTurn{synthetic}, r.turnBuffer...)
	if len(r.turnBuffer) > MaxHistoryTurns {
		r.turnBuffer = r.turnBuffer[:MaxHistoryTurns]
	}

	r.println(fmt.Sprintf("[takeover] forwarding session context to %s — includes prior conversation content", to))

	// Switch runner.
	if err := handleSwitch(ctx, r, to); err != nil {
		return err
	}

	r.println(fmt.Sprintf("[takeover] %s → %s — briefing injected", from, to))

	// Snapshot briefing to MemPalace asynchronously (best-effort).
	snapshotToMemPalaceAsync(briefing)

	return nil
}

// ----- Local runner commands -----

func (r *REPL) printLocalSettings(l *LocalRunner) {
	s := l.Settings()
	r.println(RunnerAccentText("local", "Local settings:"))
	r.println(fmt.Sprintf("  endpoint:    %s", s.Endpoint))
	r.println(fmt.Sprintf("  model:       %s", s.Model))
	if s.Temperature >= 0 {
		r.println(fmt.Sprintf("  temperature: %.2f", s.Temperature))
	} else {
		r.println("  temperature: server default")
	}
	if s.MaxTokens > 0 {
		r.println(fmt.Sprintf("  max_tokens:  %d", s.MaxTokens))
	} else {
		r.println("  max_tokens:  unlimited")
	}
}

func handleLocalModel(ctx context.Context, r *REPL, args string) error {
	l, ok := r.runner.(*LocalRunner)
	if !ok {
		return fmt.Errorf("not on local runner — use /local first")
	}
	if args == "" {
		r.println(fmt.Sprintf("local model: %s", l.Settings().Model))
		return nil
	}
	l.SetModel(args)
	r.println(fmt.Sprintf("local model set to %s", args))

	// Cross-check against the backend's model list so we don't silently send
	// requests with a model name the server will ignore (llama-server) or
	// reject (vLLM strict mode). If the lookup fails or the name isn't in
	// the list, warn but don't fail — llama-server is permissive.
	if models, err := l.ListModels(ctx); err == nil {
		found := false
		for _, m := range models {
			if m == args {
				found = true
				break
			}
		}
		if !found {
			r.println(fmt.Sprintf("[warn] backend reports models: %v — '%s' may be ignored. With llama-server, restart the server with a different -m to actually load a different model.", models, args))
		}
	}
	return nil
}

func handleLocalEndpoint(ctx context.Context, r *REPL, args string) error {
	l, ok := r.runner.(*LocalRunner)
	if !ok {
		return fmt.Errorf("not on local runner — use /local first")
	}
	if args == "" {
		r.println(fmt.Sprintf("local endpoint: %s", l.Settings().Endpoint))
		return nil
	}
	l.SetEndpoint(args)
	r.println(fmt.Sprintf("local endpoint set to %s", args))
	return nil
}

func handleLocalModels(ctx context.Context, r *REPL, _ string) error {
	l, ok := r.runner.(*LocalRunner)
	if !ok {
		return fmt.Errorf("not on local runner — use /local first")
	}
	models, err := l.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("local: list models: %w", err)
	}
	if len(models) == 0 {
		r.println("No local models found. Run scripts/install_local.sh to set one up.")
		return nil
	}
	current := l.Settings().Model
	r.println("Local models:")
	for _, m := range models {
		marker := "  "
		if m == current {
			marker = "* "
		}
		r.println(fmt.Sprintf("%s%s", marker, m))
	}
	return nil
}

func handleLocalTemp(_ context.Context, r *REPL, args string) error {
	l, ok := r.runner.(*LocalRunner)
	if !ok {
		return fmt.Errorf("not on local runner — use /local first")
	}
	if args == "" {
		s := l.Settings()
		if s.Temperature < 0 {
			r.println("local temperature: (server default)")
		} else {
			r.println(fmt.Sprintf("local temperature: %.2f", s.Temperature))
		}
		return nil
	}
	if strings.EqualFold(args, "default") || strings.EqualFold(args, "off") {
		l.SetTemperature(-1)
		r.println("local temperature reset to server default")
		return nil
	}
	t, err := strconv.ParseFloat(args, 64)
	if err != nil {
		return fmt.Errorf("invalid temperature %q (try 0.0–2.0, or 'default')", args)
	}
	if t < 0 || t > 2 {
		return fmt.Errorf("temperature out of range: %v (use 0.0–2.0)", t)
	}
	l.SetTemperature(t)
	r.println(fmt.Sprintf("local temperature set to %.2f", t))
	return nil
}

func handleLocalMaxTokens(_ context.Context, r *REPL, args string) error {
	l, ok := r.runner.(*LocalRunner)
	if !ok {
		return fmt.Errorf("not on local runner — use /local first")
	}
	if args == "" {
		s := l.Settings()
		if s.MaxTokens == 0 {
			r.println("local max_tokens: (unlimited)")
		} else {
			r.println(fmt.Sprintf("local max_tokens: %d", s.MaxTokens))
		}
		return nil
	}
	if strings.EqualFold(args, "off") || strings.EqualFold(args, "unlimited") || args == "0" {
		l.SetMaxTokens(0)
		r.println("local max_tokens reset to unlimited")
		return nil
	}
	n, err := strconv.Atoi(args)
	if err != nil || n < 1 {
		return fmt.Errorf("invalid max_tokens %q (positive integer, or 'off')", args)
	}
	l.SetMaxTokens(n)
	r.println(fmt.Sprintf("local max_tokens set to %d", n))
	return nil
}

// handleModelsContextual dispatches /models to the active runner's list handler.
// Local runner has a /v1/models endpoint we can hit; everything else falls
// back to the catalog/current-setting print path inside handleModel.
func handleModelsContextual(ctx context.Context, r *REPL, args string) error {
	if r.runner == nil {
		return fmt.Errorf("no active runner — switch to one first")
	}
	if _, ok := r.runner.(*LocalRunner); ok {
		return handleLocalModels(ctx, r, args)
	}
	// Fall back to /model with no args — runners with a catalog will print it.
	return handleModel(ctx, r, "")
}

// handleLocalHot toggles llama-swap's effective hot/standby behaviour at
// runtime by either warming every advertised model (hot) or letting them
// fall idle so llama-swap unloads them on its TTL (standby).
//   /local-hot         — show current state guess
//   /local-hot on      — issue a max_tokens=1 prompt to every advertised model
//   /local-hot off     — issue a /v1/internal/unload to every advertised model
//                        (best-effort; on llama-swap versions without the
//                        admin endpoint we just stop touching them and let
//                        the configured TTL handle eviction).
func handleLocalHot(ctx context.Context, r *REPL, args string) error {
	l, ok := r.runner.(*LocalRunner)
	if !ok {
		return fmt.Errorf("not on local runner — use /local first")
	}
	args = strings.TrimSpace(strings.ToLower(args))

	if args == "" {
		r.println("Usage: /local-hot on|off")
		r.println("  on  — warm every advertised model so switches are sub-second")
		r.println("  off — stop pinging models; llama-swap unloads them on its TTL")
		return nil
	}

	models, err := l.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("local: list models: %w", err)
	}
	if len(models) == 0 {
		return fmt.Errorf("backend has no models — run scripts/install_local_swap.sh")
	}

	endpoint := l.Settings().Endpoint

	switch args {
	case "on", "true", "1":
		r.println(fmt.Sprintf("warming %d model(s)…", len(models)))
		for _, m := range models {
			r.println(fmt.Sprintf("  → %s", m))
			if err := warmLocalModel(ctx, endpoint, m); err != nil {
				r.println(fmt.Sprintf("    [warn] %v", err))
			}
		}
		r.println("hot mode active — models resident, switches should be sub-second")
		return nil

	case "off", "false", "0":
		r.println("standby mode requested — llama-swap will unload after its configured TTL.")
		r.println("(For instant eviction, restart llama-swap: launchctl unload …local-swap.plist)")
		return nil

	default:
		return fmt.Errorf("unknown arg %q — use on|off", args)
	}
}

func warmLocalModel(ctx context.Context, endpoint, model string) error {
	body := strings.NewReader(fmt.Sprintf(`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"ok"}]}`, model))
	url := strings.TrimRight(endpoint, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
