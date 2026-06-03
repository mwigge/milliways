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

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPermissionPolicyModeAwareDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     PermissionMode
		request  ToolPermissionRequest
		decision PermissionDecision
	}{
		{
			name:     "reads allowed by default",
			mode:     PermissionModeDefault,
			request:  ToolPermissionRequest{ToolName: "Read", Operation: OperationRead},
			decision: PermissionAllow,
		},
		{
			name:     "mutations ask by default",
			mode:     PermissionModeDefault,
			request:  ToolPermissionRequest{ToolName: "Write", Operation: OperationWrite, Path: "file.txt"},
			decision: PermissionAsk,
		},
		{
			name:     "bash asks by default",
			mode:     PermissionModeDefault,
			request:  ToolPermissionRequest{ToolName: "Bash", Operation: OperationExecute, Command: "go test ./internal/tools"},
			decision: PermissionAsk,
		},
		{
			name:     "read only denies mutation",
			mode:     PermissionModeReadOnly,
			request:  ToolPermissionRequest{ToolName: "Delete", Operation: OperationDelete, Path: "file.txt"},
			decision: PermissionDeny,
		},
		{
			name:     "auto allows mutation",
			mode:     PermissionModeAuto,
			request:  ToolPermissionRequest{ToolName: "ApplyPatch", Operation: OperationApplyPatch, Path: "file.txt"},
			decision: PermissionAllow,
		},
		{
			name:     "auto asks before delete",
			mode:     PermissionModeAuto,
			request:  ToolPermissionRequest{ToolName: "Delete", Operation: OperationDelete, Path: "file.txt"},
			decision: PermissionAsk,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NewPermissionPolicy(tt.mode).Evaluate(tt.request)
			if got.Decision != tt.decision {
				t.Fatalf("decision = %q, want %q: %s", got.Decision, tt.decision, got.Reason)
			}
		})
	}
}

func TestPermissionPolicyExplicitRuleOverridesDefaults(t *testing.T) {
	t.Parallel()

	policy := NewPermissionPolicy(PermissionModeAuto)
	policy.Rules = append(policy.Rules, PermissionRule{
		ToolName: "Bash",
		Decision: PermissionDeny,
		Reason:   "bash disabled for this runner",
	})

	got := policy.Evaluate(ToolPermissionRequest{ToolName: "Bash", Operation: OperationExecute, Command: "pwd"})
	if got.Decision != PermissionDeny {
		t.Fatalf("decision = %q, want deny", got.Decision)
	}
	if !strings.Contains(got.Reason, "disabled") {
		t.Fatalf("reason = %q", got.Reason)
	}
}

func TestPermissionPolicyDeniesInvalidWorkspacePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", dir)

	policy := NewPermissionPolicy(PermissionModeAuto)
	got := policy.Evaluate(ToolPermissionRequest{
		ToolName:  "Write",
		Operation: OperationWrite,
		Path:      filepath.Join(dir, ".ssh", "id_rsa"),
	})
	if got.Decision != PermissionDeny {
		t.Fatalf("decision = %q, want deny", got.Decision)
	}
}

func TestBuildToolPermissionRequestExtractsMetadata(t *testing.T) {
	t.Parallel()

	req := BuildToolPermissionRequest("Bash", map[string]any{"command": "printf hi"})
	if req.Operation != OperationExecute || req.Command != "printf hi" {
		t.Fatalf("bash request = %+v", req)
	}

	req = BuildToolPermissionRequest("Edit", map[string]any{"path": "a.txt", "old_string": "a", "new_string": "b"})
	if req.Operation != OperationEdit || req.Path != "a.txt" {
		t.Fatalf("edit request = %+v", req)
	}
}

func TestExtractMutationMetadata(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MILLIWAYS_WORKSPACE_ROOT", dir)
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("hello\nworld\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	meta, ok, err := ExtractMutationMetadata("Edit", map[string]any{
		"path":       path,
		"old_string": "world",
		"new_string": "agent",
	})
	if err != nil {
		t.Fatalf("ExtractMutationMetadata() error = %v", err)
	}
	if !ok {
		t.Fatal("ExtractMutationMetadata() ok = false")
	}
	if meta.Operation != OperationEdit {
		t.Fatalf("operation = %q, want edit", meta.Operation)
	}
	if meta.Path != path {
		t.Fatalf("path = %q, want %q", meta.Path, path)
	}
	if !meta.BeforeExists || meta.BeforeHash == "" {
		t.Fatalf("before metadata missing: %+v", meta)
	}
	if !strings.Contains(meta.Preview, "world") || !strings.Contains(meta.Preview, "agent") {
		t.Fatalf("preview = %q", meta.Preview)
	}

	meta, ok, err = ExtractMutationMetadata("ApplyPatch", map[string]any{
		"path": path,
		"diff": "@@\n-world\n+agent\n",
	})
	if err != nil || !ok {
		t.Fatalf("apply patch metadata ok=%v err=%v", ok, err)
	}
	if meta.Operation != OperationApplyPatch || !strings.Contains(meta.Diff, "-world") {
		t.Fatalf("apply patch metadata = %+v", meta)
	}
}
