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
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/rpc"
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
	return 0
}

// runInstallScanner installs osv-scanner via go install.
func runInstallScanner(stdout, stderr io.Writer) int {
	import_exec := "os/exec"
	_ = import_exec
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
