package metrics

import (
	"path/filepath"
	"testing"
	"time"
)

// fakeClock is a manually-advanced time source. We do not use a global
// because each test gets its own Store, and Store.now is a per-instance
// hook on purpose (also lets tests run in parallel without colliding).
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) Now() time.Time { return c.t }

func newTestStore(t *testing.T) (*Store, *fakeClock) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "metrics.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	clk := &fakeClock{t: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)}
	s.now = clk.Now
	s.SetTimezone(time.UTC) // deterministic boundaries
	return s, clk
}

// countRows returns the number of rows in `tier` matching `metric`.
func countRows(t *testing.T, s *Store, tier Tier, metric string) int {
	t.Helper()
	var n int
	row := s.conn.QueryRow(
		"SELECT COUNT(*) FROM "+tableForTier(tier)+" WHERE metric = ?",
		metric,
	)
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count rows %s: %v", tier, err)
	}
	return n
}

func sumValue(t *testing.T, s *Store, tier Tier, metric string) float64 {
	t.Helper()
	var v float64
	row := s.conn.QueryRow(
		"SELECT COALESCE(SUM(value), 0) FROM "+tableForTier(tier)+" WHERE metric = ?",
		metric,
	)
	if err := row.Scan(&v); err != nil {
		t.Fatalf("sum value %s: %v", tier, err)
	}
	return v
}

// TestSchemaVersion verifies the migration runner installs schema v1.
func TestSchemaVersion(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	v, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if v != 1 {
		t.Errorf("schema version = %d, want 1", v)
	}
}

// TestRegisterRejectsKindChange ensures a metric can't be silently
// reclassified mid-run — that would corrupt aggregation rules.
func TestRegisterRejectsKindChange(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	if err := s.Register("tokens_in", KindCounter); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := s.Register("tokens_in", KindCounter); err != nil {
		t.Errorf("idempotent register should not error: %v", err)
	}
	if err := s.Register("tokens_in", KindGauge); err == nil {
		t.Errorf("kind change should error")
	}
}

// TestObserveCounterFlushesToRaw exercises the in-memory buffer +
// 1Hz flush path and verifies same-second observations merge.
func TestObserveCounterFlushesToRaw(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	if err := s.Register("dispatch_count", KindCounter); err != nil {
		t.Fatalf("register: %v", err)
	}
	for i := 0; i < 5; i++ {
		s.ObserveCounter("dispatch_count", "ping", 1)
	}
	if err := s.FlushNow(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got, want := countRows(t, s, TierRaw, "dispatch_count"), 1; got != want {
		t.Errorf("raw rows = %d, want %d (same-second merge)", got, want)
	}
	if got, want := sumValue(t, s, TierRaw, "dispatch_count"), 5.0; got != want {
		t.Errorf("counter sum = %v, want %v", got, want)
	}
}

// TestRollupCascade2Hours: with raw samples spanning the window
// 3..2h ago and a clock advanced past 60min, the rollup demotes
// raw → hourly. All observations are older than the 60-minute
// retention cutoff so they all migrate.
func TestRollupCascade2Hours(t *testing.T) {
	t.Parallel()
	s, clk := newTestStore(t)
	if err := s.Register("dispatch_count", KindCounter); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Record 10 samples between 3h ago and 2h ago (all > 60min).
	base := clk.t.Add(-3 * time.Hour)
	for i := 0; i < 10; i++ {
		clk.t = base.Add(time.Duration(i) * 6 * time.Minute)
		s.ObserveCounter("dispatch_count", "ping", 1)
	}
	if err := s.FlushNow(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	// Reset clock to "now".
	clk.t = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if err := s.Rollup(); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	if got := countRows(t, s, TierRaw, "dispatch_count"); got != 0 {
		t.Errorf("raw rows after rollup = %d, want 0", got)
	}
	hourly := countRows(t, s, TierHourly, "dispatch_count")
	if hourly < 1 || hourly > 3 {
		t.Errorf("hourly rows = %d, want 1..3", hourly)
	}
	if got, want := sumValue(t, s, TierHourly, "dispatch_count"), 10.0; got != want {
		t.Errorf("counter sum at hourly = %v, want %v", got, want)
	}
}

// TestRollupCascade25Hours: per the spec, the cascade runs all five
// tiers in a single transaction, so a sample old enough to belong at
// `daily` arrives there in one tick — no waiting for tick 2.
func TestRollupCascade25Hours(t *testing.T) {
	t.Parallel()
	s, clk := newTestStore(t)
	if err := s.Register("dispatch_count", KindCounter); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Seed a sample 25 hours in the past — older than the 24h hourly
	// cutoff, so it should cascade raw → hourly → daily in one tick.
	clk.t = time.Date(2026, 1, 14, 11, 0, 0, 0, time.UTC)
	s.ObserveCounter("dispatch_count", "ping", 7)
	if err := s.FlushNow(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	clk.t = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if err := s.Rollup(); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	if got := countRows(t, s, TierRaw, "dispatch_count"); got != 0 {
		t.Errorf("raw after rollup = %d, want 0", got)
	}
	if got := countRows(t, s, TierHourly, "dispatch_count"); got != 0 {
		t.Errorf("hourly after rollup = %d, want 0 (cascaded to daily)", got)
	}
	if got := countRows(t, s, TierDaily, "dispatch_count"); got != 1 {
		t.Errorf("daily = %d, want 1", got)
	}
	if got, want := sumValue(t, s, TierDaily, "dispatch_count"), 7.0; got != want {
		t.Errorf("counter sum at daily = %v, want %v", got, want)
	}
}

// TestRollupCascade8Days: daily → weekly transition.
func TestRollupCascade8Days(t *testing.T) {
	t.Parallel()
	s, clk := newTestStore(t)
	if err := s.Register("dispatch_count", KindCounter); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Insert directly into samples_daily with an old ts so we can test
	// the daily → weekly cascade in isolation.
	old := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -8).Unix()
	if _, err := s.conn.Exec(`INSERT INTO samples_daily
        (metric, agent_id, ts, value, count, sum, min, max, p50, p95, p99)
        VALUES ('dispatch_count', '', ?, 3, 3, 3, 1, 1, 0, 0, 0)`, old); err != nil {
		t.Fatalf("seed daily: %v", err)
	}
	clk.t = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if err := s.Rollup(); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	if got := countRows(t, s, TierDaily, "dispatch_count"); got != 0 {
		t.Errorf("daily after rollup = %d, want 0", got)
	}
	if got := countRows(t, s, TierWeekly, "dispatch_count"); got != 1 {
		t.Errorf("weekly = %d, want 1", got)
	}
	if got, want := sumValue(t, s, TierWeekly, "dispatch_count"), 3.0; got != want {
		t.Errorf("weekly value = %v, want %v", got, want)
	}
}

// TestRollupCascade30Days: weekly → monthly transition.
func TestRollupCascade30Days(t *testing.T) {
	t.Parallel()
	s, clk := newTestStore(t)
	if err := s.Register("dispatch_count", KindCounter); err != nil {
		t.Fatalf("register: %v", err)
	}
	old := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -30).Unix()
	if _, err := s.conn.Exec(`INSERT INTO samples_weekly
        (metric, agent_id, ts, value, count, sum, min, max, p50, p95, p99)
        VALUES ('dispatch_count', '', ?, 11, 11, 11, 1, 1, 0, 0, 0)`, old); err != nil {
		t.Fatalf("seed weekly: %v", err)
	}
	clk.t = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if err := s.Rollup(); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	if got := countRows(t, s, TierWeekly, "dispatch_count"); got != 0 {
		t.Errorf("weekly after rollup = %d, want 0", got)
	}
	if got := countRows(t, s, TierMonthly, "dispatch_count"); got != 1 {
		t.Errorf("monthly = %d, want 1", got)
	}
	if got, want := sumValue(t, s, TierMonthly, "dispatch_count"), 11.0; got != want {
		t.Errorf("monthly value = %v, want %v", got, want)
	}
}

// TestRollupCascade13Months: monthly buckets older than 12 months are deleted.
func TestRollupCascade13Months(t *testing.T) {
	t.Parallel()
	s, clk := newTestStore(t)
	if err := s.Register("dispatch_count", KindCounter); err != nil {
		t.Fatalf("register: %v", err)
	}
	old := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC).AddDate(0, -13, 0).Unix()
	if _, err := s.conn.Exec(`INSERT INTO samples_monthly
        (metric, agent_id, ts, value, count, sum, min, max, p50, p95, p99)
        VALUES ('dispatch_count', '', ?, 99, 99, 99, 1, 1, 0, 0, 0)`, old); err != nil {
		t.Fatalf("seed monthly: %v", err)
	}
	clk.t = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if err := s.Rollup(); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	if got := countRows(t, s, TierMonthly, "dispatch_count"); got != 0 {
		t.Errorf("monthly after expiry = %d, want 0", got)
	}
}

// TestRollupResilientToSkippedTicks: with a 4-hour-wide window of raw
// samples all aged past the 60-min cutoff, a single Rollup catches
// everything up — the spec requires processing all eligible rows,
// not just the last minute's worth.
func TestRollupResilientToSkippedTicks(t *testing.T) {
	t.Parallel()
	s, clk := newTestStore(t)
	if err := s.Register("dispatch_count", KindCounter); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Observe in the window 4h..2h ago (all > 60min cutoff).
	base := clk.t.Add(-4 * time.Hour)
	for i := 0; i < 30; i++ {
		clk.t = base.Add(time.Duration(i) * 4 * time.Minute)
		s.ObserveCounter("dispatch_count", "ping", 1)
	}
	if err := s.FlushNow(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	clk.t = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if err := s.Rollup(); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	if got := countRows(t, s, TierRaw, "dispatch_count"); got != 0 {
		t.Errorf("raw after rollup = %d, want 0", got)
	}
	if got, want := sumValue(t, s, TierHourly, "dispatch_count"), 30.0; got != want {
		t.Errorf("hourly sum = %v, want %v", got, want)
	}
}

// TestHistogramApproximate: percentiles above raw tier are flagged
// approximate via RollupGet.
func TestHistogramApproximate(t *testing.T) {
	t.Parallel()
	s, clk := newTestStore(t)
	if err := s.Register("dispatch_latency_ms", KindHistogram); err != nil {
		t.Fatalf("register: %v", err)
	}
	clk.t = time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC) // 2h ago
	for _, v := range []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10} {
		s.ObserveHistogram("dispatch_latency_ms", "ping", v)
	}
	if err := s.FlushNow(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	clk.t = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if err := s.Rollup(); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	res, err := s.RollupGet(RollupGetParams{
		Metric: "dispatch_latency_ms",
		Tier:   "hourly",
	})
	if err != nil {
		t.Fatalf("rollup.get: %v", err)
	}
	if !res.Approximate {
		t.Errorf("approximate flag = false, want true for histogram above raw")
	}
	if res.Kind != "histogram" {
		t.Errorf("kind = %q, want histogram", res.Kind)
	}
	if len(res.Buckets) == 0 {
		t.Errorf("buckets = 0, want >= 1")
	}
}

// TestRollupGetRawNotApproximate: at the raw tier, percentiles are exact.
func TestRollupGetRawNotApproximate(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	if err := s.Register("dispatch_latency_ms", KindHistogram); err != nil {
		t.Fatalf("register: %v", err)
	}
	for _, v := range []float64{1, 2, 3} {
		s.ObserveHistogram("dispatch_latency_ms", "ping", v)
	}
	if err := s.FlushNow(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	res, err := s.RollupGet(RollupGetParams{
		Metric: "dispatch_latency_ms",
		Tier:   "raw",
	})
	if err != nil {
		t.Fatalf("rollup.get: %v", err)
	}
	if res.Approximate {
		t.Errorf("raw tier should never be approximate")
	}
}

// TestRollupGetInvalidTier rejects unknown tier strings.
func TestRollupGetInvalidTier(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	_, err := s.RollupGet(RollupGetParams{Metric: "x", Tier: "yearly"})
	if err == nil {
		t.Errorf("expected error for invalid tier")
	}
}

// TestParseOffsetForms covers the relative-range parser.
func TestParseOffsetForms(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Time
	}{
		{"-24h", now.Add(-24 * time.Hour)},
		{"-7d", now.AddDate(0, 0, -7)},
		{"-12mo", now.AddDate(0, -12, 0)},
		{"-1y", now.AddDate(-1, 0, 0)},
		{"-30s", now.Add(-30 * time.Second)},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := parseTimeOrOffset(tt.in, now, time.Time{})
			if err != nil {
				t.Fatalf("parse %q: %v", tt.in, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("parseTimeOrOffset(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestBucketStartCalendarBoundaries checks day/week/month alignment.
func TestBucketStartCalendarBoundaries(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	// Tuesday 14:23:45 UTC.
	in := time.Date(2026, 4, 28, 14, 23, 45, 0, time.UTC)
	if got := s.bucketStart(in, TierHourly); got != time.Date(2026, 4, 28, 14, 0, 0, 0, time.UTC) {
		t.Errorf("hourly = %v", got)
	}
	if got := s.bucketStart(in, TierDaily); got != time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC) {
		t.Errorf("daily = %v", got)
	}
	// 2026-04-28 is a Tuesday → Monday is 2026-04-27.
	if got := s.bucketStart(in, TierWeekly); got != time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC) {
		t.Errorf("weekly = %v", got)
	}
	if got := s.bucketStart(in, TierMonthly); got != time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("monthly = %v", got)
	}
}

// TestPercentile spot-checks the nearest-rank algorithm.
func TestPercentile(t *testing.T) {
	t.Parallel()
	xs := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if p := percentile(append([]float64(nil), xs...), 0.5); p != 5 {
		t.Errorf("p50 = %v, want 5", p)
	}
	if p := percentile(append([]float64(nil), xs...), 0.95); p != 9 {
		t.Errorf("p95 = %v, want 9", p)
	}
	if p := percentile(nil, 0.5); p != 0 {
		t.Errorf("p50 of empty = %v, want 0", p)
	}
}
