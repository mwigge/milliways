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

package quarantine_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/security/quarantine"
)

func TestPlanActionsFindsDryRunActions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, ".claude/hooks.js", "console.log('hook')\n")
	writeFile(t, root, ".vscode/tasks.json", `{
  "version": "2.0.0",
  "tasks": [
    {"label": "agent-start", "type": "shell", "command": "node setup.mjs", "runOptions": {"runOn": "folderOpen"}}
  ]
}`)
	systemd := filepath.Join(root, "systemd")
	launchAgents := filepath.Join(root, "launch-agents")
	writeFile(t, systemd, "gh-token-monitor.service", "[Service]\nExecStart=gh-token-monitor\n")
	writeFile(t, launchAgents, "com.user.gh-token-monitor.plist", "<plist><string>gh-token-monitor</string></plist>\n")

	plan, err := quarantine.PlanActions(context.Background(), quarantine.Options{
		WorkspaceRoot: root,
		SystemdRoots: []quarantine.Root{
			{Name: "systemd", Path: systemd},
		},
		LaunchAgentRoots: []quarantine.Root{
			{Name: "launch-agents", Path: launchAgents},
		},
		Now: fixedTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 4 {
		t.Fatalf("actions = %d, want 4: %#v", len(plan.Actions), plan.Actions)
	}

	byKind := actionsByKind(plan.Actions)
	move := byKind[quarantine.ActionMoveToQuarantine]
	if move.SourcePath != filepath.Join(root, ".claude", "hooks.js") {
		t.Fatalf("claude source = %q", move.SourcePath)
	}
	if !strings.HasSuffix(move.DestinationPath, filepath.Join(".milliways", "quarantine", "20260514T120000Z", ".claude", "hooks.js")) {
		t.Fatalf("claude destination = %q", move.DestinationPath)
	}
	if move.ApplyRequired {
		t.Fatal("move action should not require explicit apply confirmation")
	}
	assertHash(t, move.Hash)

	vscode := byKind[quarantine.ActionDisableVSCodeFolderOpen]
	if vscode.SourcePath != filepath.Join(root, ".vscode", "tasks.json") {
		t.Fatalf("vscode source = %q", vscode.SourcePath)
	}
	if !strings.HasSuffix(vscode.DestinationPath, filepath.Join(".vscode", "tasks.json.bak")) {
		t.Fatalf("vscode backup = %q", vscode.DestinationPath)
	}
	if got := vscode.AdditionalFields["tasks"]; got != "agent-start" {
		t.Fatalf("vscode task labels = %q", got)
	}

	if !byKind[quarantine.ActionDisableSystemdUnit].ApplyRequired {
		t.Fatal("systemd disable should require explicit confirmation")
	}
	if !byKind[quarantine.ActionDisableLaunchAgent].ApplyRequired {
		t.Fatal("LaunchAgent disable should require explicit confirmation")
	}
}

func TestPlanActionsRequiresWorkspaceRoot(t *testing.T) {
	t.Parallel()

	_, err := quarantine.PlanActions(context.Background(), quarantine.Options{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPlanActionsIgnoresCleanWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, ".claude/settings.json", "{}\n")
	writeFile(t, root, ".vscode/tasks.json", `{"tasks":[{"label":"manual","runOptions":{"runOn":"default"}}]}`)

	plan, err := quarantine.PlanActions(context.Background(), quarantine.Options{
		WorkspaceRoot: root,
		Now:           fixedTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("expected no actions, got %#v", plan.Actions)
	}
}

func TestApplyPlanMovesClaudeExecutableToQuarantine(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, ".claude/hooks/setup.mjs", "console.log('setup')\n")

	plan, err := quarantine.PlanActions(context.Background(), quarantine.Options{
		WorkspaceRoot: root,
		Now:           fixedTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := quarantine.ApplyPlan(context.Background(), plan, quarantine.ApplyOptions{Now: fixedTime})
	if err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("applied actions = %d, want 1", len(result.Actions))
	}
	action := result.Actions[0]
	if action.Status != quarantine.ApplyStatusApplied {
		t.Fatalf("status = %q, want applied: %#v", action.Status, action)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/hooks/setup.mjs")); !os.IsNotExist(err) {
		t.Fatalf("source still exists or stat failed unexpectedly: %v", err)
	}
	got, err := os.ReadFile(action.DestinationPath)
	if err != nil {
		t.Fatalf("read quarantined file: %v", err)
	}
	if string(got) != "console.log('setup')\n" {
		t.Fatalf("quarantined contents = %q", got)
	}
	assertHash(t, action.AppliedHash)
	if action.AppliedHash != action.Hash {
		t.Fatalf("applied hash = %q, want original hash %q", action.AppliedHash, action.Hash)
	}
}

func TestApplyPlanRefusesChangedSourceHash(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, ".claude/hooks.js", "console.log('before')\n")
	plan, err := quarantine.PlanActions(context.Background(), quarantine.Options{
		WorkspaceRoot: root,
		Now:           fixedTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".claude/hooks.js", "console.log('after')\n")

	result, err := quarantine.ApplyPlan(context.Background(), plan, quarantine.ApplyOptions{Now: fixedTime})
	if err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	action := actionsByApplyKind(result.Actions)[quarantine.ActionMoveToQuarantine]
	if action.Status != quarantine.ApplyStatusFailed {
		t.Fatalf("status = %q, want failed: %#v", action.Status, action)
	}
	if !strings.Contains(action.Error, "source hash changed") {
		t.Fatalf("error = %q, want hash mismatch", action.Error)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/hooks.js")); err != nil {
		t.Fatalf("changed source should remain present: %v", err)
	}
	if _, err := os.Stat(action.DestinationPath); !os.IsNotExist(err) {
		t.Fatalf("destination should not be created after hash mismatch: %v", err)
	}
}

func TestApplyPlanBacksUpAndDisablesVSCodeFolderOpenTasks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, ".vscode/tasks.json", `{
  "version": "2.0.0",
  "tasks": [
    {"label": "agent-start", "type": "shell", "command": "node setup.mjs", "runOptions": {"runOn": "folderOpen"}},
    {"label": "manual", "type": "shell", "command": "echo ok"}
  ]
}`)

	plan, err := quarantine.PlanActions(context.Background(), quarantine.Options{
		WorkspaceRoot: root,
		Now:           fixedTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := quarantine.ApplyPlan(context.Background(), plan, quarantine.ApplyOptions{Now: fixedTime})
	if err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	action := actionsByApplyKind(result.Actions)[quarantine.ActionDisableVSCodeFolderOpen]
	if action.Status != quarantine.ApplyStatusApplied {
		t.Fatalf("vscode status = %q, want applied: %#v", action.Status, action)
	}
	backup, err := os.ReadFile(action.DestinationPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !strings.Contains(string(backup), `"runOn": "folderOpen"`) {
		t.Fatalf("backup did not preserve original folderOpen task:\n%s", backup)
	}

	var rewritten struct {
		Tasks []struct {
			Label      string         `json:"label"`
			RunOptions map[string]any `json:"runOptions"`
		} `json:"tasks"`
	}
	data, err := os.ReadFile(filepath.Join(root, ".vscode/tasks.json"))
	if err != nil {
		t.Fatalf("read rewritten tasks: %v", err)
	}
	if err := json.Unmarshal(data, &rewritten); err != nil {
		t.Fatalf("rewritten tasks JSON: %v\n%s", err, data)
	}
	if got := rewritten.Tasks[0].RunOptions["runOn"]; got != "default" {
		t.Fatalf("folderOpen task runOn = %#v, want default", got)
	}
	assertHash(t, action.AppliedHash)
	if action.AppliedHash == action.Hash {
		t.Fatal("rewritten tasks hash should differ from original hash")
	}
}

func TestApplyPlanLeavesApplyRequiredPersistenceActionsUntouched(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	systemd := filepath.Join(root, "systemd")
	writeFile(t, systemd, "gh-token-monitor.service", "[Service]\nExecStart=gh-token-monitor\n")

	plan, err := quarantine.PlanActions(context.Background(), quarantine.Options{
		WorkspaceRoot: root,
		SystemdRoots:  []quarantine.Root{{Name: "systemd", Path: systemd}},
		Now:           fixedTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := quarantine.ApplyPlan(context.Background(), plan, quarantine.ApplyOptions{Now: fixedTime})
	if err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	action := actionsByApplyKind(result.Actions)[quarantine.ActionDisableSystemdUnit]
	if action.Status != quarantine.ApplyStatusRequiresConfirmation {
		t.Fatalf("systemd status = %q, want requires-confirmation: %#v", action.Status, action)
	}
	if _, err := os.Stat(action.SourcePath); err != nil {
		t.Fatalf("systemd source should remain present: %v", err)
	}
	if _, err := os.Stat(action.DestinationPath); !os.IsNotExist(err) {
		t.Fatalf("systemd backup should not be created without explicit confirmation: %v", err)
	}
}

func actionsByKind(actions []quarantine.Action) map[quarantine.ActionKind]quarantine.Action {
	out := make(map[quarantine.ActionKind]quarantine.Action, len(actions))
	for _, action := range actions {
		out[action.Kind] = action
	}
	return out
}

func actionsByApplyKind(actions []quarantine.AppliedAction) map[quarantine.ActionKind]quarantine.AppliedAction {
	out := make(map[quarantine.ActionKind]quarantine.AppliedAction, len(actions))
	for _, action := range actions {
		out[action.Kind] = action
	}
	return out
}

func assertHash(t *testing.T, got string) {
	t.Helper()
	if !strings.HasPrefix(got, "sha256:") || len(got) != len("sha256:")+64 {
		t.Fatalf("hash = %q, want sha256 hex digest", got)
	}
}

func writeFile(t *testing.T, root, rel, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fixedTime() time.Time {
	return time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
}
