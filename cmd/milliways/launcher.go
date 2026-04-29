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
// exec(2)s `milliways-term` with the user's remaining args.
//
// The legacy `--repl` / `MILLIWAYS_REPL=1` built-in terminal mode was
// removed in this change; the milliways-term path is now the only
// interactive surface. Scripts/users that need a one-shot prompt should
// invoke `milliways "<prompt>"` (handled by the cobra root command) or
// use `milliwaysctl` from any terminal tab.
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
)

// launcherMode classifies what the binary should do based on its argv.
type launcherMode int

const (
	// modeCobra means: hand off to the existing cobra root command (handles
	// --version, --help, subcommands, prompt-as-args dispatch).
	modeCobra launcherMode = iota
	// modeCockpit means: start the daemon (if needed), exec milliways-term.
	modeCockpit
)

// parseLauncherMode decides how to dispatch an invocation of `milliways`
// based on the argv (excluding argv[0]).
//
// Rules (first match wins):
//
//  1. argv is empty → modeCockpit (launch milliways-term)
//  2. otherwise → modeCobra (--version, --help, subcommands, prompts)
func parseLauncherMode(args []string) launcherMode {
	if len(args) == 0 {
		return modeCockpit
	}
	return modeCobra
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

// runCockpit is the default mode: ensure the daemon is up, then exec
// milliways-term. On any non-recoverable failure we exit non-zero with a
// message that points the user at troubleshooting milliwaysd (logs, lock
// files) rather than the removed --repl fallback.
func runCockpit(ctx context.Context, args []string) error {
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

	termPath, err := exec.LookPath("milliways-term")
	if err != nil {
		fmt.Fprintf(os.Stderr, "milliways: could not find `milliways-term` on PATH.\n")
		fmt.Fprintf(os.Stderr, "  Install it (build the milliways-term fork) — see README for instructions.\n")
		return fmt.Errorf("milliways-term not found on PATH")
	}

	// syscall.Exec replaces this process so the launcher disappears from the
	// process tree the moment the terminal opens.
	argv := append([]string{"milliways-term"}, args...)
	if err := syscall.Exec(termPath, argv, os.Environ()); err != nil {
		return fmt.Errorf("exec milliways-term: %w", err)
	}
	return nil // unreachable on success
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
