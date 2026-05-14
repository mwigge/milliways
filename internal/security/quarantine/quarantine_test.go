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

func TestPlanActionsFindsLinuxUserSystemdUnitDryRun(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	systemd := filepath.Join(root, ".config", "systemd", "user")
	unitPath := filepath.Join(systemd, "gh-token-monitor.service")
	writeFile(t, systemd, "gh-token-monitor.service", `[Unit]
Description=GitHub token monitor

[Service]
ExecStart=/bin/sh -c "gh-token-monitor --background"
`)

	plan, err := quarantine.PlanActions(context.Background(), quarantine.Options{
		WorkspaceRoot: root,
		SystemdRoots:  []quarantine.Root{{Name: "user-systemd", Path: systemd}},
		Now:           fixedTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	action := onlyAction(t, plan.Actions)
	if action.Kind != quarantine.ActionDisableSystemdUnit {
		t.Fatalf("kind = %q, want systemd disable", action.Kind)
	}
	if action.SourcePath != unitPath {
		t.Fatalf("source = %q, want %q", action.SourcePath, unitPath)
	}
	if !strings.HasSuffix(action.DestinationPath, filepath.Join(".milliways", "quarantine", "20260514T120000Z", ".config", "systemd", "user", "gh-token-monitor.service.bak")) {
		t.Fatalf("backup = %q", action.DestinationPath)
	}
	if !action.ApplyRequired {
		t.Fatal("systemd dry-run should require explicit confirmation")
	}
	if !strings.Contains(action.RollbackHint, "re-enable the user systemd unit") {
		t.Fatalf("rollback hint = %q", action.RollbackHint)
	}
	assertHash(t, action.Hash)
}

func TestPlanActionsFindsMacOSLaunchAgentDryRun(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	agents := filepath.Join(root, "Users", "alice", "Library", "LaunchAgents")
	plistPath := filepath.Join(agents, "com.user.gh-token-monitor.plist")
	writeFile(t, agents, "com.user.gh-token-monitor.plist", `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
  <key>Label</key><string>com.user.gh-token-monitor</string>
  <key>ProgramArguments</key><array><string>gh-token-monitor</string></array>
</dict></plist>
`)

	plan, err := quarantine.PlanActions(context.Background(), quarantine.Options{
		WorkspaceRoot: root,
		LaunchAgentRoots: []quarantine.Root{
			{Name: "launch-agents", Path: agents},
		},
		Now: fixedTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	action := onlyAction(t, plan.Actions)
	if action.Kind != quarantine.ActionDisableLaunchAgent {
		t.Fatalf("kind = %q, want LaunchAgent disable", action.Kind)
	}
	if action.SourcePath != plistPath {
		t.Fatalf("source = %q, want %q", action.SourcePath, plistPath)
	}
	if !strings.HasSuffix(action.DestinationPath, filepath.Join(".milliways", "quarantine", "20260514T120000Z", "Users", "alice", "Library", "LaunchAgents", "com.user.gh-token-monitor.plist.bak")) {
		t.Fatalf("backup = %q", action.DestinationPath)
	}
	if !action.ApplyRequired {
		t.Fatal("LaunchAgent dry-run should require explicit confirmation")
	}
	if !strings.Contains(action.RollbackHint, "load the LaunchAgent plist") {
		t.Fatalf("rollback hint = %q", action.RollbackHint)
	}
	assertHash(t, action.Hash)
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

func TestApplyPlanRefusesServiceDisableWithoutExplicitConfirmation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	systemd := filepath.Join(root, ".config", "systemd", "user")
	writeFile(t, systemd, "gh-token-monitor.service", "[Service]\nExecStart=gh-token-monitor\n")

	plan, err := quarantine.PlanActions(context.Background(), quarantine.Options{
		WorkspaceRoot: root,
		SystemdRoots:  []quarantine.Root{{Name: "user-systemd", Path: systemd}},
		Now:           fixedTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := quarantine.ApplyPlan(context.Background(), plan, quarantine.ApplyOptions{Now: fixedTime})
	if err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	action := onlyAppliedAction(t, result.Actions)
	if action.Status != quarantine.ApplyStatusRequiresConfirmation {
		t.Fatalf("status = %q, want requires-confirmation: %#v", action.Status, action)
	}
	if action.Error == "" || !strings.Contains(action.Error, "explicit confirmation") {
		t.Fatalf("error = %q, want explicit confirmation message", action.Error)
	}
	if _, err := os.Stat(action.SourcePath); err != nil {
		t.Fatalf("systemd source should remain present: %v", err)
	}
	if _, err := os.Stat(action.DestinationPath); !os.IsNotExist(err) {
		t.Fatalf("systemd backup should not be created without explicit confirmation: %v", err)
	}
}

func TestApplyPlanDisablesServiceWithExplicitConfirmation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	launchDaemons := filepath.Join(root, "Library", "LaunchDaemons")
	writeFile(t, launchDaemons, "com.user.gh-token-monitor.plist", "<plist><string>gh-token-monitor</string></plist>\n")

	plan, err := quarantine.PlanActions(context.Background(), quarantine.Options{
		WorkspaceRoot:    root,
		LaunchAgentRoots: []quarantine.Root{{Name: "launch-daemons", Path: launchDaemons}},
		Now:              fixedTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := quarantine.ApplyPlan(context.Background(), plan, quarantine.ApplyOptions{
		Now:                   fixedTime,
		ConfirmServiceDisable: true,
	})
	if err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	action := onlyAppliedAction(t, result.Actions)
	if action.Status != quarantine.ApplyStatusApplied {
		t.Fatalf("status = %q, want applied: %#v", action.Status, action)
	}
	if _, err := os.Stat(action.SourcePath); !os.IsNotExist(err) {
		t.Fatalf("service source should be removed after confirmed disable: %v", err)
	}
	backup, err := os.ReadFile(action.DestinationPath)
	if err != nil {
		t.Fatalf("read service backup: %v", err)
	}
	if string(backup) != "<plist><string>gh-token-monitor</string></plist>\n" {
		t.Fatalf("backup contents = %q", backup)
	}
	assertHash(t, action.AppliedHash)
	if action.AppliedHash != action.Hash {
		t.Fatalf("applied hash = %q, want original hash %q", action.AppliedHash, action.Hash)
	}
}

func actionsByKind(actions []quarantine.Action) map[quarantine.ActionKind]quarantine.Action {
	out := make(map[quarantine.ActionKind]quarantine.Action, len(actions))
	for _, action := range actions {
		out[action.Kind] = action
	}
	return out
}

func onlyAction(t *testing.T, actions []quarantine.Action) quarantine.Action {
	t.Helper()
	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1: %#v", len(actions), actions)
	}
	return actions[0]
}

func onlyAppliedAction(t *testing.T, actions []quarantine.AppliedAction) quarantine.AppliedAction {
	t.Helper()
	if len(actions) != 1 {
		t.Fatalf("applied actions = %d, want 1: %#v", len(actions), actions)
	}
	return actions[0]
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
