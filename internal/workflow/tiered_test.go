package workflow

import (
	"testing"
	"time"
)

func TestNewTieredWorkflowHasRequiredNodeTypes(t *testing.T) {
	wf := NewTieredWorkflow("tiered-1", "fix tests", time.Now())
	if err := ValidateTieredWorkflow(wf); err != nil {
		t.Fatal(err)
	}
}

func TestConflictAwareReadyNodesSerializesOverlappingPaths(t *testing.T) {
	wf := Workflow{
		ID: "parallel", Status: StatusQueued,
		Nodes: []Node{
			{ID: "one", Type: NodeExecute, Status: StatusQueued, Security: SecurityEnvelope{Paths: []string{"src"}}},
			{ID: "two", Type: NodeExecute, Status: StatusQueued, Security: SecurityEnvelope{Paths: []string{"src/main.go"}}},
			{ID: "three", Type: NodeExecute, Status: StatusQueued, Security: SecurityEnvelope{Paths: []string{"docs"}}},
		},
	}
	ready, serialized, err := ConflictAwareReadyNodes(wf)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 2 || len(serialized) != 1 {
		t.Fatalf("ready=%v serialized=%v", ready, serialized)
	}
}

func TestDefaultRepairAttemptThenTakeover(t *testing.T) {
	if got := RepairOrTakeover(Node{RetryCount: 0}, 1); got != "repair" {
		t.Fatalf("first decision = %s", got)
	}
	if got := RepairOrTakeover(Node{RetryCount: 1}, 1); got != "supervisor-takeover" {
		t.Fatalf("second decision = %s", got)
	}
}

func TestDisableLocalExecutionCancelsQueuedAndDrainsRunning(t *testing.T) {
	now := time.Now()
	wf := Workflow{
		ID: "disable", Status: StatusRunning,
		Nodes: []Node{
			{ID: "queued", Type: NodeExecute, Status: StatusQueued},
			{ID: "running", Type: NodeVerify, Status: StatusRunning},
			{ID: "review", Type: NodeReview, Status: StatusQueued},
		},
	}
	got, err := DisableLocalExecution(wf, now, "operator disabled local execution")
	if err != nil {
		t.Fatal(err)
	}
	if got.Nodes[0].Status != StatusCanceled || got.Nodes[1].Status != StatusDraining || got.Nodes[2].Status != StatusQueued {
		t.Fatalf("nodes = %#v", got.Nodes)
	}
}

func TestRecoverTieredInterruptedRejectsInvalidWorkflow(t *testing.T) {
	wf := Workflow{
		Status: StatusRunning,
		Nodes: []Node{
			{ID: "review", Type: NodeReview, Status: StatusRunning},
		},
	}
	if _, _, err := RecoverTieredInterrupted(wf, time.Now(), true); err == nil {
		t.Fatal("expected error for workflow with missing ID")
	}
}

func TestRecoveryClearsStaleEndedAtOnResumedReview(t *testing.T) {
	now := time.Now()
	wf := Workflow{
		ID: "resume-ended-at", Status: StatusRunning,
		Nodes: []Node{
			{ID: "execute", Type: NodeExecute, Status: StatusCompleted, Outputs: map[string]string{"diff": "accepted"}},
			{ID: "verify", Type: NodeVerify, Status: StatusCompleted, Outputs: map[string]string{"tests": "passed"}},
			{ID: "review", Type: NodeReview, Status: StatusRunning, StartedAt: now.Add(-time.Hour), EndedAt: now.Add(-30 * time.Minute)},
		},
	}
	got, changed, err := RecoverTieredInterrupted(wf, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected workflow to be marked changed")
	}
	review := got.Nodes[2]
	if review.Status != StatusQueued || !review.StartedAt.IsZero() || !review.EndedAt.IsZero() {
		t.Fatalf("review node = %#v", review)
	}
}

func TestRecoveryResumesReviewWithoutRepeatingCompletedExecution(t *testing.T) {
	now := time.Now()
	wf := Workflow{
		ID: "resume", Status: StatusRunning,
		Nodes: []Node{
			{ID: "execute", Type: NodeExecute, Status: StatusCompleted, Outputs: map[string]string{"diff": "accepted"}},
			{ID: "verify", Type: NodeVerify, Status: StatusCompleted, Outputs: map[string]string{"tests": "passed"}},
			{ID: "review", Type: NodeReview, Status: StatusRunning},
		},
	}
	got, changed, err := RecoverTieredInterrupted(wf, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || got.Nodes[0].Status != StatusCompleted || got.Nodes[1].Status != StatusCompleted || got.Nodes[2].Status != StatusQueued {
		t.Fatalf("workflow = %#v", got)
	}
}
