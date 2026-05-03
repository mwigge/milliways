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
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPoolArgsBuilder_DefaultCommand(t *testing.T) {
	got := poolArgsBuilder("hello pool", "/tmp/project")
	want := []string{"exec", "-p", "hello pool", "--unsafe-auto-allow", "--directory", "/tmp/project"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("poolArgsBuilder() = %v, want %v", got, want)
	}

	got = poolArgsBuilder("hello pool", "")
	want = []string{"exec", "-p", "hello pool", "--unsafe-auto-allow"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("poolArgsBuilder(empty dir) = %v, want %v", got, want)
	}
}

func TestPoolRequestTimeout_DefaultAndEnv(t *testing.T) {
	cases := []struct {
		name       string
		timeoutEnv string
		aliasEnv   string
		want       time.Duration
	}{
		{"default", "", "", 0},
		{"duration", "250ms", "", 250 * time.Millisecond},
		{"seconds", "2", "", 2 * time.Second},
		{"off", "off", "", 0},
		{"none", "none", "", 0},
		{"zero", "0", "", 0},
		{"invalid", "garbage", "", 0},
		{"alias", "", "3s", 3 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("POOL_TIMEOUT", c.timeoutEnv)
			t.Setenv("MILLIWAYS_POOL_TIMEOUT", c.aliasEnv)
			if got := poolRequestTimeout(); got != c.want {
				t.Fatalf("poolRequestTimeout() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestRunPool_InputClose_PushesEnd(t *testing.T) {
	t.Parallel()
	pusher := &fakePusher{}
	in := make(chan []byte)
	done := make(chan struct{})
	go func() {
		RunPool(context.Background(), in, pusher, nil)
		close(done)
	}()
	close(in)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunPool did not return after input close")
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

func TestRunPool_NilStream(t *testing.T) {
	t.Parallel()
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)
	done := make(chan struct{})
	go func() {
		RunPool(context.Background(), in, nil, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunPool(nil stream) did not return")
	}
}

func TestRunPool_NoBinary(t *testing.T) {
	clearPoolTimeoutEnv(t)
	prev := poolBinary
	poolBinary = "/no/such/binary/that/should/not/exist"
	defer func() { poolBinary = prev }()

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)

	done := make(chan struct{})
	go func() {
		RunPool(context.Background(), in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunPool did not return")
	}

	events := pusher.snapshot()
	var sawErr, sawChunkEnd bool
	for _, e := range events {
		if e["t"] == "err" {
			sawErr = true
			if e["agent"] != AgentIDPool {
				t.Errorf("err agent = %v, want %q", e["agent"], AgentIDPool)
			}
			if e["code"] != -32015 {
				t.Errorf("start err code = %v, want -32015", e["code"])
			}
		}
		if e["t"] == "chunk_end" {
			sawChunkEnd = true
		}
	}
	if !sawErr {
		t.Errorf("expected err event for missing binary, got %v", events)
	}
	if !sawChunkEnd {
		t.Errorf("expected chunk_end for missing binary, got %v", events)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDPool); got < 1 {
		t.Errorf("error_count total = %v, want >= 1 for missing binary", got)
	}
}

func TestRunPool_StreamsStdout(t *testing.T) {
	clearPoolTimeoutEnv(t)
	prev := poolBinary
	poolBinary = "/bin/sh"
	defer func() { poolBinary = prev }()
	prevArgs := poolArgsBuilder
	poolArgsBuilder = func(prompt, dir string) []string {
		return []string{"-c", "printf 'poolside output\n'"}
	}
	defer func() { poolArgsBuilder = prevArgs }()

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("anything")
	close(in)
	done := make(chan struct{})
	go func() {
		RunPool(context.Background(), in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("RunPool did not return")
	}

	events := pusher.snapshot()
	var data string
	var dataAt, chunkEndAt, endAt = -1, -1, -1
	for _, e := range events {
		switch e["t"] {
		case "data":
			if dataAt < 0 {
				dataAt = eventIndex(events, e)
			}
			data += decodePoolData(t, e)
		case "chunk_end":
			if chunkEndAt < 0 {
				chunkEndAt = eventIndex(events, e)
			}
		case "end":
			if endAt < 0 {
				endAt = eventIndex(events, e)
			}
		}
	}
	if data != "poolside output\n" {
		t.Errorf("expected data event from echo output, got %v", events)
	}
	if chunkEndAt < 0 {
		t.Errorf("expected chunk_end event, got %v", events)
	}
	if endAt < 0 {
		t.Errorf("expected end event, got %v", events)
	}
	if dataAt < 0 || chunkEndAt < dataAt || endAt < chunkEndAt {
		t.Errorf("event order data/chunk_end/end invalid: data=%d chunk_end=%d end=%d events=%v", dataAt, chunkEndAt, endAt, events)
	}
}

func TestRunPool_EmptyPromptPushesChunkEnd(t *testing.T) {
	clearPoolTimeoutEnv(t)
	events := runPoolPrompt(t, context.Background(), []byte("\n"), nil, 2*time.Second)
	if eventIndexByType(events, "chunk_end") < 0 {
		t.Fatalf("expected chunk_end for empty prompt, got %v", events)
	}
	if got := events[len(events)-1]["t"]; got != "end" {
		t.Fatalf("last event = %v, want end; events=%v", got, events)
	}
}

func TestRunPool_TimeoutUsesEnv(t *testing.T) {
	t.Setenv("POOL_TIMEOUT", "50ms")
	t.Setenv("MILLIWAYS_POOL_TIMEOUT", "")
	withPoolCommand(t, "/bin/sleep", func(prompt, dir string) []string {
		return []string{"5"}
	})

	obs := &mockObserver{}
	start := time.Now()
	events := runPoolPrompt(t, context.Background(), []byte("slow"), obs, 3*time.Second)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout dispatch took %v, want under 2s", elapsed)
	}
	errEvent := requirePoolEvent(t, events, "err")
	if errEvent["code"] != -32009 {
		t.Fatalf("timeout err code = %v, want -32009; event=%v", errEvent["code"], errEvent)
	}
	if msg, _ := errEvent["msg"].(string); !strings.Contains(msg, "timeout") {
		t.Fatalf("timeout err msg = %q, want timeout", msg)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDPool); got < 1 {
		t.Fatalf("error_count total = %v, want >= 1", got)
	}
	assertErrBeforeChunkEnd(t, events)
}

func TestRunPool_ParentCancelStopsCommand(t *testing.T) {
	clearPoolTimeoutEnv(t)
	withPoolCommand(t, "/bin/sleep", func(prompt, dir string) []string {
		return []string{"5"}
	})

	ctx, cancel := context.WithCancel(context.Background())
	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("slow")
	close(in)
	done := make(chan struct{})
	go func() {
		RunPool(ctx, in, pusher, obs)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("RunPool did not return after parent context cancellation")
	}

	events := pusher.snapshot()
	errEvent := requirePoolEvent(t, events, "err")
	if errEvent["code"] != -32008 {
		t.Fatalf("cancel err code = %v, want -32008; event=%v", errEvent["code"], errEvent)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDPool); got < 1 {
		t.Fatalf("error_count total = %v, want >= 1", got)
	}
	assertErrBeforeChunkEnd(t, events)
}

func TestRunPool_ClassifiesQuotaSessionAndExitErrors(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		wantCode int
		wantMsg  string
	}{
		{"quota", "echo 'daily quota exceeded' >&2; exit 2", -32013, "quota"},
		{"session", "echo 'context_length_exceeded' >&2; exit 2", -32014, "session limit"},
		{"generic exit", "echo 'backend failed' >&2; exit 7", -32010, "backend failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearPoolTimeoutEnv(t)
			withPoolCommand(t, "/bin/sh", func(prompt, dir string) []string {
				return []string{"-c", tt.script}
			})

			obs := &mockObserver{}
			events := runPoolPrompt(t, context.Background(), []byte("go"), obs, 3*time.Second)
			errEvent := requirePoolEvent(t, events, "err")
			if errEvent["code"] != tt.wantCode {
				t.Fatalf("err code = %v, want %d; event=%v", errEvent["code"], tt.wantCode, errEvent)
			}
			if msg, _ := errEvent["msg"].(string); !strings.Contains(msg, tt.wantMsg) {
				t.Fatalf("err msg = %q, want it to contain %q", msg, tt.wantMsg)
			}
			if got := obs.counterTotal(MetricErrorCount, AgentIDPool); got < 1 {
				t.Fatalf("error_count total = %v, want >= 1", got)
			}
			assertErrBeforeChunkEnd(t, events)
		})
	}
}

func TestPoolStderrSignalsLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		lines []string
		want  bool
	}{
		{"resource_exhausted", []string{"poolside: resource_exhausted"}, true},
		{"quota", []string{"daily quota exceeded"}, true},
		{"rate limit", []string{"rate limit reached for plan"}, true},
		{"context window", []string{"context window full"}, true},
		{"context_length_exceeded", []string{"context_length_exceeded"}, true},
		{"benign", []string{"connecting to pool..."}, false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := poolStderrSignalsLimit(c.lines); got != c.want {
				t.Errorf("poolStderrSignalsLimit(%v) = %v, want %v", c.lines, got, c.want)
			}
		})
	}
}

func clearPoolTimeoutEnv(t *testing.T) {
	t.Helper()
	t.Setenv("POOL_TIMEOUT", "")
	t.Setenv("MILLIWAYS_POOL_TIMEOUT", "")
}

func withPoolCommand(t *testing.T, binary string, args func(prompt, dir string) []string) {
	t.Helper()
	prevBinary := poolBinary
	prevArgs := poolArgsBuilder
	poolBinary = binary
	poolArgsBuilder = args
	t.Cleanup(func() {
		poolBinary = prevBinary
		poolArgsBuilder = prevArgs
	})
}

func runPoolPrompt(t *testing.T, ctx context.Context, prompt []byte, obs MetricsObserver, wait time.Duration) []map[string]any {
	t.Helper()
	pusher := &fakePusher{}
	in := make(chan []byte, 1)
	in <- prompt
	close(in)
	done := make(chan struct{})
	go func() {
		RunPool(ctx, in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(wait):
		t.Fatalf("RunPool did not return within %v", wait)
	}
	return pusher.snapshot()
}

func requirePoolEvent(t *testing.T, events []map[string]any, typ string) map[string]any {
	t.Helper()
	for _, e := range events {
		if e["t"] == typ {
			return e
		}
	}
	t.Fatalf("expected %s event, got %v", typ, events)
	return nil
}

func eventIndexByType(events []map[string]any, typ string) int {
	for i, e := range events {
		if e["t"] == typ {
			return i
		}
	}
	return -1
}

func eventIndex(events []map[string]any, target map[string]any) int {
	for i, e := range events {
		if reflect.DeepEqual(e, target) {
			return i
		}
	}
	return -1
}

func decodePoolData(t *testing.T, event map[string]any) string {
	t.Helper()
	raw, _ := event["b64"].(string)
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode data event %v: %v", event, err)
	}
	return string(data)
}

func assertErrBeforeChunkEnd(t *testing.T, events []map[string]any) {
	t.Helper()
	errAt := eventIndexByType(events, "err")
	chunkEndAt := eventIndexByType(events, "chunk_end")
	if errAt < 0 || chunkEndAt < 0 || errAt > chunkEndAt {
		t.Fatalf("expected err before chunk_end, got err=%d chunk_end=%d events=%v", errAt, chunkEndAt, events)
	}
}
