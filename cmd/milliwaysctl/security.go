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

// `milliwaysctl security <verb>` — CVE/OSV finding management.
//
// Verbs:
//   list [--include-accepted]          — list active security findings
//   show <cve-id>                      — show full CVE detail
//   accept <cve-id> --package <name>   — accept / suppress a CVE
//             --reason <text>
//             --expires <YYYY-MM-DD>

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/rpc"
	"github.com/mwigge/milliways/internal/security/outputgate"
)

// runSecurity dispatches `milliwaysctl security <verb> [args...]`.
// socketOverride is an optional UDS path used in tests (avoids defaultSocket()).
func runSecurity(args []string, stdout, stderr io.Writer, socketOverride ...string) int {
	if len(args) == 0 {
		printSecurityUsage(stderr)
		return 2
	}

	sock := ""
	if len(socketOverride) > 0 && socketOverride[0] != "" {
		sock = socketOverride[0]
	} else {
		sock = defaultSocket()
	}

	verb := args[0]
	rest := args[1:]

	switch verb {
	case "-h", "--help", "help":
		printSecurityUsage(stdout)
		return 0
	case "list":
		return runSecurityList(rest, stdout, stderr, sock)
	case "show":
		return runSecurityShow(rest, stdout, stderr, sock)
	case "accept":
		return runSecurityAccept(rest, stdout, stderr, sock)
	case "enable":
		return runSecurityToggle(true, stdout, stderr, sock)
	case "disable":
		return runSecurityToggle(false, stdout, stderr, sock)
	case "status":
		return runSecurityStatusCmd(stdout, stderr, sock)
	case "scan":
		return runSecurityScan(rest, stdout, stderr, sock)
	case "startup-scan":
		return runSecurityStartupScan(rest, stdout, stderr, sock)
	case "warnings":
		return runSecurityWarnings(rest, stdout, stderr, sock)
	case "mode":
		return runSecurityMode(rest, stdout, stderr, sock)
	case "client":
		return runSecurityClient(rest, stdout, stderr, sock)
	case "command-check":
		return runSecurityCommandCheck(rest, stdout, stderr, sock)
	case "harden":
		return runSecurityHarden(rest, stdout, stderr)
	case "quarantine":
		return runSecurityQuarantine(rest, stdout, stderr, sock)
	case "rules":
		return runSecurityRules(rest, stdout, stderr, sock)
	case "output-plan":
		return runSecurityOutputPlan(rest, stdout, stderr)
	case "install-scanner":
		return runInstallScanner(stdout, stderr)
	default:
		fmt.Fprintf(stderr, "milliwaysctl security: unknown subcommand %q\n", verb)
		printSecurityUsage(stderr)
		return 2
	}
}

func printSecurityUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: milliwaysctl security <subcommand>")
	fmt.Fprintln(w, "  list [--include-accepted]")
	fmt.Fprintln(w, "    list active security findings (CVE ID | PACKAGE | VERSION | FIXED IN | SEVERITY | FIRST SEEN)")
	fmt.Fprintln(w, "  show <cve-id>")
	fmt.Fprintln(w, "    show full detail for a CVE")
	fmt.Fprintln(w, "  accept <cve-id> --package <name> --reason <text> --expires <YYYY-MM-DD>")
	fmt.Fprintln(w, "    accept / suppress a CVE finding (expiry ≤ 365 days)")
	fmt.Fprintln(w, "  enable                 enable OSV security scanning")
	fmt.Fprintln(w, "  disable                disable OSV security scanning")
	fmt.Fprintln(w, "  status                 show scanner status (enabled, installed, path)")
	fmt.Fprintln(w, "  scan [--json]          run dependency security scan")
	fmt.Fprintln(w, "  startup-scan [--json] [--strict]")
	fmt.Fprintln(w, "    run startup posture scan when supported by the daemon")
	fmt.Fprintln(w, "  warnings [--json]      show active security warnings")
	fmt.Fprintln(w, "  mode [off|observe|warn|strict|ci]")
	fmt.Fprintln(w, "    show or set MilliWays security policy mode")
	fmt.Fprintln(w, "  client <name> [--json]")
	fmt.Fprintln(w, "    run a per-client security profile check")
	fmt.Fprintln(w, "  command-check [--mode <mode>] [--cwd <dir>] [--client <name>] [--json] -- <command...>")
	fmt.Fprintln(w, "    evaluate a command with the Secure MilliWays firewall")
	fmt.Fprintln(w, "  harden npm [--dry-run|--apply] [--path <.npmrc>]")
	fmt.Fprintln(w, "    preview or write safer npm defaults")
	fmt.Fprintln(w, "  quarantine [--dry-run|--apply] [--json]")
	fmt.Fprintln(w, "    plan or apply quarantine actions when supported by the daemon")
	fmt.Fprintln(w, "  rules list|update [--json]")
	fmt.Fprintln(w, "    list or update security rule packs when supported by the daemon")
	fmt.Fprintln(w, "  output-plan [--json] [--generated <path> ...] [--staged <path> ...]")
	fmt.Fprintln(w, "    classify output or diff paths into requested scan types without running scanners")
	fmt.Fprintln(w, "  install-scanner        install osv-scanner via 'go install'")
}

// securityListResult is the wire type for security.list RPC result.
type securityListResult struct {
	Findings []securityFindingWire `json:"findings"`
}

// securityFindingWire mirrors the server-side wire type.
type securityFindingWire struct {
	CVEID            string `json:"cve_id"`
	PackageName      string `json:"package_name"`
	InstalledVersion string `json:"installed_version"`
	FixedInVersion   string `json:"fixed_in_version"`
	Severity         string `json:"severity"`
	Summary          string `json:"summary"`
	FirstSeen        string `json:"first_seen"`
	LastSeen         string `json:"last_seen"`
	Accepted         bool   `json:"accepted"`
}

// runSecurityList handles `security list [--include-accepted]`.
func runSecurityList(args []string, stdout, stderr io.Writer, sock string) int {
	fs := flag.NewFlagSet("security list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	includeAccepted := fs.Bool("include-accepted", false, "include accepted/suppressed findings")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	c, err := rpc.Dial(sock)
	if err != nil {
		fmt.Fprintf(stderr, "security list: dial %s: %v\n", sock, err)
		return 1
	}
	defer func() { _ = c.Close() }()

	var result securityListResult
	if err := c.Call("security.list", map[string]any{
		"include_accepted": *includeAccepted,
	}, &result); err != nil {
		fmt.Fprintf(stderr, "security list: %v\n", err)
		return 1
	}

	if len(result.Findings) == 0 {
		fmt.Fprintln(stdout, "no active security findings")
		return 0
	}

	// Render table header.
	fmt.Fprintf(stdout, "%-20s  %-30s  %-12s  %-12s  %-10s  %s\n",
		"CVE ID", "PACKAGE", "VERSION", "FIXED IN", "SEVERITY", "FIRST SEEN")
	fmt.Fprintln(stdout, strings.Repeat("-", 110))

	for _, f := range result.Findings {
		marker := ""
		if f.Accepted {
			marker = " [accepted]"
		}
		firstSeen := f.FirstSeen
		if len(firstSeen) > 10 {
			firstSeen = firstSeen[:10]
		}
		fmt.Fprintf(stdout, "%-20s  %-30s  %-12s  %-12s  %-10s  %s%s\n",
			f.CVEID,
			truncateStr(f.PackageName, 30),
			truncateStr(f.InstalledVersion, 12),
			truncateStr(f.FixedInVersion, 12),
			f.Severity,
			firstSeen,
			marker,
		)
	}
	return 0
}

// securityShowResult is the wire type for security.show RPC result.
type securityShowResult struct {
	Finding securityFindingWire `json:"finding"`
}

// runSecurityShow handles `security show <cve-id>`.
func runSecurityShow(args []string, stdout, stderr io.Writer, sock string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "security show: cve-id required")
		return 1
	}
	cveID := args[0]

	c, err := rpc.Dial(sock)
	if err != nil {
		fmt.Fprintf(stderr, "security show: dial %s: %v\n", sock, err)
		return 1
	}
	defer func() { _ = c.Close() }()

	var result securityShowResult
	if err := c.Call("security.show", map[string]any{"cve_id": cveID}, &result); err != nil {
		fmt.Fprintf(stderr, "security show: %v\n", err)
		return 1
	}

	f := result.Finding
	fmt.Fprintf(stdout, "CVE ID:           %s\n", f.CVEID)
	fmt.Fprintf(stdout, "Package:          %s\n", f.PackageName)
	fmt.Fprintf(stdout, "Installed:        %s\n", f.InstalledVersion)
	fmt.Fprintf(stdout, "Fixed in:         %s\n", f.FixedInVersion)
	fmt.Fprintf(stdout, "Severity:         %s\n", f.Severity)
	fmt.Fprintf(stdout, "Summary:          %s\n", f.Summary)
	fmt.Fprintf(stdout, "First seen:       %s\n", f.FirstSeen)
	fmt.Fprintf(stdout, "Last seen:        %s\n", f.LastSeen)
	return 0
}

// runSecurityAccept handles `security accept <cve-id> --package <name> --reason <text> --expires <date>`.
func runSecurityAccept(args []string, stdout, stderr io.Writer, sock string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "security accept: cve-id required")
		return 1
	}

	cveID := args[0]
	rest := args[1:]

	fs := flag.NewFlagSet("security accept", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pkg := fs.String("package", "", "package name")
	reason := fs.String("reason", "", "reason for accepting this CVE")
	expires := fs.String("expires", "", "expiry date (YYYY-MM-DD); max 365 days from today")
	if err := fs.Parse(rest); err != nil {
		return 1
	}

	// Validate required flags.
	if *pkg == "" {
		fmt.Fprintln(stderr, "security accept: --package is required")
		return 1
	}
	if *reason == "" {
		fmt.Fprintln(stderr, "security accept: --reason is required")
		return 1
	}
	if *expires == "" {
		fmt.Fprintln(stderr, "security accept: --expires is required")
		return 1
	}

	// Parse and validate expiry date.
	expiryDate, err := time.Parse("2006-01-02", *expires)
	if err != nil {
		fmt.Fprintf(stderr, "security accept: --expires must be YYYY-MM-DD: %v\n", err)
		return 1
	}

	maxExpiry := time.Now().Add(365 * 24 * time.Hour)
	if expiryDate.After(maxExpiry) {
		fmt.Fprintf(stderr,
			"security accept: --expires exceeds the maximum of 365 days from today (%s)\n",
			maxExpiry.UTC().Format("2006-01-02"))
		return 1
	}

	c, err := rpc.Dial(sock)
	if err != nil {
		fmt.Fprintf(stderr, "security accept: dial %s: %v\n", sock, err)
		return 1
	}
	defer func() { _ = c.Close() }()

	// Check that the CVE exists before accepting.
	var existsResult struct {
		Exists bool `json:"exists"`
	}
	if err := c.Call("security.exists", map[string]any{"cve_id": cveID}, &existsResult); err != nil {
		fmt.Fprintf(stderr, "security accept: check CVE: %v\n", err)
		return 1
	}
	if !existsResult.Exists {
		fmt.Fprintf(stderr, "security accept: CVE %q not found in findings\n", cveID)
		return 1
	}

	expiresAt := expiryDate.UTC().Format(time.RFC3339)
	var acceptResult map[string]any
	if err := c.Call("security.accept", map[string]any{
		"cve_id":       cveID,
		"package_name": *pkg,
		"reason":       *reason,
		"expires_at":   expiresAt,
	}, &acceptResult); err != nil {
		fmt.Fprintf(stderr, "security accept: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "[accepted] %s suppressed until %s\n", cveID, *expires)
	return 0
}

// truncateStr truncates s to at most n bytes.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// runSecurityToggle enables or disables OSV scanning via the daemon.
func runSecurityToggle(enable bool, stdout, stderr io.Writer, sock string) int {
	c, err := rpc.Dial(sock)
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl security: dial %s: %v\n", sock, err)
		return 1
	}
	defer func() { _ = c.Close() }()

	method := "security.disable"
	word := "disabled"
	if enable {
		method = "security.enable"
		word = "enabled"
	}

	var result map[string]any
	if err := c.Call(method, map[string]any{}, &result); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl security %s: %v\n", word, err)
		return 1
	}
	fmt.Fprintf(stdout, "[security] scanning %s\n", word)
	return 0
}

// runSecurityStatusCmd prints scanner status.
func runSecurityStatusCmd(stdout, stderr io.Writer, sock string) int {
	c, err := rpc.Dial(sock)
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl security status: dial %s: %v\n", sock, err)
		return 1
	}
	defer func() { _ = c.Close() }()

	var result map[string]any
	if err := c.Call("security.status", map[string]any{}, &result); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl security status: %v\n", err)
		return 1
	}

	installed, _ := result["installed"].(bool)
	enabled, _ := result["enabled"].(bool)
	path, _ := result["scanner_path"].(string)
	mode := stringMapField(result, "mode")
	state := firstStringField(result, "state", "posture", "level")
	lastStartup := firstStringField(result, "last_startup_scan", "last_startup_scan_at")
	lastDependency := firstStringField(result, "last_dependency_scan", "last_dependency_scan_at", "scanned_at")
	warnCount := intMapField(result, "warnings", "warn_count", "warning_count")
	blockCount := intMapField(result, "blocks", "block_count", "blocked_count")

	if !installed {
		fmt.Fprintln(stdout, "[security] osv-scanner: not installed")
		fmt.Fprintln(stdout, "  install: milliwaysctl security install-scanner")
	} else {
		fmt.Fprintf(stdout, "[security] osv-scanner: installed (%s)\n", path)
	}
	if enabled {
		fmt.Fprintln(stdout, "[security] scanning: enabled")
	} else {
		fmt.Fprintln(stdout, "[security] scanning: disabled  (enable: milliwaysctl security enable)")
	}
	if scanners := renderSecurityScanners(result["scanners"]); scanners != "" {
		fmt.Fprintf(stdout, "[security] scanners: %s\n", scanners)
	}
	if mode != "" {
		fmt.Fprintf(stdout, "[security] mode: %s\n", mode)
	}
	if state != "" {
		fmt.Fprintf(stdout, "[security] posture: %s\n", strings.ToUpper(state))
	}
	if warnCount > 0 || blockCount > 0 {
		fmt.Fprintf(stdout, "[security] warnings: %d  blocks: %d\n", warnCount, blockCount)
	}
	if lastStartup != "" {
		fmt.Fprintf(stdout, "[security] last startup scan: %s\n", lastStartup)
	}
	if lastDependency != "" {
		fmt.Fprintf(stdout, "[security] last dependency scan: %s\n", lastDependency)
	}
	return 0
}

func runSecurityScan(args []string, stdout, stderr io.Writer, sock string) int {
	fs := flag.NewFlagSet("security scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw JSON result")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	return callSecurityRPC("security scan", "security.scan", map[string]any{}, *asJSON, stdout, stderr, sock)
}

func runSecurityStartupScan(args []string, stdout, stderr io.Writer, sock string) int {
	fs := flag.NewFlagSet("security startup-scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw JSON result")
	strict := fs.Bool("strict", false, "request strict startup posture checks")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	return callSecurityRPC("security startup-scan", "security.startup_scan", map[string]any{
		"strict": *strict,
	}, *asJSON, stdout, stderr, sock)
}

func runSecurityWarnings(args []string, stdout, stderr io.Writer, sock string) int {
	fs := flag.NewFlagSet("security warnings", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw JSON result")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	return callSecurityRPC("security warnings", "security.warnings", map[string]any{}, *asJSON, stdout, stderr, sock)
}

func runSecurityMode(args []string, stdout, stderr io.Writer, sock string) int {
	if len(args) > 1 {
		fmt.Fprintln(stderr, "security mode: expected zero args or one of off|observe|warn|strict|ci")
		return 1
	}
	params := map[string]any{}
	if len(args) == 1 {
		mode := args[0]
		if !validSecurityMode(mode) {
			fmt.Fprintf(stderr, "security mode: invalid mode %q (want off, observe, warn, strict, or ci)\n", mode)
			return 1
		}
		params["mode"] = mode
	}
	return callSecurityRPC("security mode", "security.mode", params, false, stdout, stderr, sock)
}

func runSecurityClient(args []string, stdout, stderr io.Writer, sock string) int {
	fs := flag.NewFlagSet("security client", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw JSON result")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "security client: expected client name")
		return 1
	}
	client := fs.Arg(0)
	return callSecurityRPC("security client", "security.client_profile", map[string]any{
		"client": client,
	}, *asJSON, stdout, stderr, sock)
}

func runSecurityCommandCheck(args []string, stdout, stderr io.Writer, sock string) int {
	fs := flag.NewFlagSet("security command-check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	mode := fs.String("mode", "", "security mode override: off, observe, warn, strict, or ci")
	cwd := fs.String("cwd", "", "working directory for command evaluation")
	client := fs.String("client", "", "client name")
	asJSON := fs.Bool("json", false, "print raw JSON result")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *mode != "" && !validSecurityMode(*mode) {
		fmt.Fprintf(stderr, "security command-check: invalid mode %q (want off, observe, warn, strict, or ci)\n", *mode)
		return 1
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(stderr, "security command-check: expected command after --")
		return 1
	}
	params := map[string]any{
		"command": strings.Join(fs.Args(), " "),
	}
	if *mode != "" {
		params["mode"] = *mode
	}
	if *cwd != "" {
		params["cwd"] = *cwd
	}
	if *client != "" {
		params["client"] = *client
	}
	return callSecurityRPC("security command-check", "security.command_check", params, *asJSON, stdout, stderr, sock)
}

func validSecurityMode(mode string) bool {
	switch mode {
	case "off", "observe", "warn", "strict", "ci":
		return true
	default:
		return false
	}
}

func runSecurityQuarantine(args []string, stdout, stderr io.Writer, sock string) int {
	fs := flag.NewFlagSet("security quarantine", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw JSON result")
	dryRun := fs.Bool("dry-run", true, "plan actions without changing files")
	apply := fs.Bool("apply", false, "apply planned quarantine actions")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *apply {
		*dryRun = false
	}
	return callSecurityRPC("security quarantine", "security.quarantine", map[string]any{
		"dry_run": *dryRun,
		"apply":   *apply,
	}, *asJSON, stdout, stderr, sock)
}

func runSecurityRules(args []string, stdout, stderr io.Writer, sock string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "security rules: expected list or update")
		return 1
	}
	verb := args[0]
	rest := args[1:]
	fs := flag.NewFlagSet("security rules "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw JSON result")
	if err := fs.Parse(rest); err != nil {
		return 1
	}
	switch verb {
	case "list":
		return callSecurityRPC("security rules list", "security.rules_list", map[string]any{}, *asJSON, stdout, stderr, sock)
	case "update":
		return callSecurityRPC("security rules update", "security.rules_update", map[string]any{}, *asJSON, stdout, stderr, sock)
	default:
		fmt.Fprintf(stderr, "security rules: unknown action %q\n", verb)
		return 1
	}
}

type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func (f *repeatedStringFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func runSecurityOutputPlan(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("security output-plan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON scan plan")
	var generated repeatedStringFlag
	var staged repeatedStringFlag
	fs.Var(&generated, "generated", "generated output path to classify; may be repeated")
	fs.Var(&staged, "staged", "staged diff path to classify; may be repeated")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "security output-plan: unexpected positional path %q; use --generated or --staged\n", fs.Arg(0))
		return 1
	}

	changes := make([]outputgate.FileChange, 0, len(generated)+len(staged))
	for _, path := range generated {
		changes = append(changes, outputgate.FileChange{
			Path:   path,
			Status: outputgate.StatusModified,
			Source: outputgate.SourceGenerated,
		})
	}
	for _, path := range staged {
		changes = append(changes, outputgate.FileChange{
			Path:   path,
			Status: outputgate.StatusModified,
			Source: outputgate.SourceStaged,
		})
	}

	plan := outputgate.PlanScans(changes)
	if *asJSON {
		out, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "security output-plan: encode plan: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(out))
		return 0
	}

	renderSecurityOutputPlan(stdout, plan)
	return 0
}

func renderSecurityOutputPlan(stdout io.Writer, plan outputgate.Plan) {
	if len(plan.Requests) == 0 {
		fmt.Fprintln(stdout, "[security output-plan] no scans requested")
		return
	}
	fmt.Fprintf(stdout, "[security output-plan] %d scan request(s)\n", len(plan.Requests))
	for _, req := range plan.Requests {
		fmt.Fprintf(stdout, "  %s: %s\n", req.Kind, strings.Join(req.Files, ", "))
		if req.Reason != "" {
			fmt.Fprintf(stdout, "    reason: %s\n", req.Reason)
		}
	}
}

func callSecurityRPC(label, method string, params map[string]any, asJSON bool, stdout, stderr io.Writer, sock string) int {
	c, err := rpc.Dial(sock)
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl %s: dial %s: %v\n", label, sock, err)
		return 1
	}
	defer func() { _ = c.Close() }()

	var result map[string]any
	if err := c.Call(method, params, &result); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl %s: %v\n", label, err)
		if strings.Contains(err.Error(), "method not found") || strings.Contains(err.Error(), "-32601") {
			fmt.Fprintln(stderr, "  this command surface is present in milliwaysctl and will activate when milliwaysd exposes the matching Secure MilliWays RPC")
		}
		return 1
	}
	if asJSON {
		out, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "milliwaysctl %s: encode result: %v\n", label, err)
			return 1
		}
		fmt.Fprintln(stdout, string(out))
		return 0
	}
	renderSecurityGenericResult(stdout, label, result)
	return 0
}

func renderSecurityGenericResult(stdout io.Writer, label string, result map[string]any) {
	if len(result) == 0 {
		fmt.Fprintf(stdout, "[%s] ok\n", label)
		return
	}
	if mode := stringMapField(result, "mode"); mode != "" {
		fmt.Fprintf(stdout, "[%s] mode: %s\n", label, mode)
		return
	}
	if findings, ok := result["findings"].([]any); ok {
		fmt.Fprintf(stdout, "[%s] %d finding(s)\n", label, len(findings))
		return
	}
	if warnings, ok := result["warnings"].([]any); ok {
		fmt.Fprintf(stdout, "[%s] %d warning(s)\n", label, len(warnings))
		return
	}
	if decision := stringMapField(result, "decision"); decision != "" {
		fmt.Fprintf(stdout, "[%s] decision: %s\n", label, decision)
		if reason := stringMapField(result, "reason"); reason != "" {
			fmt.Fprintf(stdout, "reason: %s\n", reason)
		}
		if categories, ok := result["risk_categories"].([]any); ok && len(categories) > 0 {
			parts := make([]string, 0, len(categories))
			for _, category := range categories {
				if s, ok := category.(string); ok {
					parts = append(parts, s)
				}
			}
			if len(parts) > 0 {
				fmt.Fprintf(stdout, "risks: %s\n", strings.Join(parts, ", "))
			}
		}
		return
	}
	if actions, ok := result["actions"].([]any); ok {
		fmt.Fprintf(stdout, "[%s] %d action(s)\n", label, len(actions))
		return
	}
	if ok, _ := result["ok"].(bool); ok {
		fmt.Fprintf(stdout, "[%s] ok\n", label)
		return
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Fprintln(stdout, string(out))
}

func runSecurityHarden(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "security harden: expected npm")
		return 1
	}
	if args[0] != "npm" {
		fmt.Fprintf(stderr, "security harden: unsupported target %q (want npm)\n", args[0])
		return 1
	}
	return runSecurityHardenNPM(args[1:], stdout, stderr)
}

func runSecurityHardenNPM(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("security harden npm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", true, "preview changes without writing")
	apply := fs.Bool("apply", false, "write safer npm defaults")
	path := fs.String("path", ".npmrc", "npm config path")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *apply {
		*dryRun = false
	}

	settings := []string{
		"ignore-scripts=true",
		"audit=true",
		"fund=false",
		"package-lock=true",
	}
	if *dryRun {
		fmt.Fprintf(stdout, "[security harden npm] dry-run for %s\n", *path)
		for _, line := range settings {
			fmt.Fprintf(stdout, "  ensure: %s\n", line)
		}
		fmt.Fprintln(stdout, "  apply: milliwaysctl security harden npm --apply")
		return 0
	}

	if err := ensureNPMRCSettings(*path, settings); err != nil {
		fmt.Fprintf(stderr, "security harden npm: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "[security harden npm] wrote safer defaults to %s\n", *path)
	return 0
}

func ensureNPMRCSettings(path string, settings []string) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	existingBytes, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(string(existingBytes), "\n")
	seenKeys := make(map[string]bool, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if ok {
			seenKeys[strings.TrimSpace(key)] = true
		}
	}

	var missing []string
	for _, setting := range settings {
		key, _, _ := strings.Cut(setting, "=")
		if !seenKeys[key] {
			missing = append(missing, setting)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	content := string(existingBytes)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += "# Added by MilliWays security hardening.\n"
	content += strings.Join(missing, "\n")
	content += "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

func stringMapField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func firstStringField(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v := stringMapField(m, key); v != "" {
			return v
		}
	}
	return ""
}

func intMapField(m map[string]any, keys ...string) int {
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
		name, _ := scanner["name"].(string)
		if name == "" {
			continue
		}
		if isInstalled, _ := scanner["installed"].(bool); isInstalled {
			if version, _ := scanner["version"].(string); version != "" {
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

// runInstallScanner installs osv-scanner via go install.
func runInstallScanner(stdout, stderr io.Writer) int {
	fmt.Fprintln(stdout, "[security] installing osv-scanner via go install...")
	fmt.Fprintln(stdout, "  running: go install github.com/google/osv-scanner/v2/cmd/osv-scanner@latest")

	cmd := execCommand("go", "install", "github.com/google/osv-scanner/v2/cmd/osv-scanner@latest")
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(stderr, "[security] install failed: %v\n", err)
		fmt.Fprintln(stderr, "  manual install: go install github.com/google/osv-scanner/v2/cmd/osv-scanner@latest")
		fmt.Fprintln(stderr, "  or: brew install osv-scanner")
		return 1
	}
	fmt.Fprintln(stdout, "[security] osv-scanner installed. Restart milliwaysd to start scanning.")
	return 0
}
