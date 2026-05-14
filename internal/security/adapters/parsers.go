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

package adapters

import (
	"encoding/json"
	"strings"

	"github.com/mwigge/milliways/internal/security"
)

func parseGitleaks(workspace string, out []byte) ([]security.Finding, error) {
	var raw []struct {
		RuleID      string `json:"RuleID"`
		Description string `json:"Description"`
		File        string `json:"File"`
		StartLine   int    `json:"StartLine"`
		StartColumn int    `json:"StartColumn"`
		Secret      string `json:"Secret"`
		Fingerprint string `json:"Fingerprint"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	findings := make([]security.Finding, 0, len(raw))
	for _, item := range raw {
		findings = append(findings, security.Finding{
			ID:            item.RuleID,
			Category:      security.FindingSecret,
			Severity:      "HIGH",
			Summary:       fallback(item.Description, item.RuleID),
			WorkspacePath: workspace,
			FilePath:      workspaceRel(workspace, item.File),
			Line:          item.StartLine,
			Column:        item.StartColumn,
			ScanSource:    workspaceRel(workspace, item.File),
			EvidenceHash:  item.Fingerprint,
			Remediation:   "Rotate the exposed secret and remove it from source history.",
		})
	}
	return findings, nil
}

func parseSemgrep(workspace string, out []byte) ([]security.Finding, error) {
	var raw struct {
		Results []struct {
			CheckID string `json:"check_id"`
			Path    string `json:"path"`
			Start   struct {
				Line int `json:"line"`
				Col  int `json:"col"`
			} `json:"start"`
			Extra struct {
				Message  string `json:"message"`
				Severity string `json:"severity"`
				Metadata struct {
					CWE        []string `json:"cwe"`
					Confidence string   `json:"confidence"`
				} `json:"metadata"`
			} `json:"extra"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	findings := make([]security.Finding, 0, len(raw.Results))
	for _, item := range raw.Results {
		findings = append(findings, security.Finding{
			ID:            item.CheckID,
			Category:      security.FindingSAST,
			Severity:      normaliseSeverity(item.Extra.Severity),
			Summary:       fallback(item.Extra.Message, item.CheckID),
			WorkspacePath: workspace,
			FilePath:      workspaceRel(workspace, item.Path),
			Line:          item.Start.Line,
			Column:        item.Start.Col,
			ScanSource:    workspaceRel(workspace, item.Path),
			Remediation:   strings.Join(item.Extra.Metadata.CWE, ", "),
		})
	}
	return findings, nil
}

func parseGovulncheck(workspace string, out []byte) ([]security.Finding, error) {
	var findings []security.Finding
	vulns := make(map[string]struct {
		ID      string
		Aliases []string
		Summary string
	})
	err := parseJSONLines(out, func(line json.RawMessage) error {
		var event struct {
			Finding *struct {
				OSV   string `json:"osv"`
				Fixed string `json:"fixed_version"`
				Trace []struct {
					Module  string `json:"module"`
					Version string `json:"version"`
				} `json:"trace"`
			} `json:"finding"`
			OSV *struct {
				ID      string   `json:"id"`
				Aliases []string `json:"aliases"`
				Summary string   `json:"summary"`
			} `json:"osv"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return err
		}
		if event.OSV != nil {
			vulns[event.OSV.ID] = struct {
				ID      string
				Aliases []string
				Summary string
			}{ID: event.OSV.ID, Aliases: event.OSV.Aliases, Summary: event.OSV.Summary}
		}
		if event.Finding != nil {
			pkg, version := firstGoModule(event.Finding.Trace)
			v := vulns[event.Finding.OSV]
			findings = append(findings, security.Finding{
				ID:               event.Finding.OSV,
				Category:         security.FindingDependency,
				CVEID:            cveFromAliases(event.Finding.OSV, v.Aliases),
				PackageName:      pkg,
				InstalledVersion: version,
				FixedInVersion:   event.Finding.Fixed,
				Severity:         "UNKNOWN",
				Ecosystem:        "Go",
				Summary:          v.Summary,
				WorkspacePath:    workspace,
				ScanSource:       workspace,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return findings, nil
}

func firstGoModule(trace []struct {
	Module  string `json:"module"`
	Version string `json:"version"`
}) (string, string) {
	for _, frame := range trace {
		if frame.Module != "" {
			return frame.Module, frame.Version
		}
	}
	return "", ""
}

func parseOSVScanner(workspace string, out []byte) ([]security.Finding, error) {
	var raw struct {
		Results []struct {
			Source struct {
				Path string `json:"path"`
			} `json:"source"`
			Packages []struct {
				Package struct {
					Name      string `json:"name"`
					Version   string `json:"version"`
					Ecosystem string `json:"ecosystem"`
				} `json:"package"`
				Vulnerabilities []struct {
					ID       string   `json:"id"`
					Aliases  []string `json:"aliases"`
					Summary  string   `json:"summary"`
					Affected []struct {
						Ranges []struct {
							Events []struct {
								Fixed string `json:"fixed,omitempty"`
							} `json:"events"`
						} `json:"ranges"`
					} `json:"affected"`
				} `json:"vulnerabilities"`
				Groups []struct {
					IDs         []string `json:"ids"`
					MaxSeverity string   `json:"max_severity"`
				} `json:"groups"`
			} `json:"packages"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	var findings []security.Finding
	for _, result := range raw.Results {
		src := workspaceRel(workspace, result.Source.Path)
		for _, pkg := range result.Packages {
			for _, group := range pkg.Groups {
				id, summary, fixedIn := firstGroupDetail(group.IDs, pkg.Vulnerabilities)
				findings = append(findings, security.Finding{
					ID:               id,
					Category:         security.FindingDependency,
					CVEID:            id,
					PackageName:      pkg.Package.Name,
					InstalledVersion: pkg.Package.Version,
					FixedInVersion:   fixedIn,
					Severity:         normaliseSeverity(group.MaxSeverity),
					Ecosystem:        pkg.Package.Ecosystem,
					Summary:          summary,
					WorkspacePath:    workspace,
					ScanSource:       src,
				})
			}
		}
	}
	return findings, nil
}

func firstGroupDetail(ids []string, vulns []struct {
	ID       string   `json:"id"`
	Aliases  []string `json:"aliases"`
	Summary  string   `json:"summary"`
	Affected []struct {
		Ranges []struct {
			Events []struct {
				Fixed string `json:"fixed,omitempty"`
			} `json:"events"`
		} `json:"ranges"`
	} `json:"affected"`
}) (string, string, string) {
	id := ""
	for _, candidate := range ids {
		if strings.HasPrefix(candidate, "CVE-") {
			id = candidate
			break
		}
	}
	if id == "" && len(ids) > 0 {
		id = ids[0]
	}
	for _, vuln := range vulns {
		if vuln.ID == id || contains(ids, vuln.ID) || anyAliasIn(ids, vuln.Aliases) {
			return cveFromAliases(vuln.ID, vuln.Aliases), vuln.Summary, firstFixed(vuln.Affected)
		}
	}
	return id, "", ""
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func anyAliasIn(ids, aliases []string) bool {
	for _, alias := range aliases {
		if contains(ids, alias) {
			return true
		}
	}
	return false
}
