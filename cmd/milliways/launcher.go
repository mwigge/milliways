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
// the daemon UDS, starts `milliwaysd` detached if not reachable, then opens
// the full terminal cockpit when possible. Existing WezTerm sessions get
// split in place; graphical non-WezTerm shells exec the bundled
// milliways-term; headless shells fall back to chat in the current TTY.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	printWelcomeTo(os.Stdout)
}

func printWelcomeTo(out io.Writer) {
	fmt.Fprintln(out, "milliways "+welcomeVersion()+" — launcher")

	// Live daemon status. Short timeout so a hung daemon doesn't block
	// the welcome render.
	state := probeDaemonForWelcome(700 * time.Millisecond)
	fmt.Fprintln(out, "  daemon  "+state.daemonLine)
	if state.activeLine != "" {
		fmt.Fprintln(out, "  active  "+state.activeLine)
	}
	fmt.Fprintln(out)

	const body = `Start:
  milliways chat                 interactive chat
  milliways "explain this repo"  one-shot prompt
  milliwaysctl status            daemon status

Inside chat:
  /help                          full command reference
  /agents                        auth and model status
  /parallel --watch <prompt>     live grouped provider comparison
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

	// In WezTerm (any fork): launch the full panel deck.
	// WEZTERM_PANE is set by the mux on every pane — more reliable than
	// TERM_PROGRAM which varies across WezTerm forks. wezterm CLI must also
	// be findable on PATH. Skip deck if MILLIWAYS_NO_DECK=1 is set.
	deckDisabled := os.Getenv("MILLIWAYS_NO_DECK") == "1"

	// WezTerm (macOS + Linux): WEZTERM_PANE set by mux, or detect via TTY match.
	_, weztermCLIErr := exec.LookPath("wezterm")
	if weztermCLIErr == nil && !deckDisabled {
		rightPaneID := os.Getenv("WEZTERM_PANE")
		if rightPaneID == "" {
			rightPaneID, _ = detectWeztermCurrentPaneID()
		}
		if rightPaneID != "" {
			if err := runDeck(ctx, socketPath, rightPaneID); err != nil {
				fmt.Fprintf(os.Stderr, "milliways: deck launch failed (%v), falling back to single chat\n", err)
			} else {
				return runChat(ctx)
			}
		}
	}

	if !deckDisabled && hasGraphicalSession() {
		if termPath, err := exec.LookPath("milliways-term"); err == nil {
			return syscall.Exec(termPath, []string{termPath}, os.Environ())
		}
	}

	return runChat(ctx)
}

func hasGraphicalSession() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// detectWeztermCurrentPaneID finds the pane ID of the terminal running this
// process by matching the current TTY against wezterm cli list output.
func detectWeztermCurrentPaneID() (string, string) {
	ttyGetter := func() (string, error) {
		// Must pass os.Stdin explicitly — exec.Command defaults stdin to /dev/null
		// which causes `tty` to report "not a tty".
		cmd := exec.Command("tty")
		cmd.Stdin = os.Stdin
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	listPanes := func() ([]byte, error) {
		return exec.Command("wezterm", "cli", "list", "--format", "json").Output()
	}
	return detectWeztermCurrentPaneIDWith(ttyGetter, listPanes)
}

// detectWeztermCurrentPaneIDWith is the testable core of detectWeztermCurrentPaneID.
// It matches the current TTY path against wezterm pane list JSON, falling back to
// the first is_active pane if no exact TTY match is found.
func detectWeztermCurrentPaneIDWith(
	ttyGetter func() (string, error),
	listPanes func() ([]byte, error),
) (string, string) {
	myTTY, err := ttyGetter()
	if err != nil {
		return "", "tty failed: " + err.Error()
	}

	listOut, err := listPanes()
	if err != nil {
		return "", "wezterm list failed: " + err.Error()
	}
	var panes []struct {
		PaneID   int    `json:"pane_id"`
		IsActive bool   `json:"is_active"`
		TtyName  string `json:"tty_name"`
	}
	if err := json.Unmarshal(listOut, &panes); err != nil {
		return "", "json parse failed: " + err.Error()
	}

	// Prefer exact TTY match (unambiguous).
	for _, p := range panes {
		if p.TtyName == myTTY {
			return strconv.Itoa(p.PaneID), ""
		}
	}
	// Fall back to first active pane (covers WezTerm forks that omit tty_name).
	var ttyNames []string
	for _, p := range panes {
		ttyNames = append(ttyNames, p.TtyName)
		if p.IsActive {
			return strconv.Itoa(p.PaneID), ""
		}
	}
	return "", fmt.Sprintf("myTTY=%q not in panes %v", myTTY, ttyNames)
}

const deckNavigatorPanePercent = 25
const deckObservePanePercent = 25

var runDeckCommand = exec.Command

// runDeck opens the home-hero-dashboard layout: left navigator plus
// the calling pane as the main chat session on the right.
//
// The navigator is an interactive provider browser — arrow keys to browse,
// Enter to switch the right pane to that provider via /switch. No separate
// provider tabs are spawned; the single right pane handles all providers.
func runDeck(_ context.Context, _ string, rightPaneID string) error {

	milliwaysBin, err := os.Executable()
	if err != nil {
		milliwaysBin = "milliways"
	}
	milliwaysCtlBin := resolveMilliwaysCtlBin(milliwaysBin)

	// Split LEFT: narrow navigator pane. The current pane stays as the chat.
	navArgs := deckNavSplitArgs(rightPaneID, milliwaysBin)
	out, err := runDeckCommand("wezterm", navArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("wezterm split-pane (nav): %w\n%s", err, out)
	}
	if navPaneID := parseWeztermSplitPaneID(string(out)); navPaneID != "" {
		observeArgs := deckObserveSplitArgs(navPaneID, milliwaysCtlBin)
		if out, err := runDeckCommand("wezterm", observeArgs...).CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "milliways: observe cockpit launch failed (%v)\n%s", err, out)
		}
	} else {
		fmt.Fprintf(os.Stderr, "milliways: observe cockpit launch skipped; could not parse navigator pane id from %q\n", strings.TrimSpace(string(out)))
	}

	// Signal to printLanding that the navigator is handling provider selection.
	os.Setenv("MILLIWAYS_DECK_MODE", "1")
	return nil
}

func deckNavSplitArgs(rightPaneID, milliwaysBin string) []string {
	return []string{
		"cli", "split-pane", "--pane-id", rightPaneID,
		"--left", "--percent", strconv.Itoa(deckNavigatorPanePercent),
		"--",
		milliwaysBin, "attach", "--deck", "--right-pane", rightPaneID,
	}
}

func deckObserveSplitArgs(navPaneID, milliwaysCtlBin string) []string {
	return []string{
		"cli", "split-pane", "--pane-id", navPaneID,
		"--bottom", "--percent", strconv.Itoa(deckObservePanePercent),
		"--",
		milliwaysCtlBin, "observe-render",
	}
}

func resolveMilliwaysCtlBin(milliwaysBin string) string {
	if path, err := exec.LookPath("milliwaysctl"); err == nil {
		return path
	}
	if milliwaysBin != "" {
		candidate := filepath.Join(filepath.Dir(milliwaysBin), "milliwaysctl")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return candidate
		}
	}
	return "milliwaysctl"
}

func parseWeztermSplitPaneID(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		fields := strings.Fields(lines[i])
		for j := len(fields) - 1; j >= 0; j-- {
			if _, err := strconv.Atoi(fields[j]); err == nil {
				return fields[j]
			}
		}
	}
	return ""
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

const cockpitHintFileName = "cockpit-hint.txt"

func cockpitHintPath(state string) string {
	return filepath.Join(state, cockpitHintFileName)
}

func cockpitHintText(state string) string {
	return fmt.Sprintf(`Milliways terminal setup

The full terminal deck uses the bundled WezTerm config:
  ~/.local/share/milliways/wezterm.lua

If your WezTerm config is not linked, run:
  mkdir -p ~/.config/wezterm
  ln -sf ~/.local/share/milliways/wezterm.lua ~/.config/wezterm/wezterm.lua

Upgrade refreshes the bundled config:
  milliways chat
  /upgrade

This reminder is saved at:
  %s
`, cockpitHintPath(state))
}

func ensureCockpitHintFile(state string) error {
	path := cockpitHintPath(state)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(cockpitHintText(state)), 0o600)
}

// maybePrintCockpitHint writes a durable setup note and prints a short first-run
// reminder on interactive stderr. The reminder remains discoverable via /help
// and the state file even after the first-run marker suppresses stderr output.
func maybePrintCockpitHint(state string) {
	_ = ensureCockpitHintFile(state)
	if !isTTYStderr() || os.Getenv("MILLIWAYS_QUIET_HINTS") == "1" {
		return
	}
	marker := filepath.Join(state, "cockpit-hint-shown")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "milliways starting. Terminal setup note: %s\n", cockpitHintPath(state))
	if f, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		_ = f.Close()
	}
}
