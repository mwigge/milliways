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

type observeRenderStatus struct {
	ActiveAgent *string `json:"active_agent"`
	TokensIn    int     `json:"tokens_in"`
	TokensOut   int     `json:"tokens_out"`
	CostUSD     float64 `json:"cost_usd"`
	QuotaPct    float64 `json:"quota_pct"`
}

type observeRenderQuota struct {
	AgentID string  `json:"agent_id"`
	Used    float64 `json:"used"`
	Cap     float64 `json:"cap"`
	Pct     float64 `json:"pct"`
	Window  string  `json:"window,omitempty"`
}

type observeRenderUsage struct {
	Status   observeRenderStatus
	Quotas   []observeRenderQuota
	Security observeRenderSecurity
}

type observeRenderSecurity struct {
	Installed            bool             `json:"installed"`
	Enabled              bool             `json:"enabled"`
	Mode                 string           `json:"mode"`
	Posture              string           `json:"posture"`
	Warnings             int              `json:"warnings"`
	Blocks               int              `json:"blocks"`
	WarningCount         int              `json:"warning_count"`
	BlockCount           int              `json:"block_count"`
	LastStartupScanAt    string           `json:"last_startup_scan_at"`
	LastDependencyScanAt string           `json:"last_dependency_scan_at"`
	LastStartupScan      string           `json:"last_startup_scan"`
	LastDependencyScan   string           `json:"last_dependency_scan"`
	CRA                  observeRenderCRA `json:"cra"`
}

type observeRenderCRA struct {
	EvidenceScore           int    `json:"evidence_score"`
	ChecksTotal             int    `json:"checks_total"`
	ChecksPresent           int    `json:"checks_present"`
	ChecksPartial           int    `json:"checks_partial"`
	ChecksMissing           int    `json:"checks_missing"`
	ReportingReady          bool   `json:"reporting_ready"`
	ReportingPresent        int    `json:"reporting_present"`
	ReportingTotal          int    `json:"reporting_total"`
	DesignEvidenceStatus    string `json:"design_evidence_status"`
	DaysToReporting         int    `json:"days_to_reporting"`
	ReportingDeadline       string `json:"reporting_deadline"`
	ReportingDeadlineStatus string `json:"reporting_deadline_status"`
	FullDeadline            string `json:"full_deadline"`
	NextAction              string `json:"next_action"`
}

// observeRender opens an observability.subscribe stream and writes a
// rendered frame immediately, then refreshes it once per second. Quiet
// daemons may not emit spans for a while; the cockpit still needs to be
// visible as soon as the pane is created.
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

	var spans []observeRenderSpan
	render := func() bool {
		now := time.Now()
		usage := fetchObserveRenderUsage(c)
		out := formatObservabilityFrame(now.UTC(), spans, usage)
		// Clear scrollback + viewport and hide the cursor so the pane
		// behaves like a dashboard, not an interactive prompt.
		if _, err := fmt.Fprint(os.Stdout, "\x1b[?25l\x1b[3J\x1b[2J\x1b[H"+out); err != nil {
			return false
		}
		return true
	}
	if !render() {
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			var frame observeRenderFrame
			if err := json.Unmarshal(ev, &frame); err != nil || frame.T != "data" {
				continue
			}
			spans = frame.Spans
		case <-ticker.C:
			if !render() {
				return
			}
		}
	}
}

func fetchObserveRenderUsage(c *rpc.Client) observeRenderUsage {
	var usage observeRenderUsage
	_ = c.Call("status.get", nil, &usage.Status)
	_ = c.Call("quota.get", nil, &usage.Quotas)
	_ = c.Call("security.status", nil, &usage.Security)
	return usage
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

// formatObservabilityFrame renders a compact lower-left dashboard. Keep the
// vertical footprint short: the pane is intentionally small, and the summary,
// token/cost, time-to-limit, and latency chart are more useful at rest than a
// scrolling span tail.
func formatObservabilityFrame(now time.Time, spans []observeRenderSpan, usage observeRenderUsage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "╭── milliways observability ── %s ──\n",
		now.Format("15:04:05Z"))
	if len(spans) == 0 {
		fmt.Fprintln(&b, "│ latest: no spans")
	} else {
		sp := spans[len(spans)-1]
		fmt.Fprintf(&b, "│ latest: %-22s %6.2fms  %s\n",
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
	fmt.Fprintln(&b, "│")
	fmt.Fprintln(&b, "│ usage:")
	fmt.Fprintf(&b, "│   tokens:        in %s / out %s / total %s (last 5m)\n",
		formatObserveTokenCount(usage.Status.TokensIn),
		formatObserveTokenCount(usage.Status.TokensOut),
		formatObserveTokenCount(usage.Status.TokensIn+usage.Status.TokensOut))
	fmt.Fprintf(&b, "│   cost:          %s (last 5m)\n", formatObserveCost(usage.Status.CostUSD))
	fmt.Fprintf(&b, "│   time to limit: %s\n", formatTimeToLimit(usage.Quotas))
	fmt.Fprintf(&b, "│   security:      %s\n", formatObserveSecurity(usage.Security))
	if cra := formatObserveCRA(usage.Security.CRA); cra != "" {
		fmt.Fprintf(&b, "│   cra:           %s\n", cra)
	}
	if next := strings.TrimSpace(usage.Security.CRA.NextAction); next != "" {
		fmt.Fprintf(&b, "│   cra next:      %s\n", truncateObserveText(next, 84))
	}
	bars := computeLatencyBars(spans, latencyTopN)
	if len(bars) == 0 {
		bars = []charts.Bar{
			{Value: 1, Hint: "dim", Label: "p50"},
			{Value: 1, Hint: "dim", Label: "p95"},
			{Value: 1, Hint: "dim", Label: "p99"},
		}
	}
	fmt.Fprintln(&b, "│")
	fmt.Fprintf(&b, "│ latency (top %d methods, p50/p95/p99):\n", latencyTopN)
	png := charts.Bars(bars, charts.DefaultTheme())
	fmt.Fprintf(&b, "│   %s\n", charts.KittyEscape(png, 0))
	fmt.Fprintln(&b, "╰──")
	return b.String()
}

func formatObserveSecurity(sec observeRenderSecurity) string {
	mode := strings.TrimSpace(sec.Mode)
	if mode == "" {
		mode = "warn"
	}
	posture := strings.ToLower(strings.TrimSpace(sec.Posture))
	warnings := sec.Warnings
	if warnings == 0 {
		warnings = sec.WarningCount
	}
	blocks := sec.Blocks
	if blocks == 0 {
		blocks = sec.BlockCount
	}
	switch {
	case blocks > 0:
		posture = "block"
	case warnings > 0 && posture == "":
		posture = "warn"
	case posture == "":
		posture = "ok"
	}
	label := "SEC OK"
	if posture == "block" {
		label = fmt.Sprintf("SEC BLOCK %d", blocks)
	} else if posture == "warn" {
		label = fmt.Sprintf("SEC WARN %d", warnings)
	}
	if !sec.Installed {
		if posture == "ok" || posture == "" {
			label = "SEC WARN"
		}
		return fmt.Sprintf("%s (mode %s, osv missing)", label, mode)
	}
	return fmt.Sprintf("%s (mode %s)", label, mode)
}

func formatObserveCRA(cra observeRenderCRA) string {
	if cra.ChecksTotal == 0 && cra.ReportingTotal == 0 && cra.ReportingDeadline == "" {
		return ""
	}
	score := cra.EvidenceScore
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	reporting := "--"
	if cra.ReportingTotal > 0 {
		reporting = fmt.Sprintf("%d/%d", cra.ReportingPresent, cra.ReportingTotal)
	}
	deadline := ""
	if cra.ReportingDeadline != "" {
		deadline = ", Article 14 " + cra.ReportingDeadline
	}
	ready := "not ready"
	if cra.ReportingReady {
		ready = "ready"
	}
	design := strings.TrimSpace(cra.DesignEvidenceStatus)
	if design == "" {
		design = "missing"
	}
	return fmt.Sprintf("%d%% evidence, reporting %s %s, design %s%s", score, reporting, ready, design, deadline)
}

func truncateObserveText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return strings.TrimSpace(s[:max-3]) + "..."
}

func formatObserveTokenCount(n int) string {
	switch {
	case n < 0:
		return "0"
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	}
}

func formatObserveCost(usd float64) string {
	switch {
	case usd <= 0:
		return "$0.00"
	case usd < 0.01:
		return fmt.Sprintf("$%.4f", usd)
	case usd < 10:
		return fmt.Sprintf("$%.2f", usd)
	default:
		return fmt.Sprintf("$%.1f", usd)
	}
}

func formatTimeToLimit(quotas []observeRenderQuota) string {
	bestAgent := ""
	var bestETA time.Duration
	seenCap := false
	seenBurn := false
	seenUsage := false
	for _, q := range quotas {
		if q.Used > 0 {
			seenUsage = true
		}
		if q.Cap <= 0 {
			continue
		}
		seenCap = true
		if q.Used >= q.Cap {
			return q.AgentID + " limit reached"
		}
		window, ok := parseQuotaWindow(q.Window)
		if !ok || q.Used <= 0 {
			continue
		}
		seenBurn = true
		ratePerSecond := q.Used / window.Seconds()
		if ratePerSecond <= 0 {
			continue
		}
		eta := time.Duration(((q.Cap - q.Used) / ratePerSecond) * float64(time.Second))
		if bestAgent == "" || eta < bestETA {
			bestAgent = q.AgentID
			bestETA = eta
		}
	}
	if bestAgent != "" {
		return bestAgent + " " + formatObserveDuration(bestETA)
	}
	if seenCap && !seenBurn {
		return "-- (no current burn)"
	}
	if seenUsage {
		return "-- (no quota cap)"
	}
	return "-- (waiting for usage)"
}

func parseQuotaWindow(window string) (time.Duration, bool) {
	window = strings.TrimSpace(strings.ToLower(window))
	if window == "" {
		return 0, false
	}
	if strings.HasSuffix(window, "d") {
		var days float64
		if _, err := fmt.Sscanf(strings.TrimSuffix(window, "d"), "%f", &days); err != nil || days <= 0 {
			return 0, false
		}
		return time.Duration(days * float64(24*time.Hour)), true
	}
	d, err := time.ParseDuration(window)
	return d, err == nil && d > 0
}

func formatObserveDuration(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 48*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1fd", d.Hours()/24)
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
