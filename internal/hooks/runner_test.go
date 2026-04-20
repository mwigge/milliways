package hooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunnerRunHooksBlocksTool(t *testing.T) {
	t.Parallel()

	pluginsDir := t.TempDir()
	pluginRoot := filepath.Join(pluginsDir, "security")
	if err := os.MkdirAll(filepath.Join(pluginRoot, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(pluginRoot, "hooks", "block.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf '{\"blocked\":true,\"message\":\"denied\",\"modified\":false}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/hooks/block.sh","timeout":5}]}]}}`
	if err := os.WriteFile(filepath.Join(pluginRoot, "hooks", "hooks.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	runner, err := NewRunner(pluginsDir)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	result := runner.RunHooks(context.Background(), EventPreToolUse, HookPayload{Event: EventPreToolUse, ToolName: "Bash"})
	if !result.Blocked {
		t.Fatal("expected hook to block tool")
	}
	if result.Message != "denied" {
		t.Fatalf("message = %q", result.Message)
	}
}

func TestRunnerRunHooksAppliesModification(t *testing.T) {
	t.Parallel()

	pluginsDir := t.TempDir()
	pluginRoot := filepath.Join(pluginsDir, "rewrite")
	if err := os.MkdirAll(filepath.Join(pluginRoot, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(pluginRoot, "hooks", "modify.sh")
	script := "#!/bin/sh\ncat >/dev/null\nprintf '{\"blocked\":false,\"modified\":true,\"modified_payload\":{\"event\":\"PreToolUse\",\"tool_name\":\"Read\",\"args\":{\"path\":\"changed.txt\"}}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{"hooks":{"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/hooks/modify.sh","timeout":5}]}]}}`
	if err := os.WriteFile(filepath.Join(pluginRoot, "hooks", "hooks.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	runner, err := NewRunner(pluginsDir)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	result := runner.RunHooks(context.Background(), EventPreToolUse, HookPayload{Event: EventPreToolUse, ToolName: "Read", Args: map[string]any{"path": "old.txt"}})
	if !result.Modified {
		t.Fatal("expected hook to modify payload")
	}
	if got := result.ModifiedPayload.Args["path"]; got != "changed.txt" {
		t.Fatalf("modified path = %v", got)
	}
}
