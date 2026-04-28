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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraceListCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	traceDir := filepath.Join(home, ".config", "milliways", "traces")
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	for _, name := range []string{"b.jsonl", "a.jsonl"} {
		if err := os.WriteFile(filepath.Join(traceDir, name), []byte("\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	cmd := rootCmd()
	cmd.SetArgs([]string{"trace", "list"})

	stdout, _, err := captureOutput(t, cmd.Execute)
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if got := strings.TrimSpace(stdout); got != "a\nb" {
		t.Fatalf("stdout = %q, want a\\nb", got)
	}
}

func TestTraceShowCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	traceDir := filepath.Join(home, ".config", "milliways", "traces")
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	content := `{"session_id":"demo","timestamp":"2026-04-20T10:00:00Z","type":"delegate","description":"coder-go"}`
	if err := os.WriteFile(filepath.Join(traceDir, "demo.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	cmd := rootCmd()
	cmd.SetArgs([]string{"trace", "show", "demo"})

	stdout, _, err := captureOutput(t, cmd.Execute)
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	for _, want := range []string{"\"session_id\": \"demo\"", "\"type\": \"delegate\"", "\"description\": \"coder-go\""} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestTraceDiagramCommandGraph(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	traceDir := filepath.Join(home, ".config", "milliways", "traces")
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	content := strings.Join([]string{
		`{"session_id":"demo","timestamp":"2026-04-20T10:00:00Z","type":"delegate","description":"coder-go","data":{"to":"delegate: coder-go"}}`,
		`{"session_id":"demo","timestamp":"2026-04-20T10:01:00Z","type":"tool.called","description":"Bash","data":{"tool_name":"Bash"}}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(traceDir, "demo.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	cmd := rootCmd()
	cmd.SetArgs([]string{"trace", "diagram", "demo", "--graph"})

	stdout, _, err := captureOutput(t, cmd.Execute)
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	for _, want := range []string{"flowchart TD", "Orchestrator", "delegate: coder-go", "tool: Bash"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
}
