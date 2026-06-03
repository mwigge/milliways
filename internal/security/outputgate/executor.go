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

package outputgate

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/security"
	"github.com/mwigge/milliways/internal/security/adapters"
)

// ScannerAdapter is the scanner behavior required by the output gate executor.
type ScannerAdapter interface {
	Name() string
	Installed() bool
	Version(context.Context) (string, error)
	Scan(context.Context, string, []string) (security.ScanResult, error)
	RenderFinding(security.Finding) string
}

// Scanner pairs an adapter with the scan family it satisfies.
type Scanner struct {
	Kind    security.ScanKind
	Adapter ScannerAdapter
}

// ExecutionResult is the structured outcome of executing an output gate plan.
type ExecutionResult struct {
	Results  []security.ScanResult
	Warnings []security.Warning
}

// DefaultScanners returns the real local scanner adapters known to Secure
// MilliWays. Callers can pass their own scanner set in tests or constrained
// environments.
func DefaultScanners() []Scanner {
	return []Scanner{
		{Kind: security.ScanSecret, Adapter: adapters.NewGitleaks()},
		{Kind: security.ScanSAST, Adapter: adapters.NewSemgrep()},
		{Kind: security.ScanDependency, Adapter: adapters.NewGovulncheck()},
		{Kind: security.ScanDependency, Adapter: adapters.NewOSVScanner()},
	}
}

// ExecutePlan runs installed adapters matching the requested scan families. A
// missing or failing scanner is recorded as a warning; other adapters continue.
func ExecutePlan(ctx context.Context, workspace string, plan Plan, scanners []Scanner) ExecutionResult {
	workspace = strings.TrimSpace(workspace)
	now := time.Now().UTC()
	var tasks []scanTask
	var warnings []security.Warning

	for reqIndex, req := range plan.Requests {
		matches := scannersForKind(scanners, req.Kind)
		if len(matches) == 0 {
			warnings = append(warnings, scannerWarning(workspace, req.Kind, "", fmt.Sprintf("%s scan skipped: no %s scanner adapter configured", req.Kind, req.Kind), now))
			continue
		}
		for scannerIndex, scanner := range matches {
			if scanner.Adapter == nil {
				warnings = append(warnings, scannerWarning(workspace, req.Kind, "", fmt.Sprintf("%s scan skipped: scanner adapter is nil", req.Kind), now))
				continue
			}
			if !scanner.Adapter.Installed() {
				warnings = append(warnings, scannerWarning(workspace, req.Kind, scanner.Adapter.Name(), fmt.Sprintf("%s scan skipped: %s is not installed", req.Kind, scanner.Adapter.Name()), now))
				continue
			}
			targets := targetsForScanner(req.Kind, scanner.Adapter.Name(), req.Files)
			if len(req.Files) > 0 && len(targets) == 0 {
				continue
			}
			tasks = append(tasks, scanTask{
				requestIndex: reqIndex,
				scannerIndex: scannerIndex,
				kind:         req.Kind,
				workspace:    workspace,
				targets:      targets,
				adapter:      scanner.Adapter,
			})
		}
	}

	results := runScanTasks(ctx, tasks, now)
	for _, result := range results {
		if result.Error != "" {
			warnings = append(warnings, scannerWarning(workspace, result.Kind, result.ToolName, fmt.Sprintf("%s scan failed: %s: %s", result.Kind, result.ToolName, result.Error), now))
		}
	}
	sortWarnings(warnings)
	return ExecutionResult{Results: results, Warnings: warnings}
}

type scanTask struct {
	requestIndex int
	scannerIndex int
	kind         security.ScanKind
	workspace    string
	targets      []string
	adapter      ScannerAdapter
}

type scanTaskResult struct {
	task   scanTask
	result security.ScanResult
}

func runScanTasks(ctx context.Context, tasks []scanTask, scannedAt time.Time) []security.ScanResult {
	out := make(chan scanTaskResult, len(tasks))
	var wg sync.WaitGroup
	for _, task := range tasks {
		task := task
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := task.adapter.Scan(ctx, task.workspace, task.targets)
			if err != nil {
				result = security.ScanResult{Error: err.Error()}
			}
			fillScanResult(&result, task, scannedAt)
			out <- scanTaskResult{task: task, result: result}
		}()
	}
	wg.Wait()
	close(out)

	taskResults := make([]scanTaskResult, 0, len(tasks))
	for result := range out {
		taskResults = append(taskResults, result)
	}
	sort.SliceStable(taskResults, func(i, j int) bool {
		if taskResults[i].task.requestIndex != taskResults[j].task.requestIndex {
			return taskResults[i].task.requestIndex < taskResults[j].task.requestIndex
		}
		return taskResults[i].task.scannerIndex < taskResults[j].task.scannerIndex
	})
	results := make([]security.ScanResult, 0, len(taskResults))
	for _, taskResult := range taskResults {
		results = append(results, taskResult.result)
	}
	return results
}

func fillScanResult(result *security.ScanResult, task scanTask, scannedAt time.Time) {
	if result.ScannedAt.IsZero() {
		result.ScannedAt = scannedAt
	}
	if result.Kind == "" {
		result.Kind = task.kind
	}
	if result.Workspace == "" {
		result.Workspace = task.workspace
	}
	if result.ToolName == "" {
		result.ToolName = task.adapter.Name()
	}
	if len(result.LockFiles) == 0 {
		result.LockFiles = append([]string(nil), task.targets...)
	}
	for i := range result.Findings {
		if result.Findings[i].Category == "" {
			result.Findings[i].Category = categoryForScanKind(task.kind)
		}
		if result.Findings[i].WorkspacePath == "" {
			result.Findings[i].WorkspacePath = task.workspace
		}
		if result.Findings[i].ToolName == "" {
			result.Findings[i].ToolName = task.adapter.Name()
		}
		if result.Findings[i].Status == "" {
			result.Findings[i].Status = security.FindingActive
		}
	}
}

func scannersForKind(scanners []Scanner, kind security.ScanKind) []Scanner {
	matches := make([]Scanner, 0)
	for _, scanner := range scanners {
		if scanner.Kind == kind {
			matches = append(matches, scanner)
		}
	}
	return matches
}

func targetsForScanner(kind security.ScanKind, scannerName string, files []string) []string {
	if kind != security.ScanDependency {
		return append([]string(nil), files...)
	}
	switch strings.ToLower(strings.TrimSpace(scannerName)) {
	case "govulncheck":
		return filterFiles(files, isGoDependencyTarget)
	case "osv-scanner":
		return append([]string(nil), files...)
	default:
		return append([]string(nil), files...)
	}
}

func filterFiles(files []string, keep func(string) bool) []string {
	var out []string
	for _, file := range files {
		if keep(file) {
			out = append(out, file)
		}
	}
	return out
}

func isGoDependencyTarget(path string) bool {
	base := strings.ToLower(strings.TrimSpace(path))
	base = base[strings.LastIndex(base, "/")+1:]
	return base == "go.mod" || base == "go.sum"
}

func scannerWarning(workspace string, kind security.ScanKind, source, message string, at time.Time) security.Warning {
	return security.Warning{
		Workspace: workspace,
		Category:  categoryForScanKind(kind),
		Severity:  "WARNING",
		Source:    source,
		Message:   message,
		Status:    security.FindingActive,
		FirstSeen: at,
		LastSeen:  at,
	}
}

func categoryForScanKind(kind security.ScanKind) security.FindingCategory {
	switch kind {
	case security.ScanSecret:
		return security.FindingSecret
	case security.ScanSAST:
		return security.FindingSAST
	case security.ScanDependency:
		return security.FindingDependency
	default:
		return security.FindingCommand
	}
}

func sortWarnings(warnings []security.Warning) {
	sort.SliceStable(warnings, func(i, j int) bool {
		if warnings[i].Category != warnings[j].Category {
			return warnings[i].Category < warnings[j].Category
		}
		if warnings[i].Source != warnings[j].Source {
			return warnings[i].Source < warnings[j].Source
		}
		return warnings[i].Message < warnings[j].Message
	})
}
