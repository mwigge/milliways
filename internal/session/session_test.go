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

package session

import (
	"encoding/json"
	"testing"
	"time"
)

func TestToolCallJSONDurationMilliseconds(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(ToolCall{Name: "Read", Duration: 45 * time.Millisecond})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(encoded) == "" || !json.Valid(encoded) {
		t.Fatalf("invalid json: %s", encoded)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded["duration_ms"].(float64) != 45 {
		t.Fatalf("duration_ms = %v, want 45", decoded["duration_ms"])
	}
	var roundTrip ToolCall
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("round trip error = %v", err)
	}
	if roundTrip.Duration != 45*time.Millisecond {
		t.Fatalf("roundTrip.Duration = %s, want 45ms", roundTrip.Duration)
	}
}
