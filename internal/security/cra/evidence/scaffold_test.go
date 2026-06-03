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

package evidence_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/security/cra/evidence"
)

func TestScaffoldCreatesMissingEvidenceFiles(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	result, err := evidence.Scaffold(evidence.Options{Workspace: workspace})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}

	want := []string{
		"SECURITY.md",
		"SUPPORT.md",
		"docs/update-policy.md",
		"docs/cra-technical-file.md",
	}
	for _, rel := range want {
		path := filepath.Join(workspace, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s to be written: %v", rel, err)
		}
		if !strings.Contains(string(data), "#") {
			t.Fatalf("expected %s to contain markdown template, got %q", rel, string(data))
		}
	}
	if result.Created != 4 || result.Existing != 0 || result.Overwritten != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestScaffoldDryRunDoesNotWriteFiles(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	result, err := evidence.Scaffold(evidence.Options{Workspace: workspace, DryRun: true})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if result.Created != 4 {
		t.Fatalf("dry-run created count = %d, want 4", result.Created)
	}
	if _, err := os.Stat(filepath.Join(workspace, "SECURITY.md")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not write SECURITY.md, stat err=%v", err)
	}
}

func TestScaffoldSkipsExistingFilesUnlessForced(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "SECURITY.md")
	if err := os.WriteFile(path, []byte("custom policy\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := evidence.Scaffold(evidence.Options{Workspace: workspace})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if result.Created != 3 || result.Existing != 1 {
		t.Fatalf("unexpected non-force result: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "custom policy\n" {
		t.Fatalf("existing SECURITY.md was overwritten without force:\n%s", string(data))
	}

	result, err = evidence.Scaffold(evidence.Options{Workspace: workspace, Force: true})
	if err != nil {
		t.Fatalf("Scaffold force: %v", err)
	}
	if result.Overwritten != 4 {
		t.Fatalf("force overwritten count = %d, want 4; result=%#v", result.Overwritten, result)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile forced: %v", err)
	}
	if !strings.Contains(string(data), "Reporting a Vulnerability") {
		t.Fatalf("forced SECURITY.md missing scaffold content:\n%s", string(data))
	}
}
