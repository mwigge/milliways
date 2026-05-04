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

package runners

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/provider"
)

// errClient is a Client stub that always returns an error from Send.
type errClient struct{ err error }

func (c *errClient) Send(_ context.Context, _ []Message, _ []provider.ToolDef) (TurnResult, error) {
	return TurnResult{}, c.err
}

// summariseClient is a Client stub that records the messages it receives and
// returns a fixed summary text.
type summariseClient struct {
	receivedMessages []Message
	summary          string
	err              error
}

func (c *summariseClient) Send(_ context.Context, messages []Message, _ []provider.ToolDef) (TurnResult, error) {
	c.receivedMessages = messages
	if c.err != nil {
		return TurnResult{}, c.err
	}
	return TurnResult{Content: c.summary, FinishReason: FinishStop}, nil
}

// makeConversation builds a slice of n+1 messages: one system message followed
// by n alternating user/assistant messages. Useful for setting up compaction
// scenarios without repetitive inline literals.
func makeConversation(n int) []Message {
	msgs := make([]Message, 0, n+1)
	msgs = append(msgs, Message{Role: RoleSystem, Content: "system prompt"})
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			msgs = append(msgs, Message{Role: RoleUser, Content: fmt.Sprintf("user message %d", i)})
		} else {
			msgs = append(msgs, Message{Role: RoleAssistant, Content: fmt.Sprintf("assistant response %d", i)})
		}
	}
	return msgs
}

// TestCompactionOptions_DefaultThreshold verifies that a zero Threshold in
// CompactionOptions is treated as DefaultCompactionThreshold (0.80).
// We assert this by checking that a conversation with not enough messages
// (nothing in the compactable window) returns didCompact=false, then with
// enough messages it returns didCompact=true — both with Threshold=0.
func TestCompactionOptions_DefaultThreshold(t *testing.T) {
	t.Parallel()

	opts := CompactionOptions{
		CtxTokens: 1000,
		Threshold: 0, // zero → should use DefaultCompactionThreshold
	}
	sc := &summariseClient{summary: "summary"}

	// 11 messages → compactable window [1:7] has 6 items → triggers.
	msgs := makeConversation(10)
	_, didCompact, err := compactMessages(context.Background(), sc, msgs, opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !didCompact {
		t.Error("expected compaction to occur with zero Threshold (should default to 0.80)")
	}
}

func TestCompactMessages_NotTriggered(t *testing.T) {
	t.Parallel()

	opts := CompactionOptions{
		CtxTokens: 1000,
		Threshold: 0.80,
	}
	sc := &summariseClient{summary: "summary"}

	// system + 3 msgs = 4 total; compactable window [1:0] is empty → no compaction.
	msgs := makeConversation(3)
	result, didCompact, err := compactMessages(context.Background(), sc, msgs, opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if didCompact {
		t.Error("expected no compaction with 4 messages (nothing in compactable window)")
	}
	if len(result) != len(msgs) {
		t.Errorf("messages len = %d, want %d (unchanged)", len(result), len(msgs))
	}
}

func TestCompactMessages_TriggeredSummarises(t *testing.T) {
	t.Parallel()

	opts := CompactionOptions{
		CtxTokens: 1000,
		Threshold: 0.80,
	}
	// system + 10 msgs = 11 total; [1:7] = 6 compactable → summarisation fires.
	msgs := makeConversation(10)

	sc := &summariseClient{summary: "context compacted summary"}
	result, didCompact, err := compactMessages(context.Background(), sc, msgs, opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !didCompact {
		t.Fatal("expected compaction to occur with 11 messages")
	}
	// Result must start with system message.
	if result[0].Role != RoleSystem {
		t.Errorf("result[0].Role = %q, want %q", result[0].Role, RoleSystem)
	}
	// There must be a summary message.
	var hasSummary bool
	for _, m := range result {
		if strings.Contains(m.Content, "[Context summary]") {
			hasSummary = true
			break
		}
	}
	if !hasSummary {
		t.Errorf("no [Context summary] message found; messages = %+v", result)
	}
	// The summarise client must have been called.
	if len(sc.receivedMessages) == 0 {
		t.Error("summarise client was not called")
	}
}

func TestCompactMessages_SummarisationFails_FallsBackToDropping(t *testing.T) {
	t.Parallel()

	opts := CompactionOptions{
		CtxTokens: 1000,
		Threshold: 0.80,
	}
	// Conversation with tool results that can be dropped.
	msgs := []Message{
		{Role: RoleSystem, Content: "system"},
		{Role: RoleUser, Content: "question"},
		{Role: RoleAssistant, Content: "thinking", ToolCalls: []ToolCall{{ID: "t1", Name: "echo"}}},
		{Role: RoleTool, ToolCallID: "t1", Content: "big tool output here"},
		{Role: RoleAssistant, Content: "thinking2", ToolCalls: []ToolCall{{ID: "t2", Name: "echo"}}},
		{Role: RoleTool, ToolCallID: "t2", Content: "another big output"},
		{Role: RoleUser, Content: "follow up"},
		{Role: RoleAssistant, Content: "final answer"},
	}

	ec := &errClient{err: errors.New("summarisation unavailable")}
	result, didCompact, err := compactMessages(context.Background(), ec, msgs, opts, nil)
	if err != nil {
		t.Fatalf("unexpected error (fallback should not return an error): %v", err)
	}
	if !didCompact {
		t.Fatal("expected fallback compaction to occur")
	}
	// Tool result messages must have their content replaced with the omitted marker.
	for _, m := range result {
		if m.Role == RoleTool {
			if !strings.Contains(m.Content, "[tool result omitted") {
				t.Errorf("tool message content not redacted: %q", m.Content)
			}
		}
	}
}

func TestCompactMessages_PreservesSystemMessage(t *testing.T) {
	t.Parallel()

	opts := CompactionOptions{
		CtxTokens: 1000,
		Threshold: 0.80,
	}
	const sysContent = "my unique system prompt"
	msgs := makeConversation(10)
	msgs[0] = Message{Role: RoleSystem, Content: sysContent}

	sc := &summariseClient{summary: "summary text"}
	result, _, err := compactMessages(context.Background(), sc, msgs, opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].Role != RoleSystem {
		t.Errorf("result[0].Role = %q, want system", result[0].Role)
	}
	if result[0].Content != sysContent {
		t.Errorf("system message content changed: got %q, want %q", result[0].Content, sysContent)
	}
}

func TestCompactMessages_PreservesRecentMessages(t *testing.T) {
	t.Parallel()

	opts := CompactionOptions{
		CtxTokens: 1000,
		Threshold: 0.80,
	}
	// system + 10 others = 11 total; last 4 of the 10 must survive.
	msgs := make([]Message, 11)
	msgs[0] = Message{Role: RoleSystem, Content: "sys"}
	for i := 1; i <= 10; i++ {
		msgs[i] = Message{Role: RoleUser, Content: fmt.Sprintf("message-%d", i)}
	}

	sc := &summariseClient{summary: "summary"}
	result, didCompact, err := compactMessages(context.Background(), sc, msgs, opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !didCompact {
		t.Fatal("expected compaction to occur")
	}
	// Last 4 messages of the original slice must appear at the end of result.
	last4 := msgs[len(msgs)-4:]
	if len(result) < 4 {
		t.Fatalf("result too short to contain last 4 messages: %+v", result)
	}
	resultTail := result[len(result)-4:]
	for i, want := range last4 {
		if resultTail[i].Content != want.Content {
			t.Errorf("result tail[%d].Content = %q, want %q", i, resultTail[i].Content, want.Content)
		}
	}
}

// TestRunAgenticLoop_CompactionTriggered verifies that when cumulative token
// usage reaches the threshold, compaction fires and the loop continues to
// completion.
//
// The same Client is used for both the main loop and the compaction
// summarisation call, so the stub must include a turn for the summarisation
// request that compaction issues internally.
func TestRunAgenticLoop_CompactionTriggered(t *testing.T) {
	t.Parallel()

	// Turn breakdown:
	//   turns[0] — loop turn 1: tool call, 300 tokens  (cumulative 300, 30%)
	//   turns[1] — loop turn 2: tool call, 300 tokens  (cumulative 600, 60%)
	//   turns[2] — loop turn 3: tool call, 300 tokens  (cumulative 900, 90% → triggers compaction)
	//   turns[3] — compaction summarisation call (fires before turn 4 starts)
	//   turns[4] — loop turn 4: stop
	inner := &stubClient{turns: []TurnResult{
		{Content: "", FinishReason: FinishToolCalls,
			ToolCalls: []ToolCall{{ID: "c1", Name: "echo", Args: `{"text":"a"}`}},
			Usage:     &Usage{TotalTokens: 300}},
		{Content: "", FinishReason: FinishToolCalls,
			ToolCalls: []ToolCall{{ID: "c2", Name: "echo", Args: `{"text":"b"}`}},
			Usage:     &Usage{TotalTokens: 300}},
		{Content: "", FinishReason: FinishToolCalls,
			ToolCalls: []ToolCall{{ID: "c3", Name: "echo", Args: `{"text":"c"}`}},
			Usage:     &Usage{TotalTokens: 300}}, // cumulative: 900 >= 800 (80% of 1000)
		// Compaction summarisation — called by compactMessages, not the main loop counter.
		{Content: "summary of prior context", FinishReason: FinishStop},
		// Loop continues after compaction with the next normal turn.
		{Content: "done", FinishReason: FinishStop,
			Usage: &Usage{TotalTokens: 10}},
	}}

	registry := newRegistryWithEcho()
	messages := []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "go"},
	}

	result, err := RunAgenticLoop(context.Background(), inner, registry, &messages, LoopOptions{
		Compaction: CompactionOptions{CtxTokens: 1000},
	})
	if err != nil {
		t.Fatalf("RunAgenticLoop err = %v", err)
	}
	if result.StoppedAt != StopReasonStop {
		t.Errorf("stopped = %q, want %q", result.StoppedAt, StopReasonStop)
	}
	// Loop turns = 4 (the summarisation call is not counted as a loop turn).
	if result.Turns != 4 {
		t.Errorf("turns = %d, want 4", result.Turns)
	}
}

// TestRunAgenticLoop_CompactionDisabled verifies that CtxTokens=0 disables
// compaction entirely even when cumulative token counts are enormous.
func TestRunAgenticLoop_CompactionDisabled(t *testing.T) {
	t.Parallel()

	inner := &stubClient{turns: []TurnResult{
		{Content: "done", FinishReason: FinishStop,
			Usage: &Usage{TotalTokens: 999999}},
	}}

	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "hi"}}

	result, err := RunAgenticLoop(context.Background(), inner, registry, &messages, LoopOptions{
		Compaction: CompactionOptions{CtxTokens: 0}, // disabled
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.StoppedAt != StopReasonStop {
		t.Errorf("stopped = %q, want stop", result.StoppedAt)
	}
	if result.Turns != 1 {
		t.Errorf("turns = %d, want 1", result.Turns)
	}
}
