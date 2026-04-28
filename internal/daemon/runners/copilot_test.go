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
	"io"
	"strings"
	"testing"
	"time"
)

// TestStreamCopilotStdout_PushesChunks verifies plain-text bytes
// from copilot's stdout are wrapped in {"t":"data","b64":...} per
// Read.
func TestStreamCopilotStdout_PushesChunks(t *testing.T) {
	t.Parallel()
	pusher := &fakePusher{}
	r := strings.NewReader("hello copilot")
	streamCopilotStdout(r, pusher)
	events := pusher.snapshot()
	if len(events) == 0 {
		t.Fatalf("expected at least one data event")
	}
	// All events should be data events with valid base64.
	var assembled strings.Builder
	for _, e := range events {
		if e["t"] != "data" {
			t.Errorf("event t = %v, want data", e["t"])
			continue
		}
		b64s, ok := e["b64"].(string)
		if !ok {
			t.Errorf("b64 not string: %T", e["b64"])
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(b64s)
		if err != nil {
			t.Errorf("b64 decode: %v", err)
			continue
		}
		assembled.Write(decoded)
	}
	if assembled.String() != "hello copilot" {
		t.Errorf("assembled = %q, want %q", assembled.String(), "hello copilot")
	}
}

// TestStreamCopilotStdout_EmptyReader yields zero events and returns.
func TestStreamCopilotStdout_EmptyReader(t *testing.T) {
	t.Parallel()
	pusher := &fakePusher{}
	streamCopilotStdout(strings.NewReader(""), pusher)
	if got := len(pusher.snapshot()); got != 0 {
		t.Errorf("events = %d, want 0", got)
	}
}

// TestStreamCopilotStdout_LargePayload spans multiple chunks since
// the read buffer is copilotChunkSize bytes wide.
func TestStreamCopilotStdout_LargePayload(t *testing.T) {
	t.Parallel()
	pusher := &fakePusher{}
	payload := strings.Repeat("x", copilotChunkSize*2+17)
	streamCopilotStdout(strings.NewReader(payload), pusher)
	events := pusher.snapshot()
	if len(events) < 2 {
		t.Fatalf("expected >=2 chunks for payload of %d bytes, got %d", len(payload), len(events))
	}
	var total int
	for _, e := range events {
		b64s, _ := e["b64"].(string)
		decoded, err := base64.StdEncoding.DecodeString(b64s)
		if err != nil {
			t.Errorf("b64 decode: %v", err)
			continue
		}
		total += len(decoded)
	}
	if total != len(payload) {
		t.Errorf("total bytes = %d, want %d", total, len(payload))
	}
}

// TestStreamCopilotStdout_ReaderError stops cleanly on non-EOF errors.
type errReader struct{ msg string }

func (e *errReader) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestStreamCopilotStdout_ReaderError(t *testing.T) {
	t.Parallel()
	pusher := &fakePusher{}
	streamCopilotStdout(&errReader{}, pusher)
	if got := len(pusher.snapshot()); got != 0 {
		t.Errorf("events = %d, want 0 on read error with no bytes", got)
	}
}

// TestRunCopilot_InputClose_PushesEnd verifies that closing the input
// channel triggers a final {"t":"end"} push.
func TestRunCopilot_InputClose_PushesEnd(t *testing.T) {
	t.Parallel()
	pusher := &fakePusher{}
	in := make(chan []byte)
	done := make(chan struct{})
	go func() {
		RunCopilot(context.Background(), in, pusher, nil)
		close(done)
	}()
	close(in)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunCopilot did not return after input close")
	}
	events := pusher.snapshot()
	if len(events) == 0 {
		t.Fatalf("expected at least one event, got 0")
	}
	last := events[len(events)-1]
	if last["t"] != "end" {
		t.Errorf("last event t = %v, want end", last["t"])
	}
}

// TestRunCopilot_NilStream verifies nil-stream guard does not panic.
func TestRunCopilot_NilStream(t *testing.T) {
	t.Parallel()
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)
	done := make(chan struct{})
	go func() {
		RunCopilot(context.Background(), in, nil, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunCopilot(nil stream) did not return")
	}
}

// TestRunCopilot_NoBinary asserts that a missing copilot binary surfaces
// an error_count tick (and no token observations).
func TestRunCopilot_NoBinary(t *testing.T) {
	prev := copilotBinary
	copilotBinary = "/no/such/binary/that/should/not/exist"
	defer func() { copilotBinary = prev }()

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)

	done := make(chan struct{})
	go func() {
		RunCopilot(context.Background(), in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("RunCopilot did not return")
	}

	if got := obs.counterTotal(MetricErrorCount, AgentIDCopilot); got < 1 {
		t.Errorf("error_count total = %v, want >= 1", got)
	}
	if n := obs.counterCount(MetricTokensIn, AgentIDCopilot); n != 0 {
		t.Errorf("expected no tokens_in observations for copilot, got %d", n)
	}
}

// Sanity: encodeData wraps copilot bytes in the data shape.
func TestEncodeData_Copilot(t *testing.T) {
	t.Parallel()
	got := encodeData("copilot hi")
	if got["t"] != "data" {
		t.Errorf("t = %v, want data", got["t"])
	}
	want := base64.StdEncoding.EncodeToString([]byte("copilot hi"))
	if got["b64"] != want {
		t.Errorf("b64 = %v, want %v", got["b64"], want)
	}
}
