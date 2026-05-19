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

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	// ErrUnsafeWorkflowID means a workflow ID cannot be mapped to local storage.
	ErrUnsafeWorkflowID = errors.New("unsafe workflow id")
	// ErrWorkflowNotFound means the requested workflow does not exist.
	ErrWorkflowNotFound = errors.New("workflow not found")
)

// FileStore persists workflow graphs as JSON files under one directory.
type FileStore struct {
	root string
}

// Summary is the lightweight workflow list shape for status and queue views.
type Summary struct {
	ID     string `json:"id"`
	Goal   string `json:"goal,omitempty"`
	Status Status `json:"status"`
	Nodes  int    `json:"nodes"`
}

// NewFileStore creates a file-backed workflow store rooted at dir.
func NewFileStore(dir string) *FileStore {
	return &FileStore{root: filepath.Clean(dir)}
}

// Save validates and writes a workflow graph.
func (s *FileStore) Save(ctx context.Context, wf Workflow) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := Validate(wf); err != nil {
		return err
	}
	path, err := s.pathForID(wf.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return fmt.Errorf("workflow store mkdir: %w", err)
	}
	raw, err := json.MarshalIndent(wf, "", "  ")
	if err != nil {
		return fmt.Errorf("workflow marshal: %w", err)
	}
	tmp, err := os.CreateTemp(s.root, cleanTempPrefix(wf.ID)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("workflow temp %s: %w", wf.ID, err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("workflow temp write %s: %w", wf.ID, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("workflow temp close %s: %w", wf.ID, err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("workflow temp chmod %s: %w", wf.ID, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("workflow write %s: %w", wf.ID, err)
	}
	cleanup = false
	return nil
}

// Load reads and validates one workflow graph.
func (s *FileStore) Load(ctx context.Context, id string) (Workflow, error) {
	if err := ctx.Err(); err != nil {
		return Workflow{}, err
	}
	path, err := s.pathForID(id)
	if err != nil {
		return Workflow{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Workflow{}, fmt.Errorf("%w: %s", ErrWorkflowNotFound, id)
		}
		return Workflow{}, fmt.Errorf("workflow read %s: %w", id, err)
	}
	var wf Workflow
	if err := json.Unmarshal(raw, &wf); err != nil {
		return Workflow{}, fmt.Errorf("workflow decode %s: %w", id, err)
	}
	if err := Validate(wf); err != nil {
		return Workflow{}, err
	}
	return wf, nil
}

// List returns sorted summaries for all persisted workflow graphs.
func (s *FileStore) List(ctx context.Context) ([]Summary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("workflow store list: %w", err)
	}
	summaries := make([]Summary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		wf, err := s.Load(ctx, id)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, Summary{
			ID:     wf.ID,
			Goal:   wf.Goal,
			Status: wf.Status,
			Nodes:  len(wf.Nodes),
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].ID < summaries[j].ID
	})
	return summaries, nil
}

// RecoverInterrupted marks persisted workflows with in-flight nodes as failed
// after daemon restart. It returns the number of workflow files updated.
func (s *FileStore) RecoverInterrupted(ctx context.Context, recoveredAt time.Time, reason string) (int, error) {
	summaries, err := s.List(ctx)
	if err != nil {
		return 0, err
	}
	updatedCount := 0
	for _, summary := range summaries {
		wf, err := s.Load(ctx, summary.ID)
		if err != nil {
			return updatedCount, err
		}
		updated, changed, err := RecoverInterrupted(wf, recoveredAt, reason)
		if err != nil {
			return updatedCount, err
		}
		if !changed {
			continue
		}
		if err := s.Save(ctx, updated); err != nil {
			return updatedCount, err
		}
		updatedCount++
	}
	return updatedCount, nil
}

func (s *FileStore) pathForID(id string) (string, error) {
	cleanID := strings.TrimSpace(id)
	if cleanID == "" || cleanID != id || strings.Contains(cleanID, "/") || strings.Contains(cleanID, `\`) || cleanID == "." || cleanID == ".." {
		return "", fmt.Errorf("%w: %q", ErrUnsafeWorkflowID, id)
	}
	return filepath.Join(s.root, cleanID+".json"), nil
}

func cleanTempPrefix(id string) string {
	replacer := strings.NewReplacer("/", "_", `\`, "_", ".", "_")
	return replacer.Replace(id)
}
