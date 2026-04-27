package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/mwigge/milliways/internal/daemon/metrics"
	"github.com/mwigge/milliways/internal/daemon/observability"
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

// buildStatus returns the current cockpit Status. Stub implementation
// until TASK-1.4 wires real session/quota/error state.
func (s *Server) buildStatus() Status {
	return Status{
		Proto:       ProtoVersion{Major: ProtoMajor, Minor: ProtoMinor},
		ActiveAgent: nil,
		Turn:        0,
		TokensIn:    0,
		TokensOut:   0,
		CostUSD:     0.0,
		QuotaPct:    0.0,
		Errors5m:    0,
	}
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
			Version: "0.0.1",
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
		writeResult(enc, req.ID, s.agentsCache)
	case "agent.open":
		s.agentOpen(enc, req)
	case "agent.send":
		s.agentSend(enc, req)
	case "agent.stream":
		s.agentStream(enc, req)
	case "agent.close":
		s.agentClose(enc, req)
	case "context.get":
		s.contextGet(enc, req)
	case "context.get_all":
		s.contextGetAll(enc, req)
	case "context.subscribe":
		s.contextSubscribe(enc, req)
	case "quota.get":
		writeResult(enc, req.ID, []QuotaSnapshot{})
	case "routing.peek":
		writeResult(enc, req.ID, []RoutingDecision{})
	case "observability.spans":
		var p observabilitySpansParams
		_ = json.Unmarshal(req.Params, &p)
		writeResult(enc, req.ID, s.spans.Snapshot(p.parsedSince(), p.Limit))
	case "observability.metrics":
		// Stub — populated when TASK-5.5 metrics-rollup lands.
		writeResult(enc, req.ID, map[string]any{"buckets": []any{}})
	case "metrics.rollup.get":
		s.metricsRollupGet(enc, req)
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
