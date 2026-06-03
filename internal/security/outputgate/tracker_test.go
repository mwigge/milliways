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
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mwigge/milliways/internal/security"
	"github.com/mwigge/milliways/internal/security/outputgate"
)

func TestGeneratedFileChangesNormalizesDeduplicatesAndMarksMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, root, "cmd/app/main.go", "package main\n")

	changes, err := outputgate.GeneratedFileChanges(root, []string{
		"./cmd/app/main.go",
		filepath.Join(root, "cmd/app/main.go"),
		"missing.go",
		"",
	})
	if err != nil {
		t.Fatalf("GeneratedFileChanges returned error: %v", err)
	}

	want := []outputgate.FileChange{
		{Path: "cmd/app/main.go", Status: outputgate.StatusModified, Source: outputgate.SourceGenerated},
		{Path: "missing.go", Status: outputgate.StatusDeleted, Source: outputgate.SourceGenerated},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("GeneratedFileChanges = %#v, want %#v", changes, want)
	}
}

func TestGeneratedFileChangesRejectsPathsOutsideRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.go")
	if err := os.WriteFile(outside, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := outputgate.GeneratedFileChanges(root, []string{outside}); err == nil {
		t.Fatal("GeneratedFileChanges outside root error = nil, want error")
	}
}

func TestGeneratedFileChangesRejectsSymlinkEscapes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.go")
	if err := os.WriteFile(outside, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.go")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if _, err := outputgate.GeneratedFileChanges(root, []string{"linked.go"}); err == nil {
		t.Fatal("GeneratedFileChanges symlink escape error = nil, want error")
	}
}

func TestSnapshotDiffTracksAddedModifiedAndDeletedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, root, "cmd/app/main.go", "package main\n")
	writeTestFile(t, root, "go.sum", "module v1\n")
	writeTestFile(t, root, "README.md", "docs\n")

	before, err := outputgate.CaptureWorkspace(root)
	if err != nil {
		t.Fatalf("CaptureWorkspace before: %v", err)
	}

	writeTestFile(t, root, "cmd/app/main.go", "package main\nfunc run() {}\n")
	writeTestFile(t, root, ".env.local", "TOKEN=value\n")
	if err := os.Remove(filepath.Join(root, "go.sum")); err != nil {
		t.Fatal(err)
	}

	after, err := outputgate.CaptureWorkspace(root)
	if err != nil {
		t.Fatalf("CaptureWorkspace after: %v", err)
	}

	changes := outputgate.DiffSnapshots(before, after)
	wantChanges := []outputgate.FileChange{
		{Path: ".env.local", Status: outputgate.StatusAdded, Source: outputgate.SourceGenerated},
		{Path: "cmd/app/main.go", Status: outputgate.StatusModified, Source: outputgate.SourceGenerated},
		{Path: "go.sum", Status: outputgate.StatusDeleted, Source: outputgate.SourceGenerated},
	}
	if !reflect.DeepEqual(changes, wantChanges) {
		t.Fatalf("DiffSnapshots = %#v, want %#v", changes, wantChanges)
	}

	plan := outputgate.PlanSnapshotDiff(before, after)
	wantPlan := map[security.ScanKind][]string{
		security.ScanSecret: {".env.local", "cmd/app/main.go"},
		security.ScanSAST:   {"cmd/app/main.go"},
	}
	assertPlanFiles(t, plan, wantPlan)
}

func TestCaptureWorkspaceSkipsGitAndNodeModules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, root, ".git/config", "ignored\n")
	writeTestFile(t, root, "node_modules/pkg/index.js", "ignored\n")

	before, err := outputgate.CaptureWorkspace(root)
	if err != nil {
		t.Fatalf("CaptureWorkspace before: %v", err)
	}
	writeTestFile(t, root, ".git/hooks/post-commit", "ignored\n")
	writeTestFile(t, root, "node_modules/pkg/index.js", "ignored change\n")
	after, err := outputgate.CaptureWorkspace(root)
	if err != nil {
		t.Fatalf("CaptureWorkspace after: %v", err)
	}

	if changes := outputgate.DiffSnapshots(before, after); len(changes) != 0 {
		t.Fatalf("DiffSnapshots ignored dirs changes = %#v, want none", changes)
	}
}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
