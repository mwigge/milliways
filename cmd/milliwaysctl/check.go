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

// `milliwaysctl check` — first-run health check.
//
// Prints a formatted table of [PASS] / [WARN] / [FAIL] items covering:
//   - required binaries on PATH
//   - daemon reachability (--version probe)
//   - Python venv presence and import checks
//   - CodeGraph binary
//   - Agent toolkit
//   - API key presence (masked: "set" / "not set")
//   - Local inference server reachability (if configured)
//   - OTel endpoint URL validity (if configured)
//
// Exit code: 0 if all items are PASS or WARN; 1 if any item is FAIL.
// The check runs without a live daemon (no --socket needed).

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// checkStatus is the outcome of a single health-check item.
type checkStatus int

const (
	statusPass checkStatus = iota
	statusWarn
	statusFail
)

func (s checkStatus) String() string {
	switch s {
	case statusPass:
		return "PASS"
	case statusWarn:
		return "WARN"
	default:
		return "FAIL"
	}
}

// checkItem is a single row in the health check table.
type checkItem struct {
	label  string
	status checkStatus
	detail string
}

// runCheck performs the first-run health check and writes a formatted table
// to stdout. It returns 0 if no item is [FAIL] and 1 otherwise.
func runCheck(_ []string, stdout, _ io.Writer) int {
	items := collectCheckItems()

	fmt.Fprintln(stdout, "milliwaysctl check — milliways installation health")
	fmt.Fprintln(stdout)
	for _, item := range items {
		fmt.Fprintf(stdout, "  [%s] %-32s %s\n", item.status, item.label, item.detail)
	}
	fmt.Fprintln(stdout)

	for _, item := range items {
		if item.status == statusFail {
			return 1
		}
	}
	return 0
}

// collectCheckItems runs all individual checks and returns the results in
// display order.
func collectCheckItems() []checkItem {
	var items []checkItem

	// 1. Binaries — milliways, milliwaysd, milliwaysctl
	items = append(items, checkBinary("milliways binary", "milliways"))
	items = append(items, checkBinary("milliwaysd binary", "milliwaysd"))
	items = append(items, checkBinary("milliwaysctl binary", "milliwaysctl"))

	// 2. Daemon — milliwaysd --version exits 0
	items = append(items, checkDaemon())

	// 3. Python venv
	venvPython, venvItem := checkPythonVenv()
	items = append(items, venvItem)

	// 4. MemPalace import
	items = append(items, checkPythonImport(venvPython, "MemPalace importable", "mempalace"))

	// 5. python-pptx import
	items = append(items, checkPythonImport(venvPython, "python-pptx importable", "pptx"))

	// 6. CodeGraph binary
	items = append(items, checkCodeGraph())

	// 7. Agent toolkit
	items = append(items, checkAgentToolkit())

	// 8. API keys
	for _, key := range []string{
		"ANTHROPIC_API_KEY",
		"GEMINI_API_KEY",
		"GOOGLE_API_KEY",
		"OPENAI_API_KEY",
		"MINIMAX_API_KEY",
		"CODES_API_KEY",
	} {
		items = append(items, checkAPIKey(key))
	}

	// 9. Local server
	items = append(items, checkLocalServer())

	// 10. OTel endpoint
	items = append(items, checkOtelEndpoint())

	return items
}

// checkBinary looks up a binary on PATH and returns a checkItem.
func checkBinary(label, name string) checkItem {
	path, err := exec.LookPath(name)
	if err != nil {
		return checkItem{
			label:  label,
			status: statusFail,
			detail: "not found on PATH",
		}
	}
	return checkItem{
		label:  label,
		status: statusPass,
		detail: path,
	}
}

// checkDaemon runs milliwaysd --version and reports the result.
func checkDaemon() checkItem {
	const label = "Daemon (milliwaysd)"
	path, err := exec.LookPath("milliwaysd")
	if err != nil {
		return checkItem{label: label, status: statusWarn, detail: "milliwaysd not on PATH — skip version probe"}
	}
	out, err := exec.Command(path, "--version").CombinedOutput() //nolint:gosec // path comes from LookPath
	if err != nil {
		return checkItem{label: label, status: statusFail, detail: fmt.Sprintf("--version failed: %v", err)}
	}
	detail := strings.TrimSpace(string(out))
	if detail == "" {
		detail = "exit 0"
	}
	return checkItem{label: label, status: statusPass, detail: detail}
}

// venvCandidates returns the list of Python venv paths to probe, in
// preference order.
func venvCandidates() []string {
	candidates := []string{
		filepath.Join(xdgDataHome(), "milliways", "python", "bin", "python"),
		"/usr/share/milliways/python/bin/python",
	}
	if v := os.Getenv("MILLIWAYS_MEMPALACE_MCP_CMD"); v != "" {
		candidates = append([]string{v}, candidates...)
	}
	return candidates
}

// xdgDataHome returns $XDG_DATA_HOME or the default ~/.local/share.
func xdgDataHome() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return x
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

// checkPythonVenv finds the first usable venv Python and returns both the
// path (empty string if not found) and the check item for display.
func checkPythonVenv() (string, checkItem) {
	const label = "Python venv"
	for _, candidate := range venvCandidates() {
		if isExecutable(candidate) {
			return candidate, checkItem{label: label, status: statusPass, detail: candidate}
		}
	}
	return "", checkItem{
		label:  label,
		status: statusWarn,
		detail: "not found — run install_feature_deps.sh",
	}
}

// checkPythonImport runs `python -c "import <module>"` in the venv and
// returns the result.
func checkPythonImport(pythonBin, label, module string) checkItem {
	if pythonBin == "" {
		return checkItem{label: label, status: statusWarn, detail: "Python venv not available"}
	}
	cmd := exec.Command(pythonBin, "-c", "import "+module) //nolint:gosec // pythonBin is from an allowlist
	if err := cmd.Run(); err != nil {
		return checkItem{
			label:  label,
			status: statusWarn,
			detail: fmt.Sprintf("import %s failed — run install_feature_deps.sh", module),
		}
	}
	return checkItem{label: label, status: statusPass, detail: "importable"}
}

// checkCodeGraph looks for the codegraph binary using env var overrides, the
// known share directories, and PATH (in that order).
func checkCodeGraph() checkItem {
	const label = "CodeGraph binary"

	// 1. Honour the configured MCP command if set.
	if v := os.Getenv("MILLIWAYS_CODEGRAPH_MCP_CMD"); v != "" {
		if isExecutable(v) {
			return checkItem{label: label, status: statusPass, detail: v}
		}
	}

	// 2. Well-known installation paths.
	shareDirs := []string{
		filepath.Join(xdgDataHome(), "milliways", "node", "bin"),
		"/usr/share/milliways/node/bin",
		"/usr/local/share/milliways/node/bin",
	}
	for _, dir := range shareDirs {
		p := filepath.Join(dir, "codegraph")
		if isExecutable(p) {
			return checkItem{label: label, status: statusPass, detail: p}
		}
	}

	// 3. PATH.
	if path, err := exec.LookPath("codegraph"); err == nil {
		return checkItem{label: label, status: statusPass, detail: path}
	}

	return checkItem{
		label:  label,
		status: statusWarn,
		detail: "not installed — run install_feature_deps.sh",
	}
}

// checkAgentToolkit looks for skill-rules.json in the configured agent dir or
// well-known share directories.
func checkAgentToolkit() checkItem {
	const label = "Agent toolkit"

	dirs := []string{}
	if v := os.Getenv("MILLIWAYS_AGENTS_DIR"); v != "" {
		dirs = append(dirs, v)
	}
	dirs = append(dirs,
		filepath.Join(xdgDataHome(), "milliways", "agent-toolkit"),
		"/usr/share/milliways/agent-toolkit",
		"/usr/local/share/milliways/agent-toolkit",
	)
	for _, dir := range dirs {
		p := filepath.Join(dir, "skill-rules.json")
		if fileExists(p) {
			return checkItem{label: label, status: statusPass, detail: dir}
		}
	}
	return checkItem{
		label:  label,
		status: statusWarn,
		detail: "not installed — run install_feature_deps.sh",
	}
}

// checkAPIKey returns a check item describing whether the named env var is set.
func checkAPIKey(name string) checkItem {
	if os.Getenv(name) != "" {
		return checkItem{label: name, status: statusPass, detail: "set"}
	}
	return checkItem{label: name, status: statusWarn, detail: "not set"}
}

// checkLocalServer probes the local inference server endpoint if configured.
func checkLocalServer() checkItem {
	const label = "Local server"
	endpoint := os.Getenv("MILLIWAYS_LOCAL_ENDPOINT")
	if endpoint == "" {
		return checkItem{
			label:  label,
			status: statusWarn,
			detail: "MILLIWAYS_LOCAL_ENDPOINT not configured",
		}
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(endpoint) //nolint:gosec // endpoint is user-supplied config
	if err != nil {
		return checkItem{
			label:  label,
			status: statusWarn,
			detail: fmt.Sprintf("%s — not reachable (%v)", endpoint, err),
		}
	}
	resp.Body.Close()
	return checkItem{
		label:  label,
		status: statusPass,
		detail: fmt.Sprintf("%s — reachable (HTTP %d)", endpoint, resp.StatusCode),
	}
}

// checkOtelEndpoint validates the OTel endpoint URL if configured.
func checkOtelEndpoint() checkItem {
	const label = "OTel endpoint"
	endpoint := os.Getenv("MILLIWAYS_OTEL_ENDPOINT")
	if endpoint == "" {
		return checkItem{
			label:  label,
			status: statusWarn,
			detail: "MILLIWAYS_OTEL_ENDPOINT not configured",
		}
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return checkItem{
			label:  label,
			status: statusWarn,
			detail: fmt.Sprintf("%q is not a valid URL", endpoint),
		}
	}
	return checkItem{
		label:  label,
		status: statusPass,
		detail: endpoint,
	}
}

// isExecutable returns true if path exists and is an executable file.
func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

// fileExists returns true if path exists (file or directory).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
