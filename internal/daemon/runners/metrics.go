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

// MetricsObserver is the minimal contract the runners need to push
// per-response metrics into the daemon's metrics store. Defined here
// (instead of imported from internal/daemon/metrics) to avoid a cyclic
// import — the daemon package already depends on runners.
//
// *metrics.Store satisfies this interface naturally; tests can pass a
// mock that records calls. Runners must accept a nil observer and skip
// observation in that case.
type MetricsObserver interface {
	ObserveCounter(name, agentID string, delta float64)
	ObserveHistogram(name, agentID string, value float64)
}

// Agent IDs used as the dimension axis on per-runner metrics. Kept as
// constants so all runner sites share one spelling (and tests can refer
// to the same identifier without typos).
const (
	AgentIDClaude  = "claude"
	AgentIDCodex   = "codex"
	AgentIDCopilot = "copilot"
	AgentIDMiniMax = "minimax"
	AgentIDLocal   = "local"
)

// Metric names used by the runners. Mirror the registrations in
// internal/daemon/server.go's registerCoreMetrics.
const (
	MetricTokensIn   = "tokens_in"
	MetricTokensOut  = "tokens_out"
	MetricCostUSD    = "cost_usd"
	MetricErrorCount = "error_count"
)

// observeTokens is a small helper that pushes tokens_in/out + cost_usd
// counters in one go. Nil observer is a no-op so callers don't need to
// guard at every site.
func observeTokens(m MetricsObserver, agentID string, in, out int, costUSD float64) {
	if m == nil {
		return
	}
	if in > 0 {
		m.ObserveCounter(MetricTokensIn, agentID, float64(in))
	}
	if out > 0 {
		m.ObserveCounter(MetricTokensOut, agentID, float64(out))
	}
	if costUSD > 0 {
		m.ObserveCounter(MetricCostUSD, agentID, costUSD)
	}
}

// observeError pushes a single error_count tick. Nil observer is a no-op.
func observeError(m MetricsObserver, agentID string) {
	if m == nil {
		return
	}
	m.ObserveCounter(MetricErrorCount, agentID, 1)
}
