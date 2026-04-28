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

package metrics

import (
	"fmt"
	"strings"
	"time"
)

// RollupGetParams is the wire shape for `metrics.rollup.get`.
type RollupGetParams struct {
	Metric  string  `json:"metric"`
	Tier    string  `json:"tier"`
	Range   *Range  `json:"range,omitempty"`
	AgentID *string `json:"agent_id,omitempty"`
}

// Range bounds a metrics query. Either ISO8601 absolute or a
// relative offset like `-24h`, `-7d`, `-12m`. Empty `From`/`To`
// defaults to the tier's full retention window / now.
type Range struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// Bucket is one row of `metrics.rollup.get` output.
type Bucket struct {
	TS    string  `json:"ts"` // RFC3339 (start of bucket)
	Value float64 `json:"value"`
	Count int64   `json:"count"`
	Sum   float64 `json:"sum"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	P50   float64 `json:"p50,omitempty"`
	P95   float64 `json:"p95,omitempty"`
	P99   float64 `json:"p99,omitempty"`
}

// RollupGetResult is the wire shape returned by `metrics.rollup.get`.
type RollupGetResult struct {
	Metric      string   `json:"metric"`
	Tier        string   `json:"tier"`
	Kind        string   `json:"kind"`
	Buckets     []Bucket `json:"buckets"`
	Approximate bool     `json:"approximate"`
}

// RollupGet executes the `metrics.rollup.get` query against the store.
// Used by the JSON-RPC dispatcher and milliwaysctl.
func (s *Store) RollupGet(p RollupGetParams) (*RollupGetResult, error) {
	tier, ok := tierFromString(p.Tier)
	if !ok {
		return nil, fmt.Errorf("invalid tier %q (expected raw|hourly|daily|weekly|monthly)", p.Tier)
	}
	if p.Metric == "" {
		return nil, fmt.Errorf("metric is required")
	}

	now := s.now()
	from, to, err := s.resolveRange(p.Range, tier, now)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(`SELECT ts, value, count, sum, min, max, p50, p95, p99
        FROM %s
        WHERE metric = ? AND ts >= ? AND ts <= ?`, tableForTier(tier))
	args := []any{p.Metric, from.Unix(), to.Unix()}
	if p.AgentID != nil {
		q += " AND agent_id = ?"
		args = append(args, *p.AgentID)
	}
	q += " ORDER BY ts ASC"

	rows, err := s.conn.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	buckets := make([]Bucket, 0, 64)
	for rows.Next() {
		var b Bucket
		var ts int64
		if err := rows.Scan(&ts, &b.Value, &b.Count, &b.Sum, &b.Min, &b.Max,
			&b.P50, &b.P95, &b.P99); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		b.TS = time.Unix(ts, 0).In(s.tz).Format(time.RFC3339)
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	kind, _ := s.Kind(p.Metric)
	approximate := kind == KindHistogram && tier != TierRaw

	return &RollupGetResult{
		Metric:      p.Metric,
		Tier:        tier.String(),
		Kind:        kind.String(),
		Buckets:     buckets,
		Approximate: approximate,
	}, nil
}

// resolveRange converts a Range (which may have absolute ISO8601 or
// relative `-24h` markers, or be nil) into concrete from/to times.
func (s *Store) resolveRange(r *Range, tier Tier, now time.Time) (time.Time, time.Time, error) {
	if r == nil {
		return s.defaultRangeFor(tier, now), now, nil
	}
	from, err := parseTimeOrOffset(r.From, now, s.defaultRangeFor(tier, now))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("range.from: %w", err)
	}
	to, err := parseTimeOrOffset(r.To, now, now)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("range.to: %w", err)
	}
	return from, to, nil
}

func (s *Store) defaultRangeFor(tier Tier, now time.Time) time.Time {
	switch tier {
	case TierRaw:
		return now.Add(-60 * time.Minute)
	case TierHourly:
		return now.Add(-24 * time.Hour)
	case TierDaily:
		return now.AddDate(0, 0, -7)
	case TierWeekly:
		return now.AddDate(0, 0, -28)
	case TierMonthly:
		return now.AddDate(-1, 0, 0)
	}
	return now
}

// parseTimeOrOffset accepts:
//   - empty string → fallback
//   - RFC3339 timestamp
//   - relative offset like `-24h`, `-7d`, `-12m`, `-30d`, `0` (now)
//
// `m` is interpreted as months when the suffix is `m` and as minutes
// only when the literal `min`/`mins` suffix is used. We standardise on
// the cockpit-friendly `s/m/h/d/w/mo` set:
//
//	s seconds, h hours, d days, w weeks, mo months, y years.
//	m → minutes (legacy short form retained for backward compat).
func parseTimeOrOffset(s string, now, fallback time.Time) (time.Time, error) {
	if s == "" {
		return fallback, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Strip an optional leading sign — we only support `now-Xunit` or
	// `-Xunit` style; positive offsets are accepted but rare.
	sign := -1
	rest := s
	switch {
	case strings.HasPrefix(rest, "-"):
		rest = rest[1:]
	case strings.HasPrefix(rest, "+"):
		sign = 1
		rest = rest[1:]
	}
	// Find the unit suffix.
	type unit struct {
		suffix string
		fn     func(int) time.Duration
		mo, yr int // calendar offsets
	}
	units := []struct {
		suffix string
	}{
		{"mo"}, {"min"}, {"mins"},
		{"s"}, {"m"}, {"h"}, {"d"}, {"w"}, {"y"},
	}
	var matched string
	for _, u := range units {
		if strings.HasSuffix(rest, u.suffix) {
			matched = u.suffix
			break
		}
	}
	if matched == "" {
		return time.Time{}, fmt.Errorf("unrecognised offset %q (use RFC3339 or e.g. -24h, -7d, -12mo)", s)
	}
	numStr := strings.TrimSuffix(rest, matched)
	var n int
	if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil {
		return time.Time{}, fmt.Errorf("offset magnitude %q: %w", numStr, err)
	}
	n *= sign
	switch matched {
	case "s":
		return now.Add(time.Duration(n) * time.Second), nil
	case "min", "mins":
		return now.Add(time.Duration(n) * time.Minute), nil
	case "m":
		// Legacy short form — interpret as minutes. Use `mo` for months.
		return now.Add(time.Duration(n) * time.Minute), nil
	case "h":
		return now.Add(time.Duration(n) * time.Hour), nil
	case "d":
		return now.AddDate(0, 0, n), nil
	case "w":
		return now.AddDate(0, 0, n*7), nil
	case "mo":
		return now.AddDate(0, n, 0), nil
	case "y":
		return now.AddDate(n, 0, 0), nil
	}
	return time.Time{}, fmt.Errorf("unrecognised offset %q", s)
}
