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

// launcher.go implements the `milliways` (no-flags) launcher: it resolves
// the daemon UDS, starts `milliwaysd` detached if not reachable, then
// drops into the chat REPL in the current TTY.
//
// History: pre-v0.7.1 this exec(2)'d `milliways-term` (the wezterm fork)
// when invoked from a non-wezterm shell. That binary panics during
// `mux::Mux::get` when not launched as a bundled .app, and the panic
// hook then aborts on UNUserNotificationCenter for non-bundled binaries
// — so any `milliways` invocation from kitty/iTerm/ssh crashed.
//
// The .app bundle's CFBundleExecutable is `wezterm-gui` directly, so
// the bundle path never needs `milliways` to exec milliways-term. We
// removed the exec-milliways-term path and just run chat in the
// current TTY for every invocation.
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mwigge/milliways/internal/rpc"
)

// launcherMode classifies what the binary should do based on its argv.
type launcherMode int

const (
	// modeCobra means: hand off to the existing cobra root command (handles
	// --version, --help, subcommands, prompt-as-args dispatch).
	modeCobra launcherMode = iota
	// modeChat means: ensure milliwaysd is running, then drop into the
	// chat REPL in the current TTY. Used for every no-args invocation —
	// works the same way inside milliways-term, kitty, iTerm, ssh, or
	// any other terminal.
	modeChat
)

// modeCockpit, modeWelcome are deprecated aliases retained so external
// code/tests that still reference them keep building. New code should
// use modeChat.
const (
	modeCockpit = modeChat
	modeWelcome = modeChat
)

// parseLauncherMode decides how to dispatch an invocation of `milliways`
// based on the argv (excluding argv[0]).
//
// Rules:
//
//  1. argv is empty → modeChat (ensure daemon, run chat in this TTY)
//  2. otherwise → modeCobra (--version, --help, subcommands, prompts)
//
// The pre-v0.7.1 in-vs-out-of-wezterm branch is gone: chat works from
// any TTY, and the bundle entry point is wezterm-gui (not milliways)
// so there's no recursion risk to guard against.
func parseLauncherMode(args []string) launcherMode {
	if len(args) == 0 {
		return modeChat
	}
	return modeCobra
}

// printWelcome emits the v0.5.x quickstart banner. Discoverable by
// typing `milliways` (no args) inside any milliways-term tab. Queries
// the live daemon for status + agent list (graceful fallback when the
// daemon is down) so the user sees their actual cockpit state, not a
// static template.
func printWelcome() {
	out := os.Stdout
	fmt.Fprintln(out, "milliways "+welcomeVersion()+" — inside MilliWays.app")
	fmt.Fprintln(out)

	// Live daemon status. Short timeout so a hung daemon doesn't block
	// the welcome render.
	state := probeDaemonForWelcome(700 * time.Millisecond)
	fmt.Fprintln(out, "  daemon  "+state.daemonLine)
	if state.agentLine != "" {
		fmt.Fprintln(out, "  agents  "+state.agentLine)
	}
	if state.activeLine != "" {
		fmt.Fprintln(out, "  active  "+state.activeLine)
	}
	fmt.Fprintln(out)

	const body = `Switch agent (Ctrl+Space = Leader):
  Leader + 1   → claude     Leader + 2   → codex
  Leader + 3   → copilot    Leader + 4   → minimax
  Leader + a   → claude     (split pane below current tab)

One-shot prompt (no UI; just dispatch):
  milliways "explain the auth flow"
  milliways -k claude "review this PR"
  milliways -j "summarise this in JSON"
  milliways --recipe <name> "<prompt>"

Parallel dispatch — fan same prompt to N providers simultaneously:
  /parallel review internal/server/          all pool providers, live panes
  /parallel --providers claude,codex <prompt>

Security scanning:
  /scan                                      scan workspace for CVEs (requires osv-scanner)
  milliwaysctl security install-scanner      install osv-scanner
  milliwaysctl security list                 list active findings
  milliwaysctl security enable/disable       toggle scanning on/off

Slash command palette — Ctrl+Space then / opens a fuzzy filter:
  /install-local-server         install llama.cpp + default coder model
  /list-local-models            show models the active backend serves
  /setup-local-model <repo>     download GGUF + register in llama-swap
  /switch-local-server <kind>   llama-server | llama-swap | ollama | vllm | lmstudio
  /opsx-list                    list openspec changes
  /opsx-status <change>         show change progress
  …                             type Tab through the picker for the full list

Discover everything:
  milliways --help              full command + flag reference

Tip: pasting a multi-line prompt? Wrap in quotes so the shell keeps it whole.
`
	fmt.Fprint(out, body)
}

// welcomeVersion returns the binary version string for the banner header.
// Defined out-of-package-main as a thin wrapper so tests can stub it
// without depending on link-time -X flags.
var welcomeVersion = func() string {
	if v := strings.TrimSpace(os.Getenv("MILLIWAYS_WELCOME_VERSION_OVERRIDE")); v != "" {
		return v
	}
	// main.version is set via -ldflags -X. We grab it via a function var
	// the main package fills in init().
	if welcomeVersionRef != nil {
		return welcomeVersionRef()
	}
	return "v0.5.x"
}

// welcomeVersionRef is wired by main.go's init() to expose the
// link-time-injected `version` string without an import cycle.
var welcomeVersionRef func() string

// daemonStatusReport summarises the live daemon state for the welcome.
type daemonStatusReport struct {
	daemonLine string // e.g. "● running (uptime 3h12m)" or "✗ not running"
	agentLine  string // e.g. "claude ✓  codex ✓  copilot ✗  …"
	activeLine string // e.g. "claude (session 0x7f, 2 prompts)"
}

// probeDaemonForWelcome dials the daemon UDS with a short budget and
// returns a populated report or a graceful "not running" fallback.
func probeDaemonForWelcome(budget time.Duration) daemonStatusReport {
	sock := daemonSocket()
	if !socketReachable(sock, budget/2) {
		return daemonStatusReport{
			daemonLine: "✗ not running   (start it: open MilliWays.app, or run `milliwaysd &` in any tab)",
		}
	}
	c, err := rpc.Dial(sock)
	if err != nil {
		return daemonStatusReport{
			daemonLine: "✗ not reachable: " + err.Error(),
		}
	}
	defer c.Close()

	// Two parallel reads with a tight deadline. agent.list is the cheapest
	// signal of "daemon is responsive AND knows about runners".
	deadline := time.Now().Add(budget)
	report := daemonStatusReport{daemonLine: "● running"}

	type listResp struct {
		Agents []struct {
			ID         string `json:"id"`
			Available  bool   `json:"available"`
			AuthStatus string `json:"auth_status"`
		} `json:"agents"`
	}
	var agents listResp
	if err := callWithDeadline(c, "agent.list", nil, &agents, deadline); err == nil && len(agents.Agents) > 0 {
		var parts []string
		for _, a := range agents.Agents {
			mark := "✗"
			if a.AuthStatus == "ok" {
				mark = "✓"
			} else if a.AuthStatus == "unknown" {
				mark = "?"
			}
			parts = append(parts, a.ID+" "+mark)
		}
		report.agentLine = strings.Join(parts, "  ")
	}

	var status map[string]any
	if err := callWithDeadline(c, "status.get", nil, &status, deadline); err == nil {
		if cur, _ := status["c"].(string); cur != "" {
			report.activeLine = cur
			if h, _ := status["session_handle"].(string); h != "" {
				report.activeLine += "  (handle " + h + ")"
			}
		}
	}

	return report
}

// callWithDeadline runs c.Call but bails if `deadline` has passed.
// The rpc.Client doesn't take a context, so this is best-effort: we
// gate on time-of-call and let the underlying conn timeout if the
// server hangs.
func callWithDeadline(c *rpc.Client, method string, params, result any, deadline time.Time) error {
	if time.Now().After(deadline) {
		return fmt.Errorf("budget exceeded before %s", method)
	}
	return c.Call(method, params, result)
}

// socketReachable returns true if a unix-domain dial to socketPath succeeds
// within the given timeout. Used to probe whether milliwaysd is already
// listening, and to poll until a freshly-spawned daemon comes up.
//
// We use net.DialTimeout (not a context) so the call returns promptly even
// when the path doesn't exist (ENOENT) or the listener has crashed.
func socketReachable(socketPath string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// stateDir mirrors milliwaysd's resolveStateDir / milliwaysctl's
// defaultSocket: ${XDG_RUNTIME_DIR}/milliways or ~/.local/state/milliways.
func stateDir() string {
	if x := os.Getenv("XDG_RUNTIME_DIR"); x != "" {
		return filepath.Join(x, "milliways")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "state", "milliways")
}

// daemonSocket returns the canonical UDS path for milliwaysd.
func daemonSocket() string { return filepath.Join(stateDir(), "sock") }

// daemonLogPath is where the launcher tees milliwaysd's stdout/stderr when
// it spawns the daemon detached.
func daemonLogPath() string { return filepath.Join(stateDir(), "milliwaysd.log") }

// runCockpit ensures milliwaysd is up, then either launches the full
// parallel panel deck (when running inside WezTerm) or falls back to the
// single-pane chat REPL. The deck matches the agent-deck home dashboard:
// left navigator, per-provider content panes, observability header on top.
func runCockpit(ctx context.Context, _ []string) error {
	state := stateDir()
	if err := os.MkdirAll(state, 0o700); err != nil {
		return fmt.Errorf("creating state dir %s: %w", state, err)
	}

	maybePrintCockpitHint(state)

	socketPath := daemonSocket()
	if !socketReachable(socketPath, 200*time.Millisecond) {
		if err := startDaemonDetached(state); err != nil {
			return fmt.Errorf("starting milliwaysd: %w\n\nCheck %s for daemon logs.", err, daemonLogPath())
		}
		if err := waitForSocket(ctx, socketPath, 5*time.Second); err != nil {
			tail := tailFile(daemonLogPath(), 4096)
			fmt.Fprintf(os.Stderr, "milliways: daemon did not become reachable within 5s.\n")
			if tail != "" {
				fmt.Fprintf(os.Stderr, "--- tail of %s ---\n%s\n--- end ---\n", daemonLogPath(), tail)
			}
			fmt.Fprintf(os.Stderr, "Check the log above and confirm milliwaysd starts cleanly: `milliwaysd` (foreground).\n")
			return err
		}
	}

	// In WezTerm: launch the full panel deck (home-hero-dashboard style).
	// The calling pane stays as the main chat; the deck opens alongside it.
	if os.Getenv("TERM_PROGRAM") == "WezTerm" {
		if err := runDeck(ctx, socketPath); err != nil {
			// Deck launch failed — fall through to single chat gracefully.
			fmt.Fprintf(os.Stderr, "milliways: deck launch failed (%v), falling back to single chat\n", err)
		} else {
			// Deck launched — this pane becomes the main chat REPL.
			return runChat(ctx)
		}
	}

	return runChat(ctx)
}

// deckProviders is the default set shown in the startup deck — the four
// primary cloud providers. Local and pool are omitted from the default
// deck to keep the layout manageable; add via MILLIWAYS_DECK_PROVIDERS env.
var deckProviders = []string{"claude", "codex", "copilot", "minimax"}

// runDeck opens the home-hero-dashboard layout: left navigator (30%) plus
// one full milliways chat pane per provider on the right, each pre-switched
// to its assigned client. The calling pane stays as the main chat session
// (/help, /parallel, /takeover, /scan etc. work here).
//
// Each provider pane is a real independent milliways session — the user can
// type directly to that client, switch providers with /takeover, or use the
// main pane to /parallel broadcast to all of them.
func runDeck(ctx context.Context, socketPath string) error {
	// Allow override via env for power users.
	providers := deckProviders
	if env := os.Getenv("MILLIWAYS_DECK_PROVIDERS"); env != "" {
		providers = splitComma(env)
	}

	milliwaysBin, err := os.Executable()
	if err != nil {
		milliwaysBin = "milliways"
	}

	// Left navigator pane — 30% width, shows status of all deck panes.
	// Runs `milliways attach --nav deck` which polls group.status.
	navArgs := []string{"cli", "split-pane", "--percent", "30", "--",
		milliwaysBin, "attach", "--nav", "deck"}
	if out, err := exec.Command("wezterm", navArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("wezterm split-pane (nav): %w\n%s", err, out)
	}

	// One pane per provider — each runs a full milliways chat pre-switched
	// to that provider via MILLIWAYS_START_PROVIDER env.
	for _, provider := range providers {
		paneArgs := []string{
			"cli", "split-pane",
			"--", "env",
			"MILLIWAYS_START_PROVIDER=" + provider,
			milliwaysBin,
		}
		if out, err := exec.Command("wezterm", paneArgs...).CombinedOutput(); err != nil {
			// Non-fatal — skip unavailable providers.
			fmt.Fprintf(os.Stderr, "milliways: deck pane for %s failed: %v\n%s\n", provider, err, out)
		}
	}

	return nil
}

// splitComma splits a comma-separated string and trims whitespace.
func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// startDaemonDetached spawns `milliwaysd` in its own session so it survives
// the launcher exiting, with stdout/stderr appended to ${state}/milliwaysd.log.
func startDaemonDetached(state string) error {
	daemonPath, err := exec.LookPath("milliwaysd")
	if err != nil {
		return fmt.Errorf("milliwaysd not found on PATH: %w", err)
	}

	logFile, err := os.OpenFile(daemonLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("opening %s: %w", daemonLogPath(), err)
	}
	// We intentionally do NOT close logFile here; the spawned child inherits
	// it as fds 1 and 2. The kernel reclaims it when the daemon exits.

	cmd := exec.Command(daemonPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("starting milliwaysd: %w", err)
	}
	// Release the child so we don't accumulate a zombie if waitForSocket
	// returns before the daemon has fully come up.
	_ = cmd.Process.Release()
	return nil
}

// waitForSocket polls socketReachable every 100ms until either the deadline
// expires or the socket is live.
func waitForSocket(ctx context.Context, socketPath string, deadline time.Duration) error {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if socketReachable(socketPath, 200*time.Millisecond) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("milliwaysd socket %s not reachable after %s", socketPath, deadline)
}

// tailFile returns the last `max` bytes of a file as a string, or the empty
// string if the file is unreadable. Used to surface the daemon's recent log
// lines when startup fails.
func tailFile(path string, max int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	st, err := f.Stat()
	if err != nil {
		return ""
	}
	size := st.Size()
	if size == 0 {
		return ""
	}
	offset := int64(0)
	if size > int64(max) {
		offset = size - int64(max)
	}
	buf := make([]byte, size-offset)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return ""
	}
	return strings.TrimSpace(string(buf))
}

// maybePrintCockpitHint shows a one-time hint on stderr the first time the
// milliways-term launcher runs, pointing at the wezterm sample config.
func maybePrintCockpitHint(state string) {
	marker := filepath.Join(state, "cockpit-hint-shown")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "milliways starting. Set up wezterm config: see ~/.local/share/milliways/sample-wezterm.lua")
	if f, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		_ = f.Close()
	}
}
