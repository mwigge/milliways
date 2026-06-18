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

//nolint:errcheck // CLI help/status output writes are best-effort; subprocess errors are returned explicitly.
package main

// `milliwaysctl opsx <verb>` — thin in-app wrapper around the openspec CLI
// so REPL users (decommission target) and milliways-term users have a
// uniform `/opsx-list`, `/opsx-status` etc. UX via the wezterm Leader+/
// palette without leaving the terminal.
//
// Verbs (pure shell-out):
//   list                   list openspec changes
//   status   [--change N]  show change progress
//   show     <change>      show full change detail
//   archive  <change>      archive a completed change
//   validate <change>      validate a change
//
// Explore / apply compose openspec output with a chat runner via the
// daemon's agent.send/agent.stream APIs. They spawn the openspec subprocess
// as the foreground process group leader so the user's terminal controls
// it directly, while piping its output to the agent in real-time.
//
//   explore <change>       run openspec explore with an agent sidecar
//   apply    <change>       run openspec apply with an agent sidecar

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/mwigge/milliways/internal/rpc"
)

// runOpsx dispatches `milliwaysctl opsx <verb> [args...]` and returns the
// process exit code.
func runOpsx(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printOpsxUsage(stderr)
		return 2
	}

	// Parse --path / -p before the verb so we can pass it through.
	opsxPath := ""
	verbArgs := args
	for i := 0; i < len(args); i++ {
		if args[i] == "--path" || args[i] == "-p" {
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "milliwaysctl opsx: --path requires an argument")
				return 2
			}
			opsxPath = args[i+1]
			verbArgs = append(args[:i], args[i+2:]...)
			i--
		} else if strings.HasPrefix(args[i], "--path=") {
			opsxPath = strings.TrimPrefix(args[i], "--path=")
			verbArgs = append(args[:i], args[i+1:]...)
			i--
		} else if args[i] == "-p" && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			opsxPath = args[i+1]
			verbArgs = append(args[:i], args[i+2:]...)
			i--
		}
	}

	// Change to the project directory if specified.
	if opsxPath != "" {
		if err := os.Chdir(opsxPath); err != nil {
			fmt.Fprintf(stderr, "milliwaysctl opsx: --path %s: %v\n", opsxPath, err)
			return 1
		}
	}

	if len(verbArgs) == 0 {
		printOpsxUsage(stderr)
		return 2
	}
	verb := verbArgs[0]
	rest := verbArgs[1:]
	switch verb {
	case "-h", "--help", "help":
		printOpsxUsage(stdout)
		return 0
	case "list", "status", "show", "archive", "validate":
		return runOpsxOnce(buildOpsxArgs(verb, rest), stdout, stderr)
	case "explore", "apply":
		return runOpsxAgent(verb, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "milliwaysctl opsx: unknown verb %q\n", verb)
		printOpsxUsage(stderr)
		return 2
	}
}

func printOpsxUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: milliwaysctl opsx <verb> [args...] [--path DIR]")
	fmt.Fprintln(w, "verbs:")
	fmt.Fprintln(w, "  list                       list openspec changes")
	fmt.Fprintln(w, "  status [<change>]          show change progress (current change if omitted)")
	fmt.Fprintln(w, "  show <change>              show full change detail")
	fmt.Fprintln(w, "  archive <change>           archive a completed change")
	fmt.Fprintln(w, "  validate <change>          validate a change")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Interactive verbs (run with agent sidecar):")
	fmt.Fprintln(w, "  explore <change>           run openspec explore with agent context injection")
	fmt.Fprintln(w, "  apply <change>            run openspec apply with agent context injection")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --path DIR, -p DIR         project root (default: current working directory)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Override openspec binary with OPENSPEC_BIN env var; default: openspec on PATH.")
	fmt.Fprintln(w, "Override agent with OPSX_AGENT env var; default: claude (for explore/apply).")
	fmt.Fprintln(w, "Override socket with --socket flag or MILLIWAYS_SOCK env var.")
}

// buildOpsxArgs maps a milliwaysctl verb + rest-args to the openspec CLI
// argv. Most verbs pass through 1:1; a few translate (status →
// `status --change`, validate → `change validate`).
func buildOpsxArgs(verb string, rest []string) []string {
	switch verb {
	case "status":
		if len(rest) == 0 {
			return []string{"status"}
		}
		return append([]string{"status", "--change"}, rest...)
	case "validate":
		return append([]string{"change", "validate"}, rest...)
	default:
		return append([]string{verb}, rest...)
	}
}

// runOpsxOnce shells out to openspec, streaming stdout/stderr through to
// the caller's writers, and returns the subprocess exit code (or 1 on
// other errors).
func runOpsxOnce(args []string, stdout, stderr io.Writer) int {
	bin := lookupOpenspec()
	if bin == "" {
		fmt.Fprintln(stderr, "milliwaysctl opsx: openspec binary not found (set OPENSPEC_BIN or install from https://openspec.dev)")
		return 1
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(stderr, "milliwaysctl opsx: %v\n", err)
		return 1
	}
	return 0
}

func lookupOpenspec() string {
	if env := os.Getenv("OPENSPEC_BIN"); env != "" {
		// Strict honor: if the user pointed us at a specific binary, use
		// it as-is. Existence is verified at runtime via os.Stat below so
		// the "openspec binary not found" message stays accurate.
		if info, err := os.Stat(env); err == nil && !info.IsDir() {
			return env
		}
		// Override is set but invalid — surface that explicitly rather
		// than silently falling back to PATH.
		return ""
	}
	if path, err := exec.LookPath("openspec"); err == nil {
		return path
	}
	return ""
}

// runOpsxAgent orchestrates the openspec explore/apply interactive verb.
// It:
//   1. Opens an agent session via agent.open on the daemon socket.
//   2. Pipes openspec stdout to the terminal and agent.send in parallel.
//   3. Restores terminal control on exit so the user can continue interacting.
//
// The openspec subprocess runs in the foreground process group (the usual
// setup in a terminal) so Ctrl-C, Ctrl-Z, and window resizes flow naturally.
// The agent sidecar runs in the background; its sidecar stream events are
// printed as terminal output so the user sees the agent's responses alongside
// openspec's own UI.
func runOpsxAgent(verb string, rest []string, stdout, stderr io.Writer) int {
	if len(rest) == 0 {
		fmt.Fprintf(stderr, "milliwaysctl opsx %s: change name required\n", verb)
		return 2
	}
	changeName := rest[0]

	bin := lookupOpenspec()
	if bin == "" {
		fmt.Fprintln(stderr, "milliwaysctl opsx: openspec binary not found (set OPENSPEC_BIN or install from https://openspec.dev)")
		return 1
	}

	// Determine daemon socket — honor MILLIWAYS_SOCK env var for consistency
	// with the rest of milliwaysctl, even though runOpsxAgent is called from
	// runOpsx before flag parsing.
	socket := os.Getenv("MILLIWAYS_SOCK")
	if socket == "" {
		socket = defaultSocket()
	}

	// Determine agent — OPSX_AGENT env var lets users override.
	agentID := os.Getenv("OPSX_AGENT")
	if agentID == "" {
		agentID = "claude"
	}

	// 1. Open an agent session.
	c, err := rpc.Dial(socket)
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl opsx: dial %s: %v\n", socket, err)
		return 1
	}
	defer func() { _ = c.Close() }()

	var openResp map[string]any
	if err := c.Call("agent.open", map[string]any{"agent_id": agentID}, &openResp); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl opsx: agent.open %s: %v\n", agentID, err)
		return 1
	}
	handle, ok := openResp["handle"].(float64)
	if !ok {
		fmt.Fprintf(stderr, "milliwaysctl opsx: agent.open response missing handle\n")
		return 1
	}
	handleInt := int64(handle)

	// 2. Subscribe to the agent stream so we can relay its output.
	events, cancelEvents, err := c.Subscribe("agent.stream", map[string]any{"handle": handleInt})
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl opsx: agent.stream subscribe: %v\n", err)
		return 1
	}
	defer cancelEvents()

	// 3. Start openspec explore/apply as a foreground subprocess.
	// We pass the change name; additional args (if any) go verbatim.
	openspecArgs := append([]string{verb, changeName}, rest[1:]...)
	cmd := exec.Command(bin, openspecArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Set up a pipe so we can forward subprocess output to the agent.
	cmd.Stdin = os.Stdin // stdin stays terminal; openspec reads user input

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl opsx: start %s: %v\n", bin, err)
		return 1
	}

	// 4. Start a goroutine that reads openspec stdout and sends it to the agent.
	// Because we set cmd.Stdout=os.Stdout, output goes directly to the terminal.
	// We also need to forward it to the agent via agent.send.
	// Use a pipe for stdout so we can fan out to both terminal and agent.
	openspecStdout, pipeW, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl opsx: pipe: %v\n", err)
		_ = cmd.Process.Kill()
		return 1
	}
	openspecStderr, pipeErrW, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl opsx: pipe: %v\n", err)
		_ = cmd.Process.Kill()
		return 1
	}
	cmd.Stdout = pipeW
	cmd.Stderr = pipeErrW

	// Send goroutine — reads from openspec stdout pipe and forwards to agent.
	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		defer openspecStdout.Close()
		defer pipeW.Close()

		buf := make([]byte, 4096)
		for {
			n, err := openspecStdout.Read(buf)
			if n > 0 {
				// Write to terminal.
				os.Stdout.Write(buf[:n])
				// Also forward to agent.
				b64 := base64.StdEncoding.EncodeToString(buf[:n])
				_ = c.Call("agent.send", map[string]any{
					"handle":         handleInt,
					"b64":            b64,
					"expand_context": true,
				}, nil)
			}
			if err != nil {
				break
			}
		}
	}()

	// Stderr goroutine — reads from openspec stderr pipe and forwards to terminal.
	go func() {
		defer openspecStderr.Close()
		defer pipeErrW.Close()
		io.Copy(os.Stderr, openspecStderr)
	}()

	// 5. Print agent stream events to terminal while the subprocess runs.
	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		for ev := range events {
			// Parse the NDJSON event. Agent stream events look like:
			// {"t":"data","b64":"..."} or {"t":"end"}
			var msg struct {
				T   string `json:"t"`
				B64 string `json:"b64"`
			}
			if err := json.Unmarshal(ev, &msg); err != nil {
				continue
			}
			switch msg.T {
			case "data":
				bytes, err := base64.StdEncoding.DecodeString(msg.B64)
				if err == nil {
					os.Stdout.Write(bytes)
				}
			case "end":
				return
			}
		}
	}()

	// 6. Wait for the subprocess to finish.
	waitErr := cmd.Wait()
	<-sendDone

	// Close the agent's input side now that the subprocess is done.
	// Send a sentinel to let the agent know the input stream ended.
	_ = c.Call("agent.send", map[string]any{
		"handle":         handleInt,
		"b64":            base64.StdEncoding.EncodeToString([]byte("\n[openspec output ended]\n")),
		"expand_context": false,
	}, nil)

	// Drain remaining agent output briefly, then close.
	close(agentDone)
	cancelEvents()

	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			return ee.ExitCode()
		}
		return 1
	}
	return 0
}
