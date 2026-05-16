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
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/rpc"
	"github.com/mwigge/milliways/internal/security/cra/evidence"
	"github.com/mwigge/milliways/internal/security/outputgate"
	"github.com/mwigge/milliways/internal/security/sbom"
	"github.com/mwigge/milliways/internal/security/shims"
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
	case "cra":
		return runSecurityCRA(rest, stdout, stderr, sock)
	case "cra-scaffold":
		return runSecurityCRAScaffold(rest, stdout, stderr)
	case "sbom":
		return runSecuritySBOM(rest, stdout, stderr)
	case "scan":
		return runSecurityScan(rest, stdout, stderr, sock)
	case "startup-scan":
		return runSecurityStartupScan(rest, stdout, stderr, sock)
	case "warnings":
		return runSecurityWarnings(rest, stdout, stderr, sock)
	case "audit":
		return runSecurityAudit(rest, stdout, stderr, sock)
	case "mode":
		return runSecurityMode(rest, stdout, stderr, sock)
	case "client":
		return runSecurityClient(rest, stdout, stderr, sock)
	case "command-check":
		return runSecurityCommandCheck(rest, stdout, stderr, sock)
	case "shims":
		return runSecurityShims(rest, stdout, stderr)
	case "shim-exec":
		return runSecurityShimExec(rest, stdout, stderr, sock)
	case "harden":
		return runSecurityHarden(rest, stdout, stderr)
	case "quarantine":
		return runSecurityQuarantine(rest, stdout, stderr, sock)
	case "rules":
		return runSecurityRules(rest, stdout, stderr, sock)
	case "output-plan":
		return runSecurityOutputPlan(rest, stdout, stderr)
	case "precommit-plan":
		return runSecurityPrecommitPlan(rest, stdout, stderr)
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
	fmt.Fprintln(w, "  cra [--json]           show EU Cyber Resilience Act readiness evidence")
	fmt.Fprintln(w, "  cra-scaffold [--workspace <dir>] [--dry-run] [--force]")
	fmt.Fprintln(w, "    create missing CRA evidence files: SECURITY.md, SUPPORT.md, docs/update-policy.md, docs/cra-technical-file.md")
	fmt.Fprintln(w, "  sbom [--workspace <dir>] [--output <path>]")
	fmt.Fprintln(w, "    generate an offline SPDX JSON SBOM from local Go, Cargo, npm, pnpm, yarn, Bun, and Python manifests")
	fmt.Fprintln(w, "  scan [--json] [--startup] [--client <name>] [--diff|--staged] [--secrets] [--sast]")
	fmt.Fprintln(w, "    run dependency security scan, optionally layered with startup, client, staged-diff, secret, and SAST checks")
	fmt.Fprintln(w, "  startup-scan [--json] [--strict]")
	fmt.Fprintln(w, "    run startup posture scan when supported by the daemon")
	fmt.Fprintln(w, "  warnings [--json]      show active security warnings")
	fmt.Fprintln(w, "  audit [--json] [--workspace <dir>] [--session <id>] [--client <name>] [--decision <allow|warn|block>] [--limit <n>]")
	fmt.Fprintln(w, "    show recent command policy decisions and audit events")
	fmt.Fprintln(w, "  mode [off|observe|warn|strict|ci]")
	fmt.Fprintln(w, "    show or set MilliWays security policy mode")
	fmt.Fprintln(w, "  client <name> [--json]")
	fmt.Fprintln(w, "    run a per-client security profile check")
	fmt.Fprintln(w, "  command-check [--mode <mode>] [--cwd <dir>] [--client <name>] [--json] -- <command...>")
	fmt.Fprintln(w, "    evaluate a command with the Secure MilliWays firewall")
	fmt.Fprintln(w, "  shims status [--dir <path>] [--json]")
	fmt.Fprintln(w, "  shims install [--dir <path>] [--json]")
	fmt.Fprintln(w, "    install or inspect command shims for first-start and client-switch verification")
	fmt.Fprintln(w, "  shim-exec -- <resolved-binary> [args...]")
	fmt.Fprintln(w, "    broker generated command shims through the Secure MilliWays policy API")
	fmt.Fprintln(w, "  harden npm [--dry-run|--apply] [--path <.npmrc>]")
	fmt.Fprintln(w, "    preview or write safer npm defaults")
	fmt.Fprintln(w, "  quarantine [--dry-run|--apply] [--json]")
	fmt.Fprintln(w, "    plan or apply quarantine actions when supported by the daemon")
	fmt.Fprintln(w, "  rules list|update [--json]")
	fmt.Fprintln(w, "    list or update security rule packs when supported by the daemon")
	fmt.Fprintln(w, "  output-plan [--json] [--generated <path> ...] [--staged <path> ...]")
	fmt.Fprintln(w, "    classify output or diff paths into requested scan types without running scanners")
	fmt.Fprintln(w, "  precommit-plan [--json] [--staged <path> ...]")
	fmt.Fprintln(w, "    plan scans for staged commit files without running scanners")
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
	workspace := firstStringField(result, "security_workspace", "workspace")
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
	if rulepacks := renderSecurityRulePacks(result["rulepacks"]); rulepacks != "" {
		fmt.Fprintf(stdout, "[security] rulepacks: %s\n", rulepacks)
	}
	if shims := renderSecurityStatusShims(result["shims"]); shims != "" {
		fmt.Fprintf(stdout, "[security] shims: %s\n", shims)
	}
	shimsReady, hasShims := securityShimsReady(result["shims"])
	if clients := renderSecurityClientEnforcement(result["client_enforcement"], shimsReady, hasShims); clients != "" {
		fmt.Fprintf(stdout, "[security] clients: %s\n", clients)
	}
	if mode != "" {
		fmt.Fprintf(stdout, "[security] mode: %s\n", mode)
	}
	if workspace != "" {
		fmt.Fprintf(stdout, "[security] workspace: %s\n", workspace)
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

func runSecurityCRA(args []string, stdout, stderr io.Writer, sock string) int {
	fs := flag.NewFlagSet("security cra", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw JSON result")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	c, err := rpc.Dial(sock)
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl security cra: dial %s: %v\n", sock, err)
		return 1
	}
	defer func() { _ = c.Close() }()

	var result map[string]any
	if err := c.Call("security.cra", map[string]any{}, &result); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl security cra: %v\n", err)
		return 1
	}
	if *asJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	renderSecurityCRA(result, stdout)
	return 0
}

func renderSecurityCRA(result map[string]any, stdout io.Writer) {
	summary, _ := result["summary"].(map[string]any)
	fmt.Fprintln(stdout, "[security] CRA readiness")
	if workspace := stringMapField(result, "workspace"); workspace != "" {
		fmt.Fprintf(stdout, "workspace: %s\n", workspace)
	}
	score := intMapField(summary, "evidence_score")
	present := intMapField(summary, "checks_present")
	total := intMapField(summary, "checks_total")
	partial := intMapField(summary, "checks_partial")
	missing := intMapField(summary, "checks_missing")
	reportingPresent := intMapField(summary, "reporting_present")
	reportingTotal := intMapField(summary, "reporting_total")
	securityWarnings := intMapField(summary, "security_warnings")
	securityBlocks := intMapField(summary, "security_blocks")
	reportingReady, _ := summary["reporting_ready"].(bool)
	design := stringMapField(summary, "design_evidence_status")
	if design == "" {
		design = "missing"
	}
	fmt.Fprintf(stdout, "evidence: %d%% (%d/%d present, %d partial, %d missing)\n", score, present, total, partial, missing)
	ready := "not ready"
	if reportingReady {
		ready = "ready"
	}
	fmt.Fprintf(stdout, "vulnerability/reporting: %d/%d %s\n", reportingPresent, reportingTotal, ready)
	fmt.Fprintf(stdout, "security issues: %d warnings, %d blocks\n", securityWarnings, securityBlocks)
	fmt.Fprintf(stdout, "design evidence: %s\n", design)
	fmt.Fprintln(stdout, "checks:")
	checks, _ := result["checks"].([]any)
	for _, raw := range checks {
		check, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		status := stringMapField(check, "status")
		id := stringMapField(check, "id")
		title := stringMapField(check, "title")
		fmt.Fprintf(stdout, "  %s  %s — %s\n", craStatusMark(status), id, title)
		missing := stringSliceMapField(check, "missing_evidence")
		if len(missing) > 0 {
			fmt.Fprintf(stdout, "      missing: %s\n", strings.Join(missing, ", "))
		}
		nextActions := stringSliceMapField(check, "next_actions")
		for _, action := range nextActions {
			fmt.Fprintf(stdout, "      next: %s\n", action)
		}
	}
}

func craStatusMark(status string) string {
	switch status {
	case "present":
		return "OK"
	case "partial":
		return "WARN"
	default:
		return "MISS"
	}
}

func runSecurityCRAScaffold(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("security cra-scaffold", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workspace := fs.String("workspace", ".", "workspace root")
	dryRun := fs.Bool("dry-run", false, "show files that would be created without writing them")
	force := fs.Bool("force", false, "overwrite existing scaffold files")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "security cra-scaffold: unexpected positional arguments")
		return 1
	}
	result, err := evidence.Scaffold(evidence.Options{
		Workspace: *workspace,
		DryRun:    *dryRun,
		Force:     *force,
	})
	if err != nil {
		fmt.Fprintf(stderr, "security cra-scaffold: %v\n", err)
		return 1
	}
	renderCRAScaffoldResult(stdout, result, *dryRun)
	return 0
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

func runSecuritySBOM(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("security sbom", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workspace := fs.String("workspace", ".", "workspace root")
	output := fs.String("output", "", "output SPDX JSON path")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "security sbom: unexpected positional arguments")
		return 1
	}
	doc, err := sbom.GenerateSPDX(sbom.GenerateOptions{Workspace: *workspace})
	if err != nil {
		fmt.Fprintf(stderr, "security sbom: %v\n", err)
		return 1
	}
	if strings.TrimSpace(*output) == "" {
		if err := sbom.WriteSPDXJSON(stdout, doc); err != nil {
			fmt.Fprintf(stderr, "security sbom: write stdout: %v\n", err)
			return 1
		}
		return 0
	}
	if err := sbom.WriteSPDXJSONFile(*output, doc); err != nil {
		fmt.Fprintf(stderr, "security sbom: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "[security] wrote SBOM -> %s (%d packages)\n", *output, len(doc.Packages))
	return 0
}

func runSecurityScan(args []string, stdout, stderr io.Writer, sock string) int {
	fs := flag.NewFlagSet("security scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw JSON result")
	startup := fs.Bool("startup", false, "run startup posture checks before the dependency scan")
	client := fs.String("client", "", "run a per-client security profile check before the dependency scan")
	diff := fs.Bool("diff", false, "scan staged diff paths")
	staged := fs.Bool("staged", false, "scan staged diff paths")
	secrets := fs.Bool("secrets", false, "include secret scanning layer")
	sast := fs.Bool("sast", false, "include SAST scanning layer")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "security scan: unexpected positional path %q; use --diff/--staged or security output-plan\n", fs.Arg(0))
		return 1
	}
	if !*startup && strings.TrimSpace(*client) == "" && !*diff && !*staged && !*secrets && !*sast {
		return callSecurityRPC("security scan", "security.scan", map[string]any{}, *asJSON, stdout, stderr, sock)
	}

	results := map[string]any{}
	if *startup {
		result, rc := callSecurityRPCResult("security startup-scan", "security.startup_scan", map[string]any{"strict": false}, stderr, sock)
		if rc != 0 {
			return rc
		}
		results["startup"] = result
		if !*asJSON {
			renderSecurityGenericResult(stdout, "security startup-scan", result)
		}
	}
	if name := strings.TrimSpace(*client); name != "" {
		result, rc := callSecurityRPCResult("security client", "security.client_profile", map[string]any{"client": name}, stderr, sock)
		if rc != 0 {
			return rc
		}
		results["client"] = result
		if !*asJSON {
			renderSecurityGenericResult(stdout, "security client", result)
		}
	}

	params := securityScanParams(*diff || *staged, *secrets, *sast)
	result, rc := callSecurityRPCResult("security scan", "security.scan", params, stderr, sock)
	if rc != 0 {
		return rc
	}
	results["scan"] = result
	if *asJSON {
		out, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "milliwaysctl security scan: encode result: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(out))
		return 0
	}
	renderSecurityGenericResult(stdout, "security scan", result)
	return 0
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

func runSecurityAudit(args []string, stdout, stderr io.Writer, sock string) int {
	fs := flag.NewFlagSet("security audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print raw JSON result")
	workspace := fs.String("workspace", "", "workspace root")
	session := fs.String("session", "", "session id")
	client := fs.String("client", "", "client name")
	decision := fs.String("decision", "", "filter decision: allow, warn, or block")
	limit := fs.Int("limit", 20, "maximum events to return")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "security audit: unexpected positional arguments")
		return 1
	}
	params := map[string]any{"limit": *limit}
	if strings.TrimSpace(*workspace) != "" {
		params["workspace"] = strings.TrimSpace(*workspace)
	}
	if strings.TrimSpace(*session) != "" {
		params["session_id"] = strings.TrimSpace(*session)
	}
	if strings.TrimSpace(*client) != "" {
		params["client"] = strings.TrimSpace(*client)
	}
	if strings.TrimSpace(*decision) != "" {
		params["decision"] = strings.TrimSpace(*decision)
	}
	result, rc := callSecurityRPCResult("security audit", "security.policy_audit", params, stderr, sock)
	if rc != 0 {
		return rc
	}
	if *asJSON {
		out, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "milliwaysctl security audit: encode result: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(out))
		return 0
	}
	renderSecurityAuditResult(stdout, "security audit", result)
	return 0
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
	argv := fs.Args()
	params := map[string]any{
		"command": shellCommandForPolicy(argv[0], argv[1:]),
		"argv":    append([]string(nil), argv...),
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

func runSecurityShims(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "security shims: expected status or install")
		return 2
	}
	switch args[0] {
	case "status":
		return runSecurityShimsStatus(args[1:], stdout, stderr)
	case "install":
		return runSecurityShimsInstall(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "security shims: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runSecurityShimsStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("security shims status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", defaultSecurityShimDir(), "shim directory")
	asJSON := fs.Bool("json", false, "print JSON result")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	status, err := shims.StatusDefaultCatalog(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "security shims status: %v\n", err)
		return 1
	}
	return renderSecurityShimStatus(stdout, status, *asJSON)
}

func runSecurityShimsInstall(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("security shims install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", defaultSecurityShimDir(), "shim directory")
	asJSON := fs.Bool("json", false, "print JSON result")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	install, err := shims.InstallDefaultCatalog(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "security shims install: %v\n", err)
		return 1
	}
	status, err := shims.StatusDefaultCatalog(install.Dir)
	if err != nil {
		fmt.Fprintf(stderr, "security shims install: status: %v\n", err)
		return 1
	}
	if *asJSON {
		data, _ := json.MarshalIndent(map[string]any{
			"dir":       install.Dir,
			"installed": len(install.Paths),
			"replaced":  install.Replaced,
			"status":    status,
		}, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintf(stdout, "[security] shims: installed %d/%d (replaced %d)\n", len(install.Paths), status.Expected, install.Replaced)
	renderSecurityShimStatus(stdout, status, false)
	return 0
}

func defaultSecurityShimDir() string {
	return filepath.Join(filepath.Dir(defaultSocket()), "security-shims")
}

func renderSecurityShimStatus(stdout io.Writer, status shims.StatusResult, asJSON bool) int {
	if asJSON {
		data, _ := json.MarshalIndent(status, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	ready := "not ready"
	if status.Ready {
		ready = "ready"
	}
	fmt.Fprintf(stdout, "[security] shims: %s\n", ready)
	fmt.Fprintf(stdout, "  dir: %s\n", status.Dir)
	fmt.Fprintf(stdout, "  installed: %d/%d\n", status.Installed, status.Expected)
	if status.BrokerInstalled {
		fmt.Fprintf(stdout, "  broker: %s\n", status.BrokerPath)
	} else {
		fmt.Fprintf(stdout, "  missing broker: %s\n", status.BrokerCommand)
	}
	if len(status.MissingShims) > 0 {
		fmt.Fprintf(stdout, "  missing shims: %s\n", strings.Join(status.MissingShims, ", "))
	}
	if len(status.MissingRealTools) > 0 {
		fmt.Fprintf(stdout, "  missing optional real tools: %s\n", strings.Join(status.MissingRealTools, ", "))
	}
	return 0
}

func runSecurityShimExec(args []string, stdout, stderr io.Writer, sock string) int {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "security shim-exec: expected resolved binary after --")
		return 2
	}
	realBinary := args[0]
	realArgs := args[1:]
	commandName := strings.TrimSpace(os.Getenv(shims.EnvCommand))
	if commandName == "" {
		commandName = filepath.Base(realBinary)
	}
	if err := validateShimExecRequest(commandName, realBinary); err != nil {
		fmt.Fprintf(stderr, "milliways security shim: %v\n", err)
		return 126
	}
	commandText := shellCommandForPolicy(commandName, realArgs)
	cwd, _ := os.Getwd()
	workspace := deriveShimWorkspace(cwd, os.Getenv("MILLIWAYS_WORKSPACE_ROOT"))
	params := map[string]any{
		"operation_type":     "command",
		"command":            commandText,
		"argv":               append([]string{commandName}, realArgs...),
		"cwd":                cwd,
		"client":             strings.TrimSpace(os.Getenv("MILLIWAYS_CLIENT_ID")),
		"session_id":         strings.TrimSpace(os.Getenv("MILLIWAYS_SESSION_ID")),
		"workspace":          workspace,
		"enforcement_level":  "brokered",
		"broker_interactive": stdinIsTerminal(),
		"env_summary": map[string]any{
			"shim_command":       commandName,
			"shim_category":      strings.TrimSpace(os.Getenv(shims.EnvCategory)),
			"shim_dir":           strings.TrimSpace(os.Getenv(shims.EnvShimDir)),
			"shim_resolved_path": realBinary,
		},
	}
	decision, rc := callSecurityRPCResult("security shim-exec", "security.policy_decide", params, stderr, sock)
	if rc != 0 {
		if os.Getenv("MILLIWAYS_SHIM_FAIL_OPEN") == "1" {
			fmt.Fprintln(stderr, "milliways security shim: policy unavailable; continuing because MILLIWAYS_SHIM_FAIL_OPEN=1")
			return execResolvedCommand(realBinary, realArgs, stdout, stderr)
		}
		fmt.Fprintln(stderr, "milliways security shim: policy unavailable; blocked by default")
		return 126
	}
	action := strings.ToLower(firstStringField(decision, "decision", "action"))
	reason := firstStringField(decision, "reason")
	switch action {
	case "allow", "":
		return execResolvedCommand(realBinary, realArgs, stdout, stderr)
	case "warn":
		fmt.Fprintf(stderr, "milliways security warning: %s\n", fallbackSecurityReason(reason))
		return execResolvedCommand(realBinary, realArgs, stdout, stderr)
	case "needs-confirmation":
		if !confirmShimExecution(stderr, commandText, reason) {
			fmt.Fprintln(stderr, "milliways security shim: command cancelled")
			return 126
		}
		return execResolvedCommand(realBinary, realArgs, stdout, stderr)
	case "block":
		fmt.Fprintf(stderr, "milliways security block: %s\n", fallbackSecurityReason(reason))
		return 126
	default:
		fmt.Fprintf(stderr, "milliways security shim: unknown policy decision %q\n", action)
		return 126
	}
}

func validateShimExecRequest(commandName, realBinary string) error {
	commandName = strings.TrimSpace(commandName)
	if commandName == "" {
		return fmt.Errorf("missing shim command metadata")
	}
	if filepath.Base(commandName) != commandName {
		return fmt.Errorf("invalid shim command metadata %q", commandName)
	}
	realBinary = strings.TrimSpace(realBinary)
	if realBinary == "" {
		return fmt.Errorf("missing resolved binary")
	}
	if !filepath.IsAbs(realBinary) {
		return fmt.Errorf("resolved binary must be absolute: %s", realBinary)
	}
	if filepath.Base(realBinary) != commandName {
		return fmt.Errorf("resolved binary %q does not match shim command %q", realBinary, commandName)
	}
	if shimDir := strings.TrimSpace(os.Getenv(shims.EnvShimDir)); shimDir != "" {
		rel, err := filepath.Rel(filepath.Clean(shimDir), filepath.Clean(realBinary))
		if err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".." {
			return fmt.Errorf("resolved binary points back into shim directory: %s", realBinary)
		}
	}
	return nil
}

func shellCommandForPolicy(command string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuoteForPolicy(command))
	for _, arg := range args {
		parts = append(parts, shellQuoteForPolicy(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuoteForPolicy(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z') &&
			!(r >= 'a' && r <= 'z') &&
			!(r >= '0' && r <= '9') &&
			!strings.ContainsRune("@%_+=:,./-", r)
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func execResolvedCommand(path string, args []string, stdout, stderr io.Writer) int {
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = sanitizedShimExecEnv(os.Environ())
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(stderr, "milliways security shim: exec %s: %v\n", path, err)
		return 127
	}
	return 0
}

func deriveShimWorkspace(cwd, envWorkspace string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		absCWD = cwd
	}
	absCWD = filepath.Clean(absCWD)
	if realCWD, err := filepath.EvalSymlinks(absCWD); err == nil {
		absCWD = filepath.Clean(realCWD)
	}
	envWorkspace = strings.TrimSpace(envWorkspace)
	if envWorkspace == "" {
		return absCWD
	}
	absWorkspace, err := filepath.Abs(envWorkspace)
	if err != nil {
		return absCWD
	}
	absWorkspace = filepath.Clean(absWorkspace)
	if realWorkspace, err := filepath.EvalSymlinks(absWorkspace); err == nil {
		absWorkspace = filepath.Clean(realWorkspace)
	}
	if pathWithin(absCWD, absWorkspace) {
		return absWorkspace
	}
	return absCWD
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func sanitizedShimExecEnv(env []string) []string {
	blocked := map[string]struct{}{
		shims.EnvActive:       {},
		shims.EnvCommand:      {},
		shims.EnvCategory:     {},
		shims.EnvShimDir:      {},
		shims.EnvResolvedPath: {},
		shims.EnvOriginalPath: {},
		shims.EnvBroker:       {},
	}
	out := env[:0]
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, drop := blocked[name]; drop {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func confirmShimExecution(stderr io.Writer, command, reason string) bool {
	if !stdinIsTerminal() {
		fmt.Fprintf(stderr, "milliways security confirmation required: %s\n", fallbackSecurityReason(reason))
		return false
	}
	fmt.Fprintf(stderr, "MilliWays security gate: %s\nCommand: %s\nRun this one command? [y/N] ", fallbackSecurityReason(reason), command)
	var reply string
	if _, err := fmt.Fscan(os.Stdin, &reply); err != nil {
		return false
	}
	reply = strings.ToLower(strings.TrimSpace(reply))
	return reply == "y" || reply == "yes"
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

func fallbackSecurityReason(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return "policy requires attention"
	}
	return reason
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

type commandOutputFunc func(name string, args ...string) ([]byte, error)

var securityGitOutput commandOutputFunc = func(name string, args ...string) ([]byte, error) {
	return execCommand(name, args...).Output()
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

	renderSecurityScanPlan(stdout, "security output-plan", plan)
	return 0
}

func runSecurityPrecommitPlan(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("security precommit-plan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON scan plan")
	var staged repeatedStringFlag
	fs.Var(&staged, "staged", "staged path to classify instead of reading git; may be repeated")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "security precommit-plan: unexpected positional path %q; use --staged\n", fs.Arg(0))
		return 1
	}

	var changes []outputgate.FileChange
	if len(staged) > 0 {
		changes = stagedPathChanges(staged)
	} else {
		var err error
		changes, err = gitStagedChanges(securityGitOutput)
		if err != nil {
			fmt.Fprintf(stderr, "security precommit-plan: read staged files: %v\n", err)
			fmt.Fprintln(stderr, "  fallback: pass paths with --staged <path>")
			return 1
		}
	}

	plan := outputgate.PlanScans(changes)
	if *asJSON {
		out, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "security precommit-plan: encode plan: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(out))
		return 0
	}

	renderSecurityScanPlan(stdout, "security precommit-plan", plan)
	return 0
}

func stagedPathChanges(paths []string) []outputgate.FileChange {
	changes := make([]outputgate.FileChange, 0, len(paths))
	for _, path := range paths {
		changes = append(changes, outputgate.FileChange{
			Path:   path,
			Status: outputgate.StatusModified,
			Source: outputgate.SourceStaged,
		})
	}
	return changes
}

func gitStagedChanges(run commandOutputFunc) ([]outputgate.FileChange, error) {
	out, err := run("git", "diff", "--cached", "--name-status", "-z")
	if err != nil {
		return nil, err
	}
	return parseGitNameStatusZ(out), nil
}

func parseGitNameStatusZ(out []byte) []outputgate.FileChange {
	fields := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	changes := make([]outputgate.FileChange, 0, len(fields)/2)
	for i := 0; i < len(fields); {
		statusText := strings.TrimSpace(fields[i])
		i++
		if statusText == "" || i >= len(fields) {
			continue
		}

		status := gitStatusKind(statusText)
		path := fields[i]
		i++
		if gitStatusHasOldAndNewPath(statusText) {
			if i >= len(fields) {
				continue
			}
			path = fields[i]
			i++
		}
		changes = append(changes, outputgate.FileChange{
			Path:   path,
			Status: status,
			Source: outputgate.SourceStaged,
		})
	}
	return changes
}

func gitStatusKind(status string) outputgate.ChangeStatus {
	switch status[0] {
	case 'A':
		return outputgate.StatusAdded
	case 'C':
		return outputgate.StatusAdded
	case 'D':
		return outputgate.StatusDeleted
	case 'R':
		return outputgate.StatusRenamed
	default:
		return outputgate.StatusModified
	}
}

func gitStatusHasOldAndNewPath(status string) bool {
	return status[0] == 'R' || status[0] == 'C'
}

func renderSecurityScanPlan(stdout io.Writer, label string, plan outputgate.Plan) {
	if len(plan.Requests) == 0 {
		fmt.Fprintf(stdout, "[%s] no scans requested\n", label)
		return
	}
	fmt.Fprintf(stdout, "[%s] %d scan request(s)\n", label, len(plan.Requests))
	for _, req := range plan.Requests {
		fmt.Fprintf(stdout, "  %s: %s\n", req.Kind, strings.Join(req.Files, ", "))
		if req.Reason != "" {
			fmt.Fprintf(stdout, "    reason: %s\n", req.Reason)
		}
	}
	for _, recommendation := range plan.Recommendations {
		fmt.Fprintf(stdout, "  recommend: %s\n", recommendation)
	}
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

func callSecurityRPC(label, method string, params map[string]any, asJSON bool, stdout, stderr io.Writer, sock string) int {
	result, rc := callSecurityRPCResult(label, method, params, stderr, sock)
	if rc != 0 {
		return rc
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

func callSecurityRPCResult(label, method string, params map[string]any, stderr io.Writer, sock string) (map[string]any, int) {
	c, err := rpc.Dial(sock)
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl %s: dial %s: %v\n", label, sock, err)
		return nil, 1
	}
	defer func() { _ = c.Close() }()

	var result map[string]any
	if err := c.Call(method, params, &result); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl %s: %v\n", label, err)
		if strings.Contains(err.Error(), "method not found") || strings.Contains(err.Error(), "-32601") {
			fmt.Fprintln(stderr, "  this command surface is present in milliwaysctl and will activate when milliwaysd exposes the matching Secure MilliWays RPC")
		}
		return nil, 1
	}
	return result, 0
}

func renderSecurityGenericResult(stdout io.Writer, label string, result map[string]any) {
	if len(result) == 0 {
		fmt.Fprintf(stdout, "[%s] ok\n", label)
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
	if mode := stringMapField(result, "mode"); mode != "" {
		fmt.Fprintf(stdout, "[%s] mode: %s\n", label, mode)
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

func renderSecurityAuditResult(stdout io.Writer, label string, result map[string]any) {
	events, _ := result["events"].([]any)
	if len(events) == 0 {
		fmt.Fprintf(stdout, "[%s] no policy decisions\n", label)
		return
	}
	fmt.Fprintf(stdout, "[%s] %d policy decision(s)\n", label, len(events))
	for _, raw := range events {
		event, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		created := firstStringField(event, "created_at")
		if len(created) > 19 {
			created = created[:19] + "Z"
		}
		decision := firstStringField(event, "decision")
		mode := firstStringField(event, "mode")
		client := firstStringField(event, "client")
		session := firstStringField(event, "session_id")
		command := truncateStr(firstStringField(event, "command"), 80)
		if command == "" {
			command = firstStringField(event, "operation_type")
		}
		identity := strings.TrimSpace(strings.Join(nonEmptyStrings(client, session), "/"))
		if identity == "" {
			identity = "-"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", created, decision, mode, identity, command)
	}
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
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

func boolMapField(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		if v, ok := m[key].(bool); ok && v {
			return true
		}
	}
	return false
}

func stringSliceMapField(m map[string]any, key string) []string {
	switch v := m[key].(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func renderSecurityStatusShims(raw any) string {
	shims, ok := raw.(map[string]any)
	if !ok || len(shims) == 0 {
		return ""
	}
	ready := "not ready"
	if boolMapField(shims, "ready", "Ready") {
		ready = "ready"
	}
	installed := intMapField(shims, "installed", "Installed")
	expected := intMapField(shims, "expected", "Expected")
	var parts []string
	if expected > 0 {
		parts = append(parts, fmt.Sprintf("%s %d/%d", ready, installed, expected))
	} else {
		parts = append(parts, ready)
	}
	if boolMapField(shims, "broker_installed", "BrokerInstalled") {
		if path := firstStringField(shims, "broker_path", "BrokerPath"); path != "" {
			parts = append(parts, "broker "+path)
		} else {
			parts = append(parts, "broker installed")
		}
	} else if command := firstStringField(shims, "broker_command", "BrokerCommand"); command != "" {
		parts = append(parts, "missing broker "+command)
	}
	if missing := stringSliceMapField(shims, "missing_shims"); len(missing) > 0 {
		parts = append(parts, "missing "+strings.Join(missing, ", "))
	}
	if missing := stringSliceMapField(shims, "MissingShims"); len(missing) > 0 {
		parts = append(parts, "missing "+strings.Join(missing, ", "))
	}
	return strings.Join(parts, "; ")
}

func securityShimsReady(raw any) (bool, bool) {
	shims, ok := raw.(map[string]any)
	if !ok || len(shims) == 0 {
		return false, false
	}
	return boolMapField(shims, "ready", "Ready"), true
}

func renderSecurityClientEnforcement(raw any, shimsReady bool, hasShims bool) string {
	clients, ok := raw.(map[string]any)
	if !ok || len(clients) == 0 {
		return ""
	}
	names := make([]string, 0, len(clients))
	for name := range clients {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		meta, ok := clients[name].(map[string]any)
		if !ok {
			continue
		}
		level := firstStringField(meta, "level", "Level")
		if level == "" {
			level = "unknown"
		}
		state := securityClientProtectionState(level, boolMapField(meta, "controlled_env", "ControlledEnv"), firstStringField(meta, "broker_path", "BrokerPath"), shimsReady, hasShims)
		detail := level
		if strings.TrimSpace(detail) == "" {
			detail = "unknown"
		}
		if level == "brokered" {
			if hasShims {
				if shimsReady {
					detail += ", shim ready"
				} else {
					detail += ", shim not ready"
				}
			} else {
				detail += ", shim unknown"
			}
		}
		parts = append(parts, fmt.Sprintf("%s %s (%s)", name, state, detail))
	}
	return strings.Join(parts, "; ")
}

func securityClientProtectionState(level string, controlled bool, brokerPath string, shimsReady bool, hasShims bool) string {
	switch strings.TrimSpace(level) {
	case "full":
		return "protected"
	case "brokered":
		if controlled && hasShims && shimsReady {
			return "protected"
		}
		if controlled && !hasShims && strings.TrimSpace(brokerPath) != "" {
			return "protected"
		}
		return "unprotected"
	default:
		return "unprotected"
	}
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

func renderSecurityRulePacks(raw any) string {
	status, ok := raw.(map[string]any)
	if !ok || len(status) == 0 {
		return ""
	}
	count := intMapField(status, "count", "packs_count")
	updateState := firstStringField(status, "update_state", "status")
	if updateState == "" {
		updateState = "unknown"
	}
	var names []string
	if packs, ok := status["packs"].([]any); ok {
		for _, item := range packs {
			pack, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := firstStringField(pack, "name")
			if name == "" {
				continue
			}
			if version := firstStringField(pack, "version"); version != "" {
				name += "@" + version
			}
			names = append(names, name)
		}
	}
	rendered := fmt.Sprintf("%d loaded (%s)", count, updateState)
	if len(names) > 0 {
		rendered += ": " + strings.Join(names, ", ")
	}
	return rendered
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
