package memory

import "testing"

func TestTokenTrackerTotals(t *testing.T) {
	t.Parallel()

	tracker := &TokenTracker{}
	tracker.Add(10, 20)
	tracker.Add(3, 4)
	input, output := tracker.Totals()
	if input != 13 || output != 24 {
		t.Fatalf("Totals() = (%d, %d), want (13, 24)", input, output)
	}
}
