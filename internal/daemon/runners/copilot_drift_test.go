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

func TestCopilotStderrSignalsLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		lines []string
		want  bool
	}{
		{"rate limit", []string{"rate limit reached"}, true},
		{"context window", []string{"context window full"}, true},
		{"context_length", []string{"error: context_length"}, true},
		{"token limit", []string{"token limit exceeded"}, true},
		{"benign", []string{"connecting to github..."}, false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := copilotStderrSignalsLimit(c.lines); got != c.want {
				t.Errorf("copilotStderrSignalsLimit(%v) = %v, want %v", c.lines, got, c.want)
			}
		})
	}
}
