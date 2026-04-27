// Package runners probes the local environment for each milliways runner
// (claude, codex, minimax, copilot). Probing is intentionally minimal —
// just enough to populate agent.list's auth_status accurately. Full
// runner code lifts from internal/repl/runner_*.go in a follow-up.
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
	ID         string
	Available  bool
	AuthStatus string // "ok" | "missing_credentials" | "expired" | "unknown"
	Model      string
}

// Probe walks all four canonical runners and returns one AgentInfo each.
// Each probe has a 2-second budget; missing binaries return immediately.
func Probe(ctx context.Context) []AgentInfo {
	probes := []func(context.Context) AgentInfo{
		probeClaude,
		probeCodex,
		probeMinimax,
		probeCopilot,
	}
	out := make([]AgentInfo, 0, len(probes))
	for _, p := range probes {
		out = append(out, p(ctx))
	}
	return out
}

func probeClaude(ctx context.Context) AgentInfo {
	info := AgentInfo{ID: "claude", AuthStatus: "missing_credentials"}
	if _, err := exec.LookPath("claude"); err != nil {
		return info
	}
	info.Available = true
	// Auth: ~/.claude/credentials.json or ~/.claude/.credentials.json or
	// ~/.config/anthropic/auth.json. Presence == probably authenticated.
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".claude", "credentials.json"),
		filepath.Join(home, ".claude", ".credentials.json"),
		filepath.Join(home, ".config", "anthropic", "auth.json"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			info.AuthStatus = "ok"
			return info
		}
	}
	if runOK(ctx, "claude", "--version") {
		info.AuthStatus = "unknown"
	}
	return info
}

func probeCodex(ctx context.Context) AgentInfo {
	info := AgentInfo{ID: "codex", AuthStatus: "missing_credentials"}
	if _, err := exec.LookPath("codex"); err != nil {
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
	if runOK(ctx, "codex", "--version") {
		info.AuthStatus = "unknown"
	}
	return info
}

func probeMinimax(ctx context.Context) AgentInfo {
	info := AgentInfo{ID: "minimax", AuthStatus: "missing_credentials"}
	if _, err := exec.LookPath("minimax"); err == nil {
		info.Available = true
	}
	// MiniMax is API-key driven; presence of the env var trumps binary check.
	if k := os.Getenv("MINIMAX_API_KEY"); k != "" {
		info.AuthStatus = "ok"
	}
	return info
}

func probeCopilot(ctx context.Context) AgentInfo {
	info := AgentInfo{ID: "copilot", AuthStatus: "missing_credentials"}
	if _, err := exec.LookPath("gh"); err != nil {
		// gh is the only practical channel for copilot CLI auth on this host.
		return info
	}
	info.Available = true
	// `gh copilot --version` succeeds iff the gh-copilot extension is installed.
	if runOK(ctx, "gh", "copilot", "--version") {
		info.AuthStatus = "ok"
	}
	return info
}

// runOK runs cmd with a 2-second timeout and returns true iff it exits 0.
func runOK(ctx context.Context, name string, args ...string) bool {
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	c := exec.CommandContext(pctx, name, args...)
	c.Stdout, c.Stderr = nil, nil
	return c.Run() == nil
}
