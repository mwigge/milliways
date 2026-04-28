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
	"path/filepath"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/daemon/observability"
)

// newObsTestServer constructs a minimally-wired Server suitable for the
// observability dispatch tests. We avoid NewServer (which binds a UDS,
// opens metrics.db, probes runners) — for these tests we only need the
// span ring, stream registry, and a non-cancelled background context.
func newObsTestServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{
		streams: NewStreamRegistry(),
		spans:   observability.NewRing(100),
	}
	// bgCtx must not be nil — observabilitySubscribeLoop selects on
	// it. We use a per-test context.Background derivative cancelled in
	// Cleanup so the loop exits cleanly.
	s.bgCtx, s.bgCancel = newBackgroundContext()
	t.Cleanup(s.bgCancel)
	return s
}

// observabilitySubscribeFrame is the minimal shape we assert against in
// the dispatch test — the full Span fields are exercised in the ring
// tests, here we only care that frames arrive.
type observabilitySubscribeFrame struct {
	T     string               `json:"t"`
	Spans []observability.Span `json:"spans"`
}

// TestObservabilitySubscribe_PushesFrameWithinTwoSeconds drives the
// dispatch path the same way handle() does — JSON-RPC unary call writes
// a stream_id, then the background loop pushes frames over the Stream's
// internal channel/ring. We attach by reading the ring directly to
// avoid plumbing a real UDS sidecar.
func TestObservabilitySubscribe_PushesFrameWithinTwoSeconds(t *testing.T) {
	t.Parallel()
	prev := observabilitySubscribeTickInterval
	observabilitySubscribeTickInterval = 100 * time.Millisecond
	t.Cleanup(func() { observabilitySubscribeTickInterval = prev })

	s := newObsTestServer(t)
	// Seed a span so the first frame has content.
	s.spans.Push(observability.Span{
		TraceID:    "deadbeefcafebabedeadbeefcafebabe",
		SpanID:     "deadbeefcafebabe",
		Name:       "rpc:ping",
		StartTS:    time.Now(),
		DurationMS: 0.5,
		Status:     "ok",
	})

	req := &Request{
		Method: "observability.subscribe",
		Params: json.RawMessage(`{}`),
		ID:     json.RawMessage(`1`),
	}
	enc, captured := newCapturingEncoder()
	s.observabilitySubscribe(enc, req)

	// The unary write must include a stream_id.
	var resp struct {
		Result struct {
			StreamID int64 `json:"stream_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(captured.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal subscribe result: %v (raw=%s)", err, captured.String())
	}
	if resp.Result.StreamID == 0 {
		t.Fatalf("expected non-zero stream_id, got %d", resp.Result.StreamID)
	}

	// Wait up to 2s for at least one frame to land in the stream's
	// ring. The frame is pushed by the loop goroutine started inside
	// observabilitySubscribe.
	stream, ok := s.streams.Get(resp.Result.StreamID)
	if !ok {
		t.Fatalf("stream %d not registered", resp.Result.StreamID)
	}
	deadline := time.Now().Add(2 * time.Second)
	var frame observabilitySubscribeFrame
	for time.Now().Before(deadline) {
		stream.mu.Lock()
		ringCopy := append([]byte(nil), stream.ring...)
		stream.mu.Unlock()
		if len(ringCopy) > 0 {
			// Take the first NDJSON line.
			for i, b := range ringCopy {
				if b == '\n' {
					if err := json.Unmarshal(ringCopy[:i], &frame); err != nil {
						t.Fatalf("decode frame: %v (raw=%q)", err, string(ringCopy[:i]))
					}
					break
				}
			}
		}
		if frame.T != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if frame.T != "data" {
		t.Fatalf("no frame within 2s; got t=%q spans=%d", frame.T, len(frame.Spans))
	}
	if len(frame.Spans) == 0 {
		t.Fatalf("frame had zero spans, expected at least the seeded one")
	}
	if frame.Spans[0].Name != "rpc:ping" {
		t.Errorf("first span name = %q, want rpc:ping", frame.Spans[0].Name)
	}
}

// TestObservabilityMetrics_ShapeWithoutStore covers the graceful-fallback
// path: when the metrics store is nil (tests without state dir), each
// metric returns the empty-result sentinel rather than erroring.
func TestObservabilityMetrics_ShapeWithoutStore(t *testing.T) {
	t.Parallel()
	s := newObsTestServer(t) // metrics = nil

	req := &Request{
		Method: "observability.metrics",
		Params: json.RawMessage(`{"tier":"raw"}`),
		ID:     json.RawMessage(`2`),
	}
	enc, captured := newCapturingEncoder()
	s.observabilityMetrics(enc, req)

	var resp struct {
		Result map[string]struct {
			Metric  string           `json:"metric"`
			Tier    string           `json:"tier"`
			Buckets []map[string]any `json:"buckets"`
		} `json:"result"`
	}
	if err := json.Unmarshal(captured.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal metrics result: %v (raw=%s)", err, captured.String())
	}
	for _, want := range observabilityCoreMetrics {
		entry, ok := resp.Result[want]
		if !ok {
			t.Errorf("metrics result missing %q", want)
			continue
		}
		if entry.Metric != want {
			t.Errorf("entry[%s].metric = %q, want %q", want, entry.Metric, want)
		}
		if entry.Tier != "raw" {
			t.Errorf("entry[%s].tier = %q, want raw", want, entry.Tier)
		}
		if entry.Buckets == nil {
			t.Errorf("entry[%s].buckets is nil; expected []", want)
		}
	}
}

// TestObservabilityMetrics_WithRealStoreReturnsBuckets verifies that
// when a metrics store IS wired, observability.metrics surfaces the same
// shape as metrics.rollup.get for each of the four core metrics.
func TestObservabilityMetrics_WithRealStoreReturnsBuckets(t *testing.T) {
	t.Parallel()
	s := newObsTestServer(t)
	dir := t.TempDir()
	mstore, err := openTestMetricsStore(filepath.Join(dir, "metrics.db"))
	if err != nil {
		t.Fatalf("open metrics: %v", err)
	}
	t.Cleanup(func() { _ = mstore.Close() })
	registerCoreMetrics(mstore)
	s.metrics = mstore

	req := &Request{
		Method: "observability.metrics",
		Params: json.RawMessage(`{"tier":"raw"}`),
		ID:     json.RawMessage(`3`),
	}
	enc, captured := newCapturingEncoder()
	s.observabilityMetrics(enc, req)

	var resp struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(captured.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, captured.String())
	}
	for _, want := range observabilityCoreMetrics {
		if _, ok := resp.Result[want]; !ok {
			t.Errorf("metrics result missing %q", want)
		}
	}
}
