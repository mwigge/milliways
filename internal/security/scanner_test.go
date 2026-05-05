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
