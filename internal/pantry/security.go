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
	"strings"
	"time"
)

// SecurityFinding is one persisted OSV vulnerability finding.
type SecurityFinding struct {
	ID               int64
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

// SecurityStore provides access to mw_security_findings and mw_security_accepted_risks.
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
	_, err := s.db.Exec(`
		INSERT INTO mw_security_findings
			(cve_id, package_name, installed_version, fixed_in_version, severity,
			 ecosystem, summary, scan_source, status, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cve_id, package_name, installed_version, ecosystem) DO UPDATE SET
			fixed_in_version = excluded.fixed_in_version,
			severity         = excluded.severity,
			summary          = excluded.summary,
			scan_source      = excluded.scan_source,
			last_seen        = excluded.last_seen`,
		f.CVEID, f.PackageName, f.InstalledVersion, f.FixedInVersion,
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
		       f.scan_source, f.status, f.first_seen, f.last_seen
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
		       severity, ecosystem, summary, scan_source, status, first_seen, last_seen
		FROM mw_security_findings
		ORDER BY first_seen DESC`)
}

// GetByCVE returns the first finding for the given CVE ID.
func (s *SecurityStore) GetByCVE(cveID string) (SecurityFinding, error) {
	findings, err := s.queryFindings(`
		SELECT id, cve_id, package_name, installed_version, fixed_in_version,
		       severity, ecosystem, summary, scan_source, status, first_seen, last_seen
		FROM mw_security_findings WHERE cve_id = ? LIMIT 1`, cveID)
	if err != nil {
		return SecurityFinding{}, err
	}
	if len(findings) == 0 {
		return SecurityFinding{}, fmt.Errorf("CVE %q not found", cveID)
	}
	return findings[0], nil
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
			&f.ScanSource, &f.Status, &firstSeen, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		f.FirstSeen = secParseTime(firstSeen)
		f.LastSeen = secParseTime(lastSeen)
		findings = append(findings, f)
	}
	return findings, rows.Err()
}
