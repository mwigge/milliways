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

package pantry

import (
	"testing"
	"time"
)

func TestParallelStore_InsertAndGetGroup(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ps := db.Parallel()

	group := ParallelGroupRecord{
		ID:        "test-group-1",
		Prompt:    "review internal/server/",
		Status:    ParallelStatusRunning,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := ps.InsertGroup(group); err != nil {
		t.Fatalf("InsertGroup: %v", err)
	}

	got, err := ps.GetGroup("test-group-1")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if got.ID != group.ID {
		t.Errorf("ID: got %q, want %q", got.ID, group.ID)
	}
	if got.Prompt != group.Prompt {
		t.Errorf("Prompt: got %q, want %q", got.Prompt, group.Prompt)
	}
	if got.Status != ParallelStatusRunning {
		t.Errorf("Status: got %q, want %q", got.Status, ParallelStatusRunning)
	}
}

func TestParallelStore_GetGroup_NotFound(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	_, err := db.Parallel().GetGroup("does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing group, got nil")
	}
}

func TestParallelStore_InsertSlot(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ps := db.Parallel()

	group := ParallelGroupRecord{ID: "g1", Prompt: "test", Status: ParallelStatusRunning, CreatedAt: time.Now().UTC()}
	if err := ps.InsertGroup(group); err != nil {
		t.Fatalf("InsertGroup: %v", err)
	}

	slot := ParallelSlotRecord{
		GroupID:   "g1",
		Handle:    42,
		Provider:  "claude",
		Status:    ParallelStatusRunning,
		StartedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := ps.InsertSlot(slot); err != nil {
		t.Fatalf("InsertSlot: %v", err)
	}

	got, err := ps.GetGroup("g1")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if len(got.Slots) != 1 {
		t.Fatalf("Slots len: got %d, want 1", len(got.Slots))
	}
	if got.Slots[0].Provider != "claude" {
		t.Errorf("Provider: got %q, want %q", got.Slots[0].Provider, "claude")
	}
	if got.Slots[0].Handle != 42 {
		t.Errorf("Handle: got %d, want 42", got.Slots[0].Handle)
	}
}

func TestParallelStore_UpdateSlotStatus(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ps := db.Parallel()

	if err := ps.InsertGroup(ParallelGroupRecord{ID: "g2", Prompt: "p", Status: ParallelStatusRunning, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("InsertGroup: %v", err)
	}
	if err := ps.InsertSlot(ParallelSlotRecord{GroupID: "g2", Handle: 10, Provider: "codex", Status: ParallelStatusRunning, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("InsertSlot: %v", err)
	}

	if err := ps.UpdateSlotStatus(10, ParallelStatusDone, 100, 200); err != nil {
		t.Fatalf("UpdateSlotStatus: %v", err)
	}

	got, err := ps.GetGroup("g2")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if got.Slots[0].Status != ParallelStatusDone {
		t.Errorf("Status: got %q, want done", got.Slots[0].Status)
	}
	if got.Slots[0].TokensIn != 100 {
		t.Errorf("TokensIn: got %d, want 100", got.Slots[0].TokensIn)
	}
	if got.Slots[0].TokensOut != 200 {
		t.Errorf("TokensOut: got %d, want 200", got.Slots[0].TokensOut)
	}
}

func TestParallelStore_ListGroups(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ps := db.Parallel()

	for i := range 3 {
		g := ParallelGroupRecord{
			ID:        "list-g" + string(rune('0'+i)),
			Prompt:    "prompt " + string(rune('0'+i)),
			Status:    ParallelStatusRunning,
			CreatedAt: time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
		if err := ps.InsertGroup(g); err != nil {
			t.Fatalf("InsertGroup %d: %v", i, err)
		}
	}

	groups, err := ps.ListGroups(20)
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(groups) != 3 {
		t.Errorf("len: got %d, want 3", len(groups))
	}
}

func TestParallelStore_MarkInterrupted(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ps := db.Parallel()

	if err := ps.InsertGroup(ParallelGroupRecord{ID: "gi", Prompt: "p", Status: ParallelStatusRunning, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("InsertGroup: %v", err)
	}
	if err := ps.InsertSlot(ParallelSlotRecord{GroupID: "gi", Handle: 99, Provider: "local", Status: ParallelStatusRunning, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("InsertSlot: %v", err)
	}

	if err := ps.MarkInterruptedSlots(); err != nil {
		t.Fatalf("MarkInterruptedSlots: %v", err)
	}

	got, err := ps.GetGroup("gi")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if got.Slots[0].Status != ParallelStatusInterrupted {
		t.Errorf("Status: got %q, want interrupted", got.Slots[0].Status)
	}
}
