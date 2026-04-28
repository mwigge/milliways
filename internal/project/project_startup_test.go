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

package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectStartupProjectWithoutPalaceGracefullyDegrades(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}

	ctx, err := DetectStartupProject(repoRoot)
	if err != nil {
		t.Fatalf("DetectStartupProject: %v", err)
	}
	if ctx == nil {
		t.Fatal("expected project context")
	}
	if ctx.PalaceExists {
		t.Fatal("expected palace to be absent")
	}
	if ctx.PalacePath != nil {
		t.Fatalf("expected nil palace path, got %v", *ctx.PalacePath)
	}
	if !ctx.CodeGraphIndexing {
		t.Fatal("expected codegraph indexing to be true when .codegraph is absent")
	}
}

func TestDetectStartupProjectOutsideRepositoryReturnsNil(t *testing.T) {
	t.Parallel()

	ctx, err := DetectStartupProject(t.TempDir())
	if err != nil {
		t.Fatalf("DetectStartupProject: %v", err)
	}
	if ctx != nil {
		t.Fatalf("expected nil project context, got %#v", ctx)
	}
}

func TestDetectStartupProjectWithPalacePresentCapturesPalaceInfo(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}
	palacePath := filepath.Join(repoRoot, ".mempalace")
	if err := os.Mkdir(palacePath, 0o755); err != nil {
		t.Fatalf("create .mempalace dir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(repoRoot, ".codegraph"), 0o755); err != nil {
		t.Fatalf("create .codegraph dir: %v", err)
	}

	ctx, err := DetectStartupProject(repoRoot)
	if err != nil {
		t.Fatalf("DetectStartupProject: %v", err)
	}
	if ctx == nil {
		t.Fatal("expected project context")
	}
	if !ctx.PalaceExists {
		t.Fatal("expected palace to exist")
	}
	if ctx.PalacePath == nil || *ctx.PalacePath != palacePath {
		t.Fatalf("palace path = %v, want %q", ctx.PalacePath, palacePath)
	}
	if !ctx.CodeGraphExists {
		t.Fatal("expected codegraph to exist")
	}
	if ctx.CodeGraphIndexing {
		t.Fatal("expected codegraph indexing to be false when .codegraph exists")
	}
}
