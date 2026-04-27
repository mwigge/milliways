package daemon

import (
	"encoding/json"
	"time"

	"github.com/mwigge/milliways/internal/daemon/metrics"
)

// JSON-RPC method handlers for the observability.* surface.
//
// observability.subscribe   — server-pushed stream of recent spans (1 Hz).
// observability.metrics     — one-shot fetch of the four core dashboard
//                             metrics for a tier/range, returned as a
//                             single combined map. The plotters renderer
//                             (Phase 6 follow-up) will call this on every
//                             repaint.

// observabilitySubscribeParams is the wire shape for observability.subscribe.
// `Since` and `Limit` only affect the first emission; subsequent ticks
// always use the rolling 60s / top-50 window (matches the cockpit budget).
type observabilitySubscribeParams struct {
	Since string `json:"since,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

func (p observabilitySubscribeParams) parsedSince() time.Time {
	if p.Since == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, p.Since)
	if err != nil {
		return time.Time{}
	}
	return t
}

// observabilitySubscribeTickInterval controls the cockpit's 1Hz cadence.
// Variable (not const) so tests can override it without flake.
var observabilitySubscribeTickInterval = 1 * time.Second

// observabilitySubscribe allocates a Stream and returns its id; the
// background goroutine pushes a {t:"data", spans:[...]} frame at the
// configured cadence. Each tick queries the in-memory ring for spans
// from the last 60s, capped at 50. The first frame is emitted
// synchronously after the result write so clients see data without
// waiting a full tick.
func (s *Server) observabilitySubscribe(enc *json.Encoder, req *Request) {
	var p observabilitySubscribeParams
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &p)
	}
	stream := s.streams.Allocate()
	writeResult(enc, req.ID, map[string]any{
		"stream_id":     stream.ID,
		"output_offset": int64(0),
	})

	// Initial frame: respect the caller's `since`/`limit` so they can
	// hydrate from beyond the rolling window if they want to. After the
	// first emission we lock to the rolling 60s/top-50 cockpit window.
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	since := p.parsedSince()
	if since.IsZero() {
		since = time.Now().Add(-60 * time.Second)
	}

	go s.observabilitySubscribeLoop(stream, since, limit)
}

// observabilitySubscribeLoop pumps frames into stream until the daemon
// shuts down or the sidecar drops. The first frame uses the
// caller-supplied window; subsequent frames use the rolling 60s/top-50.
func (s *Server) observabilitySubscribeLoop(stream *Stream, firstSince time.Time, firstLimit int) {
	push := func(since time.Time, limit int) {
		spans := s.spans.Snapshot(since, limit)
		stream.Push(map[string]any{"t": "data", "spans": spans})
	}

	push(firstSince, firstLimit)

	ticker := time.NewTicker(observabilitySubscribeTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.bgCtx.Done():
			return
		case <-ticker.C:
			push(time.Now().Add(-60*time.Second), 50)
		}
	}
}

// observabilityMetricsParams selects the tier and time range for the
// combined metrics fetch. `Bucket` is reserved for future
// downsample-on-read; today we always return native tier buckets.
type observabilityMetricsParams struct {
	Range  *metrics.Range `json:"range,omitempty"`
	Tier   string         `json:"tier,omitempty"`
	Bucket string         `json:"bucket,omitempty"`
}

// observabilityCoreMetrics is the fixed set of dashboard metrics. They
// are returned as a single map keyed by metric name so the cockpit can
// render the four panels from one RPC call rather than four.
var observabilityCoreMetrics = []string{
	"dispatch_count",
	"dispatch_latency_ms",
	"error_count",
	"cost_usd",
}

// observabilityMetrics returns the four core dashboard metrics for the
// requested tier (default `raw`). If the metrics store is unavailable or
// a metric isn't registered yet, that metric's entry contains an empty
// `buckets` array — the cockpit treats it as "no data" rather than an
// error so the pane stays renderable in early-startup.
func (s *Server) observabilityMetrics(enc *json.Encoder, req *Request) {
	var p observabilityMetricsParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, "invalid observability.metrics params: "+err.Error())
			return
		}
	}
	tier := p.Tier
	if tier == "" {
		tier = "raw"
	}

	out := make(map[string]any, len(observabilityCoreMetrics))
	for _, name := range observabilityCoreMetrics {
		out[name] = s.observabilityFetchMetric(name, tier, p.Range)
	}
	writeResult(enc, req.ID, out)
}

// observabilityFetchMetric returns a metrics.RollupGetResult for the
// named metric, or a sentinel empty result if the store is unavailable
// or the call errors. The wire shape stays consistent regardless of the
// underlying state.
func (s *Server) observabilityFetchMetric(metric string, tier string, r *metrics.Range) any {
	empty := map[string]any{
		"metric":      metric,
		"tier":        tier,
		"kind":        "",
		"buckets":     []any{},
		"approximate": false,
	}
	if s.metrics == nil {
		return empty
	}
	res, err := s.metrics.RollupGet(metrics.RollupGetParams{
		Metric: metric,
		Tier:   tier,
		Range:  r,
	})
	if err != nil {
		return empty
	}
	return res
}
