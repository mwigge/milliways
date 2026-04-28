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
