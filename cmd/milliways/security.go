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
)

// handleSecurity implements the /security slash command surface.
func (l *chatLoop) handleSecurity(rest string) {
	args := splitFields(rest)
	if len(args) == 0 || args[0] == "help" {
		printSecurityUsage(l.out)
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
	fmt.Fprintln(w, "  /security client <name>")
	fmt.Fprintln(w, "  /security command-check [--mode <mode>] [--cwd <dir>] [--client <name>] -- <command...>")
	fmt.Fprintln(w, "  /security warnings")
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
