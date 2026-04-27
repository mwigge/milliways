package metrics

import (
	"fmt"
	"time"
)

// Register declares a metric and its kind. Calling Register on an
// already-registered metric with the same kind is a no-op; with a
// different kind it returns an error to prevent silent reclassification.
func (s *Store) Register(name string, kind Kind) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.kinds[name]; ok {
		if existing != kind {
			return fmt.Errorf("metric %q already registered as %s, refusing to redefine as %s",
				name, existing, kind)
		}
		return nil
	}
	s.kinds[name] = kind
	return nil
}

// Kind returns the registered kind for a metric, or false if unknown.
func (s *Store) Kind(name string) (Kind, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.kinds[name]
	return k, ok
}

// ObserveCounter records a counter increment of `value` for `metric`.
// `agentID` may be empty for daemon-wide metrics. Observations are
// buffered in memory and flushed to samples_raw on the next 1Hz tick.
func (s *Store) ObserveCounter(metric, agentID string, value float64) {
	s.observe(metric, agentID, KindCounter, value)
}

// ObserveHistogram records a single histogram observation.
func (s *Store) ObserveHistogram(metric, agentID string, value float64) {
	s.observe(metric, agentID, KindHistogram, value)
}

// ObserveGauge records the current gauge value.
func (s *Store) ObserveGauge(metric, agentID string, value float64) {
	s.observe(metric, agentID, KindGauge, value)
}

func (s *Store) observe(metric, agentID string, kind Kind, value float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Auto-register on first observation to keep the wiring simple. If
	// the caller already registered with a conflicting kind, drop the
	// observation rather than silently reclassify.
	if existing, ok := s.kinds[metric]; ok {
		if existing != kind {
			return
		}
	} else {
		s.kinds[metric] = kind
	}
	s.pending = append(s.pending, sample{
		metric:  metric,
		agentID: agentID,
		kind:    kind,
		ts:      s.now(),
		value:   value,
	})
}

// FlushNow forces a synchronous flush of pending observations. Useful
// from tests; production code relies on the 1Hz loop.
func (s *Store) FlushNow() error {
	return s.flushPending()
}

// flushPending drains the in-memory buffer and writes the resulting
// per-(metric, agent_id, ts-second) rows to samples_raw. Each
// observation contributes a single row at second-resolution; multiple
// observations within the same second are merged using the kind's
// aggregation rule.
func (s *Store) flushPending() error {
	s.mu.Lock()
	if len(s.pending) == 0 {
		s.mu.Unlock()
		return nil
	}
	pending := s.pending
	s.pending = nil
	s.mu.Unlock()

	// Bucket key: (metric, agent_id, ts-truncated-to-second).
	type bucketKey struct {
		metric, agentID string
		ts              int64
	}
	type bucket struct {
		kind     Kind
		count    int64
		sum      float64
		min, max float64
		// For counters, the running sum is the value. For gauges the
		// running sum is sum-of-values; mean = sum/count. Histograms
		// keep all observations sorted at the end for percentile calc.
		obs []float64
	}
	buckets := make(map[bucketKey]*bucket)
	for _, p := range pending {
		k := bucketKey{
			metric:  p.metric,
			agentID: p.agentID,
			ts:      p.ts.Truncate(time.Second).Unix(),
		}
		b, ok := buckets[k]
		if !ok {
			b = &bucket{kind: p.kind, min: p.value, max: p.value}
			buckets[k] = b
		}
		b.count++
		b.sum += p.value
		if p.value < b.min {
			b.min = p.value
		}
		if p.value > b.max {
			b.max = p.value
		}
		if p.kind == KindHistogram {
			b.obs = append(b.obs, p.value)
		}
	}

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO samples_raw
        (metric, agent_id, ts, value, count, sum, min, max, p50, p95, p99)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for k, b := range buckets {
		var value, p50, p95, p99 float64
		switch b.kind {
		case KindCounter:
			value = b.sum
		case KindGauge:
			value = b.sum / float64(b.count)
		case KindHistogram:
			value = b.sum / float64(b.count) // mean as the single-number summary
			p50 = percentile(b.obs, 0.50)
			p95 = percentile(b.obs, 0.95)
			p99 = percentile(b.obs, 0.99)
		}
		if _, err := stmt.Exec(
			k.metric, k.agentID, k.ts,
			value, b.count, b.sum, b.min, b.max, p50, p95, p99,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	// Best-effort signal to anything waiting on a flush.
	select {
	case s.flushedC <- struct{}{}:
	default:
	}
	return nil
}
