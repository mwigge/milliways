package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/hooks"
)

func TestRegistryLoadAll_LoadsPluginAssets(t *testing.T) {
	t.Parallel()

	userRoot := filepath.Join(t.TempDir(), "plugins")
	projectRoot := filepath.Join(t.TempDir(), ".claude", "plugins")
	userPlugin := filepath.Join(userRoot, "hookify")
	projectPlugin := filepath.Join(projectRoot, "reviewer")
	writeTestPlugin(t, userPlugin, pluginFixture{
		Name:        "hookify",
		Description: "Hookify plugin",
		CommandName: "hookify",
		AgentName:   "code-reviewer",
		SkillName:   "rust-expert",
		HookCommand: "python3 ${CLAUDE_PLUGIN_ROOT}/hooks/pretooluse.py",
	})
	writeTestPlugin(t, projectPlugin, pluginFixture{
		Name:        "reviewer",
		Description: "Reviewer plugin",
		CommandName: "review",
		AgentName:   "code-explorer",
		SkillName:   "go-expert",
		HookCommand: "python3 ${CLAUDE_PLUGIN_ROOT}/hooks/posttooluse.py",
	})

	registry := NewRegistry()
	if err := registry.LoadAll([]string{userRoot, projectRoot}); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	commands := registry.ListCommands()
	if len(commands) != 2 {
		t.Fatalf("len(commands) = %d, want 2", len(commands))
	}
	namespaces := make(map[string]string, len(commands))
	for _, command := range commands {
		namespaces[command.Name] = command.Namespace
	}
	if namespaces["hookify"] != "user" {
		t.Fatalf("hookify namespace = %q, want user", namespaces["hookify"])
	}
	if namespaces["review"] != "project" {
		t.Fatalf("review namespace = %q, want project", namespaces["review"])
	}

	agents := registry.ListAgents()
	if len(agents) != 2 {
		t.Fatalf("len(agents) = %d, want 2", len(agents))
	}

	skills := registry.ListSkills()
	if len(skills) != 2 {
		t.Fatalf("len(skills) = %d, want 2", len(skills))
	}
	if !skills[0].AutoInvoke && !skills[1].AutoInvoke {
		t.Fatal("expected at least one autoinvoked skill")
	}

	hookMap := registry.ListHooks()
	if len(hookMap["PreToolUse"]) != 2 {
		t.Fatalf("len(PreToolUse hooks) = %d, want 2", len(hookMap["PreToolUse"]))
	}
	for _, hook := range hookMap["PreToolUse"] {
		if strings.Contains(hook.Command, "${CLAUDE_PLUGIN_ROOT}") {
			t.Fatalf("hook command not resolved: %q", hook.Command)
		}
	}
}

func TestRunAgent_SubstitutesVariables(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{response: "done"}
	response, err := RunAgent(Agent{
		Name:   "code-reviewer",
		Prompt: "Review $DIFF",
	}, map[string]string{"DIFF": "git diff HEAD"}, provider)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if response != "done" {
		t.Fatalf("response = %q, want done", response)
	}
	if provider.prompt != "Review git diff HEAD" {
		t.Fatalf("prompt = %q", provider.prompt)
	}
}

func TestLoadSkills_ParsesFrontmatter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(root, "skills", "rust.md")
	content := `---
name: rust-expert
description: Rust guidance
auto_invoke: true
trigger:
  - "*.rs"
  - "Cargo.toml"
---

Use Result values.
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := LoadSkills(root)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("len(skills) = %d, want 1", len(loaded))
	}
	if loaded[0].Name != "rust-expert" || !loaded[0].AutoInvoke {
		t.Fatalf("skill = %#v", loaded[0])
	}
	if len(loaded[0].Trigger) != 2 {
		t.Fatalf("trigger len = %d, want 2", len(loaded[0].Trigger))
	}
}

func TestLoadHooks_ResolvesPluginRoot(t *testing.T) {
	t.Parallel()

	pluginRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(pluginRoot, "hooks"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	hooksJSON := `{
  "description": "test hooks",
  "hooks": {
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "python3 ${CLAUDE_PLUGIN_ROOT}/hooks/pretooluse.py",
            "timeout": 5
          }
        ],
        "matcher": "Edit|Write|Bash"
      }
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(pluginRoot, "hooks", "hooks.json"), []byte(hooksJSON), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := hooks.LoadHooks(pluginRoot)
	if err != nil {
		t.Fatalf("LoadHooks: %v", err)
	}
	if len(loaded.Hooks["PreToolUse"]) != 1 {
		t.Fatalf("len(PreToolUse) = %d, want 1", len(loaded.Hooks["PreToolUse"]))
	}
	if got := loaded.Hooks["PreToolUse"][0].Command; got != "python3 "+filepath.Join(pluginRoot, "hooks", "pretooluse.py") {
		t.Fatalf("command = %q", got)
	}
	if got := loaded.Hooks["PreToolUse"][0].Matcher; got != "Edit|Write|Bash" {
		t.Fatalf("matcher = %q", got)
	}
}

type stubProvider struct {
	prompt   string
	response string
}

func (s *stubProvider) Send(_ context.Context, prompt string) (string, error) {
	s.prompt = prompt
	return s.response, nil
}

type pluginFixture struct {
	Name        string
	Description string
	CommandName string
	AgentName   string
	SkillName   string
	HookCommand string
}

func writeTestPlugin(t *testing.T, root string, fixture pluginFixture) {
	t.Helper()

	for _, dir := range []string{
		filepath.Join(root, ".claude-plugin"),
		filepath.Join(root, "commands"),
		filepath.Join(root, "agents"),
		filepath.Join(root, "skills"),
		filepath.Join(root, "hooks"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", dir, err)
		}
	}

	manifest := fmt.Sprintf(`{"name": %q, "description": %q}`, fixture.Name, fixture.Description)
	if err := os.WriteFile(filepath.Join(root, ".claude-plugin", "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("WriteFile(plugin.json): %v", err)
	}

	command := fmt.Sprintf("---\nname: %s\ndescription: %s command\n---\n\n```bash\nprintf '%%s' \"$VALUE\"\n```\n", fixture.CommandName, fixture.CommandName)
	if err := os.WriteFile(filepath.Join(root, "commands", fixture.CommandName+".md"), []byte(command), 0o600); err != nil {
		t.Fatalf("WriteFile(command): %v", err)
	}

	agent := fmt.Sprintf(`---
name: %s
description: %s agent
model: minimax
max_tokens: 2000
temperature: 0.3
---

Review $DIFF
`, fixture.AgentName, fixture.AgentName)
	if err := os.WriteFile(filepath.Join(root, "agents", fixture.AgentName+".md"), []byte(agent), 0o600); err != nil {
		t.Fatalf("WriteFile(agent): %v", err)
	}

	skill := fmt.Sprintf(`---
name: %s
description: %s guidance
auto_invoke: true
trigger:
  - "*.go"
---

Use contexts carefully.
`, fixture.SkillName, fixture.SkillName)
	if err := os.WriteFile(filepath.Join(root, "skills", fixture.SkillName+".md"), []byte(skill), 0o600); err != nil {
		t.Fatalf("WriteFile(skill): %v", err)
	}

	hooksJSON := fmt.Sprintf(`{
  "description": %q,
  "hooks": {
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": %q,
            "timeout": 10
          }
        ],
        "matcher": "Edit|Write|Bash"
      }
    ]
  }
}`,
		fixture.Description,
		fixture.HookCommand,
	)
	if err := os.WriteFile(filepath.Join(root, "hooks", "hooks.json"), []byte(hooksJSON), 0o600); err != nil {
		t.Fatalf("WriteFile(hooks): %v", err)
	}
}
