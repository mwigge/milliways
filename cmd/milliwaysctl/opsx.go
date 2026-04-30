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
// Apply / explore (which compose openspec output with a chat runner) are
// deferred — they require orchestration with the daemon's agent.send and
// will land as `milliwaysctl opsx apply <change> --agent <name>` in a
// follow-up.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// opsxVerbs is the discoverable verb list for --help and the wezterm palette.
var opsxVerbs = []string{
	"list",
	"status",
	"show",
	"archive",
	"validate",
}

// runOpsx dispatches `milliwaysctl opsx <verb> [args...]` and returns the
// process exit code.
func runOpsx(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printOpsxUsage(stderr)
		return 2
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "-h", "--help", "help":
		printOpsxUsage(stdout)
		return 0
	case "list", "status", "show", "archive", "validate":
		return runOpsxOnce(buildOpsxArgs(verb, rest), stdout, stderr)
	default:
		fmt.Fprintf(stderr, "milliwaysctl opsx: unknown verb %q\n", verb)
		printOpsxUsage(stderr)
		return 2
	}
}

func printOpsxUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: milliwaysctl opsx <verb> [args...]")
	fmt.Fprintln(w, "verbs:")
	fmt.Fprintln(w, "  list                       list openspec changes")
	fmt.Fprintln(w, "  status [<change>]          show change progress (current change if omitted)")
	fmt.Fprintln(w, "  show <change>              show full change detail")
	fmt.Fprintln(w, "  archive <change>           archive a completed change")
	fmt.Fprintln(w, "  validate <change>          validate a change")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Override binary with OPENSPEC_BIN env var; default: openspec on PATH.")
	fmt.Fprintln(w, "apply / explore (compose with a chat runner) are deferred — see")
	fmt.Fprintln(w, "openspec/changes/decommission-repl-into-daemon/tasks.md task 4.2.")
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
