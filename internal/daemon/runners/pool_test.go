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
	"testing"
	"time"
)

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
	var sawErr bool
	for _, e := range events {
		if e["t"] == "err" {
			sawErr = true
			break
		}
	}
	if !sawErr {
		t.Errorf("expected err event for missing binary, got %v", events)
	}
	if got := obs.counterTotal(MetricErrorCount, AgentIDPool); got < 1 {
		t.Errorf("error_count total = %v, want >= 1 for missing binary", got)
	}
}

func TestRunPool_StreamsStdout(t *testing.T) {
	prev := poolBinary
	poolBinary = "/bin/sh"
	defer func() { poolBinary = prev }()
	prevArgs := poolArgsBuilder
	poolArgsBuilder = func(prompt string) []string {
		return []string{"-c", "echo poolside output"}
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
	var seenData, sawChunkEnd, sawEnd bool
	for _, e := range events {
		switch e["t"] {
		case "data":
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
	if !sawChunkEnd {
		t.Errorf("expected chunk_end event, got %v", events)
	}
	if !sawEnd {
		t.Errorf("expected end event, got %v", events)
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
