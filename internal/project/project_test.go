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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultAccessRules(t *testing.T) {
	t.Parallel()

	rules := DefaultAccessRules()

	if rules.Read != "all" {
		t.Fatalf("expected read access all, got %q", rules.Read)
	}
	if rules.Write != "project" {
		t.Fatalf("expected write access project, got %q", rules.Write)
	}
}

func TestProjectContextJSONTags(t *testing.T) {
	t.Parallel()

	palacePath := "/tmp/repo/.mempalace"
	palaceDrawers := 3

	ctx := ProjectContext{
		RepoRoot:         "/tmp/repo",
		RepoName:         "repo",
		Branch:           "feature/test",
		Commit:           "abc1234",
		CodeGraphExists:  true,
		CodeGraphPath:    "/tmp/repo/.codegraph",
		CodeGraphSymbols: 42,
		PalacePath:       &palacePath,
		PalaceExists:     true,
		PalaceDrawers:    &palaceDrawers,
		AccessRules:      DefaultAccessRules(),
	}

	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("marshal project context: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal project context: %v", err)
	}

	for _, key := range []string{"repo_root", "repo_name", "branch", "commit", "codegraph_exists", "codegraph_path", "codegraph_symbols", "palace_path", "palace_exists", "palace_drawers", "access_rules"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("expected JSON key %q in marshalled project context", key)
		}
	}

	if got["codegraph_exists"] != true {
		t.Fatalf("expected codegraph_exists true, got %#v", got["codegraph_exists"])
	}

	if got["palace_path"] != palacePath {
		t.Fatalf("expected palace_path %q, got %#v", palacePath, got["palace_path"])
	}
	if got["palace_exists"] != true {
		t.Fatalf("expected palace_exists true, got %#v", got["palace_exists"])
	}
	if got["palace_drawers"] != float64(palaceDrawers) {
		t.Fatalf("expected palace_drawers %d, got %#v", palaceDrawers, got["palace_drawers"])
	}

	accessRules, ok := got["access_rules"].(map[string]any)
	if !ok {
		t.Fatal("expected access_rules to marshal as an object")
	}
	if accessRules["read"] != "all" {
		t.Fatalf("expected access_rules.read all, got %#v", accessRules["read"])
	}
	if accessRules["write"] != "project" {
		t.Fatalf("expected access_rules.write project, got %#v", accessRules["write"])
	}
}

func TestFindRepoRootInCurrentDirectory(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	gitDir := filepath.Join(repoRoot, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}

	got, err := FindRepoRoot(repoRoot)
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}
	if got != repoRoot {
		t.Fatalf("expected repo root %q, got %q", repoRoot, got)
	}
}

func TestFindRepoRootInParentDirectory(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	gitDir := filepath.Join(repoRoot, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}

	startDir := filepath.Join(repoRoot, "src", "nested")
	if err := os.MkdirAll(startDir, 0o755); err != nil {
		t.Fatalf("create start dir: %v", err)
	}

	got, err := FindRepoRoot(startDir)
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}
	if got != repoRoot {
		t.Fatalf("expected repo root %q, got %q", repoRoot, got)
	}
}

func TestFindRepoRootWithoutRepository(t *testing.T) {
	t.Parallel()

	startDir := t.TempDir()

	got, err := FindRepoRoot(startDir)
	if !errors.Is(err, ErrNoRepository) {
		t.Fatalf("expected ErrNoRepository, got %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty repo root, got %q", got)
	}
}

func TestDetectCodeGraphExists(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	codeGraphDir := filepath.Join(repoRoot, ".codegraph")
	if err := os.Mkdir(codeGraphDir, 0o755); err != nil {
		t.Fatalf("create .codegraph dir: %v", err)
	}

	gotPath, gotExists := DetectCodeGraph(repoRoot)
	if !gotExists {
		t.Fatal("expected code graph to exist")
	}
	if gotPath != codeGraphDir {
		t.Fatalf("expected code graph path %q, got %q", codeGraphDir, gotPath)
	}
}

func TestDetectCodeGraphMissing(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	gotPath, gotExists := DetectCodeGraph(repoRoot)
	if gotExists {
		t.Fatal("expected code graph to be missing")
	}
	if gotPath != "" {
		t.Fatalf("expected empty code graph path, got %q", gotPath)
	}
}

func TestInitCodeGraphExists(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	codeGraphDir := filepath.Join(repoRoot, ".codegraph")
	if err := os.Mkdir(codeGraphDir, 0o755); err != nil {
		t.Fatalf("create .codegraph dir: %v", err)
	}

	if err := InitCodeGraph(repoRoot); err != nil {
		t.Fatalf("init codegraph: %v", err)
	}
}

func TestInitCodeGraphMissing(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	err := InitCodeGraph(repoRoot)
	if err == nil {
		t.Fatal("expected init codegraph to fail")
	}

	want := "CodeGraph not initialized at " + filepath.Join(repoRoot, ".codegraph") + ". Run codegraph init or wait for background indexing."
	if err.Error() != want {
		t.Fatalf("expected error %q, got %q", want, err.Error())
	}
}

func TestDetectPalaceExists(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	palaceDir := filepath.Join(repoRoot, ".mempalace")
	if err := os.Mkdir(palaceDir, 0o755); err != nil {
		t.Fatalf("create .mempalace dir: %v", err)
	}

	gotPath, gotExists := DetectPalace(repoRoot)
	if !gotExists {
		t.Fatal("expected palace to exist")
	}
	if gotPath != palaceDir {
		t.Fatalf("expected palace path %q, got %q", palaceDir, gotPath)
	}
}

func TestDetectPalaceMissing(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	gotPath, gotExists := DetectPalace(repoRoot)
	if gotExists {
		t.Fatal("expected palace to be missing")
	}
	if gotPath != "" {
		t.Fatalf("expected empty palace path, got %q", gotPath)
	}
}

func TestResolveProjectWithOverride(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	gitDir := filepath.Join(repoRoot, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}

	ctx, err := ResolveProject(repoRoot)
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}

	if ctx.RepoRoot != repoRoot {
		t.Fatalf("expected repo root %q, got %q", repoRoot, ctx.RepoRoot)
	}
	if ctx.RepoName != filepath.Base(repoRoot) {
		t.Fatalf("expected repo name %q, got %q", filepath.Base(repoRoot), ctx.RepoName)
	}
	if ctx.AccessRules != DefaultAccessRules() {
		t.Fatalf("expected default access rules, got %#v", ctx.AccessRules)
	}
	if ctx.Branch != "" {
		t.Fatalf("expected empty branch, got %q", ctx.Branch)
	}
	if ctx.Commit != "" {
		t.Fatalf("expected empty commit, got %q", ctx.Commit)
	}
	if ctx.CodeGraphExists {
		t.Fatal("expected code graph to be missing")
	}
	if ctx.CodeGraphPath != "" {
		t.Fatalf("expected empty code graph path, got %q", ctx.CodeGraphPath)
	}
	if ctx.PalaceExists {
		t.Fatal("expected palace to be missing")
	}
	if ctx.PalacePath != nil {
		t.Fatalf("expected nil palace path, got %v", *ctx.PalacePath)
	}
}

func TestResolveProjectWithOverrideAndCodeGraph(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	gitDir := filepath.Join(repoRoot, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}
	codeGraphDir := filepath.Join(repoRoot, ".codegraph")
	if err := os.Mkdir(codeGraphDir, 0o755); err != nil {
		t.Fatalf("create .codegraph dir: %v", err)
	}

	ctx, err := ResolveProject(repoRoot)
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}

	if !ctx.CodeGraphExists {
		t.Fatal("expected code graph to exist")
	}
	if ctx.CodeGraphPath != codeGraphDir {
		t.Fatalf("expected code graph path %q, got %q", codeGraphDir, ctx.CodeGraphPath)
	}
}

func TestResolveProjectWithOverrideAndPalace(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	gitDir := filepath.Join(repoRoot, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}
	palaceDir := filepath.Join(repoRoot, ".mempalace")
	if err := os.Mkdir(palaceDir, 0o755); err != nil {
		t.Fatalf("create .mempalace dir: %v", err)
	}

	ctx, err := ResolveProject(repoRoot)
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}

	if !ctx.PalaceExists {
		t.Fatal("expected palace to exist")
	}
	if ctx.PalacePath == nil {
		t.Fatal("expected palace path to be set")
	}
	if *ctx.PalacePath != palaceDir {
		t.Fatalf("expected palace path %q, got %q", palaceDir, *ctx.PalacePath)
	}
}

func TestResolveProjectWithMissingOverride(t *testing.T) {
	t.Parallel()

	missingRoot := filepath.Join(t.TempDir(), "missing")

	_, err := ResolveProject(missingRoot)
	if err == nil {
		t.Fatal("expected resolve project to fail")
	}

	want := "Project root does not exist: " + missingRoot
	if err.Error() != want {
		t.Fatalf("expected error %q, got %q", want, err.Error())
	}
}

func TestResolveProjectWithNonRepositoryOverride(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	_, err := ResolveProject(repoRoot)
	if err == nil {
		t.Fatal("expected resolve project to fail")
	}

	want := "No git repository at " + repoRoot
	if err.Error() != want {
		t.Fatalf("expected error %q, got %q", want, err.Error())
	}
}

func TestResolveProjectFromWorkingDirectory(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore working directory: %v", chdirErr)
		}
	})

	repoRoot := t.TempDir()
	gitDir := filepath.Join(repoRoot, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}
	nestedDir := filepath.Join(repoRoot, "nested", "dir")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}
	if err := os.Chdir(nestedDir); err != nil {
		t.Fatalf("chdir nested dir: %v", err)
	}

	ctx, err := ResolveProject("")
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}

	wantRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("evaluate repo root symlinks: %v", err)
	}
	gotRepoRoot, err := filepath.EvalSymlinks(ctx.RepoRoot)
	if err != nil {
		t.Fatalf("evaluate resolved repo root symlinks: %v", err)
	}

	if gotRepoRoot != wantRepoRoot {
		t.Fatalf("expected repo root %q, got %q", wantRepoRoot, gotRepoRoot)
	}
}

func TestResolveProjectWithoutRepository(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore working directory: %v", chdirErr)
		}
	})

	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}

	_, err = ResolveProject("")
	if err == nil {
		t.Fatal("expected resolve project to fail")
	}

	want := "No project repository found. Run from within a git repo or specify --project-root"
	if err.Error() != want {
		t.Fatalf("expected error %q, got %q", want, err.Error())
	}
}
