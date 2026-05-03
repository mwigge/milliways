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
	"strings"
	"testing"
	"time"
)

func TestRunGemini_InputClose_PushesEnd(t *testing.T) {
	t.Parallel()
	pusher := &fakePusher{}
	in := make(chan []byte)
	done := make(chan struct{})
	go func() {
		RunGemini(context.Background(), in, pusher, nil)
		close(done)
	}()
	close(in)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunGemini did not return after input close")
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

func TestRunGemini_NilStream(t *testing.T) {
	t.Parallel()
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)
	done := make(chan struct{})
	go func() {
		RunGemini(context.Background(), in, nil, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunGemini(nil stream) did not return")
	}
}

func TestRunGemini_NoBinary(t *testing.T) {
	prev := geminiBinary
	geminiBinary = "/no/such/binary/that/should/not/exist"
	defer func() { geminiBinary = prev }()

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)

	done := make(chan struct{})
	go func() {
		RunGemini(context.Background(), in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunGemini did not return")
	}

	events := pusher.snapshot()
	var sawErr bool
	for _, e := range events {
		if e["t"] == "err" {
			if e["agent"] != AgentIDGemini {
				t.Errorf("err agent = %v, want %s", e["agent"], AgentIDGemini)
			}
			if code, _ := e["code"].(int); code != -32015 {
				t.Errorf("err code = %v, want -32015", e["code"])
			}
			sawErr = true
			break
		}
	}
	if !sawErr {
		t.Errorf("expected err event for missing binary, got %v", events)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDGemini); got < 1 {
		t.Errorf("error_count total = %v, want >= 1 for missing binary", got)
	}
}

func TestRunGemini_StreamsStdout(t *testing.T) {
	prev := geminiBinary
	geminiBinary = "/bin/sh"
	defer func() { geminiBinary = prev }()
	prevArgs := geminiArgsBuilder
	geminiArgsBuilder = func(prompt string) []string {
		return []string{"-c", "echo hello world"}
	}
	defer func() { geminiArgsBuilder = prevArgs }()

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("anything")
	close(in)
	done := make(chan struct{})
	go func() {
		RunGemini(context.Background(), in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("RunGemini did not return")
	}

	events := pusher.snapshot()
	var dataPayloads []string
	var seenData, sawChunkEnd, sawEnd bool
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
			seenData = true
		case "chunk_end":
			sawChunkEnd = true
		case "end":
			sawEnd = true
		}
	}
	if !seenData {
		t.Errorf("expected data event from echo output, got %v", events)
	}
	if got := strings.Join(dataPayloads, ""); !strings.Contains(got, "hello world") {
		t.Errorf("stdout payload = %q, want hello world", got)
	}
	if !sawChunkEnd {
		t.Errorf("expected chunk_end event, got %v", events)
	}
	if !sawEnd {
		t.Errorf("expected end event, got %v", events)
	}
}

func TestRunGemini_StderrLimitClassified(t *testing.T) {
	prev := geminiBinary
	geminiBinary = "/bin/sh"
	defer func() { geminiBinary = prev }()
	prevArgs := geminiArgsBuilder
	defer func() { geminiArgsBuilder = prevArgs }()

	cases := []struct {
		name string
		line string
		code int
		msg  string
	}{
		{"quota", "Error: resource_exhausted quota exceeded", -32013, "quota"},
		{"session", "Error: context window exceeded", -32014, "session"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			geminiArgsBuilder = func(prompt string) []string {
				return []string{"-c", "echo '" + c.line + "' >&2; exit 1"}
			}

			pusher := &fakePusher{}
			obs := &mockObserver{}
			runGeminiPrompt(t, context.Background(), []byte("anything"), pusher, obs, 5*time.Second)

			events := pusher.snapshot()
			errEvent := findGeminiEvent(events, "err")
			if errEvent == nil {
				t.Fatalf("expected err event, got %v", events)
			}
			if code, _ := errEvent["code"].(int); code != c.code {
				t.Fatalf("%s err code = %v, want %d; event=%v", c.name, errEvent["code"], c.code, errEvent)
			}
			if msg := errEvent["msg"].(string); !strings.Contains(msg, c.msg) {
				t.Errorf("%s err msg = %q, want %q classification", c.name, msg, c.msg)
			}
			assertGeminiHasChunkEnd(t, events)
			if got := obs.counterTotal(MetricErrorCount, AgentIDGemini); got < 1 {
				t.Errorf("error_count total = %v, want >= 1 for %s", got, c.name)
			}
		})
	}
}

func TestRunGemini_ParentCancelWhileIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pusher := &fakePusher{}
	in := make(chan []byte)
	done := make(chan struct{})
	go func() {
		RunGemini(ctx, in, pusher, nil)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunGemini did not return after idle parent cancellation")
	}
	events := pusher.snapshot()
	if len(events) == 0 {
		t.Fatalf("expected end event, got none")
	}
	if events[len(events)-1]["t"] != "end" {
		t.Fatalf("last event = %v, want end; events=%v", events[len(events)-1], events)
	}
}

func TestRunGemini_ContextCancellationClassified(t *testing.T) {
	prev := geminiBinary
	geminiBinary = "/bin/sh"
	defer func() { geminiBinary = prev }()
	prevArgs := geminiArgsBuilder
	geminiArgsBuilder = func(prompt string) []string {
		return []string{"-c", "echo started; exec sleep 5"}
	}
	defer func() { geminiArgsBuilder = prevArgs }()

	ctx, cancel := context.WithCancel(context.Background())
	pusher := &fakePusher{}
	done := make(chan struct{})
	in := make(chan []byte, 1)
	in <- []byte("anything")
	close(in)
	go func() {
		RunGemini(ctx, in, pusher, &mockObserver{})
		close(done)
	}()

	waitForGeminiEvent(t, pusher, "data", 2*time.Second)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("RunGemini did not return after parent cancellation")
	}

	events := pusher.snapshot()
	errEvent := findGeminiEvent(events, "err")
	if errEvent == nil {
		t.Fatalf("expected cancellation err event, got %v", events)
	}
	if code, _ := errEvent["code"].(int); code != -32008 {
		t.Fatalf("cancel err code = %v, want -32008; event=%v", errEvent["code"], errEvent)
	}
	assertGeminiHasChunkEnd(t, events)
}

func TestRunGemini_TimeoutClassified(t *testing.T) {
	prev := geminiBinary
	geminiBinary = "/bin/sh"
	defer func() { geminiBinary = prev }()
	prevArgs := geminiArgsBuilder
	geminiArgsBuilder = func(prompt string) []string {
		return []string{"-c", "exec sleep 5"}
	}
	defer func() { geminiArgsBuilder = prevArgs }()
	t.Setenv("GEMINI_TIMEOUT", "50ms")

	pusher := &fakePusher{}
	obs := &mockObserver{}
	runGeminiPrompt(t, context.Background(), []byte("anything"), pusher, obs, 3*time.Second)

	events := pusher.snapshot()
	errEvent := findGeminiEvent(events, "err")
	if errEvent == nil {
		t.Fatalf("expected timeout err event, got %v", events)
	}
	if code, _ := errEvent["code"].(int); code != -32009 {
		t.Fatalf("timeout err code = %v, want -32009; event=%v", errEvent["code"], errEvent)
	}
	assertGeminiHasChunkEnd(t, events)
	if got := obs.counterTotal(MetricErrorCount, AgentIDGemini); got < 1 {
		t.Errorf("error_count total = %v, want >= 1 for timeout", got)
	}
}

func TestRunGemini_ExitFailureClassified(t *testing.T) {
	prev := geminiBinary
	geminiBinary = "/bin/sh"
	defer func() { geminiBinary = prev }()
	prevArgs := geminiArgsBuilder
	geminiArgsBuilder = func(prompt string) []string {
		return []string{"-c", "echo 'plain failure' >&2; exit 7"}
	}
	defer func() { geminiArgsBuilder = prevArgs }()

	pusher := &fakePusher{}
	obs := &mockObserver{}
	runGeminiPrompt(t, context.Background(), []byte("anything"), pusher, obs, 5*time.Second)

	events := pusher.snapshot()
	errEvent := findGeminiEvent(events, "err")
	if errEvent == nil {
		t.Fatalf("expected exit err event, got %v", events)
	}
	if code, _ := errEvent["code"].(int); code != -32010 {
		t.Fatalf("exit err code = %v, want -32010; event=%v", errEvent["code"], errEvent)
	}
	msg, _ := errEvent["msg"].(string)
	if !strings.Contains(msg, "code 7") || !strings.Contains(msg, "plain failure") {
		t.Fatalf("exit err msg = %q, want exit code and stderr", msg)
	}
	assertGeminiHasChunkEnd(t, events)
	if got := obs.counterTotal(MetricErrorCount, AgentIDGemini); got < 1 {
		t.Errorf("error_count total = %v, want >= 1 for exit failure", got)
	}
}

func TestGeminiRequestTimeout(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{"default", "", 0},
		{"off", "off", 0},
		{"none", "none", 0},
		{"zero", "0", 0},
		{"bad", "wat", 0},
		{"duration", "150ms", 150 * time.Millisecond},
		{"seconds", "2", 2 * time.Second},
		{"alias", "3", 3 * time.Second},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "alias" {
				t.Setenv("GEMINI_TIMEOUT", "")
				t.Setenv("MILLIWAYS_GEMINI_TIMEOUT", c.raw)
			} else {
				t.Setenv("GEMINI_TIMEOUT", c.raw)
				t.Setenv("MILLIWAYS_GEMINI_TIMEOUT", "")
			}
			if got := geminiRequestTimeout(); got != c.want {
				t.Errorf("geminiRequestTimeout() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestGeminiStderrSignalsLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		lines []string
		want  bool
	}{
		{"resource_exhausted", []string{"Error: resource_exhausted"}, true},
		{"quota", []string{"daily quota exceeded"}, true},
		{"rate limit", []string{"rate limit reached"}, true},
		{"context window", []string{"context window full"}, true},
		{"context_length_exceeded", []string{"error: context_length_exceeded"}, true},
		{"benign", []string{"connecting to gemini api..."}, false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := geminiStderrSignalsLimit(c.lines); got != c.want {
				t.Errorf("geminiStderrSignalsLimit(%v) = %v, want %v", c.lines, got, c.want)
			}
		})
	}
}

func runGeminiPrompt(t *testing.T, ctx context.Context, prompt []byte, pusher *fakePusher, obs *mockObserver, timeout time.Duration) {
	t.Helper()
	in := make(chan []byte, 1)
	in <- prompt
	close(in)
	done := make(chan struct{})
	go func() {
		RunGemini(ctx, in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("RunGemini did not return within %s", timeout)
	}
}

func findGeminiEvent(events []map[string]any, eventType string) map[string]any {
	for _, e := range events {
		if e["t"] == eventType {
			return e
		}
	}
	return nil
}

func waitForGeminiEvent(t *testing.T, pusher *fakePusher, eventType string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if findGeminiEvent(pusher.snapshot(), eventType) != nil {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s event; events=%v", eventType, pusher.snapshot())
		case <-ticker.C:
		}
	}
}

func assertGeminiHasChunkEnd(t *testing.T, events []map[string]any) {
	t.Helper()
	if findGeminiEvent(events, "chunk_end") == nil {
		t.Fatalf("expected chunk_end event, got %v", events)
	}
}
