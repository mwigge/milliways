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

// `milliwaysctl install <client>` — bootstrap the upstream CLI behind a
// runner so it becomes usable from milliways. Surfaced in the chat
// landing zone as `/install <client>` (chat.go's chatCtlAliases routes
// to this).
//
// Each spec records:
//   - prereq:  binary that must already be on PATH (npm, gh, …); if
//              absent we print install pointers instead of running
//   - check:   `<bin> --version`-style command that reports success
//   - install: argv to invoke (uses execCommand so tests can stub)
//
// HTTP-only runners (minimax, pool) have no CLI to install — they fall
// through to a usage message that points to the API-key envs they need.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// installSpec describes how to install one client's CLI.
type installSpec struct {
	// client is the user-facing name (matches chatSwitchableAgents).
	client string
	// prereq is the binary that must be on PATH for `install` to work
	// (e.g. "npm", "gh"). Empty for HTTP-only runners.
	prereq string
	// prereqHint is shown when prereq is missing.
	prereqHint string
	// check is the argv that reports whether the CLI is already
	// installed (exit 0 = installed). Skipped if empty.
	check []string
	// install is the argv that actually performs the install. Empty
	// means "no CLI to install — print info only".
	install []string
	// info is shown alongside the install plan and after success.
	info string
}

// installSpecs is the source of truth for install routes. Keep ordered
// the same way as chatSwitchableAgents so help output matches the
// landing zone.
var installSpecs = []installSpec{
	{
		client: "berget",
		info:   "berget is HTTP-only; no CLI to install. Set BERGET_API_KEY (and optionally BERGET_API_URL / BERGET_MODEL) and the runner will work.",
	},
	{
		client: "minimax",
		info:   "minimax is HTTP-only; no CLI to install. Set MINIMAX_API_KEY (and optionally MINIMAX_API_URL) and the runner will work.",
	},
	{
		client:     "claude",
		prereq:     "npm",
		prereqHint: "install Node.js (https://nodejs.org/) — npm ships with it",
		check:      []string{"claude", "--version"},
		install:    []string{"npm", "install", "-g", "@anthropic-ai/claude-code"},
		info:       "After install: run `claude` once outside milliways to authenticate.",
	},
	{
		client:     "codex",
		prereq:     "npm",
		prereqHint: "install Node.js (https://nodejs.org/) — npm ships with it",
		check:      []string{"codex", "--version"},
		install:    []string{"npm", "install", "-g", "@openai/codex"},
		info:       "After install: set OPENAI_API_KEY (or run `codex login`).",
	},
	{
		client:     "copilot",
		prereq:     "gh",
		prereqHint: "install GitHub CLI: https://cli.github.com",
		check:      []string{"gh", "extension", "list"}, // grep happens via post-check
		install:    []string{"gh", "extension", "install", "github/gh-copilot"},
		info:       "After install: run `gh auth login` if you haven't already.",
	},
	{
		client:     "gemini",
		prereq:     "npm",
		prereqHint: "install Node.js (https://nodejs.org/) — npm ships with it",
		check:      []string{"gemini", "--version"},
		install:    []string{"npm", "install", "-g", "@google/gemini-cli"},
		info:       "After install: set GEMINI_API_KEY or run `gemini auth`.",
	},
	{
		client:  "local",
		install: []string{}, // handled specially — shells to local install-server
		info:    "Installs rs-llmctl + a default coder model. Same as /install-local-server.",
	},
	{
		client: "kimi",
		info:   "kimi is HTTP-only; no CLI to install. Set KIMI_API_KEY (or MOONSHOT_API_KEY) and the runner will work.",
	},
	{
		client: "deepseek",
		info:   "deepseek is HTTP-only; no CLI to install. Set DEEPSEEK_API_KEY and the runner will work.",
	},
	{
		client: "pool",
		prereq: "npm",
		prereqHint: "install Node.js (https://nodejs.org/) — npm ships with it",
		check:   []string{"pool", "--version"},
		install: []string{"npm", "install", "-g", "@poolside-ai/pool"},
		info:    "After install: run `pool login` to authenticate (see https://docs.poolside.ai/cli/pool), then use /pool in the chat.",
	},
	{
		client:     "scanners",
		prereq:     "go",
		prereqHint: "install Go (https://go.dev/) — needed to install security scanners",
		check:      []string{"gitleaks", "version"},
		info:       "Installs gitleaks, govulncheck, osv-scanner, and semgrep to $(go env GOPATH)/bin",
		install: []string{"sh", "-c",
			`gobin="$(go env GOPATH)/bin" && mkdir -p "$gobin" && ` +
				`GOBIN="$gobin" go install github.com/gitleaks/gitleaks/v8@latest && ` +
				`GOBIN="$gobin" go install golang.org/x/vuln/cmd/govulncheck@latest && ` +
				`GOBIN="$gobin" go install github.com/google/osv-scanner/v2/cmd/osv-scanner@latest && ` +
				`pip install --user semgrep 2>/dev/null || pip3 install --user semgrep 2>/dev/null || true && ` +
				`echo "Scanners installed to $gobin — ensure it is on PATH"`},
	},
}

// installSpecByClient returns the spec for client or false.
func installSpecByClient(client string) (installSpec, bool) {
	for _, s := range installSpecs {
		if s.client == client {
			return s, true
		}
	}
	return installSpec{}, false
}

// findGoDir returns the directory containing the go binary if it can be
// located outside the current PATH, or an empty string if not found.
// The caller should prepend the returned directory to PATH when spawning
// the install subprocess.
func findGoDir() string {
	homeDir, _ := os.UserHomeDir()
	candidates := []string{
		"/usr/local/go/bin/go",
		"/opt/homebrew/bin/go",                              // Homebrew on Apple Silicon
		"/usr/local/bin/go",                                 // Homebrew on Intel Mac
		filepath.Join(homeDir, "go", "bin", "go"),           // default GOPATH
		filepath.Join(homeDir, ".local", "go", "bin", "go"), // alternative user install
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return filepath.Dir(c)
		}
	}
	return ""
}

// runInstall dispatches `milliwaysctl install <client>` and returns the
// process exit code. Pulled out so chat.go (via the ctl alias) and the
// CLI both go through the same code path.
func runInstall(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printInstallUsage(stdout)
		return 0
	}
	verb := args[0]
	switch verb {
	case "-h", "--help", "help":
		printInstallUsage(stdout)
		return 0
	case "list":
		printInstallList(stdout)
		return 0
	}

	spec, ok := installSpecByClient(verb)
	if !ok {
		_, _ = fmt.Fprintf(stderr, "milliwaysctl install: unknown client %q\n", verb)
		printInstallUsage(stderr)
		return 2
	}

	// HTTP-only / pool: just show info, don't try to install.
	if len(spec.install) == 0 && spec.client != "local" {
		_, _ = fmt.Fprintln(stdout, spec.info)
		return 0
	}

	// local: shim to local install-server (single source of truth).
	if spec.client == "local" {
		_, _ = fmt.Fprintln(stdout, "→ delegating to `milliwaysctl local install-server`")
		return runLocalInstallServer(nil, stdout, stderr)
	}

	// Prereq check.
	var extraPathDir string // non-empty: prepend to subprocess PATH
	if spec.prereq != "" {
		if _, err := exec.LookPath(spec.prereq); err != nil {
			// For the "go" prereq, check well-known install locations before
			// failing — Go is often installed but not on the shell PATH.
			if spec.prereq == "go" {
				extraPathDir = findGoDir()
			}
			if extraPathDir == "" {
				_, _ = fmt.Fprintf(stderr, "milliwaysctl install %s: prerequisite %q not on PATH\n", spec.client, spec.prereq)
				_, _ = fmt.Fprintf(stderr, "  → %s\n", spec.prereqHint)
				return 1
			}
			// Found at a non-PATH location — proceed with augmented PATH.
			_, _ = fmt.Fprintf(stdout, "note: %s found at %s (not on PATH); adding to subprocess PATH\n", spec.prereq, extraPathDir)
		}
	}

	// Already installed?
	if len(spec.check) > 0 {
		if alreadyInstalled(spec) {
			_, _ = fmt.Fprintf(stdout, "✓ %s already installed (skipping). %s\n", spec.client, spec.info)
			return 0
		}
	}

	// Install.
	_, _ = fmt.Fprintf(stdout, "→ installing %s via: %s\n", spec.client, strings.Join(spec.install, " "))
	cmd := execCommand(spec.install[0], spec.install[1:]...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if extraPathDir != "" {
		augmented := extraPathDir + string(os.PathListSeparator) + os.Getenv("PATH")
		cmd.Env = append(os.Environ(), "PATH="+augmented)
	}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			_, _ = fmt.Fprintf(stderr, "milliwaysctl install %s: install command exited %d\n", spec.client, ee.ExitCode())
			return ee.ExitCode()
		}
		_, _ = fmt.Fprintf(stderr, "milliwaysctl install %s: %v\n", spec.client, err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "✓ %s installed. %s\n", spec.client, spec.info)
	return 0
}

// alreadyInstalled runs spec.check; exit 0 means yes.
//
// Special case for copilot: `gh extension list` always exits 0; we
// have to scan the output for "github/gh-copilot".
//
// Special case for scanners: check gitleaks only (first of the batch).
func alreadyInstalled(spec installSpec) bool {
	if spec.client == "copilot" {
		out, err := execCommand("gh", "extension", "list").CombinedOutput()
		if err != nil {
			return false
		}
		return strings.Contains(string(out), "gh-copilot")
	}
	if spec.client == "scanners" {
		// Check just gitleaks as proxy for the whole batch.
		// gitleaks v8 uses "version" (not "--version").
		out, err := execCommand("gitleaks", "version").CombinedOutput()
		return err == nil && len(out) > 0
	}
	cmd := execCommand(spec.check[0], spec.check[1:]...)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

func printInstallUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "usage: milliwaysctl install <client>")
	_, _ = fmt.Fprintln(w, "       /install <client>      (from inside the milliways chat)")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Installs the upstream CLI for a runner so milliways can drive it.")
	_, _ = fmt.Fprintln(w, "Run `milliwaysctl install list` for the supported clients.")
}

func printInstallList(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Clients:")
	// Stable display: by spec order, not map iteration.
	clients := make([]string, 0, len(installSpecs))
	for _, s := range installSpecs {
		clients = append(clients, s.client)
	}
	sort.SliceStable(clients, func(i, j int) bool {
		// Preserve original spec order; sort.SliceStable + always-false less
		// keeps insertion order, but we want explicit guarantee.
		return false
	})
	for _, c := range clients {
		spec, _ := installSpecByClient(c)
		switch {
		case len(spec.install) > 0:
			_, _ = fmt.Fprintf(w, "  %-8s  %s\n", c, strings.Join(spec.install, " "))
		case spec.client == "local":
			_, _ = fmt.Fprintf(w, "  %-8s  scripts/install_local.sh\n", c)
		default:
			_, _ = fmt.Fprintf(w, "  %-8s  (no CLI; configure API key — see `milliwaysctl install %s`)\n", c, c)
		}
	}
}
