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

// --- SaveConversation ---

func TestSaveConversation_CallsAddDrawer(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{result: json.RawMessage(`{}`)}
	c := NewWithCaller(fc, "test-wing")

	conv := conversation.New("conv-1", "block-1", "do the thing")
	if err := c.SaveConversation(context.Background(), conv); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	if len(fc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fc.calls))
	}
	call := fc.lastCall()
	if call.toolName != "mempalace_add_drawer" {
		t.Errorf("expected mempalace_add_drawer, got %q", call.toolName)
	}
	if call.args["wing"] != "test-wing" {
		t.Errorf("expected wing 'test-wing', got %v", call.args["wing"])
	}
	if call.args["room"] != "conversations" {
		t.Errorf("expected room 'conversations', got %v", call.args["room"])
	}

	// Content should be valid JSON with conversation_id
	content, ok := call.args["content"].(string)
	if !ok {
		t.Fatal("content is not a string")
	}
	var rec ConversationRecord
	if err := json.Unmarshal([]byte(content), &rec); err != nil {
		t.Fatalf("content is not valid JSON: %v", err)
	}
	if rec.ConversationID != "conv-1" {
		t.Errorf("expected conversation_id 'conv-1', got %q", rec.ConversationID)
	}
	if rec.Prompt != "do the thing" {
		t.Errorf("expected prompt 'do the thing', got %q", rec.Prompt)
	}
}

func TestSaveConversation_PropagatesMCPError(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{err: fmt.Errorf("mcp down")}
	c := NewWithCaller(fc, "test-wing")

	err := c.SaveConversation(context.Background(), conversation.New("conv-err", "b", "p"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- GetConversation ---

func TestGetConversation_ParsesSearchResult(t *testing.T) {
	t.Parallel()

	rec := ConversationRecord{
		ConversationID: "conv-42",
		BlockID:        "blk-1",
		Prompt:         "hello",
		Status:         "active",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	recJSON, _ := json.Marshal(rec)

	// MemPalace returns a list of drawers; each drawer's body is in "text".
	drawers := []drawerResult{{
		ID:      "d1",
		Content: string(recJSON),
		Wing:    "test-wing",
		Room:    "conversations",
		Score:   0.99,
	}}
	drawersJSON, _ := json.Marshal(drawers)

	fc := &fakeCaller{result: drawersJSON}
	c := NewWithCaller(fc, "test-wing")

	got, err := c.GetConversation(context.Background(), "conv-42")
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got.ConversationID != "conv-42" {
		t.Errorf("expected conv-42, got %q", got.ConversationID)
	}
	if got.Prompt != "hello" {
		t.Errorf("expected 'hello', got %q", got.Prompt)
	}
}

func TestGetConversation_NotFound(t *testing.T) {
	t.Parallel()

	// Empty drawer list
	fc := &fakeCaller{result: json.RawMessage(`[]`)}
	c := NewWithCaller(fc, "test-wing")

	_, err := c.GetConversation(context.Background(), "conv-missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- SetMemory / GetMemory ---

func TestSetMemory_CallsAddDrawer(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{result: json.RawMessage(`{}`)}
	c := NewWithCaller(fc, "test-wing")

	mem := conversation.MemoryState{
		WorkingSummary: "still going",
		NextAction:     "continue",
	}
	if err := c.SetMemory(context.Background(), "conv-1", mem); err != nil {
		t.Fatalf("SetMemory: %v", err)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_add_drawer" {
		t.Errorf("expected mempalace_add_drawer, got %q", call.toolName)
	}
	if call.args["room"] != "working-memory" {
		t.Errorf("expected room 'working-memory', got %v", call.args["room"])
	}

	content, ok := call.args["content"].(string)
	if !ok {
		t.Fatal("content is not a string")
	}
	var wrapper struct {
		ConvID string                   `json:"conversation_id"`
		Memory conversation.MemoryState `json:"memory"`
	}
	if err := json.Unmarshal([]byte(content), &wrapper); err != nil {
		t.Fatalf("content JSON: %v", err)
	}
	if wrapper.ConvID != "conv-1" {
		t.Errorf("expected conversation_id 'conv-1', got %q", wrapper.ConvID)
	}
	if wrapper.Memory.WorkingSummary != "still going" {
		t.Errorf("expected 'still going', got %q", wrapper.Memory.WorkingSummary)
	}
}

// --- AppendEvent / QueryEvents ---

func TestAppendEvent_CallsAddDrawer(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{result: json.RawMessage(`{}`)}
	c := NewWithCaller(fc, "test-wing")

	ev := Event{
		ConversationID: "conv-1",
		Kind:           "segment.started",
		Payload:        `{"provider":"anthropic"}`,
		At:             time.Now(),
	}
	if err := c.AppendEvent(context.Background(), ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_add_drawer" {
		t.Errorf("expected mempalace_add_drawer, got %q", call.toolName)
	}
	if call.args["room"] != "events" {
		t.Errorf("expected room 'events', got %v", call.args["room"])
	}
}

func TestQueryEvents_FiltersById(t *testing.T) {
	t.Parallel()

	ev := Event{
		ConversationID: "conv-1",
		Kind:           "segment.started",
		Payload:        "{}",
		At:             time.Now(),
	}
	evJSON, _ := json.Marshal(ev)
	drawers := []drawerResult{{ID: "e1", Content: string(evJSON), Wing: "test-wing", Room: "events", Score: 0.9}}
	drawersJSON, _ := json.Marshal(drawers)

	fc := &fakeCaller{result: drawersJSON}
	c := NewWithCaller(fc, "test-wing")

	events, err := c.QueryEvents(context.Background(), "conv-1", "", 10)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != "segment.started" {
		t.Errorf("expected 'segment.started', got %q", events[0].Kind)
	}
}

// --- SaveCheckpoint ---

func TestSaveCheckpoint_CallsAddDrawer(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{result: json.RawMessage(`{}`)}
	c := NewWithCaller(fc, "test-wing")

	conv := conversation.New("conv-ckpt", "b", "p")
	ckpt := conv.Snapshot("test-checkpoint")

	if err := c.SaveCheckpoint(context.Background(), ckpt); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_add_drawer" {
		t.Errorf("expected mempalace_add_drawer, got %q", call.toolName)
	}
	if call.args["room"] != "checkpoints" {
		t.Errorf("expected room 'checkpoints', got %v", call.args["room"])
	}
}

// --- AppendLineage ---

func TestAppendLineage_CallsAddDrawer(t *testing.T) {
	t.Parallel()

	fc := &fakeCaller{result: json.RawMessage(`{}`)}
	c := NewWithCaller(fc, "test-wing")

	edge := LineageEdge{
		FromID:    "conv-1",
		ToID:      "conv-2",
		Reason:    "context-limit-failover",
		CreatedAt: time.Now(),
	}
	if err := c.AppendLineage(context.Background(), edge); err != nil {
		t.Fatalf("AppendLineage: %v", err)
	}

	call := fc.lastCall()
	if call.toolName != "mempalace_add_drawer" {
		t.Errorf("expected mempalace_add_drawer, got %q", call.toolName)
	}
	if call.args["room"] != "lineage" {
		t.Errorf("expected room 'lineage', got %v", call.args["room"])
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
