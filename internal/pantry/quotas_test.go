package pantry

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRemaining(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dailyLimit int
		dispatches int
		want       int
	}{
		{name: "unlimited returns negative one", dailyLimit: 0, dispatches: 5, want: -1},
		{name: "unlimited with no data", dailyLimit: 0, dispatches: 0, want: -1},
		{name: "partial usage", dailyLimit: 50, dispatches: 12, want: 38},
		{name: "at limit", dailyLimit: 50, dispatches: 50, want: 0},
		{name: "over limit", dailyLimit: 50, dispatches: 55, want: 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := openTestDB(t)
			store := db.Quotas()

			for range tt.dispatches {
				if err := store.Increment("claude", 1, false); err != nil {
					t.Fatalf("Increment: %v", err)
				}
			}

			got, err := store.Remaining("claude", tt.dailyLimit)
			if err != nil {
				t.Fatalf("Remaining: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Remaining() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTrend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		today      int
		yesterday  int
		wantPrefix string
		wantExact  string
	}{
		{name: "no data returns empty"},
		{name: "trending up", today: 12, yesterday: 6, wantPrefix: "↑"},
		{name: "trending down", today: 6, yesterday: 12, wantPrefix: "↓"},
		{name: "flat", today: 10, yesterday: 10, wantExact: "±0%"},
		{name: "new up", today: 4, yesterday: 0, wantExact: "↑new"},
		{name: "new down", today: 0, yesterday: 4, wantExact: "↓new"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := openTestDB(t)
			now := time.Now().UTC()
			currentHour := now.Hour()
			if currentHour == 0 {
				currentHour = 1
			}

			seedLedgerEntries(t, db, "claude", now.Format("2006-01-02"), tt.today, currentHour)
			seedLedgerEntries(t, db, "claude", now.AddDate(0, 0, -1).Format("2006-01-02"), tt.yesterday, currentHour)

			got, err := db.Quotas().Trend("claude")
			if err != nil {
				t.Fatalf("Trend: %v", err)
			}

			switch {
			case tt.wantExact != "":
				if got != tt.wantExact {
					t.Fatalf("Trend() = %q, want %q", got, tt.wantExact)
				}
			case tt.wantPrefix != "":
				if !strings.HasPrefix(got, tt.wantPrefix) {
					t.Fatalf("Trend() = %q, want prefix %q", got, tt.wantPrefix)
				}
			default:
				if got != "" {
					t.Fatalf("Trend() = %q, want empty", got)
				}
			}
		})
	}
}

func TestResetsAt(t *testing.T) {
	t.Parallel()

	t.Run("returns zero time for unlimited kitchen", func(t *testing.T) {
		t.Parallel()

		db := openTestDB(t)
		got, err := db.Quotas().ResetsAt("claude", 0)
		if err != nil {
			t.Fatalf("ResetsAt: %v", err)
		}
		if !got.IsZero() {
			t.Fatalf("ResetsAt() = %v, want zero time", got)
		}
	})

	t.Run("returns override while active", func(t *testing.T) {
		t.Parallel()

		db := openTestDB(t)
		want := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
		if err := db.Quotas().MarkExhausted("claude", want); err != nil {
			t.Fatalf("MarkExhausted: %v", err)
		}

		got, err := db.Quotas().ResetsAt("claude", 10)
		if err != nil {
			t.Fatalf("ResetsAt: %v", err)
		}
		if !got.Equal(want) {
			t.Fatalf("ResetsAt() = %v, want %v", got, want)
		}
	})
}

func seedLedgerEntries(t *testing.T, db *DB, kitchen, date string, count, currentHour int) {
	t.Helper()

	for i := 0; i < count; i++ {
		hour := 0
		if currentHour > 1 {
			hour = i % currentHour
		}
		ts := date + "T" + twoDigit(hour) + ":00:00Z"
		_, err := db.Ledger().Insert(LedgerEntry{
			Timestamp: ts,
			TaskHash:  kitchen + "-" + date + "-" + twoDigit(i),
			Kitchen:   kitchen,
			Outcome:   "success",
		})
		if err != nil {
			t.Fatalf("Insert ledger entry: %v", err)
		}
	}
}

func twoDigit(v int) string {
	return fmt.Sprintf("%02d", v)
}
