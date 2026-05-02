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
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
	fmt.Fprintln(w, "  setup-model    <repo> [--quant Q] [--alias A]   download + register in llama-swap")
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
		if info, err := os.Stat(dest); err == nil && info.Size() > 0 {
			fmt.Fprintf(stdout, "%s (cached, %d bytes; pass --force to redownload)\n", dest, info.Size())
			return 0
		}
	}
	url := buildHFGGUFURL(repo, quant)
	cmd := execCommand("curl", "-fL", "-o", dest, url)
	cmd.Stdout = stderr // curl progress goes to stderr in our convention
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			fmt.Fprintf(stderr, "local download-model: curl exit %d for %s\n", ee.ExitCode(), url)
			return ee.ExitCode()
		}
		fmt.Fprintf(stderr, "local download-model: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, dest)
	return 0
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

	// Step 3: verification is best-effort — backend may not be running yet.
	// We probe but don't fail setup-model on a cold backend.
	probeCode := runLocalListModels(nil, io.Discard, io.Discard)
	if probeCode == 0 {
		fmt.Fprintln(stdout, "backend reachable; model is ready to use")
	} else {
		fmt.Fprintln(stdout, "backend not yet reachable — start it with `milliwaysctl local install-server` if not already running")
	}
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
