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
	"sort"
	"time"
)

// Tier identifies one of the five retention tiers.
type Tier int

const (
	TierRaw Tier = iota
	TierHourly
	TierDaily
	TierWeekly
	TierMonthly
)

// String returns the lower-case tier name as used in `metrics.rollup.get`
// requests and the SQLite table suffix.
func (t Tier) String() string {
	switch t {
	case TierRaw:
		return "raw"
	case TierHourly:
		return "hourly"
	case TierDaily:
		return "daily"
	case TierWeekly:
		return "weekly"
	case TierMonthly:
		return "monthly"
	default:
		return "unknown"
	}
}

func tierFromString(s string) (Tier, bool) {
	switch s {
	case "raw":
		return TierRaw, true
	case "hourly":
		return TierHourly, true
	case "daily":
		return TierDaily, true
	case "weekly":
		return TierWeekly, true
	case "monthly":
		return TierMonthly, true
	default:
		return 0, false
	}
}

func tableForTier(t Tier) string {
	switch t {
	case TierRaw:
		return "samples_raw"
	case TierHourly:
		return "samples_hourly"
	case TierDaily:
		return "samples_daily"
	case TierWeekly:
		return "samples_weekly"
	case TierMonthly:
		return "samples_monthly"
	default:
		return ""
	}
}

// percentile returns the p-th percentile (0..1) using nearest-rank.
// Returns 0 for an empty slice. Modifies its input by sorting.
func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sort.Float64s(xs)
	if p <= 0 {
		return xs[0]
	}
	if p >= 1 {
		return xs[len(xs)-1]
	}
	idx := int(float64(len(xs)-1) * p)
	return xs[idx]
}

// bucketStart returns the start of the bucket containing ts, in the
// store's configured timezone.
func (s *Store) bucketStart(ts time.Time, tier Tier) time.Time {
	t := ts.In(s.tz)
	switch tier {
	case TierRaw:
		return t.Truncate(time.Second)
	case TierHourly:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, s.tz)
	case TierDaily:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, s.tz)
	case TierWeekly:
		// ISO week starts Monday. Step back to Monday of this week.
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, s.tz)
		offset := (int(day.Weekday()) + 6) % 7 // Mon=0, Sun=6
		return day.AddDate(0, 0, -offset)
	case TierMonthly:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, s.tz)
	default:
		return t
	}
}

// retentionCutoff returns the timestamp before which rows in `tier`
// SHALL be demoted to the next tier (or deleted, if monthly).
//
// Per the spec:
//
//	raw     → 60 minutes
//	hourly  → 24 hours
//	daily   → 7 days
//	weekly  → 28 days
//	monthly → 12 months
func (s *Store) retentionCutoff(tier Tier, now time.Time) time.Time {
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
	default:
		return now
	}
}

// nextTier returns the tier that `tier` demotes into, and a flag
// indicating whether there is a next tier (false for monthly).
func nextTier(tier Tier) (Tier, bool) {
	switch tier {
	case TierRaw:
		return TierHourly, true
	case TierHourly:
		return TierDaily, true
	case TierDaily:
		return TierWeekly, true
	case TierWeekly:
		return TierMonthly, true
	default:
		return 0, false
	}
}
