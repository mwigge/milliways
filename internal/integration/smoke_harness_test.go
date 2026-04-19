package integration

import (
	"strings"
	"testing"
)

func TestSmokeHarness_IncludesNewScenarioFunctions(t *testing.T) {
	t.Parallel()

	smokeScript := readRepoFile(t, "scripts", "smoke.sh")

	for _, want := range []string{
		"run_scenario_kp22_2",
		"run_scenario_tp9_4",
		"run_scenario_tp9_5",
		"run_scenario_hk5_2",
		"fake-claude-web-search",
		"fake-kitchen-question",
		"fake-claude-streaming",
		"mock-http-server-main.go",
	} {
		if !strings.Contains(smokeScript, want) {
			t.Fatalf("scripts/smoke.sh missing %q", want)
		}
	}
}

func TestSmokeConfigTemplate_IncludesNewSmokeKitchens(t *testing.T) {
	t.Parallel()

	template := readRepoFile(t, "testdata", "smoke", "config", "carte.yaml.tmpl")

	for _, want := range []string{
		"claude-web-search:",
		"claude-streaming:",
		"question-kitchen:",
		"search: claude-web-search",
		"route: claude-streaming",
		"ask: question-kitchen",
	} {
		if !strings.Contains(template, want) {
			t.Fatalf("carte.yaml.tmpl missing %q", want)
		}
	}
}

func TestSmokeMockHTTPServerSource_ContainsStreamingEndpoint(t *testing.T) {
	t.Parallel()

	serverSource := readRepoFile(t, "testdata", "smoke", "bin", "mock-http-server-main.go")

	for _, want := range []string{
		"package main",
		"/v1/chat/completions",
		"text/event-stream",
		"data: [DONE]",
	} {
		if !strings.Contains(serverSource, want) {
			t.Fatalf("mock-http-server-main.go missing %q", want)
		}
	}
}
