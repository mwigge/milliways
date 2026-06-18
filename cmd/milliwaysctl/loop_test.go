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

func TestIsValidChangeName(t *testing.T) {
	tests := []struct {
		name   string
		change string
		want   bool
	}{
		{"valid lowercase kebab", "add-remember-me", true},
		{"valid with digits", "fix-bug-123", true},
		{"valid single segment", "hello", true},
		{"empty string", "", false},
		{"uppercase", "Add-Remember-Me", false},
		{"underscore", "add_remember_me", false},
		{"spaces", "add remember me", false},
		{"leading hyphen", "-add", false},
		{"trailing hyphen", "add-", false},
		{"double hyphen", "add--me", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidChangeName(tt.change)
			if got != tt.want {
				t.Errorf("isValidChangeName(%q) = %v, want %v", tt.change, got, tt.want)
			}
		})
	}
}

func TestPendingTasks(t *testing.T) {
	tmp := t.TempDir()
	tasksPath := filepath.Join(tmp, "tasks.md")

	tests := []struct {
		name     string
		content  string
		wantLen  int
		wantTask string
	}{
		{
			name:     "all complete",
			content:  "# Tasks\n- [x] Implement foo\n- [x] Add tests\n",
			wantLen:  0,
			wantTask: "",
		},
		{
			name:     "one pending",
			content:  "# Tasks\n- [ ] Implement foo\n- [x] Add tests\n",
			wantLen:  1,
			wantTask: "Implement foo",
		},
		{
			name:     "multiple pending",
			content:  "# Tasks\n- [ ] Task one\n- [ ] Task two\n- [x] Task three\n",
			wantLen:  2,
			wantTask: "Task one",
		},
		{
			name:     "empty file",
			content:  "",
			wantLen:  0,
			wantTask: "",
		},
		{
			name:     "no checkboxes",
			content:  "# Tasks\nImplement foo\nAdd tests\n",
			wantLen:  0,
			wantTask: "",
		},
		{
			name:     "whitespace only pending skipped",
			content:  "- [ ]   \n- [x] Done\n",
			wantLen:  0,
			wantTask: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(tasksPath, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("write tasks.md: %v", err)
			}
			got := pendingTasks(tasksPath)
			if len(got) != tt.wantLen {
				t.Errorf("pendingTasks() len = %d, want %d; tasks=%v", len(got), tt.wantLen, got)
			}
			if tt.wantLen > 0 && got[0] != tt.wantTask {
				t.Errorf("pendingTasks()[0] = %q, want %q", got[0], tt.wantTask)
			}
		})
	}
}

func TestContainsTask(t *testing.T) {
	tasks := []string{"Implement foo", "Add tests", "Write docs"}
	tests := []struct {
		name string
		task string
		want bool
	}{
		{"first task", "Implement foo", true},
		{"middle task", "Add tests", true},
		{"last task", "Write docs", true},
		{"missing task", "Delete everything", false},
		{"empty task", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsTask(tasks, tt.task)
			if got != tt.want {
				t.Errorf("containsTask(%q) = %v, want %v", tt.task, got, tt.want)
			}
		})
	}
}

func TestRenderLoopPrompt(t *testing.T) {
	tmp := t.TempDir()
	promptsDir := filepath.Join(tmp, "prompts")
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	testerPath := filepath.Join(promptsDir, "tester.md")
	testerContent := "# Test prompt for {{RUN_ID}}\nTask: {{CURRENT_TASK}}\n"
	if err := os.WriteFile(testerPath, []byte(testerContent), 0o644); err != nil {
		t.Fatalf("write tester.md: %v", err)
	}

	vars := map[string]string{
		"{{RUN_ID}}":       "slice-1",
		"{{CURRENT_TASK}}":  "Add login feature",
		"{{SPEC_CONTEXT}}":  "should not be replaced",
	}

	got := renderLoopPrompt(promptsDir, "tester", vars)
	want := "# Test prompt for slice-1\nTask: Add login feature\n"
	if got != want {
		t.Errorf("renderLoopPrompt = %q, want %q", got, want)
	}
}

func TestRenderLoopPromptMissingFile(t *testing.T) {
	// Should return empty string if the file doesn't exist.
	got := renderLoopPrompt("/nonexistent/prompts", "missing", map[string]string{
		"{{FOO}}": "bar",
	})
	if got != "" {
		t.Errorf("renderLoopPrompt on missing file = %q, want empty string", got)
	}
}

func TestRunVerifyCmd(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		wantCode int
	}{
		{"true command", "true", 0},
		{"false command", "false", 1},
		{"echo", "echo hello", 0},
		{"empty string", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runVerifyCmd(tt.cmd, os.Stderr, os.Stderr)
			if got != tt.wantCode {
				t.Errorf("runVerifyCmd(%q) = %d, want %d", tt.cmd, got, tt.wantCode)
			}
		})
	}
}

func TestGetEnvOr(t *testing.T) {
	t.Setenv("MILLIWAYS_TEST_VAR", "from_env")
	got := getEnvOr("MILLIWAYS_TEST_VAR", "default")
	if got != "from_env" {
		t.Errorf("getEnvOr = %q, want from_env", got)
	}
	got = getEnvOr("MILLIWAYS_TEST_MISSING", "default")
	if got != "default" {
		t.Errorf("getEnvOr missing = %q, want default", got)
	}
}

func TestIntEnvOr(t *testing.T) {
	t.Setenv("MILLIWAYS_TEST_INT", "42")
	got := intEnvOr("MILLIWAYS_TEST_INT", 99)
	if got != 42 {
		t.Errorf("intEnvOr = %d, want 42", got)
	}
	got = intEnvOr("MILLIWAYS_TEST_INT_MISSING", 99)
	if got != 99 {
		t.Errorf("intEnvOr missing = %d, want 99", got)
	}
	t.Setenv("MILLIWAYS_TEST_INT_INVALID", "not_a_number")
	got = intEnvOr("MILLIWAYS_TEST_INT_INVALID", 77)
	if got != 77 {
		t.Errorf("intEnvOr invalid = %d, want 77", got)
	}
}

func TestDefaultLoopConfig(t *testing.T) {
	cfg := defaultLoopConfig("test-change")
	if cfg.change != "test-change" {
		t.Errorf("change = %q, want test-change", cfg.change)
	}
	if cfg.agentID == "" {
		t.Error("agentID should not be empty")
	}
	if cfg.socket == "" {
		t.Error("socket should not be empty")
	}
	if cfg.maxAttempts != 3 {
		t.Errorf("maxAttempts = %d, want 3", cfg.maxAttempts)
	}
	if cfg.timeoutSecs != 1800 {
		t.Errorf("timeoutSecs = %d, want 1800", cfg.timeoutSecs)
	}
	if !cfg.enableReview {
		t.Error("enableReview should be true by default")
	}
}

func TestBuildSpecContext(t *testing.T) {
	tmp := t.TempDir()

	// Create a minimal OpenSpec change dir structure.
	changeDir := filepath.Join(tmp, "openspec", "changes", "test-change")
	if err := os.MkdirAll(filepath.Join(changeDir, "specs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write proposal.md and tasks.md.
	if err := os.WriteFile(filepath.Join(changeDir, "proposal.md"), []byte("## Proposal\nImplement X."), 0o644); err != nil {
		t.Fatalf("write proposal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(changeDir, "tasks.md"), []byte("## Tasks\n- [ ] Task 1\n"), 0o644); err != nil {
		t.Fatalf("write tasks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(changeDir, "specs", "behavior.md"), []byte("## Behavior\nX must Y."), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	// Save cwd and restore after.
	origCwd, _ := os.Getwd()
	defer os.Chdir(origCwd)
	os.Chdir(tmp)

	got := buildSpecContext("openspec", "test-change")
	if !strings.Contains(got, "# proposal.md") {
		t.Error("spec context should contain proposal.md")
	}
	if !strings.Contains(got, "# tasks.md") {
		t.Error("spec context should contain tasks.md")
	}
	// specs/ subdirectory may be walked in any order; check for either form.
	if !strings.Contains(got, "specs") && !strings.Contains(got, "behavior") {
		t.Error("spec context should contain specs/behavior.md or its content")
	}
	if !strings.Contains(got, "Implement X") {
		t.Error("spec context should contain proposal content")
	}
}

func TestRunLoopUsage(t *testing.T) {
	// No args → usage exit code 2.
	got := runLoop(nil, os.Stdout, os.Stderr)
	if got != 2 {
		t.Errorf("runLoop(nil) = %d, want 2", got)
	}
}

func TestRunLoopInvalidChangeName(t *testing.T) {
	got := runLoop([]string{"Invalid-Change"}, os.Stdout, os.Stderr)
	if got != 2 {
		t.Errorf("runLoop([Invalid-Change]) = %d, want 2", got)
	}
}

func TestRunLoopMissingChange(t *testing.T) {
	// Set a fake openspec bin that will fail.
	t.Setenv("OPENSPEC_BIN", "/nonexistent/openspec")
	got := runLoop([]string{"nonexistent-change"}, os.Stdout, os.Stderr)
	if got != 1 {
		t.Errorf("runLoop nonexistent change = %d, want 1", got)
	}
}

func TestRunLoopStatusUsage(t *testing.T) {
	got := runLoopStatus(nil, os.Stdout, os.Stderr)
	if got != 2 {
		t.Errorf("runLoopStatus(nil) = %d, want 2", got)
	}
}

func TestRunLoopStatusInvalidChangeName(t *testing.T) {
	got := runLoopStatus([]string{"Invalid-Change"}, os.Stdout, os.Stderr)
	if got != 2 {
		t.Errorf("runLoopStatus([Invalid-Change]) = %d, want 2", got)
	}
}

func TestRunLoopStatusNoChange(t *testing.T) {
	// Just a flag, no change name.
	got := runLoopStatus([]string{"--path", "/tmp"}, os.Stdout, os.Stderr)
	if got != 2 {
		t.Errorf("runLoopStatus([--path /tmp]) = %d, want 2", got)
	}
}
