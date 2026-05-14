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

// Package shims describes command shims and resolves the underlying binaries
// they broker to milliwaysd.
package shims

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Category groups shimmed commands by policy surface.
type Category string

const (
	CategoryShell          Category = "shell"
	CategoryPackageManager Category = "package-manager"
	CategoryBuildTool      Category = "build-tool"
	CategoryNetwork        Category = "network"
	CategoryVCS            Category = "vcs"
	CategoryPersistence    Category = "persistence"
)

// Metadata describes one command shim that can be installed into a shim
// directory.
type Metadata struct {
	Name        string   `json:"name"`
	Category    Category `json:"category"`
	Description string   `json:"description"`
}

const (
	// EnvActive marks a milliwaysctl shim-exec invocation launched by a
	// generated MilliWays command shim.
	EnvActive = "MILLIWAYS_SECURITY_SHIM_ACTIVE"
	// EnvCommand is the catalog command name the shim represents.
	EnvCommand = "MILLIWAYS_SECURITY_SHIM_COMMAND"
	// EnvCategory is the policy category from the shim catalog.
	EnvCategory = "MILLIWAYS_SECURITY_SHIM_CATEGORY"
	// EnvShimDir is the directory containing generated shims.
	EnvShimDir = "MILLIWAYS_SECURITY_SHIM_DIR"
	// EnvResolvedPath is the real executable resolved outside the shim dir.
	EnvResolvedPath = "MILLIWAYS_SECURITY_SHIM_RESOLVED"
	// EnvOriginalPath preserves the caller's PATH before the shim removed
	// itself for lookup and downstream execution.
	EnvOriginalPath = "MILLIWAYS_SECURITY_SHIM_ORIGINAL_PATH"
	// EnvBroker is a legacy broker override name. Generated shims intentionally
	// do not trust this caller-controlled environment variable.
	EnvBroker = "MILLIWAYS_SECURITY_SHIM_BROKER"
)

const (
	defaultBrokerCommand = "milliwaysctl"
)

// DefaultCatalog is the command broker surface covered by generated shims.
var DefaultCatalog = []Metadata{
	{Name: "bash", Category: CategoryShell, Description: "Bourne Again shell"},
	{Name: "sh", Category: CategoryShell, Description: "POSIX shell"},
	{Name: "zsh", Category: CategoryShell, Description: "Z shell"},
	{Name: "npm", Category: CategoryPackageManager, Description: "Node package manager"},
	{Name: "pnpm", Category: CategoryPackageManager, Description: "Performant Node package manager"},
	{Name: "yarn", Category: CategoryPackageManager, Description: "Yarn package manager"},
	{Name: "bun", Category: CategoryPackageManager, Description: "Bun JavaScript runtime and package manager"},
	{Name: "pip", Category: CategoryPackageManager, Description: "Python package installer"},
	{Name: "uv", Category: CategoryPackageManager, Description: "Python package and project manager"},
	{Name: "poetry", Category: CategoryPackageManager, Description: "Python dependency manager"},
	{Name: "go", Category: CategoryBuildTool, Description: "Go toolchain"},
	{Name: "cargo", Category: CategoryBuildTool, Description: "Rust package manager and build tool"},
	{Name: "curl", Category: CategoryNetwork, Description: "Network transfer client"},
	{Name: "wget", Category: CategoryNetwork, Description: "Network transfer client"},
	{Name: "git", Category: CategoryVCS, Description: "Git version control"},
	{Name: "systemctl", Category: CategoryPersistence, Description: "systemd service manager"},
	{Name: "launchctl", Category: CategoryPersistence, Description: "launchd service manager"},
	{Name: "crontab", Category: CategoryPersistence, Description: "cron table editor"},
}

// Names returns the catalog command names in catalog order.
func Names(catalog []Metadata) []string {
	out := make([]string, 0, len(catalog))
	for _, shim := range catalog {
		out = append(out, shim.Name)
	}
	return out
}

// ValidateCatalog reports duplicate or empty shim metadata entries.
func ValidateCatalog(catalog []Metadata) error {
	seen := make(map[string]struct{}, len(catalog))
	for _, shim := range catalog {
		name := strings.TrimSpace(shim.Name)
		if name == "" {
			return errors.New("shim catalog contains an empty command name")
		}
		if shim.Category == "" {
			return fmt.Errorf("shim %q has no category", name)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("shim catalog contains duplicate command %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// PrependPath returns path with shimDir first and any existing occurrences of
// shimDir removed.
func PrependPath(path, shimDir string) string {
	shimDir = strings.TrimSpace(shimDir)
	if shimDir == "" {
		return path
	}
	parts := []string{shimDir}
	normalizedShim := comparableDir(shimDir)
	for _, dir := range filepath.SplitList(path) {
		if dir == "" || comparableDir(dir) == normalizedShim {
			continue
		}
		parts = append(parts, dir)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

// ActivationEnv returns environment entries that make shimDir first on PATH.
func ActivationEnv(path, shimDir string) map[string]string {
	return map[string]string{
		"PATH": PrependPath(path, shimDir),
	}
}

// InstallOptions controls command shim generation and installation.
type InstallOptions struct {
	Dir           string
	Catalog       []Metadata
	BrokerCommand string
	BrokerArgs    []string
}

// InstallResult describes an installed shim catalog.
type InstallResult struct {
	Dir      string
	Paths    []string
	Env      map[string]string
	Replaced int
}

// StatusOptions controls command shim readiness inspection.
type StatusOptions struct {
	Dir           string
	Catalog       []Metadata
	BrokerCommand string
	Path          string
}

// StatusIssue describes one problem that prevents full shim readiness.
type StatusIssue struct {
	Kind    string `json:"kind"`
	Command string `json:"command,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// StatusResult reports whether a shim directory is ready for first-start and
// client-switch command brokering.
type StatusResult struct {
	Dir              string            `json:"dir"`
	Expected         int               `json:"expected"`
	Installed        int               `json:"installed"`
	Ready            bool              `json:"ready"`
	Protected        bool              `json:"protected"`
	BrokerCommand    string            `json:"broker_command"`
	BrokerPath       string            `json:"broker_path,omitempty"`
	BrokerInstalled  bool              `json:"broker_installed"`
	MissingShims     []string          `json:"missing_shims,omitempty"`
	MissingRealTools []string          `json:"missing_real_tools,omitempty"`
	Issues           []StatusIssue     `json:"issues,omitempty"`
	ActivationEnv    map[string]string `json:"activation_env,omitempty"`
}

// InstallDefaultCatalog installs generated command shims for DefaultCatalog.
func InstallDefaultCatalog(dir string) (InstallResult, error) {
	return InstallCatalog(InstallOptions{Dir: dir, Catalog: DefaultCatalog})
}

// StatusDefaultCatalog inspects readiness for DefaultCatalog in dir.
func StatusDefaultCatalog(dir string) (StatusResult, error) {
	return StatusCatalog(StatusOptions{Dir: dir, Catalog: DefaultCatalog})
}

// InstallCatalog creates or updates executable POSIX shell shims for a catalog.
// Existing matching files are left untouched, making repeated installs
// idempotent while still correcting mode bits.
func InstallCatalog(opts InstallOptions) (InstallResult, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return InstallResult{}, fmt.Errorf("install command shims: unsupported OS %q", runtime.GOOS)
	}
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		return InstallResult{}, errors.New("install command shims: dir is required")
	}
	catalog := opts.Catalog
	if catalog == nil {
		catalog = DefaultCatalog
	}
	if err := ValidateCatalog(catalog); err != nil {
		return InstallResult{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("install command shims: mkdir %q: %w", dir, err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return InstallResult{}, fmt.Errorf("install command shims: abs dir: %w", err)
	}
	absDir = filepath.Clean(absDir)

	result := InstallResult{
		Dir:   absDir,
		Paths: make([]string, 0, len(catalog)),
		Env:   ActivationEnv(os.Getenv("PATH"), absDir),
	}
	for _, meta := range catalog {
		content, err := GenerateScript(meta, GenerateOptions{
			ShimDir:       absDir,
			BrokerCommand: opts.BrokerCommand,
			BrokerArgs:    opts.BrokerArgs,
		})
		if err != nil {
			return InstallResult{}, err
		}
		path := filepath.Join(absDir, meta.Name)
		replaced, err := writeExecutableIfChanged(path, []byte(content))
		if err != nil {
			return InstallResult{}, err
		}
		if replaced {
			result.Replaced++
		}
		result.Paths = append(result.Paths, path)
	}
	return result, nil
}

// StatusCatalog inspects installed shim files, broker availability, and real
// tool availability outside the shim directory.
func StatusCatalog(opts StatusOptions) (StatusResult, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return StatusResult{}, fmt.Errorf("inspect command shims: unsupported OS %q", runtime.GOOS)
	}
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		return StatusResult{}, errors.New("inspect command shims: dir is required")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return StatusResult{}, fmt.Errorf("inspect command shims: abs dir: %w", err)
	}
	absDir = filepath.Clean(absDir)
	catalog := opts.Catalog
	if catalog == nil {
		catalog = DefaultCatalog
	}
	if err := ValidateCatalog(catalog); err != nil {
		return StatusResult{}, err
	}
	path := opts.Path
	if path == "" {
		path = os.Getenv("PATH")
	}
	brokerCommand := strings.TrimSpace(opts.BrokerCommand)
	if brokerCommand == "" {
		brokerCommand = defaultBrokerCommand
	}
	result := StatusResult{
		Dir:              absDir,
		Expected:         len(catalog),
		BrokerCommand:    brokerCommand,
		ActivationEnv:    ActivationEnv(path, absDir),
		MissingShims:     []string{},
		MissingRealTools: []string{},
		Issues:           []StatusIssue{},
	}
	for _, meta := range catalog {
		if isExecutable(filepath.Join(absDir, meta.Name)) {
			result.Installed++
		} else {
			result.MissingShims = append(result.MissingShims, meta.Name)
			result.Issues = append(result.Issues, StatusIssue{Kind: "missing-shim", Command: meta.Name})
		}
		if _, err := ResolveRealBinary(ResolveOptions{Command: meta.Name, Path: path, ShimDir: absDir}); err != nil {
			result.MissingRealTools = append(result.MissingRealTools, meta.Name)
			result.Issues = append(result.Issues, StatusIssue{Kind: "missing-real-tool", Command: meta.Name, Detail: err.Error()})
		}
	}
	if brokerPath := lookPathIn(brokerCommand, path); brokerPath != "" {
		result.BrokerInstalled = true
		result.BrokerPath = brokerPath
	} else {
		result.Issues = append(result.Issues, StatusIssue{Kind: "missing-broker", Command: brokerCommand})
	}
	result.Protected = result.Installed == result.Expected && result.BrokerInstalled
	result.Ready = result.Protected
	return result, nil
}

// GenerateOptions controls shell script generation for one shim.
type GenerateOptions struct {
	ShimDir       string
	BrokerCommand string
	BrokerArgs    []string
}

// GenerateScript returns a deterministic POSIX shell shim for one catalog entry.
func GenerateScript(meta Metadata, opts GenerateOptions) (string, error) {
	if err := ValidateCatalog([]Metadata{meta}); err != nil {
		return "", err
	}
	shimDir := strings.TrimSpace(opts.ShimDir)
	if shimDir == "" {
		return "", errors.New("generate command shim: shim dir is required")
	}
	absShimDir, err := filepath.Abs(shimDir)
	if err != nil {
		return "", fmt.Errorf("generate command shim: abs shim dir: %w", err)
	}
	brokerCommand := strings.TrimSpace(opts.BrokerCommand)
	if brokerCommand == "" {
		brokerCommand = defaultBrokerCommand
	}
	brokerArgs := opts.BrokerArgs
	if len(brokerArgs) == 0 {
		brokerArgs = []string{"security", "shim-exec"}
	}

	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Generated by MilliWays security command shims. Do not edit.\n")
	b.WriteString("set -eu\n")
	fmt.Fprintf(&b, "mw_shim_command=%s\n", shellQuote(meta.Name))
	fmt.Fprintf(&b, "mw_shim_category=%s\n", shellQuote(string(meta.Category)))
	fmt.Fprintf(&b, "mw_shim_dir=%s\n", shellQuote(filepath.Clean(absShimDir)))
	b.WriteString("mw_original_path=${PATH:-}\n")
	b.WriteString("mw_path_without_shim=\n")
	b.WriteString("IFS=:\n")
	b.WriteString("for mw_dir in ${PATH:-}; do\n")
	b.WriteString("\t[ -n \"$mw_dir\" ] || continue\n")
	b.WriteString("\tcase \"$mw_dir\" in \"$mw_shim_dir\"|\"$mw_shim_dir/\"|\"$mw_shim_dir/.\") continue ;; esac\n")
	b.WriteString("\tif [ -z \"$mw_path_without_shim\" ]; then mw_path_without_shim=$mw_dir; else mw_path_without_shim=$mw_path_without_shim:$mw_dir; fi\n")
	b.WriteString("done\n")
	b.WriteString("unset IFS\n")
	b.WriteString("mw_resolved=\n")
	b.WriteString("IFS=:\n")
	b.WriteString("for mw_dir in $mw_path_without_shim; do\n")
	b.WriteString("\t[ -n \"$mw_dir\" ] || continue\n")
	b.WriteString("\tif [ -x \"$mw_dir/$mw_shim_command\" ] && [ ! -d \"$mw_dir/$mw_shim_command\" ]; then mw_resolved=$mw_dir/$mw_shim_command; break; fi\n")
	b.WriteString("done\n")
	b.WriteString("unset IFS\n")
	b.WriteString("if [ -z \"$mw_resolved\" ]; then\n")
	b.WriteString("\techo \"milliways security shim: real executable not found outside $mw_shim_dir: $mw_shim_command\" >&2\n")
	b.WriteString("\texit 127\n")
	b.WriteString("fi\n")
	fmt.Fprintf(&b, "mw_broker=%s\n", shellQuote(brokerCommand))
	b.WriteString("mw_broker_path=\n")
	b.WriteString("if [ -n \"$mw_broker\" ]; then mw_broker_path=$(PATH=$mw_path_without_shim command -v \"$mw_broker\" 2>/dev/null || true); fi\n")
	b.WriteString("if [ -n \"$mw_broker_path\" ] && [ -x \"$mw_broker_path\" ]; then\n")
	fmt.Fprintf(&b, "\t%s=1 \\\n", EnvActive)
	fmt.Fprintf(&b, "\t%s=\"$mw_shim_command\" \\\n", EnvCommand)
	fmt.Fprintf(&b, "\t%s=\"$mw_shim_category\" \\\n", EnvCategory)
	fmt.Fprintf(&b, "\t%s=\"$mw_shim_dir\" \\\n", EnvShimDir)
	fmt.Fprintf(&b, "\t%s=\"$mw_resolved\" \\\n", EnvResolvedPath)
	fmt.Fprintf(&b, "\t%s=\"$mw_original_path\" \\\n", EnvOriginalPath)
	b.WriteString("\tPATH=$mw_path_without_shim \\\n")
	b.WriteString("\texec \"$mw_broker_path\"")
	for _, arg := range brokerArgs {
		fmt.Fprintf(&b, " %s", shellQuote(arg))
	}
	b.WriteString(" -- \"$mw_resolved\" \"$@\"\n")
	b.WriteString("fi\n")
	b.WriteString("if [ \"${MILLIWAYS_SHIM_FAIL_OPEN:-}\" = \"1\" ]; then\n")
	b.WriteString("\techo \"milliways security shim: broker unavailable; continuing because MILLIWAYS_SHIM_FAIL_OPEN=1\" >&2\n")
	fmt.Fprintf(&b, "\tunset %s %s %s %s %s %s %s\n", EnvActive, EnvCommand, EnvCategory, EnvShimDir, EnvResolvedPath, EnvOriginalPath, EnvBroker)
	b.WriteString("\tPATH=$mw_path_without_shim exec \"$mw_resolved\" \"$@\"\n")
	b.WriteString("fi\n")
	b.WriteString("echo \"milliways security shim: broker unavailable; blocked by default\" >&2\n")
	b.WriteString("exit 126\n")
	return b.String(), nil
}

// ResolveOptions controls real command lookup.
type ResolveOptions struct {
	Command string
	Path    string
	ShimDir string
}

// ResolveRealBinary finds Command on Path while excluding ShimDir. It returns
// the first executable candidate after shim-directory entries are removed.
func ResolveRealBinary(opts ResolveOptions) (string, error) {
	command := strings.TrimSpace(opts.Command)
	if command == "" {
		return "", errors.New("resolve real binary: command is required")
	}
	if filepath.Base(command) != command {
		return "", fmt.Errorf("resolve real binary: command %q must be a bare executable name", command)
	}
	path := opts.Path
	if path == "" {
		path = os.Getenv("PATH")
	}
	shimDir, err := canonicalDir(opts.ShimDir)
	if err != nil {
		return "", fmt.Errorf("resolve real binary: shim dir: %w", err)
	}
	for _, dir := range filepath.SplitList(path) {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		candidateDir, err := canonicalDir(dir)
		if err == nil && sameDir(candidateDir, shimDir) {
			continue
		}
		candidate := filepath.Join(dir, command)
		if isExecutable(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("resolve real binary: %q not found outside shim dir", command)
}

// Action is the daemon policy decision for a shimmed command.
type Action string

const (
	ActionAllow             Action = "allow"
	ActionWarn              Action = "warn"
	ActionNeedsConfirmation Action = "needs-confirmation"
	ActionBlock             Action = "block"
)

// Request is the library-level protocol a shim sends to milliwaysd before
// executing the real command.
type Request struct {
	Command       string            `json:"command"`
	Argv          []string          `json:"argv"`
	CWD           string            `json:"cwd"`
	ClientID      string            `json:"client_id,omitempty"`
	SessionID     string            `json:"session_id,omitempty"`
	Workspace     string            `json:"workspace,omitempty"`
	ShimDir       string            `json:"shim_dir,omitempty"`
	ResolvedPath  string            `json:"resolved_path,omitempty"`
	Environment   map[string]string `json:"environment,omitempty"`
	CorrelationID string            `json:"correlation_id,omitempty"`
	RequestedAt   time.Time         `json:"requested_at,omitempty"`
}

// Decision is the daemon policy response a shim uses to decide whether to exec
// the real binary.
type Decision struct {
	Action       Action            `json:"action"`
	Reason       string            `json:"reason,omitempty"`
	Warnings     []string          `json:"warnings,omitempty"`
	ResolvedPath string            `json:"resolved_path,omitempty"`
	Environment  map[string]string `json:"environment,omitempty"`
}

func canonicalDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", errors.New("directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return filepath.Clean(abs), nil
	}
	return filepath.Clean(resolved), nil
}

func comparableDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return filepath.Clean(abs)
}

func sameDir(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0111 != 0
}

func lookPathIn(command, path string) string {
	command = strings.TrimSpace(command)
	if command == "" || filepath.Base(command) != command {
		return ""
	}
	for _, dir := range filepath.SplitList(path) {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		candidate := filepath.Join(dir, command)
		if isExecutable(candidate) {
			return candidate
		}
	}
	return ""
}

func writeExecutableIfChanged(path string, content []byte) (bool, error) {
	current, err := os.ReadFile(path)
	if err == nil && string(current) == string(content) {
		if chmodErr := os.Chmod(path, 0o755); chmodErr != nil {
			return false, fmt.Errorf("install command shims: chmod %q: %w", path, chmodErr)
		}
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("install command shims: read %q: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return false, fmt.Errorf("install command shims: temp file for %q: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("install command shims: write %q: %w", tmpName, err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("install command shims: chmod %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("install command shims: close %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return false, fmt.Errorf("install command shims: rename %q: %w", path, err)
	}
	return true, nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func init() {
	if err := ValidateCatalog(DefaultCatalog); err != nil {
		panic(err)
	}
}
