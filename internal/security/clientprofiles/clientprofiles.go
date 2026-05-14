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

package clientprofiles

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ClientProfileCheck checks one MilliWays client profile for local risk signals.
type ClientProfileCheck interface {
	Check(ctx context.Context, workspace string) ProfileResult
}

// Severity is a coarse local profile finding severity.
type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityWarning  Severity = "WARNING"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"
)

// Client names used by built-in profile checks.
const (
	ClientClaude  = "claude"
	ClientCodex   = "codex"
	ClientCopilot = "copilot"
	ClientGemini  = "gemini"
	ClientPool    = "pool"
	ClientMiniMax = "minimax"
	ClientLocal   = "local"
)

// ProfileWarning is a structured per-client warning that can be aggregated by
// daemon or CLI layers.
type ProfileWarning struct {
	Client   string   `json:"client"`
	ID       string   `json:"id"`
	Severity Severity `json:"severity"`
	Summary  string   `json:"summary"`
	Detail   string   `json:"detail,omitempty"`
	Path     string   `json:"path,omitempty"`
	Key      string   `json:"key,omitempty"`
}

// ProfileResult is the result of one client profile check.
type ProfileResult struct {
	Client    string           `json:"client"`
	CheckedAt time.Time        `json:"checked_at"`
	Workspace string           `json:"workspace"`
	Warnings  []ProfileWarning `json:"warnings,omitempty"`
	Error     string           `json:"error,omitempty"`
}

// Options injects host-specific paths and environment for testable checks.
type Options struct {
	HomeDir   string
	ConfigDir string
	Env       map[string]string
	LookPath  func(string) (string, error)
	Now       func() time.Time
}

// DefaultOptions returns checks wired to the current process environment.
func DefaultOptions() Options {
	home, _ := os.UserHomeDir()
	config, err := os.UserConfigDir()
	if err != nil || config == "" {
		config = filepath.Join(home, ".config")
	}
	return Options{
		HomeDir:   home,
		ConfigDir: config,
		Env:       environMap(os.Environ()),
		LookPath:  exec.LookPath,
		Now:       func() time.Time { return time.Now().UTC() },
	}
}

// NewAll returns all built-in MilliWays client profile checks.
func NewAll(opts Options) []ClientProfileCheck {
	return []ClientProfileCheck{
		New(ClientClaude, opts),
		New(ClientCodex, opts),
		New(ClientCopilot, opts),
		New(ClientGemini, opts),
		New(ClientPool, opts),
		New(ClientMiniMax, opts),
		New(ClientLocal, opts),
	}
}

// New returns a built-in profile check for client. Unknown clients still return
// a check that reports an error in ProfileResult.
func New(client string, opts Options) ClientProfileCheck {
	opts = normalizeOptions(opts)
	return profileCheck{client: strings.ToLower(strings.TrimSpace(client)), opts: opts}
}

type profileCheck struct {
	client string
	opts   Options
}

func (p profileCheck) Check(ctx context.Context, workspace string) ProfileResult {
	result := ProfileResult{
		Client:    p.client,
		CheckedAt: p.opts.Now().UTC(),
		Workspace: workspace,
	}
	if err := ctx.Err(); err != nil {
		result.Error = err.Error()
		return result
	}

	var warnings []ProfileWarning
	switch p.client {
	case ClientClaude:
		warnings = p.checkClaude(ctx, workspace)
	case ClientCodex:
		warnings = p.checkCodex(ctx, workspace)
	case ClientCopilot:
		warnings = p.checkCopilot(ctx, workspace)
	case ClientGemini:
		warnings = p.checkGemini(ctx, workspace)
	case ClientPool:
		warnings = p.checkPool(ctx, workspace)
	case ClientMiniMax:
		warnings = p.checkMiniMax(ctx, workspace)
	case ClientLocal:
		warnings = p.checkLocal(ctx, workspace)
	default:
		result.Error = fmt.Sprintf("unknown client %q", p.client)
	}
	sortWarnings(warnings)
	result.Warnings = warnings
	if err := ctx.Err(); err != nil {
		result.Error = err.Error()
	}
	return result
}

func (p profileCheck) checkClaude(ctx context.Context, workspace string) []ProfileWarning {
	var warnings []ProfileWarning
	warnings = append(warnings, p.checkBinary(ClientClaude, "claude")...)
	for _, path := range p.candidatePaths(workspace,
		".claude/settings.json",
		".claude/settings.local.json",
		".claude/mcp.json",
		filepath.Join(".config", "claude", "settings.json"),
		filepath.Join(".config", "claude", "mcp.json"),
	) {
		if ctx.Err() != nil {
			return warnings
		}
		warnings = append(warnings, scanConfigFile(ClientClaude, path, "claude")...)
	}
	warnings = append(warnings, scanGlob(ClientClaude, filepath.Join(workspace, ".claude", "*.js"), "claude-script")...)
	warnings = append(warnings, scanInstructionFile(ClientClaude, filepath.Join(workspace, "CLAUDE.md"), "claude-instructions")...)
	warnings = append(warnings, scanWorkspacePackage(workspace, ClientClaude)...)
	return warnings
}

func (p profileCheck) checkCodex(ctx context.Context, workspace string) []ProfileWarning {
	var warnings []ProfileWarning
	warnings = append(warnings, p.checkBinary(ClientCodex, "codex")...)
	for _, path := range p.candidatePaths(workspace,
		".codex/config.toml",
		".codex/config.json",
		filepath.Join(".codex", "config.toml"),
		filepath.Join(".config", "codex", "config.toml"),
		filepath.Join(".config", "codex", "config.json"),
	) {
		if ctx.Err() != nil {
			return warnings
		}
		warnings = append(warnings, scanConfigFile(ClientCodex, path, "codex")...)
	}
	warnings = append(warnings, scanEnvFlags(ClientCodex, p.opts.Env, []string{"CODEX_FLAGS", "CODEX_ARGS", "OPENAI_CODEX_FLAGS"})...)
	warnings = append(warnings, scanWorkspacePackage(workspace, ClientCodex)...)
	return warnings
}

func (p profileCheck) checkCopilot(ctx context.Context, workspace string) []ProfileWarning {
	var warnings []ProfileWarning
	warnings = append(warnings, p.checkBinary(ClientCopilot, "gh")...)
	for _, path := range p.candidatePaths(workspace,
		".copilot/config.json",
		".copilot/settings.json",
		filepath.Join(".config", "github-copilot", "config.json"),
		filepath.Join(".config", "copilot", "config.json"),
	) {
		if ctx.Err() != nil {
			return warnings
		}
		warnings = append(warnings, scanConfigFile(ClientCopilot, path, "copilot")...)
	}
	warnings = append(warnings, scanEnvFlags(ClientCopilot, p.opts.Env, []string{"COPILOT_FLAGS", "COPILOT_ARGS", "GITHUB_COPILOT_FLAGS"})...)
	warnings = append(warnings, scanWorkspacePackage(workspace, ClientCopilot)...)
	return warnings
}

func (p profileCheck) checkGemini(ctx context.Context, workspace string) []ProfileWarning {
	var warnings []ProfileWarning
	warnings = append(warnings, p.checkBinary(ClientGemini, "gemini")...)
	for _, path := range p.candidatePaths(workspace,
		".gemini/settings.json",
		".gemini/config.json",
		filepath.Join(".config", "gemini", "settings.json"),
		filepath.Join(".config", "gemini", "config.json"),
	) {
		if ctx.Err() != nil {
			return warnings
		}
		warnings = append(warnings, scanConfigFile(ClientGemini, path, "gemini")...)
	}
	warnings = append(warnings, scanEnvFlags(ClientGemini, p.opts.Env, []string{"GEMINI_FLAGS", "GEMINI_ARGS"})...)
	warnings = append(warnings, scanWorkspacePackage(workspace, ClientGemini)...)
	return warnings
}

func (p profileCheck) checkPool(ctx context.Context, workspace string) []ProfileWarning {
	var warnings []ProfileWarning
	warnings = append(warnings, p.checkBinary(ClientPool, "pool")...)
	for _, path := range p.candidatePaths(workspace,
		".pool/config.json",
		".pool/settings.json",
		filepath.Join(".config", "pool", "config.json"),
		filepath.Join(".config", "pool", "settings.json"),
	) {
		if ctx.Err() != nil {
			return warnings
		}
		warnings = append(warnings, scanConfigFile(ClientPool, path, "pool")...)
	}
	warnings = append(warnings, scanEnvFlags(ClientPool, p.opts.Env, []string{"POOL_FLAGS", "POOL_ARGS"})...)
	warnings = append(warnings, scanWorkspacePackage(workspace, ClientPool)...)
	return warnings
}

func (p profileCheck) checkMiniMax(ctx context.Context, workspace string) []ProfileWarning {
	var warnings []ProfileWarning
	warnings = append(warnings, p.checkBinary(ClientMiniMax, "minimax")...)
	for _, path := range p.candidatePaths(workspace,
		".minimax/config.json",
		".minimax/settings.json",
		filepath.Join(".config", "minimax", "config.json"),
		filepath.Join(".config", "milliways", "local.env"),
	) {
		if ctx.Err() != nil {
			return warnings
		}
		warnings = append(warnings, scanConfigFile(ClientMiniMax, path, "minimax")...)
		warnings = append(warnings, scanSecretFile(ClientMiniMax, path)...)
	}
	warnings = append(warnings, scanEnvFlags(ClientMiniMax, p.opts.Env, []string{"MINIMAX_FLAGS", "MINIMAX_ARGS"})...)
	warnings = append(warnings, scanWorkspacePackage(workspace, ClientMiniMax)...)
	return warnings
}

func (p profileCheck) checkLocal(ctx context.Context, workspace string) []ProfileWarning {
	var warnings []ProfileWarning
	for _, path := range p.candidatePaths(workspace,
		".milliways/local.env",
		filepath.Join(".config", "milliways", "local.env"),
		filepath.Join(".config", "milliways", "local.yaml"),
	) {
		if ctx.Err() != nil {
			return warnings
		}
		warnings = append(warnings, scanConfigFile(ClientLocal, path, "local")...)
		warnings = append(warnings, scanLocalEndpointFile(path)...)
	}
	if endpoint := strings.TrimSpace(p.opts.Env["MILLIWAYS_LOCAL_ENDPOINT"]); endpoint != "" {
		warnings = append(warnings, localEndpointWarning("", "MILLIWAYS_LOCAL_ENDPOINT", endpoint)...)
	}
	if bind := strings.TrimSpace(p.opts.Env["MILLIWAYS_LOCAL_BIND"]); isPublicBind(bind) && strings.TrimSpace(p.opts.Env["MILLIWAYS_LOCAL_AUTH_TOKEN"]) == "" {
		warnings = append(warnings, ProfileWarning{
			Client:   ClientLocal,
			ID:       "local-public-bind-no-auth",
			Severity: SeverityCritical,
			Summary:  "Local model server is configured to bind publicly without an auth token.",
			Key:      "MILLIWAYS_LOCAL_BIND",
		})
	}
	warnings = append(warnings, scanWorkspacePackage(workspace, ClientLocal)...)
	return warnings
}

func (p profileCheck) checkBinary(client, name string) []ProfileWarning {
	if p.opts.LookPath == nil {
		return nil
	}
	path, err := p.opts.LookPath(name)
	if err != nil || path == "" {
		return nil
	}
	if isWorkspacePath(path) {
		return []ProfileWarning{{
			Client:   client,
			ID:       "client-binary-from-workspace",
			Severity: SeverityHigh,
			Summary:  "Client executable resolves from a workspace-relative path.",
			Detail:   "Prefer a trusted absolute installation path for agent client binaries.",
			Path:     path,
		}}
	}
	return nil
}

func (p profileCheck) candidatePaths(workspace string, rels ...string) []string {
	var paths []string
	for _, rel := range rels {
		if strings.HasPrefix(rel, ".config"+string(filepath.Separator)) {
			if p.opts.ConfigDir != "" {
				paths = append(paths, filepath.Join(p.opts.ConfigDir, strings.TrimPrefix(rel, ".config"+string(filepath.Separator))))
			}
			continue
		}
		if strings.HasPrefix(rel, ".") && p.opts.HomeDir != "" {
			paths = append(paths, filepath.Join(p.opts.HomeDir, rel))
		}
		if workspace != "" {
			paths = append(paths, filepath.Join(workspace, rel))
		}
	}
	return dedupe(paths)
}

func scanConfigFile(client, path, family string) []ProfileWarning {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var warnings []ProfileWarning
	lower := strings.ToLower(string(data))
	if containsAny(lower, []string{"danger-full-access", "sandbox_mode = \"danger-full-access\"", `"sandbox_mode":"danger-full-access"`}) {
		warnings = append(warnings, warning(client, "codex-danger-full-access", SeverityCritical, "Codex sandbox is configured for unrestricted filesystem access.", path, "sandbox_mode"))
	}
	if containsAny(lower, []string{"approval_policy = \"never\"", `"approval_policy":"never"`, "approval-mode = \"never\""}) {
		warnings = append(warnings, warning(client, "codex-no-approval", SeverityHigh, "Codex approval policy disables human approval prompts.", path, "approval_policy"))
	}
	if containsAny(lower, []string{"--allow-all-tools", "allowalltools", "allow_all_tools", "allow all tools", `"allowall":true`, `"autoallow":true`, "auto_allow = true"}) {
		warnings = append(warnings, warning(client, family+"-allow-all-tools", SeverityHigh, "Client config appears to allow all tools without review.", path, "tools"))
	}
	if containsAny(lower, []string{"\"hooks\"", "[hooks]", "pretooluse", "posttooluse", "stop hook", "notification hook"}) {
		warnings = append(warnings, warning(client, family+"-hooks-enabled", SeverityWarning, "Client hooks are configured and should be reviewed for persistent command execution.", path, "hooks"))
	}
	if containsAny(lower, []string{"mcpservers", "mcp_servers", "\"mcp\"", "[mcp]"}) {
		warnings = append(warnings, warning(client, family+"-mcp-config", SeverityWarning, "MCP/tool server configuration is present and should be reviewed.", path, "mcp"))
	}
	if containsAny(lower, []string{"curl | sh", "curl -fs", "wget -q", "bash -c", "sh -c", "child_process", "exec(", "spawn(", "eval("}) {
		warnings = append(warnings, warning(client, family+"-shell-bootstrap", SeverityHigh, "Client config contains shell execution or remote bootstrap patterns.", path, "command"))
	}
	if containsAny(lower, []string{`"writable_roots":["/"]`, "writable_roots = [\"/\"]", "writable-roots /", "workspace = \"/\"", "index_root = \"/\"", "indexroot = \"/\""}) {
		warnings = append(warnings, warning(client, family+"-broad-path-scope", SeverityHigh, "Client config appears to grant broad root filesystem scope.", path, "path"))
	}
	if containsAny(lower, []string{"0.0.0.0", "::", "host = \"0.", "bind = \"0."}) && !containsAny(lower, []string{"auth_token", "bearer", "api_key"}) {
		warnings = append(warnings, warning(client, family+"-public-bind-no-auth", SeverityCritical, "Config exposes a local service on a public bind address without an obvious auth setting.", path, "bind"))
	}
	warnings = append(warnings, scanStructuredConfigSignals(client, path, family, data)...)
	return warnings
}

func scanStructuredConfigSignals(client, path, family string, data []byte) []ProfileWarning {
	var warnings []ProfileWarning
	var parsed any
	if err := json.Unmarshal(data, &parsed); err == nil {
		seen := map[string]bool{}
		walkConfigValue(parsed, "", func(key string, value any) {
			warnings = appendConfigSignal(warnings, seen, client, path, family, key, value)
		})
		return warnings
	}

	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := splitConfigLine(line)
		if !ok {
			continue
		}
		warnings = appendConfigSignal(warnings, seen, client, path, family, key, value)
	}
	return warnings
}

func walkConfigValue(value any, key string, visit func(string, any)) {
	visit(key, value)
	switch typed := value.(type) {
	case map[string]any:
		for childKey, childValue := range typed {
			fullKey := childKey
			if key != "" {
				fullKey = key + "." + childKey
			}
			walkConfigValue(childValue, fullKey, visit)
		}
	case []any:
		for _, childValue := range typed {
			walkConfigValue(childValue, key, visit)
		}
	}
}

func appendConfigSignal(warnings []ProfileWarning, seen map[string]bool, client, path, family, key string, value any) []ProfileWarning {
	normalizedKey := normalizeConfigKey(key)
	if isAutoApprovalSetting(normalizedKey, value) {
		warnings = appendOnce(warnings, seen, warning(client, family+"-auto-approval", SeverityHigh, "Client config auto-approves tools or bypasses approval prompts.", path, key))
	}
	if isBroadPathSetting(normalizedKey, value) {
		warnings = appendOnce(warnings, seen, warning(client, family+"-broad-path-scope", SeverityHigh, "Client config appears to grant broad root filesystem scope.", path, key))
	}
	if configKeyOrValueMentionsRiskyEnv(normalizedKey, value) {
		warnings = appendOnce(warnings, seen, warning(client, family+"-risky-env-var", SeverityHigh, "Client config references environment variables that commonly carry secrets.", path, key))
	}
	return warnings
}

func appendOnce(warnings []ProfileWarning, seen map[string]bool, w ProfileWarning) []ProfileWarning {
	key := w.ID + "\x00" + w.Path + "\x00" + w.Key
	if seen[key] {
		return warnings
	}
	seen[key] = true
	return append(warnings, w)
}

func normalizeConfigKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	replacer := strings.NewReplacer("_", "", "-", "", " ", "", ".", "")
	return replacer.Replace(key)
}

func isAutoApprovalSetting(key string, value any) bool {
	if !truthyConfigValue(value) {
		if key != "permissionsallow" {
			return false
		}
		return configValueContainsAny(value, []string{"*", "bash(*)", "read(/)", "write(/)"})
	}
	return containsAny(key, []string{
		"autoapprove",
		"autoapproval",
		"autoallow",
		"allowall",
		"allowalltools",
		"dangerouslybypassapprovalsandsandbox",
		"dangerouslyskippermissions",
		"skippermissions",
		"yolo",
	})
}

func isBroadPathSetting(key string, value any) bool {
	if !containsAny(key, []string{"writableroots", "workspaceroots", "adddirs", "allowpaths", "allowedpaths", "workspace", "workspacepath", "root", "indexroot"}) {
		return false
	}
	return configValueHasRootPath(value)
}

func configKeyOrValueMentionsRiskyEnv(key string, value any) bool {
	if riskyEnvKey(key) {
		return true
	}
	return configValueContainsAny(value, []string{
		"github_token",
		"gh_token",
		"aws_access_key_id",
		"aws_secret_access_key",
		"npm_token",
		"slack_bot_token",
		"stripe_secret_key",
		"google_application_credentials",
	})
}

func riskyEnvKey(key string) bool {
	switch key {
	case "githubtoken", "ghtoken", "awsaccesskeyid", "awssecretaccesskey", "npmtoken", "slackbottoken", "stripesecretkey", "googleapplicationcredentials":
		return true
	default:
		return false
	}
}

func truthyConfigValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.Trim(strings.TrimSpace(typed), `"'`)) {
		case "true", "yes", "on", "1", "always", "never":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func configValueContainsAny(value any, needles []string) bool {
	switch typed := value.(type) {
	case string:
		return containsAny(strings.ToLower(typed), needles)
	case []any:
		for _, item := range typed {
			if configValueContainsAny(item, needles) {
				return true
			}
		}
	case map[string]any:
		for k, item := range typed {
			if containsAny(strings.ToLower(k), needles) || configValueContainsAny(item, needles) {
				return true
			}
		}
	}
	return false
}

func configValueHasRootPath(value any) bool {
	switch typed := value.(type) {
	case string:
		for _, part := range strings.FieldsFunc(typed, func(r rune) bool {
			return r == '[' || r == ']' || r == ',' || r == '\'' || r == '"' || r == ' ' || r == '\t'
		}) {
			if strings.TrimSpace(part) == "/" {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if configValueHasRootPath(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if configValueHasRootPath(item) {
				return true
			}
		}
	}
	return false
}

func scanGlob(client, pattern, family string) []ProfileWarning {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	var warnings []ProfileWarning
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lower := strings.ToLower(string(data))
		if containsAny(lower, []string{"child_process", "exec(", "spawn(", "curl", "wget", "eval(", "process.env"}) {
			warnings = append(warnings, warning(client, family+"-executable-script", SeverityHigh, "Client script contains command execution, download, or environment access patterns.", path, "script"))
		}
	}
	return warnings
}

func scanInstructionFile(client, path, id string) []ProfileWarning {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lower := strings.ToLower(string(data))
	if containsAny(lower, []string{"curl | sh", "wget | sh", "chmod +x", "sudo ", "eval ", "bash -c", "sh -c"}) {
		return []ProfileWarning{warning(client, id, SeverityWarning, "Workspace instructions contain executable setup directives.", path, "instructions")}
	}
	return nil
}

func scanEnvFlags(client string, env map[string]string, keys []string) []ProfileWarning {
	var warnings []ProfileWarning
	for _, key := range keys {
		value := strings.ToLower(env[key])
		if value == "" {
			continue
		}
		if containsAny(value, []string{"--allow-all-tools", "--dangerously-skip-permissions", "danger-full-access", "--approval-mode never", "--approval never"}) {
			warnings = append(warnings, ProfileWarning{
				Client:   client,
				ID:       client + "-unsafe-env-flags",
				Severity: SeverityHigh,
				Summary:  "Client environment flags request unsafe tool or approval behavior.",
				Key:      key,
			})
		}
		if containsAny(value, []string{" --cwd /", "--workspace /", "--path /", "--root /"}) {
			warnings = append(warnings, ProfileWarning{
				Client:   client,
				ID:       client + "-broad-env-path",
				Severity: SeverityHigh,
				Summary:  "Client environment flags appear to grant root filesystem scope.",
				Key:      key,
			})
		}
	}
	return warnings
}

func scanSecretFile(client, path string) []ProfileWarning {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lower := strings.ToLower(string(data))
	if containsAny(lower, []string{"minimax_api_key=", "minimax-api-key", "\"minimax_api_key\"", "api_key:"}) && containsAny(lower, []string{"sk-", "api_key=", "apikey"}) {
		return []ProfileWarning{warning(client, "minimax-key-in-config", SeverityHigh, "MiniMax API key material appears to be stored in a local config file.", path, "api_key")}
	}
	return nil
}

func scanLocalEndpointFile(path string) []ProfileWarning {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var warnings []ProfileWarning
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := splitConfigLine(line)
		if !ok {
			continue
		}
		switch strings.ToUpper(key) {
		case "MILLIWAYS_LOCAL_ENDPOINT", "LOCAL_ENDPOINT", "ENDPOINT":
			warnings = append(warnings, localEndpointWarning(path, key, value)...)
		case "MILLIWAYS_LOCAL_BIND", "LOCAL_BIND", "BIND":
			if isPublicBind(value) && !fileContainsAuthSetting(data) {
				warnings = append(warnings, warning(ClientLocal, "local-public-bind-no-auth", SeverityCritical, "Local model server binds publicly without an obvious auth token.", path, key))
			}
		}
	}
	return warnings
}

func localEndpointWarning(path, key, endpoint string) []ProfileWarning {
	u, err := url.Parse(strings.Trim(endpoint, `"' `))
	if err != nil || u.Hostname() == "" {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil
	}
	return []ProfileWarning{{
		Client:   ClientLocal,
		ID:       "local-non-loopback-endpoint",
		Severity: SeverityHigh,
		Summary:  "Local model endpoint is not loopback-scoped.",
		Detail:   "Local model traffic should stay on localhost unless a wider endpoint is explicitly trusted.",
		Path:     path,
		Key:      key,
	}}
}

func scanWorkspacePackage(workspace, client string) []ProfileWarning {
	if workspace == "" {
		return nil
	}
	path := filepath.Join(workspace, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var pkg struct {
		Scripts         map[string]string `json:"scripts"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}

	var warnings []ProfileWarning
	for name, script := range pkg.Scripts {
		lower := strings.ToLower(script)
		if containsAny(lower, []string{"curl |", "curl -fs", "wget |", "wget -q", "bash -c", "sh -c", "node -e", "eval ", "gh-token-monitor"}) {
			warnings = append(warnings, warning(client, "workspace-suspicious-package-script", SeverityHigh, "Workspace package script contains shell bootstrap or credential-monitoring patterns.", path, "scripts."+name))
		}
	}
	for name := range mergePackageMaps(pkg.Dependencies, pkg.DevDependencies) {
		if isSuspiciousPackageName(name) {
			warnings = append(warnings, warning(client, "workspace-suspicious-package", SeverityHigh, "Workspace package dependencies include a known suspicious package name.", path, name))
		}
	}
	return warnings
}

func warning(client, id string, sev Severity, summary, path, key string) ProfileWarning {
	return ProfileWarning{
		Client:   client,
		ID:       id,
		Severity: sev,
		Summary:  summary,
		Path:     path,
		Key:      key,
	}
}

func normalizeOptions(opts Options) Options {
	if opts.HomeDir == "" && opts.ConfigDir == "" && opts.Env == nil && opts.LookPath == nil && opts.Now == nil {
		opts = DefaultOptions()
	}
	if opts.ConfigDir == "" && opts.HomeDir != "" {
		opts.ConfigDir = filepath.Join(opts.HomeDir, ".config")
	}
	if opts.Env == nil {
		opts.Env = map[string]string{}
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	return opts
}

func environMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}

func containsAny(s string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func splitConfigLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		key, value, ok = strings.Cut(line, ":")
	}
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), true
}

func fileContainsAuthSetting(data []byte) bool {
	lower := strings.ToLower(string(data))
	return containsAny(lower, []string{"auth_token", "bearer_token", "api_key", "authorization"})
}

func isPublicBind(value string) bool {
	value = strings.Trim(strings.ToLower(strings.TrimSpace(value)), `"'`)
	return value == "0.0.0.0" || value == "::" || strings.HasPrefix(value, "0.0.0.0:") || strings.HasPrefix(value, "[::]:")
}

func isWorkspacePath(path string) bool {
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	first := strings.Split(filepath.Clean(path), string(filepath.Separator))[0]
	return first == "." || first == ".." || first == "node_modules" || first == "vendor" || first == "bin"
}

func isSuspiciousPackageName(name string) bool {
	switch strings.ToLower(name) {
	case "gh-token-monitor", "router_init", "router-runtime", "tanstack_runner", "git-tanstack":
		return true
	default:
		return false
	}
}

func mergePackageMaps(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func dedupe(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := values[:0]
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sortWarnings(warnings []ProfileWarning) {
	sort.SliceStable(warnings, func(i, j int) bool {
		if warnings[i].Client != warnings[j].Client {
			return warnings[i].Client < warnings[j].Client
		}
		if warnings[i].Path != warnings[j].Path {
			return warnings[i].Path < warnings[j].Path
		}
		if warnings[i].ID != warnings[j].ID {
			return warnings[i].ID < warnings[j].ID
		}
		return warnings[i].Key < warnings[j].Key
	})
}

var _ ClientProfileCheck = profileCheck{}
