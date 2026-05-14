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

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mwigge/milliways/internal/security/cra/evidence"
	"github.com/mwigge/milliways/internal/security/sbom"
)

// handleSecurity implements the /security slash command surface.
func (l *chatLoop) handleSecurity(rest string) {
	args := splitFields(rest)
	if len(args) == 0 || args[0] == "help" {
		printSecurityUsage(l.out)
		return
	}
	if args[0] == "sbom" {
		l.handleSecuritySBOM(args[1:])
		return
	}
	if args[0] == "cra-scaffold" {
		l.handleSecurityCRAScaffold(args[1:])
		return
	}
	if l.client == nil {
		fmt.Fprintln(l.errw, "[security] not connected to daemon")
		return
	}

	verb := args[0]
	switch verb {
	case "status":
		if len(args) != 1 {
			fmt.Fprintln(l.errw, "usage: /security status")
			return
		}
		l.callSecurityRPC("security status", "security.status", map[string]any{}, renderSecurityStatus)
	case "cra":
		if len(args) != 1 {
			fmt.Fprintln(l.errw, "usage: /security cra")
			return
		}
		l.callSecurityRPC("security cra", "security.cra", map[string]any{}, renderSecurityCRA)
	case "scan":
		l.handleSecurityScan(args[1:])
	case "startup-scan":
		l.handleSecurityStartupScan(args[1:])
	case "mode":
		l.handleSecurityMode(args[1:])
	case "client":
		l.handleSecurityClient(args[1:])
	case "command-check":
		l.handleSecurityCommandCheck(args[1:])
	case "warnings":
		if len(args) != 1 {
			fmt.Fprintln(l.errw, "usage: /security warnings")
			return
		}
		l.callSecurityRPC("security warnings", "security.warnings", map[string]any{}, renderSecurityGeneric)
	default:
		fmt.Fprintf(l.errw, "unknown security command %q\n", verb)
		printSecurityUsage(l.errw)
	}
}

func (l *chatLoop) handleSecuritySBOM(args []string) {
	fs := flag.NewFlagSet("security sbom", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	workspace := fs.String("workspace", ".", "workspace root")
	output := fs.String("output", "", "output SPDX JSON path")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(l.errw, "security sbom: %v\n", err)
		return
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(l.errw, "usage: /security sbom [--workspace <dir>] [--output <path>]")
		return
	}
	doc, err := sbom.GenerateSPDX(sbom.GenerateOptions{Workspace: *workspace})
	if err != nil {
		fmt.Fprintf(l.errw, "security sbom: %v\n", err)
		return
	}
	if strings.TrimSpace(*output) == "" {
		if err := sbom.WriteSPDXJSON(l.out, doc); err != nil {
			fmt.Fprintf(l.errw, "security sbom: write stdout: %v\n", err)
		}
		return
	}
	if err := sbom.WriteSPDXJSONFile(*output, doc); err != nil {
		fmt.Fprintf(l.errw, "security sbom: %v\n", err)
		return
	}
	fmt.Fprintf(l.out, "[security] wrote SBOM -> %s (%d packages)\n", *output, len(doc.Packages))
}

func (l *chatLoop) handleSecurityStartupScan(args []string) {
	fs := flag.NewFlagSet("security startup-scan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	strict := fs.Bool("strict", false, "request strict startup posture checks")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(l.errw, "security startup-scan: %v\n", err)
		return
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(l.errw, "usage: /security startup-scan [--strict]")
		return
	}
	l.callSecurityRPC("security startup-scan", "security.startup_scan", map[string]any{
		"strict": *strict,
	}, renderSecurityGeneric)
}

func (l *chatLoop) handleSecurityScan(args []string) {
	fs := flag.NewFlagSet("security scan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	startup := fs.Bool("startup", false, "run startup posture checks before the dependency scan")
	client := fs.String("client", "", "run a per-client profile check before the dependency scan")
	diff := fs.Bool("diff", false, "request scan planning for the staged diff")
	staged := fs.Bool("staged", false, "request scan planning for the staged diff")
	secrets := fs.Bool("secrets", false, "include the secret scanning layer")
	sast := fs.Bool("sast", false, "include the SAST scanning layer")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(l.errw, "security scan: %v\n", err)
		return
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(l.errw, "usage: /security scan [--startup] [--client <name>] [--diff|--staged] [--secrets] [--sast]")
		return
	}
	if *startup {
		l.callSecurityRPC("security startup-scan", "security.startup_scan", map[string]any{
			"strict": false,
		}, renderSecurityGeneric)
	}
	if strings.TrimSpace(*client) != "" {
		l.callSecurityRPC("security client", "security.client_profile", map[string]any{
			"client": strings.TrimSpace(*client),
		}, renderSecurityGeneric)
	}
	l.callSecurityRPC("security scan", "security.scan", securityScanParams(*diff || *staged, *secrets, *sast), renderSecurityGeneric)
}

func (l *chatLoop) handleSecurityMode(args []string) {
	if len(args) > 1 {
		fmt.Fprintln(l.errw, "usage: /security mode [off|observe|warn|strict|ci]")
		return
	}
	params := map[string]any{}
	if len(args) == 1 {
		mode := args[0]
		if !validSecurityMode(mode) {
			fmt.Fprintf(l.errw, "security mode: invalid mode %q (want off, observe, warn, strict, or ci)\n", mode)
			return
		}
		params["mode"] = mode
	}
	l.callSecurityRPC("security mode", "security.mode", params, renderSecurityGeneric)
}

func (l *chatLoop) handleSecurityClient(args []string) {
	fs := flag.NewFlagSet("security client", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(l.errw, "security client: %v\n", err)
		return
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(l.errw, "usage: /security client <name>")
		return
	}
	l.callSecurityRPC("security client", "security.client_profile", map[string]any{
		"client": fs.Arg(0),
	}, renderSecurityGeneric)
}

func (l *chatLoop) handleSecurityCommandCheck(args []string) {
	fs := flag.NewFlagSet("security command-check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	mode := fs.String("mode", "", "security mode override")
	cwd := fs.String("cwd", "", "working directory")
	client := fs.String("client", "", "client name")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(l.errw, "security command-check: %v\n", err)
		return
	}
	if *mode != "" && !validSecurityMode(*mode) {
		fmt.Fprintf(l.errw, "security command-check: invalid mode %q (want off, observe, warn, strict, or ci)\n", *mode)
		return
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(l.errw, "usage: /security command-check [--mode <mode>] [--cwd <dir>] [--client <name>] -- <command...>")
		return
	}
	params := map[string]any{"command": strings.Join(fs.Args(), " ")}
	if *mode != "" {
		params["mode"] = *mode
	}
	if *cwd != "" {
		params["cwd"] = *cwd
	}
	if *client != "" {
		params["client"] = *client
	}
	l.callSecurityRPC("security command-check", "security.command_check", params, renderSecurityGeneric)
}

func (l *chatLoop) callSecurityRPC(label, method string, params map[string]any, render func(io.Writer, string, map[string]any)) {
	var result map[string]any
	if err := l.client.Call(method, params, &result); err != nil {
		fmt.Fprintln(l.errw, friendlyError("["+label+"] ", "", err))
		return
	}
	render(l.out, label, result)
}

func printSecurityUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: /security <command>")
	fmt.Fprintln(w, "  /security status")
	fmt.Fprintln(w, "  /security cra")
	fmt.Fprintln(w, "  /security cra-scaffold [--workspace <dir>] [--dry-run] [--force]")
	fmt.Fprintln(w, "  /security sbom [--workspace <dir>] [--output <path>]")
	fmt.Fprintln(w, "  /security scan [--startup] [--client <name>] [--diff|--staged] [--secrets] [--sast]")
	fmt.Fprintln(w, "  /security startup-scan [--strict]")
	fmt.Fprintln(w, "  /security mode [off|observe|warn|strict|ci]")
	fmt.Fprintln(w, "  /security client <name>")
	fmt.Fprintln(w, "  /security command-check [--mode <mode>] [--cwd <dir>] [--client <name>] -- <command...>")
	fmt.Fprintln(w, "  /security warnings")
}

func securityScanParams(staged, secrets, sast bool) map[string]any {
	params := map[string]any{}
	var layers []string
	if secrets {
		layers = append(layers, "secret")
	}
	if sast {
		layers = append(layers, "sast")
	}
	if len(layers) > 0 {
		layers = append(layers, "dependency")
		params["layers"] = layers
	}
	if staged {
		params["diff"] = "staged"
		params["staged"] = true
	}
	return params
}

func (l *chatLoop) handleSecurityCRAScaffold(args []string) {
	fs := flag.NewFlagSet("security cra-scaffold", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	workspace := fs.String("workspace", ".", "workspace root")
	dryRun := fs.Bool("dry-run", false, "show files that would be created without writing them")
	force := fs.Bool("force", false, "overwrite existing scaffold files")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(l.errw, "security cra-scaffold: %v\n", err)
		return
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(l.errw, "usage: /security cra-scaffold [--workspace <dir>] [--dry-run] [--force]")
		return
	}
	result, err := evidence.Scaffold(evidence.Options{Workspace: *workspace, DryRun: *dryRun, Force: *force})
	if err != nil {
		fmt.Fprintf(l.errw, "security cra-scaffold: %v\n", err)
		return
	}
	renderCRAScaffoldResult(l.out, result, *dryRun)
}

func renderCRAScaffoldResult(stdout io.Writer, result evidence.Result, dryRun bool) {
	label := "[security] CRA evidence scaffold"
	if dryRun {
		label += " (dry-run)"
	}
	fmt.Fprintf(stdout, "%s workspace: %s\n", label, result.Workspace)
	for _, action := range result.Actions {
		status := action.Status
		if dryRun {
			switch action.Status {
			case "create":
				status = "would create"
			case "overwrite":
				status = "would overwrite"
			}
		}
		fmt.Fprintf(stdout, "%s %s\n", status, action.RelPath)
	}
	fmt.Fprintf(stdout, "[security] %d created, %d existing, %d overwritten\n", result.Created, result.Existing, result.Overwritten)
}

func validSecurityMode(mode string) bool {
	switch mode {
	case "off", "observe", "warn", "strict", "ci":
		return true
	default:
		return false
	}
}

func renderSecurityStatus(stdout io.Writer, label string, result map[string]any) {
	mode := stringField(result, "mode")
	posture := firstStringField(result, "state", "posture", "level")
	warnCount := intSecurityField(result, "warnings", "warn_count", "warning_count")
	blockCount := intSecurityField(result, "blocks", "block_count", "blocked_count")

	var summary []string
	if mode != "" {
		summary = append(summary, "mode: "+mode)
	}
	if posture != "" {
		summary = append(summary, "posture: "+strings.ToUpper(posture))
	}
	summary = append(summary, fmt.Sprintf("warnings: %d", warnCount), fmt.Sprintf("blocks: %d", blockCount))
	fmt.Fprintf(stdout, "[%s] %s\n", label, strings.Join(summary, "  "))

	if scanners := renderSecurityScanners(result["scanners"]); scanners != "" {
		fmt.Fprintf(stdout, "scanners: %s\n", scanners)
	}
	if lastStartup := firstStringField(result, "last_startup_scan", "last_startup_scan_at"); lastStartup != "" {
		fmt.Fprintf(stdout, "last startup scan: %s\n", lastStartup)
	}
	if lastDependency := firstStringField(result, "last_dependency_scan", "last_dependency_scan_at", "scanned_at"); lastDependency != "" {
		fmt.Fprintf(stdout, "last dependency scan: %s\n", lastDependency)
	}
	if cra, _ := result["cra"].(map[string]any); len(cra) > 0 {
		fmt.Fprintf(stdout, "cra: %s\n", formatSecurityCRASummary(cra))
	}
}

func renderSecurityCRA(stdout io.Writer, label string, result map[string]any) {
	summary, _ := result["summary"].(map[string]any)
	fmt.Fprintln(stdout, "[security] CRA readiness")
	if workspace := stringField(result, "workspace"); workspace != "" {
		fmt.Fprintf(stdout, "workspace: %s\n", workspace)
	}
	fmt.Fprintf(stdout, "evidence: %s\n", formatSecurityCRAEvidence(summary))
	fmt.Fprintf(stdout, "vulnerability/reporting: %s\n", formatSecurityCRAReporting(summary))
	design := stringField(summary, "design_evidence_status")
	if design == "" {
		design = "missing"
	}
	fmt.Fprintf(stdout, "design evidence: %s\n", design)
	if deadline := stringField(summary, "reporting_deadline"); deadline != "" {
		fmt.Fprintf(stdout, "Article 14 reporting: %s\n", deadline)
	}
	checks, _ := result["checks"].([]any)
	if len(checks) == 0 {
		return
	}
	fmt.Fprintln(stdout, "checks:")
	for i, raw := range checks {
		if i >= 5 {
			fmt.Fprintf(stdout, "  ... %d more\n", len(checks)-i)
			break
		}
		check, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fmt.Fprintf(stdout, "  %s  %s — %s\n", securityCRAStatusMark(stringField(check, "status")), stringField(check, "id"), stringField(check, "title"))
		if missing := stringListField(check, "missing_evidence"); len(missing) > 0 {
			fmt.Fprintf(stdout, "      missing: %s\n", strings.Join(missing, ", "))
		}
		for _, action := range stringListField(check, "next_actions") {
			fmt.Fprintf(stdout, "      next: %s\n", action)
		}
	}
}

func formatSecurityCRASummary(cra map[string]any) string {
	return strings.Join([]string{
		formatSecurityCRAEvidence(cra),
		formatSecurityCRAReporting(cra),
		"design " + firstNonEmptyString(stringField(cra, "design_evidence_status"), "missing"),
		"Article 14 " + firstNonEmptyString(stringField(cra, "reporting_deadline"), "2026-09-11"),
	}, ", ")
}

func formatSecurityCRAEvidence(summary map[string]any) string {
	score := intSecurityField(summary, "evidence_score")
	present := intSecurityField(summary, "checks_present")
	total := intSecurityField(summary, "checks_total")
	partial := intSecurityField(summary, "checks_partial")
	missing := intSecurityField(summary, "checks_missing")
	return fmt.Sprintf("%d%% (%d/%d present, %d partial, %d missing)", score, present, total, partial, missing)
}

func formatSecurityCRAReporting(summary map[string]any) string {
	present := intSecurityField(summary, "reporting_present")
	total := intSecurityField(summary, "reporting_total")
	ready := "not ready"
	if ok, _ := summary["reporting_ready"].(bool); ok {
		ready = "ready"
	}
	return fmt.Sprintf("%d/%d %s", present, total, ready)
}

func securityCRAStatusMark(status string) string {
	switch status {
	case "present":
		return "OK"
	case "partial":
		return "WARN"
	default:
		return "MISS"
	}
}

func firstNonEmptyString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func renderSecurityGeneric(stdout io.Writer, label string, result map[string]any) {
	if len(result) == 0 {
		fmt.Fprintf(stdout, "[%s] ok\n", label)
		return
	}
	if decision := stringField(result, "decision"); decision != "" {
		fmt.Fprintf(stdout, "[%s] decision: %s\n", label, decision)
		if reason := stringField(result, "reason"); reason != "" {
			fmt.Fprintf(stdout, "reason: %s\n", reason)
		}
		if risks := stringListField(result, "risk_categories"); len(risks) > 0 {
			fmt.Fprintf(stdout, "risks: %s\n", strings.Join(risks, ", "))
		}
		return
	}
	if warnings, ok := result["warnings"].([]any); ok {
		fmt.Fprintf(stdout, "[%s] %d warning(s)\n", label, len(warnings))
		for i, item := range warnings {
			if i >= 3 {
				fmt.Fprintf(stdout, "... %d more\n", len(warnings)-i)
				break
			}
			if s, ok := item.(string); ok && s != "" {
				fmt.Fprintf(stdout, "- %s\n", s)
			} else if m, ok := item.(map[string]any); ok {
				msg := firstStringField(m, "message", "summary", "reason")
				severity := strings.ToUpper(stringField(m, "severity"))
				if msg != "" && severity != "" {
					fmt.Fprintf(stdout, "- %s: %s\n", severity, msg)
				} else if msg != "" {
					fmt.Fprintf(stdout, "- %s\n", msg)
				}
			}
		}
		return
	}
	if findings, ok := result["findings"].([]any); ok {
		fmt.Fprintf(stdout, "[%s] %d finding(s)\n", label, len(findings))
		return
	}
	if mode := stringField(result, "mode"); mode != "" {
		fmt.Fprintf(stdout, "[%s] mode: %s\n", label, mode)
		return
	}
	if ok, _ := result["ok"].(bool); ok {
		fmt.Fprintf(stdout, "[%s] ok\n", label)
		return
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Fprintln(stdout, string(out))
}

func firstStringField(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v := stringField(m, key); v != "" {
			return v
		}
	}
	return ""
}

func intSecurityField(m map[string]any, keys ...string) int {
	for _, key := range keys {
		switch v := m[key].(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case []any:
			return len(v)
		}
	}
	return 0
}

func stringListField(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func renderSecurityScanners(raw any) string {
	scanners, ok := raw.([]any)
	if !ok || len(scanners) == 0 {
		return ""
	}
	var installed []string
	var missing []string
	for _, item := range scanners {
		scanner, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringField(scanner, "name")
		if name == "" {
			continue
		}
		if ok, _ := scanner["installed"].(bool); ok {
			if version := stringField(scanner, "version"); version != "" {
				installed = append(installed, fmt.Sprintf("%s (%s)", name, version))
			} else {
				installed = append(installed, name)
			}
		} else {
			missing = append(missing, name)
		}
	}
	var parts []string
	if len(installed) > 0 {
		parts = append(parts, "installed "+strings.Join(installed, ", "))
	}
	if len(missing) > 0 {
		parts = append(parts, "missing "+strings.Join(missing, ", "))
	}
	return strings.Join(parts, "; ")
}
