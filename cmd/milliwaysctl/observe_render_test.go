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

package main

import (
	"strings"
	"testing"
	"time"
)

// TestFormatObservabilityFrame is the table-driven span formatter test.
// It exercises the helper that produces each redraw cycle's bytes,
// independent of the JSON-RPC subscription loop.
func TestFormatObservabilityFrame(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, 4, 27, 12, 34, 56, 0, time.UTC)

	tests := []struct {
		name        string
		spans       []observeRenderSpan
		wantSubs    []string // substrings that MUST appear
		wantNotSubs []string // substrings that MUST NOT appear
	}{
		{
			name:  "empty snapshot",
			spans: nil,
			wantSubs: []string{
				"milliways observability",
				"12:34:56Z",
				"total spans:   0",
				"p50 latency:   0.00ms",
			},
		},
		{
			name: "single ok span",
			spans: []observeRenderSpan{
				{
					Name:       "rpc:ping",
					StartTS:    time.Date(2026, 4, 27, 12, 34, 55, 0, time.UTC),
					DurationMS: 0.04,
					Status:     "ok",
				},
			},
			wantSubs: []string{
				"rpc:ping",
				"0.04ms",
				"total spans:   1",
				"error rate:    0/min",
			},
			wantNotSubs: []string{
				"error rate:    1/min",
			},
		},
		{
			name: "error span surfaces in error rate",
			spans: []observeRenderSpan{
				{Name: "rpc:agent.open", DurationMS: 0.5, Status: "ok",
					StartTS: time.Date(2026, 4, 27, 12, 34, 55, 0, time.UTC)},
				{Name: "rpc:agent.send", DurationMS: 1.2, Status: "error",
					StartTS: time.Date(2026, 4, 27, 12, 34, 56, 0, time.UTC)},
			},
			wantSubs: []string{
				"total spans:   2",
				"error rate:    1/min",
				"rpc:agent.send",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatObservabilityFrame(fixedNow, tt.spans)
			for _, want := range tt.wantSubs {
				if !strings.Contains(got, want) {
					t.Errorf("missing substring %q in:\n%s", want, got)
				}
			}
			for _, dontWant := range tt.wantNotSubs {
				if strings.Contains(got, dontWant) {
					t.Errorf("unexpected substring %q in:\n%s", dontWant, got)
				}
			}
		})
	}
}

// TestFormatObservabilityFrame_EmbedsBarsChart asserts that when the
// snapshot has at least one span, the frame embeds a kitty-graphics
// bars chart (latency p50/p95/p99) after the summary block. Empty
// snapshots fall back to text-only.
func TestFormatObservabilityFrame_EmbedsBarsChart(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, 4, 27, 12, 34, 56, 0, time.UTC)

	// Empty: no escape.
	got := formatObservabilityFrame(fixedNow, nil)
	if strings.Contains(got, "\x1b_G") {
		t.Errorf("empty snapshot should not embed a kitty escape")
	}

	// Populated: escape and label appear.
	spans := []observeRenderSpan{
		{Name: "rpc.ping", DurationMS: 0.5, Status: "ok"},
		{Name: "rpc.ping", DurationMS: 1.5, Status: "ok"},
		{Name: "rpc.ping", DurationMS: 12.0, Status: "ok"},
	}
	got = formatObservabilityFrame(fixedNow, spans)
	if !strings.Contains(got, "\x1b_G") {
		t.Errorf("populated snapshot should embed a kitty escape:\n%s", got)
	}
	if !strings.Contains(got, "latency (top") {
		t.Errorf("expected 'latency (top …)' label in frame:\n%s", got)
	}
}

// TestComputeLatencyBars groups spans by name, computes percentile
// triples, applies the hint mapping (ok < 1ms, warn < 10ms, err ≥ 10ms),
// and returns up to 5 methods worth of bars.
func TestComputeLatencyBars(t *testing.T) {
	t.Parallel()
	spans := []observeRenderSpan{
		// rpc.fast — all under 1ms, so all hints "ok"
		{Name: "rpc.fast", DurationMS: 0.1, Status: "ok"},
		{Name: "rpc.fast", DurationMS: 0.5, Status: "ok"},
		{Name: "rpc.fast", DurationMS: 0.9, Status: "ok"},
		// rpc.slow — well into err territory at p99
		{Name: "rpc.slow", DurationMS: 0.5, Status: "ok"},
		{Name: "rpc.slow", DurationMS: 5.0, Status: "ok"},
		{Name: "rpc.slow", DurationMS: 50.0, Status: "ok"},
	}
	bars := computeLatencyBars(spans, 5)
	if len(bars) != 6 {
		t.Fatalf("len(bars) = %d, want 6 (2 methods * 3 percentiles)", len(bars))
	}
	// rpc.fast comes first (alphabetical), so bars[0..2] are p50/p95/p99
	// of rpc.fast; all hints should be "ok".
	for i := 0; i < 3; i++ {
		if bars[i].Hint != "ok" {
			t.Errorf("rpc.fast[%d] hint = %q, want ok", i, bars[i].Hint)
		}
	}
	// rpc.slow's p99 is 50ms → hint = err.
	if bars[5].Hint != "err" {
		t.Errorf("rpc.slow p99 hint = %q, want err", bars[5].Hint)
	}
}

// TestLatencyHint covers the 1ms / 10ms cutoffs.
func TestLatencyHint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ms   float64
		want string
	}{
		{0.0, "ok"},
		{0.99, "ok"},
		{1.0, "warn"},
		{9.99, "warn"},
		{10.0, "err"},
		{500.0, "err"},
	}
	for _, c := range cases {
		c := c
		t.Run("", func(t *testing.T) {
			t.Parallel()
			got := latencyHint(c.ms)
			if got != c.want {
				t.Errorf("latencyHint(%v) = %q, want %q", c.ms, got, c.want)
			}
		})
	}
}

// TestPercentileNearestRank is the standalone test for the latency
// percentile helper. Nearest-rank is conservative (always returns an
// observed value) which is what we want for a small-N cockpit.
func TestPercentileNearestRank(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		sorted []float64
		p      float64
		want   float64
	}{
		{"empty", nil, 0.5, 0.0},
		{"single", []float64{42.0}, 0.5, 42.0},
		{"p50 of 1..10", []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 0.50, 5.0},
		{"p99 of 1..100", linspace(1, 100), 0.99, 99.0},
		{"p100 clamps to max", []float64{1, 2, 3}, 1.0, 3.0},
		{"p0 clamps to min", []float64{1, 2, 3}, 0.0, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := percentile(tt.sorted, tt.p)
			if got != tt.want {
				t.Errorf("percentile(%v, %v) = %v, want %v", tt.sorted, tt.p, got, tt.want)
			}
		})
	}
}

// linspace returns [start, start+1, …, end] as float64s.
func linspace(start, end int) []float64 {
	out := make([]float64, 0, end-start+1)
	for i := start; i <= end; i++ {
		out = append(out, float64(i))
	}
	return out
}
