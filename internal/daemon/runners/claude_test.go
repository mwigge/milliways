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
	"sync"
	"testing"
	"time"
)

// fakePusher captures pushed events for assertions.
type fakePusher struct {
	mu     sync.Mutex
	events []map[string]any
}

func (p *fakePusher) Push(event any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if m, ok := event.(map[string]any); ok {
		p.events = append(p.events, m)
	}
}

func (p *fakePusher) snapshot() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]map[string]any, len(p.events))
	copy(out, p.events)
	return out
}

// mockObserver records every counter/histogram observation. Used across
// every runner test to assert tokens / cost / error metrics flow.
type mockObserver struct {
	mu         sync.Mutex
	counters   []observationRecord
	histograms []observationRecord
}

type observationRecord struct {
	name    string
	agentID string
	value   float64
}

func (m *mockObserver) ObserveCounter(name, agentID string, delta float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters = append(m.counters, observationRecord{name, agentID, delta})
}

func (m *mockObserver) ObserveHistogram(name, agentID string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.histograms = append(m.histograms, observationRecord{name, agentID, value})
}

// counterTotal returns the sum of all observations matching name+agentID.
func (m *mockObserver) counterTotal(name, agentID string) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total float64
	for _, c := range m.counters {
		if c.name == name && c.agentID == agentID {
			total += c.value
		}
	}
	return total
}

// counterCount returns how many separate observations were made for name+agentID.
func (m *mockObserver) counterCount(name, agentID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int
	for _, c := range m.counters {
		if c.name == name && c.agentID == agentID {
			n++
		}
	}
	return n
}

func TestExtractAssistantText_Happy(t *testing.T) {
	t.Parallel()
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"hello world"}]}}`
	got, ok := extractAssistantText(line)
	if !ok {
		t.Fatalf("extractAssistantText: expected ok=true, got false")
	}
	if got != "hello world" {
		t.Errorf("text = %q, want %q", got, "hello world")
	}
}

func TestExtractAssistantText_NotAssistant(t *testing.T) {
	t.Parallel()
	line := `{"type":"system","subtype":"init"}`
	if _, ok := extractAssistantText(line); ok {
		t.Errorf("extractAssistantText: expected ok=false for system event")
	}
}

func TestExtractAssistantText_NotJSON(t *testing.T) {
	t.Parallel()
	if _, ok := extractAssistantText("not json"); ok {
		t.Errorf("extractAssistantText: expected ok=false for non-JSON")
	}
}

func TestExtractAssistantText_EmptyContent(t *testing.T) {
	t.Parallel()
	line := `{"type":"assistant","message":{"content":[]}}`
	if _, ok := extractAssistantText(line); ok {
		t.Errorf("expected ok=false for empty content")
	}
}

func TestExtractResultCost(t *testing.T) {
	t.Parallel()
	line := `{"type":"result","total_cost_usd":0.0123}`
	cost, ok := extractResultCost(line)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if cost != 0.0123 {
		t.Errorf("cost = %v, want 0.0123", cost)
	}
}

func TestExtractResultCost_IsError(t *testing.T) {
	t.Parallel()
	line := `{"type":"result","is_error":true,"total_cost_usd":0.5}`
	if _, ok := extractResultCost(line); ok {
		t.Errorf("expected ok=false when is_error=true")
	}
}

// TestRunClaude_InputClose_PushesEnd verifies that closing the input channel
// triggers a final {"t":"end"} push and Close() on the stream.
func TestRunClaude_InputClose_PushesEnd(t *testing.T) {
	t.Parallel()
	pusher := &fakePusher{}
	in := make(chan []byte)
	done := make(chan struct{})
	go func() {
		RunClaude(context.Background(), in, pusher, nil)
		close(done)
	}()
	close(in)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunClaude did not return after input close")
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

// TestExtractResult_WithUsage covers the happy path: a result event with
// total_cost_usd + usage block surfaces all three numbers.
func TestExtractResult_WithUsage(t *testing.T) {
	t.Parallel()
	line := `{"type":"result","total_cost_usd":0.0123,"usage":{"input_tokens":100,"output_tokens":42}}`
	r, ok := extractResult(line)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if r.costUSD != 0.0123 {
		t.Errorf("costUSD = %v, want 0.0123", r.costUSD)
	}
	if r.inputTokens != 100 {
		t.Errorf("inputTokens = %d, want 100", r.inputTokens)
	}
	if r.outputTokens != 42 {
		t.Errorf("outputTokens = %d, want 42", r.outputTokens)
	}
}

// TestExtractResult_NoUsage covers a result event without a usage block;
// cost still flows but token counts default to zero.
func TestExtractResult_NoUsage(t *testing.T) {
	t.Parallel()
	line := `{"type":"result","total_cost_usd":0.5}`
	r, ok := extractResult(line)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if r.costUSD != 0.5 {
		t.Errorf("costUSD = %v, want 0.5", r.costUSD)
	}
	if r.inputTokens != 0 || r.outputTokens != 0 {
		t.Errorf("expected zero tokens, got in=%d out=%d", r.inputTokens, r.outputTokens)
	}
}

// TestObserveTokens_Helper verifies the package-private observation helper
// pushes counters under the right names and skips zero values.
func TestObserveTokens_Helper(t *testing.T) {
	t.Parallel()
	obs := &mockObserver{}
	observeTokens(obs, AgentIDClaude, 100, 42, 0.0123)
	if got := obs.counterTotal(MetricTokensIn, AgentIDClaude); got != 100 {
		t.Errorf("tokens_in total = %v, want 100", got)
	}
	if got := obs.counterTotal(MetricTokensOut, AgentIDClaude); got != 42 {
		t.Errorf("tokens_out total = %v, want 42", got)
	}
	if got := obs.counterTotal(MetricCostUSD, AgentIDClaude); got != 0.0123 {
		t.Errorf("cost_usd total = %v, want 0.0123", got)
	}
}

// TestObserveTokens_NilObserver should be a no-op (no panic).
func TestObserveTokens_NilObserver(t *testing.T) {
	t.Parallel()
	observeTokens(nil, AgentIDClaude, 1, 2, 0.1)
	observeError(nil, AgentIDClaude)
}

// TestObserveTokens_SkipsZeros verifies that zero-valued counts are not
// recorded (avoids polluting the rollup with no-op observations).
func TestObserveTokens_SkipsZeros(t *testing.T) {
	t.Parallel()
	obs := &mockObserver{}
	observeTokens(obs, AgentIDClaude, 0, 0, 0)
	if n := obs.counterCount(MetricTokensIn, AgentIDClaude); n != 0 {
		t.Errorf("expected no tokens_in observations, got %d", n)
	}
	if n := obs.counterCount(MetricTokensOut, AgentIDClaude); n != 0 {
		t.Errorf("expected no tokens_out observations, got %d", n)
	}
	if n := obs.counterCount(MetricCostUSD, AgentIDClaude); n != 0 {
		t.Errorf("expected no cost_usd observations, got %d", n)
	}
}

// TestRunClaude_NoBinary verifies that when claudeBinary cannot be exec'd
// the runner pushes an error_count tick and an err event without crashing.
// Drives the cmd.Start failure path that flows through observeError.
func TestRunClaude_NoBinary(t *testing.T) {
	prev := claudeBinary
	claudeBinary = "/no/such/binary/that/should/not/exist"
	defer func() { claudeBinary = prev }()

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)

	done := make(chan struct{})
	go func() {
		RunClaude(context.Background(), in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("RunClaude did not return")
	}

	if got := obs.counterTotal(MetricErrorCount, AgentIDClaude); got < 1 {
		t.Errorf("error_count total = %v, want >= 1", got)
	}
	// And no token observations should have happened.
	if n := obs.counterCount(MetricTokensIn, AgentIDClaude); n != 0 {
		t.Errorf("expected no tokens_in observations, got %d", n)
	}
}

// Sanity: assert encodeData wraps text correctly (drives the output shape
// the bridge expects without needing a subprocess).
func TestEncodeData(t *testing.T) {
	t.Parallel()
	got := encodeData("hi")
	if got["t"] != "data" {
		t.Errorf("t = %v, want data", got["t"])
	}
	want := base64.StdEncoding.EncodeToString([]byte("hi"))
	if got["b64"] != want {
		t.Errorf("b64 = %v, want %v", got["b64"], want)
	}
}
