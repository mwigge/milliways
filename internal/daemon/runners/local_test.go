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
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/provider"
	"github.com/mwigge/milliways/internal/tools"
)

type localStubTransport struct {
	fn func(*http.Request) (*http.Response, error)
}

func (t localStubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.fn(req)
}

func stubLocalTransport(t *testing.T, fn func(*http.Request) (*http.Response, error)) {
	t.Helper()
	withLocalHTTPClient(t, &http.Client{
		Transport: localStubTransport{fn: fn},
	})
}

func localStubResponse(status int, body string, headers http.Header) *http.Response {
	if headers == nil {
		headers = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestRunLocal_DefaultsTargetMilliwaysEndpoint(t *testing.T) {
	captured := make(chan *http.Request, 1)
	stubLocalTransport(t, func(r *http.Request) (*http.Response, error) {
		select {
		case captured <- r:
		default:
		}
		return localStubResponse(http.StatusOK,
			"data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n",
			http.Header{"Content-Type": {"text/event-stream"}}), nil
	})

	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "")
	t.Setenv("MILLIWAYS_LOCAL_MODEL", "")

	in := make(chan []byte, 1)
	in <- []byte("hello")
	close(in)
	go RunLocal(context.Background(), in, &fakePusher{}, &mockObserver{})

	select {
	case req := <-captured:
		if got, want := req.URL.String(), "http://localhost:8765/v1/chat/completions"; got != want {
			t.Errorf("URL = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received request")
	}
}

func TestRunLocal_PayloadIncludesSystemPromptAndToolsByDefault(t *testing.T) {
	captured := make(chan map[string]any, 1)
	stubLocalTransport(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		select {
		case captured <- parsed:
		default:
		}
		return localStubResponse(http.StatusOK,
			"data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n",
			http.Header{"Content-Type": {"text/event-stream"}}), nil
	})
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "http://example.test/v1")
	t.Setenv("MILLIWAYS_LOCAL_MODEL", "qwen2.5-coder-7b")
	withLocalToolRegistry(t, tools.NewBuiltInRegistry())

	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)
	go RunLocal(context.Background(), in, &fakePusher{}, &mockObserver{})

	select {
	case body := <-captured:
		if model, _ := body["model"].(string); model != "qwen2.5-coder-7b" {
			t.Errorf("model = %v, want qwen2.5-coder-7b", body["model"])
		}
		if body["stream"] != true {
			t.Errorf("stream = %v, want true", body["stream"])
		}
		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) < 2 {
			t.Fatalf("messages = %v, want at least 2 (system + user)", body["messages"])
		}
		first, _ := msgs[0].(map[string]any)
		if first["role"] != "system" {
			t.Errorf("first message role = %v, want \"system\"", first["role"])
		}
		last, _ := msgs[len(msgs)-1].(map[string]any)
		if last["role"] != "user" || last["content"] != "hi" {
			t.Errorf("last message = %v, want {role:user content:hi}", last)
		}
		toolsAny, ok := body["tools"].([]any)
		if !ok || len(toolsAny) == 0 {
			t.Fatalf("tools missing or empty: %v", body["tools"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received request")
	}
}

func TestRunLocal_ToolsDisabledByEnv(t *testing.T) {
	captured := make(chan map[string]any, 1)
	stubLocalTransport(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		select {
		case captured <- parsed:
		default:
		}
		return localStubResponse(http.StatusOK,
			"data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n",
			http.Header{"Content-Type": {"text/event-stream"}}), nil
	})
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "http://example.test/v1")
	t.Setenv("MILLIWAYS_LOCAL_TOOLS", "off")
	withLocalToolRegistry(t, tools.NewBuiltInRegistry())

	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)
	go RunLocal(context.Background(), in, &fakePusher{}, &mockObserver{})

	select {
	case body := <-captured:
		if _, ok := body["tools"]; ok {
			t.Errorf("tools field present despite MILLIWAYS_LOCAL_TOOLS=off: %v", body["tools"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received request")
	}
}

func TestRunLocal_StreamsContentDeltas(t *testing.T) {
	stubLocalTransport(t, func(r *http.Request) (*http.Response, error) {
		fakeSSE := strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
			``,
			`data: {"choices":[{"delta":{"content":" world"}}]}`,
			``,
			`data: {"choices":[{"finish_reason":"stop","delta":{"content":""}}]}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")
		return localStubResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
	})
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "http://example.test/v1")

	pusher := &fakePusher{}
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)
	done := make(chan struct{})
	go func() {
		RunLocal(context.Background(), in, pusher, &mockObserver{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RunLocal did not return")
	}

	events := pusher.snapshot()
	var dataPayloads []string
	var sawChunkEnd, sawEnd bool
	for _, e := range events {
		switch e["t"] {
		case "data":
			b64, _ := e["b64"].(string)
			raw, _ := base64.StdEncoding.DecodeString(b64)
			dataPayloads = append(dataPayloads, string(raw))
		case "chunk_end":
			sawChunkEnd = true
		case "end":
			sawEnd = true
		}
	}
	if joined := strings.Join(dataPayloads, ""); !strings.Contains(joined, "Hello") || !strings.Contains(joined, "world") {
		t.Errorf("data payloads = %q, want to contain 'Hello' and 'world'", joined)
	}
	if !sawChunkEnd {
		t.Errorf("expected chunk_end, got %v", events)
	}
	if !sawEnd {
		t.Errorf("expected end, got %v", events)
	}
}

func TestRunLocal_HonorsBearerAuthEnv(t *testing.T) {
	captured := make(chan string, 1)
	stubLocalTransport(t, func(r *http.Request) (*http.Response, error) {
		select {
		case captured <- r.Header.Get("Authorization"):
		default:
		}
		return localStubResponse(http.StatusOK,
			"data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n",
			http.Header{"Content-Type": {"text/event-stream"}}), nil
	})
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "http://example.test/v1")
	t.Setenv("MILLIWAYS_LOCAL_API_KEY", "secret-token")

	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)
	go RunLocal(context.Background(), in, &fakePusher{}, nil)

	select {
	case auth := <-captured:
		if want := "Bearer secret-token"; auth != want {
			t.Errorf("Authorization = %q, want %q", auth, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received request")
	}
}

func TestRunLocal_ApiErrorPushesErrAndChunkEnd(t *testing.T) {
	stubLocalTransport(t, func(r *http.Request) (*http.Response, error) {
		return localStubResponse(http.StatusInternalServerError, `{"error":"backend down"}`, nil), nil
	})
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "http://example.test/v1")

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)
	done := make(chan struct{})
	go func() {
		RunLocal(context.Background(), in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RunLocal did not return")
	}
	var sawErr, sawChunkEnd bool
	for _, e := range pusher.snapshot() {
		switch e["t"] {
		case "err":
			sawErr = true
		case "chunk_end":
			sawChunkEnd = true
		}
	}
	if !sawErr {
		t.Errorf("expected err event for non-200")
	}
	if !sawChunkEnd {
		t.Errorf("expected chunk_end (terminal-frame contract) even on err path")
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDLocal); got < 1 {
		t.Errorf("error_count = %v, want >= 1", got)
	}
}

func TestRunLocal_AgenticToolLoop(t *testing.T) {
	var turn atomic.Int32
	var lastBody atomic.Value
	stubLocalTransport(t, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		lastBody.Store(parsed)
		switch turn.Add(1) {
		case 1:
			fakeSSE := strings.Join([]string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"hi\"}"}}]}}]}`,
				``,
				`data: {"choices":[{"finish_reason":"tool_calls","delta":{}}]}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n")
			return localStubResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
		default:
			fakeSSE := strings.Join([]string{
				`data: {"choices":[{"finish_reason":"stop","delta":{"content":"got hi"}}]}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n")
			return localStubResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
		}
	})

	var echoRan atomic.Bool
	reg := tools.NewRegistry()
	reg.Register("echo", func(_ context.Context, args map[string]any) (string, error) {
		echoRan.Store(true)
		if v, ok := args["text"].(string); ok {
			return v, nil
		}
		return "", nil
	}, provider.ToolDef{Name: "echo"})
	withLocalToolRegistry(t, reg)
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "http://example.test/v1")

	in := make(chan []byte, 1)
	in <- []byte("call echo")
	close(in)
	pusher := &fakePusher{}
	done := make(chan struct{})
	go func() {
		RunLocal(context.Background(), in, pusher, &mockObserver{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunLocal did not return")
	}

	if !echoRan.Load() {
		t.Errorf("echo tool was never invoked")
	}
	if got := turn.Load(); got != 2 {
		t.Errorf("API turns = %d, want 2 (initial + post-tool)", got)
	}
}

func TestClassifyDispatchError_DistinguishesCancelTimeoutIntegrity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		err      error
		wantCode int
		wantMsg  string
	}{
		{"cancelled", context.Canceled, -32008, "cancelled"},
		{"timeout", context.DeadlineExceeded, -32009, "timeout"},
		{"incomplete stream", ErrIncompleteStream, -32011, "incomplete stream"},
		{"sse line too large", ErrSSELineTooLarge, -32012, "SSE line"},
		{"generic backend", errors.New("API 500: blew up"), -32010, "API 500"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ev := classifyDispatchError(AgentIDLocal, c.err)
			if got, _ := ev["code"].(int); got != c.wantCode {
				t.Errorf("code = %v, want %d", ev["code"], c.wantCode)
			}
			if msg, _ := ev["msg"].(string); !strings.Contains(msg, c.wantMsg) {
				t.Errorf("msg = %q, want it to contain %q", msg, c.wantMsg)
			}
			if ev["agent"] != AgentIDLocal {
				t.Errorf("agent = %v, want %q", ev["agent"], AgentIDLocal)
			}
		})
	}
}
