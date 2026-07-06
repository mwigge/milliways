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

package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/daemon/observability"
)

// TestSecurityScanWorkspaceChanges_EnumeratesRealFiles covers S3: a layered
// scan without a staged diff must enumerate the actual workspace files (so the
// output-gate planner classifies real content) instead of a hardcoded
// representative file per layer.
func TestSecurityScanWorkspaceChanges_EnumeratesRealFiles(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	// A non-git workspace exercises the filesystem-walk fallback.
	mustWrite(t, filepath.Join(ws, "go.mod"), "module example.com/x\n")
	mustWrite(t, filepath.Join(ws, "main.go"), "package main\n")
	mustWrite(t, filepath.Join(ws, ".env.local"), "TOKEN=abc\n")
	// A heavyweight dir must be skipped by the walk.
	mustWrite(t, filepath.Join(ws, "node_modules", "dep", "index.js"), "x\n")

	changes, err := securityScanWorkspaceChanges(ws, nil)
	if err != nil {
		t.Fatalf("securityScanWorkspaceChanges: %v", err)
	}
	got := map[string]bool{}
	for _, c := range changes {
		got[c.Path] = true
		if c.Path == filepath.ToSlash(filepath.Join("node_modules", "dep", "index.js")) {
			t.Errorf("walk should skip node_modules, found %q", c.Path)
		}
	}
	for _, want := range []string{"go.mod", "main.go", ".env.local"} {
		if !got[want] {
			t.Errorf("expected real workspace file %q in enumerated changes", want)
		}
	}
	// The old behavior only ever produced representative files; real
	// enumeration must not invent a path that does not exist on disk.
	for path := range got {
		if _, statErr := os.Stat(filepath.Join(ws, path)); statErr != nil {
			t.Errorf("enumerated path %q does not exist on disk: %v", path, statErr)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestBroadcastStatus_PrunesDisconnectedSubscriber covers S4: a status
// subscriber whose sidecar has dropped (stream closed) must be removed from
// statusSubscribers and never pushed to again.
func TestBroadcastStatus_PrunesDisconnectedSubscriber(t *testing.T) {
	t.Parallel()
	s := &Server{
		streams:           NewStreamRegistry(),
		statusSubscribers: make(map[int64]*Stream),
	}

	stream := s.streams.Allocate()
	s.registerStatusSubscriber(stream)
	if got := len(s.statusSubscribers); got != 1 {
		t.Fatalf("expected 1 subscriber after register, got %d", got)
	}

	// Simulate the sidecar disconnecting: the stream is closed (this is what
	// handleSidecar now does on drain return).
	stream.Close()

	// The 1Hz broadcaster must drop the dead subscriber instead of pushing to
	// it forever.
	s.broadcastStatus()
	if got := len(s.statusSubscribers); got != 0 {
		t.Fatalf("expected disconnected subscriber to be pruned, still have %d", got)
	}

	// And a closed stream is never written to: pushing is a no-op so its ring
	// stays empty.
	stream.Push(map[string]any{"t": "data"})
	stream.mu.Lock()
	ringLen := len(stream.ring)
	stream.mu.Unlock()
	if ringLen != 0 {
		t.Fatalf("closed stream should not accept pushes, ring has %d bytes", ringLen)
	}
}

// TestDeregisterStatusSubscriber covers the explicit delete used by
// handleSidecar on sidecar disconnect (S4).
func TestDeregisterStatusSubscriber(t *testing.T) {
	t.Parallel()
	s := &Server{
		streams:           NewStreamRegistry(),
		statusSubscribers: make(map[int64]*Stream),
	}
	stream := s.streams.Allocate()
	s.registerStatusSubscriber(stream)
	s.deregisterStatusSubscriber(stream.ID)
	if got := len(s.statusSubscribers); got != 0 {
		t.Fatalf("expected 0 subscribers after deregister, got %d", got)
	}
	// Idempotent: deregistering an unknown id is a no-op.
	s.deregisterStatusSubscriber(stream.ID)
}

// TestObservabilitySubscribeLoop_ExitsOnClosedStream covers S5: the loop must
// detect a dead stream and return instead of leaking a goroutine forever.
func TestObservabilitySubscribeLoop_ExitsOnClosedStream(t *testing.T) {
	t.Parallel()
	prev := observabilitySubscribeTickInterval
	observabilitySubscribeTickInterval = 10 * time.Millisecond
	t.Cleanup(func() { observabilitySubscribeTickInterval = prev })

	s := newObsTestServer(t)
	stream := s.streams.Allocate()
	// Close the stream up front, mimicking a sidecar that never attached / has
	// dropped. The loop pushes its first frame (a no-op on a closed stream)
	// then must exit on the first tick via the streamIsClosed guard.
	stream.Close()

	done := make(chan struct{})
	go func() {
		s.observabilitySubscribeLoop(stream, time.Now().Add(-time.Minute), 50)
		close(done)
	}()

	select {
	case <-done:
		// exited as expected
	case <-time.After(2 * time.Second):
		t.Fatal("observabilitySubscribeLoop did not exit after stream closed (goroutine leak)")
	}
}

// TestObservabilitySubscribe_RejectsMalformedParams covers S10: inbound params
// must run through the decoder and a malformed payload must be rejected with
// invalid-params instead of being silently ignored.
func TestObservabilitySubscribe_RejectsMalformedParams(t *testing.T) {
	t.Parallel()
	s := &Server{
		streams: NewStreamRegistry(),
		spans:   observability.NewRing(10),
	}
	s.bgCtx, s.bgCancel = newBackgroundContext()
	t.Cleanup(s.bgCancel)

	req := &Request{
		Method: "observability.subscribe",
		// limit is typed int; a string value is a type error at decode time.
		Params: json.RawMessage(`{"limit":"not-an-int"}`),
		ID:     json.RawMessage(`1`),
	}
	enc, captured := newCapturingEncoder()
	s.observabilitySubscribe(enc, req)

	var resp struct {
		Result *struct {
			StreamID int64 `json:"stream_id"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(captured.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v (raw=%s)", err, captured.String())
	}
	if resp.Error == nil {
		t.Fatalf("expected invalid-params error, got result=%+v", resp.Result)
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Fatalf("error code = %d, want %d (%s)", resp.Error.Code, ErrInvalidParams, resp.Error.Message)
	}
	// A rejected request must not have allocated/leaked a stream.
	if resp.Result != nil && resp.Result.StreamID != 0 {
		t.Fatalf("malformed request should not allocate a stream, got id %d", resp.Result.StreamID)
	}
}
