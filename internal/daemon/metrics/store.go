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

// Package metrics is the milliwaysd five-tier metrics retention store.
//
// Metrics are observed in-memory and flushed once per second to the
// `samples_raw` table. A 60-second scheduler then runs the demotion
// cascade (raw>60min → hourly>24h → daily>7d → weekly>28d → monthly>12m)
// in a single SQLite write transaction. See
// openspec/changes/milliways-emulator-fork/specs/metrics-rollup/spec.md.
package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Kind classifies a registered metric. The aggregation rule used when
// demoting between tiers depends on the kind:
//
//   - Counter   — higher tier value is the SUM of lower-tier values.
//   - Histogram — count/sum/min/max are exact; p50/p95/p99 are
//     count-weighted averages (approximate above raw).
//   - Gauge     — higher tier value is the count-weighted mean.
type Kind int

const (
	KindCounter Kind = iota
	KindHistogram
	KindGauge
)

// String renders a Kind for JSON encoding.
func (k Kind) String() string {
	switch k {
	case KindCounter:
		return "counter"
	case KindHistogram:
		return "histogram"
	case KindGauge:
		return "gauge"
	default:
		return "unknown"
	}
}

// Store owns the metrics.db connection, the registry of known metrics,
// and the in-memory observation buffer. A Store with the zero
// timezone field defaults to time.Local.
type Store struct {
	conn *sql.DB
	path string

	now func() time.Time // injectable for tests; defaults to time.Now
	tz  *time.Location

	mu       sync.Mutex
	kinds    map[string]Kind
	pending  []sample // observations awaiting flush
	flushedC chan struct{}

	// Background scheduler lifecycle.
	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup
}

// sample is one in-memory observation buffered until the next 1Hz flush.
type sample struct {
	metric  string
	agentID string
	kind    Kind
	ts      time.Time
	// Counter / Gauge: value used directly.
	// Histogram: value is the single observation; we'll aggregate at flush.
	value float64
}

// Open opens or creates ${state}/metrics.db with WAL journaling and
// applies any pending migrations. It does NOT start the scheduler —
// callers who want background flushing/rollup should call Run.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir state dir: %w", err)
	}
	conn, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(1)
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	if err := runMigrations(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	bgCtx, bgCancel := context.WithCancel(context.Background())
	s := &Store{
		conn:     conn,
		path:     path,
		now:      time.Now,
		tz:       time.Local,
		kinds:    make(map[string]Kind),
		bgCtx:    bgCtx,
		bgCancel: bgCancel,
		flushedC: make(chan struct{}, 1),
	}
	return s, nil
}

// Path returns the metrics.db file path.
func (s *Store) Path() string { return s.path }

// SetTimezone overrides the calendar timezone used for daily/weekly/
// monthly bucket alignment. Defaults to time.Local.
func (s *Store) SetTimezone(loc *time.Location) {
	if loc == nil {
		loc = time.Local
	}
	s.mu.Lock()
	s.tz = loc
	s.mu.Unlock()
}

// Run starts the 1Hz flush loop and the 60s rollup scheduler. Idempotent:
// calling Run twice is a no-op. The loops exit when Close is called.
func (s *Store) Run() {
	s.bgWG.Add(2)
	go s.flushLoop()
	go s.rollupLoop()
}

// Close stops the scheduler, flushes any pending samples, and closes the
// underlying SQLite connection.
func (s *Store) Close() error {
	s.bgCancel()
	s.bgWG.Wait()
	// Final synchronous flush so observations made just before shutdown
	// aren't lost.
	if err := s.flushPending(); err != nil {
		slog.Warn("metrics: final flush failed", "err", err)
	}
	return s.conn.Close()
}

// flushLoop ticks at 1Hz and writes any buffered observations to
// samples_raw. Resilient: a single failed flush is logged and retried
// on the next tick.
func (s *Store) flushLoop() {
	defer s.bgWG.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.bgCtx.Done():
			return
		case <-ticker.C:
			if err := s.flushPending(); err != nil {
				slog.Warn("metrics: flush failed", "err", err)
			}
		}
	}
}

// rollupLoop ticks at 60s and runs the full demotion cascade in a
// single write transaction.
func (s *Store) rollupLoop() {
	defer s.bgWG.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.bgCtx.Done():
			return
		case <-ticker.C:
			if err := s.Rollup(); err != nil {
				slog.Warn("metrics: rollup failed", "err", err)
			}
		}
	}
}
