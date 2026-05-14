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
			got := formatObservabilityFrame(fixedNow, tt.spans, observeRenderUsage{})
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
// snapshots keep a dim placeholder chart so the cockpit layout does not jump.
func TestFormatObservabilityFrame_EmbedsBarsChart(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, 4, 27, 12, 34, 56, 0, time.UTC)

	// Empty: placeholder chart remains mounted.
	got := formatObservabilityFrame(fixedNow, nil, observeRenderUsage{})
	if !strings.Contains(got, "\x1b_G") {
		t.Errorf("empty snapshot should embed a placeholder kitty escape")
	}

	// Populated: escape and label appear.
	spans := []observeRenderSpan{
		{Name: "rpc.ping", DurationMS: 0.5, Status: "ok"},
		{Name: "rpc.ping", DurationMS: 1.5, Status: "ok"},
		{Name: "rpc.ping", DurationMS: 12.0, Status: "ok"},
	}
	got = formatObservabilityFrame(fixedNow, spans, observeRenderUsage{})
	if !strings.Contains(got, "\x1b_G") {
		t.Errorf("populated snapshot should embed a kitty escape:\n%s", got)
	}
	if !strings.Contains(got, "latency (top") {
		t.Errorf("expected 'latency (top …)' label in frame:\n%s", got)
	}
}

func TestFormatObservabilityFrame_ShowsUsageAndTimeToLimit(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, 4, 27, 12, 34, 56, 0, time.UTC)
	usage := observeRenderUsage{
		Status: observeRenderStatus{TokensIn: 1200, TokensOut: 800, CostUSD: 0.0123},
		Quotas: []observeRenderQuota{{
			AgentID: "claude",
			Used:    500,
			Cap:     1000,
			Window:  "1h",
		}},
	}

	got := formatObservabilityFrame(fixedNow, nil, usage)
	for _, want := range []string{
		"usage:",
		"tokens:        in 1.2k / out 800 / total 2.0k",
		"cost:          $0.01",
		"time to limit: claude 1.0h",
		"security:      SEC WARN",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("frame missing %q:\n%s", want, got)
		}
	}
}

func TestFormatObservabilityFrame_ShowsSecurityPosture(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, 4, 27, 12, 34, 56, 0, time.UTC)
	usage := observeRenderUsage{
		Security: observeRenderSecurity{
			Installed:         true,
			Enabled:           true,
			Mode:              "strict",
			Posture:           "block",
			Warnings:          2,
			Blocks:            1,
			SecurityWorkspace: "/repo/service",
		},
	}

	got := formatObservabilityFrame(fixedNow, nil, usage)
	for _, want := range []string{
		"security:      SEC BLOCK 1 (mode strict)",
		"sec workspace: /repo/service",
		"milliways observability",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("frame missing %q:\n%s", want, got)
		}
	}
}

func TestFormatObservabilityFrame_ShowsStartupScanAndScannerGapsCompactly(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	usage := observeRenderUsage{
		Security: observeRenderSecurity{
			Installed:           true,
			Enabled:             true,
			Mode:                "warn",
			Posture:             "warn",
			StartupScanRequired: true,
			StartupScanStale:    true,
			Scanners: []observeRenderScanner{
				{Name: "osv-scanner", Installed: true},
				{Name: "gitleaks", Installed: false},
				{Name: "semgrep", Installed: true},
				{Name: "govulncheck", Installed: false},
			},
		},
	}

	got := formatObservabilityFrame(fixedNow, nil, usage)
	want := "sec detail:    startup scan stale; missing local scanners gitleaks, govulncheck"
	if !strings.Contains(got, want) {
		t.Fatalf("frame missing compact security detail %q:\n%s", want, got)
	}
	if strings.Contains(got, "osv-scanner") || strings.Contains(got, "semgrep") {
		t.Fatalf("security detail should only include scanner gaps:\n%s", got)
	}
}

func TestFormatObservabilityFrame_ShowsCRAReadinessKPIs(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	usage := observeRenderUsage{
		Security: observeRenderSecurity{
			Installed: true,
			Mode:      "warn",
			CRA: observeRenderCRA{
				EvidenceScore:        67,
				ChecksTotal:          6,
				ChecksPresent:        3,
				ChecksPartial:        2,
				ChecksMissing:        1,
				ReportingReady:       false,
				ReportingPresent:     2,
				ReportingTotal:       3,
				DesignEvidenceStatus: "partial",
				SecurityWarnings:     2,
				SecurityBlocks:       1,
				DaysToReporting:      120,
				ReportingDeadline:    "2026-09-11",
				NextAction:           "Generate SBOM evidence: milliwaysctl security sbom --output dist/milliways.spdx.json",
			},
		},
	}

	got := formatObservabilityFrame(fixedNow, nil, usage)
	want := "cra:           67% evidence, reporting 2/3 not ready, security 2w/1b, design partial"
	if !strings.Contains(got, want) {
		t.Fatalf("frame missing CRA KPIs %q:\n%s", want, got)
	}
	if !strings.Contains(got, "cra next:      Generate SBOM evidence: milliwaysctl security sbom") {
		t.Fatalf("frame missing CRA next action:\n%s", got)
	}
	if strings.Contains(got, "120d to 2026-09-11") {
		t.Fatalf("frame should not render CRA as a countdown:\n%s", got)
	}
	if strings.Contains(got, "Article 14 2026-09-11") {
		t.Fatalf("frame should treat Article 14 as active posture, not a date KPI:\n%s", got)
	}
}

func TestFormatTimeToLimitFallbacks(t *testing.T) {
	t.Parallel()
	if got := formatTimeToLimit(nil); got != "-- (waiting for usage)" {
		t.Fatalf("empty quotas = %q", got)
	}
	if got := formatTimeToLimit([]observeRenderQuota{{AgentID: "claude", Used: 0, Cap: 1000, Window: "1h"}}); got != "-- (no current burn)" {
		t.Fatalf("no burn = %q", got)
	}
	if got := formatTimeToLimit([]observeRenderQuota{{AgentID: "claude", Used: 100, Cap: 0, Window: "1h"}}); got != "-- (no quota cap)" {
		t.Fatalf("uncapped usage = %q", got)
	}
	if got := formatTimeToLimit([]observeRenderQuota{{AgentID: "claude", Used: 1000, Cap: 1000, Window: "1h"}}); got != "claude limit reached" {
		t.Fatalf("limit reached = %q", got)
	}
}

func TestFormatObservabilityFrame_StaysCompact(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, 4, 27, 12, 34, 56, 0, time.UTC)
	spans := []observeRenderSpan{
		{Name: "rpc.status.get", DurationMS: 0.5, Status: "ok"},
		{Name: "rpc.agent.list", DurationMS: 0.1, Status: "ok"},
		{Name: "rpc.observe.latest", DurationMS: 2.0, Status: "ok"},
	}

	got := formatObservabilityFrame(fixedNow, spans, observeRenderUsage{})
	if strings.Contains(got, "recent spans") {
		t.Fatalf("frame should not render a scrolling span tail:\n%s", got)
	}
	if !strings.Contains(got, "latest: rpc.observe.latest") {
		t.Fatalf("frame missing compact latest span:\n%s", got)
	}
	if lines := strings.Count(got, "\n"); lines > 19 {
		t.Fatalf("frame too tall for lower-left pane: %d lines\n%s", lines, got)
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
