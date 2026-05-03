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

// `milliwaysctl local <verb>` — in-app local-model bootstrap. Lets users
// install llama-server / llama-swap, list available models, switch which
// backend the runner talks to, download GGUF weights from HuggingFace, and
// register new models in the llama-swap config — all without leaving the
// milliways terminal.
//
// Verbs:
//   install-server         install llama.cpp via scripts/install_local.sh
//   install-swap           install llama-swap via scripts/install_local_swap.sh
//   list-models            GET $MILLIWAYS_LOCAL_ENDPOINT/models
//   switch-server <kind>   write ~/.config/milliways/local.env for the kind
//   download-model <repo>  curl a GGUF from HuggingFace into MODEL_DIR
//   setup-model    <repo>  download + register in llama-swap.yaml + verify

import (
	"context"
	"encoding/json"
	"errors"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// localVerbs is the list of supported `local` verbs surfaced by --help and
// by the wezterm slash dispatcher (which can read it via help output).
var localVerbs = []string{
	"install-server",
	"install-swap",
	"list-models",
	"switch-server",
	"download-model",
	"setup-model",
	"swap-mode",
	"server-start",
	"server-stop",
	"server-status",
	"server-port",
	"server-uninstall",
	"default-model",
}

// runLocal dispatches `milliwaysctl local <verb> [args...]` and returns the
// process exit code.
func runLocal(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printLocalUsage(stderr)
		return 2
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "install-server":
		return runLocalInstallServer(rest, stdout, stderr)
	case "install-swap":
		return runLocalInstallSwap(rest, stdout, stderr)
	case "list-models":
		return runLocalListModels(rest, stdout, stderr)
	case "switch-server":
		return runLocalSwitchServer(rest, stdout, stderr)
	case "download-model":
		return runLocalDownloadModel(rest, stdout, stderr)
	case "setup-model":
		return runLocalSetupModel(rest, stdout, stderr)
	case "swap-mode":
		return runLocalSwapMode(rest, stdout, stderr)
	case "server-start":
		return runLocalServerStart(rest, stdout, stderr)
	case "server-stop":
		return runLocalServerStop(rest, stdout, stderr)
	case "server-status":
		return runLocalServerStatus(rest, stdout, stderr)
	case "server-port":
		return runLocalServerPort(rest, stdout, stderr)
	case "server-uninstall":
		return runLocalServerUninstall(rest, stdout, stderr)
	case "default-model":
		return runLocalDefaultModel(rest, stdout, stderr)
	case "-h", "--help", "help":
		printLocalUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "milliwaysctl local: unknown verb %q\n", verb)
		printLocalUsage(stderr)
		return 2
	}
}

func printLocalUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: milliwaysctl local <verb> [args...]")
	fmt.Fprintln(w, "verbs:")
	fmt.Fprintln(w, "  install-server                     install llama.cpp + default model")
	fmt.Fprintln(w, "  install-swap [--hot]               install llama-swap (hot-swap setup)")
	fmt.Fprintln(w, "  list-models                        list models exposed by the configured backend")
	fmt.Fprintln(w, "  switch-server <kind>               kind = llama-server | llama-swap | ollama | vllm | lmstudio")
	fmt.Fprintln(w, "  download-model <repo> [--quant Q] [--alias A]   curl a GGUF from HuggingFace")
	fmt.Fprintln(w, "  setup-model list                                list curated top-10 models")
	fmt.Fprintln(w, "  setup-model refresh                             refresh list from HuggingFace API")
	fmt.Fprintln(w, "  setup-model    <repo> [--quant Q] [--alias A]   download + register in llama-swap")
	fmt.Fprintln(w, "  swap-mode hot|cold [--ttl N]                    set llama-swap to hot (always-loaded) or cold (unload after TTL seconds, default 600)")
	fmt.Fprintln(w, "  server-start                                    start the local server (launchctl / systemd / direct)")
	fmt.Fprintln(w, "  server-stop                                     stop the local server")
	fmt.Fprintln(w, "  server-status                                   check server reachability and list loaded models")
	fmt.Fprintln(w, "  server-port                                     print the port number from MILLIWAYS_LOCAL_ENDPOINT")
	fmt.Fprintln(w, "  server-uninstall [--yes]                        stop server, remove service files and launcher")
	fmt.Fprintln(w, "  default-model <alias>                           set the default model in the launcher and local.env")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Endpoint defaults to http://localhost:8765/v1; override with MILLIWAYS_LOCAL_ENDPOINT.")
	fmt.Fprintln(w, "Models cache to $HOME/.local/share/milliways/models/; override with MODEL_DIR.")
}

// install-server / install-swap shell out to the existing install scripts so
// they stay the single source of truth for the install procedure. Tests
// override `execCommand` to assert pass-through behaviour without actually
// running the scripts.
var execCommand = exec.Command

func runLocalInstallServer(_ []string, stdout, stderr io.Writer) int {
	return runInstallScript("scripts/install_local.sh", stdout, stderr)
}

func runLocalInstallSwap(args []string, stdout, stderr io.Writer) int {
	if hasFlag(args, "--hot") {
		_ = os.Setenv("HOT_MODE", "1")
	}
	return runInstallScript("scripts/install_local_swap.sh", stdout, stderr)
}

func runInstallScript(relPath string, stdout, stderr io.Writer) int {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "local: cannot determine working dir: %v\n", err)
		return 1
	}

	// Build a priority list of candidate paths so the script is found
	// regardless of whether milliways was installed via:
	//   - a git checkout (relPath relative to cwd)
	//   - a native package (.deb/.rpm/.pkg) with scripts/ subdir
	//   - a native package (v1.0.1 and earlier) without scripts/ subdir
	//   - install.sh binary install (scripts in ~/.local/share/milliways/scripts/)
	scriptName := filepath.Base(relPath)
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	exeShare := ""
	if exe != "" {
		exeShare = filepath.Join(filepath.Dir(filepath.Dir(exe)), "share", "milliways")
	}

	candidates := []string{
		filepath.Join(wd, relPath),                                           // checkout: ./scripts/install_local.sh
		filepath.Join(exeShare, relPath),                                     // pkg new: /usr/share/milliways/scripts/install_local.sh
		filepath.Join(exeShare, scriptName),                                  // pkg old: /usr/share/milliways/install_local.sh
		filepath.Join(home, ".local", "share", "milliways", "scripts", scriptName), // binary install
		filepath.Join(home, ".local", "share", "milliways", scriptName),      // legacy
	}

	candidate := ""
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if _, err2 := os.Stat(p); err2 == nil {
			candidate = p
			break
		}
	}
	if candidate == "" {
		fmt.Fprintf(stderr, "local: script %s not found; searched:\n", scriptName)
		for _, p := range candidates {
			if p != "" {
				fmt.Fprintf(stderr, "  %s\n", p)
			}
		}
		return 1
	}
	cmd := execCommand("bash", candidate)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	// Augment PATH so install scripts find tools installed by Homebrew, MacPorts,
	// nvm, and ~/.local/bin even when launched from a GUI app without a full shell.
	cmd.Env = enrichedEnvForScripts()
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(stderr, "local: %v\n", err)
		return 1
	}
	return 0
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// list-models hits the configured backend and prints model IDs one per line.

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func parseListModelsResponse(body []byte) ([]string, error) {
	var resp modelsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(resp.Data))
	for _, m := range resp.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

func runLocalListModels(_ []string, stdout, stderr io.Writer) int {
	endpoint := strings.TrimRight(localEndpoint(), "/")
	url := endpoint + "/models"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintf(stderr, "local: build request: %v\n", err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "local: GET %s: %v (is the backend running? `milliwaysctl local install-server` to bootstrap)\n", url, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "local: backend HTTP %d at %s\n", resp.StatusCode, url)
		return 1
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		fmt.Fprintf(stderr, "local: read body: %v\n", err)
		return 1
	}
	models, err := parseListModelsResponse(body)
	if err != nil {
		fmt.Fprintf(stderr, "local: parse models: %v\n", err)
		return 1
	}
	for _, m := range models {
		fmt.Fprintln(stdout, m)
	}
	return 0
}

func localEndpoint() string {
	if v := os.Getenv("MILLIWAYS_LOCAL_ENDPOINT"); v != "" {
		return v
	}
	return "http://localhost:8765/v1"
}

// switch-server resolves a backend kind to its default endpoint and writes
// it into ~/.config/milliways/local.env. Users source that file (or
// milliways reads it on startup) to pick the active backend.

func localEndpointForKind(kind string) (string, error) {
	switch kind {
	case "llama-server", "llama-swap":
		return "http://127.0.0.1:8765/v1", nil
	case "ollama":
		return "http://127.0.0.1:11434/v1", nil
	case "vllm":
		return "http://127.0.0.1:8000/v1", nil
	case "lmstudio":
		return "http://127.0.0.1:1234/v1", nil
	default:
		return "", fmt.Errorf("unknown backend kind %q (supported: llama-server, llama-swap, ollama, vllm, lmstudio)", kind)
	}
}

func runLocalSwitchServer(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "local switch-server: kind required (llama-server | llama-swap | ollama | vllm | lmstudio)")
		return 2
	}
	kind := args[0]
	endpoint, err := localEndpointForKind(kind)
	if err != nil {
		fmt.Fprintf(stderr, "local switch-server: %v\n", err)
		return 2
	}

	envPath, err := configPath("local.env")
	if err != nil {
		fmt.Fprintf(stderr, "local switch-server: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		fmt.Fprintf(stderr, "local switch-server: mkdir: %v\n", err)
		return 1
	}
	body := fmt.Sprintf("# written by `milliwaysctl local switch-server %s`\nMILLIWAYS_LOCAL_ENDPOINT=%s\n", kind, endpoint)
	if err := os.WriteFile(envPath, []byte(body), 0o644); err != nil {
		fmt.Fprintf(stderr, "local switch-server: write %s: %v\n", envPath, err)
		return 1
	}
	fmt.Fprintf(stdout, "%s -> %s (wrote %s)\n", kind, endpoint, envPath)
	return 0
}

// download-model curls a GGUF from HuggingFace and writes it into MODEL_DIR.

func buildHFGGUFURL(repo, quant string) string {
	// scripts/install_local.sh:114 — the file name strips the trailing -GGUF
	// from the repo basename, then suffixes the quant.
	base := strings.TrimSuffix(filepath.Base(repo), "-GGUF")
	return fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s-%s.gguf", repo, base, quant)
}

func defaultGGUFDest(modelDir, repo, quant string) string {
	// scripts/install_local.sh:115 — keeps the -GGUF in the cached filename
	// so two repos with different basenames don't collide.
	return filepath.Join(modelDir, fmt.Sprintf("%s-%s.gguf", filepath.Base(repo), quant))
}

func runLocalDownloadModel(args []string, stdout, stderr io.Writer) int {
	repo, quant, _, force, code := parseDownloadFlags(args, stderr)
	if code != 0 {
		return code
	}
	modelDir := os.Getenv("MODEL_DIR")
	if modelDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(stderr, "local download-model: home dir: %v\n", err)
			return 1
		}
		modelDir = filepath.Join(home, ".local", "share", "milliways", "models")
	}
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "local download-model: mkdir: %v\n", err)
		return 1
	}
	dest := defaultGGUFDest(modelDir, repo, quant)
	if !force {
		if info, err := os.Stat(dest); err == nil && info.Size() > 50*1024*1024 {
			// Only treat as cached if > 50MB — guards against partial downloads
			// being silently reused. GGUF models are always at least a few hundred MB.
			fmt.Fprintf(stdout, "%s (cached, %d bytes; pass --force to redownload)\n", dest, info.Size())
			return 0
		} else if err == nil && info.Size() > 0 {
			fmt.Fprintf(stderr, "local download-model: partial download detected (%d bytes) — re-downloading\n", info.Size())
			_ = os.Remove(dest)
		}
	}
	// Try mirrors in order: primary HF → hf-mirror.com (bypasses many proxies) → HF token auth.
	mirrors := buildDownloadMirrors(repo, quant)
	for i, url := range mirrors {
		if i > 0 {
			fmt.Fprintf(stderr, "local download-model: trying mirror %d: %s\n", i+1, url)
		}
		// -C - resumes a partial download; safe even when starting fresh.
		args := []string{"-fL", "-C", "-", "-o", dest, url}
		if tok := os.Getenv("HF_TOKEN"); tok != "" {
			args = append([]string{"-H", "Authorization: Bearer " + tok}, args...)
		}
		cmd := execCommand("curl", args...)
		cmd.Stdout = stderr
		cmd.Stderr = stderr
		if err := cmd.Run(); err == nil {
			fmt.Fprintln(stdout, dest)
			return 0
		}
	}
	// All mirrors failed.
	fmt.Fprintf(stderr, "local download-model: all download sources failed for %s %s\n", repo, quant)
	fmt.Fprintf(stderr, "  If behind a corporate proxy, set HF_TOKEN and try again:\n")
	fmt.Fprintf(stderr, "    /login HF_TOKEN hf_xxxx\n")
	fmt.Fprintf(stderr, "  Or download manually and place the .gguf at: %s\n", dest)
	return 1
}

// buildDownloadMirrors returns candidate URLs for a GGUF model, ordered by
// likelihood of succeeding on corporate networks (Zscaler etc).
func buildDownloadMirrors(repo, quant string) []string {
	base := strings.TrimSuffix(filepath.Base(repo), "-GGUF")
	file := fmt.Sprintf("%s-%s.gguf", base, quant)
	// xethub CDN: HuggingFace redirects to cas-bridge.xethub.hf.co which is
	// typically not categorised as "Generative AI" by Zscaler and passes through.
	// We resolve it by following the HF redirect first (HEAD request), then
	// downloading from the CDN URL directly.
	cdnURL := resolveHFCDNURL(
		fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", repo, file),
		os.Getenv("HF_TOKEN"),
	)
	mirrors := []string{}
	if cdnURL != "" {
		mirrors = append(mirrors, cdnURL)
	}
	mirrors = append(mirrors,
		fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", repo, file),
		fmt.Sprintf("https://hf-mirror.com/%s/resolve/main/%s", repo, file),
		fmt.Sprintf("https://modelscope.cn/api/v1/models/%s/repo?Revision=master&FilePath=%s", repo, file),
	)
	return mirrors
}

// resolveHFCDNURL follows a HuggingFace redirect to extract the pre-signed CDN
// URL (cas-bridge.xethub.hf.co). This CDN host is not blocked by Zscaler on
// most corporate networks even when huggingface.co itself is blocked.
func resolveHFCDNURL(hfURL, token string) string {
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Stop at first redirect — that's the CDN URL.
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest("GET", hfURL, nil)
	if err != nil {
		return ""
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusMovedPermanently {
		loc := resp.Header.Get("Location")
		if strings.Contains(loc, "xethub.hf.co") || strings.Contains(loc, "cdn-lfs") {
			return loc
		}
	}
	return ""
}

func parseDownloadFlags(args []string, stderr io.Writer) (repo, quant, alias string, force bool, code int) {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "local download-model: <repo> required (e.g. unsloth/Qwen2.5-Coder-7B-Instruct-GGUF)")
		return "", "", "", false, 2
	}
	repo = args[0]
	quant = "Q4_K_M"
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--quant":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "local download-model: --quant requires a value")
				return "", "", "", false, 2
			}
			quant = args[i+1]
			i++
		case "--alias":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "local download-model: --alias requires a value")
				return "", "", "", false, 2
			}
			alias = args[i+1]
			i++
		case "--force":
			force = true
		default:
			fmt.Fprintf(stderr, "local download-model: unknown flag %q\n", args[i])
			return "", "", "", false, 2
		}
	}
	if alias == "" {
		// default alias: the basename minus -GGUF, lowercased.
		base := strings.TrimSuffix(filepath.Base(repo), "-GGUF")
		alias = strings.ToLower(base)
	}
	return repo, quant, alias, force, 0
}

// setup-model composes download-model + register-in-llama-swap + verify.

func runLocalSetupModel(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "list":
			return runModelCatalogList(stdout)
		case "refresh":
			return runModelCatalogRefresh(stdout, stderr)
		}
		// Accept catalog index (1-10) or short model name as a convenience.
		args = resolveCatalogArg(args, stdout)
	}
	repo, quant, alias, _, code := parseDownloadFlags(args, stderr)
	if code != 0 {
		return code
	}
	// Step 1: download. We re-invoke the same logic to keep behaviour aligned.
	dlCode := runLocalDownloadModel(args, stdout, stderr)
	if dlCode != 0 {
		return dlCode
	}
	modelDir := os.Getenv("MODEL_DIR")
	if modelDir == "" {
		home, _ := os.UserHomeDir()
		modelDir = filepath.Join(home, ".local", "share", "milliways", "models")
	}
	dest := defaultGGUFDest(modelDir, repo, quant)

	// Step 2: register in llama-swap.yaml (idempotent).
	cfgPath, err := configPath("llama-swap.yaml")
	if err != nil {
		fmt.Fprintf(stderr, "local setup-model: %v\n", err)
		return 1
	}
	var existing []byte
	if data, err := os.ReadFile(cfgPath); err == nil {
		existing = data
	} else {
		existing = []byte("models: {}\n")
	}
	updated, changed, err := insertSwapModelEntry(existing, alias, dest)
	if err != nil {
		fmt.Fprintf(stderr, "local setup-model: yaml: %v\n", err)
		return 1
	}
	if changed {
		if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
			fmt.Fprintf(stderr, "local setup-model: mkdir: %v\n", err)
			return 1
		}
		if err := os.WriteFile(cfgPath, updated, 0o644); err != nil {
			fmt.Fprintf(stderr, "local setup-model: write %s: %v\n", cfgPath, err)
			return 1
		}
		fmt.Fprintf(stdout, "registered %s -> %s in %s\n", alias, dest, cfgPath)
	} else {
		fmt.Fprintf(stdout, "%s already registered in %s\n", alias, cfgPath)
	}

	// Step 3: update the launcher script and local.env to point at the new model,
	// then restart (or start) the server so the model is immediately active.
	if err := updateLocalServerLauncher(dest, alias, stderr); err != nil {
		fmt.Fprintf(stderr, "local setup-model: launcher update: %v\n", err)
		fmt.Fprintln(stderr, "  Server not restarted — run /install-local-server manually to activate the model.")
		return 0 // non-fatal: model is registered, just needs manual restart
	}

	// Step 4: restart server with new model.
	fmt.Fprintln(stdout, "Restarting local server with new model...")
	if code := runLocalInstallServer(nil, stdout, stderr); code != 0 {
		fmt.Fprintln(stderr, "  Could not auto-restart — run /install-local-server to activate.")
	}
	return 0
}

// updateLocalServerLauncher rewrites ~/.local/bin/milliways-local-server to
// use the given model path and alias, and updates MILLIWAYS_LOCAL_MODEL in
// local.env. This makes the next server start use the new model automatically.
func updateLocalServerLauncher(modelPath, alias string, stderr io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	launcher := filepath.Join(home, ".local", "bin", "milliways-local-server")

	// Read existing launcher to extract host/port/ctx-size.
	data, _ := os.ReadFile(launcher)
	content := string(data)

	host := "127.0.0.1"
	port := "8765"
	ctx := "16384"
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "--host") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				host = strings.Trim(parts[1], `"`)
			}
		}
		if strings.HasPrefix(line, "--port") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				port = strings.Trim(parts[1], `"`)
			}
		}
		if strings.HasPrefix(line, "--ctx-size") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				ctx = strings.Trim(parts[1], `"`)
			}
		}
	}

	// Find llama-server binary.
	llamaBin := "/opt/homebrew/bin/llama-server"
	if p, err := exec.LookPath("llama-server"); err == nil {
		llamaBin = p
	}

	newLauncher := fmt.Sprintf(`#!/usr/bin/env bash
exec %q \
  -m %q \
  --alias %q \
  --host %q \
  --port %q \
  --ctx-size %q \
  --jinja
`, llamaBin, modelPath, alias, host, port, ctx)

	if err := os.WriteFile(launcher, []byte(newLauncher), 0o755); err != nil {
		return fmt.Errorf("write launcher: %w", err)
	}

	// Update MILLIWAYS_LOCAL_MODEL in local.env.
	envFile, err := configPath("local.env")
	if err != nil {
		envFile = filepath.Join(home, ".config", "milliways", "local.env")
	}
	_ = os.MkdirAll(filepath.Dir(envFile), 0o700)
	var lines []string
	if existing, err := os.ReadFile(envFile); err == nil {
		for _, l := range strings.Split(string(existing), "\n") {
			if !strings.HasPrefix(l, "MILLIWAYS_LOCAL_MODEL=") {
				lines = append(lines, l)
			}
		}
	}
	lines = append(lines, "MILLIWAYS_LOCAL_MODEL="+alias)
	_ = os.WriteFile(envFile, []byte(strings.Join(lines, "\n")+"\n"), 0o600)

	fmt.Fprintf(stderr, "  launcher updated: %s → %s\n", alias, modelPath)
	return nil
}

// enrichedEnvForScripts returns the current environment with PATH augmented to
// include the standard tool locations that are missing when milliways is
// launched from a GUI app (MilliWays.app) without a login shell.
func enrichedEnvForScripts() []string {
	home, _ := os.UserHomeDir()
	extra := []string{
		"/opt/homebrew/bin",         // Apple Silicon Homebrew
		"/usr/local/bin",            // Intel Homebrew + manual installs
		"/opt/pkg/bin",              // MacPorts
		home + "/.local/bin",        // user installs (milliways itself)
		home + "/.cargo/bin",        // Rust toolchain
		"/usr/bin", "/bin", "/usr/sbin", "/sbin",
	}
	// Prepend extras to current PATH, deduplicating.
	cur := os.Getenv("PATH")
	seen := map[string]bool{}
	parts := []string{}
	for _, p := range append(extra, strings.Split(cur, ":")...) {
		if p != "" && !seen[p] {
			seen[p] = true
			parts = append(parts, p)
		}
	}
	env := os.Environ()
	result := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "PATH=") {
			result = append(result, e)
		}
	}
	return append(result, "PATH="+strings.Join(parts, ":"))
}

// runLocalSwapMode sets llama-swap to hot (ttl=0, always loaded) or cold
// (ttl=N seconds, unload after idle). It rewrites the global `healthCheckTimeout`
// field and every per-model `ttl` field in llama-swap.yaml.
//
// Usage: milliwaysctl local swap-mode hot|cold [--ttl N]
// In the REPL: /swap hot  or  /swap cold
func runLocalSwapMode(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: milliwaysctl local swap-mode hot|cold [--ttl N]")
		return 2
	}
	mode := args[0]
	if mode != "hot" && mode != "cold" {
		fmt.Fprintf(stderr, "swap-mode: mode must be 'hot' or 'cold', got %q\n", mode)
		return 2
	}

	ttl := 600 // cold default: 10 minutes
	if mode == "hot" {
		ttl = 0
	}
	for i := 1; i < len(args)-1; i++ {
		if args[i] == "--ttl" {
			if n, err := strconv.Atoi(args[i+1]); err == nil {
				ttl = n
				i++
			}
		}
	}

	cfgPath, err := configPath("llama-swap.yaml")
	if err != nil {
		fmt.Fprintf(stderr, "swap-mode: %v\n", err)
		return 1
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		fmt.Fprintf(stderr, "swap-mode: read %s: %v\n", cfgPath, err)
		fmt.Fprintf(stderr, "  run /install-local-swap first\n")
		return 1
	}

	// Patch every `ttl:` line in the file. llama-swap uses the same field
	// both at the global level and per-model. A simple regexp rewrite is
	// sufficient — the YAML structure is machine-generated and predictable.
	updated := swapModePatchTTL(data, ttl)
	if err := os.WriteFile(cfgPath, updated, 0o644); err != nil {
		fmt.Fprintf(stderr, "swap-mode: write %s: %v\n", cfgPath, err)
		return 1
	}

	label := fmt.Sprintf("cold (unload after %ds idle)", ttl)
	if ttl == 0 {
		label = "hot (always loaded, sub-second switches)"
	}
	fmt.Fprintf(stdout, "llama-swap mode set to %s\n", label)
	fmt.Fprintf(stdout, "  config: %s\n", cfgPath)
	fmt.Fprintf(stdout, "  restart the swap proxy to apply: launchctl kickstart -k gui/$(id -u)/dev.milliways.swap 2>/dev/null || true\n")
	return 0
}

// swapModePatchTTL rewrites every `ttl: <N>` line in the llama-swap YAML.
func swapModePatchTTL(data []byte, ttl int) []byte {
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("ttl:")) {
			// Preserve leading whitespace, replace value.
			indent := line[:len(line)-len(bytes.TrimLeft(line, " \t"))]
			lines[i] = append(indent, []byte(fmt.Sprintf("ttl: %d", ttl))...)
		}
	}
	return bytes.Join(lines, []byte("\n"))
}

// resolveCatalogArg checks if args[0] is a catalog index (1-10) or a short
// model name that matches a catalog entry, and expands it to the full
// --repo/--quant/--alias flags. Returns args unchanged if no match.
func resolveCatalogArg(args []string, stdout io.Writer) []string {
	if len(args) == 0 {
		return args
	}
	first := args[0]
	catalog := loadCatalog()

	// Numeric index: /setup-model 1
	if n, err := strconv.Atoi(first); err == nil && n >= 1 && n <= len(catalog) {
		e := catalog[n-1]
		fmt.Fprintf(stdout, "Installing catalog entry %d: %s\n", n, e.Name)
		return expandCatalogEntry(e, args[1:])
	}

	// Short name match (case-insensitive prefix): /setup-model Qwen2.5-Coder-7B
	lower := strings.ToLower(first)
	for _, e := range catalog {
		if strings.ToLower(e.Name) == lower ||
			strings.HasPrefix(strings.ToLower(e.Name), lower) ||
			strings.HasPrefix(strings.ToLower(filepath.Base(e.Repo)), lower) {
			fmt.Fprintf(stdout, "Matched catalog: %s → %s\n", first, e.Repo)
			return expandCatalogEntry(e, args[1:])
		}
	}

	return args
}

func expandCatalogEntry(e catalogEntry, extra []string) []string {
	args := []string{e.Repo, "--quant", e.Quant, "--alias", strings.ToLower(e.Name)}
	return append(args, extra...)
}

// ── Model catalog ─────────────────────────────────────────────────────────────

type catalogEntry struct {
	Name    string `json:"name"`
	Repo    string `json:"repo"`
	Quant   string `json:"quant"`
	SizeGB  string `json:"size_gb"`
	MinRAM  string `json:"min_ram_gb"`
	Tools   bool   `json:"tool_use"`
	Think   bool   `json:"reasoning"`
	Note    string `json:"note"`
}

// builtinCatalog is the hardcoded curated list of top developer-laptop models.
// Ordered by quality-per-byte for code and reasoning tasks.
// Run `/setup-model refresh` to fetch a live updated list from HuggingFace.
var builtinCatalog = []catalogEntry{
	{
		Name: "Hermes-3-Llama-3.1-8B", Repo: "NousResearch/Hermes-3-Llama-3.1-8B-GGUF",
		Quant: "Q4_K_M", SizeGB: "4.9", MinRAM: "8",
		Tools: true, Note: "★ Best for agentic/tool use. Native OpenAI tool_calls JSON — no translation needed.",
	},
	{
		Name: "Llama-3.1-8B", Repo: "unsloth/Meta-Llama-3.1-8B-Instruct-GGUF",
		Quant: "Q4_K_M", SizeGB: "4.9", MinRAM: "8",
		Tools: true, Note: "Native OpenAI tool_calls format. Solid for coding + file writes.",
	},
	{
		Name: "Qwen2.5-Coder-7B", Repo: "unsloth/Qwen2.5-Coder-7B-Instruct-GGUF",
		Quant: "Q4_K_M", SizeGB: "4.7", MinRAM: "6",
		Tools: true, Note: "Best 7B coder. XML tool format (milliways translates automatically).",
	},
	{
		Name: "Qwen2.5-Coder-14B", Repo: "unsloth/Qwen2.5-Coder-14B-Instruct-GGUF",
		Quant: "Q4_K_M", SizeGB: "8.9", MinRAM: "12",
		Tools: true, Note: "Best code quality under 15B. Requires 16GB RAM.",
	},
	{
		Name: "Qwen3-8B", Repo: "unsloth/Qwen3-8B-GGUF",
		Quant: "Q4_K_M", SizeGB: "5.2", MinRAM: "8",
		Tools: true, Think: true, Note: "Hybrid think/chat mode. Strong reasoning + tool use.",
	},
	{
		Name: "Qwen3-14B", Repo: "unsloth/Qwen3-14B-GGUF",
		Quant: "Q4_K_M", SizeGB: "9.3", MinRAM: "12",
		Tools: true, Think: true, Note: "Best all-round model under 20B for Apple Silicon.",
	},
	{
		Name: "DeepSeek-R1-7B", Repo: "unsloth/DeepSeek-R1-Distill-Qwen-7B-GGUF",
		Quant: "Q4_K_M", SizeGB: "4.7", MinRAM: "6",
		Think: true, Note: "R1 distill — strong chain-of-thought reasoning.",
	},
	{
		Name: "DeepSeek-Coder-V2-Lite", Repo: "unsloth/DeepSeek-Coder-V2-Lite-Instruct-GGUF",
		Quant: "Q4_K_M", SizeGB: "9.0", MinRAM: "12",
		Tools: true, Note: "Excellent for complex code refactors. MoE architecture.",
	},
	{
		Name: "Llama-3.1-8B", Repo: "unsloth/Meta-Llama-3.1-8B-Instruct-GGUF",
		Quant: "Q4_K_M", SizeGB: "4.9", MinRAM: "8",
		Tools: true, Note: "Good general-purpose. Solid tool use. Well-tested.",
	},
	{
		Name: "Mistral-7B-v0.3", Repo: "unsloth/mistral-7b-instruct-v0.3-GGUF",
		Quant: "Q4_K_M", SizeGB: "4.1", MinRAM: "6",
		Tools: true, Note: "Fast and light. Good for quick edits and completions.",
	},
	{
		Name: "Phi-3.5-mini", Repo: "unsloth/Phi-3.5-mini-instruct-GGUF",
		Quant: "Q4_K_M", SizeGB: "2.2", MinRAM: "4",
		Tools: true, Note: "Smallest capable model. Good on 8GB machines.",
	},
	{
		Name: "CodeLlama-13B", Repo: "TheBloke/CodeLlama-13B-Instruct-GGUF",
		Quant: "Q4_K_M", SizeGB: "7.9", MinRAM: "10",
		Note: "Specialised code completion. No structured tool use.",
	},
}

func catalogCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "milliways", "model-catalog.json"), nil
}

func loadCatalog() []catalogEntry {
	p, err := catalogCachePath()
	if err != nil {
		return builtinCatalog
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return builtinCatalog
	}
	var entries []catalogEntry
	if json.Unmarshal(data, &entries) == nil && len(entries) > 0 {
		return entries
	}
	return builtinCatalog
}

func runModelCatalogList(stdout io.Writer) int {
	entries := loadCatalog()
	fmt.Fprintln(stdout, "Top models for developer laptops (run /setup-model refresh to update from HuggingFace):")
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "  %-24s  %-6s  %-6s  %-5s  %-5s  %s\n", "Model", "Size", "RAM", "Tools", "Think", "Notes")
	fmt.Fprintf(stdout, "  %-24s  %-6s  %-6s  %-5s  %-5s  %s\n",
		strings.Repeat("─", 24), "──────", "──────", "─────", "─────", strings.Repeat("─", 30))
	for _, e := range entries {
		tools := " "
		if e.Tools {
			tools = "✓"
		}
		think := " "
		if e.Think {
			think = "✓"
		}
		fmt.Fprintf(stdout, "  %-24s  %5sGB  %5sGB  %-5s  %-5s  %s\n",
			e.Name, e.SizeGB, e.MinRAM, tools, think, e.Note)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Install: /setup-model <Repo> --quant Q4_K_M --alias <name>")
	fmt.Fprintln(stdout, "Example: /setup-model unsloth/Qwen3-8B-GGUF --quant Q4_K_M --alias qwen3-8b")
	return 0
}

func runModelCatalogRefresh(stdout, stderr io.Writer) int {
	fmt.Fprintln(stdout, "Fetching top GGUF models from HuggingFace...")
	url := "https://huggingface.co/api/models?filter=gguf&sort=downloads&direction=-1&limit=100&full=false"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url) //nolint:noctx
	if err != nil {
		fmt.Fprintf(stderr, "refresh: fetch failed: %v\n", err)
		fmt.Fprintf(stderr, "  Showing built-in catalog instead (may be behind a proxy).\n")
		return runModelCatalogList(stdout)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Fprintf(stderr, "refresh: HuggingFace API returned HTTP %d\n", resp.StatusCode)
		fmt.Fprintf(stderr, "  Showing built-in catalog instead.\n")
		return runModelCatalogList(stdout)
	}

	body, _ := io.ReadAll(resp.Body)
	var hfModels []struct {
		ID        string `json:"id"`
		Downloads int    `json:"downloads"`
	}
	if err := json.Unmarshal(body, &hfModels); err != nil {
		fmt.Fprintf(stderr, "refresh: parse error: %v\n", err)
		return runModelCatalogList(stdout)
	}

	// Build a refreshed catalog from the HF response, keeping only models
	// that look like GGUF instruction-tuned models with known quants.
	seen := map[string]bool{}
	var refreshed []catalogEntry
	for _, m := range hfModels {
		if len(refreshed) >= 10 {
			break
		}
		name := m.ID
		if seen[name] {
			continue
		}
		// Only include -Instruct or -Chat models (not base weights).
		lower := strings.ToLower(name)
		if !strings.Contains(lower, "instruct") && !strings.Contains(lower, "chat") &&
			!strings.Contains(lower, "coder") && !strings.Contains(lower, "r1") {
			continue
		}
		seen[name] = true
		refreshed = append(refreshed, catalogEntry{
			Name:  filepath.Base(strings.TrimSuffix(name, "-GGUF")),
			Repo:  name,
			Quant: "Q4_K_M",
			Note:  fmt.Sprintf("HuggingFace top download #%d", len(refreshed)+1),
		})
	}

	if len(refreshed) == 0 {
		fmt.Fprintln(stderr, "refresh: no suitable models found in API response — using built-in catalog")
		return runModelCatalogList(stdout)
	}

	// Save to cache.
	if p, err := catalogCachePath(); err == nil {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if data, err := json.Marshal(refreshed); err == nil {
			_ = os.WriteFile(p, data, 0o644)
			fmt.Fprintf(stdout, "Catalog updated: %s\n\n", p)
		}
	}

	// Print the refreshed catalog.
	fmt.Fprintf(stdout, "  %-40s  %s\n", "Model (HuggingFace ID)", "Downloads")
	fmt.Fprintf(stdout, "  %-40s  %s\n", strings.Repeat("─", 40), strings.Repeat("─", 10))
	for i, e := range refreshed {
		fmt.Fprintf(stdout, "  %-40s  #%d\n", e.Repo, i+1)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Install: /setup-model <Repo> --quant Q4_K_M --alias <name>")
	return 0
}

// insertSwapModelEntry adds (or refreshes) one model entry in a llama-swap
// YAML config. Returns the (possibly unchanged) bytes, a `changed` flag,
// and any error. Idempotent: re-running with the same alias+path yields
// the same output and changed=false.
func insertSwapModelEntry(yamlBytes []byte, alias, ggufPath string) ([]byte, bool, error) {
	var root map[string]any
	if err := yaml.Unmarshal(yamlBytes, &root); err != nil {
		return nil, false, err
	}
	if root == nil {
		root = map[string]any{}
	}
	models, _ := root["models"].(map[string]any)
	if models == nil {
		models = map[string]any{}
	}
	desiredCmd := fmt.Sprintf("llama-server -m %s --port ${PORT}", ggufPath)
	if existing, ok := models[alias].(map[string]any); ok {
		if cmd, _ := existing["cmd"].(string); cmd == desiredCmd {
			return yamlBytes, false, nil
		}
	}
	models[alias] = map[string]any{
		"cmd": desiredCmd,
	}
	root["models"] = models
	out, err := yaml.Marshal(root)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// ── server maintenance verbs ───────────────────────────────────────────────────

// macosPlistPath returns the standard path for the macOS launchd plist.
func macosPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", "dev.milliways.local.plist"), nil
}

// linuxServicePath returns the standard path for the systemd user unit.
func linuxServicePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user", "dev.milliways.local.service"), nil
}

// parsePortFromEndpoint extracts the port number from a URL like
// http://127.0.0.1:8765/v1. Returns "" when the port cannot be found.
func parsePortFromEndpoint(endpoint string) string {
	// Strip scheme.
	s := endpoint
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// Isolate host:port from path.
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	// Extract port from host:port.
	if i := strings.LastIndex(s, ":"); i >= 0 {
		port := s[i+1:]
		if _, err := strconv.Atoi(port); err == nil {
			return port
		}
	}
	return ""
}

// runLocalServerStart starts the local inference server via launchctl (macOS),
// systemctl --user (Linux), or by exec-ing the launcher directly as a fallback.
func runLocalServerStart(_ []string, stdout, stderr io.Writer) int {
	endpoint := localEndpoint()

	// macOS: launchctl load
	if plist, err := macosPlistPath(); err == nil {
		if _, err2 := os.Stat(plist); err2 == nil {
			cmd := execCommand("launchctl", "load", "-w", plist)
			cmd.Stdout = stdout
			cmd.Stderr = stderr
			if err3 := cmd.Run(); err3 == nil {
				fmt.Fprintf(stdout, "[ok] local server started on %s\n", endpoint)
				return 0
			}
		}
	}

	// Linux: systemctl --user start
	if svc, err := linuxServicePath(); err == nil {
		if _, err2 := os.Stat(svc); err2 == nil {
			cmd := execCommand("systemctl", "--user", "start", "dev.milliways.local")
			cmd.Stdout = stdout
			cmd.Stderr = stderr
			if err3 := cmd.Run(); err3 == nil {
				fmt.Fprintf(stdout, "[ok] local server started on %s\n", endpoint)
				return 0
			}
		}
	}

	// Fallback: run milliways-local-server in background.
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "server-start: home dir: %v\n", err)
		return 1
	}
	launcher := filepath.Join(home, ".local", "bin", "milliways-local-server")
	if _, err2 := os.Stat(launcher); err2 != nil {
		fmt.Fprintf(stderr, "server-start: launcher not found at %s; run `milliwaysctl local install-server` first\n", launcher)
		return 1
	}
	cmd := execCommand(launcher)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err3 := cmd.Start(); err3 != nil {
		fmt.Fprintf(stderr, "server-start: %v\n", err3)
		return 1
	}
	fmt.Fprintf(stdout, "[ok] local server started on %s\n", endpoint)
	return 0
}

// runLocalServerStop stops the local inference server via launchctl (macOS),
// systemctl --user (Linux), or pkill as a last resort.
func runLocalServerStop(_ []string, stdout, stderr io.Writer) int {
	stopped := false

	// macOS: launchctl unload
	if plist, err := macosPlistPath(); err == nil {
		if _, err2 := os.Stat(plist); err2 == nil {
			cmd := execCommand("launchctl", "unload", plist)
			cmd.Stdout = stdout
			cmd.Stderr = stderr
			if err3 := cmd.Run(); err3 == nil {
				stopped = true
			}
		}
	}

	// Linux: systemctl --user stop
	if !stopped {
		if svc, err := linuxServicePath(); err == nil {
			if _, err2 := os.Stat(svc); err2 == nil {
				cmd := execCommand("systemctl", "--user", "stop", "dev.milliways.local")
				cmd.Stdout = stdout
				cmd.Stderr = stderr
				if err3 := cmd.Run(); err3 == nil {
					stopped = true
				}
			}
		}
	}

	// Fallback: pkill
	if !stopped {
		cmd := execCommand("pkill", "-f", "milliways-local-server")
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		// pkill exits 1 when no processes were found — that's acceptable.
		_ = cmd.Run()
	}

	fmt.Fprintln(stdout, "[ok] local server stopped")
	return 0
}

// runLocalServerStatus checks whether the local inference server is reachable,
// and if so lists the loaded models. Exits 0 when running, 1 when stopped.
func runLocalServerStatus(_ []string, stdout, stderr io.Writer) int {
	endpoint := strings.TrimRight(localEndpoint(), "/")
	url := endpoint + "/models"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintf(stderr, "server-status: build request: %v\n", err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(stdout, "status:   not running\n")
		fmt.Fprintf(stdout, "endpoint: %s\n", endpoint)
		fmt.Fprintf(stdout, "reason:   unreachable (%v)\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stdout, "status:   not running\n")
		fmt.Fprintf(stdout, "endpoint: %s\n", endpoint)
		fmt.Fprintf(stdout, "reason:   HTTP %d\n", resp.StatusCode)
		return 1
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		fmt.Fprintf(stderr, "server-status: read body: %v\n", err)
		return 1
	}
	models, err := parseListModelsResponse(body)
	if err != nil {
		fmt.Fprintf(stderr, "server-status: parse models: %v\n", err)
		return 1
	}

	port := parsePortFromEndpoint(endpoint)
	fmt.Fprintf(stdout, "status:   running\n")
	fmt.Fprintf(stdout, "endpoint: %s\n", endpoint)
	if port != "" {
		fmt.Fprintf(stdout, "port:     %s\n", port)
	}
	if len(models) > 0 {
		fmt.Fprintf(stdout, "models:\n")
		for _, m := range models {
			fmt.Fprintf(stdout, "  - %s\n", m)
		}
	}
	return 0
}

// runLocalServerPort prints the port number parsed from MILLIWAYS_LOCAL_ENDPOINT
// (or the default endpoint). Useful for shell scripts: PORT=$(milliwaysctl local server-port).
func runLocalServerPort(_ []string, stdout, stderr io.Writer) int {
	endpoint := localEndpoint()
	port := parsePortFromEndpoint(endpoint)
	if port == "" {
		fmt.Fprintf(stderr, "server-port: cannot parse port from endpoint %q\n", endpoint)
		return 1
	}
	fmt.Fprintln(stdout, port)
	return 0
}

// runLocalServerUninstall stops the server, removes service/plist files, the
// launcher binary, and the MILLIWAYS_LOCAL_ENDPOINT key from local.env.
func runLocalServerUninstall(args []string, stdout, stderr io.Writer) int {
	yes := hasFlag(args, "--yes")
	if !yes {
		fmt.Fprintln(stderr, "server-uninstall: this will remove the local server service files and launcher.")
		fmt.Fprintln(stderr, "  Pass --yes to confirm, or run `milliwaysctl local server-stop` to only stop it.")
		return 2
	}

	// Stop first (best-effort).
	_ = runLocalServerStop(nil, stdout, stderr)

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "server-uninstall: home dir: %v\n", err)
		return 1
	}

	removed := []string{}

	// macOS plist.
	if plist, err2 := macosPlistPath(); err2 == nil {
		if err3 := os.Remove(plist); err3 == nil {
			removed = append(removed, plist)
		}
	}

	// Linux systemd unit.
	if svc, err2 := linuxServicePath(); err2 == nil {
		if err3 := os.Remove(svc); err3 == nil {
			removed = append(removed, svc)
		}
	}

	// Launcher binary.
	launcher := filepath.Join(home, ".local", "bin", "milliways-local-server")
	if err2 := os.Remove(launcher); err2 == nil {
		removed = append(removed, launcher)
	}

	// Remove MILLIWAYS_LOCAL_ENDPOINT from local.env.
	envPath, err := configPath("local.env")
	if err == nil {
		if data, err2 := os.ReadFile(envPath); err2 == nil {
			var kept []string
			for _, line := range strings.Split(string(data), "\n") {
				if !strings.HasPrefix(line, "MILLIWAYS_LOCAL_ENDPOINT=") {
					kept = append(kept, line)
				}
			}
			_ = os.WriteFile(envPath, []byte(strings.Join(kept, "\n")), 0o644)
		}
	}

	for _, p := range removed {
		fmt.Fprintf(stdout, "removed: %s\n", p)
	}
	if len(removed) == 0 {
		fmt.Fprintln(stdout, "nothing to remove (service files and launcher not found)")
	}
	return 0
}

// runLocalDefaultModel updates the launcher script and local.env to use the
// model matching alias in llama-swap.yaml, then prints confirmation.
func runLocalDefaultModel(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "local default-model: <alias> required")
		return 2
	}
	alias := args[0]

	// Locate the model path from llama-swap.yaml.
	cfgPath, err := configPath("llama-swap.yaml")
	if err != nil {
		fmt.Fprintf(stderr, "default-model: %v\n", err)
		return 1
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		fmt.Fprintf(stderr, "default-model: read %s: %v\n  run `milliwaysctl local setup-model` to register models first\n", cfgPath, err)
		return 1
	}

	var swapCfg struct {
		Models map[string]struct {
			Cmd string `yaml:"cmd"`
		} `yaml:"models"`
	}
	if err := yaml.Unmarshal(data, &swapCfg); err != nil {
		fmt.Fprintf(stderr, "default-model: parse %s: %v\n", cfgPath, err)
		return 1
	}

	entry, ok := swapCfg.Models[alias]
	if !ok {
		fmt.Fprintf(stderr, "default-model: alias %q not found in %s\n  available: ", alias, cfgPath)
		for k := range swapCfg.Models {
			fmt.Fprintf(stderr, "%s ", k)
		}
		fmt.Fprintln(stderr)
		return 1
	}

	// Extract model path from the cmd field: "llama-server -m <path> --port ${PORT}"
	modelPath := ""
	parts := strings.Fields(entry.Cmd)
	for i, p := range parts {
		if p == "-m" && i+1 < len(parts) {
			modelPath = parts[i+1]
			break
		}
	}
	if modelPath == "" {
		fmt.Fprintf(stderr, "default-model: cannot extract -m path from cmd %q\n", entry.Cmd)
		return 1
	}

	if err := updateLocalServerLauncher(modelPath, alias, stderr); err != nil {
		fmt.Fprintf(stderr, "default-model: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "[ok] default model set to %s\n", alias)
	return 0
}

// configPath returns the absolute path under $XDG_CONFIG_HOME (or
// $HOME/.config) for milliways config files.
func configPath(name string) (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "milliways", name), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config path: %w", err)
	}
	return filepath.Join(home, ".config", "milliways", name), nil
}
