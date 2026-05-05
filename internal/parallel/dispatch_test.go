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
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/parallel"
)

// ---- test helpers -----------------------------------------------------------

func openTestStore(t *testing.T) *pantry.ParallelStore {
	t.Helper()
	db, err := pantry.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("pantry.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db.Parallel()
}

// ---- stub AgentOpener -------------------------------------------------------

type stubOpener struct {
	failProviders map[string]error
	mu            sync.Mutex
	nextHandle    int64
}

func (s *stubOpener) OpenSession(_ context.Context, providerID string) (int64, error) {
	if err, ok := s.failProviders[providerID]; ok {
		return 0, err
	}
	s.mu.Lock()
	s.nextHandle++
	h := s.nextHandle
	s.mu.Unlock()
	return h, nil
}

// ---- stub MPClient ----------------------------------------------------------

type stubMP struct {
	queryResults []parallel.KGTriple
	queryErr     error
	addErr       error
}

func (m *stubMP) KGQuery(_ context.Context, _, _ string, _ map[string]string) ([]parallel.KGTriple, error) {
	return m.queryResults, m.queryErr
}

func (m *stubMP) KGAdd(_ context.Context, _, _, _ string, _ map[string]string) error {
	return m.addErr
}

// ---- Dispatch tests ---------------------------------------------------------

func TestDispatch_ThreeProviders_ThreeSlots(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	opener := &stubOpener{}
	req := parallel.DispatchRequest{
		Prompt:    "review internal/server/",
		Providers: []string{"claude", "codex", "local"},
	}
	result, err := parallel.Dispatch(context.Background(), req, opener, store, nil, nil)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result.GroupID == "" {
		t.Error("GroupID is empty")
	}
	if len(result.Slots) != 3 {
		t.Errorf("len(Slots) = %d, want 3", len(result.Slots))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("len(Skipped) = %d, want 0", len(result.Skipped))
	}
	// Verify persisted
	g, err := store.GetGroup(result.GroupID)
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if len(g.Slots) != 3 {
		t.Errorf("persisted slots = %d, want 3", len(g.Slots))
	}
}

func TestDispatch_OneProviderFails_InSkipped(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	opener := &stubOpener{failProviders: map[string]error{"codex": errors.New("auth error")}}
	req := parallel.DispatchRequest{
		Prompt:    "test",
		Providers: []string{"claude", "codex", "local"},
	}
	result, err := parallel.Dispatch(context.Background(), req, opener, store, nil, nil)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(result.Slots) != 2 {
		t.Errorf("len(Slots) = %d, want 2", len(result.Slots))
	}
	if len(result.Skipped) != 1 {
		t.Errorf("len(Skipped) = %d, want 1", len(result.Skipped))
	}
	if result.Skipped[0].Provider != "codex" {
		t.Errorf("Skipped[0].Provider = %q, want codex", result.Skipped[0].Provider)
	}
	if !strings.Contains(result.Skipped[0].Reason, "auth error") {
		t.Errorf("Skipped[0].Reason = %q, want auth error", result.Skipped[0].Reason)
	}
}

func TestDispatch_AllProvidersFail_Error(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	opener := &stubOpener{failProviders: map[string]error{
		"claude": errors.New("down"),
		"codex":  errors.New("down"),
	}}
	_, err := parallel.Dispatch(context.Background(), parallel.DispatchRequest{
		Prompt:    "test",
		Providers: []string{"claude", "codex"},
	}, opener, store, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDispatch_CustomGroupID_Preserved(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	result, err := parallel.Dispatch(context.Background(), parallel.DispatchRequest{
		GroupID:   "my-id",
		Prompt:    "test",
		Providers: []string{"claude"},
	}, &stubOpener{}, store, nil, nil)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result.GroupID != "my-id" {
		t.Errorf("GroupID = %q, want my-id", result.GroupID)
	}
}

// ---- InjectBaseline tests ---------------------------------------------------

func TestInjectBaseline_NilMP_Empty(t *testing.T) {
	t.Parallel()
	got := parallel.InjectBaseline(context.Background(), "review anything", nil)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestInjectBaseline_NoFilePath_Empty(t *testing.T) {
	t.Parallel()
	mp := &stubMP{queryResults: []parallel.KGTriple{{Subject: "file:foo.go"}}}
	got := parallel.InjectBaseline(context.Background(), "just a plain prompt", mp)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestInjectBaseline_FilePathFound_ReturnsBlock(t *testing.T) {
	t.Parallel()
	mp := &stubMP{queryResults: []parallel.KGTriple{{
		Subject:    "file:internal/server/auth.go",
		Predicate:  "has_finding",
		Object:     "null deref at line 42",
		Properties: map[string]string{"source": "claude", "ts": "2026-01-01T00:00:00Z"},
	}}}
	got := parallel.InjectBaseline(context.Background(), "review internal/server/auth.go", mp)
	if got == "" {
		t.Fatal("got empty, want findings block")
	}
	if !strings.Contains(got, "null deref") {
		t.Errorf("missing finding in output: %q", got)
	}
	if !strings.Contains(got, "[prior findings from mempalace]") {
		t.Errorf("missing header in output: %q", got)
	}
}

func TestInjectBaseline_KGQueryError_Empty(t *testing.T) {
	t.Parallel()
	mp := &stubMP{queryErr: errors.New("network")}
	got := parallel.InjectBaseline(context.Background(), "review internal/server/auth.go", mp)
	if got != "" {
		t.Errorf("got %q, want empty on error", got)
	}
}

func TestInjectBaseline_Truncates(t *testing.T) {
	t.Parallel()
	triples := make([]parallel.KGTriple, 25)
	for i := range triples {
		triples[i] = parallel.KGTriple{
			Subject:    "file:cmd/foo/main.go",
			Predicate:  "has_finding",
			Object:     "issue",
			Properties: map[string]string{"source": "bot", "ts": "2026-01-01T00:00:00Z"},
		}
	}
	got := parallel.InjectBaseline(context.Background(), "review cmd/foo/main.go", &stubMP{queryResults: triples})
	if !strings.Contains(got, "[truncated") {
		t.Errorf("missing truncation note in output: %q", got)
	}
}

// ---- RecoverInterrupted -----------------------------------------------------

func TestRecoverInterrupted(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	// Insert a running slot
	if err := store.InsertGroup(pantry.ParallelGroupRecord{
		ID: "g1", Prompt: "p", Status: pantry.ParallelStatusRunning,
	}); err != nil {
		t.Fatalf("InsertGroup: %v", err)
	}
	if err := store.InsertSlot(pantry.ParallelSlotRecord{
		GroupID: "g1", Handle: 5, Provider: "claude", Status: pantry.ParallelStatusRunning,
	}); err != nil {
		t.Fatalf("InsertSlot: %v", err)
	}

	if err := parallel.RecoverInterrupted(store); err != nil {
		t.Fatalf("RecoverInterrupted: %v", err)
	}

	g, err := store.GetGroup("g1")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if g.Slots[0].Status != pantry.ParallelStatusInterrupted {
		t.Errorf("status = %q, want interrupted", g.Slots[0].Status)
	}
}
