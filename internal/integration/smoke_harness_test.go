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
