package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/daemon/charts"
	"github.com/mwigge/milliways/internal/rpc"
)

// latencyTopN is the maximum number of methods rendered in the bars
// chart. Three percentile bars (p50/p95/p99) per method × 5 methods =
// 15 bars in the 256-pixel-wide canvas which fits with labels.
const latencyTopN = 5

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

// latencyHint maps a latency in milliseconds to a Bar.Hint per the
// thresholds in the change spec: ok < 1ms, warn < 10ms, err ≥ 10ms.
func latencyHint(ms float64) string {
	switch {
	case ms < 1.0:
		return "ok"
	case ms < 10.0:
		return "warn"
	default:
		return "err"
	}
}

// computeLatencyBars groups spans by name, computes p50/p95/p99 per
// group, and returns up to 3*topN bars (3 percentile bars per method,
// alphabetical by method name). Hint is the latency band; label is
// just the percentile so a 12px bar can render "p50".
//
// Pure function: input is not mutated. Empty input returns nil.
func computeLatencyBars(spans []observeRenderSpan, topN int) []charts.Bar {
	if len(spans) == 0 || topN <= 0 {
		return nil
	}
	groups := make(map[string][]float64, 8)
	for _, sp := range spans {
		groups[sp.Name] = append(groups[sp.Name], sp.DurationMS)
	}
	names := make([]string, 0, len(groups))
	for n := range groups {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > topN {
		names = names[:topN]
	}
	out := make([]charts.Bar, 0, len(names)*3)
	for _, n := range names {
		durs := groups[n]
		sort.Float64s(durs)
		for _, p := range []struct {
			rank float64
			lbl  string
		}{
			{0.50, "p50"},
			{0.95, "p95"},
			{0.99, "p99"},
		} {
			v := percentile(durs, p.rank)
			out = append(out, charts.Bar{
				Value: v,
				Hint:  latencyHint(v),
				Label: p.lbl,
			})
		}
	}
	return out
}

// formatObservabilityFrame renders the full text block: header,
// span tail (top 20 most recent), summary stats, latency bars chart,
// footer. The wallclock is passed in so tests can assert against a
// fixed value.
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
	if bars := computeLatencyBars(spans, latencyTopN); len(bars) > 0 {
		fmt.Fprintln(&b, "│")
		fmt.Fprintf(&b, "│ latency (top %d methods, p50/p95/p99):\n", latencyTopN)
		png := charts.Bars(bars, charts.DefaultTheme())
		fmt.Fprintf(&b, "│   %s\n", charts.KittyEscape(png, 0))
	}
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
