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

// Package runners probes the local environment for each milliways runner
// (claude, codex, copilot, gemini, local, minimax, pool). Probing is
// intentionally minimal — just enough to populate agent.list's
// auth_status accurately. Note: opsx is intentionally not probed; it was
// reclassified as a `milliwaysctl opsx <verb>` subcommand rather than a
// chat/stream runner during the decommission-repl-into-daemon change.
package runners

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// AgentInfo mirrors the AgentInfo shape in proto/milliways.json. Defined
// here (not in package daemon) to avoid a circular import; the daemon
// package converts to its own AgentInfo before serializing.
type AgentInfo struct {
	ID          string
	Available   bool
	AuthStatus  string // "ok" | "missing_credentials" | "expired" | "unknown"
	Model       string
	Enforcement EnforcementMetadata
}

// Probe walks the seven canonical chat-surface runners and returns one
// AgentInfo each. Each probe has a 2-second budget; missing binaries
// return immediately.
func Probe(ctx context.Context) []AgentInfo {
	probes := []func(context.Context) AgentInfo{
		probeClaude,
		probeCodex,
		probeCopilot,
		probeGemini,
		probeLocal,
		probeMinimax,
		probePool,
	}
	out := make([]AgentInfo, 0, len(probes))
	for _, p := range probes {
		info := p(ctx)
		info.Enforcement = ClientEnforcementMetadata(info.ID)
		out = append(out, info)
	}
	return out
}

func probeClaude(ctx context.Context) AgentInfo {
	info := AgentInfo{ID: "claude", AuthStatus: "missing_credentials"}
	binary, err := probeRunnerBinary(AgentIDClaude, "claude")
	if err != nil {
		return info
	}
	info.Available = true
	home, _ := os.UserHomeDir()
	// v1.x: credentials stored as a JSON file.
	// v2.x: credentials stored in macOS Keychain (no credential file); auth
	// is signalled by CLAUDE_CODE_EXECPATH being set (the versioned install
	// path) or by a non-empty ~/.claude/sessions/ directory.
	credCandidates := []string{
		filepath.Join(home, ".claude", "credentials.json"),
		filepath.Join(home, ".claude", ".credentials.json"),
		filepath.Join(home, ".config", "anthropic", "auth.json"),
	}
	for _, c := range credCandidates {
		if _, err := os.Stat(c); err == nil {
			info.AuthStatus = "ok"
			return info
		}
	}
	// v2.x Keychain auth: CLAUDE_CODE_EXECPATH is set when the user has
	// authenticated via `claude auth login`.
	if os.Getenv("CLAUDE_CODE_EXECPATH") != "" {
		info.AuthStatus = "ok"
		return info
	}
	// Fallback: sessions dir non-empty means at least one login happened.
	sessDir := filepath.Join(home, ".claude", "sessions")
	if entries, err := os.ReadDir(sessDir); err == nil && len(entries) > 0 {
		info.AuthStatus = "ok"
		return info
	}
	if runOK(ctx, binary, "--version") {
		info.AuthStatus = "unknown"
	}
	return info
}

func probeCodex(ctx context.Context) AgentInfo {
	info := AgentInfo{ID: "codex", AuthStatus: "missing_credentials"}
	binary, err := probeRunnerBinary(AgentIDCodex, "codex")
	if err != nil {
		return info
	}
	info.Available = true
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".codex", "auth.json"),
		filepath.Join(home, ".config", "codex", "auth.json"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			info.AuthStatus = "ok"
			return info
		}
	}
	if runOK(ctx, binary, "--version") {
		info.AuthStatus = "unknown"
	}
	return info
}

func probeMinimax(ctx context.Context) AgentInfo {
	// MiniMax is HTTP-only — no binary required. Available whenever the API key
	// is set, regardless of whether any 'minimax' binary exists on PATH.
	info := AgentInfo{ID: "minimax", AuthStatus: "missing_credentials"}
	if k := os.Getenv("MINIMAX_API_KEY"); k != "" {
		info.Available = true
		info.AuthStatus = "ok"
	}
	return info
}

func probeCopilot(ctx context.Context) AgentInfo {
	info := AgentInfo{ID: "copilot", AuthStatus: "missing_credentials"}
	// RunCopilot shells the standalone `copilot` binary (not `gh copilot`).
	// Probe the same binary the runner actually invokes — anything else
	// produces a probe/runtime mismatch where probe says ok and dispatch
	// fails with "exec: copilot: not found".
	binary, err := probeRunnerBinary(AgentIDCopilot, "copilot")
	if err != nil {
		return info
	}
	info.Available = true
	if runOK(ctx, binary, "--version") {
		info.AuthStatus = "ok"
	}
	return info
}

func probeGemini(ctx context.Context) AgentInfo {
	info := AgentInfo{ID: "gemini", AuthStatus: "missing_credentials"}
	binary, err := probeRunnerBinary(AgentIDGemini, "gemini")
	if err != nil {
		return info
	}
	info.Available = true
	// gemini CLI uses gcloud-style auth in $HOME/.config/gcloud or
	// per-project credentials; presence of the binary + a successful
	// --version is the strongest non-network signal we can give.
	if runOK(ctx, binary, "--version") {
		info.AuthStatus = "ok"
	}
	return info
}

func probeLocal(_ context.Context) AgentInfo {
	// Local is not gated by a binary on PATH — it's an HTTP backend.
	// The runner has a default endpoint (http://localhost:8765/v1) so
	// configuration is always present. Real reachability is tested at
	// dispatch-time; probing here would add network latency to every
	// agent.list. Mark "ok" so UIs show the runner; users will see a
	// clear connect error on first use if the backend isn't running.
	return AgentInfo{ID: "local", Available: true, AuthStatus: "ok"}
}

func probePool(ctx context.Context) AgentInfo {
	info := AgentInfo{ID: "pool", AuthStatus: "missing_credentials"}
	binary, err := probeRunnerBinary(AgentIDPool, "pool")
	if err != nil {
		return info
	}
	info.Available = true
	// pool --version exits 0 regardless of auth state; treats it as a best-effort
	// signal that the binary is functional. Real auth is file-based at
	// ~/.config/poolside/credentials.json and verified at dispatch time.
	if runOK(ctx, binary, "--version") {
		info.AuthStatus = "ok"
	}
	return info
}

func probeRunnerBinary(agentID, binary string) (string, error) {
	return execLookPathInRunnerPathExcluding(binary, brokerShimDirForAgent(agentID))
}

// runOK runs cmd with a 2-second timeout and returns true iff it exits 0.
func runOK(ctx context.Context, name string, args ...string) bool {
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	c := exec.CommandContext(pctx, name, args...)
	c.Stdout, c.Stderr = nil, nil
	return c.Run() == nil
}
