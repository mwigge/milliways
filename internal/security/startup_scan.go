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

package security

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/security/rules"
)

const maxStartupScanFileBytes = 1024 * 1024

// StartupScanRoot is an additional root scanned for user-level persistence.
// Tests and daemon wiring can inject platform-specific locations without the
// scanner reaching into the real user home directory by itself.
type StartupScanRoot struct {
	Name string
	Path string
}

// StartupScanOptions controls the fast local startup security scan.
type StartupScanOptions struct {
	WorkspaceRoot        string
	UserPersistenceRoots []StartupScanRoot
	Rules                []rules.Rule
	Now                  func() time.Time
}

// StartupScanResult is the structured output of the fast local startup scan.
type StartupScanResult struct {
	WorkspaceRoot string
	StartedAt     time.Time
	CompletedAt   time.Time
	FilesScanned  []string
	Findings      []StartupFinding
}

// StartupFinding is a deterministic local warning suitable for later CLI and
// daemon rendering. Evidence should be concise and must not contain secrets.
type StartupFinding struct {
	ID          string
	RuleID      string
	Category    rules.Category
	Severity    rules.Severity
	Title       string
	Description string
	Remediation string
	Path        string
	RelPath     string
	Line        int
	Evidence    string
}

// RunStartupScan performs the mandatory fast local security posture scan. It
// does not execute project code, call the network, or shell out to scanners.
func RunStartupScan(ctx context.Context, opts StartupScanOptions) (StartupScanResult, error) {
	if strings.TrimSpace(opts.WorkspaceRoot) == "" {
		return StartupScanResult{}, errors.New("startup scan: workspace root required")
	}

	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	ruleSet := opts.Rules
	if len(ruleSet) == 0 {
		ruleSet = rules.Bundled()
	}

	root, err := filepath.Abs(opts.WorkspaceRoot)
	if err != nil {
		return StartupScanResult{}, fmt.Errorf("startup scan: workspace root: %w", err)
	}
	started := now()
	scanner := startupScanner{
		root:     root,
		rules:    ruleSet,
		findings: make(map[string]StartupFinding),
		scanned:  make(map[string]struct{}),
	}

	if err := scanner.scanWorkspace(ctx); err != nil {
		return StartupScanResult{}, err
	}
	for _, r := range opts.UserPersistenceRoots {
		if err := ctx.Err(); err != nil {
			return StartupScanResult{}, err
		}
		scanner.scanPersistenceRoot(r)
	}

	return StartupScanResult{
		WorkspaceRoot: root,
		StartedAt:     started,
		CompletedAt:   now(),
		FilesScanned:  scanner.filesScanned(),
		Findings:      scanner.findingsList(),
	}, nil
}

type startupScanner struct {
	root     string
	rules    []rules.Rule
	findings map[string]StartupFinding
	scanned  map[string]struct{}
}

func (s *startupScanner) scanWorkspace(ctx context.Context) error {
	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".milliways":
				if path != s.root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !s.relevantWorkspaceFile(path) {
			return nil
		}
		s.scanFile(path, s.rel(path))
		return nil
	})
	if err != nil {
		return fmt.Errorf("startup scan: walk workspace: %w", err)
	}

	s.scanPackagePolicy(filepath.Join(s.root, "package.json"))
	s.scanPackageJSON(filepath.Join(s.root, "package.json"))
	s.scanVSCodeTasks(filepath.Join(s.root, ".vscode", "tasks.json"))
	s.scanClaudeSettings(filepath.Join(s.root, ".claude", "settings.json"))
	return nil
}

func (s *startupScanner) relevantWorkspaceFile(path string) bool {
	rel := filepath.ToSlash(s.rel(path))
	base := filepath.Base(path)
	if strings.HasPrefix(rel, ".claude/") || strings.HasPrefix(rel, ".vscode/") {
		return true
	}
	switch base {
	case "package.json", ".npmrc", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb",
		"pnpm-workspace.yaml", "go.mod", "go.sum", "Cargo.toml", "Cargo.lock", "requirements.txt", "pyproject.toml",
		"pdm.lock", "poetry.lock", "uv.lock", "router_init.js", "router_runtime.js", "setup.mjs", "tanstack_runner.js":
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".json", ".js", ".mjs", ".cjs", ".yaml", ".yml", ".toml":
		return true
	default:
		return false
	}
}

func (s *startupScanner) scanFile(path, rel string) {
	s.scanned[path] = struct{}{}
	base := filepath.Base(path)
	for _, rule := range s.rulesByType(rules.MatchPath) {
		if containsPattern(rule.Patterns, base) {
			s.addRuleFinding(rule, path, rel, 0, base)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil || len(data) > maxStartupScanFileBytes {
		return
	}
	text := string(data)
	lowerText := strings.ToLower(text)
	for _, rule := range s.rulesByType(rules.MatchDomainIP) {
		for _, pattern := range rule.Patterns {
			if line := lineForSubstring([]byte(lowerText), strings.ToLower(pattern)); line > 0 {
				s.addRuleFinding(rule, path, rel, line, pattern)
			}
		}
	}
	if strings.HasPrefix(filepath.ToSlash(rel), ".claude/") {
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".js" || ext == ".mjs" || ext == ".cjs" {
			s.addFinding(StartupFinding{
				ID:          "client.claude.executable-config",
				RuleID:      "client.claude.executable-config",
				Category:    rules.CategoryClientProfile,
				Severity:    rules.SeverityWarn,
				Title:       "Claude workspace config contains executable code",
				Description: "Executable files under .claude can persist agent behavior in this workspace.",
				Remediation: "Review the file before starting an agent and remove it if it is not expected.",
				Path:        path,
				RelPath:     rel,
				Evidence:    base,
			})
		}
	}
	for _, rule := range s.rulesByType(rules.MatchServiceUnit) {
		if strings.Contains(text, "gh-token-monitor") {
			s.addRuleFinding(rule, path, rel, lineForSubstring(data, "gh-token-monitor"), "gh-token-monitor")
		}
	}
}

func (s *startupScanner) scanPersistenceRoot(root StartupScanRoot) {
	if strings.TrimSpace(root.Path) == "" {
		return
	}
	abs, err := filepath.Abs(root.Path)
	if err != nil {
		return
	}
	_ = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel := filepath.Base(path)
		if r, err := filepath.Rel(abs, path); err == nil {
			rel = filepath.ToSlash(filepath.Join(root.Name, r))
		}
		s.scanFile(path, rel)
		for _, rule := range s.rulesByType(rules.MatchServiceUnit) {
			if patternInString(rule.Patterns, filepath.Base(path)) {
				s.addRuleFinding(rule, path, rel, 0, filepath.Base(path))
			}
		}
		return nil
	})
}

func (s *startupScanner) scanVSCodeTasks(path string) {
	var doc map[string]any
	if !readJSON(path, &doc) {
		return
	}
	tasks, _ := doc["tasks"].([]any)
	for _, task := range tasks {
		m, _ := task.(map[string]any)
		runOptions, _ := m["runOptions"].(map[string]any)
		runOn, _ := runOptions["runOn"].(string)
		if runOn != "folderOpen" {
			continue
		}
		label, _ := m["label"].(string)
		for _, rule := range s.rulesByType(rules.MatchJSON) {
			if containsPattern(rule.Patterns, "runOptions.runOn=folderOpen") {
				evidence := "runOptions.runOn=folderOpen"
				if label != "" {
					evidence += " task=" + label
				}
				s.addRuleFinding(rule, path, s.rel(path), 0, evidence)
			}
		}
	}
}

func (s *startupScanner) scanClaudeSettings(path string) {
	var doc map[string]any
	if !readJSON(path, &doc) {
		return
	}
	if _, ok := doc["hooks"]; ok {
		s.addFinding(StartupFinding{
			ID:          "client.claude.hooks",
			RuleID:      "client.claude.hooks",
			Category:    rules.CategoryClientProfile,
			Severity:    rules.SeverityWarn,
			Title:       "Claude workspace hooks are configured",
			Description: "Claude hooks can run commands from workspace configuration.",
			Remediation: "Review .claude/settings.json hooks before starting an agent.",
			Path:        path,
			RelPath:     s.rel(path),
			Evidence:    "hooks",
		})
	}
}

func (s *startupScanner) scanPackageJSON(path string) {
	var doc map[string]any
	if !readJSON(path, &doc) {
		return
	}
	scripts, _ := doc["scripts"].(map[string]any)
	for name, raw := range scripts {
		command, _ := raw.(string)
		for _, rule := range s.rulesByType(rules.MatchPackageScript) {
			if containsPattern(rule.Patterns, name) {
				s.addRuleFinding(rule, path, s.rel(path), 0, name+": "+truncateEvidence(command))
			}
		}
	}

	for _, field := range []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"} {
		deps, _ := doc[field].(map[string]any)
		for name, raw := range deps {
			version, _ := raw.(string)
			if isGitHubCommitDependency(version) {
				for _, rule := range s.rulesByType(rules.MatchCommand) {
					if containsPattern(rule.Patterns, "github:") {
						s.addRuleFinding(rule, path, s.rel(path), 0, field+"."+name+"="+truncateEvidence(version))
					}
				}
			}
		}
	}
}

func (s *startupScanner) scanPackagePolicy(packageJSON string) {
	if _, err := os.Stat(packageJSON); err != nil {
		return
	}
	npmrc := filepath.Join(s.root, ".npmrc")
	values := readNPMRC(npmrc)
	if strings.ToLower(values["ignore-scripts"]) != "true" {
		s.addFinding(StartupFinding{
			ID:          "policy.npm.ignore-scripts",
			RuleID:      "policy.npm.ignore-scripts",
			Category:    rules.CategoryPolicy,
			Severity:    rules.SeverityWarn,
			Title:       "npm install scripts are not disabled",
			Description: "Missing ignore-scripts=true allows package lifecycle scripts to run during installs.",
			Remediation: "Add ignore-scripts=true to .npmrc or enforce an equivalent package-manager policy.",
			Path:        npmrc,
			RelPath:     ".npmrc",
			Evidence:    "ignore-scripts!=true",
		})
	}
	if !hasAnyFile(s.root, "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb") {
		s.addFinding(StartupFinding{
			ID:          "policy.package.lockfile",
			RuleID:      "policy.package.lockfile",
			Category:    rules.CategoryPolicy,
			Severity:    rules.SeverityWarn,
			Title:       "Package manifest has no lockfile",
			Description: "Missing lockfiles make dependency resolution less reproducible before agent work starts.",
			Remediation: "Commit the package manager lockfile for this workspace.",
			Path:        packageJSON,
			RelPath:     "package.json",
			Evidence:    "no package lockfile found",
		})
	}
	if values["minimum-release-age"] == "" && values["minimumreleaseage"] == "" && values["min-release-age"] == "" {
		s.addFinding(StartupFinding{
			ID:          "policy.package.release-age",
			RuleID:      "policy.package.release-age",
			Category:    rules.CategoryPolicy,
			Severity:    rules.SeverityWarn,
			Title:       "No package release-age policy found",
			Description: "A release-age policy can reduce exposure to freshly published malicious packages where supported.",
			Remediation: "Configure the package manager's release-age policy where available.",
			Path:        npmrc,
			RelPath:     ".npmrc",
			Evidence:    "minimum-release-age missing",
		})
	}
	if registry := values["registry"]; strings.HasPrefix(strings.ToLower(registry), "http://") {
		s.addFinding(StartupFinding{
			ID:          "policy.npm.insecure-registry",
			RuleID:      "policy.npm.insecure-registry",
			Category:    rules.CategoryPolicy,
			Severity:    rules.SeverityWarn,
			Title:       "npm registry uses plaintext HTTP",
			Description: "Plaintext registries allow package metadata and tarball responses to be intercepted or modified.",
			Remediation: "Use an HTTPS registry URL or a trusted internal mirror with transport security.",
			Path:        npmrc,
			RelPath:     ".npmrc",
			Evidence:    "registry=" + truncateEvidence(registry),
		})
	}
	if strings.ToLower(values["always-auth"]) == "true" {
		s.addFinding(StartupFinding{
			ID:          "policy.npm.always-auth",
			RuleID:      "policy.npm.always-auth",
			Category:    rules.CategoryPolicy,
			Severity:    rules.SeverityWarn,
			Title:       "npm always-auth is enabled",
			Description: "always-auth sends registry credentials more broadly and increases token exposure from compromised package tooling.",
			Remediation: "Disable always-auth unless a trusted private registry explicitly requires it.",
			Path:        npmrc,
			RelPath:     ".npmrc",
			Evidence:    "always-auth=true",
		})
	}
	if shell := strings.TrimSpace(values["script-shell"]); shell != "" {
		s.addFinding(StartupFinding{
			ID:          "policy.npm.script-shell",
			RuleID:      "policy.npm.script-shell",
			Category:    rules.CategoryPolicy,
			Severity:    rules.SeverityWarn,
			Title:       "npm script-shell is overridden",
			Description: "script-shell changes the interpreter used by lifecycle scripts and can hide unexpected agent or package execution.",
			Remediation: "Remove script-shell unless it is required and reviewed for this workspace.",
			Path:        npmrc,
			RelPath:     ".npmrc",
			Evidence:    "script-shell=" + truncateEvidence(shell),
		})
	}
}

func (s *startupScanner) rulesByType(t rules.MatchType) []rules.Rule {
	var out []rules.Rule
	for _, r := range s.rules {
		if r.MatchType == t {
			out = append(out, r)
		}
	}
	return out
}

func (s *startupScanner) addRuleFinding(rule rules.Rule, path, rel string, line int, evidence string) {
	s.addFinding(StartupFinding{
		ID:          rule.ID,
		RuleID:      rule.ID,
		Category:    rule.Category,
		Severity:    rule.Severity,
		Title:       rule.Title,
		Description: rule.Description,
		Remediation: rule.Remediation,
		Path:        path,
		RelPath:     rel,
		Line:        line,
		Evidence:    truncateEvidence(evidence),
	})
}

func (s *startupScanner) addFinding(f StartupFinding) {
	if f.Line < 0 {
		f.Line = 0
	}
	key := f.ID + "\x00" + f.Path + "\x00" + f.Evidence
	s.findings[key] = f
}

func (s *startupScanner) filesScanned() []string {
	out := make([]string, 0, len(s.scanned))
	for p := range s.scanned {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (s *startupScanner) findingsList() []StartupFinding {
	out := make([]StartupFinding, 0, len(s.findings))
	for _, f := range s.findings {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return severityRank(out[i].Severity) > severityRank(out[j].Severity)
		}
		if out[i].RelPath != out[j].RelPath {
			return out[i].RelPath < out[j].RelPath
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func severityRank(severity rules.Severity) int {
	switch severity {
	case rules.SeverityBlock:
		return 3
	case rules.SeverityWarn:
		return 2
	case rules.SeverityInfo:
		return 1
	default:
		return 0
	}
}

func (s *startupScanner) rel(path string) string {
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func readJSON(path string, dst any) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > maxStartupScanFileBytes {
		return false
	}
	return json.Unmarshal(data, dst) == nil
}

func readNPMRC(path string) map[string]string {
	values := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return values
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(val)
	}
	return values
}

func hasAnyFile(root string, names ...string) bool {
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return true
		}
	}
	return false
}

func containsPattern(patterns []string, value string) bool {
	for _, p := range patterns {
		if p == value {
			return true
		}
	}
	return false
}

func patternInString(patterns []string, value string) bool {
	for _, p := range patterns {
		if strings.Contains(value, p) {
			return true
		}
	}
	return false
}

func lineForSubstring(data []byte, needle string) int {
	if needle == "" {
		return 0
	}
	idx := bytes.Index(data, []byte(needle))
	if idx < 0 {
		return 0
	}
	return bytes.Count(data[:idx], []byte{'\n'}) + 1
}

func isGitHubCommitDependency(version string) bool {
	if !strings.Contains(version, "github:") && !strings.Contains(version, "github.com/") {
		return false
	}
	return strings.Contains(version, "#") || strings.Contains(version, "commit")
}

func truncateEvidence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= 160 {
		return s
	}
	return s[:157] + "..."
}
