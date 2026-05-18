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
	"errors"
	"testing"
)

func TestFileStoreSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	store := NewFileStore(t.TempDir())
	wf := Workflow{
		ID:     "wf-1",
		Goal:   "persist graph",
		Status: StatusRunning,
		Nodes: []Node{
			{ID: "context", Status: StatusCompleted},
			{ID: "edit", Status: StatusQueued},
		},
		Edges: []Edge{{From: "context", To: "edit"}},
	}

	if err := store.Save(context.Background(), wf); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load(context.Background(), "wf-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != wf.ID || got.Goal != wf.Goal || len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Fatalf("loaded workflow = %#v, want round trip of %#v", got, wf)
	}
	if got.Status != StatusRunning {
		t.Fatalf("loaded status = %q, want %q", got.Status, StatusRunning)
	}
}

func TestFileStoreRejectsUnsafeWorkflowIDs(t *testing.T) {
	t.Parallel()

	store := NewFileStore(t.TempDir())
	for _, id := range []string{"../escape", " wf ", "wf/child", `wf\child`} {
		err := store.Save(context.Background(), Workflow{
			ID:     id,
			Status: StatusQueued,
			Nodes:  []Node{{ID: "a", Status: StatusQueued}},
		})
		if !errors.Is(err, ErrUnsafeWorkflowID) {
			t.Fatalf("Save(%q) error = %v, want %v", id, err, ErrUnsafeWorkflowID)
		}

		_, err = store.Load(context.Background(), id)
		if !errors.Is(err, ErrUnsafeWorkflowID) {
			t.Fatalf("Load(%q) error = %v, want %v", id, err, ErrUnsafeWorkflowID)
		}
	}
}

func TestFileStoreListReturnsSortedSummaries(t *testing.T) {
	t.Parallel()

	store := NewFileStore(t.TempDir())
	for _, wf := range []Workflow{
		{ID: "wf-b", Goal: "second", Status: StatusCompleted, Nodes: []Node{{ID: "b"}}},
		{ID: "wf-a", Goal: "first", Status: StatusQueued, Nodes: []Node{{ID: "a"}}},
	} {
		if err := store.Save(context.Background(), wf); err != nil {
			t.Fatalf("Save(%s): %v", wf.ID, err)
		}
	}

	got, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("summaries len = %d, want 2", len(got))
	}
	if got[0].ID != "wf-a" || got[1].ID != "wf-b" {
		t.Fatalf("summary order = %#v, want wf-a then wf-b", got)
	}
	if got[0].Goal != "first" || got[1].Status != StatusCompleted {
		t.Fatalf("summaries = %#v, want goal/status preserved", got)
	}
}
