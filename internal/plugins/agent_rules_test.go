package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/rules"
)

func TestRunAgent_UsesRulesLoaderContext(t *testing.T) {
	aiLocalDir := t.TempDir()
	rulesDir := t.TempDir()
	rulesLoader := rules.NewRulesLoader(aiLocalDir, rulesDir)
	writeRulesFixture(t, aiLocalDir, rulesDir)
	if err := rulesLoader.LoadAgents(); err != nil {
		t.Fatalf("LoadAgents() error = %v", err)
	}
	if err := rulesLoader.LoadSkills(); err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}

	SetRulesLoader(rulesLoader)
	t.Cleanup(func() { SetRulesLoader(nil) })

	provider := &stubProvider{response: "done"}
	_, err := RunAgent(Agent{
		Name:   "coder-go",
		Prompt: "Task: $INPUT",
	}, map[string]string{"INPUT": "please use pandas"}, provider)
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}

	for _, want := range []string{"# Global Rules", "# @coder-go", "# Data Analyst Skill", "Task: please use pandas"} {
		if !strings.Contains(provider.prompt, want) {
			t.Fatalf("provider prompt missing %q\n%s", want, provider.prompt)
		}
	}
}

func writeRulesFixture(t *testing.T, aiLocalDir, rulesDir string) {
	t.Helper()
	for _, dir := range []string{
		filepath.Join(aiLocalDir, "opencode", "agents"),
		filepath.Join(aiLocalDir, ".claude"),
		filepath.Join(aiLocalDir, "skills", "data-analyst"),
		rulesDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(aiLocalDir, "opencode", "agents", "coder-go.md"), []byte(`---
description: Go implementation agent
mode: implementor
permission:
  "*": allow
---

# @coder-go

Write Go code directly.
`), 0o600); err != nil {
		t.Fatalf("WriteFile(agent): %v", err)
	}
	if err := os.WriteFile(filepath.Join(aiLocalDir, "CLAUDE.md"), []byte("# Core Conventions\n\nUse structured logging.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(CLAUDE.md): %v", err)
	}
	if err := os.WriteFile(filepath.Join(aiLocalDir, ".claude", "skill-rules.json"), []byte(`[{"pattern":"pandas","skill":"data-analyst"}]`), 0o600); err != nil {
		t.Fatalf("WriteFile(skill-rules.json): %v", err)
	}
	if err := os.WriteFile(filepath.Join(aiLocalDir, "skills", "data-analyst", "SKILL.md"), []byte("# Data Analyst Skill\n\nUse pandas effectively.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(skill): %v", err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "global.md"), []byte("# Global Rules\n\nProject-wide rules.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(global.md): %v", err)
	}
}
