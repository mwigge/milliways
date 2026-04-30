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

package runners

import "testing"

func TestExtractRateLimitEvent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		line      string
		wantOK    bool
		wantStatus string
		wantReset int64
	}{
		{
			name:       "rate_limit_event with status and resetsAt",
			line:       `{"type":"rate_limit_event","rate_limit_info":{"status":"approaching","resetsAt":1735689600}}`,
			wantOK:     true,
			wantStatus: "approaching",
			wantReset:  1735689600,
		},
		{
			name:       "rate_limit_event with status only",
			line:       `{"type":"rate_limit_event","rate_limit_info":{"status":"throttled"}}`,
			wantOK:     true,
			wantStatus: "throttled",
		},
		{
			name:   "non rate_limit_event",
			line:   `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`,
			wantOK: false,
		},
		{
			name:   "rate_limit_event without info block",
			line:   `{"type":"rate_limit_event"}`,
			wantOK: false,
		},
		{
			name:   "malformed",
			line:   `not json`,
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			info, ok := extractRateLimitEvent(c.line)
			if ok != c.wantOK {
				t.Errorf("ok = %v, want %v", ok, c.wantOK)
				return
			}
			if !ok {
				return
			}
			if info.Status != c.wantStatus {
				t.Errorf("status = %q, want %q", info.Status, c.wantStatus)
			}
			if info.ResetsAt != c.wantReset {
				t.Errorf("resetsAt = %d, want %d", info.ResetsAt, c.wantReset)
			}
		})
	}
}

func TestClaudeStderrSignalsLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		lines []string
		want  bool
	}{
		{"context window", []string{"context window exceeded"}, true},
		{"session limit", []string{"session limit reached"}, true},
		{"context_length", []string{"context_length_exceeded"}, true},
		{"too long", []string{"prompt too long"}, true},
		{"benign", []string{"contacting api..."}, false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := claudeStderrSignalsLimit(c.lines); got != c.want {
				t.Errorf("claudeStderrSignalsLimit(%v) = %v, want %v", c.lines, got, c.want)
			}
		})
	}
}

func TestExtractResult_IncludesCacheTokens(t *testing.T) {
	t.Parallel()

	line := `{"type":"result","total_cost_usd":0.0042,"usage":{"input_tokens":120,"output_tokens":80,"cache_read_input_tokens":50,"cache_creation_input_tokens":10}}`
	r, ok := extractResult(line)
	if !ok {
		t.Fatalf("extractResult ok = false")
	}
	if r.cacheReadTokens != 50 {
		t.Errorf("cacheReadTokens = %d, want 50", r.cacheReadTokens)
	}
	if r.cacheWriteTokens != 10 {
		t.Errorf("cacheWriteTokens = %d, want 10", r.cacheWriteTokens)
	}
}
