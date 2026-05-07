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

package runners

// Environment scoping for subprocess CLI runners. Without this, daemon
// runners that spawn `claude`, `codex`, `copilot`, etc. inherit the full
// daemon env — including MINIMAX_API_KEY, MILLIWAYS_LOCAL_API_KEY, AWS_*,
// GITHUB_TOKEN, GH_TOKEN, and any other secrets the user happens to have
// in their shell. With the agentic tool loop wired into HTTP runners, a
// prompt-injected codex session can `printenv` or read /proc/self/environ
// and the agentic loop folds it back to a remote model.
//
// Same shape as internal/kitchen/adapter/adapter.go's safeEnv (duplicated
// rather than imported because adapter is a sibling package; consolidating
// into an internal/sandbox package is a follow-up).

import (
	"os"
	"strings"
)

var runnerSystemPathFallbacks = []string{
	"/opt/homebrew/bin",
	"/usr/local/bin",
	"/usr/bin",
	"/bin",
	"/usr/sbin",
	"/sbin",
}

// safeRunnerEnvKeys is the set of environment variables passed to runner
// subprocess execution. Mirrors the kitchen adapter list with the same
// trade-offs:
//   - PATH/HOME/USER/SHELL/TERM/LANG/LC_*/TMPDIR/XDG_*  → required for
//     basic CLI operation
//   - ANTHROPIC_API_KEY / OPENAI_API_KEY / GOOGLE_API_KEY / GEMINI_API_KEY
//     → required for the respective CLI to authenticate
//   - OLLAMA_HOST → required if the user's local CLI workflow involves it
//
// Notably absent: MINIMAX_API_KEY, MILLIWAYS_LOCAL_API_KEY, AWS_*,
// GITHUB_TOKEN, GH_TOKEN — these are not required by any of the CLIs we
// shell to, so withholding them prevents accidental exfil.
var safeRunnerEnvKeys = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "SHELL": true,
	"TERM": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
	"TMPDIR": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_RUNTIME_DIR": true,
	"ANTHROPIC_API_KEY": true, "OPENAI_API_KEY": true,
	"GOOGLE_API_KEY": true, "GEMINI_API_KEY": true,
	"OLLAMA_HOST": true,
	// Model selection — injected live via config.setenv so /model <name>
	// takes effect without restarting the daemon or its subprocesses.
	"ANTHROPIC_MODEL": true, "OPENAI_MODEL": true, "CODEX_MODEL": true,
	"CLAUDE_MODEL": true, "GEMINI_MODEL": true, "GOOGLE_MODEL": true,
	"COPILOT_MODEL": true,
	// Claude Code 2.x runtime identity vars. CLAUDE_CODE_EXECPATH tells the
	// binary where its versioned install lives (used to locate the credential
	// store). Without these the daemon subprocess reports "Not logged in" even
	// though claude works fine in the user's shell.
	"CLAUDECODE": true, "CLAUDE_CODE_ENTRYPOINT": true, "CLAUDE_CODE_EXECPATH": true,
}

// safeRunnerEnv returns a filtered environment for runner subprocess
// execution. Uses os.Environ() as the source and keeps only entries
// whose key appears in safeRunnerEnvKeys.
//
// PATH override: if MILLIWAYS_PATH is set (via /path or local.env), it
// replaces the inherited PATH so CLIs installed in non-standard locations
// (e.g. ~/.local/bin, /opt/homebrew/bin) are found when milliways is
// launched from a GUI app bundle whose PATH is minimal.
func safeRunnerEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		key := e
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			key = e[:idx]
		}
		if safeRunnerEnvKeys[key] {
			env = append(env, e)
		}
	}
	// Allow an explicit PATH override so users can extend the search path
	// without restarting the daemon. MILLIWAYS_PATH replaces PATH entirely
	// when set; it is not appended to avoid duplicates.
	if p := os.Getenv("MILLIWAYS_PATH"); p != "" {
		filtered := make([]string, 0, len(env))
		for _, e := range env {
			if !strings.HasPrefix(e, "PATH=") {
				filtered = append(filtered, e)
			}
		}
		env = append(filtered, "PATH="+ensureRunnerSystemPath(p))
	} else {
		for i, e := range env {
			if strings.HasPrefix(e, "PATH=") {
				env[i] = "PATH=" + ensureRunnerSystemPath(strings.TrimPrefix(e, "PATH="))
				return env
			}
		}
		env = append(env, "PATH="+ensureRunnerSystemPath(""))
	}
	return env
}

func ensureRunnerSystemPath(path string) string {
	parts := splitPath(path)
	seen := make(map[string]bool, len(parts)+len(runnerSystemPathFallbacks))
	for _, part := range parts {
		seen[part] = true
	}
	for _, fallback := range runnerSystemPathFallbacks {
		if !seen[fallback] {
			parts = append(parts, fallback)
			seen[fallback] = true
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	raw := strings.Split(path, string(os.PathListSeparator))
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}
