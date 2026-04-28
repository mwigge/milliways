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

package adapter

import (
	"testing"
	"time"
)

func TestParseGeminiQuotaError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		line      string
		wantEvent bool
		wantHours int
	}{
		{
			name:      "valid quota error",
			line:      "TerminalQuotaError: You have exhausted your capacity on this model. Your quota will reset after 11h26m32s.",
			wantEvent: true,
			wantHours: 11,
		},
		{
			name:      "non-quota error",
			line:      "Error: connection refused",
			wantEvent: false,
		},
		{
			name:      "empty line",
			line:      "",
			wantEvent: false,
		},
		{
			name:      "partial match missing seconds",
			line:      "reset after 5h",
			wantEvent: false,
		},
		{
			name:      "zero duration",
			line:      "reset after 0h0m0s",
			wantEvent: true,
			wantHours: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := parseGeminiQuotaError("gemini", tt.line)

			if tt.wantEvent && result == nil {
				t.Fatal("expected event, got nil")
			}
			if !tt.wantEvent && result != nil {
				t.Fatalf("expected nil, got %+v", result)
			}

			if result != nil {
				if result.Type != EventRateLimit {
					t.Errorf("Type = %v, want EventRateLimit", result.Type)
				}
				if result.RateLimit.Status != "exhausted" {
					t.Errorf("Status = %q, want %q", result.RateLimit.Status, "exhausted")
				}
				// Verify ResetsAt is in the future (within reasonable bounds)
				if tt.wantHours > 0 {
					untilReset := time.Until(result.RateLimit.ResetsAt)
					minExpected := time.Duration(tt.wantHours) * time.Hour
					maxExpected := minExpected + 30*time.Minute
					if untilReset < minExpected-time.Minute || untilReset > maxExpected {
						t.Errorf("ResetsAt is %v from now, expected between %v and %v", untilReset, minExpected, maxExpected)
					}
				}
			}
		})
	}
}

func TestGeminiAdapter_Send(t *testing.T) {
	t.Parallel()

	a := NewGeminiAdapter(newTestKitchen("echo"), AdapterOpts{})
	if err := a.Send(nil, "msg"); err != ErrNotInteractive {
		t.Errorf("Send = %v, want ErrNotInteractive", err)
	}
}

func TestGeminiAdapter_Resume(t *testing.T) {
	t.Parallel()

	a := NewGeminiAdapter(newTestKitchen("echo"), AdapterOpts{})
	if a.SupportsResume() {
		t.Error("SupportsResume() = true, want false")
	}
	if a.SessionID() != "" {
		t.Errorf("SessionID() = %q, want empty", a.SessionID())
	}
	caps := a.Capabilities()
	if caps.NativeResume {
		t.Error("Capabilities.NativeResume = true, want false")
	}
	if caps.ExhaustionDetection != "stderr" {
		t.Errorf("Capabilities.ExhaustionDetection = %q, want stderr", caps.ExhaustionDetection)
	}
}
