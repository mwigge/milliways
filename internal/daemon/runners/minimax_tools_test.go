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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/provider"
	"github.com/mwigge/milliways/internal/tools"
)

// TestRunMiniMax_IncludesSystemPromptAndTools — the daemon's minimax payload
// should now include a system message at index 0 plus a `tools` array derived
// from the configured registry.
func TestRunMiniMax_IncludesSystemPromptAndTools(t *testing.T) {
	captured := make(chan map[string]any, 1)

	withMiniMaxDaemonTransport(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		select {
		case captured <- parsed:
		default:
		}
		// Empty terminating SSE so the loop exits cleanly.
		fakeSSE := strings.Join([]string{
			`data: {"choices":[{"finish_reason":"stop","delta":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")
		return minimaxDaemonResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
	})

	t.Setenv("MINIMAX_API_KEY", "test-key")
	t.Setenv("MINIMAX_API_URL", "http://minimax.test/v1/chat/completions")
	withMinimaxToolRegistry(t, tools.NewBuiltInRegistry())

	in := make(chan []byte, 1)
	in <- []byte("do something")
	close(in)
	pusher := &fakePusher{}
	done := make(chan struct{})
	go func() {
		RunMiniMax(context.Background(), in, pusher, &mockObserver{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("RunMiniMax did not return")
	}

	select {
	case body := <-captured:
		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) < 2 {
			t.Fatalf("messages = %v, want at least 2 entries (system + user)", body["messages"])
		}
		first, _ := msgs[0].(map[string]any)
		if first["role"] != "system" {
			t.Errorf("first message role = %v, want \"system\"", first["role"])
		}
		if s, _ := first["content"].(string); s == "" {
			t.Errorf("first message content empty; want a system prompt")
		}
		toolsAny, ok := body["tools"].([]any)
		if !ok || len(toolsAny) == 0 {
			t.Fatalf("body.tools missing or empty: %v", body["tools"])
		}
		// Each entry should be {type:"function", function:{name,description,parameters}}.
		first0, _ := toolsAny[0].(map[string]any)
		if first0["type"] != "function" {
			t.Errorf("tool[0].type = %v, want \"function\"", first0["type"])
		}
		fn, _ := first0["function"].(map[string]any)
		if _, ok := fn["name"].(string); !ok {
			t.Errorf("tool[0].function.name missing or wrong type: %v", fn)
		}
	default:
		t.Fatalf("server never received request")
	}
}

// TestRunMiniMax_ToolsDisabledByEnv — when MINIMAX_TOOLS=off the payload omits
// the tools array entirely so the model behaves as a plain chat backend.
func TestRunMiniMax_ToolsDisabledByEnv(t *testing.T) {
	captured := make(chan map[string]any, 1)
	withMiniMaxDaemonTransport(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		select {
		case captured <- parsed:
		default:
		}
		fakeSSE := strings.Join([]string{
			`data: {"choices":[{"finish_reason":"stop","delta":{"content":"ok"}}]}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")
		return minimaxDaemonResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
	})

	t.Setenv("MINIMAX_API_KEY", "k")
	t.Setenv("MINIMAX_API_URL", "http://minimax.test/v1/chat/completions")
	t.Setenv("MINIMAX_TOOLS", "off")
	withMinimaxToolRegistry(t, tools.NewBuiltInRegistry())

	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)
	go RunMiniMax(context.Background(), in, &fakePusher{}, &mockObserver{})

	select {
	case body := <-captured:
		if _, ok := body["tools"]; ok {
			t.Errorf("payload contains tools field even though MINIMAX_TOOLS=off: %v", body["tools"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received request")
	}
}

// TestRunMiniMax_AgenticToolLoop — when the model returns tool_calls the
// daemon executes them via the registry, appends results to the conversation,
// and re-requests until the model issues finish_reason: stop.
func TestRunMiniMax_AgenticToolLoop(t *testing.T) {
	var turn atomic.Int32
	var lastBody atomic.Value // map[string]any

	withMiniMaxDaemonTransport(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		lastBody.Store(parsed)
		switch turn.Add(1) {
		case 1:
			// First turn: emit a tool call.
			fakeSSE := strings.Join([]string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"text\":"}}]}}]}`,
				``,
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"hello\"}"}}]}}]}`,
				``,
				`data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n")
			return minimaxDaemonResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
		default:
			// Subsequent turns: stop with content acknowledging the tool result.
			fakeSSE := strings.Join([]string{
				`data: {"choices":[{"finish_reason":"stop","delta":{"content":"got hello"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n")
			return minimaxDaemonResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
		}
	})

	// Custom registry with one "echo" tool we can verify ran.
	var echoRan atomic.Bool
	reg := tools.NewRegistry()
	reg.Register("echo", func(_ context.Context, args map[string]any) (string, error) {
		echoRan.Store(true)
		if v, ok := args["text"].(string); ok {
			return v, nil
		}
		return "", nil
	}, provider.ToolDef{Name: "echo", Description: "echo a string", InputSchema: map[string]any{
		"type":       "object",
		"properties": map[string]any{"text": map[string]any{"type": "string"}},
		"required":   []string{"text"},
	}})
	withMinimaxToolRegistry(t, reg)

	t.Setenv("MINIMAX_API_KEY", "k")
	t.Setenv("MINIMAX_API_URL", "http://minimax.test/v1/chat/completions")

	in := make(chan []byte, 1)
	in <- []byte("call echo")
	close(in)
	pusher := &fakePusher{}
	done := make(chan struct{})
	go func() {
		RunMiniMax(context.Background(), in, pusher, &mockObserver{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunMiniMax did not return")
	}

	if !echoRan.Load() {
		t.Errorf("echo tool was never invoked")
	}
	if got := turn.Load(); got != 2 {
		t.Errorf("API turns = %d, want 2 (initial + post-tool)", got)
	}
	// The second-turn body must include the assistant tool-call message AND a
	// tool-result message before the next assistant turn.
	body, _ := lastBody.Load().(map[string]any)
	msgs, _ := body["messages"].([]any)
	if len(msgs) < 4 {
		t.Fatalf("second turn messages = %d, want >= 4 (system + user + assistant(tool_calls) + tool); got %v", len(msgs), msgs)
	}
	var sawAssistantTool, sawToolResult bool
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "assistant" {
			if _, ok := mm["tool_calls"]; ok {
				sawAssistantTool = true
			}
		}
		if mm["role"] == "tool" {
			if id, _ := mm["tool_call_id"].(string); id == "call_1" {
				sawToolResult = true
			}
		}
	}
	if !sawAssistantTool {
		t.Errorf("second turn missing assistant message with tool_calls")
	}
	if !sawToolResult {
		t.Errorf("second turn missing tool message with tool_call_id=call_1")
	}

	// The final content "got hello" should have made it to the stream.
	events := pusher.snapshot()
	var seenContent bool
	for _, e := range events {
		if e["t"] == "data" {
			b64, _ := e["b64"].(string)
			raw, _ := base64.StdEncoding.DecodeString(b64)
			if strings.Contains(string(raw), "got hello") {
				seenContent = true
			}
		}
	}
	if !seenContent {
		t.Errorf("final assistant content 'got hello' not pushed to stream; events=%v", events)
	}
}

func TestRunMiniMax_ApprovalGatePlansBeforeTools(t *testing.T) {
	var turn atomic.Int32
	var firstBody atomic.Value
	withMiniMaxDaemonTransport(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		if turn.Add(1) == 1 {
			firstBody.Store(parsed)
		}
		fakeSSE := strings.Join([]string{
			`data: {"choices":[{"finish_reason":"stop","delta":{"content":"Plan: edit the target file and test it."}}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")
		return minimaxDaemonResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
	})

	var echoRan atomic.Bool
	reg := tools.NewRegistry()
	reg.Register("echo", func(_ context.Context, _ map[string]any) (string, error) {
		echoRan.Store(true)
		return "should not run", nil
	}, provider.ToolDef{Name: "echo"})
	withMinimaxToolRegistry(t, reg)
	t.Setenv("MINIMAX_API_KEY", "k")
	t.Setenv("MINIMAX_API_URL", "http://minimax.test/v1/chat/completions")

	in := make(chan []byte, 1)
	in <- []byte("implement the feature")
	close(in)
	pusher := &fakePusher{}
	done := make(chan struct{})
	go func() {
		RunMiniMax(context.Background(), in, pusher, &mockObserver{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunMiniMax did not return")
	}

	if echoRan.Load() {
		t.Fatal("tool ran during planning gate")
	}
	body, _ := firstBody.Load().(map[string]any)
	if _, hasTools := body["tools"]; hasTools {
		t.Fatalf("planning request exposed tools: %v", body)
	}
	var sawPrompt, sawNeedsInput bool
	for _, e := range pusher.snapshot() {
		switch e["t"] {
		case "data":
			b64, _ := e["b64"].(string)
			raw, _ := base64.StdEncoding.DecodeString(b64)
			if strings.Contains(string(raw), "reply `y` to implement") {
				sawPrompt = true
			}
		case "chunk_end":
			if v, _ := e["needs_input"].(bool); v {
				sawNeedsInput = true
			}
		}
	}
	if !sawPrompt || !sawNeedsInput {
		t.Fatalf("approval gate did not clearly block for input; events=%v", pusher.snapshot())
	}
}

func TestRunMiniMax_ApprovalGateRequiresYesBeforeToolExecution(t *testing.T) {
	var turn atomic.Int32
	withMiniMaxDaemonTransport(t, func(r *http.Request) (*http.Response, error) {
		switch turn.Add(1) {
		case 1:
			fakeSSE := strings.Join([]string{
				`data: {"choices":[{"finish_reason":"stop","delta":{"content":"Plan: call echo after approval."}}]}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n")
			return minimaxDaemonResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
		case 2:
			fakeSSE := strings.Join([]string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"approved\"}"}}]}}]}`,
				``,
				`data: {"choices":[{"finish_reason":"tool_calls","delta":{}}]}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n")
			return minimaxDaemonResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
		default:
			fakeSSE := strings.Join([]string{
				`data: {"choices":[{"finish_reason":"stop","delta":{"content":"done"}}]}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n")
			return minimaxDaemonResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
		}
	})

	var echoRan atomic.Bool
	reg := tools.NewRegistry()
	reg.Register("echo", func(_ context.Context, args map[string]any) (string, error) {
		echoRan.Store(true)
		return fmt.Sprint(args["text"]), nil
	}, provider.ToolDef{Name: "echo", Description: "echo a string"})
	withMinimaxToolRegistry(t, reg)
	t.Setenv("MINIMAX_API_KEY", "k")
	t.Setenv("MINIMAX_API_URL", "http://minimax.test/v1/chat/completions")

	in := make(chan []byte, 2)
	in <- []byte("implement the feature")
	in <- []byte("y")
	close(in)
	done := make(chan struct{})
	go func() {
		RunMiniMax(context.Background(), in, &fakePusher{}, &mockObserver{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunMiniMax did not return")
	}
	if !echoRan.Load() {
		t.Fatal("tool did not run after explicit approval")
	}
	if got := turn.Load(); got != 3 {
		t.Fatalf("API turns = %d, want planning + tool + final", got)
	}
}

func TestRunMiniMax_AsksConfirmationStopsBeforeToolExecution(t *testing.T) {
	var turn atomic.Int32

	withMiniMaxDaemonTransport(t, func(r *http.Request) (*http.Response, error) {
		turn.Add(1)
		fakeSSE := strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"This will edit files. Should I proceed?"}}]}`,
			``,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"unsafe\"}"}}]}}]}`,
			``,
			`data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")
		return minimaxDaemonResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
	})

	var echoRan atomic.Bool
	reg := tools.NewRegistry()
	reg.Register("echo", func(_ context.Context, _ map[string]any) (string, error) {
		echoRan.Store(true)
		return "should not run", nil
	}, provider.ToolDef{Name: "echo", Description: "echo a string"})
	withMinimaxToolRegistry(t, reg)

	t.Setenv("MINIMAX_API_KEY", "k")
	t.Setenv("MINIMAX_API_URL", "http://minimax.test/v1/chat/completions")

	in := make(chan []byte, 1)
	in <- []byte("edit files, but ask first")
	close(in)
	pusher := &fakePusher{}
	done := make(chan struct{})
	go func() {
		RunMiniMax(context.Background(), in, pusher, &mockObserver{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunMiniMax did not return")
	}

	if echoRan.Load() {
		t.Fatal("tool ran even though assistant asked for confirmation")
	}
	if got := turn.Load(); got != 1 {
		t.Fatalf("API turns = %d, want 1 before waiting for user confirmation", got)
	}
	events := pusher.snapshot()
	var sawQuestion bool
	var sawNeedsInput bool
	for _, e := range events {
		switch e["t"] {
		case "data":
			b64, _ := e["b64"].(string)
			raw, _ := base64.StdEncoding.DecodeString(b64)
			if strings.Contains(string(raw), "Should I proceed?") {
				sawQuestion = true
			}
		case "chunk_end":
			if needsInput, _ := e["needs_input"].(bool); needsInput {
				sawNeedsInput = true
			}
		}
	}
	if !sawQuestion {
		t.Fatalf("confirmation question was not streamed; events=%v", events)
	}
	if !sawNeedsInput {
		t.Fatalf("chunk_end did not mark needs_input; events=%v", events)
	}
}

// TestRunMiniMax_ToolFailureFoldedAsErrorContent — a failing tool produces an
// "error: …" tool message back to the model so it can recover. The loop must
// not crash the daemon stream.
func TestRunMiniMax_ToolFailureFoldedAsErrorContent(t *testing.T) {
	var turn atomic.Int32
	var secondBody atomic.Value
	withMiniMaxDaemonTransport(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		switch turn.Add(1) {
		case 1:
			fakeSSE := strings.Join([]string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","type":"function","function":{"name":"boom","arguments":"{}"}}]}}]}`,
				``,
				`data: {"choices":[{"finish_reason":"tool_calls","delta":{}}]}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n")
			return minimaxDaemonResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
		default:
			secondBody.Store(parsed)
			fakeSSE := strings.Join([]string{
				`data: {"choices":[{"finish_reason":"stop","delta":{"content":"ok"}}]}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n")
			return minimaxDaemonResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
		}
	})

	reg := tools.NewRegistry()
	reg.Register("boom", func(_ context.Context, _ map[string]any) (string, error) {
		return "", errors.New("kaboom")
	}, provider.ToolDef{Name: "boom"})
	withMinimaxToolRegistry(t, reg)

	t.Setenv("MINIMAX_API_KEY", "k")
	t.Setenv("MINIMAX_API_URL", "http://minimax.test/v1/chat/completions")

	in := make(chan []byte, 1)
	in <- []byte("trigger boom")
	close(in)
	go RunMiniMax(context.Background(), in, &fakePusher{}, &mockObserver{})
	time.Sleep(500 * time.Millisecond)

	body, _ := secondBody.Load().(map[string]any)
	if body == nil {
		t.Fatal("second turn never reached the server")
	}
	msgs, _ := body["messages"].([]any)
	var foundErr bool
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "tool" {
			if c, _ := mm["content"].(string); strings.Contains(c, "error: ") && strings.Contains(c, "kaboom") {
				foundErr = true
			}
		}
	}
	if !foundErr {
		t.Errorf("expected tool message with 'error: ... kaboom' content; got messages=%v", msgs)
	}
}
