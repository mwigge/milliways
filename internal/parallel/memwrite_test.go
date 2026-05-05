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

package parallel_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/parallel"
)

// ---- stubOnSlotDoneStore -------------------------------------------------------

// stubOnSlotDoneStore satisfies parallel.OnSlotDoneStore for tests.
type stubOnSlotDoneStore struct {
	updated map[int64]pantry.ParallelStatus
}

func newStubOnSlotDoneStore() *stubOnSlotDoneStore {
	return &stubOnSlotDoneStore{updated: make(map[int64]pantry.ParallelStatus)}
}

func (s *stubOnSlotDoneStore) UpdateSlotStatus(handle int64, status pantry.ParallelStatus, _, _ int) error {
	s.updated[handle] = status
	return nil
}

// ---- ExtractFindings tests --------------------------------------------------

func TestExtractFindings_FileColonFormat(t *testing.T) {
	t.Parallel()
	text := "internal/foo/bar.go: null pointer dereference on line 42\ncmd/main.go: missing error check after db.Query"
	findings := parallel.ExtractFindings(text)
	if len(findings) != 2 {
		t.Fatalf("len = %d, want 2", len(findings))
	}
	if findings[0].File != "internal/foo/bar.go" {
		t.Errorf("File[0] = %q", findings[0].File)
	}
	if findings[1].File != "cmd/main.go" {
		t.Errorf("File[1] = %q", findings[1].File)
	}
}

func TestExtractFindings_NarrativeText_EmptySlice(t *testing.T) {
	t.Parallel()
	findings := parallel.ExtractFindings("This is just prose with no file references.")
	if findings == nil {
		t.Error("returned nil, want empty slice")
	}
	if len(findings) != 0 {
		t.Errorf("len = %d, want 0", len(findings))
	}
}

func TestExtractFindings_BulletedFormat_NoMatch(t *testing.T) {
	t.Parallel()
	// Lines starting with "- " do NOT match — anchored at line start without bullet.
	text := "- internal/foo/bar.go: some issue\n- cmd/main.go: another issue"
	findings := parallel.ExtractFindings(text)
	if len(findings) != 0 {
		t.Errorf("len = %d, want 0 (bullets do not match)", len(findings))
	}
}

func TestExtractFindings_Empty(t *testing.T) {
	t.Parallel()
	findings := parallel.ExtractFindings("")
	if findings == nil {
		t.Error("returned nil, want empty slice")
	}
}

// ---- WriteFindings tests ----------------------------------------------------

func TestWriteFindings_NilMP_NoPanic(t *testing.T) {
	t.Parallel()
	findings := []parallel.Finding{{File: "internal/foo/bar.go", Description: "issue"}}
	err := parallel.WriteFindings(context.Background(), findings, nil, "claude", "grp-1")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWriteFindings_FailingKGAdd_NilError(t *testing.T) {
	t.Parallel()
	mp := &stubMP{addErr: errors.New("KG write failed")}
	findings := []parallel.Finding{
		{File: "internal/foo/bar.go", Description: "issue A"},
		{File: "cmd/main.go", Description: "issue B"},
	}
	err := parallel.WriteFindings(context.Background(), findings, mp, "claude", "grp-1")
	if err != nil {
		t.Errorf("should not propagate KGAdd errors, got: %v", err)
	}
}

func TestWriteFindings_Empty_NoPanic(t *testing.T) {
	t.Parallel()
	err := parallel.WriteFindings(context.Background(), []parallel.Finding{}, &stubMP{}, "claude", "grp-1")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---- OnSlotDone tests -------------------------------------------------------

func TestOnSlotDone_NoPanic(t *testing.T) {
	t.Parallel()
	store := newStubOnSlotDoneStore()
	slot := parallel.SlotRecord{Handle: 1, Provider: "claude", Status: parallel.SlotRunning}
	// Must not panic even when final text contains no findings.
	parallel.OnSlotDone(context.Background(), slot, "grp-1", "analysis complete, no issues found", &stubMP{}, store)
	if store.updated[1] != pantry.ParallelStatusDone {
		t.Errorf("slot not marked done: %v", store.updated[1])
	}
}

func TestOnSlotDone_ExtractsAndWritesFindings(t *testing.T) {
	t.Parallel()
	store := newStubOnSlotDoneStore()
	slot := parallel.SlotRecord{Handle: 2, Provider: "codex", Status: parallel.SlotRunning}
	kgAdded := make([]string, 0)
	mp := &captureMP{added: &kgAdded}
	finalText := "internal/service/svc.go: unhandled error from http.Get"
	parallel.OnSlotDone(context.Background(), slot, "grp-2", finalText, mp, store)
	if len(kgAdded) == 0 {
		t.Error("no findings written to KG")
	}
}

// captureMP records calls to KGAdd for inspection in tests.
type captureMP struct{ added *[]string }

func (c *captureMP) KGQuery(_ context.Context, _, _ string, _ map[string]string) ([]parallel.KGTriple, error) {
	return nil, nil
}

func (c *captureMP) KGAdd(_ context.Context, subject, _, _ string, _ map[string]string) error {
	*c.added = append(*c.added, subject)
	return nil
}
