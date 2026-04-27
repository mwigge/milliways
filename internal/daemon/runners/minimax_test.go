package runners

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRunMiniMax_NoAPIKey asserts that when MINIMAX_API_KEY is empty the
// runner pushes a structured err event and exits cleanly without crashing.
func TestRunMiniMax_NoAPIKey(t *testing.T) {
	t.Setenv("MINIMAX_API_KEY", "")

	pusher := &fakePusher{}
	in := make(chan []byte, 1)
	in <- []byte("hello")
	close(in)

	done := make(chan struct{})
	go func() {
		RunMiniMax(context.Background(), in, pusher)
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
}

// TestRunMiniMax_StreamsDeltas stubs the MiniMax API with httptest, asserts
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		captured <- capturedReq{
			auth:        r.Header.Get("Authorization"),
			contentType: r.Header.Get("Content-Type"),
			body:        parsed,
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
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
		_, _ = io.WriteString(w, fakeSSE)
	}))
	defer srv.Close()

	t.Setenv("MINIMAX_API_KEY", "test-key-123")
	t.Setenv("MINIMAX_API_URL", srv.URL)

	pusher := &fakePusher{}
	in := make(chan []byte, 1)
	in <- []byte("hi there")
	close(in)

	done := make(chan struct{})
	go func() {
		RunMiniMax(context.Background(), in, pusher)
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
}

// TestRunMiniMax_APIError verifies that a non-200 response surfaces as an err
// event (still pushing end so the protocol stays well-formed).
func TestRunMiniMax_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid api key"}`)
	}))
	defer srv.Close()

	t.Setenv("MINIMAX_API_KEY", "bad")
	t.Setenv("MINIMAX_API_URL", srv.URL)

	pusher := &fakePusher{}
	in := make(chan []byte, 1)
	in <- []byte("hello")
	close(in)

	done := make(chan struct{})
	go func() {
		RunMiniMax(context.Background(), in, pusher)
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
}
