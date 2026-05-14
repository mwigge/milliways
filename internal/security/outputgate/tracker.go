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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WorkspaceSnapshot is a content snapshot used to identify files generated or
// modified during an agent turn. It is intentionally scanner-agnostic.
type WorkspaceSnapshot struct {
	Root  string
	files map[string]fileState
}

type fileState struct {
	hash string
	size int64
	mode os.FileMode
}

// CaptureWorkspace snapshots regular files under root. Heavy and generated
// dependency/cache directories are skipped so the hook can run at turn
// boundaries without walking common large trees.
func CaptureWorkspace(root string) (WorkspaceSnapshot, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return WorkspaceSnapshot{}, fmt.Errorf("workspace root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return WorkspaceSnapshot{}, fmt.Errorf("resolve workspace root: %w", err)
	}
	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		return WorkspaceSnapshot{}, fmt.Errorf("resolve workspace root symlinks: %w", err)
	}

	snap := WorkspaceSnapshot{Root: absRoot, files: map[string]fileState{}}
	err = filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != absRoot && shouldSkipSnapshotDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}
		hash, err := hashFile(path)
		if err != nil {
			return err
		}
		snap.files[filepath.ToSlash(rel)] = fileState{
			hash: hash,
			size: info.Size(),
			mode: info.Mode().Perm(),
		}
		return nil
	})
	if err != nil {
		return WorkspaceSnapshot{}, fmt.Errorf("snapshot workspace %q: %w", absRoot, err)
	}
	return snap, nil
}

// DiffSnapshots returns generated-source changes between before and after.
// Deleted files are included for auditability; PlanScans ignores them because
// there is no current content to scan.
func DiffSnapshots(before, after WorkspaceSnapshot) []FileChange {
	changes := make([]FileChange, 0)
	seen := map[string]struct{}{}
	for path, next := range after.files {
		prev, ok := before.files[path]
		if !ok {
			changes = append(changes, FileChange{Path: path, Status: StatusAdded, Source: SourceGenerated})
			seen[path] = struct{}{}
			continue
		}
		if prev != next {
			changes = append(changes, FileChange{Path: path, Status: StatusModified, Source: SourceGenerated})
		}
		seen[path] = struct{}{}
	}
	for path := range before.files {
		if _, ok := seen[path]; !ok {
			changes = append(changes, FileChange{Path: path, Status: StatusDeleted, Source: SourceGenerated})
		}
	}
	sortFileChanges(changes)
	return changes
}

// PlanSnapshotDiff plans scanner work for files changed between snapshots.
func PlanSnapshotDiff(before, after WorkspaceSnapshot) Plan {
	return PlanScans(DiffSnapshots(before, after))
}

// GeneratedFileChanges converts an explicit list of generated files to
// generated-source changes. When root is provided, absolute paths must resolve
// inside root and missing files are marked deleted.
func GeneratedFileChanges(root string, paths []string) ([]FileChange, error) {
	absRoot := ""
	if strings.TrimSpace(root) != "" {
		var err error
		absRoot, err = filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace root: %w", err)
		}
		absRoot, err = filepath.EvalSymlinks(absRoot)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace root symlinks: %w", err)
		}
	}

	changesByPath := map[string]FileChange{}
	for _, raw := range paths {
		path, status, err := generatedPathChange(absRoot, raw)
		if err != nil {
			return nil, err
		}
		if path == "" {
			continue
		}
		changesByPath[path] = FileChange{Path: path, Status: status, Source: SourceGenerated}
	}
	changes := make([]FileChange, 0, len(changesByPath))
	for _, change := range changesByPath {
		changes = append(changes, change)
	}
	sortFileChanges(changes)
	return changes, nil
}

// PlanGeneratedFiles plans scanner work for an explicit generated-file list.
func PlanGeneratedFiles(root string, paths []string) (Plan, error) {
	changes, err := GeneratedFileChanges(root, paths)
	if err != nil {
		return Plan{}, err
	}
	return PlanScans(changes), nil
}

func generatedPathChange(absRoot, raw string) (string, ChangeStatus, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil
	}
	if absRoot == "" {
		return normalizedPath(raw), StatusModified, nil
	}
	candidate := raw
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(absRoot, candidate)
	}
	absPath, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", fmt.Errorf("resolve generated file %q: %w", raw, err)
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return "", "", fmt.Errorf("relativize generated file %q: %w", raw, err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("generated file %q escapes workspace root", raw)
	}
	if _, err := os.Stat(absPath); err != nil {
		if os.IsNotExist(err) {
			return filepath.ToSlash(filepath.Clean(rel)), StatusDeleted, nil
		}
		return "", "", fmt.Errorf("stat generated file %q: %w", raw, err)
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve generated file symlinks %q: %w", raw, err)
	}
	resolvedRel, err := filepath.Rel(absRoot, resolvedPath)
	if err != nil {
		return "", "", fmt.Errorf("relativize generated file symlinks %q: %w", raw, err)
	}
	if resolvedRel == "." || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("generated file %q resolves outside workspace root", raw)
	}
	return filepath.ToSlash(filepath.Clean(rel)), StatusModified, nil
}

func shouldSkipSnapshotDir(name string) bool {
	switch name {
	case ".git", ".milliways", "node_modules":
		return true
	default:
		return false
	}
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sortFileChanges(changes []FileChange) {
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Path != changes[j].Path {
			return changes[i].Path < changes[j].Path
		}
		if changes[i].Status != changes[j].Status {
			return changes[i].Status < changes[j].Status
		}
		return changes[i].Source < changes[j].Source
	})
}
