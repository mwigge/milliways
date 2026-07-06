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
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/daemon/metrics"
	"github.com/mwigge/milliways/internal/daemon/observability"
	"github.com/mwigge/milliways/internal/daemon/runners"
	"github.com/mwigge/milliways/internal/history"
)

// startTime is set when the package is first loaded so ping can report
// uptime against the actual daemon-process start.
var startTime = time.Now()

// Wire-format types — manual mirrors of proto/milliways.json. The Rust side
// has its own mirrors in milliways/src/rpc/types.rs. Both sides will be
// replaced by codegen output (typify Rust, go-jsonschema Go) once Phase 1
// wires them in. Until then, schema drift is caught only by smoke tests.

type PingResult struct {
	Pong    bool         `json:"pong"`
	Version string       `json:"version"`
	UptimeS float64      `json:"uptime_s"`
	Proto   ProtoVersion `json:"proto"`
}

type ProtoVersion struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

// QualitySignals aggregates delegate outcome counts since daemon start.
type QualitySignals struct {
	Pass        int    `json:"pass"`
	Rework      int    `json:"rework"`
	Fail        int    `json:"fail"`
	LastOutcome string `json:"last_outcome,omitempty"`
}

// ProviderStat holds per-agent usage aggregates for the current daily bucket.
type ProviderStat struct {
	AgentID      string  `json:"agent_id"`
	Turns        int     `json:"turns"`
	TokensIn     int     `json:"tokens_in"`
	TokensOut    int     `json:"tokens_out"`
	CostUSD      float64 `json:"cost_usd"`
	P50LatencyMS float64 `json:"p50_latency_ms"`
}

type Status struct {
	Proto             ProtoVersion                           `json:"proto"`
	ActiveAgent       *string                                `json:"active_agent"`
	Turn              int                                    `json:"turn"`
	TokensIn          int                                    `json:"tokens_in"`
	TokensOut         int                                    `json:"tokens_out"`
	CostUSD           float64                                `json:"cost_usd"`
	QuotaPct          float64                                `json:"quota_pct"`
	Errors5m          int                                    `json:"errors_5m"`
	ClientEnforcement map[string]runners.EnforcementMetadata `json:"client_enforcement,omitempty"`
	// OpenTelemetry GenAI performance metrics
	RequestCount      int     `json:"request_count"`
	FailureRate       float64 `json:"failure_rate"`
	TTFTMedian        float64 `json:"ttft_median"`
	TPOTMedian        float64 `json:"tpot_median"`
	OperationDuration float64 `json:"operation_duration"`
	RequestModel      string  `json:"request_model"`
	ResponseModel     string  `json:"response_model"`
	// Delegation quality signals
	QualitySignals QualitySignals `json:"quality_signals"`
	// Per-provider usage breakdown for today's daily bucket
	PerProviderStats []ProviderStat `json:"per_provider_stats,omitempty"`
	ActiveProvider   string         `json:"active_provider,omitempty"`
	RoutingReason    string         `json:"routing_reason,omitempty"`
	SessionCostUSD   float64        `json:"session_cost_usd"`
}

type AgentInfo struct {
	ID          string                      `json:"id"`
	Available   bool                        `json:"available"`
	AuthStatus  string                      `json:"auth_status"`
	Model       string                      `json:"model,omitempty"`
	Enforcement runners.EnforcementMetadata `json:"enforcement"`
}

type QuotaSnapshot struct {
	AgentID     string   `json:"agent_id"`
	Used        float64  `json:"used"`
	Cap         float64  `json:"cap"`
	Pct         float64  `json:"pct"`
	Window      string   `json:"window,omitempty"`
	UsedDaily   *float64 `json:"used_daily,omitempty"`
	UsedWeekly  *float64 `json:"used_weekly,omitempty"`
	UsedMonthly *float64 `json:"used_monthly,omitempty"`
}

type RoutingDecision struct {
	TS         string   `json:"ts"`
	Selected   string   `json:"selected"`
	Considered []string `json:"considered,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

// coreAgentIDs is the canonical list of agents used for per-provider metrics,
// quota snapshots, and enforcement metadata. Order is stable; do not sort.
var coreAgentIDs = []string{
	"claude", "codex", "copilot", "gemini",
	"minimax", "berget", "kimi", "deepseek",
	"local", "pool",
}

// historyAgents is the allowlist for history.append agent_ids.
// Only these agents may have their history recorded.
var historyAgents = map[string]bool{
	"_echo":    true,
	"claude":   true,
	"codex":    true,
	"copilot":  true,
	"gemini":   true,
	"pool":     true,
	"minimax":  true,
	"berget":   true,
	"kimi":     true,
	"deepseek": true,
	"local":    true,
}

const (
	historyRateWindow        = 1 * time.Minute
	historyMaxCallsPerWindow = 60
	historyMaxFileBytes      = 10 << 20 // 10 MiB per agent history file
)

// HistoryQuota enforces per-agent rate and file-size limits on history.append.
type HistoryQuota struct {
	mu       sync.Mutex
	counters map[string]*rateBucket
	fileSize map[string]int64
}

type rateBucket struct {
	count     int
	windowEnd time.Time
}

// NewHistoryQuota returns a fresh per-agent quota tracker.
func NewHistoryQuota() *HistoryQuota {
	return &HistoryQuota{
		counters: make(map[string]*rateBucket),
		fileSize: make(map[string]int64),
	}
}

// Check verifies the append is within rate and size limits.
// Returns ErrQuotaExceeded (code -32005) with a descriptive message on violation.
func (q *HistoryQuota) Check(agentID, stateDir string, payloadBytes int) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()

	// Rate check.
	bucket, ok := q.counters[agentID]
	if !ok || now.After(bucket.windowEnd) {
		q.counters[agentID] = &rateBucket{
			count:     1,
			windowEnd: now.Add(historyRateWindow),
		}
	} else {
		if bucket.count >= historyMaxCallsPerWindow {
			return fmt.Errorf("rate limit: %d calls/min exceeded for agent %q", historyMaxCallsPerWindow, agentID)
		}
		bucket.count++
	}

	// Size check: probe file size.
	fpath := filepath.Join(stateDir, "history", agentID+".ndjson")
	var currentSize int64
	if fi, err := os.Stat(fpath); err == nil {
		currentSize = fi.Size()
	}
	if currentSize+int64(payloadBytes) > historyMaxFileBytes {
		return fmt.Errorf("size limit: history file for agent %q exceeds %d bytes", agentID, historyMaxFileBytes)
	}
	q.fileSize[agentID] = currentSize + int64(payloadBytes)
	return nil
}

// sumMetric queries the metrics store for the sum of metric over range r
// across all agents. Returns 0 on any error.
func (s *Server) sumMetric(metric string, r *metrics.Range) float64 {
	if s.metrics == nil {
		return 0
	}
	res, err := s.metrics.RollupGet(metrics.RollupGetParams{
		Metric: metric,
		Tier:   "raw",
		Range:  r,
	})
	if err != nil {
		return 0
	}
	var total float64
	for _, b := range res.Buckets {
		total += b.Sum
	}
	return total
}

// getMetricCount returns the total count of observations for a metric.
func (s *Server) getMetricCount(metric string, r *metrics.Range) (float64, error) {
	if s.metrics == nil {
		return 0, nil
	}
	res, err := s.metrics.RollupGet(metrics.RollupGetParams{
		Metric: metric,
		Tier:   "raw",
		Range:  r,
	})
	if err != nil {
		return 0, err
	}
	var total float64
	for _, b := range res.Buckets {
		total += float64(b.Count)
	}
	return total, nil
}

// getHistogramMedian returns the median value from a histogram metric.
// Returns 0 if no data available.
func (s *Server) getHistogramMedian(metric string, r *metrics.Range) float64 {
	if s.metrics == nil {
		return 0
	}
	res, err := s.metrics.RollupGet(metrics.RollupGetParams{
		Metric: metric,
		Tier:   "raw",
		Range:  r,
	})
	if err != nil || len(res.Buckets) == 0 {
		return 0
	}
	// For histograms, median is approximated using p50 if available
	// or falling back to sum/count
	var values []float64
	for _, b := range res.Buckets {
		if b.P50 > 0 {
			return b.P50
		}
		// Build a representative sample from count and sum
		if b.Count > 0 {
			avg := b.Sum / float64(b.Count)
			values = append(values, avg)
		}
	}
	if len(values) == 0 {
		return 0
	}
	// Return average of available values as approximation
	var sum, count float64
	for _, v := range values {
		sum += v
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / count
}

// buildStatus returns the current cockpit Status.
func (s *Server) buildStatus() Status {
	s.statusMu.Lock()
	curAgent := s.currentAgent
	s.statusMu.Unlock()
	var activeAgent *string
	if curAgent != "" {
		activeAgent = &curAgent
	}
	// Compute GenAI performance metrics from histograms (last 1h)
	r1h := &metrics.Range{From: "-1h"}
	reqCount, _ := s.getMetricCount("gen_ai.client.request.count", r1h)
	errCount, _ := s.getMetricCount("error_count", r1h)
	failureRate := 0.0
	if reqCount > 0 {
		failureRate = float64(errCount) / float64(reqCount) * 100
		if failureRate > 100 {
			failureRate = 100
		}
	}
	opDuration := s.getHistogramMedian("gen_ai.client.operation.duration", r1h)
	ttft := s.getHistogramMedian("gen_ai.client.operation.time_to_first_chunk", r1h)
	tpot := s.getHistogramMedian("gen_ai.client.operation.time_per_output_chunk", r1h)

	qualitySignals := s.buildQualitySignals()

	// Use a 48h window so both today's and yesterday's midnight-aligned daily
	// buckets are included regardless of when in the day buildStatus is called.
	rDaily := &metrics.Range{From: "-48h"}
	perProvider := s.buildPerProviderStats(rDaily)
	sessionCost := s.buildSessionCostUSD(rDaily)

	s.statusMu.Lock()
	routingReason := s.lastRoutingReason
	s.statusMu.Unlock()

	return Status{
		Proto:             ProtoVersion{Major: ProtoMajor, Minor: ProtoMinor},
		ActiveAgent:       activeAgent,
		Turn:              0,
		TokensIn:          int(s.sumMetric("tokens_in", r1h)),
		TokensOut:         int(s.sumMetric("tokens_out", r1h)),
		CostUSD:           s.sumMetric("cost_usd", r1h),
		QuotaPct:          0.0,
		Errors5m:          int(s.sumMetric("error_count", r1h)),
		ClientEnforcement: clientEnforcementSnapshot(),
		RequestCount:      int(reqCount),
		FailureRate:       failureRate,
		TTFTMedian:        ttft * 1000, // convert s to ms
		TPOTMedian:        tpot * 1000,
		OperationDuration: opDuration * 1000,
		QualitySignals: qualitySignals,
		PerProviderStats: perProvider,
		ActiveProvider:   curAgent,
		RoutingReason:    routingReason,
		SessionCostUSD:   sessionCost,
	}
}

// buildQualitySignals returns delegate outcome counters and the most recently
// completed outcome (the actual last, not the dominant).
func (s *Server) buildQualitySignals() QualitySignals {
	lastOutcome := ""
	if p := s.lastDelegateOutcome.Load(); p != nil {
		lastOutcome = *p
	}
	return QualitySignals{
		Pass:        int(s.delegatePass.Load()),
		Rework:      int(s.delegateRework.Load()),
		Fail:        int(s.delegateFail.Load()),
		LastOutcome: lastOutcome,
	}
}

// buildPerProviderStats returns per-agent usage aggregates from the daily metrics bucket.
// Only agents with non-zero token activity are included. Turns is approximated by the
// number of token-recording observations stored in the daily tier.
func (s *Server) buildPerProviderStats(r *metrics.Range) []ProviderStat {
	if s.metrics == nil {
		return nil
	}
	var stats []ProviderStat
	for _, agentID := range coreAgentIDs {
		id := agentID
		tokIn := s.getTokenTotal("tokens_in", &id, r, "daily")
		if tokIn == 0 {
			continue
		}
		tokOut := s.getTokenTotal("tokens_out", &id, r, "daily")
		cost := s.getTokenTotal("cost_usd", &id, r, "daily")
		turns := s.getAgentTurns(&id, r)
		stats = append(stats, ProviderStat{
			AgentID:      agentID,
			Turns:        turns,
			TokensIn:     int(tokIn),
			TokensOut:    int(tokOut),
			CostUSD:      cost,
			P50LatencyMS: 0,
		})
	}
	return stats
}

// buildSessionCostUSD returns the total cost_usd across all providers for today's daily bucket.
func (s *Server) buildSessionCostUSD(r *metrics.Range) float64 {
	if s.metrics == nil {
		return 0
	}
	res, err := s.metrics.RollupGet(metrics.RollupGetParams{
		Metric: "cost_usd",
		Tier:   "daily",
		Range:  r,
	})
	if err != nil {
		slog.Debug("metrics: buildSessionCostUSD", "err", err)
		return 0
	}
	var total float64
	for _, b := range res.Buckets {
		total += b.Sum
	}
	return total
}

// getAgentTurns returns the number of dispatch turns for an agent in the given range.
// Uses the count of tokens_in observations as a proxy for turns.
func (s *Server) getAgentTurns(agentID *string, r *metrics.Range) int {
	if s.metrics == nil {
		return 0
	}
	res, err := s.metrics.RollupGet(metrics.RollupGetParams{
		Metric:  "tokens_in",
		Tier:    "daily",
		Range:   r,
		AgentID: agentID,
	})
	if err != nil {
		slog.Debug("metrics: getAgentTurns", "err", err)
		return 0
	}
	var total int64
	for _, b := range res.Buckets {
		total += b.Count
	}
	return int(total)
}

func clientEnforcementSnapshot() map[string]runners.EnforcementMetadata {
	out := make(map[string]runners.EnforcementMetadata, len(coreAgentIDs))
	for _, agent := range coreAgentIDs {
		out[agent] = runners.ClientEnforcementMetadata(agent)
	}
	return out
}

// buildQuotaSnapshots returns per-agent token/cost usage for the last hour.
// Cap is 0 (unlimited) for all runners until per-agent limits are configured.
func (s *Server) buildQuotaSnapshots() []QuotaSnapshot {
	if s.metrics == nil {
		return nil
	}
	r1h := &metrics.Range{From: "-1h"}
	r24h := &metrics.Range{From: "-24h"}
	r1w := &metrics.Range{From: "-1w"}
	r1m := &metrics.Range{From: "-1mo"}
	// Wider range to detect if agent has ever had historical data
	r30d := &metrics.Range{From: "-30d"}
	var out []QuotaSnapshot
	for _, agent := range coreAgentIDs {
		agentCopy := agent

		// Check if agent has any historical data (30 days, use daily tier)
		hasHistory := s.getTokenTotal("tokens_in", &agentCopy, r30d, "daily") > 0

		// Get current 1h usage
		res, err := s.metrics.RollupGet(metrics.RollupGetParams{
			Metric:  "tokens_in",
			Tier:    "raw",
			Range:   r1h,
			AgentID: &agentCopy,
		})
		if err != nil {
			// Still include if has history
			if !hasHistory {
				continue
			}
		}
		var used float64
		for _, b := range res.Buckets {
			used += b.Sum
		}

		// Get 24h/weekly/monthly totals using the right aggregation tier so
		// each window reflects its actual period, not just the 60-min raw buffer.
		hour24Used := s.getTokenTotal("tokens_in", &agentCopy, r24h, "hourly")
		weeklyUsed := s.getTokenTotal("tokens_in", &agentCopy, r1w, "daily")
		monthlyUsed := s.getTokenTotal("tokens_in", &agentCopy, r1m, "daily")

		// Include agent if it has current usage OR historical data
		if used == 0 && !hasHistory {
			continue
		}

		out = append(out, QuotaSnapshot{
			AgentID:     agent,
			Used:        used,
			Cap:         0,
			Pct:         0,
			Window:      "1h",
			UsedDaily:   &hour24Used,
			UsedWeekly:  &weeklyUsed,
			UsedMonthly: &monthlyUsed,
		})
	}
	return out
}

// getTokenTotal returns the sum of a metric for the given agent and range.
// tier selects the aggregation granularity: "raw" (1h window), "hourly"
// (24h), "daily" (7d+), "weekly" (months). Falls back to "raw" on error.
func (s *Server) getTokenTotal(metric string, agentID *string, rng *metrics.Range, tier string) float64 {
	res, err := s.metrics.RollupGet(metrics.RollupGetParams{
		Metric:  metric,
		Tier:    tier,
		Range:   rng,
		AgentID: agentID,
	})
	if err != nil && tier != "raw" {
		// Aggregated tiers may be empty if daemon just started; fall back to raw.
		return s.getTokenTotal(metric, agentID, rng, "raw")
	}
	if err != nil {
		return 0
	}
	var total float64
	for _, b := range res.Buckets {
		total += b.Sum
	}
	return total
}

// dispatch routes a JSON-RPC method call to its handler. Methods that are
// stubs return canned shapes that conform to the JSON Schema; real
// implementations land per Phase 1+ tasks.
func (s *Server) dispatch(enc *json.Encoder, req *Request) {
	slog.Debug("rpc", "method", req.Method, "id", string(req.ID))
	start := time.Now()
	defer s.recordSpan(req.Method, start)
	defer s.recordDispatchMetrics(req.Method, start)
	switch req.Method {
	case "ping":
		writeResult(enc, req.ID, PingResult{
			Pong:    true,
			Version: Version,
			UptimeS: time.Since(startTime).Seconds(),
			Proto:   ProtoVersion{Major: ProtoMajor, Minor: ProtoMinor},
		})
	case "status.get":
		writeResult(enc, req.ID, s.buildStatus())
	case "capabilities.get":
		writeResult(enc, req.ID, map[string]any{
			"clients": clientEnforcementSnapshot(),
		})
	case "workflow.list":
		s.workflowList(enc, req)
	case "workflow.templates":
		s.workflowTemplates(enc, req)
	case "workflow.create":
		s.workflowCreate(enc, req)
	case "workflow.get":
		s.workflowGet(enc, req)
	case "workflow.export":
		s.workflowExport(enc, req)
	case "workflow.import":
		s.workflowImport(enc, req)
	case "workflow.ready":
		s.workflowReady(enc, req)
	case "workflow.cancel":
		s.workflowCancel(enc, req)
	case "workflow.node.start":
		s.workflowNodeStart(enc, req)
	case "workflow.node.delegate":
		s.workflowNodeDelegate(enc, req)
	case "workflow.node.complete":
		s.workflowNodeComplete(enc, req)
	case "workflow.node.fail":
		s.workflowNodeFail(enc, req)
	case "workflow.node.retry":
		s.workflowNodeRetry(enc, req)
	case "workflow.node.wait_approval":
		s.workflowNodeWaitApproval(enc, req)
	case "workflow.node.resume":
		s.workflowNodeResume(enc, req)
	case "workflow.node.deny":
		s.workflowNodeDeny(enc, req)
	case "status.subscribe":
		stream := s.streams.Allocate()
		s.registerStatusSubscriber(stream)
		writeResult(enc, req.ID, map[string]any{
			"stream_id":     stream.ID,
			"output_offset": int64(0),
		})
		// Push an initial snapshot so the client doesn't have to wait
		// for the first 1Hz tick.
		go func() {
			stream.Push(map[string]any{"t": "data", "snapshot": s.buildStatus()})
		}()
	case "agent.list":
		writeResult(enc, req.ID, s.agentList())
	case "agent.open":
		s.agentOpen(enc, req)
	case "agent.set_active":
		s.agentSetActive(enc, req)
	case "agent.send":
		s.agentSend(enc, req)
	case "agent.stream":
		s.agentStream(enc, req)
	case "agent.close":
		s.agentClose(enc, req)
	case "deck.snapshot":
		s.deckSnapshot(enc, req)
	case "approval.list":
		s.approvalList(enc, req)
	case "approval.respond":
		s.approvalRespond(enc, req)
	case "security.gate_tool":
		s.securityGateTool(enc, req)
	case "coding.changes":
		s.codingChanges(enc, req)
	case "coding.diff":
		s.codingDiff(enc, req)
	case "apply.extract":
		s.applyExtract(enc, req)
	case "context.get":
		s.contextGet(enc, req)
	case "context.get_all":
		s.contextGetAll(enc, req)
	case "context.subscribe":
		s.contextSubscribe(enc, req)
	case "quota.get":
		writeResult(enc, req.ID, s.buildQuotaSnapshots())
	case "routing.peek":
		writeResult(enc, req.ID, []RoutingDecision{})
	case "observability.spans":
		var p observabilitySpansParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
				return
			}
		}
		writeResult(enc, req.ID, s.spans.Snapshot(p.parsedSince(), p.Limit))
	case "observability.subscribe":
		s.observabilitySubscribe(enc, req)
	case "observability.metrics":
		s.observabilityMetrics(enc, req)
	case "metrics.rollup.get":
		s.metricsRollupGet(enc, req)
	case "history.append":
		// params: {agent_id: string, payload: any, max_lines: int}
		var p struct {
			AgentID  string          `json:"agent_id"`
			Payload  json.RawMessage `json:"payload"`
			MaxLines int             `json:"max_lines,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
		if p.AgentID == "" {
			writeError(enc, req.ID, ErrInvalidParams, "agent_id is required")
			return
		}
		if !historyAgents[p.AgentID] {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("unknown agent_id: %q", p.AgentID))
			return
		}
		if len(p.Payload) > 1<<20 {
			writeError(enc, req.ID, ErrInvalidParams, "payload too large")
			return
		}
		stateDir := filepath.Dir(s.socket)
		if s.historyQuota != nil {
			if err := s.historyQuota.Check(p.AgentID, stateDir, len(p.Payload)); err != nil {
				writeError(enc, req.ID, ErrQuotaExceeded, err.Error())
				return
			}
		}
		var anyPayload any
		if len(p.Payload) > 0 {
			if err := json.Unmarshal(p.Payload, &anyPayload); err != nil {
				writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode payload: %v", err))
				return
			}
		}
		if err := history.AppendAgentHistory(stateDir, p.AgentID, anyPayload, p.MaxLines); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, err.Error())
			return
		}
		writeResult(enc, req.ID, map[string]any{"ok": true})
	case "history.get":
		// params: {agent_id: string, limit: int}
		var p2 struct {
			AgentID string `json:"agent_id"`
			Limit   int    `json:"limit,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p2); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
		stateDir := filepath.Dir(s.socket)
		res, err := history.ReadAgentHistory(stateDir, p2.AgentID, p2.Limit)
		if err != nil {
			writeError(enc, req.ID, ErrInvalidParams, err.Error())
			return
		}
		writeResult(enc, req.ID, res)
	case "parallel.dispatch":
		s.parallelDispatch(enc, req)
	case "group.status":
		s.groupStatus(enc, req)
	case "group.list":
		s.groupList(enc, req)
	case "consensus.aggregate":
		s.consensusAggregate(enc, req)
	case "mempalace.write_handoff":
		s.mempalaceWriteHandoff(enc, req)
	case "security.list":
		s.securityList(enc, req)
	case "security.show":
		s.securityShow(enc, req)
	case "security.exists":
		s.securityExists(enc, req)
	case "security.accept":
		s.securityAccept(enc, req)
	case "security.scan":
		s.securityScan(enc, req)
	case "security.enable":
		s.securityEnable(enc, req)
	case "security.disable":
		s.securityDisable(enc, req)
	case "security.status":
		s.securityStatus(enc, req)
	case "security.cra":
		s.securityCRA(enc, req)
	case "security.startup_scan":
		s.securityStartupScan(enc, req)
	case "security.warnings":
		s.securityWarnings(enc, req)
	case "security.mode":
		s.securityMode(enc, req)
	case "security.client_profile":
		s.securityClientProfile(enc, req)
	case "security.command_check":
		s.securityCommandCheck(enc, req)
	case "security.policy_decide":
		s.securityPolicyDecide(enc, req)
	case "security.policy_audit":
		s.securityPolicyAudit(enc, req)
	case "security.quarantine":
		s.securityQuarantine(enc, req)
	case "security.rules_list":
		s.securityRulesList(enc, req)
	case "security.rules_update":
		s.securityRulesUpdate(enc, req)
	case "config.setenv":
		// Injects a single env var into the daemon process so runners that
		// read it on each request (e.g. MINIMAX_API_KEY) pick it up without
		// a restart. Only a pre-approved set of milliways-specific keys is
		// accepted to prevent callers from mutating unrelated env vars.
		var p struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
		if !allowedSetenvKeys[p.Key] {
			writeError(enc, req.ID, ErrInvalidParams, "key not in allowed set: "+p.Key)
			return
		}
		// Empty value means "unset the variable" (e.g. /path reset).
		var setErr error
		if p.Value == "" {
			setErr = os.Unsetenv(p.Key)
		} else {
			setErr = os.Setenv(p.Key, p.Value)
		}
		if setErr != nil {
			writeError(enc, req.ID, ErrInvalidParams, "setenv: "+setErr.Error())
			return
		}
		localEnvPath := localEnvDefaultPath()
		persisted := true
		persistErr := ""
		if err := persistLocalEnv(localEnvPath, p.Key, p.Value); err != nil {
			persisted = false
			persistErr = err.Error()
			slog.Warn("config.setenv: could not persist to local.env", "key", p.Key, "err", err)
		}
		writeResult(enc, req.ID, map[string]any{
			"ok":            true,
			"key":           p.Key,
			"persisted":     persisted,
			"persist_path":  localEnvPath,
			"persist_error": persistErr,
		})
	default:
		writeError(enc, req.ID, ErrMethodNotFound, "unknown method: "+req.Method)
	}
}

type observabilitySpansParams struct {
	Since string `json:"since,omitempty"` // RFC3339; empty = unbounded
	Limit int    `json:"limit,omitempty"` // 0 = no limit
}

func (p observabilitySpansParams) parsedSince() time.Time {
	if p.Since == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, p.Since)
	if err != nil {
		return time.Time{}
	}
	return t
}

// recordSpan creates an observability span for the just-completed dispatch
// and pushes it to the ring buffer. Called from a defer on dispatch entry.
func (s *Server) recordSpan(method string, start time.Time) {
	s.spans.Push(observability.Span{
		TraceID:    observability.NewTraceID(),
		SpanID:     observability.NewSpanID(),
		Name:       "rpc:" + method,
		StartTS:    start,
		DurationMS: float64(time.Since(start).Microseconds()) / 1000.0,
		Status:     "ok",
	})
}

// recordDispatchMetrics observes the per-call dispatch_count counter and
// dispatch_latency_ms histogram, tagged by method via the agent_id slot
// (no separate `method` column in samples — the agent_id column doubles
// as the dimension axis for daemon-wide RPCs).
func (s *Server) recordDispatchMetrics(method string, start time.Time) {
	if s.metrics == nil {
		return
	}
	latencyMS := float64(time.Since(start).Microseconds()) / 1000.0
	s.metrics.ObserveCounter("dispatch_count", method, 1)
	s.metrics.ObserveHistogram("dispatch_latency_ms", method, latencyMS)
}

// metricsRollupGet handles the metrics.rollup.get JSON-RPC method.
func (s *Server) metricsRollupGet(enc *json.Encoder, req *Request) {
	if s.metrics == nil {
		writeError(enc, req.ID, ErrMethodNotFound, "metrics store not initialised")
		return
	}
	var p metrics.RollupGetParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	res, err := s.metrics.RollupGet(p)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, err.Error())
		return
	}
	writeResult(enc, req.ID, res)
}
