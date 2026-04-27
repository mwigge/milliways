// Package observability holds the daemon's in-memory span ring buffer
// and metric counters. Per design.md Decision 6 + observability-cockpit
// spec, the daemon owns the data; the Rust observability pane subscribes
// and renders it.
//
// We deliberately do NOT install the full OTel SDK here yet. The cockpit
// only needs the ring and a metrics surface; an OTel ExportSpan hook can
// land later as a SpanProcessor that also writes to this ring.
package observability

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Span is a JSON-serializable copy of an OTel-flavoured span. Field tags
// match proto/milliways.json (#/$defs/Span).
type Span struct {
	TraceID      string         `json:"trace_id"`
	SpanID       string         `json:"span_id"`
	ParentSpanID string         `json:"parent_span_id,omitempty"`
	Name         string         `json:"name"`
	StartTS      time.Time      `json:"start_ts"`
	DurationMS   float64        `json:"duration_ms"`
	Status       string         `json:"status"` // "ok" | "error" | "unset"
	Attributes   map[string]any `json:"attributes,omitempty"`
}

// Ring is a fixed-size, thread-safe FIFO of Span values.
type Ring struct {
	mu   sync.RWMutex
	cap  int
	buf  []Span
	idx  int  // next write position
	full bool // true once we've wrapped at least once
}

// NewRing returns a Ring with the given capacity (>= 1).
func NewRing(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{cap: capacity, buf: make([]Span, capacity)}
}

// Push records a Span in the ring, overwriting the oldest if full.
func (r *Ring) Push(s Span) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.idx] = s
	r.idx = (r.idx + 1) % r.cap
	if r.idx == 0 {
		r.full = true
	}
}

// Snapshot returns a copy of the spans currently in the ring, oldest first.
// If `since` is non-zero, only spans with StartTS >= since are returned.
// If `limit` is positive, results are truncated to the most recent `limit`.
func (r *Ring) Snapshot(since time.Time, limit int) []Span {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Span
	if r.full {
		out = make([]Span, 0, r.cap)
		for i := 0; i < r.cap; i++ {
			s := r.buf[(r.idx+i)%r.cap]
			if !since.IsZero() && s.StartTS.Before(since) {
				continue
			}
			out = append(out, s)
		}
	} else {
		out = make([]Span, 0, r.idx)
		for i := 0; i < r.idx; i++ {
			s := r.buf[i]
			if !since.IsZero() && s.StartTS.Before(since) {
				continue
			}
			out = append(out, s)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// NewSpanID returns 8 random bytes hex-encoded — enough for an internal
// span identifier without crypto-strong uniqueness guarantees.
func NewSpanID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// NewTraceID returns 16 random bytes hex-encoded.
func NewTraceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
