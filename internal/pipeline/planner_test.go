package pipeline

import (
	"testing"
)

func TestParsePlanJSON_Valid(t *testing.T) {
	input := `{"tasks": [{"id": "check-auth", "kitchen": "gemini", "prompt": "Check auth service"}]}`
	plan, err := parsePlanJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(plan.Tasks))
	}
	if plan.Tasks[0].ID != "check-auth" {
		t.Errorf("task ID = %q, want %q", plan.Tasks[0].ID, "check-auth")
	}
	if plan.Tasks[0].Kitchen != "gemini" {
		t.Errorf("task kitchen = %q, want %q", plan.Tasks[0].Kitchen, "gemini")
	}
}

func TestParsePlanJSON_WithCodeFences(t *testing.T) {
	input := "Here is the plan:\n```json\n{\"tasks\": [{\"id\": \"a\", \"kitchen\": \"claude\", \"prompt\": \"do thing\"}]}\n```\n"
	plan, err := parsePlanJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(plan.Tasks))
	}
}

func TestParsePlanJSON_EmptyTasks(t *testing.T) {
	input := `{"tasks": []}`
	plan, err := parsePlanJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Tasks) != 0 {
		t.Fatalf("got %d tasks, want 0", len(plan.Tasks))
	}
}

func TestParsePlanJSON_NoJSON(t *testing.T) {
	input := "This is just text with no JSON"
	_, err := parsePlanJSON(input)
	if err == nil {
		t.Fatal("expected error for non-JSON input")
	}
}

func TestParsePlanJSON_InvalidJSON(t *testing.T) {
	input := `{"tasks": [{"id": "a", "kitchen":}]}`
	_, err := parsePlanJSON(input)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParsePlanJSON_MissingFields(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty id", `{"tasks": [{"id": "", "kitchen": "claude", "prompt": "x"}]}`},
		{"empty kitchen", `{"tasks": [{"id": "a", "kitchen": "", "prompt": "x"}]}`},
		{"empty prompt", `{"tasks": [{"id": "a", "kitchen": "claude", "prompt": ""}]}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parsePlanJSON(tc.input)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestParsePlanJSON_TruncatesExcessTasks(t *testing.T) {
	// Build more than MaxSteps-1 tasks.
	input := `{"tasks": [`
	for i := 0; i < MaxSteps+5; i++ {
		if i > 0 {
			input += ","
		}
		input += `{"id": "t` + string(rune('a'+i)) + `", "kitchen": "claude", "prompt": "task"}`
	}
	input += `]}`

	plan, err := parsePlanJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Tasks) != MaxSteps-1 {
		t.Errorf("got %d tasks, want %d (truncated)", len(plan.Tasks), MaxSteps-1)
	}
}

func TestParsePlanJSON_JSONEmbeddedInText(t *testing.T) {
	input := `Sure! Here is the decomposition:

{"tasks": [{"id": "check-db", "kitchen": "opencode", "prompt": "Check DB schema"}]}

Let me know if you need changes.`

	plan, err := parsePlanJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(plan.Tasks))
	}
}

func TestBuildSummarizePrompt(t *testing.T) {
	steps := []*Step{
		{ID: "check-a", Kitchen: "gemini", Status: StatusDone, Output: "All good in A"},
		{ID: "check-b", Kitchen: "codex", Status: StatusFailed, Output: "Error in B"},
	}

	prompt := BuildSummarizePrompt("check everything", steps)
	if prompt == "" {
		t.Fatal("summarize prompt is empty")
	}

	// Should contain step outputs.
	if !containsStr(prompt, "All good in A") {
		t.Error("prompt missing output from check-a")
	}
	if !containsStr(prompt, "FAILED") {
		t.Error("prompt missing failure indicator")
	}
}

func TestResolveKitchen(t *testing.T) {
	p := &Planner{
		availableKitchens: []string{"claude", "gemini", "opencode"},
		defaultKitchen:    "claude",
	}

	if got := p.resolveKitchen("gemini"); got != "gemini" {
		t.Errorf("resolveKitchen(gemini) = %q, want gemini", got)
	}

	if got := p.resolveKitchen("GEMINI"); got != "gemini" {
		t.Errorf("resolveKitchen(GEMINI) = %q, want gemini (case-insensitive)", got)
	}

	if got := p.resolveKitchen("unknown"); got != "claude" {
		t.Errorf("resolveKitchen(unknown) = %q, want claude (default)", got)
	}
}

func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(haystack) > 0 && findStr(haystack, needle))
}

func findStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
