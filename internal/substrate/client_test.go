package substrate

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
)

// fakeCaller is a test double for the Caller interface.
type fakeCaller struct {
	calls  []fakeCall
	result json.RawMessage
	err    error
}

type fakeCall struct {
	toolName string
	args     map[string]any
}

func (f *fakeCaller) CallTool(_ context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	f.calls = append(f.calls, fakeCall{toolName: toolName, args: args})
	return f.result, f.err
}

func (f *fakeCaller) lastCall() fakeCall {
	return f.calls[len(f.calls)-1]
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// --- ConversationStart ---

func TestConversationStart_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	resp := StartResponse{ConversationID: "conv-1", Status: "active", CreatedAt: time.Now()}
	fc := &fakeCaller{result: mustJSON(resp)}
	c := NewWithCaller(fc)

	got, err := c.ConversationStart(context.Background(), StartRequest{
		ConversationID: "conv-1",
		BlockID:        "blk-1",
		Prompt:         "do the thing",
	})
	if err != nil {
		t.Fatalf("ConversationStart: %v", err)
	}
	if got.ConversationID != "conv-1" {
		t.Errorf("expected conv-1, got %q", got.ConversationID)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_start" {
		t.Errorf("expected mempalace_conversation_start, got %q", call.toolName)
	}
	if call.args["conversation_id"] != "conv-1" {
		t.Errorf("expected conversation_id conv-1, got %v", call.args["conversation_id"])
	}
	if call.args["block_id"] != "blk-1" {
		t.Errorf("expected block_id blk-1, got %v", call.args["block_id"])
	}
	if call.args["prompt"] != "do the thing" {
		t.Errorf("expected prompt 'do the thing', got %v", call.args["prompt"])
	}
}

func TestConversationStart_PropagatesMCPError(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{err: fmt.Errorf("mcp down")}
	c := NewWithCaller(fc)

	_, err := c.ConversationStart(context.Background(), StartRequest{ConversationID: "conv-err"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- ConversationEnd ---

func TestConversationEnd_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{result: json.RawMessage(`{}`)}
	c := NewWithCaller(fc)

	err := c.ConversationEnd(context.Background(), EndRequest{
		ConversationID: "conv-1",
		Status:         "done",
		Reason:         "task complete",
	})
	if err != nil {
		t.Fatalf("ConversationEnd: %v", err)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_end" {
		t.Errorf("expected mempalace_conversation_end, got %q", call.toolName)
	}
	if call.args["status"] != "done" {
		t.Errorf("expected status done, got %v", call.args["status"])
	}
}

// --- ConversationGet ---

func TestConversationGet_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	rec := ConversationRecord{ConversationID: "conv-42", Prompt: "hello", Status: "active"}
	fc := &fakeCaller{result: mustJSON(rec)}
	c := NewWithCaller(fc)

	got, err := c.ConversationGet(context.Background(), "conv-42")
	if err != nil {
		t.Fatalf("ConversationGet: %v", err)
	}
	if got.ConversationID != "conv-42" {
		t.Errorf("expected conv-42, got %q", got.ConversationID)
	}
	if got.Prompt != "hello" {
		t.Errorf("expected hello, got %q", got.Prompt)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_get" {
		t.Errorf("expected mempalace_conversation_get, got %q", call.toolName)
	}
	if call.args["conversation_id"] != "conv-42" {
		t.Errorf("expected conversation_id conv-42, got %v", call.args["conversation_id"])
	}
}

func TestConversationGet_PropagatesMCPError(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{err: fmt.Errorf("not found")}
	c := NewWithCaller(fc)

	_, err := c.ConversationGet(context.Background(), "conv-missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- ConversationList ---

func TestConversationList_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	summaries := []ConversationSummary{
		{ConversationID: "conv-1", Status: "active"},
		{ConversationID: "conv-2", Status: "done"},
	}
	fc := &fakeCaller{result: mustJSON(summaries)}
	c := NewWithCaller(fc)

	list, err := c.ConversationList(context.Background())
	if err != nil {
		t.Fatalf("ConversationList: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(list))
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_list" {
		t.Errorf("expected mempalace_conversation_list, got %q", call.toolName)
	}
}

// --- ConversationAppendTurn ---

func TestConversationAppendTurn_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{result: json.RawMessage(`{}`)}
	c := NewWithCaller(fc)

	err := c.ConversationAppendTurn(context.Background(), AppendTurnRequest{
		ConversationID: "conv-1",
		Turn: conversation.Turn{
			Role:     conversation.RoleAssistant,
			Provider: "claude",
			Text:     "here is the answer",
			At:       time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("ConversationAppendTurn: %v", err)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_append_turn" {
		t.Errorf("expected mempalace_conversation_append_turn, got %q", call.toolName)
	}
	if call.args["role"] != "assistant" {
		t.Errorf("expected role assistant, got %v", call.args["role"])
	}
	if call.args["text"] != "here is the answer" {
		t.Errorf("expected text 'here is the answer', got %v", call.args["text"])
	}
}

// --- ConversationStartSegment / EndSegment ---

func TestConversationStartSegment_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	resp := StartSegmentResponse{SegmentID: "seg-1", StartedAt: time.Now()}
	fc := &fakeCaller{result: mustJSON(resp)}
	c := NewWithCaller(fc)

	got, err := c.ConversationStartSegment(context.Background(), StartSegmentRequest{
		ConversationID: "conv-1",
		Provider:       "claude",
	})
	if err != nil {
		t.Fatalf("ConversationStartSegment: %v", err)
	}
	if got.SegmentID != "seg-1" {
		t.Errorf("expected seg-1, got %q", got.SegmentID)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_start_segment" {
		t.Errorf("expected mempalace_conversation_start_segment, got %q", call.toolName)
	}
	if call.args["provider"] != "claude" {
		t.Errorf("expected provider claude, got %v", call.args["provider"])
	}
}

func TestConversationEndSegment_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{result: json.RawMessage(`{}`)}
	c := NewWithCaller(fc)

	err := c.ConversationEndSegment(context.Background(), EndSegmentRequest{
		ConversationID: "conv-1",
		SegmentID:      "seg-1",
		Status:         "exhausted",
		Reason:         "context limit",
	})
	if err != nil {
		t.Fatalf("ConversationEndSegment: %v", err)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_end_segment" {
		t.Errorf("expected mempalace_conversation_end_segment, got %q", call.toolName)
	}
	if call.args["status"] != "exhausted" {
		t.Errorf("expected status exhausted, got %v", call.args["status"])
	}
}

// --- Working Memory ---

func TestConversationWorkingMemoryGet_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	mem := conversation.MemoryState{WorkingSummary: "in progress", NextAction: "continue"}
	fc := &fakeCaller{result: mustJSON(mem)}
	c := NewWithCaller(fc)

	got, err := c.ConversationWorkingMemoryGet(context.Background(), "conv-1")
	if err != nil {
		t.Fatalf("WorkingMemoryGet: %v", err)
	}
	if got.WorkingSummary != "in progress" {
		t.Errorf("expected 'in progress', got %q", got.WorkingSummary)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_working_memory_get" {
		t.Errorf("expected mempalace_conversation_working_memory_get, got %q", call.toolName)
	}
}

func TestConversationWorkingMemorySet_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{result: json.RawMessage(`{}`)}
	c := NewWithCaller(fc)

	mem := conversation.MemoryState{WorkingSummary: "done", NextAction: "finish"}
	err := c.ConversationWorkingMemorySet(context.Background(), "conv-1", mem)
	if err != nil {
		t.Fatalf("WorkingMemorySet: %v", err)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_working_memory_set" {
		t.Errorf("expected mempalace_conversation_working_memory_set, got %q", call.toolName)
	}
	if call.args["conversation_id"] != "conv-1" {
		t.Errorf("expected conversation_id conv-1, got %v", call.args["conversation_id"])
	}
}

// --- Context Bundle ---

func TestConversationContextBundleGet_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	bundle := conversation.ContextBundle{MemPalaceText: "some context"}
	fc := &fakeCaller{result: mustJSON(bundle)}
	c := NewWithCaller(fc)

	got, err := c.ConversationContextBundleGet(context.Background(), "conv-1")
	if err != nil {
		t.Fatalf("ContextBundleGet: %v", err)
	}
	if got.MemPalaceText != "some context" {
		t.Errorf("expected 'some context', got %q", got.MemPalaceText)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_context_bundle_get" {
		t.Errorf("expected mempalace_conversation_context_bundle_get, got %q", call.toolName)
	}
}

func TestConversationContextBundleSet_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{result: json.RawMessage(`{}`)}
	c := NewWithCaller(fc)

	bundle := conversation.ContextBundle{CodeGraphText: "graph data"}
	err := c.ConversationContextBundleSet(context.Background(), "conv-1", bundle)
	if err != nil {
		t.Fatalf("ContextBundleSet: %v", err)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_context_bundle_set" {
		t.Errorf("expected mempalace_conversation_context_bundle_set, got %q", call.toolName)
	}
}

// --- Events ---

func TestConversationEventsAppend_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{result: json.RawMessage(`{}`)}
	c := NewWithCaller(fc)

	ev := Event{
		ConversationID: "conv-1",
		Kind:           "segment.started",
		Payload:        `{"provider":"claude"}`,
	}
	err := c.ConversationEventsAppend(context.Background(), ev)
	if err != nil {
		t.Fatalf("EventsAppend: %v", err)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_events_append" {
		t.Errorf("expected mempalace_conversation_events_append, got %q", call.toolName)
	}
	if call.args["kind"] != "segment.started" {
		t.Errorf("expected kind segment.started, got %v", call.args["kind"])
	}
	if call.args["payload"] != `{"provider":"claude"}` {
		t.Errorf("unexpected payload: %v", call.args["payload"])
	}
}

func TestConversationEventsQuery_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	events := []Event{
		{ConversationID: "conv-1", Kind: "segment.started", Payload: "{}"},
	}
	fc := &fakeCaller{result: mustJSON(events)}
	c := NewWithCaller(fc)

	got, err := c.ConversationEventsQuery(context.Background(), EventsQueryRequest{
		ConversationID: "conv-1",
		Kind:           "segment.started",
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("EventsQuery: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Kind != "segment.started" {
		t.Errorf("expected segment.started, got %q", got[0].Kind)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_events_query" {
		t.Errorf("expected mempalace_conversation_events_query, got %q", call.toolName)
	}
}

// --- Checkpoint ---

func TestConversationCheckpoint_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	resp := CheckpointResponse{CheckpointID: "ckpt-1", TakenAt: time.Now()}
	fc := &fakeCaller{result: mustJSON(resp)}
	c := NewWithCaller(fc)

	got, err := c.ConversationCheckpoint(context.Background(), CheckpointRequest{
		ConversationID: "conv-1",
		Reason:         "context-limit-failover",
	})
	if err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	if got.CheckpointID != "ckpt-1" {
		t.Errorf("expected ckpt-1, got %q", got.CheckpointID)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_checkpoint" {
		t.Errorf("expected mempalace_conversation_checkpoint, got %q", call.toolName)
	}
	if call.args["reason"] != "context-limit-failover" {
		t.Errorf("expected reason context-limit-failover, got %v", call.args["reason"])
	}
}

// --- Resume ---

func TestConversationResume_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	resp := ResumeResponse{
		ConversationID: "conv-1",
		RestoredFrom:   "ckpt-1",
		Memory:         conversation.MemoryState{WorkingSummary: "resuming"},
	}
	fc := &fakeCaller{result: mustJSON(resp)}
	c := NewWithCaller(fc)

	got, err := c.ConversationResume(context.Background(), ResumeRequest{
		ConversationID: "conv-1",
		CheckpointID:   "ckpt-1",
	})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got.RestoredFrom != "ckpt-1" {
		t.Errorf("expected ckpt-1, got %q", got.RestoredFrom)
	}
	if got.Memory.WorkingSummary != "resuming" {
		t.Errorf("expected 'resuming', got %q", got.Memory.WorkingSummary)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_resume" {
		t.Errorf("expected mempalace_conversation_resume, got %q", call.toolName)
	}
	if call.args["checkpoint_id"] != "ckpt-1" {
		t.Errorf("expected checkpoint_id ckpt-1, got %v", call.args["checkpoint_id"])
	}
}

// --- Lineage ---

func TestConversationLineage_CallsCorrectTool(t *testing.T) {
	t.Parallel()

	resp := LineageResponse{Edges: []LineageEdge{{FromID: "conv-1", ToID: "conv-2", Reason: "failover"}}}
	fc := &fakeCaller{result: mustJSON(resp)}
	c := NewWithCaller(fc)

	got, err := c.ConversationLineage(context.Background(), LineageEdge{
		FromID: "conv-1",
		ToID:   "conv-2",
		Reason: "failover",
	})
	if err != nil {
		t.Fatalf("Lineage: %v", err)
	}
	if len(got.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(got.Edges))
	}
	if got.Edges[0].Reason != "failover" {
		t.Errorf("expected failover, got %q", got.Edges[0].Reason)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_conversation_lineage" {
		t.Errorf("expected mempalace_conversation_lineage, got %q", call.toolName)
	}
	if call.args["from_id"] != "conv-1" {
		t.Errorf("expected from_id conv-1, got %v", call.args["from_id"])
	}
	if call.args["to_id"] != "conv-2" {
		t.Errorf("expected to_id conv-2, got %v", call.args["to_id"])
	}
}

// --- parseContent helper ---

func TestParseContent_DirectJSON(t *testing.T) {
	t.Parallel()

	type item struct {
		Name string `json:"name"`
	}
	raw := json.RawMessage(`[{"name":"alpha"}]`)

	got, err := parseContent[[]item](raw)
	if err != nil {
		t.Fatalf("parseContent: %v", err)
	}
	if len(got) != 1 || got[0].Name != "alpha" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestParseContent_MCPWrapper(t *testing.T) {
	t.Parallel()

	type item struct {
		Name string `json:"name"`
	}
	inner := `[{"name":"wrapped"}]`
	raw := json.RawMessage(fmt.Sprintf(`{"content":[{"type":"text","text":%q}]}`, inner))

	got, err := parseContent[[]item](raw)
	if err != nil {
		t.Fatalf("parseContent: %v", err)
	}
	if len(got) != 1 || got[0].Name != "wrapped" {
		t.Errorf("unexpected result: %+v", got)
	}
}
