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
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// SecurityFinding is one persisted OSV vulnerability finding.
type SecurityFinding struct {
	ID               int64
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
			(category, cve_id, package_name, installed_version, fixed_in_version, severity,
			 ecosystem, summary, scan_source, status, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cve_id, package_name, installed_version, ecosystem) DO UPDATE SET
			category         = excluded.category,
			fixed_in_version = excluded.fixed_in_version,
			severity         = excluded.severity,
			summary          = excluded.summary,
			scan_source      = excluded.scan_source,
			last_seen        = excluded.last_seen`,
		f.Category, f.CVEID, f.PackageName, f.InstalledVersion, f.FixedInVersion,
		f.Severity, f.Ecosystem, f.Summary, f.ScanSource, f.Status,
		firstSeen, now,
	)
	if err != nil {
		return fmt.Errorf("upsert security finding: %w", err)
	}
	return nil
}

// ListActive returns findings with status=active, optionally filtered to
// the given severity values. Excluded are findings with a non-expired
// accepted-risk entry. Results are ordered CRITICAL→HIGH→MEDIUM→LOW,
// then by first_seen descending.
func (s *SecurityStore) ListActive(severities []string) ([]SecurityFinding, error) {
	query := `
		SELECT f.id, f.cve_id, f.package_name, f.installed_version,
		       f.fixed_in_version, f.severity, f.ecosystem, f.summary,
		       f.scan_source, f.status, f.first_seen, f.last_seen, f.category
		FROM mw_security_findings f
		WHERE f.status = 'active'
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

// ListAll returns all findings regardless of status or accepted risks.
func (s *SecurityStore) ListAll() ([]SecurityFinding, error) {
	return s.queryFindings(`
		SELECT id, cve_id, package_name, installed_version, fixed_in_version,
		       severity, ecosystem, summary, scan_source, status, first_seen, last_seen, category
		FROM mw_security_findings
		ORDER BY first_seen DESC`)
}

// GetByCVE returns the first finding for the given CVE ID.
func (s *SecurityStore) GetByCVE(cveID string) (SecurityFinding, error) {
	findings, err := s.queryFindings(`
		SELECT id, cve_id, package_name, installed_version, fixed_in_version,
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
	rows, err := s.db.Query(`
		SELECT cve_id, package_name, installed_version, ecosystem
		FROM mw_security_findings
		WHERE scan_source = ? AND status = 'active'`, scanSource)
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
		if err := s.MarkResolved(r.cveID, r.pkg, r.ver, r.eco); err != nil {
			slog.Debug("MarkResolvedForSource: skip non-matching", "err", err)
		}
	}
	return nil
}

// MarkResolved sets a finding's status to "resolved".
func (s *SecurityStore) MarkResolved(cveID, packageName, installedVersion, ecosystem string) error {
	res, err := s.db.Exec(`
		UPDATE mw_security_findings SET status = 'resolved'
		WHERE cve_id = ? AND package_name = ? AND installed_version = ? AND ecosystem = ?`,
		cveID, packageName, installedVersion, ecosystem,
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

	if err := s.addFindingCounts(st.CountsByCategory, st.CountsBySeverity); err != nil {
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
		if err := rows.Scan(&f.ID, &f.CVEID, &f.PackageName, &f.InstalledVersion,
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

func (s *SecurityStore) addFindingCounts(byCategory, bySeverity map[string]int) error {
	rows, err := s.db.Query(`
		SELECT category, severity, COUNT(*)
		FROM mw_security_findings f
		WHERE f.status = 'active'
		  AND NOT EXISTS (
		      SELECT 1 FROM mw_security_accepted_risks r
		      WHERE r.cve_id = f.cve_id
		        AND r.package_name = f.package_name
		        AND r.expires_at > datetime('now')
		  )
		GROUP BY category, severity`)
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
