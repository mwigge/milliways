package runners

import "testing"

func TestModelHintUsesConfiguredExternalModel(t *testing.T) {
	t.Setenv("CODEX_MODEL", "gpt-5.5")
	model, source := ModelHint(AgentIDCodex)
	if model != "gpt-5.5" || source != "configured" {
		t.Fatalf("ModelHint(codex) = %q/%q, want configured gpt-5.5", model, source)
	}
}

func TestModelHintDoesNotInventExternalCLIDefault(t *testing.T) {
	t.Setenv("CODEX_MODEL", "")
	t.Setenv("OPENAI_MODEL", "")
	model, source := ModelHint(AgentIDCodex)
	if model != "codex CLI default" || source != "cli-default" {
		t.Fatalf("ModelHint(codex) = %q/%q, want CLI default label", model, source)
	}
}

func TestExtractModelFromJSONLineFindsNestedModel(t *testing.T) {
	line := `{"type":"response.started","response":{"model":"gpt-5.5","id":"resp_1"}}`
	if got := extractModelFromJSONLine(line); got != "gpt-5.5" {
		t.Fatalf("extractModelFromJSONLine = %q, want gpt-5.5", got)
	}
}
