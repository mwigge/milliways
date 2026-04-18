package substrate

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
)

// multiResultCaller returns a pre-configured result per call index, cycling
// when the list is exhausted. It records every call for later inspection.
type multiResultCaller struct {
	results []json.RawMessage
	calls   []fakeCall
	idx     int
	err     error // if non-nil, returned on every call
}

func (m *multiResultCaller) CallTool(_ context.Context, toolName string, args map[string]any) (json.RawMessage, error) {
	m.calls = append(m.calls, fakeCall{toolName: toolName, args: args})
	if m.err != nil {
		return nil, m.err
	}
	if len(m.results) == 0 {
		return json.RawMessage(`{}`), nil
	}
	result := m.results[m.idx%len(m.results)]
	m.idx++
	return result, nil
}

func (m *multiResultCaller) toolNames() []string {
	names := make([]string, len(m.calls))
	for i, c := range m.calls {
		names[i] = c.toolName
	}
	return names
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- Begin ---

func TestSessionWriter_Begin_CallsStartThenAppendTurn(t *testing.T) {
	t.Parallel()

	startResp := StartResponse{ConversationID: "conv-1", Status: "active", CreatedAt: time.Now()}
	mc := &multiResultCaller{
		results: []json.RawMessage{mustJSON(startResp), json.RawMessage(`{}`)},
	}
	w := NewSessionWriter(NewWithCaller(mc))

	if err := w.Begin(context.Background(), "conv-1", "blk-1", "claude", "do the thing"); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	want := []string{
		"mempalace_conversation_start",
		"mempalace_conversation_append_turn",
	}
	if got := mc.toolNames(); !stringsEqual(got, want) {
		t.Errorf("tool call order: got %v, want %v", got, want)
	}

	// Verify conversation_start args
	startCall := mc.calls[0]
	if startCall.args["conversation_id"] != "conv-1" {
		t.Errorf("conversation_start: expected conv-1, got %v", startCall.args["conversation_id"])
	}
	if startCall.args["block_id"] != "blk-1" {
		t.Errorf("conversation_start: expected blk-1, got %v", startCall.args["block_id"])
	}
	if startCall.args["prompt"] != "do the thing" {
		t.Errorf("conversation_start: unexpected prompt %v", startCall.args["prompt"])
	}

	// Verify append_turn is the user turn with the prompt text
	appendCall := mc.calls[1]
	if appendCall.args["role"] != "user" {
		t.Errorf("append_turn: expected role user, got %v", appendCall.args["role"])
	}
	if appendCall.args["text"] != "do the thing" {
		t.Errorf("append_turn: expected prompt text, got %v", appendCall.args["text"])
	}
}

func TestSessionWriter_Begin_PropagatesStartError(t *testing.T) {
	t.Parallel()

	mc := &multiResultCaller{err: fmt.Errorf("mcp down")}
	w := NewSessionWriter(NewWithCaller(mc))

	if err := w.Begin(context.Background(), "conv-err", "", "claude", "prompt"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- StartSegment ---

func TestSessionWriter_StartSegment_CallsStartSegmentAndStoresID(t *testing.T) {
	t.Parallel()

	segResp := StartSegmentResponse{SegmentID: "seg-42", StartedAt: time.Now()}
	mc := &multiResultCaller{results: []json.RawMessage{mustJSON(segResp)}}
	w := NewSessionWriter(NewWithCaller(mc))
	w.convID = "conv-1"

	if err := w.StartSegment(context.Background(), "claude"); err != nil {
		t.Fatalf("StartSegment: %v", err)
	}
	if w.segmentID != "seg-42" {
		t.Errorf("expected segmentID seg-42, got %q", w.segmentID)
	}

	call := mc.calls[0]
	if call.toolName != "mempalace_conversation_start_segment" {
		t.Errorf("expected mempalace_conversation_start_segment, got %q", call.toolName)
	}
	if call.args["provider"] != "claude" {
		t.Errorf("expected provider claude, got %v", call.args["provider"])
	}
}

// --- AppendTurn ---

func TestSessionWriter_AppendTurn_CallsAppendTurnTool(t *testing.T) {
	t.Parallel()

	mc := &multiResultCaller{results: []json.RawMessage{json.RawMessage(`{}`)}}
	w := NewSessionWriter(NewWithCaller(mc))
	w.convID = "conv-1"

	err := w.AppendTurn(context.Background(), conversation.RoleAssistant, "claude", "here is my answer")
	if err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}

	call := mc.calls[0]
	if call.toolName != "mempalace_conversation_append_turn" {
		t.Errorf("expected mempalace_conversation_append_turn, got %q", call.toolName)
	}
	if call.args["role"] != "assistant" {
		t.Errorf("expected role assistant, got %v", call.args["role"])
	}
	if call.args["text"] != "here is my answer" {
		t.Errorf("expected text 'here is my answer', got %v", call.args["text"])
	}
}

// --- EndSegment ---

func TestSessionWriter_EndSegment_CallsEndSegmentAndClearsID(t *testing.T) {
	t.Parallel()

	mc := &multiResultCaller{results: []json.RawMessage{json.RawMessage(`{}`)}}
	w := NewSessionWriter(NewWithCaller(mc))
	w.convID = "conv-1"
	w.segmentID = "seg-1"

	if err := w.EndSegment(context.Background(), "done", "task complete"); err != nil {
		t.Fatalf("EndSegment: %v", err)
	}
	if w.segmentID != "" {
		t.Errorf("expected segmentID cleared, got %q", w.segmentID)
	}

	call := mc.calls[0]
	if call.toolName != "mempalace_conversation_end_segment" {
		t.Errorf("expected mempalace_conversation_end_segment, got %q", call.toolName)
	}
	if call.args["status"] != "done" {
		t.Errorf("expected status done, got %v", call.args["status"])
	}
	if call.args["segment_id"] != "seg-1" {
		t.Errorf("expected segment_id seg-1, got %v", call.args["segment_id"])
	}
}

// --- CheckpointOnExhaustion ---

func TestSessionWriter_CheckpointOnExhaustion_EndsSegmentExhaustedThenCheckpoints(t *testing.T) {
	t.Parallel()

	ckptResp := CheckpointResponse{CheckpointID: "ckpt-7", TakenAt: time.Now()}
	mc := &multiResultCaller{
		results: []json.RawMessage{
			json.RawMessage(`{}`), // end_segment returns {} (ignored)
			mustJSON(ckptResp),    // checkpoint returns CheckpointResponse
		},
	}
	w := NewSessionWriter(NewWithCaller(mc))
	w.convID = "conv-1"
	w.segmentID = "seg-1"

	resp, err := w.CheckpointOnExhaustion(context.Background(), "context-limit-failover")
	if err != nil {
		t.Fatalf("CheckpointOnExhaustion: %v", err)
	}
	if resp.CheckpointID != "ckpt-7" {
		t.Errorf("expected ckpt-7, got %q", resp.CheckpointID)
	}

	want := []string{
		"mempalace_conversation_end_segment",
		"mempalace_conversation_checkpoint",
	}
	if got := mc.toolNames(); !stringsEqual(got, want) {
		t.Errorf("tool call order: got %v, want %v", got, want)
	}

	// Verify end_segment uses status "exhausted"
	endCall := mc.calls[0]
	if endCall.args["status"] != "exhausted" {
		t.Errorf("expected status exhausted, got %v", endCall.args["status"])
	}

	// Verify checkpoint reason
	ckptCall := mc.calls[1]
	if ckptCall.args["reason"] != "context-limit-failover" {
		t.Errorf("expected reason context-limit-failover, got %v", ckptCall.args["reason"])
	}
}

// --- Finish ---

func TestSessionWriter_Finish_CallsConversationEnd(t *testing.T) {
	t.Parallel()

	mc := &multiResultCaller{results: []json.RawMessage{json.RawMessage(`{}`)}}
	w := NewSessionWriter(NewWithCaller(mc))
	w.convID = "conv-1"

	if err := w.Finish(context.Background(), "done", "all steps completed"); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	call := mc.calls[0]
	if call.toolName != "mempalace_conversation_end" {
		t.Errorf("expected mempalace_conversation_end, got %q", call.toolName)
	}
	if call.args["status"] != "done" {
		t.Errorf("expected status done, got %v", call.args["status"])
	}
}

// --- Full write path ordering ---

// TestSessionWriter_FullWritePath_OrderIsPreserved is the primary ordering
// proof: Begin → StartSegment → AppendTurn(user) → AppendTurn(assistant) →
// EndSegment → CheckpointOnExhaustion → Finish.
//
// No events or checkpoints are lost: every expected MCP tool call appears
// exactly once in the correct sequence.
func TestSessionWriter_FullWritePath_OrderIsPreserved(t *testing.T) {
	t.Parallel()

	startResp := StartResponse{ConversationID: "conv-full", Status: "active", CreatedAt: time.Now()}
	segResp := StartSegmentResponse{SegmentID: "seg-1", StartedAt: time.Now()}
	ckptResp := CheckpointResponse{CheckpointID: "ckpt-end", TakenAt: time.Now()}

	mc := &multiResultCaller{
		results: []json.RawMessage{
			mustJSON(startResp),   // 0: conversation_start
			json.RawMessage(`{}`), // 1: append_turn (user/prompt)
			mustJSON(segResp),     // 2: start_segment
			json.RawMessage(`{}`), // 3: append_turn (assistant)
			json.RawMessage(`{}`), // 4: end_segment (exhausted)
			mustJSON(ckptResp),    // 5: checkpoint
			json.RawMessage(`{}`), // 6: conversation_end
		},
	}
	w := NewSessionWriter(NewWithCaller(mc))

	ctx := context.Background()

	if err := w.Begin(ctx, "conv-full", "blk-full", "claude", "build it"); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := w.StartSegment(ctx, "claude"); err != nil {
		t.Fatalf("StartSegment: %v", err)
	}
	if err := w.AppendTurn(ctx, conversation.RoleAssistant, "claude", "working on it"); err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	ckpt, err := w.CheckpointOnExhaustion(ctx, "context-limit")
	if err != nil {
		t.Fatalf("CheckpointOnExhaustion: %v", err)
	}
	if ckpt.CheckpointID != "ckpt-end" {
		t.Errorf("expected ckpt-end, got %q", ckpt.CheckpointID)
	}
	if err := w.Finish(ctx, "done", "failover checkpoint written"); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	want := []string{
		"mempalace_conversation_start",
		"mempalace_conversation_append_turn", // user turn (Begin)
		"mempalace_conversation_start_segment",
		"mempalace_conversation_append_turn", // assistant turn
		"mempalace_conversation_end_segment", // exhausted (CheckpointOnExhaustion)
		"mempalace_conversation_checkpoint",
		"mempalace_conversation_end",
	}
	if got := mc.toolNames(); !stringsEqual(got, want) {
		t.Errorf("write path order mismatch\n  got:  %v\n  want: %v", got, want)
	}

	// No checkpoint was skipped: verify checkpoint_id in last checkpoint call
	ckptCall := mc.calls[5]
	if ckptCall.args["reason"] != "context-limit" {
		t.Errorf("checkpoint reason: expected context-limit, got %v", ckptCall.args["reason"])
	}
}

// TestSessionWriter_ImplementsWriter verifies the compile-time interface check.
var _ Writer = (*SessionWriter)(nil)
