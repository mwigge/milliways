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
	"reflect"
	"testing"

	"github.com/mwigge/milliways/internal/security"
	"github.com/mwigge/milliways/internal/security/outputgate"
)

func TestPlanScansRequestsSecretSASTAndDependencyScans(t *testing.T) {
	t.Parallel()

	plan := outputgate.PlanScans([]outputgate.FileChange{
		{Path: "cmd/app/main.go", Status: outputgate.StatusModified, Source: outputgate.SourceGenerated},
		{Path: "package-lock.json", Status: outputgate.StatusModified, Source: outputgate.SourceGenerated},
		{Path: ".env.local", Status: outputgate.StatusAdded, Source: outputgate.SourceStaged},
		{Path: "README.md", Status: outputgate.StatusModified, Source: outputgate.SourceStaged},
	})

	want := map[security.ScanKind][]string{
		security.ScanSecret:     {".env.local", "cmd/app/main.go", "package-lock.json"},
		security.ScanSAST:       {"cmd/app/main.go"},
		security.ScanDependency: {"package-lock.json"},
	}
	assertPlanFiles(t, plan, want)
}

func TestPlanScansIgnoresDeletedFilesAndDeduplicates(t *testing.T) {
	t.Parallel()

	plan := outputgate.PlanScans([]outputgate.FileChange{
		{Path: "go.mod", Status: outputgate.StatusDeleted, Source: outputgate.SourceGenerated},
		{Path: "./go.sum", Status: outputgate.StatusModified, Source: outputgate.SourceGenerated},
		{Path: "go.sum", Status: outputgate.StatusModified, Source: outputgate.SourceStaged},
	})

	want := map[security.ScanKind][]string{
		security.ScanSecret:     {"go.sum"},
		security.ScanDependency: {"go.sum"},
	}
	assertPlanFiles(t, plan, want)
}

func TestPlanScansIncludesWorkflowAndDockerfileForSAST(t *testing.T) {
	t.Parallel()

	plan := outputgate.PlanScans([]outputgate.FileChange{
		{Path: ".github/workflows/release.yml", Status: outputgate.StatusModified, Source: outputgate.SourceStaged},
		{Path: "build/Dockerfile.alpine", Status: outputgate.StatusAdded, Source: outputgate.SourceGenerated},
	})

	want := map[security.ScanKind][]string{
		security.ScanSecret: {".github/workflows/release.yml"},
		security.ScanSAST:   {".github/workflows/release.yml", "build/Dockerfile.alpine"},
	}
	assertPlanFiles(t, plan, want)
}

func TestPlanScansEmptyForDocsOnly(t *testing.T) {
	t.Parallel()

	plan := outputgate.PlanScans([]outputgate.FileChange{
		{Path: "docs/guide.md", Status: outputgate.StatusModified, Source: outputgate.SourceGenerated},
	})
	if len(plan.Requests) != 0 {
		t.Fatalf("PlanScans docs-only requests = %+v, want none", plan.Requests)
	}
}

func assertPlanFiles(t *testing.T, plan outputgate.Plan, want map[security.ScanKind][]string) {
	t.Helper()

	got := map[security.ScanKind][]string{}
	for _, req := range plan.Requests {
		got[req.Kind] = req.Files
		if req.Reason == "" {
			t.Fatalf("request for %s has empty reason", req.Kind)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PlanScans files = %#v, want %#v", got, want)
	}
}
