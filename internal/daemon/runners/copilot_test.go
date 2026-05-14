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
	"os"
	"path/filepath"
	"reflect"
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

func TestBuildCopilotCmdArgs_ModelAndDirectoryFlags(t *testing.T) {
	t.Setenv("COPILOT_MODEL", "gpt-test")

	got := buildCopilotCmdArgs("hello", "/tmp/project")
	want := []string{
		"-p", "hello",
		"--model", "gpt-test",
		"--add-dir", "/tmp/project",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestBuildCopilotCmdArgs_OmitsOptionalFlags(t *testing.T) {
	t.Setenv("COPILOT_MODEL", "")

	got := buildCopilotCmdArgs("hello", "")
	want := []string{"-p", "hello"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestRunCopilot_StreamsStdout(t *testing.T) {
	withCopilotFixture(t, "printf 'hello copilot'\n", nil)

	pusher := &fakePusher{}
	obs := &mockObserver{}
	runCopilotInput(t, context.Background(), []byte("anything"), pusher, obs)

	events := pusher.snapshot()
	var payload strings.Builder
	var sawChunkEnd, sawEnd bool
	for _, e := range events {
		switch e["t"] {
		case "data":
			b64, _ := e["b64"].(string)
			raw, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				t.Fatalf("decode data event: %v", err)
			}
			payload.Write(raw)
		case "chunk_end":
			sawChunkEnd = true
		case "end":
			sawEnd = true
		}
	}
	if payload.String() != "hello copilot" {
		t.Fatalf("payload = %q, want hello copilot; events=%v", payload.String(), events)
	}
	if !sawChunkEnd {
		t.Fatalf("expected chunk_end event, got %v", events)
	}
	if !sawEnd {
		t.Fatalf("expected end event, got %v", events)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDCopilot); got != 0 {
		t.Fatalf("error_count total = %v, want 0", got)
	}
}

func TestRunCopilot_ExitFailureClassified(t *testing.T) {
	withCopilotFixture(t, "echo 'fatal: nope' >&2\nexit 7\n", nil)

	pusher := &fakePusher{}
	obs := &mockObserver{}
	runCopilotInput(t, context.Background(), []byte("anything"), pusher, obs)

	errEvent := findCopilotEvent(pusher.snapshot(), "err")
	if errEvent == nil {
		t.Fatalf("expected err event, got %v", pusher.snapshot())
	}
	if code, _ := errEvent["code"].(int); code != -32010 {
		t.Fatalf("code = %v, want -32010; event=%v", errEvent["code"], errEvent)
	}
	msg, _ := errEvent["msg"].(string)
	if !strings.Contains(msg, "code 7") || !strings.Contains(msg, "fatal: nope") {
		t.Fatalf("msg = %q, want exit code and stderr", msg)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDCopilot); got < 1 {
		t.Fatalf("error_count total = %v, want >= 1", got)
	}
}

func TestRunCopilot_StderrQuotaClassified(t *testing.T) {
	withCopilotFixture(t, "echo 'quota exceeded' >&2\nexit 1\n", nil)

	pusher := &fakePusher{}
	obs := &mockObserver{}
	runCopilotInput(t, context.Background(), []byte("anything"), pusher, obs)

	events := pusher.snapshot()
	errEvent := findCopilotEvent(events, "err")
	if errEvent == nil {
		t.Fatalf("expected err event, got %v", events)
	}
	if code, _ := errEvent["code"].(int); code != -32013 {
		t.Fatalf("code = %v, want -32013; event=%v", errEvent["code"], errEvent)
	}
	msg, _ := errEvent["msg"].(string)
	if !strings.Contains(msg, "quota") {
		t.Fatalf("msg = %q, want quota classification", msg)
	}
	if findCopilotEvent(events, "chunk_end") == nil {
		t.Fatalf("expected chunk_end after stderr classification, got %v", events)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDCopilot); got < 1 {
		t.Fatalf("error_count total = %v, want >= 1", got)
	}
}

func TestRunCopilot_StartFailureClassified(t *testing.T) {
	prev := copilotBinary
	copilotBinary = "/no/such/binary/that/should/not/exist"
	defer func() { copilotBinary = prev }()

	pusher := &fakePusher{}
	obs := &mockObserver{}
	runCopilotInput(t, context.Background(), []byte("anything"), pusher, obs)

	events := pusher.snapshot()
	errEvent := findCopilotEvent(events, "err")
	if errEvent == nil {
		t.Fatalf("expected err event, got %v", events)
	}
	if errEvent["agent"] != AgentIDCopilot {
		t.Fatalf("agent = %v, want %q", errEvent["agent"], AgentIDCopilot)
	}
	if code, _ := errEvent["code"].(int); code != -32010 {
		t.Fatalf("code = %v, want -32010; event=%v", errEvent["code"], errEvent)
	}
	if findCopilotEvent(events, "chunk_end") == nil {
		t.Fatalf("expected chunk_end after start failure, got %v", events)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDCopilot); got < 1 {
		t.Fatalf("error_count total = %v, want >= 1", got)
	}
}

func TestRunCopilot_TimeoutFromEnvClassified(t *testing.T) {
	t.Setenv("COPILOT_TIMEOUT", "20ms")
	withCopilotFixture(t, "exec sleep 5\n", nil)

	pusher := &fakePusher{}
	obs := &mockObserver{}
	runCopilotInput(t, context.Background(), []byte("anything"), pusher, obs)

	events := pusher.snapshot()
	errEvent := findCopilotEvent(events, "err")
	if errEvent == nil {
		t.Fatalf("expected err event, got %v", events)
	}
	if code, _ := errEvent["code"].(int); code != -32009 {
		t.Fatalf("code = %v, want -32009; event=%v", errEvent["code"], errEvent)
	}
	if findCopilotEvent(events, "chunk_end") == nil {
		t.Fatalf("expected chunk_end after timeout, got %v", events)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDCopilot); got < 1 {
		t.Fatalf("error_count total = %v, want >= 1", got)
	}
}

func TestRunCopilot_ParentCancellationEndsSession(t *testing.T) {
	pusher := &fakePusher{}
	in := make(chan []byte)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunCopilot(ctx, in, pusher, nil)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunCopilot did not return after parent cancellation")
	}
	events := pusher.snapshot()
	if len(events) == 0 || events[len(events)-1]["t"] != "end" {
		t.Fatalf("expected terminal end event, got %v", events)
	}
}

func TestRunCopilot_ParentCancellationKillsDispatch(t *testing.T) {
	withCopilotFixture(t, "printf 'started'\nexec sleep 5\n", nil)

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("anything")
	close(in)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunCopilot(ctx, in, pusher, obs)
		close(done)
	}()

	waitForCopilotEvent(t, pusher, "data")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("RunCopilot did not return after parent cancellation")
	}

	events := pusher.snapshot()
	errEvent := findCopilotEvent(events, "err")
	if errEvent == nil {
		t.Fatalf("expected err event, got %v", events)
	}
	if code, _ := errEvent["code"].(int); code != -32008 {
		t.Fatalf("code = %v, want -32008; event=%v", errEvent["code"], errEvent)
	}
	if findCopilotEvent(events, "chunk_end") == nil {
		t.Fatalf("expected chunk_end after cancellation, got %v", events)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDCopilot); got < 1 {
		t.Fatalf("error_count total = %v, want >= 1", got)
	}
}

func TestCopilotRequestTimeoutParsing(t *testing.T) {
	cases := []struct {
		value string
		want  time.Duration
	}{
		{"", 0},
		{"off", 0},
		{"none", 0},
		{"0", 0},
		{"2s", 2 * time.Second},
		{"3", 3 * time.Second},
		{"bad", 0},
		{"-1s", 0},
	}
	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			t.Setenv("COPILOT_TIMEOUT", c.value)
			if got := copilotRequestTimeout(); got != c.want {
				t.Fatalf("timeout = %v, want %v", got, c.want)
			}
		})
	}
}

func TestClassifyCopilotStderr(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		lines    []string
		wantCode int
		wantMsg  string
		wantOK   bool
	}{
		{"quota", []string{"quota exceeded"}, -32013, "quota", true},
		{"rate_limit", []string{"rate limit reached"}, -32013, "quota", true},
		{"session_limit", []string{"session limit reached"}, -32013, "session", true},
		{"context", []string{"context_length_exceeded"}, -32013, "session", true},
		{"timeout", []string{"request timed out"}, -32009, "timeout", true},
		{"benign", []string{"working..."}, 0, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			event, ok := classifyCopilotStderr(c.lines)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v; event=%v", ok, c.wantOK, event)
			}
			if !ok {
				return
			}
			if code, _ := event["code"].(int); code != c.wantCode {
				t.Fatalf("code = %v, want %d; event=%v", event["code"], c.wantCode, event)
			}
			msg, _ := event["msg"].(string)
			if !strings.Contains(msg, c.wantMsg) {
				t.Fatalf("msg = %q, want it to contain %q", msg, c.wantMsg)
			}
		})
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

func withCopilotFixture(t *testing.T, body string, args func(prompt, cwd string) []string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "copilot-fixture")
	script := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	prevBinary := copilotBinary
	prevArgs := copilotArgsBuilder
	copilotBinary = path
	if args == nil {
		copilotArgsBuilder = func(prompt, cwd string) []string {
			return nil
		}
	} else {
		copilotArgsBuilder = args
	}
	t.Cleanup(func() {
		copilotBinary = prevBinary
		copilotArgsBuilder = prevArgs
	})
}

func runCopilotInput(t *testing.T, ctx context.Context, prompt []byte, pusher *fakePusher, obs MetricsObserver) {
	t.Helper()
	in := make(chan []byte, 1)
	in <- prompt
	close(in)
	done := make(chan struct{})
	go func() {
		RunCopilot(ctx, in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("RunCopilot did not return")
	}
}

func findCopilotEvent(events []map[string]any, eventType string) map[string]any {
	for _, event := range events {
		if event["t"] == eventType {
			return event
		}
	}
	return nil
}

func waitForCopilotEvent(t *testing.T, pusher *fakePusher, eventType string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %q event; events=%v", eventType, pusher.snapshot())
		case <-tick.C:
			if findCopilotEvent(pusher.snapshot(), eventType) != nil {
				return
			}
		}
	}
}
