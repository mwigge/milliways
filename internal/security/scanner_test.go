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

package security_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mwigge/milliways/internal/security"
)

func TestDiscoverLockfiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Supported files.
	supported := []string{"go.sum", "Cargo.lock", "pnpm-lock.yaml", "package-lock.json"}
	for _, name := range supported {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Unsupported files that should be ignored.
	unsupported := []string{"go.mod", "README.md", "main.go", ".gitignore"}
	for _, name := range unsupported {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := security.DiscoverLockfiles(dir)
	if len(got) != len(supported) {
		t.Fatalf("DiscoverLockfiles returned %d paths, want %d; got: %v", len(got), len(supported), got)
	}
	gotSet := make(map[string]struct{}, len(got))
	for _, p := range got {
		gotSet[filepath.Base(p)] = struct{}{}
	}
	for _, name := range supported {
		if _, ok := gotSet[name]; !ok {
			t.Errorf("expected %q in result but not found", name)
		}
	}
}

func TestDiscoverLockfilesRecursesAndIgnoresHeavyDirs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.sum"))
	writeFile(t, filepath.Join(dir, "service", "pnpm-lock.yaml"))
	writeFile(t, filepath.Join(dir, "service", "vendor", "Cargo.lock"))
	writeFile(t, filepath.Join(dir, "node_modules", "package-lock.json"))
	writeFile(t, filepath.Join(dir, ".git", "package-lock.json"))

	got := security.DiscoverLockfiles(dir)
	gotSet := make(map[string]struct{}, len(got))
	for _, path := range got {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			t.Fatal(err)
		}
		gotSet[filepath.ToSlash(rel)] = struct{}{}
	}

	want := []string{"go.sum", "service/pnpm-lock.yaml"}
	for _, rel := range want {
		if _, ok := gotSet[rel]; !ok {
			t.Fatalf("DiscoverLockfiles missing %q in %v", rel, gotSet)
		}
	}
	for _, ignored := range []string{"service/vendor/Cargo.lock", "node_modules/package-lock.json", ".git/package-lock.json"} {
		if _, ok := gotSet[ignored]; ok {
			t.Fatalf("DiscoverLockfiles included ignored path %q in %v", ignored, gotSet)
		}
	}
}

func TestDiscoverLockfilesWithOptionsStopsAtMaxFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a", "go.sum"))
	writeFile(t, filepath.Join(dir, "b", "Cargo.lock"))
	writeFile(t, filepath.Join(dir, "c", "pnpm-lock.yaml"))

	got := security.DiscoverLockfilesWithOptions(dir, security.LockfileDiscoveryOptions{MaxFiles: 2})
	if len(got) != 2 {
		t.Fatalf("DiscoverLockfilesWithOptions returned %d paths, want 2; got %v", len(got), got)
	}
}

func TestDiscoverLockfilesWithOptionsStopsAtMaxDepth(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app", "go.sum"))
	writeFile(t, filepath.Join(dir, "app", "nested", "Cargo.lock"))

	got := security.DiscoverLockfilesWithOptions(dir, security.LockfileDiscoveryOptions{MaxDepth: 1, MaxFiles: -1})
	if len(got) != 1 || filepath.Base(got[0]) != "go.sum" {
		t.Fatalf("DiscoverLockfilesWithOptions returned %v, want only app/go.sum", got)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverLockfiles_Empty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	got := security.DiscoverLockfiles(dir)
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

func TestDiscoverLockfiles_NonexistentDir(t *testing.T) {
	t.Parallel()

	got := security.DiscoverLockfiles("/no/such/directory/xyz123")
	if len(got) != 0 {
		t.Fatalf("expected empty slice for nonexistent dir, got %v", got)
	}
}

func TestScan_NoLockfiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, err := security.Scan(ctx, nil)
	if !errors.Is(err, security.ErrNoLockfiles) {
		t.Fatalf("Scan(ctx, nil) error = %v, want ErrNoLockfiles", err)
	}
}

func TestScan_EmptyLockfiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, err := security.Scan(ctx, []string{})
	if !errors.Is(err, security.ErrNoLockfiles) {
		t.Fatalf("Scan(ctx, []) error = %v, want ErrNoLockfiles", err)
	}
}
