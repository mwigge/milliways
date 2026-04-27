package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/rpc"
)

// observe-render is the text-only observability cockpit renderer. It
// subscribes to observability.subscribe, formats each {t:"data", spans}
// frame as a single text block with span tail + summary stats, and
// writes it to stdout prefixed by clear-screen + cursor-home so the
// pane updates in place at 1 Hz.
//
// Phase 6 baseline: text only. Charts (sparklines, percentile bars)
// land later when the plotters renderer ships.

// observeRenderSpan mirrors the daemon's observability.Span on the
// wire. We unmarshal into this rather than reuse internal/daemon types
// to keep milliwaysctl out of the daemon's dependency graph.
type observeRenderSpan struct {
	TraceID    string         `json:"trace_id"`
	SpanID     string         `json:"span_id"`
	Name       string         `json:"name"`
	StartTS    time.Time      `json:"start_ts"`
	DurationMS float64        `json:"duration_ms"`
	Status     string         `json:"status"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// observeRenderFrame is the wire shape emitted by observability.subscribe.
type observeRenderFrame struct {
	T     string              `json:"t"`
	Spans []observeRenderSpan `json:"spans"`
}

// observeRender opens an observability.subscribe stream and writes a
// rendered frame to stdout for each event. Returns when the daemon
// closes the stream or stdout fails (e.g. parent pane closed).
func observeRender(socket string) {
	c, err := rpc.Dial(socket)
	if err != nil {
		die("dial %s: %v", socket, err)
	}
	defer c.Close()
	events, cancel, err := c.Subscribe("observability.subscribe", nil)
	if err != nil {
		die("observability.subscribe: %v", err)
	}
	defer cancel()

	// Throttle emission to 1 Hz even if the daemon ever emits faster —
	// the cockpit's frame budget is 1 Hz steady-state.
	const minInterval = 1 * time.Second
	var lastEmit time.Time

	for ev := range events {
		var frame observeRenderFrame
		if err := json.Unmarshal(ev, &frame); err != nil {
			continue
		}
		if frame.T != "data" {
			continue
		}
		now := time.Now()
		if !lastEmit.IsZero() && now.Sub(lastEmit) < minInterval {
			continue
		}
		lastEmit = now
		out := formatObservabilityFrame(now.UTC(), frame.Spans)
		// Clear+home prefix so the pane updates in place.
		if _, err := fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H"+out); err != nil {
			return
		}
	}
}

// observeRenderSummary captures derived stats shown under the span tail.
type observeRenderSummary struct {
	TotalSpans    int
	ErrorRatePerM float64
	P50LatencyMS  float64
	P99LatencyMS  float64
}

// computeObservabilitySummary derives the four headline numbers from a
// span snapshot. Pure function; the input slice is not mutated.
//
// Error rate is errors observed in the snapshot scaled to per-minute
// assuming a 60s window (matches the daemon's rolling window). Latency
// percentiles are simple nearest-rank from a sorted copy.
func computeObservabilitySummary(spans []observeRenderSpan) observeRenderSummary {
	s := observeRenderSummary{TotalSpans: len(spans)}
	if len(spans) == 0 {
		return s
	}
	errs := 0
	durs := make([]float64, 0, len(spans))
	for _, sp := range spans {
		if sp.Status == "error" {
			errs++
		}
		durs = append(durs, sp.DurationMS)
	}
	// 60s window → already per-minute.
	s.ErrorRatePerM = float64(errs)
	sort.Float64s(durs)
	s.P50LatencyMS = percentile(durs, 0.50)
	s.P99LatencyMS = percentile(durs, 0.99)
	return s
}

// percentile returns the value at rank p (0..1) using nearest-rank,
// from a sorted ascending slice. p ≤ 0 returns the min, p ≥ 1 returns
// the max. Empty input → 0.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	// nearest-rank: ceil(p * N) - 1
	idx := int(p*float64(len(sorted))+0.999999) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// formatObservabilityFrame renders the full text block: header,
// span tail (top 20 most recent), summary stats, footer. The wallclock
// is passed in so tests can assert against a fixed value.
func formatObservabilityFrame(now time.Time, spans []observeRenderSpan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "╭── milliways observability ── %s ──\n",
		now.Format("15:04:05Z"))
	fmt.Fprintln(&b, "│ recent spans (last 60s, top 20):")

	// Daemon returns oldest-first; show newest at top of tail.
	tail := spans
	if len(tail) > 20 {
		tail = tail[len(tail)-20:]
	}
	for i := len(tail) - 1; i >= 0; i-- {
		sp := tail[i]
		fmt.Fprintf(&b, "│   %s  %-22s %6.2fms  %s\n",
			sp.StartTS.UTC().Format("15:04:05.000"),
			truncate(sp.Name, 22),
			sp.DurationMS,
			sp.Status,
		)
	}
	fmt.Fprintln(&b, "│")
	sum := computeObservabilitySummary(spans)
	fmt.Fprintln(&b, "│ summary:")
	fmt.Fprintf(&b, "│   total spans:   %d\n", sum.TotalSpans)
	fmt.Fprintf(&b, "│   error rate:    %.0f/min\n", sum.ErrorRatePerM)
	fmt.Fprintf(&b, "│   p50 latency:   %.2fms\n", sum.P50LatencyMS)
	fmt.Fprintf(&b, "│   p99 latency:   %.2fms\n", sum.P99LatencyMS)
	fmt.Fprintln(&b, "╰──")
	return b.String()
}

// truncate clips s to max runes, padding with ellipsis if truncated.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}
