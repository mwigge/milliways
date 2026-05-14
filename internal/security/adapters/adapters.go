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

// Package adapters contains external scanner adapter foundations for
// Secure MilliWays. Adapters are intentionally injectable so tests and callers
// can avoid relying on local binaries or network access.
package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/security"
)

// ErrNotInstalled is returned when an adapter binary cannot be found on PATH.
var ErrNotInstalled = errors.New("scanner adapter not installed")

// LookPathFunc resolves an executable name.
type LookPathFunc func(file string) (string, error)

// ExecFunc runs an executable with arguments and returns stdout, stderr, and an
// optional process error. Exit codes that represent findings are normalized by
// each adapter.
type ExecFunc func(ctx context.Context, path string, args ...string) ([]byte, []byte, error)

// ScannerAdapter is the common contract for external scanner integrations.
type ScannerAdapter interface {
	Name() string
	Installed() bool
	Version(ctx context.Context) (string, error)
	Scan(ctx context.Context, workspace string, targets []string) (security.ScanResult, error)
	RenderFinding(security.Finding) string
}

// Option customizes adapter execution.
type Option func(*baseAdapter)

// WithLookPath injects binary resolution.
func WithLookPath(fn LookPathFunc) Option {
	return func(a *baseAdapter) {
		if fn != nil {
			a.lookPath = fn
		}
	}
}

// WithExec injects process execution.
func WithExec(fn ExecFunc) Option {
	return func(a *baseAdapter) {
		if fn != nil {
			a.exec = fn
		}
	}
}

type baseAdapter struct {
	name       string
	binary     string
	kind       security.ScanKind
	category   security.FindingCategory
	versionArg []string
	scanArgs   func(workspace string, targets []string) []string
	parse      func(workspace string, out []byte) ([]security.Finding, error)
	render     func(security.Finding) string
	lookPath   LookPathFunc
	exec       ExecFunc
	now        func() time.Time
}

func newBaseAdapter(name, binary string, kind security.ScanKind, category security.FindingCategory, opts ...Option) *baseAdapter {
	a := &baseAdapter{
		name:     name,
		binary:   binary,
		kind:     kind,
		category: category,
		lookPath: exec.LookPath,
		exec:     commandExec,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func (a *baseAdapter) Name() string {
	return a.name
}

func (a *baseAdapter) Installed() bool {
	_, err := a.lookPath(a.binary)
	return err == nil
}

func (a *baseAdapter) Version(ctx context.Context) (string, error) {
	path, err := a.path()
	if err != nil {
		return "", err
	}
	args := a.versionArg
	if len(args) == 0 {
		args = []string{"--version"}
	}
	stdout, stderr, err := a.exec(ctx, path, args...)
	if err != nil {
		return "", fmt.Errorf("%s version: %w: %s", a.name, err, strings.TrimSpace(string(stderr)))
	}
	return firstLine(stdout, stderr), nil
}

func (a *baseAdapter) Scan(ctx context.Context, workspace string, targets []string) (security.ScanResult, error) {
	path, err := a.path()
	if err != nil {
		return security.ScanResult{}, err
	}
	args := a.scanArgs(workspace, targets)
	stdout, stderr, err := a.exec(ctx, path, args...)
	if err != nil && !isFindingsExit(err) {
		return security.ScanResult{}, fmt.Errorf("%s scan: %w: %s", a.name, err, strings.TrimSpace(string(stderr)))
	}
	if len(stdout) == 0 {
		return security.ScanResult{
			ScannedAt: a.now(),
			LockFiles: targets,
			Kind:      a.kind,
			Workspace: workspace,
			ToolName:  a.name,
		}, nil
	}
	findings, err := a.parse(workspace, stdout)
	if err != nil {
		return security.ScanResult{}, fmt.Errorf("parse %s output: %w", a.name, err)
	}
	for i := range findings {
		if findings[i].Category == "" {
			findings[i].Category = a.category
		}
		if findings[i].ToolName == "" {
			findings[i].ToolName = a.name
		}
		if findings[i].WorkspacePath == "" {
			findings[i].WorkspacePath = workspace
		}
		if findings[i].Status == "" {
			findings[i].Status = security.FindingActive
		}
	}
	return security.ScanResult{
		Findings:  findings,
		ScannedAt: a.now(),
		LockFiles: targets,
		Kind:      a.kind,
		Workspace: workspace,
		ToolName:  a.name,
	}, nil
}

func (a *baseAdapter) RenderFinding(f security.Finding) string {
	if a.render != nil {
		return a.render(f)
	}
	return renderGenericFinding(f)
}

func (a *baseAdapter) path() (string, error) {
	path, err := a.lookPath(a.binary)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotInstalled, a.binary)
	}
	return path, nil
}

// NewGitleaks returns an adapter for local secret scanning.
func NewGitleaks(opts ...Option) ScannerAdapter {
	a := newBaseAdapter("gitleaks", "gitleaks", security.ScanSecret, security.FindingSecret, opts...)
	a.scanArgs = func(workspace string, _ []string) []string {
		return []string{"detect", "--source", workspace, "--report-format", "json", "--no-banner", "--exit-code", "1"}
	}
	a.parse = parseGitleaks
	a.render = func(f security.Finding) string {
		loc := renderLocation(f)
		if loc != "" {
			return fmt.Sprintf("%s %s in %s", severityPrefix(f), fallback(f.Summary, f.ID), loc)
		}
		return fmt.Sprintf("%s %s", severityPrefix(f), fallback(f.Summary, f.ID))
	}
	return a
}

// NewSemgrep returns an adapter for local SAST scanning. The arguments avoid
// registry-dependent configs; callers can layer policy-specific configuration
// later without changing the adapter contract.
func NewSemgrep(opts ...Option) ScannerAdapter {
	a := newBaseAdapter("semgrep", "semgrep", security.ScanSAST, security.FindingSAST, opts...)
	a.scanArgs = func(workspace string, _ []string) []string {
		return []string{"scan", "--json", "--disable-version-check", "--metrics=off", "--config", "auto", workspace}
	}
	a.parse = parseSemgrep
	return a
}

// NewGovulncheck returns an adapter for Go vulnerability scanning.
func NewGovulncheck(opts ...Option) ScannerAdapter {
	a := newBaseAdapter("govulncheck", "govulncheck", security.ScanDependency, security.FindingDependency, opts...)
	a.scanArgs = func(workspace string, targets []string) []string {
		args := []string{"-json"}
		if len(targets) == 0 {
			return append(args, filepath.Join(workspace, "..."))
		}
		return append(args, targets...)
	}
	a.parse = parseGovulncheck
	a.render = renderDependencyFinding
	return a
}

// NewOSVScanner returns an adapter for osv-scanner dependency scanning.
func NewOSVScanner(opts ...Option) ScannerAdapter {
	a := newBaseAdapter("osv-scanner", "osv-scanner", security.ScanDependency, security.FindingDependency, opts...)
	a.scanArgs = func(_ string, targets []string) []string {
		args := []string{"--format", "json"}
		for _, target := range targets {
			args = append(args, "--lockfile", target)
		}
		return args
	}
	a.parse = parseOSVScanner
	a.render = renderDependencyFinding
	return a
}

func commandExec(ctx context.Context, path string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func isFindingsExit(err error) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() == 1
	}
	return false
}

func firstLine(stdout, stderr []byte) string {
	text := strings.TrimSpace(string(stdout))
	if text == "" {
		text = strings.TrimSpace(string(stderr))
	}
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}

func renderGenericFinding(f security.Finding) string {
	loc := renderLocation(f)
	if loc != "" {
		return fmt.Sprintf("%s %s in %s", severityPrefix(f), fallback(f.Summary, f.ID), loc)
	}
	return fmt.Sprintf("%s %s", severityPrefix(f), fallback(f.Summary, f.ID))
}

func renderDependencyFinding(f security.Finding) string {
	pkg := f.PackageName
	if f.InstalledVersion != "" {
		pkg += "@" + f.InstalledVersion
	}
	msg := fallback(f.CVEID, f.ID)
	if msg == "" {
		msg = fallback(f.Summary, pkg)
	}
	if f.FixedInVersion != "" {
		return fmt.Sprintf("%s %s in %s; fixed in %s", severityPrefix(f), msg, pkg, f.FixedInVersion)
	}
	return fmt.Sprintf("%s %s in %s", severityPrefix(f), msg, pkg)
}

func renderLocation(f security.Finding) string {
	path := fallback(f.FilePath, f.ScanSource)
	if path == "" {
		return ""
	}
	if f.Line > 0 && f.Column > 0 {
		return fmt.Sprintf("%s:%d:%d", path, f.Line, f.Column)
	}
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d", path, f.Line)
	}
	return path
}

func severityPrefix(f security.Finding) string {
	if f.Severity == "" {
		return "[UNKNOWN]"
	}
	return "[" + strings.ToUpper(f.Severity) + "]"
}

func fallback(primary, secondary string) string {
	if primary != "" {
		return primary
	}
	return secondary
}

func normaliseSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return "CRITICAL"
	case "high", "error":
		return "HIGH"
	case "medium", "moderate", "warning", "warn":
		return "MEDIUM"
	case "low", "info", "note":
		return "LOW"
	default:
		if s == "" {
			return "UNKNOWN"
		}
		return strings.ToUpper(s)
	}
}

func workspaceRel(workspace, path string) string {
	if workspace == "" || path == "" {
		return path
	}
	if rel, err := filepath.Rel(workspace, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

func cveFromAliases(id string, aliases []string) string {
	if strings.HasPrefix(id, "CVE-") {
		return id
	}
	for _, alias := range aliases {
		if strings.HasPrefix(alias, "CVE-") {
			return alias
		}
	}
	return id
}

func firstFixed(affected []struct {
	Ranges []struct {
		Events []struct {
			Fixed string `json:"fixed,omitempty"`
		} `json:"events"`
	} `json:"ranges"`
}) string {
	for _, a := range affected {
		for _, r := range a.Ranges {
			for _, e := range r.Events {
				if e.Fixed != "" {
					return e.Fixed
				}
			}
		}
	}
	return ""
}

func parseJSONLines(data []byte, handle func(json.RawMessage) error) error {
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if err := handle(json.RawMessage(line)); err != nil {
			return err
		}
	}
	return nil
}
