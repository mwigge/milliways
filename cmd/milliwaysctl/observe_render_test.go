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
