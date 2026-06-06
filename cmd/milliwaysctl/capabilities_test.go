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

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunCapabilitiesRendersClientToolContract(t *testing.T) {
	sock, calls := startSecurityRPCTestServer(t, map[string]any{
		"capabilities.get": map[string]any{
			"clients": map[string]any{
				"codex": map[string]any{
					"level":          "brokered",
					"controlled_env": true,
					"broker_path":    "/opt/milliways/bin/codex-broker",
					"capabilities": map[string]any{
						"tools":             "brokered",
						"permissions":       "brokered",
						"file_changes":      "brokered",
						"memory":            "runner-controlled",
						"observability":     "runner-controlled",
						"enforcement_level": "brokered",
						"contract": map[string]any{
							"read":              "brokered",
							"write":             "brokered",
							"edit":              "brokered",
							"delete":            "brokered",
							"bash":              "brokered",
							"glob":              "brokered",
							"grep":              "brokered",
							"list_tree":         "brokered",
							"artifacts":         "brokered",
							"approvals":         "brokered",
							"structured_errors": "brokered",
						},
					},
				},
				"minimax": map[string]any{
					"level": "full",
					"capabilities": map[string]any{
						"tools":             "runner-controlled",
						"permissions":       "runner-controlled",
						"file_changes":      "runner-controlled",
						"memory":            "runner-controlled",
						"observability":     "runner-controlled",
						"enforcement_level": "full",
						"contract": map[string]any{
							"read":              "runner-controlled",
							"write":             "runner-controlled",
							"edit":              "runner-controlled",
							"delete":            "runner-controlled",
							"bash":              "runner-controlled",
							"glob":              "runner-controlled",
							"grep":              "runner-controlled",
							"list_tree":         "runner-controlled",
							"artifacts":         "runner-controlled",
							"approvals":         "runner-controlled",
							"structured_errors": "runner-controlled",
						},
					},
				},
			},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runCapabilities([]string{}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runCapabilities rc=%d stderr=%s", rc, stderr.String())
	}
	call := <-calls
	if call.Method != "capabilities.get" {
		t.Fatalf("method = %q, want capabilities.get", call.Method)
	}
	for _, want := range []string{
		"client",
		"broker",
		"available",
		"n/a",
		"codex",
		"brokered",
		"minimax",
		"runner-controlled",
		"write",
		"edit",
		"delete",
		"bash",
		"glob",
		"grep",
		"list_tree",
		"artifacts",
		"approvals",
		"structured_errors",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("capabilities output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunCapabilitiesJSONRendersRawDaemonShape(t *testing.T) {
	sock, _ := startSecurityRPCTestServer(t, map[string]any{
		"capabilities.get": map[string]any{
			"clients": map[string]any{
				"local": map[string]any{"level": "full"},
			},
		},
	})

	var stdout, stderr bytes.Buffer
	if rc := runCapabilities([]string{"--json"}, &stdout, &stderr, sock); rc != 0 {
		t.Fatalf("runCapabilities --json rc=%d stderr=%s", rc, stderr.String())
	}
	for _, want := range []string{`"clients"`, `"local"`, `"level"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("json output missing %q:\n%s", want, stdout.String())
		}
	}
}
