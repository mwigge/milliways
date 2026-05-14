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

package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/security"
	"github.com/mwigge/milliways/internal/security/adapters"
	"github.com/mwigge/milliways/internal/security/clientprofiles"
	"github.com/mwigge/milliways/internal/security/firewall"
	"github.com/mwigge/milliways/internal/security/quarantine"
	"github.com/mwigge/milliways/internal/security/rulepacks"
)

var securityStatusAdapters = func() []adapters.ScannerAdapter {
	return []adapters.ScannerAdapter{
		adapters.NewOSVScanner(),
		adapters.NewGitleaks(),
		adapters.NewSemgrep(),
		adapters.NewGovulncheck(),
	}
}

// securityFindingWire is the JSON wire type for a security finding.
type securityFindingWire struct {
	CVEID            string `json:"cve_id"`
	PackageName      string `json:"package_name"`
	InstalledVersion string `json:"installed_version"`
	FixedInVersion   string `json:"fixed_in_version,omitempty"`
	Severity         string `json:"severity"`
	Summary          string `json:"summary,omitempty"`
	FirstSeen        string `json:"first_seen,omitempty"`
	LastSeen         string `json:"last_seen,omitempty"`
	Accepted         bool   `json:"accepted,omitempty"`
}

// secFindingToWire converts a pantry.SecurityFinding to wire format.
func secFindingToWire(f pantry.SecurityFinding, accepted bool) securityFindingWire {
	w := securityFindingWire{
		CVEID:            f.CVEID,
		PackageName:      f.PackageName,
		InstalledVersion: f.InstalledVersion,
		FixedInVersion:   f.FixedInVersion,
		Severity:         f.Severity,
		Summary:          f.Summary,
		Accepted:         accepted,
	}
	if !f.FirstSeen.IsZero() {
		w.FirstSeen = f.FirstSeen.UTC().Format(time.RFC3339)
	}
	if !f.LastSeen.IsZero() {
		w.LastSeen = f.LastSeen.UTC().Format(time.RFC3339)
	}
	return w
}

// securityList handles the "security.list" RPC.
// Params: {include_accepted: bool}
// Result: {findings: [...]}
func (s *Server) securityList(enc *json.Encoder, req *Request) {
	if s.pantryDB == nil {
		writeError(enc, req.ID, ErrInvalidParams, "pantry not available")
		return
	}

	var p struct {
		IncludeAccepted bool `json:"include_accepted"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}

	store := s.pantryDB.Security()

	var findings []pantry.SecurityFinding
	var err error
	if p.IncludeAccepted {
		findings, err = store.ListAll()
	} else {
		findings, err = store.ListActive(nil)
	}
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("list findings: %v", err))
		return
	}

	// When include_accepted is true, mark each finding as accepted if it has
	// a non-expired accepted risk entry.
	var acceptedSet map[string]bool
	if p.IncludeAccepted && len(findings) > 0 {
		risks, _ := store.ListAcceptedRisks()
		acceptedSet = make(map[string]bool, len(risks))
		now := time.Now()
		for _, r := range risks {
			if r.ExpiresAt.After(now) {
				acceptedSet[r.CVEID+"|"+r.PackageName] = true
			}
		}
	}

	wires := make([]securityFindingWire, 0, len(findings))
	for _, f := range findings {
		accepted := false
		if acceptedSet != nil {
			accepted = acceptedSet[f.CVEID+"|"+f.PackageName]
		}
		wires = append(wires, secFindingToWire(f, accepted))
	}

	writeResult(enc, req.ID, map[string]any{"findings": wires})
}

// securityShow handles the "security.show" RPC.
// Params: {cve_id: string}
// Result: {finding: {...}}
func (s *Server) securityShow(enc *json.Encoder, req *Request) {
	if s.pantryDB == nil {
		writeError(enc, req.ID, ErrInvalidParams, "pantry not available")
		return
	}

	var p struct {
		CVEID string `json:"cve_id"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	if p.CVEID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "cve_id is required")
		return
	}

	f, err := s.pantryDB.Security().GetByCVE(p.CVEID)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("get CVE: %v", err))
		return
	}

	writeResult(enc, req.ID, map[string]any{"finding": secFindingToWire(f, false)})
}

// securityExists handles the "security.exists" RPC.
// Params: {cve_id: string}
// Result: {exists: bool}
func (s *Server) securityExists(enc *json.Encoder, req *Request) {
	if s.pantryDB == nil {
		writeError(enc, req.ID, ErrInvalidParams, "pantry not available")
		return
	}

	var p struct {
		CVEID string `json:"cve_id"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	if p.CVEID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "cve_id is required")
		return
	}

	exists, err := s.pantryDB.Security().CVEExists(p.CVEID)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("check CVE: %v", err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"exists": exists})
}

// securityAccept handles the "security.accept" RPC.
// Params: {cve_id, package_name, reason, expires_at}
// Validates: expiry ≤ 365 days from today.
// Result: {ok: true}
func (s *Server) securityAccept(enc *json.Encoder, req *Request) {
	if s.pantryDB == nil {
		writeError(enc, req.ID, ErrInvalidParams, "pantry not available")
		return
	}

	var p struct {
		CVEID       string `json:"cve_id"`
		PackageName string `json:"package_name"`
		Reason      string `json:"reason"`
		ExpiresAt   string `json:"expires_at"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	if p.CVEID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "cve_id is required")
		return
	}
	if p.PackageName == "" {
		writeError(enc, req.ID, ErrInvalidParams, "package_name is required")
		return
	}
	if p.Reason == "" {
		writeError(enc, req.ID, ErrInvalidParams, "reason is required")
		return
	}
	if p.ExpiresAt == "" {
		writeError(enc, req.ID, ErrInvalidParams, "expires_at is required")
		return
	}

	expiresAt, err := time.Parse(time.RFC3339, p.ExpiresAt)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("invalid expires_at (want RFC3339): %v", err))
		return
	}

	maxExpiry := time.Now().Add(365 * 24 * time.Hour)
	if expiresAt.After(maxExpiry) {
		writeError(enc, req.ID, ErrInvalidParams,
			fmt.Sprintf("expires_at exceeds maximum of 365 days from today (%s)",
				maxExpiry.UTC().Format("2006-01-02")))
		return
	}

	if err := s.pantryDB.Security().InsertAcceptedRisk(p.CVEID, p.PackageName, p.Reason, expiresAt); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("accept risk: %v", err))
		return
	}
	writeResult(enc, req.ID, map[string]any{
		"ok":         true,
		"cve_id":     p.CVEID,
		"expires_at": expiresAt.UTC().Format("2006-01-02"),
	})
}

// securityScan handles the "security.scan" RPC.
// Uses the live runner when available (30s timeout); falls back to cached DB findings.
func (s *Server) securityScan(enc *json.Encoder, req *Request) {
	if s.pantryDB == nil {
		writeError(enc, req.ID, ErrInvalidParams, "pantry not available")
		return
	}

	var lockfiles []string
	if s.secRunner != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := s.secRunner.ScanNow(ctx)
		if err == nil {
			lockfiles = result.LockFiles
		}
	}

	findings, err := s.pantryDB.Security().ListActive(nil)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("list active findings: %v", err))
		return
	}

	wires := make([]securityFindingWire, 0, len(findings))
	for _, f := range findings {
		wires = append(wires, secFindingToWire(f, false))
	}

	writeResult(enc, req.ID, map[string]any{
		"scanned_at": time.Now().UTC().Format(time.RFC3339),
		"lockfiles":  lockfiles,
		"findings":   wires,
	})
}

// securityEnable handles "security.enable" — turns on OSV scanning.
func (s *Server) securityEnable(enc *json.Encoder, req *Request) {
	if s.secRunner != nil {
		s.secRunner.Enable()
		writeResult(enc, req.ID, map[string]any{"enabled": true})
		return
	}
	writeError(enc, req.ID, ErrInvalidParams, "security runner not available")
}

// securityDisable handles "security.disable" — turns off OSV scanning.
func (s *Server) securityDisable(enc *json.Encoder, req *Request) {
	if s.secRunner != nil {
		s.secRunner.Disable()
		writeResult(enc, req.ID, map[string]any{"enabled": false})
		return
	}
	writeError(enc, req.ID, ErrInvalidParams, "security runner not available")
}

// securityStatus handles "security.status" — reports scanner state.
func (s *Server) securityStatus(enc *json.Encoder, req *Request) {
	scannerPath := security.ScannerPath()
	enabled := s.secRunner != nil && s.secRunner.IsEnabled()
	result := map[string]any{
		"enabled":                enabled,
		"scanner_path":           scannerPath,
		"installed":              scannerPath != "",
		"scanners":               securityScannerAdapterStatus(context.Background()),
		"mode":                   string(security.ModeWarn),
		"posture":                string(security.PostureOK),
		"warnings":               0,
		"blocks":                 0,
		"startup_scan_completed": false,
		"startup_scan_stale":     false,
		"startup_scan_required":  true,
	}
	if s.pantryDB != nil {
		workspace := s.securityWorkspaceRoot()
		status, err := s.pantryDB.Security().SecurityStatus(workspace)
		if err == nil {
			result["workspace"] = status.Workspace
			result["mode"] = status.Mode
			result["posture"] = status.Posture
			result["warnings"] = status.CountsBySeverity["WARN"] + status.CountsBySeverity["HIGH"] + status.CountsBySeverity["CRITICAL"]
			result["blocks"] = status.CountsBySeverity["BLOCK"]
			result["warning_count"] = result["warnings"]
			result["block_count"] = result["blocks"]
			result["active_client"] = status.ActiveClient
			completed, stale, required := startupScanState(status, startupScanConfigHash(workspace))
			result["startup_scan_completed"] = completed
			result["startup_scan_stale"] = stale
			result["startup_scan_required"] = required
			if !status.StartupScanCompletedAt.IsZero() {
				result["startup_scan_completed_at"] = status.StartupScanCompletedAt.UTC().Format(time.RFC3339)
			}
			if status.LastStartupScan != nil && !status.LastStartupScan.CompletedAt.IsZero() {
				result["last_startup_scan_at"] = status.LastStartupScan.CompletedAt.UTC().Format(time.RFC3339)
			}
			if status.LastDependencyScan != nil && !status.LastDependencyScan.CompletedAt.IsZero() {
				result["last_dependency_scan_at"] = status.LastDependencyScan.CompletedAt.UTC().Format(time.RFC3339)
			}
			result["cra"] = securityCRAStatus(workspace, status, result["scanners"])
		}
	}
	if _, ok := result["cra"]; !ok {
		result["cra"] = securityCRAStatus("", pantry.SecurityStatus{}, result["scanners"])
	}
	writeResult(enc, req.ID, result)
}

func (s *Server) securityCRA(enc *json.Encoder, req *Request) {
	scanners := securityScannerAdapterStatus(context.Background())
	workspace := s.securityWorkspaceRoot()
	var status pantry.SecurityStatus
	if s.pantryDB != nil {
		if st, err := s.pantryDB.Security().SecurityStatus(workspace); err == nil {
			status = st
		}
	}
	report, summary := evaluateCRAReadiness(workspace, status, scanners)
	checks := make([]map[string]any, 0, len(report.Checks))
	for _, check := range report.Checks {
		checks = append(checks, map[string]any{
			"id":               check.ID,
			"title":            check.Title,
			"category":         string(check.Category),
			"article":          check.Article,
			"status":           string(check.Status),
			"due_date":         check.DueDate,
			"deadline_status":  string(check.DeadlineStatus),
			"source_url":       check.SourceURL,
			"present_evidence": check.PresentEvidence,
			"missing_evidence": check.MissingEvidence,
		})
	}
	writeResult(enc, req.ID, map[string]any{
		"workspace": workspace,
		"summary":   summary,
		"checks":    checks,
	})
}

func securityCRAStatus(workspace string, status pantry.SecurityStatus, scannerStatus any) map[string]any {
	_, summary := evaluateCRAReadiness(workspace, status, scannerStatus)
	return summary
}

func evaluateCRAReadiness(workspace string, status pantry.SecurityStatus, scannerStatus any) (adapters.CRAReport, map[string]any) {
	report := adapters.NewCRAAdapter().Evaluate(adapters.CRAEvidenceInput{
		ProductName:                     "MilliWays",
		AsOf:                            time.Now().UTC(),
		SBOMPaths:                       findCRAEvidenceFiles(workspace, "sbom"),
		VulnerabilityHandlingPolicy:     firstCRAEvidenceFile(workspace, "security-policy"),
		VulnerabilityReportingContact:   firstCRAEvidenceFile(workspace, "security-contact"),
		VulnerabilityReportingProcess:   firstCRAEvidenceFile(workspace, "security-process"),
		SecureByDefaultEvidence:         secureByDefaultCRAEvidence(status),
		ScannerCoverage:                 scannerCoverageCRAEvidence(scannerStatus),
		SupportPeriod:                   firstCRAEvidenceFile(workspace, "support-period"),
		SupportUntil:                    nil,
		ConformityDocumentationPaths:    findCRAEvidenceFiles(workspace, "conformity"),
		AutomaticSecurityUpdateEvidence: findCRAEvidenceFiles(workspace, "updates"),
	})

	total, present, partial, missing := len(report.Checks), 0, 0, 0
	reportingPresent, reportingTotal := 0, 0
	daysToReporting := 0
	reportingDeadlineStatus := string(adapters.CRADeadlineUnknown)
	designEvidenceStatus := string(adapters.CRAEvidenceMissing)
	for _, check := range report.Checks {
		switch check.Status {
		case adapters.CRAEvidencePresent:
			present++
		case adapters.CRAEvidencePartial:
			partial++
		default:
			missing++
		}
		if check.ID == "cra-vulnerability-handling" {
			reportingPresent = len(check.PresentEvidence)
			reportingTotal = len(check.PresentEvidence) + len(check.MissingEvidence)
			daysToReporting = check.DaysUntilDue
			reportingDeadlineStatus = string(check.DeadlineStatus)
		}
		if check.ID == "cra-secure-by-default" {
			designEvidenceStatus = string(check.Status)
		}
	}
	score := 0
	if total > 0 {
		score = int((float64(present)+0.5*float64(partial))/float64(total)*100 + 0.5)
	}
	summary := map[string]any{
		"regulation":                report.Regulation,
		"evidence_score":            score,
		"checks_total":              total,
		"checks_present":            present,
		"checks_partial":            partial,
		"checks_missing":            missing,
		"reporting_ready":           reportingTotal > 0 && reportingPresent == reportingTotal,
		"reporting_present":         reportingPresent,
		"reporting_total":           reportingTotal,
		"design_evidence_status":    designEvidenceStatus,
		"days_to_reporting":         daysToReporting,
		"reporting_deadline":        "2026-09-11",
		"reporting_deadline_status": reportingDeadlineStatus,
		"full_deadline":             "2027-12-11",
	}
	return report, summary
}

func findCRAEvidenceFiles(workspace, kind string) []string {
	if strings.TrimSpace(workspace) == "" {
		return nil
	}
	candidates := map[string][]string{
		"sbom": {
			"sbom.spdx.json", "sbom.cdx.json", "bom.json", "dist/sbom.spdx.json",
			"dist/sbom.cdx.json", "dist/milliways.spdx.json",
		},
		"conformity": {
			"docs/cra-technical-file.md", "docs/declaration-of-conformity.md",
			"docs/conformity.md", "COMPLIANCE.md",
		},
		"updates": {
			"docs/update-policy.md", "docs/security-updates.md", "SECURITY.md",
		},
		"support-period": {
			"SUPPORT.md", "docs/support.md", "SECURITY.md",
		},
		"security-policy": {
			"SECURITY.md", ".github/SECURITY.md", "docs/security.md",
		},
		"security-contact": {
			"SECURITY.md", ".well-known/security.txt", ".github/SECURITY.md",
		},
		"security-process": {
			"SECURITY.md", "docs/security-reporting.md", ".github/SECURITY.md",
		},
	}
	var found []string
	for _, rel := range candidates[kind] {
		path := filepath.Join(workspace, rel)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			found = append(found, rel)
		}
	}
	return found
}

func firstCRAEvidenceFile(workspace, kind string) string {
	files := findCRAEvidenceFiles(workspace, kind)
	if len(files) == 0 {
		return ""
	}
	return files[0]
}

func secureByDefaultCRAEvidence(status pantry.SecurityStatus) []string {
	var evidence []string
	if strings.TrimSpace(status.Workspace) == "" {
		return evidence
	}
	if security.NormalizeMode(security.Mode(status.Mode)) != security.ModeOff {
		evidence = append(evidence, "security mode "+status.Mode)
	}
	if !status.StartupScanCompletedAt.IsZero() {
		evidence = append(evidence, "startup scan completed")
	}
	return evidence
}

func scannerCoverageCRAEvidence(raw any) []adapters.CRAScannerCoverage {
	items, ok := raw.([]map[string]any)
	if !ok {
		return nil
	}
	var coverage []adapters.CRAScannerCoverage
	for _, item := range items {
		installed, _ := item["installed"].(bool)
		name, _ := item["name"].(string)
		if !installed || name == "" {
			continue
		}
		coverage = append(coverage, adapters.CRAScannerCoverage{Name: name, Kind: craScannerKind(name)})
	}
	return coverage
}

func craScannerKind(name string) string {
	switch name {
	case "osv-scanner", "govulncheck":
		return "dependency"
	case "gitleaks":
		return "secret"
	case "semgrep":
		return "sast"
	default:
		return "scanner"
	}
}

func securityScannerAdapterStatus(ctx context.Context) []map[string]any {
	scannerAdapters := securityStatusAdapters()
	statuses := make([]map[string]any, 0, len(scannerAdapters))
	for _, adapter := range scannerAdapters {
		status := map[string]any{
			"name":      adapter.Name(),
			"installed": adapter.Installed(),
		}
		installed, _ := status["installed"].(bool)
		if installed {
			versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			version, err := adapter.Version(versionCtx)
			cancel()
			if err != nil {
				status["version_error"] = err.Error()
			} else if version != "" {
				status["version"] = version
			}
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// securityStartupScan handles "security.startup_scan" by running the fast
// deterministic local scanner and persisting warnings into pantry.
func (s *Server) securityStartupScan(enc *json.Encoder, req *Request) {
	if s.pantryDB == nil {
		writeError(enc, req.ID, ErrInvalidParams, "pantry not available")
		return
	}
	var p struct {
		Workspace string `json:"workspace,omitempty"`
		Strict    bool   `json:"strict,omitempty"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	workspace := strings.TrimSpace(p.Workspace)
	if workspace == "" {
		workspace = s.securityWorkspaceRoot()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := s.runStartupSecurityScan(ctx, workspace, p.Strict)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("startup scan: %v", err))
		return
	}
	writeResult(enc, req.ID, result)
}

func (s *Server) runStartupSecurityScan(ctx context.Context, workspace string, strict bool) (map[string]any, error) {
	if s.pantryDB == nil {
		return nil, fmt.Errorf("pantry not available")
	}
	if abs, err := filepath.Abs(workspace); err == nil {
		workspace = abs
	}
	store := s.pantryDB.Security()
	runID, _ := store.InsertScanRun(pantry.SecurityScanRun{
		Kind:      string(security.ScanStartup),
		Workspace: workspace,
		Status:    "running",
		ToolName:  "milliways-startup-scan",
	})
	result, err := security.RunStartupScan(ctx, security.StartupScanOptions{
		WorkspaceRoot:        workspace,
		UserPersistenceRoots: startupPersistenceRoots(),
	})
	if err != nil {
		if runID > 0 {
			_ = store.CompleteScanRun(runID, "error", 0, 0, 0, err.Error())
		}
		return nil, err
	}
	warnCount, blockCount := 0, 0
	warnings := make([]map[string]any, 0, len(result.Findings))
	for _, f := range result.Findings {
		sev := startupSeverityToStored(f.Severity)
		if sev == "BLOCK" {
			blockCount++
		} else {
			warnCount++
		}
		if err := store.UpsertWarning(pantry.SecurityWarning{
			Workspace:   result.WorkspaceRoot,
			Category:    string(f.Category),
			Severity:    sev,
			Source:      f.RelPath,
			Message:     f.Title,
			Status:      string(security.FindingActive),
			ScanRunID:   runID,
			Remediation: f.Remediation,
		}); err != nil {
			return nil, fmt.Errorf("persist warning: %w", err)
		}
		warnings = append(warnings, map[string]any{
			"rule_id":     f.RuleID,
			"category":    string(f.Category),
			"severity":    sev,
			"title":       f.Title,
			"path":        f.RelPath,
			"line":        f.Line,
			"remediation": f.Remediation,
		})
	}
	if runID > 0 {
		_ = store.CompleteScanRun(runID, "completed", len(result.Findings), warnCount, blockCount, "")
	}
	if err := store.MarkStartupScanCompleted(result.WorkspaceRoot, startupScanConfigHash(result.WorkspaceRoot)); err != nil {
		return nil, fmt.Errorf("mark startup scan completed: %w", err)
	}
	posture := string(security.PostureOK)
	if blockCount > 0 || (strict && warnCount > 0) {
		posture = string(security.PostureBlock)
	} else if warnCount > 0 {
		posture = string(security.PostureWarn)
	}
	_ = store.SetWorkspaceStatus(result.WorkspaceRoot, string(security.ModeWarn), s.currentAgent)
	return map[string]any{
		"workspace":     result.WorkspaceRoot,
		"scanned_at":    result.CompletedAt.UTC().Format(time.RFC3339),
		"files":         result.FilesScanned,
		"findings":      warnings,
		"warnings":      warnCount,
		"blocks":        blockCount,
		"warning_count": warnCount,
		"block_count":   blockCount,
		"posture":       posture,
	}, nil
}

// securityWarnings handles "security.warnings".
func (s *Server) securityWarnings(enc *json.Encoder, req *Request) {
	if s.pantryDB == nil {
		writeError(enc, req.ID, ErrInvalidParams, "pantry not available")
		return
	}
	warnings, err := s.pantryDB.Security().ListActiveWarnings(s.securityWorkspaceRoot())
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("list warnings: %v", err))
		return
	}
	out := make([]map[string]any, 0, len(warnings))
	for _, w := range warnings {
		out = append(out, map[string]any{
			"id":          w.ID,
			"workspace":   w.Workspace,
			"category":    w.Category,
			"severity":    w.Severity,
			"source":      w.Source,
			"message":     w.Message,
			"remediation": w.Remediation,
			"last_seen":   w.LastSeen.UTC().Format(time.RFC3339),
		})
	}
	writeResult(enc, req.ID, map[string]any{"warnings": out})
}

// securityMode handles "security.mode" get/set for the current workspace.
func (s *Server) securityMode(enc *json.Encoder, req *Request) {
	if s.pantryDB == nil {
		writeError(enc, req.ID, ErrInvalidParams, "pantry not available")
		return
	}
	var p struct {
		Mode string `json:"mode,omitempty"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	workspace := s.securityWorkspaceRoot()
	if p.Mode != "" {
		mode := security.NormalizeMode(security.Mode(p.Mode))
		if string(mode) != p.Mode {
			writeError(enc, req.ID, ErrInvalidParams, "invalid security mode")
			return
		}
		if err := s.pantryDB.Security().SetWorkspaceStatus(workspace, string(mode), s.currentAgent); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("set mode: %v", err))
			return
		}
	}
	status, err := s.pantryDB.Security().SecurityStatus(workspace)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("security status: %v", err))
		return
	}
	writeResult(enc, req.ID, map[string]any{
		"workspace": status.Workspace,
		"mode":      status.Mode,
		"posture":   status.Posture,
	})
}

type securityCommandCheckParams struct {
	Command string `json:"command"`
	CWD     string `json:"cwd,omitempty"`
	Client  string `json:"client,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

type securityCommandRiskWire struct {
	Category string `json:"category"`
	Reason   string `json:"reason"`
	Evidence string `json:"evidence,omitempty"`
}

type securityCommandCheckResult struct {
	Command        string                    `json:"command"`
	CWD            string                    `json:"cwd,omitempty"`
	Client         string                    `json:"client,omitempty"`
	Mode           string                    `json:"mode"`
	Posture        string                    `json:"posture,omitempty"`
	Decision       string                    `json:"decision"`
	Reason         string                    `json:"reason"`
	Parsed         bool                      `json:"parsed"`
	Risks          []securityCommandRiskWire `json:"risks"`
	RiskCategories []string                  `json:"risk_categories"`
}

// securityCommandCheck handles "security.command_check".
func (s *Server) securityCommandCheck(enc *json.Encoder, req *Request) {
	var p securityCommandCheckParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	result, err := s.runSecurityCommandCheck(p)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, err.Error())
		return
	}
	writeResult(enc, req.ID, result)
}

func (s *Server) runSecurityCommandCheck(p securityCommandCheckParams) (securityCommandCheckResult, error) {
	command := strings.TrimSpace(p.Command)
	if command == "" {
		return securityCommandCheckResult{}, fmt.Errorf("command is required")
	}
	cwd := strings.TrimSpace(p.CWD)
	if cwd == "" {
		cwd = s.securityWorkspaceRoot()
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}
	client := strings.TrimSpace(p.Client)
	if client == "" {
		client = s.currentAgent
	}

	mode, posture := security.ModeWarn, security.PostureUnknown
	if s.pantryDB != nil {
		status, err := s.pantryDB.Security().SecurityStatus(cwd)
		if err == nil {
			mode = security.Mode(status.Mode)
			posture = security.Posture(status.Posture)
			if client == "" {
				client = status.ActiveClient
			}
		}
	}
	if strings.TrimSpace(p.Mode) != "" {
		requested := security.Mode(strings.TrimSpace(p.Mode))
		normalized := security.NormalizeMode(requested)
		if normalized != requested {
			return securityCommandCheckResult{}, fmt.Errorf("invalid security mode")
		}
		mode = normalized
	}

	fwResult := firewall.Evaluate(firewall.Request{
		Command:  command,
		RunnerID: client,
		CWD:      cwd,
		Policy: firewall.Policy{
			Mode:                      mode,
			BlockNetworkDownloadsInCI: true,
		},
		Posture: posture,
	})

	risks := make([]securityCommandRiskWire, 0, len(fwResult.Risks))
	categories := make([]string, 0, len(fwResult.Risks))
	for _, risk := range fwResult.Risks {
		category := string(risk.Category)
		categories = append(categories, category)
		risks = append(risks, securityCommandRiskWire{
			Category: category,
			Reason:   risk.Reason,
			Evidence: risk.Evidence,
		})
	}
	return securityCommandCheckResult{
		Command:        command,
		CWD:            cwd,
		Client:         client,
		Mode:           string(fwResult.Mode),
		Posture:        string(posture),
		Decision:       string(fwResult.Decision),
		Reason:         fwResult.Reason,
		Parsed:         fwResult.Parsed,
		Risks:          risks,
		RiskCategories: categories,
	}, nil
}

func (s *Server) recordClientProfileSecurity(ctx context.Context, workspace, client string) error {
	_, err := s.runClientProfileSecurity(ctx, workspace, client)
	return err
}

func (s *Server) securityClientProfile(enc *json.Encoder, req *Request) {
	if s.pantryDB == nil {
		writeError(enc, req.ID, ErrInvalidParams, "pantry not available")
		return
	}
	var p struct {
		Client    string `json:"client"`
		Workspace string `json:"workspace,omitempty"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	if strings.TrimSpace(p.Client) == "" {
		writeError(enc, req.ID, ErrInvalidParams, "client is required")
		return
	}
	workspace := strings.TrimSpace(p.Workspace)
	if workspace == "" {
		workspace = s.securityWorkspaceRoot()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := s.runClientProfileSecurity(ctx, workspace, p.Client)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("client profile: %v", err))
		return
	}
	writeResult(enc, req.ID, result)
}

// securityQuarantine handles "security.quarantine" as a dry-run planner.
func (s *Server) securityQuarantine(enc *json.Encoder, req *Request) {
	var p struct {
		Workspace string `json:"workspace,omitempty"`
		DryRun    bool   `json:"dry_run"`
		Apply     bool   `json:"apply"`
	}
	p.DryRun = true
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	if p.Apply {
		writeError(enc, req.ID, ErrInvalidParams, "security.quarantine apply is not implemented; run dry-run and apply manually")
		return
	}
	workspace := strings.TrimSpace(p.Workspace)
	if workspace == "" {
		workspace = s.securityWorkspaceRoot()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	plan, err := quarantine.PlanActions(ctx, quarantine.Options{
		WorkspaceRoot:    workspace,
		SystemdRoots:     quarantineRoots("systemd-user", filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")),
		LaunchAgentRoots: quarantineRoots("launch-agents", filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents")),
	})
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("plan quarantine: %v", err))
		return
	}
	actions := make([]map[string]any, 0, len(plan.Actions))
	for _, a := range plan.Actions {
		actions = append(actions, map[string]any{
			"kind":              string(a.Kind),
			"reason":            a.Reason,
			"source_path":       a.SourcePath,
			"destination_path":  a.DestinationPath,
			"hash":              a.Hash,
			"apply_required":    a.ApplyRequired,
			"rollback_hint":     a.RollbackHint,
			"additional_fields": a.AdditionalFields,
		})
	}
	writeResult(enc, req.ID, map[string]any{
		"workspace":       plan.WorkspaceRoot,
		"quarantine_root": plan.QuarantineRoot,
		"planned_at":      plan.PlannedAt.UTC().Format(time.RFC3339),
		"dry_run":         true,
		"actions":         actions,
	})
}

// securityRulesList handles "security.rules_list".
func (s *Server) securityRulesList(enc *json.Encoder, req *Request) {
	workspace := s.securityWorkspaceRoot()
	packs, err := rulepacks.LoadAll(securityRulePackOptions(workspace))
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("load rule packs: %v", err))
		return
	}
	persisted, err := s.persistSecurityRulePacks(workspace, packs)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("persist rule packs: %v", err))
		return
	}
	writeResult(enc, req.ID, map[string]any{
		"rules":              securityRulePacksToWire(packs),
		"persisted_metadata": securityPersistedRulePacksToWire(persisted),
		"offline":            true,
	})
}

// securityRulesUpdate verifies local rule packs. Network updates are
// intentionally disabled by default.
func (s *Server) securityRulesUpdate(enc *json.Encoder, req *Request) {
	workspace := s.securityWorkspaceRoot()
	packs, err := rulepacks.LoadAll(securityRulePackOptions(workspace))
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("verify rule packs: %v", err))
		return
	}
	persisted, err := s.persistSecurityRulePacks(workspace, packs)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("persist rule packs: %v", err))
		return
	}
	writeResult(enc, req.ID, map[string]any{
		"ok":                 true,
		"offline":            true,
		"packs":              len(packs),
		"persisted_metadata": securityPersistedRulePacksToWire(persisted),
		"message":            "local rule packs verified; network updates are disabled by default",
	})
}

func (s *Server) persistSecurityRulePacks(workspace string, packs []rulepacks.Pack) ([]pantry.SecurityRulePack, error) {
	if s.pantryDB == nil {
		return nil, nil
	}
	store := s.pantryDB.Security()
	for _, p := range packs {
		if err := store.UpsertRulePack(pantry.SecurityRulePack{
			Workspace:               workspace,
			Name:                    p.Manifest.Name,
			Version:                 p.Manifest.Version,
			Source:                  string(p.Source),
			ManifestSource:          p.Manifest.Source,
			Checksum:                p.Manifest.Checksum,
			MinimumMilliWaysVersion: p.Manifest.MinimumMilliWaysVersion,
			RulesFile:               p.Manifest.RulesFile,
			RulesCount:              len(p.Rules),
			Root:                    p.Root,
			ManifestPath:            p.ManifestPath,
			RulesPath:               p.RulesPath,
			Status:                  "loaded",
		}); err != nil {
			return nil, err
		}
	}
	return store.ListRulePacks(workspace)
}

func securityRulePacksToWire(packs []rulepacks.Pack) []map[string]any {
	out := make([]map[string]any, 0, len(packs))
	for _, p := range packs {
		out = append(out, map[string]any{
			"name":                      p.Manifest.Name,
			"version":                   p.Manifest.Version,
			"source":                    string(p.Source),
			"manifest_source":           p.Manifest.Source,
			"checksum":                  p.Manifest.Checksum,
			"minimum_milliways_version": p.Manifest.MinimumMilliWaysVersion,
			"rules_file":                p.Manifest.RulesFile,
			"rules":                     len(p.Rules),
			"root":                      p.Root,
			"manifest_path":             p.ManifestPath,
			"rules_path":                p.RulesPath,
		})
	}
	return out
}

func securityPersistedRulePacksToWire(packs []pantry.SecurityRulePack) []map[string]any {
	out := make([]map[string]any, 0, len(packs))
	for _, p := range packs {
		out = append(out, map[string]any{
			"workspace":                 p.Workspace,
			"name":                      p.Name,
			"version":                   p.Version,
			"source":                    p.Source,
			"manifest_source":           p.ManifestSource,
			"checksum":                  p.Checksum,
			"minimum_milliways_version": p.MinimumMilliWaysVersion,
			"rules_file":                p.RulesFile,
			"rules":                     p.RulesCount,
			"root":                      p.Root,
			"manifest_path":             p.ManifestPath,
			"rules_path":                p.RulesPath,
			"status":                    p.Status,
			"first_seen":                secWireTime(p.FirstSeen),
			"last_seen":                 secWireTime(p.LastSeen),
		})
	}
	return out
}

func secWireTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (s *Server) runClientProfileSecurity(ctx context.Context, workspace, client string) (map[string]any, error) {
	client = strings.ToLower(strings.TrimSpace(client))
	if s.pantryDB == nil || client == "" {
		return nil, fmt.Errorf("pantry not available or client empty")
	}
	configHash := clientProfileConfigHash(workspace, client)
	store := s.pantryDB.Security()
	if cached, ok, err := store.GetClientProfile(workspace, client, configHash); err != nil {
		return nil, err
	} else if ok && cached.Status == "completed" && cached.ResultJSON != "" {
		var result map[string]any
		if err := json.Unmarshal([]byte(cached.ResultJSON), &result); err != nil {
			return nil, fmt.Errorf("decode cached client profile: %w", err)
		}
		if err := store.SetWorkspaceStatus(workspace, string(security.ModeWarn), client); err != nil {
			return nil, err
		}
		result["cached"] = true
		result["config_hash"] = configHash
		return result, nil
	}

	check := clientprofiles.New(client, clientprofiles.DefaultOptions())
	result := check.Check(ctx, workspace)
	runID, _ := store.InsertScanRun(pantry.SecurityScanRun{
		Kind:      string(security.ScanClientProfile),
		Workspace: workspace,
		Status:    "running",
		ToolName:  "milliways-client-profile",
	})
	warnCount, blockCount := 0, 0
	for _, warning := range result.Warnings {
		sev := profileSeverityToStored(warning.Severity)
		if sev == "BLOCK" {
			blockCount++
		} else {
			warnCount++
		}
		source := warning.Path
		if source == "" {
			source = warning.Key
		}
		if source == "" {
			source = warning.ID
		}
		if err := store.UpsertWarning(pantry.SecurityWarning{
			Workspace:   workspace,
			Category:    string(security.FindingClient),
			Severity:    sev,
			Source:      client + ":" + source,
			Message:     warning.Summary,
			Status:      string(security.FindingActive),
			ScanRunID:   runID,
			Remediation: "Review the client configuration before using this client in the workspace.",
		}); err != nil {
			return nil, err
		}
	}
	status := "completed"
	scanErr := result.Error
	if scanErr != "" {
		status = "error"
	}
	if runID > 0 {
		_ = store.CompleteScanRun(runID, status, len(result.Warnings), warnCount, blockCount, scanErr)
	}
	if err := store.SetWorkspaceStatus(workspace, string(security.ModeWarn), client); err != nil {
		return nil, err
	}
	warnings := make([]map[string]any, 0, len(result.Warnings))
	for _, w := range result.Warnings {
		warnings = append(warnings, map[string]any{
			"client":   w.Client,
			"id":       w.ID,
			"severity": string(w.Severity),
			"summary":  w.Summary,
			"detail":   w.Detail,
			"path":     w.Path,
			"key":      w.Key,
		})
	}
	out := map[string]any{
		"client":        result.Client,
		"workspace":     result.Workspace,
		"checked_at":    result.CheckedAt.UTC().Format(time.RFC3339),
		"warnings":      warnings,
		"warning_count": warnCount,
		"block_count":   blockCount,
		"error":         result.Error,
		"config_hash":   configHash,
		"cached":        false,
	}
	resultJSON, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encode client profile cache: %w", err)
	}
	if err := store.UpsertClientProfile(pantry.SecurityClientProfile{
		Workspace:    workspace,
		Client:       client,
		ConfigHash:   configHash,
		WarningCount: warnCount,
		BlockCount:   blockCount,
		Status:       status,
		ResultJSON:   string(resultJSON),
		Error:        scanErr,
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func clientProfileConfigHash(workspace, client string) string {
	if abs, err := filepath.Abs(workspace); err == nil {
		workspace = abs
	}
	home, _ := os.UserHomeDir()
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		configDir = filepath.Join(home, ".config")
	}

	h := sha256.New()
	writeHashPart(h, "client-profile-v1")
	writeHashPart(h, "workspace="+workspace)
	writeHashPart(h, "client="+client)
	for _, env := range clientProfileEnvKeys(client) {
		writeHashPart(h, "env:"+env+"="+os.Getenv(env))
	}
	for _, path := range clientProfileConfigPaths(workspace, home, configDir, client) {
		writeHashPart(h, "path="+path)
		data, err := os.ReadFile(path)
		if err != nil {
			writeHashPart(h, "missing")
			continue
		}
		writeHashPart(h, string(data))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func writeHashPart(h interface{ Write([]byte) (int, error) }, part string) {
	_, _ = h.Write([]byte(part))
	_, _ = h.Write([]byte{0})
}

func clientProfileConfigPaths(workspace, home, configDir, client string) []string {
	var paths []string
	add := func(path string) {
		if path != "" {
			paths = append(paths, path)
		}
	}
	addWorkspace := func(rel string) {
		if workspace != "" {
			add(filepath.Join(workspace, rel))
		}
	}
	addConfig := func(rel string) {
		if configDir != "" {
			add(filepath.Join(configDir, rel))
		}
	}
	switch client {
	case clientprofiles.ClientClaude:
		addWorkspace(".claude/settings.json")
		addWorkspace(".claude/mcp.json")
		addConfig(filepath.Join("claude", "settings.json"))
		addConfig(filepath.Join("claude", "mcp.json"))
		for _, path := range globProfilePaths(filepath.Join(workspace, ".claude", "*.js")) {
			add(path)
		}
		addWorkspace("CLAUDE.md")
	case clientprofiles.ClientCodex:
		if home != "" {
			add(filepath.Join(home, ".codex", "config.toml"))
			add(filepath.Join(home, ".codex", "config.json"))
		}
		addWorkspace(filepath.Join(".codex", "config.toml"))
		addConfig(filepath.Join("codex", "config.toml"))
		addConfig(filepath.Join("codex", "config.json"))
	case clientprofiles.ClientCopilot:
		addWorkspace(".copilot/config.json")
		addWorkspace(".copilot/settings.json")
		addConfig(filepath.Join("github-copilot", "config.json"))
		addConfig(filepath.Join("copilot", "config.json"))
	case clientprofiles.ClientGemini:
		addWorkspace(".gemini/settings.json")
		addWorkspace(".gemini/config.json")
		addConfig(filepath.Join("gemini", "settings.json"))
		addConfig(filepath.Join("gemini", "config.json"))
	case clientprofiles.ClientPool:
		addWorkspace(".pool/config.json")
		addWorkspace(".pool/settings.json")
		addConfig(filepath.Join("pool", "config.json"))
		addConfig(filepath.Join("pool", "settings.json"))
	case clientprofiles.ClientMiniMax:
		addWorkspace(".minimax/config.json")
		addWorkspace(".minimax/settings.json")
		addConfig(filepath.Join("minimax", "config.json"))
		addConfig(filepath.Join("milliways", "local.env"))
	case clientprofiles.ClientLocal:
		addWorkspace(filepath.Join(".milliways", "local.env"))
		addConfig(filepath.Join("milliways", "local.env"))
		addConfig(filepath.Join("milliways", "local.yaml"))
	}
	addWorkspace("package.json")
	sort.Strings(paths)
	return dedupeStrings(paths)
}

func globProfilePaths(pattern string) []string {
	if strings.TrimSpace(pattern) == "" {
		return nil
	}
	matches, _ := filepath.Glob(pattern)
	sort.Strings(matches)
	return matches
}

func clientProfileEnvKeys(client string) []string {
	switch client {
	case clientprofiles.ClientCodex:
		return []string{"CODEX_FLAGS", "CODEX_ARGS", "OPENAI_CODEX_FLAGS"}
	case clientprofiles.ClientCopilot:
		return []string{"COPILOT_FLAGS", "COPILOT_ARGS", "GITHUB_COPILOT_FLAGS"}
	case clientprofiles.ClientGemini:
		return []string{"GEMINI_FLAGS", "GEMINI_ARGS"}
	case clientprofiles.ClientPool:
		return []string{"POOL_FLAGS", "POOL_ARGS"}
	case clientprofiles.ClientMiniMax:
		return []string{"MINIMAX_FLAGS", "MINIMAX_ARGS"}
	case clientprofiles.ClientLocal:
		return []string{"MILLIWAYS_LOCAL_ENDPOINT", "MILLIWAYS_LOCAL_BIND", "MILLIWAYS_LOCAL_AUTH_TOKEN"}
	default:
		return nil
	}
}

func dedupeStrings(in []string) []string {
	out := in[:0]
	var prev string
	for _, item := range in {
		if item == prev {
			continue
		}
		out = append(out, item)
		prev = item
	}
	return out
}

func (s *Server) securityWorkspaceRoot() string {
	if root := strings.TrimSpace(os.Getenv("MILLIWAYS_WORKSPACE_ROOT")); root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			return abs
		}
		return root
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

func startupScanState(status pantry.SecurityStatus, currentConfigHash string) (completed, stale, required bool) {
	completed = !status.StartupScanCompletedAt.IsZero()
	stale = completed && status.StartupScanConfigHash != currentConfigHash
	required = !completed || stale
	return completed, stale, required
}

func startupScanConfigHash(workspace string) string {
	if abs, err := filepath.Abs(workspace); err == nil {
		workspace = abs
	}
	roots := startupPersistenceRoots()
	parts := make([]string, 0, 2+len(roots))
	parts = append(parts, "startup-scan-v1", "workspace="+workspace)
	for _, root := range roots {
		path := root.Path
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		parts = append(parts, root.Name+"="+path)
	}
	sort.Strings(parts[2:])
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func startupPersistenceRoots() []security.StartupScanRoot {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return nil
	}
	return []security.StartupScanRoot{
		{Name: "systemd-user", Path: filepath.Join(home, ".config", "systemd", "user")},
		{Name: "launch-agents", Path: filepath.Join(home, "Library", "LaunchAgents")},
	}
}

func quarantineRoots(name, path string) []quarantine.Root {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return []quarantine.Root{{Name: name, Path: path}}
}

func securityRulePackOptions(workspace string) rulepacks.Options {
	home := strings.TrimSpace(os.Getenv("HOME"))
	var userDirs []string
	if home != "" {
		userDirs = append(userDirs, filepath.Join(home, ".config", "milliways", "security", "rules"))
	}
	return rulepacks.Options{
		BundledDirs:   []string{"/usr/share/milliways/security/rules"},
		UserDirs:      userDirs,
		WorkspaceDirs: []string{filepath.Join(workspace, ".milliways", "security", "rules")},
		AllowNetwork:  false,
	}
}

func startupSeverityToStored(sev any) string {
	switch strings.ToLower(fmt.Sprint(sev)) {
	case "block":
		return "BLOCK"
	default:
		return "WARN"
	}
}

func profileSeverityToStored(sev clientprofiles.Severity) string {
	switch sev {
	case clientprofiles.SeverityCritical:
		return "BLOCK"
	default:
		return "WARN"
	}
}
