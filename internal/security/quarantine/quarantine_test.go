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

func actionsByKind(actions []quarantine.Action) map[quarantine.ActionKind]quarantine.Action {
	out := make(map[quarantine.ActionKind]quarantine.Action, len(actions))
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
