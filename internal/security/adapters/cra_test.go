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

package adapters_test

import (
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/security/adapters"
)

func TestCRAAdapterEvaluatesCompleteEvidence(t *testing.T) {
	t.Parallel()

	supportUntil := time.Date(2029, 12, 31, 0, 0, 0, 0, time.UTC)
	report := adapters.NewCRAAdapter().Evaluate(adapters.CRAEvidenceInput{
		ProductName:                     "MilliWays",
		AsOf:                            time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
		SBOMPaths:                       []string{"dist/milliways.spdx.json"},
		VulnerabilityHandlingPolicy:     "SECURITY.md",
		VulnerabilityReportingContact:   "security@example.test",
		VulnerabilityReportingProcess:   "docs/security-reporting.md",
		SecureByDefaultEvidence:         []string{"docs/secure-defaults.md"},
		ScannerCoverage:                 []adapters.CRAScannerCoverage{{Name: "osv-scanner", Kind: "dependency"}, {Name: "gitleaks", Kind: "secret"}},
		SupportPeriod:                   "security updates through 2029",
		SupportUntil:                    &supportUntil,
		ConformityDocumentationPaths:    []string{"docs/cra-technical-file.md"},
		AutomaticSecurityUpdateEvidence: []string{"docs/update-policy.md"},
	})

	if report.AdapterName != "cra" || report.Regulation != "Regulation (EU) 2024/2847" {
		t.Fatalf("unexpected report identity: %#v", report)
	}
	for _, id := range []string{"cra-sbom", "cra-vulnerability-handling", "cra-secure-by-default", "cra-scanner-coverage", "cra-support-period", "cra-deadlines"} {
		check := craCheck(t, report, id)
		if check.Status != adapters.CRAEvidencePresent {
			t.Fatalf("%s status = %q, want present; missing %v", id, check.Status, check.MissingEvidence)
		}
		if check.SourceURL == "" {
			t.Fatalf("%s missing source URL", id)
		}
	}

	reporting := craCheck(t, report, "cra-vulnerability-handling")
	if reporting.DueDate != "2026-09-11" || reporting.DeadlineStatus != adapters.CRADeadlineUpcoming || reporting.DaysUntilDue != 120 {
		t.Fatalf("reporting deadline = (%q, %q, %d), want 2026-09-11 upcoming 120", reporting.DueDate, reporting.DeadlineStatus, reporting.DaysUntilDue)
	}
	full := craCheck(t, report, "cra-secure-by-default")
	if full.DueDate != "2027-12-11" || full.DeadlineStatus != adapters.CRADeadlineFuture {
		t.Fatalf("full deadline = (%q, %q), want 2027-12-11 future", full.DueDate, full.DeadlineStatus)
	}
}

func TestCRAAdapterReportsMissingAndPartialEvidence(t *testing.T) {
	t.Parallel()

	report := adapters.NewCRAAdapter().Evaluate(adapters.CRAEvidenceInput{
		AsOf:                          time.Date(2027, 12, 12, 0, 0, 0, 0, time.UTC),
		VulnerabilityReportingContact: "security@example.test",
	})

	if got := craCheck(t, report, "cra-sbom"); got.Status != adapters.CRAEvidenceMissing || !contains(got.MissingEvidence, "sbom_paths") {
		t.Fatalf("SBOM check = %#v, want missing sbom_paths", got)
	}
	if got := craCheck(t, report, "cra-vulnerability-handling"); got.Status != adapters.CRAEvidencePartial {
		t.Fatalf("vulnerability handling status = %q, want partial: %#v", got.Status, got)
	}
	if got := craCheck(t, report, "cra-secure-by-default"); got.DeadlineStatus != adapters.CRADeadlineActive {
		t.Fatalf("secure-by-default deadline = %q, want active", got.DeadlineStatus)
	}
}

func TestCRAAdapterCanUseCustomChecklistWithoutNetworkConfiguration(t *testing.T) {
	t.Parallel()

	adapter := adapters.NewCRAAdapter(adapters.WithCRARequirements([]adapters.CRARequirement{{
		ID:             "custom-evidence",
		Title:          "Custom evidence",
		Category:       adapters.CRACategoryScannerCoverage,
		Article:        "internal policy",
		DueDate:        "2026-01-01",
		SourceURL:      "https://example.test/policy",
		RequiredFields: []string{"scanner_coverage"},
	}}))

	report := adapter.Evaluate(adapters.CRAEvidenceInput{
		AsOf:            time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		ScannerCoverage: []adapters.CRAScannerCoverage{{Name: "semgrep", Kind: "sast"}},
	})
	if len(report.Checks) != 1 {
		t.Fatalf("checks = %d, want 1", len(report.Checks))
	}
	check := report.Checks[0]
	if check.ID != "custom-evidence" || check.Status != adapters.CRAEvidencePresent || check.DeadlineStatus != adapters.CRADeadlineActive {
		t.Fatalf("custom check = %#v", check)
	}
}

func craCheck(t *testing.T, report adapters.CRAReport, id string) adapters.CRACheck {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("check %q not found in %#v", id, report.Checks)
	return adapters.CRACheck{}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
