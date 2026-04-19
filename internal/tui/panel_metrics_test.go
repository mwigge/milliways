package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/sommelier"
)

func TestUpdateAccumulatesCostEvents(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	for _, evt := range []adapter.Event{
		{
			Type:    adapter.EventCost,
			Kitchen: "claude",
			Cost: &adapter.CostInfo{
				USD:          0.02,
				InputTokens:  1000,
				OutputTokens: 500,
			},
		},
		{
			Type:    adapter.EventCost,
			Kitchen: "claude",
			Cost: &adapter.CostInfo{
				USD:          0.03,
				InputTokens:  2000,
				OutputTokens: 800,
			},
		},
	} {
		updated, _ := m.Update(blockEventMsg{Event: evt})
		m = updated.(Model)
	}

	acc := m.costByKitchen["claude"]
	if got, want := acc.Calls, 2; got != want {
		t.Fatalf("Calls = %d, want %d", got, want)
	}
	if got, want := acc.TotalUSD, 0.05; got != want {
		t.Fatalf("TotalUSD = %.2f, want %.2f", got, want)
	}
	if got, want := m.costTotalUSD, 0.05; got != want {
		t.Fatalf("costTotalUSD = %.2f, want %.2f", got, want)
	}
}

func TestCostPanelAccumulates(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.costByKitchen = map[string]costAccumulator{
		"claude": {Calls: 1, InputToks: 1000, OutputToks: 500, TotalUSD: 0.02},
	}
	m.costTotalUSD = 0.02

	got := m.renderCostPanel(24, 10)
	if !strings.Contains(got, "$0.02") {
		t.Fatalf("renderCostPanel missing cost: %q", got)
	}
}

func TestCostPanelEmpty(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	got := m.renderCostPanel(24, 10)
	if !strings.Contains(got, "(no cost data yet)") {
		t.Fatalf("renderCostPanel not empty: %q", got)
	}
}

func TestCostPanelRoundsUSD(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.costByKitchen = map[string]costAccumulator{
		"claude": {TotalUSD: 1.555},
	}
	m.costTotalUSD = 1.555

	got := m.renderCostPanel(24, 10)
	if !strings.Contains(got, "$1.56") {
		t.Fatalf("renderCostPanel rounding wrong: %q", got)
	}
}

func TestUpdateRoutingHistoryGrowsAndTrims(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	for i := 0; i < 25; i++ {
		updated, _ := m.Update(blockRoutedMsg{Decision: sommelier.Decision{
			Kitchen:      fmt.Sprintf("k%d", i),
			Tier:         "keyword",
			Reason:       "test",
			SignalScores: map[string]float64{"score": float64(i)},
		}})
		m = updated.(Model)
	}

	if got, want := len(m.routingHistory), 20; got != want {
		t.Fatalf("routingHistory len = %d, want %d", got, want)
	}
	if got := m.routingHistory[0].Kitchen; got != "k24" {
		t.Fatalf("latest routing kitchen = %q, want %q", got, "k24")
	}
	if got := m.routingHistory[0].Signals["score"]; got != 24 {
		t.Fatalf("latest routing score = %.0f, want 24", got)
	}
}

func TestRoutingPanelEmpty(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	got := m.renderRoutingPanel(24, 10)
	if !strings.Contains(got, "(no routing decisions yet)") {
		t.Fatalf("renderRoutingPanel not empty: %q", got)
	}
}

func TestTierBadge(t *testing.T) {
	t.Parallel()

	cases := []struct{ tier, want string }{
		{"forced", "[forced]"},
		{"keyword", "[kw]"},
		{"enriched", "[enr]"},
		{"learned", "[lrnd]"},
		{"fallback", "[fallbk]"},
		{"unknown", "[unknown]"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.tier, func(t *testing.T) {
			t.Parallel()

			got := TierBadge(tc.tier)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("TierBadge(%q) = %q, want contains %q", tc.tier, got, tc.want)
			}
		})
	}
}

func TestSystemPanelEmpty(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	got := m.renderSystemPanel(24, 10)
	if !strings.Contains(got, "(idle)") {
		t.Fatalf("renderSystemPanel not idle: %q", got)
	}
}

func TestProcStatsWithHighCPU(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.procStats = map[string]procInfo{
		"claude": {PID: 12345, CPU: 85, MemMB: 400},
	}

	got := m.renderSystemPanel(24, 10)
	if !strings.Contains(got, "CPU") || !strings.Contains(got, "MEM") {
		t.Fatalf("renderSystemPanel missing stats: %q", got)
	}
}

func TestParseProcStatsOutput(t *testing.T) {
	t.Parallel()

	got, err := parseProcStatsOutput(123, "12.5 4.0 /usr/bin/claude\n")
	if err != nil {
		t.Fatalf("parseProcStatsOutput returned error: %v", err)
	}
	if got.PID != 123 {
		t.Fatalf("PID = %d, want 123", got.PID)
	}
	if got.CPU != 12.5 {
		t.Fatalf("CPU = %.1f, want 12.5", got.CPU)
	}
	if got.Exe != "/usr/bin/claude" {
		t.Fatalf("Exe = %q, want %q", got.Exe, "/usr/bin/claude")
	}
	if got.MemMB <= 0 {
		t.Fatalf("MemMB = %.2f, want > 0", got.MemMB)
	}
}

func TestBlockPIDMsgUpdatesBlock(t *testing.T) {
	t.Parallel()

	m := NewModel(nil)
	m.blocks = append(m.blocks, Block{ID: "b1", StartedAt: time.Now()})

	updated, _ := m.Update(blockPIDMsg{BlockID: "b1", PID: 4321})
	m = updated.(Model)

	if got := m.blocks[0].PID; got != 4321 {
		t.Fatalf("PID = %d, want 4321", got)
	}
}
