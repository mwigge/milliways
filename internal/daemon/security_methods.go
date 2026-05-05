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
	"encoding/json"
	"fmt"
	"time"

	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/security"
)

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
	writeResult(enc, req.ID, map[string]any{
		"enabled":      enabled,
		"scanner_path": scannerPath,
		"installed":    scannerPath != "",
	})
}
