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
	"time"
)

const (
	craRegulation     = "Regulation (EU) 2024/2847"
	craEURlexSource   = "https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng"
	craSummarySource  = "https://digital-strategy.ec.europa.eu/en/policies/cra-summary"
	craReportSource   = "https://digital-strategy.ec.europa.eu/en/policies/cra-reporting"
	craReportingDue   = "2026-09-11"
	craFullApplyDue   = "2027-12-11"
	craUpcomingWindow = 180
)

// CRA evidence field names used by requirements.
const (
	CRAEvidenceFieldSBOM                    = "sbom_paths"
	CRAEvidenceFieldVulnerabilityPolicy     = "vulnerability_handling_policy"
	CRAEvidenceFieldReportingContact        = "vulnerability_reporting_contact"
	CRAEvidenceFieldReportingProcess        = "vulnerability_reporting_process"
	CRAEvidenceFieldSecureByDefault         = "secure_by_default_evidence"
	CRAEvidenceFieldScannerCoverage         = "scanner_coverage"
	CRAEvidenceFieldSupportPeriod           = "support_period"
	CRAEvidenceFieldSupportUntil            = "support_until"
	CRAEvidenceFieldConformityDocumentation = "conformity_documentation_paths"
	CRAEvidenceFieldAutomaticUpdates        = "automatic_security_update_evidence"
)

// CRAEvidenceStatus is the local evidence completeness state for a requirement.
type CRAEvidenceStatus string

const (
	CRAEvidencePresent CRAEvidenceStatus = "present"
	CRAEvidencePartial CRAEvidenceStatus = "partial"
	CRAEvidenceMissing CRAEvidenceStatus = "missing"
)

// CRADeadlineStatus describes whether a CRA applicability date is active.
type CRADeadlineStatus string

const (
	CRADeadlineActive   CRADeadlineStatus = "active"
	CRADeadlineUpcoming CRADeadlineStatus = "upcoming"
	CRADeadlineFuture   CRADeadlineStatus = "future"
	CRADeadlineUnknown  CRADeadlineStatus = "unknown"
)

// CRACategory groups CRA evidence checks.
type CRACategory string

const (
	CRACategorySBOM                    CRACategory = "sbom"
	CRACategoryVulnerabilityHandling   CRACategory = "vulnerability-handling"
	CRACategorySecureByDefault         CRACategory = "secure-by-default"
	CRACategoryScannerCoverage         CRACategory = "scanner-coverage"
	CRACategorySupportPeriod           CRACategory = "support-period"
	CRACategoryConformityDocumentation CRACategory = "conformity-documentation"
	CRACategoryDeadlines               CRACategory = "deadlines"
)

// CRARequirement defines one local CRA evidence check.
type CRARequirement struct {
	ID             string
	Title          string
	Category       CRACategory
	Article        string
	DueDate        string
	SourceURL      string
	RequiredFields []string
}

// CRAScannerCoverage records scanner evidence relevant to CRA vulnerability
// handling and secure-development posture.
type CRAScannerCoverage struct {
	Name string
	Kind string
}

// CRAEvidenceInput is the local evidence supplied by callers. The CRA adapter
// intentionally does not fetch remote compliance status.
type CRAEvidenceInput struct {
	ProductName                     string
	AsOf                            time.Time
	SBOMPaths                       []string
	VulnerabilityHandlingPolicy     string
	VulnerabilityReportingContact   string
	VulnerabilityReportingProcess   string
	SecureByDefaultEvidence         []string
	ScannerCoverage                 []CRAScannerCoverage
	SupportPeriod                   string
	SupportUntil                    *time.Time
	ConformityDocumentationPaths    []string
	AutomaticSecurityUpdateEvidence []string
}

// CRAReport is a local CRA evidence posture report.
type CRAReport struct {
	AdapterName string
	Regulation  string
	ProductName string
	AsOf        time.Time
	Checks      []CRACheck
}

// CRACheck is the evaluated state of one CRA requirement.
type CRACheck struct {
	ID              string
	Title           string
	Category        CRACategory
	Article         string
	DueDate         string
	DeadlineStatus  CRADeadlineStatus
	DaysUntilDue    int
	SourceURL       string
	Status          CRAEvidenceStatus
	PresentEvidence []string
	MissingEvidence []string
}

// CRAOption customizes the CRA evidence adapter.
type CRAOption func(*CRAAdapter)

// WithCRARequirements replaces the default CRA checklist. This keeps the
// foundation usable while official guidance and internal policies evolve.
func WithCRARequirements(requirements []CRARequirement) CRAOption {
	return func(a *CRAAdapter) {
		a.requirements = append([]CRARequirement(nil), requirements...)
	}
}

// CRAAdapter evaluates local CRA policy evidence.
type CRAAdapter struct {
	requirements []CRARequirement
}

// NewCRAAdapter returns a local CRA evidence checklist adapter.
func NewCRAAdapter(opts ...CRAOption) *CRAAdapter {
	a := &CRAAdapter{requirements: defaultCRARequirements()}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func (a *CRAAdapter) Name() string {
	return "cra"
}

func (a *CRAAdapter) Evaluate(input CRAEvidenceInput) CRAReport {
	asOf := input.AsOf
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	report := CRAReport{
		AdapterName: a.Name(),
		Regulation:  craRegulation,
		ProductName: input.ProductName,
		AsOf:        asOf,
	}

	present := input.presentEvidence()
	for _, requirement := range a.requirements {
		check := CRACheck{
			ID:              requirement.ID,
			Title:           requirement.Title,
			Category:        requirement.Category,
			Article:         requirement.Article,
			DueDate:         requirement.DueDate,
			SourceURL:       requirement.SourceURL,
			DeadlineStatus:  deadlineStatus(asOf, requirement.DueDate),
			DaysUntilDue:    daysUntil(asOf, requirement.DueDate),
			PresentEvidence: make([]string, 0, len(requirement.RequiredFields)),
			MissingEvidence: make([]string, 0, len(requirement.RequiredFields)),
		}
		for _, field := range requirement.RequiredFields {
			if present[field] {
				check.PresentEvidence = append(check.PresentEvidence, field)
				continue
			}
			check.MissingEvidence = append(check.MissingEvidence, field)
		}
		check.Status = evidenceStatus(len(check.PresentEvidence), len(check.MissingEvidence))
		report.Checks = append(report.Checks, check)
	}
	return report
}

func defaultCRARequirements() []CRARequirement {
	return []CRARequirement{
		{
			ID:             "cra-sbom",
			Title:          "SBOM evidence",
			Category:       CRACategorySBOM,
			Article:        "Annex I Part II",
			DueDate:        craFullApplyDue,
			SourceURL:      craSummarySource,
			RequiredFields: []string{CRAEvidenceFieldSBOM},
		},
		{
			ID:        "cra-vulnerability-handling",
			Title:     "Vulnerability handling and reporting evidence",
			Category:  CRACategoryVulnerabilityHandling,
			Article:   "Article 14; Annex I Part II",
			DueDate:   craReportingDue,
			SourceURL: craReportSource,
			RequiredFields: []string{
				CRAEvidenceFieldVulnerabilityPolicy,
				CRAEvidenceFieldReportingContact,
				CRAEvidenceFieldReportingProcess,
			},
		},
		{
			ID:        "cra-secure-by-default",
			Title:     "Secure-by-default evidence",
			Category:  CRACategorySecureByDefault,
			Article:   "Annex I Part I",
			DueDate:   craFullApplyDue,
			SourceURL: craEURlexSource,
			RequiredFields: []string{
				CRAEvidenceFieldSecureByDefault,
				CRAEvidenceFieldAutomaticUpdates,
			},
		},
		{
			ID:             "cra-scanner-coverage",
			Title:          "Scanner coverage evidence",
			Category:       CRACategoryScannerCoverage,
			Article:        "Annex I Part II",
			DueDate:        craFullApplyDue,
			SourceURL:      craSummarySource,
			RequiredFields: []string{CRAEvidenceFieldScannerCoverage},
		},
		{
			ID:        "cra-support-period",
			Title:     "Support-period evidence",
			Category:  CRACategorySupportPeriod,
			Article:   "Annex I Part II",
			DueDate:   craFullApplyDue,
			SourceURL: craSummarySource,
			RequiredFields: []string{
				CRAEvidenceFieldSupportPeriod,
				CRAEvidenceFieldSupportUntil,
			},
		},
		{
			ID:             "cra-conformity-documentation",
			Title:          "Conformity documentation evidence",
			Category:       CRACategoryConformityDocumentation,
			Article:        "Annex VII; Annex V",
			DueDate:        craFullApplyDue,
			SourceURL:      craEURlexSource,
			RequiredFields: []string{CRAEvidenceFieldConformityDocumentation},
		},
		{
			ID:        "cra-deadlines",
			Title:     "CRA applicability dates",
			Category:  CRACategoryDeadlines,
			Article:   "Article 71",
			DueDate:   craFullApplyDue,
			SourceURL: craEURlexSource,
		},
	}
}

func (input CRAEvidenceInput) presentEvidence() map[string]bool {
	present := map[string]bool{
		CRAEvidenceFieldSBOM:                    len(input.SBOMPaths) > 0,
		CRAEvidenceFieldVulnerabilityPolicy:     input.VulnerabilityHandlingPolicy != "",
		CRAEvidenceFieldReportingContact:        input.VulnerabilityReportingContact != "",
		CRAEvidenceFieldReportingProcess:        input.VulnerabilityReportingProcess != "",
		CRAEvidenceFieldSecureByDefault:         len(input.SecureByDefaultEvidence) > 0,
		CRAEvidenceFieldScannerCoverage:         len(input.ScannerCoverage) > 0,
		CRAEvidenceFieldSupportPeriod:           input.SupportPeriod != "",
		CRAEvidenceFieldSupportUntil:            input.SupportUntil != nil,
		CRAEvidenceFieldConformityDocumentation: len(input.ConformityDocumentationPaths) > 0,
		CRAEvidenceFieldAutomaticUpdates:        len(input.AutomaticSecurityUpdateEvidence) > 0,
	}
	for key := range present {
		if present[key] {
			continue
		}
		delete(present, key)
	}
	return present
}

func evidenceStatus(present, missing int) CRAEvidenceStatus {
	switch {
	case missing == 0:
		return CRAEvidencePresent
	case present > 0:
		return CRAEvidencePartial
	default:
		return CRAEvidenceMissing
	}
}

func deadlineStatus(asOf time.Time, dueDate string) CRADeadlineStatus {
	days := daysUntil(asOf, dueDate)
	switch {
	case days < 0:
		return CRADeadlineActive
	case days <= craUpcomingWindow:
		return CRADeadlineUpcoming
	case days >= 0:
		return CRADeadlineFuture
	default:
		return CRADeadlineUnknown
	}
}

func daysUntil(asOf time.Time, dueDate string) int {
	if dueDate == "" {
		return -1
	}
	due, err := time.Parse("2006-01-02", dueDate)
	if err != nil {
		return -1
	}
	asOf = time.Date(asOf.UTC().Year(), asOf.UTC().Month(), asOf.UTC().Day(), 0, 0, 0, 0, time.UTC)
	return int(due.Sub(asOf).Hours() / 24)
}
