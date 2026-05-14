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

package outputgate_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/security"
	"github.com/mwigge/milliways/internal/security/outputgate"
)

func TestExecutePlanRunsInstalledAdaptersForRequestedKinds(t *testing.T) {
	t.Parallel()

	secret := &fakeScanAdapter{
		name:      "secret-tool",
		installed: true,
		result: security.ScanResult{
			Kind:     security.ScanSecret,
			ToolName: "secret-tool",
			Findings: []security.Finding{{
				ID:       "secret-1",
				Category: security.FindingSecret,
				Severity: "HIGH",
			}},
		},
	}
	sast := &fakeScanAdapter{
		name:      "sast-tool",
		installed: true,
		result: security.ScanResult{
			Kind:     security.ScanSAST,
			ToolName: "sast-tool",
		},
	}
	dependency := &fakeScanAdapter{name: "dependency-tool", installed: true}
	workspace := "/work/repo"
	plan := outputgate.Plan{Requests: []outputgate.ScanRequest{
		{Kind: security.ScanSecret, Files: []string{"app.go"}},
		{Kind: security.ScanSAST, Files: []string{"app.go"}},
	}}

	result := outputgate.ExecutePlan(context.Background(), workspace, plan, []outputgate.Scanner{
		{Kind: security.ScanSecret, Adapter: secret},
		{Kind: security.ScanSAST, Adapter: sast},
		{Kind: security.ScanDependency, Adapter: dependency},
	})

	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", result.Warnings)
	}
	if len(result.Results) != 2 {
		t.Fatalf("results = %d, want 2: %#v", len(result.Results), result.Results)
	}
	if !reflect.DeepEqual(secret.calls, []scanCall{{workspace: workspace, targets: []string{"app.go"}}}) {
		t.Fatalf("secret calls = %#v", secret.calls)
	}
	if !reflect.DeepEqual(sast.calls, []scanCall{{workspace: workspace, targets: []string{"app.go"}}}) {
		t.Fatalf("sast calls = %#v", sast.calls)
	}
	if len(dependency.calls) != 0 {
		t.Fatalf("dependency calls = %#v, want none", dependency.calls)
	}
}

func TestExecutePlanWarnsForMissingAdapters(t *testing.T) {
	t.Parallel()

	result := outputgate.ExecutePlan(context.Background(), "/work/repo", outputgate.Plan{Requests: []outputgate.ScanRequest{
		{Kind: security.ScanSecret, Files: []string{".env"}},
		{Kind: security.ScanDependency, Files: []string{"go.sum"}},
	}}, []outputgate.Scanner{
		{Kind: security.ScanSecret, Adapter: &fakeScanAdapter{name: "gitleaks", installed: false}},
	})

	if len(result.Results) != 0 {
		t.Fatalf("results = %#v, want none", result.Results)
	}
	got := warningMessages(result.Warnings)
	want := []string{
		"dependency scan skipped: no dependency scanner adapter configured",
		"secret scan skipped: gitleaks is not installed",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %#v, want %#v", got, want)
	}
}

func TestExecutePlanCapturesScannerErrors(t *testing.T) {
	t.Parallel()

	result := outputgate.ExecutePlan(context.Background(), "/work/repo", outputgate.Plan{Requests: []outputgate.ScanRequest{
		{Kind: security.ScanSAST, Files: []string{"app.go"}},
	}}, []outputgate.Scanner{
		{Kind: security.ScanSAST, Adapter: &fakeScanAdapter{name: "semgrep", installed: true, err: errors.New("scan failed")}},
	})

	if len(result.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(result.Results))
	}
	if result.Results[0].Error != "scan failed" {
		t.Fatalf("result error = %q, want scan failed", result.Results[0].Error)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Message != "sast scan failed: semgrep: scan failed" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestExecutePlanRunsAdaptersConcurrently(t *testing.T) {
	t.Parallel()

	started := make(chan string, 4)
	release := make(chan struct{})
	scanners := []outputgate.Scanner{
		{Kind: security.ScanSecret, Adapter: &blockingScanAdapter{name: "gitleaks", started: started, release: release}},
		{Kind: security.ScanSAST, Adapter: &blockingScanAdapter{name: "semgrep", started: started, release: release}},
		{Kind: security.ScanDependency, Adapter: &blockingScanAdapter{name: "govulncheck", started: started, release: release}},
		{Kind: security.ScanDependency, Adapter: &blockingScanAdapter{name: "osv-scanner", started: started, release: release}},
	}
	plan := outputgate.Plan{Requests: []outputgate.ScanRequest{
		{Kind: security.ScanSecret, Files: []string{".env"}},
		{Kind: security.ScanSAST, Files: []string{"app.go"}},
		{Kind: security.ScanDependency, Files: []string{"go.sum"}},
	}}

	done := make(chan outputgate.ExecutionResult, 1)
	go func() {
		done <- outputgate.ExecutePlan(context.Background(), "/work/repo", plan, scanners)
	}()

	var names []string
	for len(names) < 4 {
		select {
		case name := <-started:
			names = append(names, name)
		case <-time.After(time.Second):
			t.Fatalf("only %d adapters started before timeout: %v", len(names), names)
		}
	}
	close(release)
	result := <-done
	if len(result.Results) != 4 {
		t.Fatalf("results = %d, want 4", len(result.Results))
	}
	sort.Strings(names)
	want := []string{"gitleaks", "govulncheck", "osv-scanner", "semgrep"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("started = %#v, want %#v", names, want)
	}
}

type fakeScanAdapter struct {
	name      string
	installed bool
	result    security.ScanResult
	err       error
	calls     []scanCall
}

type scanCall struct {
	workspace string
	targets   []string
}

func (a *fakeScanAdapter) Name() string {
	return a.name
}

func (a *fakeScanAdapter) Installed() bool {
	return a.installed
}

func (a *fakeScanAdapter) Version(context.Context) (string, error) {
	return "", nil
}

func (a *fakeScanAdapter) Scan(_ context.Context, workspace string, targets []string) (security.ScanResult, error) {
	a.calls = append(a.calls, scanCall{workspace: workspace, targets: append([]string(nil), targets...)})
	if a.err != nil {
		return security.ScanResult{}, a.err
	}
	result := a.result
	if result.ScannedAt.IsZero() {
		result.ScannedAt = time.Unix(1, 0).UTC()
	}
	return result, nil
}

func (a *fakeScanAdapter) RenderFinding(security.Finding) string {
	return ""
}

type blockingScanAdapter struct {
	name    string
	started chan<- string
	release <-chan struct{}
}

func (a *blockingScanAdapter) Name() string {
	return a.name
}

func (a *blockingScanAdapter) Installed() bool {
	return true
}

func (a *blockingScanAdapter) Version(context.Context) (string, error) {
	return "", nil
}

func (a *blockingScanAdapter) Scan(context.Context, string, []string) (security.ScanResult, error) {
	a.started <- a.name
	<-a.release
	return security.ScanResult{ToolName: a.name}, nil
}

func (a *blockingScanAdapter) RenderFinding(security.Finding) string {
	return ""
}

func warningMessages(warnings []security.Warning) []string {
	messages := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		messages = append(messages, warning.Message)
	}
	return messages
}
