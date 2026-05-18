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

func TestCodingStore_ChangeSetFileChangeLifecycle(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := db.Coding()
	created := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	changeSetID, err := store.InsertChangeSet(CodingChangeSet{
		Workspace:  "/work/repo",
		SessionID:  "session-1",
		Client:     "codex",
		TurnID:     "turn-1",
		ToolCallID: "tool-1",
		Operation:  "apply_patch",
		Status:     "open",
		Reason:     "implement requested persistence",
		CreatedAt:  created,
	})
	if err != nil {
		t.Fatalf("InsertChangeSet: %v", err)
	}
	if changeSetID == 0 {
		t.Fatal("InsertChangeSet returned zero ID")
	}

	fileChangeID, err := store.InsertFileChange(CodingFileChange{
		ChangeSetID: changeSetID,
		Workspace:   "/work/repo",
		SessionID:   "session-1",
		Client:      "codex",
		TurnID:      "turn-1",
		ToolCallID:  "tool-1",
		Operation:   "modify",
		Path:        "internal/pantry/coding.go",
		BeforeHash:  "sha256:before",
		AfterHash:   "sha256:after",
		Diff:        "-old\n+new\n",
		Preview:     "updated persistence primitives",
		CreatedAt:   created.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("InsertFileChange: %v", err)
	}
	if fileChangeID == 0 {
		t.Fatal("InsertFileChange returned zero ID")
	}

	if err := store.UpdateChangeSet(CodingChangeSet{
		ID:          changeSetID,
		Status:      "completed",
		Reason:      "tests passing",
		CompletedAt: created.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("UpdateChangeSet: %v", err)
	}

	sets, err := store.ListChangeSets("/work/repo", "session-1", 10)
	if err != nil {
		t.Fatalf("ListChangeSets: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("ListChangeSets len = %d, want 1", len(sets))
	}
	got := sets[0]
	if got.ID != changeSetID || got.Status != "completed" || got.Reason != "tests passing" {
		t.Fatalf("change set = %#v, want completed set %d", got, changeSetID)
	}
	if got.Workspace != "/work/repo" || got.SessionID != "session-1" || got.Client != "codex" {
		t.Fatalf("change set ownership fields = %#v", got)
	}
	if got.CompletedAt.IsZero() {
		t.Fatalf("CompletedAt was not persisted: %#v", got)
	}

	files, err := store.ListFileChanges(changeSetID)
	if err != nil {
		t.Fatalf("ListFileChanges: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("ListFileChanges len = %d, want 1", len(files))
	}
	file := files[0]
	if file.Path != "internal/pantry/coding.go" || file.BeforeHash != "sha256:before" || file.AfterHash != "sha256:after" {
		t.Fatalf("file hashes/path = %#v", file)
	}
	if file.Diff != "-old\n+new\n" || file.Preview != "updated persistence primitives" {
		t.Fatalf("file diff/preview = %#v", file)
	}
}

func TestCodingStore_ToolApprovalLifecycle(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := db.Coding()
	created := time.Date(2026, 5, 18, 12, 30, 0, 0, time.UTC)

	approvalID, err := store.RecordToolApproval(ToolApproval{
		Workspace:  "/work/repo",
		SessionID:  "session-2",
		Client:     "codex",
		TurnID:     "turn-2",
		ToolCallID: "tool-2",
		ToolName:   "apply_patch",
		Operation:  "write_file",
		Path:       "internal/pantry/coding.go",
		Preview:    "create coding store",
		Decision:   "pending",
		Reason:     "requires write approval",
		CreatedAt:  created,
	})
	if err != nil {
		t.Fatalf("RecordToolApproval: %v", err)
	}
	if approvalID == 0 {
		t.Fatal("RecordToolApproval returned zero ID")
	}

	if err := store.UpdateToolApprovalDecision(approvalID, "approved", "inside owned scope"); err != nil {
		t.Fatalf("UpdateToolApprovalDecision: %v", err)
	}

	approvals, err := store.ListToolApprovals("/work/repo", "session-2", 10)
	if err != nil {
		t.Fatalf("ListToolApprovals: %v", err)
	}
	if len(approvals) != 1 {
		t.Fatalf("ListToolApprovals len = %d, want 1", len(approvals))
	}
	got := approvals[0]
	if got.ID != approvalID || got.Decision != "approved" || got.Reason != "inside owned scope" {
		t.Fatalf("approval = %#v, want approved record %d", got, approvalID)
	}
	if got.ToolName != "apply_patch" || got.Operation != "write_file" || got.Path != "internal/pantry/coding.go" {
		t.Fatalf("approval tool fields = %#v", got)
	}
	if got.UpdatedAt.Before(got.CreatedAt) {
		t.Fatalf("UpdatedAt %s before CreatedAt %s", got.UpdatedAt, got.CreatedAt)
	}
}
