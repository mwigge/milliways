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
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type minimaxDaemonRoundTripFunc func(*http.Request) (*http.Response, error)

func (f minimaxDaemonRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func withMiniMaxDaemonTransport(t *testing.T, fn func(*http.Request) (*http.Response, error)) {
	t.Helper()
	original := http.DefaultTransport
	http.DefaultTransport = minimaxDaemonRoundTripFunc(fn)
	t.Cleanup(func() {
		http.DefaultTransport = original
	})
}

func minimaxDaemonResponse(status int, body string, headers http.Header) *http.Response {
	if headers == nil {
		headers = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// TestRunMiniMax_NoAPIKey asserts that when MINIMAX_API_KEY is empty the
// runner pushes a structured err event and exits cleanly without crashing.
// Also verifies an error_count tick lands in the metrics observer.
func TestRunMiniMax_NoAPIKey(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "")

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("hello")
	close(in)

	done := make(chan struct{})
	go func() {
		RunMiniMax(context.Background(), in, pusher, obs)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunMiniMax did not return")
	}

	events := pusher.snapshot()
	if len(events) == 0 {
		t.Fatalf("expected events, got 0")
	}
	// Find the err event with code -32005.
	var foundErr bool
	for _, e := range events {
		if e["t"] == "err" {
			if code, ok := e["code"].(int); ok && code == -32005 {
				foundErr = true
				break
			}
			if codeF, ok := e["code"].(float64); ok && int(codeF) == -32005 {
				foundErr = true
				break
			}
		}
	}
	if !foundErr {
		t.Errorf("expected err event with code -32005, got events=%v", events)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDMiniMax); got < 1 {
		t.Errorf("error_count total = %v, want >= 1 for missing API key", got)
	}
}

// TestRunMiniMax_StreamsDeltas stubs the MiniMax API transport, asserts
// the request shape (auth header + JSON body), sends a fake SSE stream back,
// and verifies the runner emits {"t":"data","b64":...} for each delta and a
// terminating {"t":"chunk_end","cost_usd":N} event.
func TestRunMiniMax_StreamsDeltas(t *testing.T) {
	type capturedReq struct {
		auth        string
		contentType string
		body        map[string]any
	}
	captured := make(chan capturedReq, 1)

	withMiniMaxDaemonTransport(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		captured <- capturedReq{
			auth:        r.Header.Get("Authorization"),
			contentType: r.Header.Get("Content-Type"),
			body:        parsed,
		}
		// Two delta chunks then a usage chunk then [DONE].
		fakeSSE := strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
			``,
			`data: {"choices":[{"delta":{"content":" world"}}]}`,
			``,
			`data: {"choices":[{"finish_reason":"stop","delta":{"content":""}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")
		return minimaxDaemonResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
	})

	t.Setenv("MINIMAX_API_KEY", "test-key-123")
	t.Setenv("MINIMAX_API_URL", "http://minimax.test/v1/text/chatcompletion_v2")

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("hi there")
	close(in)

	done := make(chan struct{})
	go func() {
		RunMiniMax(context.Background(), in, pusher, obs)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("RunMiniMax did not return")
	}

	// Verify request shape.
	select {
	case req := <-captured:
		if req.auth != "Bearer test-key-123" {
			t.Errorf("auth header = %q, want Bearer test-key-123", req.auth)
		}
		if !strings.Contains(req.contentType, "application/json") {
			t.Errorf("content-type = %q, want application/json", req.contentType)
		}
		if req.body["stream"] != true {
			t.Errorf("body.stream = %v, want true", req.body["stream"])
		}
		msgs, ok := req.body["messages"].([]any)
		if !ok || len(msgs) == 0 {
			t.Fatalf("body.messages missing or empty: %v", req.body["messages"])
		}
		last, _ := msgs[len(msgs)-1].(map[string]any)
		if last["role"] != "user" || last["content"] != "hi there" {
			t.Errorf("last message = %v, want {role:user content:'hi there'}", last)
		}
	default:
		t.Fatalf("server never received request")
	}

	// Verify pushed events.
	events := pusher.snapshot()
	var dataPayloads []string
	var sawChunkEnd, sawEnd bool
	for _, e := range events {
		switch e["t"] {
		case "data":
			b64, _ := e["b64"].(string)
			raw, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				t.Errorf("data event b64 decode: %v", err)
				continue
			}
			dataPayloads = append(dataPayloads, string(raw))
		case "chunk_end":
			sawChunkEnd = true
		case "end":
			sawEnd = true
		}
	}
	if !sawChunkEnd {
		t.Errorf("expected chunk_end event, got events=%v", events)
	}
	if !sawEnd {
		t.Errorf("expected end event, got events=%v", events)
	}
	joined := strings.Join(dataPayloads, "")
	if !strings.Contains(joined, "Hello") || !strings.Contains(joined, "world") {
		t.Errorf("data payload = %q, expected to contain 'Hello' and 'world'", joined)
	}

	// The faked SSE includes usage{prompt_tokens:3, completion_tokens:2}.
	// Both should land in the observer; cost is computed from the same.
	if got := obs.counterTotal(MetricTokensIn, AgentIDMiniMax); got != 3 {
		t.Errorf("tokens_in total = %v, want 3", got)
	}
	if got := obs.counterTotal(MetricTokensOut, AgentIDMiniMax); got != 2 {
		t.Errorf("tokens_out total = %v, want 2", got)
	}
	// cost_usd is small but non-zero (3 input + 2 output tokens at the
	// configured per-million-token rates).
	if got := obs.counterTotal(MetricCostUSD, AgentIDMiniMax); got <= 0 {
		t.Errorf("cost_usd total = %v, want > 0", got)
	}
	// No error on the happy path.
	if got := obs.counterTotal(MetricErrorCount, AgentIDMiniMax); got != 0 {
		t.Errorf("error_count total = %v, want 0 on happy path", got)
	}
}

// TestRunMiniMax_APIError verifies that a non-200 response surfaces as an err
// event (still pushing end so the protocol stays well-formed).
func TestRunMiniMax_APIError(t *testing.T) {
	withMiniMaxDaemonTransport(t, func(r *http.Request) (*http.Response, error) {
		return minimaxDaemonResponse(http.StatusUnauthorized, `{"error":"invalid api key"}`, nil), nil
	})

	t.Setenv("MINIMAX_API_KEY", "bad")
	t.Setenv("MINIMAX_API_URL", "http://minimax.test/v1/text/chatcompletion_v2")

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("hello")
	close(in)

	done := make(chan struct{})
	go func() {
		RunMiniMax(context.Background(), in, pusher, obs)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("RunMiniMax did not return")
	}

	events := pusher.snapshot()
	var sawErr bool
	for _, e := range events {
		if e["t"] == "err" {
			sawErr = true
			break
		}
	}
	if !sawErr {
		t.Errorf("expected err event for non-200 response, got %v", events)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDMiniMax); got < 1 {
		t.Errorf("error_count total = %v, want >= 1 for non-2xx response", got)
	}
}

func TestRunMiniMax_IncompleteStreamEmitsError(t *testing.T) {
	withMiniMaxDaemonTransport(t, func(r *http.Request) (*http.Response, error) {
		return minimaxDaemonResponse(http.StatusOK,
			"data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n",
			http.Header{"Content-Type": {"text/event-stream"}},
		), nil
	})

	t.Setenv("MINIMAX_API_KEY", "test-key-123")
	t.Setenv("MINIMAX_API_URL", "http://minimax.test/v1/text/chatcompletion_v2")

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("hi there")
	close(in)

	done := make(chan struct{})
	go func() {
		RunMiniMax(context.Background(), in, pusher, obs)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("RunMiniMax did not return")
	}

	events := pusher.snapshot()
	var sawErr, sawChunkEnd bool
	for _, e := range events {
		switch e["t"] {
		case "err":
			if strings.Contains(fmt.Sprint(e["msg"]), "incomplete stream") {
				sawErr = true
			}
		case "chunk_end":
			sawChunkEnd = true
		}
	}
	if !sawErr {
		t.Fatalf("expected incomplete stream error, got events=%v", events)
	}
	if sawChunkEnd {
		t.Fatalf("unexpected chunk_end for incomplete stream: events=%v", events)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDMiniMax); got < 1 {
		t.Errorf("error_count total = %v, want >= 1 for incomplete stream", got)
	}
}
