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
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type localRoundTripFunc func(*http.Request) (*http.Response, error)

func (f localRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func withLocalTransport(t *testing.T, fn func(*http.Request) (*http.Response, error)) {
	t.Helper()
	original := http.DefaultTransport
	http.DefaultTransport = localRoundTripFunc(fn)
	t.Cleanup(func() { http.DefaultTransport = original })
}

func localResponse(status int, body string, headers http.Header) *http.Response {
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
	withLocalTransport(t, func(r *http.Request) (*http.Response, error) {
		select {
		case captured <- r:
		default:
		}
		// Minimal happy stream so the runner exits cleanly.
		fakeSSE := strings.Join([]string{
			`data: {"choices":[{"finish_reason":"stop","delta":{"content":"ok"}}]}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")
		return localResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
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

func TestRunLocal_HonorsEndpointAndModelEnv(t *testing.T) {
	captured := make(chan map[string]any, 1)
	var seenURL string
	withLocalTransport(t, func(r *http.Request) (*http.Response, error) {
		seenURL = r.URL.String()
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		select {
		case captured <- parsed:
		default:
		}
		return localResponse(http.StatusOK, "data: [DONE]\n\n", http.Header{"Content-Type": {"text/event-stream"}}), nil
	})

	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "http://example.test/v1")
	t.Setenv("MILLIWAYS_LOCAL_MODEL", "qwen2.5-coder-7b")

	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)
	go RunLocal(context.Background(), in, &fakePusher{}, &mockObserver{})

	select {
	case body := <-captured:
		if got, want := seenURL, "http://example.test/v1/chat/completions"; got != want {
			t.Errorf("URL = %q, want %q", got, want)
		}
		if model, _ := body["model"].(string); model != "qwen2.5-coder-7b" {
			t.Errorf("model = %v, want qwen2.5-coder-7b", body["model"])
		}
		if body["stream"] != true {
			t.Errorf("stream = %v, want true", body["stream"])
		}
		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) == 0 {
			t.Fatalf("messages missing or empty: %v", body["messages"])
		}
		last, _ := msgs[len(msgs)-1].(map[string]any)
		if last["role"] != "user" || last["content"] != "hi" {
			t.Errorf("last message = %v, want {role:user content:hi}", last)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received request")
	}
}

func TestRunLocal_StreamsContentDeltas(t *testing.T) {
	withLocalTransport(t, func(r *http.Request) (*http.Response, error) {
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
		return localResponse(http.StatusOK, fakeSSE, http.Header{"Content-Type": {"text/event-stream"}}), nil
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
	withLocalTransport(t, func(r *http.Request) (*http.Response, error) {
		select {
		case captured <- r.Header.Get("Authorization"):
		default:
		}
		return localResponse(http.StatusOK, "data: [DONE]\n\n", http.Header{"Content-Type": {"text/event-stream"}}), nil
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

func TestRunLocal_ApiErrorPushesErrEvent(t *testing.T) {
	withLocalTransport(t, func(r *http.Request) (*http.Response, error) {
		return localResponse(http.StatusInternalServerError, `{"error":"backend down"}`, nil), nil
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
	var sawErr bool
	for _, e := range pusher.snapshot() {
		if e["t"] == "err" {
			sawErr = true
			break
		}
	}
	if !sawErr {
		t.Errorf("expected err event for non-200 response")
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDLocal); got < 1 {
		t.Errorf("error_count = %v, want >= 1", got)
	}
}
