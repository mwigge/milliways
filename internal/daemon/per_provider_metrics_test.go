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
	"path/filepath"
	"testing"
	"time"

	daemonobs "github.com/mwigge/milliways/internal/daemon/observability"
	"github.com/mwigge/milliways/internal/daemon/metrics"
)

// newMetricsServer creates a Server backed by a real SQLite metrics store seeded
// with per-agent daily-tier data.
func newMetricsServer(t *testing.T) (*Server, *metrics.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := metrics.Open(filepath.Join(dir, "metrics.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	store.SetTimezone(time.UTC)

	for _, m := range []struct {
		name string
		kind metrics.Kind
	}{
		{"tokens_in", metrics.KindCounter},
		{"tokens_out", metrics.KindCounter},
		{"cost_usd", metrics.KindCounter},
	} {
		if err := store.Register(m.name, m.kind); err != nil {
			t.Fatalf("register %s: %v", m.name, err)
		}
	}

	srv := &Server{
		spans:   daemonobs.NewRing(10),
		metrics: store,
	}
	return srv, store
}

// seedDailyData inserts observations at a timestamp old enough to cascade to the
// daily tier in a single Rollup call, then rolls up.
func seedDailyData(t *testing.T, store *metrics.Store, agentID string, tokIn, tokOut, cost float64, observations int) {
	t.Helper()
	// Observations from 25h ago cascade raw → hourly → daily in one Rollup call.
	base := time.Now().UTC()
	past := base.Add(-25 * time.Hour)
	store.SetNow(func() time.Time { return past })

	for i := 0; i < observations; i++ {
		store.ObserveCounter("tokens_in", agentID, tokIn/float64(observations))
		store.ObserveCounter("tokens_out", agentID, tokOut/float64(observations))
		store.ObserveCounter("cost_usd", agentID, cost/float64(observations))
	}
	if err := store.FlushNow(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	// Reset clock to now before rollup so retention cutoffs are correct.
	store.SetNow(func() time.Time { return base })
	if err := store.Rollup(); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	// Restore real time.
	store.SetNow(nil)
}

func TestBuildPerProviderStatsEmptyWhenNoData(t *testing.T) {
	srv, _ := newMetricsServer(t)
	stats := srv.buildPerProviderStats(&metrics.Range{From: "-24h"})
	if len(stats) != 0 {
		t.Errorf("stats = %d entries, want 0 when no data", len(stats))
	}
}

func TestBuildPerProviderStatsSingleAgent(t *testing.T) {
	srv, store := newMetricsServer(t)

	// 3 requests, 900 tokens_in, 450 tokens_out, $0.90
	seedDailyData(t, store, "claude", 900, 450, 0.9, 3)

	stats := srv.buildPerProviderStats(&metrics.Range{From: "-48h"})

	if len(stats) != 1 {
		t.Fatalf("stats len = %d, want 1", len(stats))
	}
	s := stats[0]
	if s.AgentID != "claude" {
		t.Errorf("AgentID = %q, want claude", s.AgentID)
	}
	if s.TokensIn != 900 {
		t.Errorf("TokensIn = %d, want 900", s.TokensIn)
	}
	if s.TokensOut != 450 {
		t.Errorf("TokensOut = %d, want 450", s.TokensOut)
	}
	if s.CostUSD < 0.89 || s.CostUSD > 0.91 {
		t.Errorf("CostUSD = %v, want ~0.90", s.CostUSD)
	}
	if s.Turns != 3 {
		t.Errorf("Turns = %d, want 3", s.Turns)
	}
}

func TestBuildPerProviderStatsMultipleAgents(t *testing.T) {
	srv, store := newMetricsServer(t)

	seedDailyData(t, store, "claude", 1000, 500, 1.0, 2)
	seedDailyData(t, store, "codex", 2000, 1000, 2.0, 4)

	stats := srv.buildPerProviderStats(&metrics.Range{From: "-48h"})

	if len(stats) != 2 {
		t.Fatalf("stats len = %d, want 2", len(stats))
	}

	byAgent := make(map[string]ProviderStat)
	for _, s := range stats {
		byAgent[s.AgentID] = s
	}

	if byAgent["claude"].TokensIn != 1000 {
		t.Errorf("claude TokensIn = %d, want 1000", byAgent["claude"].TokensIn)
	}
	if byAgent["codex"].TokensIn != 2000 {
		t.Errorf("codex TokensIn = %d, want 2000", byAgent["codex"].TokensIn)
	}
	if byAgent["claude"].Turns != 2 {
		t.Errorf("claude Turns = %d, want 2", byAgent["claude"].Turns)
	}
	if byAgent["codex"].Turns != 4 {
		t.Errorf("codex Turns = %d, want 4", byAgent["codex"].Turns)
	}
}

func TestBuildSessionCostUSDSumsAllAgents(t *testing.T) {
	srv, store := newMetricsServer(t)

	seedDailyData(t, store, "claude", 0, 0, 1.5, 1)
	seedDailyData(t, store, "codex", 0, 0, 0.5, 1)

	cost := srv.buildSessionCostUSD(&metrics.Range{From: "-48h"})
	if cost < 1.9 || cost > 2.1 {
		t.Errorf("SessionCostUSD = %v, want ~2.0", cost)
	}
}

func TestBuildSessionCostUSDZeroWhenNoMetrics(t *testing.T) {
	srv := &Server{spans: daemonobs.NewRing(10)}
	cost := srv.buildSessionCostUSD(&metrics.Range{From: "-24h"})
	if cost != 0 {
		t.Errorf("SessionCostUSD = %v, want 0 with nil metrics store", cost)
	}
}

func TestBuildStatusIncludesPerProviderStats(t *testing.T) {
	srv, store := newMetricsServer(t)
	seedDailyData(t, store, "gemini", 500, 250, 0.25, 1)

	status := srv.buildStatus()

	if len(status.PerProviderStats) == 0 {
		t.Fatal("PerProviderStats should be non-empty after seeding")
	}
	if status.SessionCostUSD < 0.24 || status.SessionCostUSD > 0.26 {
		t.Errorf("SessionCostUSD = %v, want ~0.25", status.SessionCostUSD)
	}
}

func TestBuildStatusRoutingReason(t *testing.T) {
	srv := newQualityServer()
	srv.statusMu.Lock()
	srv.lastRoutingReason = "fallback: claude quota exceeded"
	srv.statusMu.Unlock()

	status := srv.buildStatus()
	if status.RoutingReason != "fallback: claude quota exceeded" {
		t.Errorf("RoutingReason = %q, want fallback reason", status.RoutingReason)
	}
}
