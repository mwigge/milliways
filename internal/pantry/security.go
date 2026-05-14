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

package pantry

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// SecurityFinding is one persisted OSV vulnerability finding.
type SecurityFinding struct {
	ID               int64
	Workspace        string
	Category         string
	CVEID            string
	PackageName      string
	InstalledVersion string
	FixedInVersion   string
	Severity         string
	Ecosystem        string
	Summary          string
	ScanSource       string
	Status           string // "active" | "resolved"
	FirstSeen        time.Time
	LastSeen         time.Time
}

// AcceptedRisk records a user-acknowledged CVE suppression.
type AcceptedRisk struct {
	ID          int64
	CVEID       string
	PackageName string
	Reason      string
	AcceptedAt  time.Time
	ExpiresAt   time.Time
}

// SecurityScanRun is one persisted security scan attempt.
type SecurityScanRun struct {
	ID            int64
	Kind          string
	Workspace     string
	Status        string
	StartedAt     time.Time
	CompletedAt   time.Time
	ToolName      string
	ToolVersion   string
	FindingsTotal int
	WarnCount     int
	BlockCount    int
	Error         string
}

// SecurityWarning is a non-CVE security posture warning or block.
type SecurityWarning struct {
	ID           int64
	Workspace    string
	Category     string
	Severity     string
	Source       string
	Message      string
	Status       string
	ScanRunID    int64
	FirstSeen    time.Time
	LastSeen     time.Time
	ResolvedAt   time.Time
	EvidenceHash string
	Remediation  string
}

// SecurityStatus is the durable workspace security summary.
type SecurityStatus struct {
	Workspace              string
	Mode                   string
	ActiveClient           string
	UpdatedAt              time.Time
	Posture                string
	StartupScanCompletedAt time.Time
	StartupScanConfigHash  string
	LastStartupScan        *SecurityScanRun
	LastDependencyScan     *SecurityScanRun
	CountsByCategory       map[string]int
	CountsBySeverity       map[string]int
	Warnings               []SecurityWarning
}

// SecurityRulePack is validated rule-pack metadata persisted by security rules RPCs.
type SecurityRulePack struct {
	ID                      int64
	Workspace               string
	Name                    string
	Version                 string
	Source                  string
	ManifestSource          string
	Checksum                string
	MinimumMilliWaysVersion string
	RulesFile               string
	RulesCount              int
	Root                    string
	ManifestPath            string
	RulesPath               string
	Status                  string
	FirstSeen               time.Time
	LastSeen                time.Time
}

// SecurityClientProfile is a cached client profile check result for one
// workspace/client/config hash.
type SecurityClientProfile struct {
	ID             int64
	Workspace      string
	Client         string
	ConfigHash     string
	WarningCount   int
	BlockCount     int
	Status         string
	ResultJSON     string
	Error          string
	FirstCheckedAt time.Time
	LastCheckedAt  time.Time
}

// SecurityQuarantineAction is a durable record of one quarantine apply action.
type SecurityQuarantineAction struct {
	ID               int64
	Workspace        string
	Kind             string
	SourcePath       string
	DestinationPath  string
	OriginalHash     string
	AppliedHash      string
	Status           string
	Error            string
	RollbackHint     string
	AdditionalFields map[string]string
	AppliedAt        time.Time
}

// SecurityPolicyDecision is one durable Security Control Plane policy/audit event.
type SecurityPolicyDecision struct {
	ID               int64
	CreatedAt        time.Time
	Workspace        string
	SessionID        string
	Client           string
	CWD              string
	OperationType    string
	Command          string
	ArgvJSON         string
	EnvSummaryJSON   string
	Mode             string
	Decision         string
	Reason           string
	Parsed           bool
	RisksJSON        string
	EnforcementLevel string
}

// SecurityStore provides access to durable security posture tables.
type SecurityStore struct {
	db *sql.DB
}

const secTimeLayout = "2006-01-02T15:04:05Z"

func secFormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(secTimeLayout)
}

func secParseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(secTimeLayout, s)
	return t
}

// UpsertFinding inserts a new finding or updates summary, fixed_in_version,
// severity, scan_source, and last_seen on conflict.
func (s *SecurityStore) UpsertFinding(f SecurityFinding) error {
	now := secFormatTime(time.Now().UTC())
	firstSeen := secFormatTime(f.FirstSeen)
	if firstSeen == "" {
		firstSeen = now
	}
	if f.Category == "" {
		f.Category = "dependency"
	}
	if f.Status == "" {
		f.Status = "active"
	}
	_, err := s.db.Exec(`
		INSERT INTO mw_security_findings
			(workspace, category, cve_id, package_name, installed_version, fixed_in_version, severity,
			 ecosystem, summary, scan_source, status, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace, cve_id, package_name, installed_version, ecosystem) DO UPDATE SET
			category         = excluded.category,
			fixed_in_version = excluded.fixed_in_version,
			severity         = excluded.severity,
			summary          = excluded.summary,
			scan_source      = excluded.scan_source,
			status           = excluded.status,
			last_seen        = excluded.last_seen`,
		f.Workspace, f.Category, f.CVEID, f.PackageName, f.InstalledVersion, f.FixedInVersion,
		f.Severity, f.Ecosystem, f.Summary, f.ScanSource, f.Status,
		firstSeen, now,
	)
	if err != nil {
		return fmt.Errorf("upsert security finding: %w", err)
	}
	return nil
}

// ListActive returns active or blocked findings, optionally filtered to the
// given severity values. Excluded are findings with a non-expired accepted-risk
// entry. Results are ordered CRITICAL→HIGH→MEDIUM→LOW, then by first_seen
// descending.
func (s *SecurityStore) ListActive(severities []string) ([]SecurityFinding, error) {
	query := `
		SELECT f.id, f.workspace, f.cve_id, f.package_name, f.installed_version,
		       f.fixed_in_version, f.severity, f.ecosystem, f.summary,
		       f.scan_source, f.status, f.first_seen, f.last_seen, f.category
		FROM mw_security_findings f
		WHERE f.status IN ('active', 'blocked')
		  AND NOT EXISTS (
		      SELECT 1 FROM mw_security_accepted_risks r
		      WHERE r.cve_id = f.cve_id
		        AND r.package_name = f.package_name
		        AND r.expires_at > datetime('now')
		  )`

	var args []any
	if len(severities) > 0 {
		placeholders := strings.Repeat("?,", len(severities))
		placeholders = placeholders[:len(placeholders)-1]
		query += " AND f.severity IN (" + placeholders + ")"
		for _, sv := range severities {
			args = append(args, sv)
		}
	}

	query += ` ORDER BY CASE f.severity
		WHEN 'CRITICAL' THEN 1 WHEN 'HIGH' THEN 2
		WHEN 'MEDIUM' THEN 3 WHEN 'LOW' THEN 4 ELSE 5 END,
		f.first_seen DESC`

	return s.queryFindings(query, args...)
}

// ListActiveForWorkspace returns active or blocked findings, optionally
// filtered to the given severity values, for one workspace.
func (s *SecurityStore) ListActiveForWorkspace(workspace string, severities []string) ([]SecurityFinding, error) {
	query := `
		SELECT f.id, f.workspace, f.cve_id, f.package_name, f.installed_version,
		       f.fixed_in_version, f.severity, f.ecosystem, f.summary,
		       f.scan_source, f.status, f.first_seen, f.last_seen, f.category
		FROM mw_security_findings f
		WHERE f.workspace = ? AND f.status IN ('active', 'blocked')
		  AND NOT EXISTS (
		      SELECT 1 FROM mw_security_accepted_risks r
		      WHERE r.cve_id = f.cve_id
		        AND r.package_name = f.package_name
		        AND r.expires_at > datetime('now')
		  )`

	args := []any{workspace}
	if len(severities) > 0 {
		placeholders := strings.Repeat("?,", len(severities))
		placeholders = placeholders[:len(placeholders)-1]
		query += " AND f.severity IN (" + placeholders + ")"
		for _, sv := range severities {
			args = append(args, sv)
		}
	}

	query += ` ORDER BY CASE f.severity
		WHEN 'CRITICAL' THEN 1 WHEN 'HIGH' THEN 2
		WHEN 'MEDIUM' THEN 3 WHEN 'LOW' THEN 4 ELSE 5 END,
		f.first_seen DESC`

	return s.queryFindings(query, args...)
}

// ListAll returns all findings regardless of status or accepted risks.
func (s *SecurityStore) ListAll() ([]SecurityFinding, error) {
	return s.queryFindings(`
		SELECT id, workspace, cve_id, package_name, installed_version, fixed_in_version,
		       severity, ecosystem, summary, scan_source, status, first_seen, last_seen, category
		FROM mw_security_findings
		ORDER BY first_seen DESC`)
}

// ListAllForWorkspace returns all findings for one workspace regardless of status.
func (s *SecurityStore) ListAllForWorkspace(workspace string) ([]SecurityFinding, error) {
	return s.queryFindings(`
		SELECT id, workspace, cve_id, package_name, installed_version, fixed_in_version,
		       severity, ecosystem, summary, scan_source, status, first_seen, last_seen, category
		FROM mw_security_findings
		WHERE workspace = ?
		ORDER BY first_seen DESC`, workspace)
}

// GetByCVE returns the first finding for the given CVE ID.
func (s *SecurityStore) GetByCVE(cveID string) (SecurityFinding, error) {
	findings, err := s.queryFindings(`
		SELECT id, workspace, cve_id, package_name, installed_version, fixed_in_version,
		       severity, ecosystem, summary, scan_source, status, first_seen, last_seen, category
		FROM mw_security_findings WHERE cve_id = ? LIMIT 1`, cveID)
	if err != nil {
		return SecurityFinding{}, err
	}
	if len(findings) == 0 {
		return SecurityFinding{}, fmt.Errorf("CVE %q not found", cveID)
	}
	return findings[0], nil
}

// MarkResolvedForSource marks all active findings for a given scan_source as
// resolved UNLESS their "cve_id:package_name" key appears in keepKeys. Called
// by the runner after each scan to retire stale findings for one lockfile.
func (s *SecurityStore) MarkResolvedForSource(scanSource string, keepKeys map[string]struct{}) error {
	return s.MarkResolvedForWorkspaceSource("", scanSource, keepKeys)
}

// MarkResolvedForWorkspaceSource marks stale active findings for one source in one workspace.
func (s *SecurityStore) MarkResolvedForWorkspaceSource(workspace, scanSource string, keepKeys map[string]struct{}) error {
	rows, err := s.db.Query(`
		SELECT cve_id, package_name, installed_version, ecosystem
		FROM mw_security_findings
		WHERE workspace = ? AND scan_source = ? AND status = 'active'`, workspace, scanSource)
	if err != nil {
		return fmt.Errorf("query findings for source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type row struct{ cveID, pkg, ver, eco string }
	var toResolve []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.cveID, &r.pkg, &r.ver, &r.eco); err != nil {
			return fmt.Errorf("scan finding row: %w", err)
		}
		key := r.cveID + ":" + r.pkg
		if _, keep := keepKeys[key]; !keep {
			toResolve = append(toResolve, r)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, r := range toResolve {
		if err := s.MarkResolvedForWorkspace(workspace, r.cveID, r.pkg, r.ver, r.eco); err != nil {
			slog.Debug("MarkResolvedForSource: skip non-matching", "err", err)
		}
	}
	return nil
}

// MarkResolved sets a finding's status to "resolved".
func (s *SecurityStore) MarkResolved(cveID, packageName, installedVersion, ecosystem string) error {
	return s.MarkResolvedForWorkspace("", cveID, packageName, installedVersion, ecosystem)
}

// MarkResolvedForWorkspace sets a finding's status to "resolved" for one workspace.
func (s *SecurityStore) MarkResolvedForWorkspace(workspace, cveID, packageName, installedVersion, ecosystem string) error {
	res, err := s.db.Exec(`
		UPDATE mw_security_findings SET status = 'resolved'
		WHERE workspace = ? AND cve_id = ? AND package_name = ? AND installed_version = ? AND ecosystem = ?`,
		workspace, cveID, packageName, installedVersion, ecosystem,
	)
	if err != nil {
		return fmt.Errorf("mark resolved: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mark resolved: no matching finding for %s/%s@%s (%s)", cveID, packageName, installedVersion, ecosystem)
	}
	return nil
}

// InsertAcceptedRisk records a user-acknowledged suppression for a CVE.
// Upserts on (cve_id, package_name) so re-accepting updates reason and expiry.
func (s *SecurityStore) InsertAcceptedRisk(cveID, packageName, reason string, expiresAt time.Time) error {
	now := secFormatTime(time.Now().UTC())
	_, err := s.db.Exec(`
		INSERT INTO mw_security_accepted_risks
			(cve_id, package_name, reason, accepted_at, expires_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(cve_id, package_name) DO UPDATE SET
			reason      = excluded.reason,
			accepted_at = excluded.accepted_at,
			expires_at  = excluded.expires_at`,
		cveID, packageName, reason, now, secFormatTime(expiresAt),
	)
	if err != nil {
		return fmt.Errorf("insert accepted risk: %w", err)
	}
	return nil
}

// ListAcceptedRisks returns all accepted-risk entries.
func (s *SecurityStore) ListAcceptedRisks() ([]AcceptedRisk, error) {
	rows, err := s.db.Query(`
		SELECT id, cve_id, package_name, reason, accepted_at, expires_at
		FROM mw_security_accepted_risks ORDER BY accepted_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list accepted risks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var risks []AcceptedRisk
	for rows.Next() {
		var r AcceptedRisk
		var acceptedAt, expiresAt string
		if err := rows.Scan(&r.ID, &r.CVEID, &r.PackageName, &r.Reason, &acceptedAt, &expiresAt); err != nil {
			return nil, fmt.Errorf("scan accepted risk: %w", err)
		}
		r.AcceptedAt = secParseTime(acceptedAt)
		r.ExpiresAt = secParseTime(expiresAt)
		risks = append(risks, r)
	}
	return risks, rows.Err()
}

// CVEExists returns true if any finding with the given CVE ID exists.
func (s *SecurityStore) CVEExists(cveID string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM mw_security_findings WHERE cve_id = ?`, cveID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return n > 0, err
}

// SetWorkspaceStatus upserts the mode and active client for a workspace.
func (s *SecurityStore) SetWorkspaceStatus(workspace, mode, activeClient string) error {
	if mode == "" {
		mode = "warn"
	}
	_, err := s.db.Exec(`
		INSERT INTO mw_security_workspace_status (workspace, mode, active_client, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(workspace) DO UPDATE SET
			mode = excluded.mode,
			active_client = excluded.active_client,
			updated_at = excluded.updated_at`,
		workspace, mode, activeClient, secFormatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("set workspace security status: %w", err)
	}
	return nil
}

// MarkStartupScanCompleted records that the mandatory startup scan has
// completed for this workspace and startup scan configuration.
func (s *SecurityStore) MarkStartupScanCompleted(workspace, configHash string) error {
	now := secFormatTime(time.Now().UTC())
	_, err := s.db.Exec(`
		INSERT INTO mw_security_workspace_status
			(workspace, mode, active_client, updated_at, startup_scan_completed_at, startup_scan_config_hash)
		VALUES (?, 'warn', '', ?, ?, ?)
		ON CONFLICT(workspace) DO UPDATE SET
			updated_at = excluded.updated_at,
			startup_scan_completed_at = excluded.startup_scan_completed_at,
			startup_scan_config_hash = excluded.startup_scan_config_hash`,
		workspace, now, now, configHash)
	if err != nil {
		return fmt.Errorf("mark startup scan completed: %w", err)
	}
	return nil
}

// InsertScanRun records the start of a security scan and returns its row ID.
func (s *SecurityStore) InsertScanRun(run SecurityScanRun) (int64, error) {
	if run.Status == "" {
		run.Status = "running"
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	res, err := s.db.Exec(`
		INSERT INTO mw_security_scan_runs
			(kind, workspace, status, started_at, completed_at, tool_name, tool_version,
			 findings_total, warn_count, block_count, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.Kind, run.Workspace, run.Status, secFormatTime(run.StartedAt),
		secFormatTime(run.CompletedAt), run.ToolName, run.ToolVersion,
		run.FindingsTotal, run.WarnCount, run.BlockCount, run.Error)
	if err != nil {
		return 0, fmt.Errorf("insert security scan run: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("security scan run id: %w", err)
	}
	return id, nil
}

// CompleteScanRun updates the terminal metadata for an existing scan run.
func (s *SecurityStore) CompleteScanRun(id int64, status string, findingsTotal, warnCount, blockCount int, scanErr string) error {
	if status == "" {
		status = "completed"
	}
	_, err := s.db.Exec(`
		UPDATE mw_security_scan_runs
		SET status = ?, completed_at = ?, findings_total = ?, warn_count = ?, block_count = ?, error = ?
		WHERE id = ?`,
		status, secFormatTime(time.Now().UTC()), findingsTotal, warnCount, blockCount, scanErr, id)
	if err != nil {
		return fmt.Errorf("complete security scan run: %w", err)
	}
	return nil
}

// UpsertWarning inserts or refreshes an active security warning.
func (s *SecurityStore) UpsertWarning(w SecurityWarning) error {
	now := secFormatTime(time.Now().UTC())
	firstSeen := secFormatTime(w.FirstSeen)
	if firstSeen == "" {
		firstSeen = now
	}
	if w.Status == "" {
		w.Status = "active"
	}
	if w.Severity == "" {
		w.Severity = "WARN"
	}
	_, err := s.db.Exec(`
		INSERT INTO mw_security_warnings
			(workspace, category, severity, source, message, status, scan_run_id,
			 first_seen, last_seen, resolved_at, evidence_hash, remediation)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace, category, source, message) DO UPDATE SET
			severity = excluded.severity,
			status = excluded.status,
			scan_run_id = excluded.scan_run_id,
			last_seen = excluded.last_seen,
			resolved_at = excluded.resolved_at,
			evidence_hash = excluded.evidence_hash,
			remediation = excluded.remediation`,
		w.Workspace, w.Category, w.Severity, w.Source, w.Message, w.Status, w.ScanRunID,
		firstSeen, now, secFormatTime(w.ResolvedAt), w.EvidenceHash, w.Remediation)
	if err != nil {
		return fmt.Errorf("upsert security warning: %w", err)
	}
	return nil
}

// ResolveWarningsNotSeen marks previously active warnings as resolved when a
// new scan over the same warning ownership surface no longer reports them.
func (s *SecurityStore) ResolveWarningsNotSeen(workspace string, categories []string, sourcePrefix string, active []SecurityWarning) error {
	if len(categories) == 0 {
		return nil
	}
	cats := make(map[string]struct{}, len(categories))
	for _, c := range categories {
		c = strings.TrimSpace(c)
		if c != "" {
			cats[c] = struct{}{}
		}
	}
	if len(cats) == 0 {
		return nil
	}
	keep := make(map[string]struct{}, len(active))
	for _, w := range active {
		keep[warningKey(w)] = struct{}{}
	}
	warnings, err := s.ListActiveWarnings(workspace)
	if err != nil {
		return err
	}
	now := secFormatTime(time.Now().UTC())
	for _, w := range warnings {
		if _, ok := cats[w.Category]; !ok {
			continue
		}
		if sourcePrefix != "" && !strings.HasPrefix(w.Source, sourcePrefix) {
			continue
		}
		if _, ok := keep[warningKey(w)]; ok {
			continue
		}
		if _, err := s.db.Exec(`
			UPDATE mw_security_warnings
			SET status = 'resolved', resolved_at = ?
			WHERE id = ? AND status = 'active'`, now, w.ID); err != nil {
			return fmt.Errorf("resolve stale security warning: %w", err)
		}
	}
	return nil
}

func warningKey(w SecurityWarning) string {
	return w.Category + "\x00" + w.Source + "\x00" + w.Message
}

// ListActiveWarnings returns active warnings for one workspace. Empty workspace
// matches global warnings only.
func (s *SecurityStore) ListActiveWarnings(workspace string) ([]SecurityWarning, error) {
	return s.queryWarnings(`
		SELECT id, workspace, category, severity, source, message, status, scan_run_id,
		       first_seen, last_seen, resolved_at, evidence_hash, remediation
		FROM mw_security_warnings
		WHERE workspace = ? AND status = 'active'
		ORDER BY CASE severity
			WHEN 'BLOCK' THEN 1 WHEN 'CRITICAL' THEN 2 WHEN 'HIGH' THEN 3
			WHEN 'WARN' THEN 4 WHEN 'MEDIUM' THEN 5 WHEN 'LOW' THEN 6 ELSE 7 END,
			last_seen DESC`, workspace)
}

// SecurityStatus returns the aggregated workspace security status.
func (s *SecurityStore) SecurityStatus(workspace string) (SecurityStatus, error) {
	st := SecurityStatus{
		Workspace:        workspace,
		Mode:             "warn",
		CountsByCategory: make(map[string]int),
		CountsBySeverity: make(map[string]int),
		Posture:          "ok",
	}

	var updatedAt, startupScanCompletedAt string
	err := s.db.QueryRow(`
		SELECT mode, active_client, updated_at, startup_scan_completed_at, startup_scan_config_hash
		FROM mw_security_workspace_status WHERE workspace = ?`, workspace).
		Scan(&st.Mode, &st.ActiveClient, &updatedAt, &startupScanCompletedAt, &st.StartupScanConfigHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SecurityStatus{}, fmt.Errorf("query workspace security status: %w", err)
	}
	st.UpdatedAt = secParseTime(updatedAt)
	st.StartupScanCompletedAt = secParseTime(startupScanCompletedAt)

	startup, err := s.lastScanRun(workspace, "startup")
	if err != nil {
		return SecurityStatus{}, err
	}
	st.LastStartupScan = startup
	dep, err := s.lastScanRun(workspace, "dependency")
	if err != nil {
		return SecurityStatus{}, err
	}
	st.LastDependencyScan = dep

	if err := s.addFindingCounts(workspace, st.CountsByCategory, st.CountsBySeverity); err != nil {
		return SecurityStatus{}, err
	}
	warnings, err := s.ListActiveWarnings(workspace)
	if err != nil {
		return SecurityStatus{}, err
	}
	st.Warnings = warnings
	for _, w := range warnings {
		st.CountsByCategory[w.Category]++
		st.CountsBySeverity[w.Severity]++
	}
	if st.CountsBySeverity["BLOCK"] > 0 {
		st.Posture = "block"
	} else if len(warnings) > 0 || hasAnyCount(st.CountsBySeverity) {
		st.Posture = "warn"
	}
	return st, nil
}

// UpsertRulePack records validated rule-pack metadata. The caller is expected
// to only pass packs after manifest and checksum validation.
func (s *SecurityStore) UpsertRulePack(p SecurityRulePack) error {
	now := secFormatTime(time.Now().UTC())
	firstSeen := secFormatTime(p.FirstSeen)
	if firstSeen == "" {
		firstSeen = now
	}
	if p.Status == "" {
		p.Status = "loaded"
	}
	_, err := s.db.Exec(`
		INSERT INTO mw_security_rule_packs
			(workspace, name, version, source, manifest_source, checksum,
			 minimum_milliways_version, rules_file, rules_count, root,
			 manifest_path, rules_path, status, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace, source, name, version, root) DO UPDATE SET
			manifest_source = excluded.manifest_source,
			checksum = excluded.checksum,
			minimum_milliways_version = excluded.minimum_milliways_version,
			rules_file = excluded.rules_file,
			rules_count = excluded.rules_count,
			manifest_path = excluded.manifest_path,
			rules_path = excluded.rules_path,
			status = excluded.status,
			last_seen = excluded.last_seen`,
		p.Workspace, p.Name, p.Version, p.Source, p.ManifestSource, p.Checksum,
		p.MinimumMilliWaysVersion, p.RulesFile, p.RulesCount, p.Root,
		p.ManifestPath, p.RulesPath, p.Status, firstSeen, now)
	if err != nil {
		return fmt.Errorf("upsert security rule pack: %w", err)
	}
	return nil
}

// ListRulePacks returns persisted rule-pack metadata for a workspace.
func (s *SecurityStore) ListRulePacks(workspace string) ([]SecurityRulePack, error) {
	rows, err := s.db.Query(`
		SELECT id, workspace, name, version, source, manifest_source, checksum,
		       minimum_milliways_version, rules_file, rules_count, root,
		       manifest_path, rules_path, status, first_seen, last_seen
		FROM mw_security_rule_packs
		WHERE workspace = ?
		ORDER BY source, name, version, root`, workspace)
	if err != nil {
		return nil, fmt.Errorf("list security rule packs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var packs []SecurityRulePack
	for rows.Next() {
		var p SecurityRulePack
		var firstSeen, lastSeen string
		if err := rows.Scan(&p.ID, &p.Workspace, &p.Name, &p.Version, &p.Source,
			&p.ManifestSource, &p.Checksum, &p.MinimumMilliWaysVersion, &p.RulesFile,
			&p.RulesCount, &p.Root, &p.ManifestPath, &p.RulesPath, &p.Status,
			&firstSeen, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan security rule pack: %w", err)
		}
		p.FirstSeen = secParseTime(firstSeen)
		p.LastSeen = secParseTime(lastSeen)
		packs = append(packs, p)
	}
	return packs, rows.Err()
}

// UpsertClientProfile inserts or refreshes a cached client profile check.
func (s *SecurityStore) UpsertClientProfile(p SecurityClientProfile) error {
	now := secFormatTime(time.Now().UTC())
	firstCheckedAt := secFormatTime(p.FirstCheckedAt)
	if firstCheckedAt == "" {
		firstCheckedAt = now
	}
	if p.Status == "" {
		p.Status = "completed"
	}
	if p.ResultJSON == "" {
		p.ResultJSON = "{}"
	}
	_, err := s.db.Exec(`
		INSERT INTO mw_security_client_profiles
			(workspace, client, config_hash, warning_count, block_count, status,
			 result_json, error, first_checked_at, last_checked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace, client, config_hash) DO UPDATE SET
			warning_count = excluded.warning_count,
			block_count = excluded.block_count,
			status = excluded.status,
			result_json = excluded.result_json,
			error = excluded.error,
			last_checked_at = excluded.last_checked_at`,
		p.Workspace, p.Client, p.ConfigHash, p.WarningCount, p.BlockCount, p.Status,
		p.ResultJSON, p.Error, firstCheckedAt, now)
	if err != nil {
		return fmt.Errorf("upsert security client profile: %w", err)
	}
	return nil
}

// GetClientProfile returns a cached profile check for the exact config hash.
func (s *SecurityStore) GetClientProfile(workspace, client, configHash string) (SecurityClientProfile, bool, error) {
	rows, err := s.db.Query(`
		SELECT id, workspace, client, config_hash, warning_count, block_count,
		       status, result_json, error, first_checked_at, last_checked_at
		FROM mw_security_client_profiles
		WHERE workspace = ? AND client = ? AND config_hash = ?
		LIMIT 1`, workspace, client, configHash)
	if err != nil {
		return SecurityClientProfile{}, false, fmt.Errorf("get security client profile: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return SecurityClientProfile{}, false, rows.Err()
	}
	profile, err := scanSecurityClientProfile(rows)
	if err != nil {
		return SecurityClientProfile{}, false, err
	}
	return profile, true, rows.Err()
}

// ListClientProfiles returns cached profile checks for a workspace, optionally
// filtered by client. Most recent checks are returned first.
func (s *SecurityStore) ListClientProfiles(workspace, client string) ([]SecurityClientProfile, error) {
	query := `
		SELECT id, workspace, client, config_hash, warning_count, block_count,
		       status, result_json, error, first_checked_at, last_checked_at
		FROM mw_security_client_profiles
		WHERE workspace = ?`
	args := []any{workspace}
	if client != "" {
		query += ` AND client = ?`
		args = append(args, client)
	}
	query += ` ORDER BY last_checked_at DESC, id DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list security client profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var profiles []SecurityClientProfile
	for rows.Next() {
		profile, err := scanSecurityClientProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

// RecordQuarantineAction persists the outcome of one quarantine apply action.
func (s *SecurityStore) RecordQuarantineAction(a SecurityQuarantineAction) error {
	if a.Status == "" {
		a.Status = "unknown"
	}
	if a.AppliedAt.IsZero() {
		a.AppliedAt = time.Now().UTC()
	}
	fields, err := json.Marshal(a.AdditionalFields)
	if err != nil {
		return fmt.Errorf("marshal quarantine action fields: %w", err)
	}
	if string(fields) == "null" {
		fields = []byte("{}")
	}
	_, err = s.db.Exec(`
		INSERT INTO mw_security_quarantine_actions
			(workspace, kind, source_path, destination_path, original_hash, applied_hash,
			 status, error, rollback_hint, additional_fields_json, applied_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Workspace, a.Kind, a.SourcePath, a.DestinationPath, a.OriginalHash, a.AppliedHash,
		a.Status, a.Error, a.RollbackHint, string(fields), secFormatTime(a.AppliedAt))
	if err != nil {
		return fmt.Errorf("record quarantine action: %w", err)
	}
	return nil
}

// ListQuarantineActions returns quarantine apply records for a workspace.
func (s *SecurityStore) ListQuarantineActions(workspace string) ([]SecurityQuarantineAction, error) {
	rows, err := s.db.Query(`
		SELECT id, workspace, kind, source_path, destination_path, original_hash, applied_hash,
		       status, error, rollback_hint, additional_fields_json, applied_at
		FROM mw_security_quarantine_actions
		WHERE workspace = ?
		ORDER BY applied_at DESC, id DESC`, workspace)
	if err != nil {
		return nil, fmt.Errorf("list quarantine actions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var actions []SecurityQuarantineAction
	for rows.Next() {
		var a SecurityQuarantineAction
		var fieldsJSON, appliedAt string
		if err := rows.Scan(&a.ID, &a.Workspace, &a.Kind, &a.SourcePath, &a.DestinationPath,
			&a.OriginalHash, &a.AppliedHash, &a.Status, &a.Error, &a.RollbackHint,
			&fieldsJSON, &appliedAt); err != nil {
			return nil, fmt.Errorf("scan quarantine action: %w", err)
		}
		a.AppliedAt = secParseTime(appliedAt)
		if fieldsJSON != "" {
			if err := json.Unmarshal([]byte(fieldsJSON), &a.AdditionalFields); err != nil {
				return nil, fmt.Errorf("decode quarantine action fields: %w", err)
			}
		}
		if a.AdditionalFields == nil {
			a.AdditionalFields = map[string]string{}
		}
		actions = append(actions, a)
	}
	return actions, rows.Err()
}

// RecordPolicyDecision appends one durable policy/audit decision.
func (s *SecurityStore) RecordPolicyDecision(d SecurityPolicyDecision) error {
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(d.ArgvJSON) == "" {
		d.ArgvJSON = "[]"
	}
	if strings.TrimSpace(d.EnvSummaryJSON) == "" {
		d.EnvSummaryJSON = "{}"
	}
	if strings.TrimSpace(d.RisksJSON) == "" {
		d.RisksJSON = "[]"
	}
	parsed := 0
	if d.Parsed {
		parsed = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO mw_security_policy_decisions
			(created_at, workspace, session_id, client, cwd, operation_type, command,
			 argv_json, env_summary_json, mode, decision, reason, parsed, risks_json, enforcement_level)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		secFormatTime(d.CreatedAt), d.Workspace, d.SessionID, d.Client, d.CWD, d.OperationType, d.Command,
		d.ArgvJSON, d.EnvSummaryJSON, d.Mode, d.Decision, d.Reason, parsed, d.RisksJSON, d.EnforcementLevel,
	)
	if err != nil {
		return fmt.Errorf("record security policy decision: %w", err)
	}
	return nil
}

// ListPolicyDecisions returns recent policy decisions for audit callers.
func (s *SecurityStore) ListPolicyDecisions(workspace string, limit int) ([]SecurityPolicyDecision, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT id, created_at, workspace, session_id, client, cwd, operation_type,
		       command, argv_json, env_summary_json, mode, decision, reason, parsed,
		       risks_json, enforcement_level
		FROM mw_security_policy_decisions`
	var args []any
	if strings.TrimSpace(workspace) != "" {
		query += " WHERE workspace = ?"
		args = append(args, workspace)
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list security policy decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var decisions []SecurityPolicyDecision
	for rows.Next() {
		var d SecurityPolicyDecision
		var createdAt string
		var parsed int
		if err := rows.Scan(&d.ID, &createdAt, &d.Workspace, &d.SessionID, &d.Client, &d.CWD,
			&d.OperationType, &d.Command, &d.ArgvJSON, &d.EnvSummaryJSON, &d.Mode, &d.Decision,
			&d.Reason, &parsed, &d.RisksJSON, &d.EnforcementLevel); err != nil {
			return nil, fmt.Errorf("scan security policy decision: %w", err)
		}
		d.CreatedAt = secParseTime(createdAt)
		d.Parsed = parsed != 0
		decisions = append(decisions, d)
	}
	return decisions, rows.Err()
}

func (s *SecurityStore) queryFindings(query string, args ...any) ([]SecurityFinding, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var findings []SecurityFinding
	for rows.Next() {
		var f SecurityFinding
		var firstSeen, lastSeen string
		if err := rows.Scan(&f.ID, &f.Workspace, &f.CVEID, &f.PackageName, &f.InstalledVersion,
			&f.FixedInVersion, &f.Severity, &f.Ecosystem, &f.Summary,
			&f.ScanSource, &f.Status, &firstSeen, &lastSeen, &f.Category); err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		f.FirstSeen = secParseTime(firstSeen)
		f.LastSeen = secParseTime(lastSeen)
		findings = append(findings, f)
	}
	return findings, rows.Err()
}

func (s *SecurityStore) queryWarnings(query string, args ...any) ([]SecurityWarning, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query security warnings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var warnings []SecurityWarning
	for rows.Next() {
		var w SecurityWarning
		var firstSeen, lastSeen, resolvedAt string
		if err := rows.Scan(&w.ID, &w.Workspace, &w.Category, &w.Severity, &w.Source,
			&w.Message, &w.Status, &w.ScanRunID, &firstSeen, &lastSeen, &resolvedAt,
			&w.EvidenceHash, &w.Remediation); err != nil {
			return nil, fmt.Errorf("scan security warning: %w", err)
		}
		w.FirstSeen = secParseTime(firstSeen)
		w.LastSeen = secParseTime(lastSeen)
		w.ResolvedAt = secParseTime(resolvedAt)
		warnings = append(warnings, w)
	}
	return warnings, rows.Err()
}

func (s *SecurityStore) lastScanRun(workspace, kind string) (*SecurityScanRun, error) {
	rows, err := s.db.Query(`
		SELECT id, kind, workspace, status, started_at, completed_at, tool_name, tool_version,
		       findings_total, warn_count, block_count, error
		FROM mw_security_scan_runs
		WHERE workspace = ? AND kind = ?
		ORDER BY started_at DESC, id DESC
		LIMIT 1`, workspace, kind)
	if err != nil {
		return nil, fmt.Errorf("query last security scan run: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, rows.Err()
	}
	run, err := scanSecurityRun(rows)
	if err != nil {
		return nil, err
	}
	return &run, rows.Err()
}

func scanSecurityRun(rows *sql.Rows) (SecurityScanRun, error) {
	var run SecurityScanRun
	var startedAt, completedAt string
	if err := rows.Scan(&run.ID, &run.Kind, &run.Workspace, &run.Status, &startedAt,
		&completedAt, &run.ToolName, &run.ToolVersion, &run.FindingsTotal,
		&run.WarnCount, &run.BlockCount, &run.Error); err != nil {
		return SecurityScanRun{}, fmt.Errorf("scan security run: %w", err)
	}
	run.StartedAt = secParseTime(startedAt)
	run.CompletedAt = secParseTime(completedAt)
	return run, nil
}

type securityClientProfileScanner interface {
	Scan(dest ...any) error
}

func scanSecurityClientProfile(rows securityClientProfileScanner) (SecurityClientProfile, error) {
	var p SecurityClientProfile
	var firstCheckedAt, lastCheckedAt string
	if err := rows.Scan(&p.ID, &p.Workspace, &p.Client, &p.ConfigHash,
		&p.WarningCount, &p.BlockCount, &p.Status, &p.ResultJSON, &p.Error,
		&firstCheckedAt, &lastCheckedAt); err != nil {
		return SecurityClientProfile{}, fmt.Errorf("scan security client profile: %w", err)
	}
	p.FirstCheckedAt = secParseTime(firstCheckedAt)
	p.LastCheckedAt = secParseTime(lastCheckedAt)
	return p, nil
}

func (s *SecurityStore) addFindingCounts(workspace string, byCategory, bySeverity map[string]int) error {
	rows, err := s.db.Query(`
		SELECT category, severity, COUNT(*)
		FROM mw_security_findings f
		WHERE f.workspace = ? AND f.status IN ('active', 'blocked')
		  AND NOT EXISTS (
		      SELECT 1 FROM mw_security_accepted_risks r
		      WHERE r.cve_id = f.cve_id
		        AND r.package_name = f.package_name
		        AND r.expires_at > datetime('now')
		  )
		GROUP BY category, severity`, workspace)
	if err != nil {
		return fmt.Errorf("query finding counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var category, severity string
		var count int
		if err := rows.Scan(&category, &severity, &count); err != nil {
			return fmt.Errorf("scan finding counts: %w", err)
		}
		byCategory[category] += count
		bySeverity[severity] += count
	}
	return rows.Err()
}

func hasAnyCount(counts map[string]int) bool {
	for _, count := range counts {
		if count > 0 {
			return true
		}
	}
	return false
}
