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
	"database/sql"
	"fmt"
	"time"
)

// Rollup runs the demotion cascade across all five tiers in a single
// SQLite write transaction. Per the spec, this guarantees that readers
// (e.g. `metrics.rollup.get`) never see a partially-aggregated bucket —
// neither cross-tier nor within-tier.
//
// The cascade is resilient to skipped ticks: each invocation processes
// ALL rows older than the retention cutoff, not just rows that aged
// out in the last minute. A laptop waking from a 2-hour sleep gets
// caught up on the next tick.
func (s *Store) Rollup() error {
	now := s.now()

	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		// If we return early via error, this rollback is the safety
		// net. On success Commit has already drained the tx.
		_ = tx.Rollback()
	}()

	// Process tiers in order so a row can cascade through multiple
	// tiers in a single tick if the daemon was paused for a long time.
	for _, t := range []Tier{TierRaw, TierHourly, TierDaily, TierWeekly} {
		if err := s.demoteTier(tx, t, now); err != nil {
			return fmt.Errorf("demote %s: %w", t, err)
		}
	}
	// Monthly: delete only — no further tier.
	if err := s.expireMonthly(tx, now); err != nil {
		return fmt.Errorf("expire monthly: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// demoteTier moves all rows in `from` whose ts is older than the
// retention cutoff into the next tier, aggregated per the kind rule,
// then deletes the source rows.
func (s *Store) demoteTier(tx *sql.Tx, from Tier, now time.Time) error {
	to, ok := nextTier(from)
	if !ok {
		return nil
	}
	cutoff := s.retentionCutoff(from, now).Unix()

	rows, err := tx.Query(
		fmt.Sprintf(`SELECT metric, agent_id, ts, value, count, sum, min, max, p50, p95, p99
		             FROM %s WHERE ts < ?`, tableForTier(from)),
		cutoff,
	)
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}

	type srcRow struct {
		metric, agentID            string
		ts                         int64
		value                      float64
		count                      int64
		sum, mn, mx, p50, p95, p99 float64
	}
	type aggKey struct {
		metric, agentID string
		bucketTS        int64
	}
	type agg struct {
		count       int64
		sum, mn, mx float64
		// Histogram percentile inputs (count-weighted average).
		p50WeightedNum, p95WeightedNum, p99WeightedNum float64
		// First-row sentinel so min/max init correctly.
		seen bool
	}

	pending := make(map[aggKey]*agg)
	for rows.Next() {
		var r srcRow
		if err := rows.Scan(&r.metric, &r.agentID, &r.ts, &r.value,
			&r.count, &r.sum, &r.mn, &r.mx, &r.p50, &r.p95, &r.p99); err != nil {
			rows.Close()
			return fmt.Errorf("scan: %w", err)
		}
		bucketTS := s.bucketStart(time.Unix(r.ts, 0), to).Unix()
		k := aggKey{metric: r.metric, agentID: r.agentID, bucketTS: bucketTS}
		a, ok := pending[k]
		if !ok {
			a = &agg{mn: r.mn, mx: r.mx}
			pending[k] = a
		}
		// raw rows have count=1 by virtue of the flush logic; for
		// already-rolled-up sources count reflects sample volume.
		c := r.count
		if c == 0 {
			c = 1
		}
		a.count += c
		a.sum += r.sum
		if !a.seen || r.mn < a.mn {
			a.mn = r.mn
		}
		if !a.seen || r.mx > a.mx {
			a.mx = r.mx
		}
		a.seen = true
		// count-weighted percentile contribution.
		w := float64(c)
		a.p50WeightedNum += r.p50 * w
		a.p95WeightedNum += r.p95 * w
		a.p99WeightedNum += r.p99 * w
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("rows iter: %w", err)
	}
	rows.Close()

	// Merge each pending aggregate into any existing destination bucket
	// (read-modify-write within the same tx so we never observe a
	// partial state). The kind drives how `value` and percentiles are
	// recomputed; the count/sum/min/max columns are merged exactly.
	selStmt, err := tx.Prepare(fmt.Sprintf(`SELECT count, sum, min, max, p50, p95, p99
            FROM %s WHERE metric = ? AND agent_id = ? AND ts = ?`, tableForTier(to)))
	if err != nil {
		return fmt.Errorf("prepare select dst: %w", err)
	}
	defer selStmt.Close()

	upsert, err := tx.Prepare(fmt.Sprintf(`INSERT INTO %s
        (metric, agent_id, ts, value, count, sum, min, max, p50, p95, p99)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(metric, agent_id, ts) DO UPDATE SET
            value = excluded.value,
            count = excluded.count,
            sum   = excluded.sum,
            min   = excluded.min,
            max   = excluded.max,
            p50   = excluded.p50,
            p95   = excluded.p95,
            p99   = excluded.p99`, tableForTier(to)))
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer upsert.Close()

	for k, a := range pending {
		// Read any existing bucket so we can merge.
		var existing struct {
			count         int64
			sum, mn, mx   float64
			p50, p95, p99 float64
			present       bool
		}
		row := selStmt.QueryRow(k.metric, k.agentID, k.bucketTS)
		if err := row.Scan(
			&existing.count, &existing.sum, &existing.mn, &existing.mx,
			&existing.p50, &existing.p95, &existing.p99,
		); err == nil {
			existing.present = true
		} else if err != sql.ErrNoRows {
			return fmt.Errorf("read existing dst: %w", err)
		}

		mergedCount := a.count + existing.count
		mergedSum := a.sum + existing.sum
		mergedMin := a.mn
		mergedMax := a.mx
		if existing.present {
			if existing.mn < mergedMin {
				mergedMin = existing.mn
			}
			if existing.mx > mergedMax {
				mergedMax = existing.mx
			}
		}
		// count-weighted percentile merge: (incoming weighted num + existing*count) / total
		// where the incoming weighted numerator was accumulated above.
		mergedP50num := a.p50WeightedNum + existing.p50*float64(existing.count)
		mergedP95num := a.p95WeightedNum + existing.p95*float64(existing.count)
		mergedP99num := a.p99WeightedNum + existing.p99*float64(existing.count)

		kind, kok := s.Kind(k.metric)
		if !kok {
			kind = KindCounter
		}
		var value, p50, p95, p99 float64
		switch kind {
		case KindCounter:
			value = mergedSum
		case KindGauge:
			value = mergedSum / float64(max64(mergedCount, 1))
		case KindHistogram:
			value = mergedSum / float64(max64(mergedCount, 1))
			w := float64(max64(mergedCount, 1))
			p50 = mergedP50num / w
			p95 = mergedP95num / w
			p99 = mergedP99num / w
		}
		if _, err := upsert.Exec(
			k.metric, k.agentID, k.bucketTS,
			value, mergedCount, mergedSum, mergedMin, mergedMax, p50, p95, p99,
		); err != nil {
			return fmt.Errorf("upsert: %w", err)
		}
	}

	if _, err := tx.Exec(
		fmt.Sprintf("DELETE FROM %s WHERE ts < ?", tableForTier(from)),
		cutoff,
	); err != nil {
		return fmt.Errorf("delete src: %w", err)
	}
	return nil
}

// expireMonthly drops monthly buckets older than the 12-month window.
func (s *Store) expireMonthly(tx *sql.Tx, now time.Time) error {
	cutoff := s.retentionCutoff(TierMonthly, now).Unix()
	if _, err := tx.Exec("DELETE FROM samples_monthly WHERE ts < ?", cutoff); err != nil {
		return fmt.Errorf("delete monthly: %w", err)
	}
	return nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
