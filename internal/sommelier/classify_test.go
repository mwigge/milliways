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

package sommelier

import "testing"

func TestClassifyTaskType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		prompt string
		want   string
	}{
		{"explain the auth flow", "think"},
		{"code a rate limiter", "code"},
		{"refactor the store module", "refactor"},
		{"search for DORA regulations", "search"},
		{"review the security changes", "review"},
		{"write tests for the handler", "test"},
		{"implement JWT middleware", "code"},
		{"plan the migration strategy", "think"},
		{"find all references to store.py", "search"},
		{"fix the broken endpoint", "code"},
		{"add rate limiting to the API", "code"},
		{"why does this fail", "think"},
		{"compare DuckDB vs SQLite", "search"},
		{"audit the auth module", "review"},
		{"something completely unrelated xyz", "general"},
	}
	for _, tt := range tests {
		t.Run(tt.prompt, func(t *testing.T) {
			t.Parallel()
			got := ClassifyTaskType(tt.prompt)
			if got != tt.want {
				t.Errorf("ClassifyTaskType(%q) = %q, want %q", tt.prompt, got, tt.want)
			}
		})
	}
}
