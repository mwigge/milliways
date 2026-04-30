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
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/provider"
	"github.com/mwigge/milliways/internal/tools"
)

// stubClient is a Client that returns a queued sequence of TurnResults,
// recording every Send call so tests can inspect the message stream.
type stubClient struct {
	turns []TurnResult
	calls int
	seen  []int // length of messages slice at each call
}

func (c *stubClient) Send(_ context.Context, messages []Message, _ []provider.ToolDef) (TurnResult, error) {
	c.seen = append(c.seen, len(messages))
	if c.calls >= len(c.turns) {
		return TurnResult{}, errors.New("stubClient: out of queued turns")
	}
	t := c.turns[c.calls]
	c.calls++
	return t, nil
}

func newRegistryWithEcho() *tools.Registry {
	r := tools.NewRegistry()
	r.Register("echo", func(_ context.Context, args map[string]any) (string, error) {
		if v, ok := args["text"].(string); ok {
			return v, nil
		}
		return "", nil
	}, provider.ToolDef{Name: "echo"})
	r.Register("boom", func(_ context.Context, _ map[string]any) (string, error) {
		return "", errors.New("kaboom")
	}, provider.ToolDef{Name: "boom"})
	return r
}

func TestRunAgenticLoop_CleanStopExecutesNoTools(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{Content: "all done", FinishReason: FinishStop},
	}}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "hi"}}

	result, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{})
	if err != nil {
		t.Fatalf("RunAgenticLoop err = %v", err)
	}
	if result.Turns != 1 {
		t.Errorf("turns = %d, want 1", result.Turns)
	}
	if result.StoppedAt != StopReasonStop {
		t.Errorf("stopped = %q, want %q", result.StoppedAt, StopReasonStop)
	}
	if result.FinalContent != "all done" {
		t.Errorf("final content = %q, want %q", result.FinalContent, "all done")
	}
	// Only the original user message — no tool messages appended.
	if len(messages) != 2 {
		t.Errorf("messages len = %d, want 2 (user + final assistant)", len(messages))
	}
}

func TestRunAgenticLoop_MultipleToolCallsExecutedInOrder(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls: []ToolCall{
				{ID: "c1", Name: "echo", Args: `{"text":"first"}`},
				{ID: "c2", Name: "echo", Args: `{"text":"second"}`},
			},
			FinishReason: FinishToolCalls,
		},
		{Content: "done", FinishReason: FinishStop},
	}}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "go"}}

	result, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.Turns != 2 {
		t.Errorf("turns = %d, want 2", result.Turns)
	}
	// Expect: user, assistant(toolCalls), tool(c1), tool(c2), assistant(final)
	if len(messages) != 5 {
		t.Fatalf("messages len = %d, want 5; got %+v", len(messages), messages)
	}
	// Tool results are wrapped in <tool_result> markers for prompt-injection
	// hardening; assert the substring rather than exact equality.
	if messages[2].Role != RoleTool || messages[2].ToolCallID != "c1" || !strings.Contains(messages[2].Content, "first") {
		t.Errorf("messages[2] = %+v, want tool/c1 containing 'first'", messages[2])
	}
	if messages[3].Role != RoleTool || messages[3].ToolCallID != "c2" || !strings.Contains(messages[3].Content, "second") {
		t.Errorf("messages[3] = %+v, want tool/c2 containing 'second'", messages[3])
	}
}

func TestRunAgenticLoop_MaxTurnsCap(t *testing.T) {
	t.Parallel()

	// Five queued turns that all request a tool call; cap at 3.
	turns := make([]TurnResult, 5)
	for i := range turns {
		turns[i] = TurnResult{
			ToolCalls:    []ToolCall{{ID: "c", Name: "echo", Args: `{"text":"x"}`}},
			FinishReason: FinishToolCalls,
		}
	}
	client := &stubClient{turns: turns}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "loop"}}

	result, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{MaxTurns: 3})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.StoppedAt != StopReasonMaxTurns {
		t.Errorf("stopped = %q, want %q", result.StoppedAt, StopReasonMaxTurns)
	}
	if result.Turns != 3 {
		t.Errorf("turns = %d, want 3", result.Turns)
	}
	if client.calls != 3 {
		t.Errorf("client calls = %d, want 3", client.calls)
	}
}

func TestRunAgenticLoop_ToolFailureFoldedAsErrorAndLoopContinues(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls:    []ToolCall{{ID: "c1", Name: "boom", Args: `{}`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "recovered", FinishReason: FinishStop},
	}}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "try"}}

	result, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.StoppedAt != StopReasonStop {
		t.Errorf("stopped = %q, want %q", result.StoppedAt, StopReasonStop)
	}
	// Find the tool message and verify it carries the error prefix.
	var foundToolMsg *Message
	for i := range messages {
		if messages[i].Role == RoleTool {
			foundToolMsg = &messages[i]
			break
		}
	}
	if foundToolMsg == nil {
		t.Fatalf("no tool message appended; messages = %+v", messages)
	}
	// Tool results are wrapped in <tool_result> markers; assert that the
	// error appears anywhere in the wrapped payload.
	if !strings.Contains(foundToolMsg.Content, "error: ") {
		t.Errorf("tool content = %q, want it to contain \"error: \"", foundToolMsg.Content)
	}
	if !strings.Contains(foundToolMsg.Content, "kaboom") {
		t.Errorf("tool content = %q, want it to contain underlying error", foundToolMsg.Content)
	}
}

func TestRunAgenticLoop_UnknownToolFoldedAsError(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls:    []ToolCall{{ID: "c1", Name: "nonesuch", Args: `{}`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "ok", FinishReason: FinishStop},
	}}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "go"}}

	_, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var toolMsg *Message
	for i := range messages {
		if messages[i].Role == RoleTool {
			toolMsg = &messages[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatalf("no tool message; messages = %+v", messages)
	}
	if !strings.Contains(toolMsg.Content, "error: ") || !strings.Contains(toolMsg.Content, "not found") {
		t.Errorf("tool content = %q, want error mentioning not found", toolMsg.Content)
	}
}

func TestRunAgenticLoop_MalformedArgsJSONFoldedAsError(t *testing.T) {
	t.Parallel()

	client := &stubClient{turns: []TurnResult{
		{
			ToolCalls:    []ToolCall{{ID: "c1", Name: "echo", Args: `{not valid json`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "ok", FinishReason: FinishStop},
	}}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "go"}}

	_, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var toolMsg *Message
	for i := range messages {
		if messages[i].Role == RoleTool {
			toolMsg = &messages[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatalf("no tool message; messages = %+v", messages)
	}
	if !strings.Contains(toolMsg.Content, "error: ") {
		t.Errorf("tool content = %q, want it to contain error", toolMsg.Content)
	}
}

func TestRunAgenticLoop_DefaultMaxTurnsIsDefaultMaxTurns(t *testing.T) {
	t.Setenv("MILLIWAYS_MAX_TURNS", "") // isolate from host env; can't Parallel with Setenv

	over := DefaultMaxTurns + 5
	turns := make([]TurnResult, over)
	for i := range turns {
		turns[i] = TurnResult{
			ToolCalls:    []ToolCall{{ID: "c", Name: "echo", Args: `{"text":"x"}`}},
			FinishReason: FinishToolCalls,
		}
	}
	client := &stubClient{turns: turns}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "loop"}}

	result, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{}) // MaxTurns left zero
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.StoppedAt != StopReasonMaxTurns {
		t.Errorf("stopped = %q, want %q", result.StoppedAt, StopReasonMaxTurns)
	}
	if result.Turns != DefaultMaxTurns {
		t.Errorf("turns = %d, want %d (DefaultMaxTurns)", result.Turns, DefaultMaxTurns)
	}
}

func TestRunAgenticLoop_MaxTurnsEnvVar(t *testing.T) {
	t.Setenv("MILLIWAYS_MAX_TURNS", "3")

	turns := make([]TurnResult, 10)
	for i := range turns {
		turns[i] = TurnResult{
			ToolCalls:    []ToolCall{{ID: "c", Name: "echo", Args: `{"text":"x"}`}},
			FinishReason: FinishToolCalls,
		}
	}
	client := &stubClient{turns: turns}
	registry := newRegistryWithEcho()
	messages := []Message{{Role: RoleUser, Content: "loop"}}

	result, err := RunAgenticLoop(context.Background(), client, registry, &messages, LoopOptions{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.Turns != 3 {
		t.Errorf("turns = %d, want 3 (from MILLIWAYS_MAX_TURNS=3)", result.Turns)
	}
}
