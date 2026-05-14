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

type Status struct {
	Proto       ProtoVersion `json:"proto"`
	ActiveAgent *string      `json:"active_agent"`
	Turn        int          `json:"turn"`
	TokensIn    int          `json:"tokens_in"`
	TokensOut   int          `json:"tokens_out"`
	CostUSD     float64      `json:"cost_usd"`
	QuotaPct    float64      `json:"quota_pct"`
	Errors5m    int          `json:"errors_5m"`
}

type AgentInfo struct {
	ID         string `json:"id"`
	Available  bool   `json:"available"`
	AuthStatus string `json:"auth_status"`
	Model      string `json:"model,omitempty"`
}

type QuotaSnapshot struct {
	AgentID string  `json:"agent_id"`
	Used    float64 `json:"used"`
	Cap     float64 `json:"cap"`
	Pct     float64 `json:"pct"`
	Window  string  `json:"window,omitempty"`
}

type RoutingDecision struct {
	TS         string   `json:"ts"`
	Selected   string   `json:"selected"`
	Considered []string `json:"considered,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

// historyAgents is the allowlist for history.append agent_ids.
// Only these agents may have their history recorded.
var historyAgents = map[string]bool{
	"_echo":   true,
	"claude":  true,
	"codex":   true,
	"copilot": true,
	"gemini":  true,
	"pool":    true,
	"minimax": true,
	"local":   true,
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

// buildStatus returns the current cockpit Status.
func (s *Server) buildStatus() Status {
	s.statusMu.Lock()
	curAgent := s.currentAgent
	s.statusMu.Unlock()
	var activeAgent *string
	if curAgent != "" {
		activeAgent = &curAgent
	}
	r5m := &metrics.Range{From: "-5min"}
	return Status{
		Proto:       ProtoVersion{Major: ProtoMajor, Minor: ProtoMinor},
		ActiveAgent: activeAgent,
		Turn:        0,
		TokensIn:    int(s.sumMetric("tokens_in", r5m)),
		TokensOut:   int(s.sumMetric("tokens_out", r5m)),
		CostUSD:     s.sumMetric("cost_usd", r5m),
		QuotaPct:    0.0,
		Errors5m:    int(s.sumMetric("error_count", r5m)),
	}
}

// buildQuotaSnapshots returns per-agent token/cost usage for the last hour.
// Cap is 0 (unlimited) for all runners until per-agent limits are configured.
func (s *Server) buildQuotaSnapshots() []QuotaSnapshot {
	if s.metrics == nil {
		return nil
	}
	agents := []string{"claude", "codex", "copilot", "gemini", "pool", "minimax", "local"}
	r1h := &metrics.Range{From: "-1h"}
	var out []QuotaSnapshot
	for _, agent := range agents {
		agentCopy := agent
		res, err := s.metrics.RollupGet(metrics.RollupGetParams{
			Metric:  "tokens_in",
			Tier:    "raw",
			Range:   r1h,
			AgentID: &agentCopy,
		})
		if err != nil {
			continue
		}
		var used float64
		for _, b := range res.Buckets {
			used += b.Sum
		}
		if used == 0 {
			continue
		}
		out = append(out, QuotaSnapshot{
			AgentID: agent,
			Used:    used,
			Cap:     0,
			Pct:     0,
			Window:  "1h",
		})
	}
	return out
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
		_ = json.Unmarshal(req.Params, &p)
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
			_ = json.Unmarshal(p.Payload, &anyPayload)
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
	case "security.startup_scan":
		s.securityStartupScan(enc, req)
	case "security.warnings":
		s.securityWarnings(enc, req)
	case "security.mode":
		s.securityMode(enc, req)
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
