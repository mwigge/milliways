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
