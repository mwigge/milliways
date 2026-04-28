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

package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRulesLoaderLoadAgentsAndBuildContext(t *testing.T) {
	t.Parallel()

	aiLocalDir := t.TempDir()
	rulesDir := t.TempDir()
	writeRuleFixture(t, aiLocalDir, rulesDir)

	loader := NewRulesLoader(aiLocalDir, rulesDir)
	if err := loader.LoadAgents(); err != nil {
		t.Fatalf("LoadAgents() error = %v", err)
	}
	if err := loader.LoadSkills(); err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}

	agent, ok := loader.GetAgent("coder-go")
	if !ok {
		t.Fatal("expected coder-go agent to be loaded")
	}
	if agent.Description != "Go implementation agent" {
		t.Fatalf("Description = %q, want %q", agent.Description, "Go implementation agent")
	}
	if agent.Mode != "implementor" {
		t.Fatalf("Mode = %q, want %q", agent.Mode, "implementor")
	}
	if agent.Perms["*"] != "allow" {
		t.Fatalf("Perms[*] = %q, want allow", agent.Perms["*"])
	}
	if agent.Perms["read.*"] != "allow" {
		t.Fatalf("Perms[read.*] = %q, want allow", agent.Perms["read.*"])
	}
	if !strings.Contains(agent.Content, "# @coder-go") {
		t.Fatalf("agent content missing role body: %q", agent.Content)
	}

	context := loader.BuildContext("neutral", "coder-go", "please use pandas for this data analysis")
	for _, want := range []string{
		"# Global Rules",
		"# Core Conventions",
		"# @coder-go",
		"# Data Analyst Skill",
		"# Project Override",
	} {
		if !strings.Contains(context, want) {
			t.Fatalf("BuildContext() missing %q\n%s", want, context)
		}
	}

	globalIndex := strings.Index(context, "# Global Rules")
	coreIndex := strings.Index(context, "# Core Conventions")
	agentIndex := strings.Index(context, "# @coder-go")
	skillIndex := strings.Index(context, "# Data Analyst Skill")
	overrideIndex := strings.Index(context, "# Project Override")
	if !(globalIndex < coreIndex && coreIndex < agentIndex && agentIndex < skillIndex && skillIndex < overrideIndex) {
		t.Fatalf("BuildContext() order incorrect:\n%s", context)
	}
}

func TestRulesLoaderMatchSkillsReturnsUniqueMatches(t *testing.T) {
	t.Parallel()

	aiLocalDir := t.TempDir()
	rulesDir := t.TempDir()
	writeRuleFixture(t, aiLocalDir, rulesDir)

	loader := NewRulesLoader(aiLocalDir, rulesDir)
	if err := loader.LoadSkills(); err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}

	matched := loader.MatchSkills("pandas chart pandas dataframe")
	if len(matched) != 2 {
		t.Fatalf("len(MatchSkills()) = %d, want 2", len(matched))
	}
	if matched[0] != "data-analyst" {
		t.Fatalf("matched[0] = %q, want data-analyst", matched[0])
	}
	if matched[1] != "data-visualisation" {
		t.Fatalf("matched[1] = %q, want data-visualisation", matched[1])
	}
}

func TestRulesLoaderLoadSkillsFromBundleRoot(t *testing.T) {
	t.Parallel()

	aiLocalDir := t.TempDir()
	rulesDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(aiLocalDir, "skills", "root-skill"), 0o755); err != nil {
		t.Fatalf("MkdirAll(skill): %v", err)
	}
	if err := os.WriteFile(filepath.Join(aiLocalDir, "skill-rules.json"), []byte(`[{"pattern":"root","skill":"root-skill"}]`), 0o600); err != nil {
		t.Fatalf("WriteFile(root skill-rules.json): %v", err)
	}
	if err := os.WriteFile(filepath.Join(aiLocalDir, "skills", "root-skill", "SKILL.md"), []byte("# Root Skill\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(skill): %v", err)
	}

	loader := NewRulesLoader(aiLocalDir, rulesDir)
	if err := loader.LoadSkills(); err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}
	matched := loader.MatchSkills("use root rules")
	if len(matched) != 1 || matched[0] != "root-skill" {
		t.Fatalf("MatchSkills() = %v, want [root-skill]", matched)
	}
}

func TestRulesLoaderLoadSkillsRootTakesPrecedenceOverLegacy(t *testing.T) {
	t.Parallel()

	aiLocalDir := t.TempDir()
	rulesDir := t.TempDir()
	for _, dir := range []string{
		filepath.Join(aiLocalDir, ".claude"),
		filepath.Join(aiLocalDir, "skills", "root-skill"),
		filepath.Join(aiLocalDir, "skills", "legacy-skill"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(aiLocalDir, "skill-rules.json"), []byte(`[{"pattern":"shared","skill":"root-skill"}]`), 0o600); err != nil {
		t.Fatalf("WriteFile(root skill-rules.json): %v", err)
	}
	if err := os.WriteFile(filepath.Join(aiLocalDir, ".claude", "skill-rules.json"), []byte(`[{"pattern":"shared","skill":"legacy-skill"}]`), 0o600); err != nil {
		t.Fatalf("WriteFile(legacy skill-rules.json): %v", err)
	}
	if err := os.WriteFile(filepath.Join(aiLocalDir, "skills", "root-skill", "SKILL.md"), []byte("# Root Skill\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(root skill): %v", err)
	}
	if err := os.WriteFile(filepath.Join(aiLocalDir, "skills", "legacy-skill", "SKILL.md"), []byte("# Legacy Skill\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(legacy skill): %v", err)
	}

	loader := NewRulesLoader(aiLocalDir, rulesDir)
	if err := loader.LoadSkills(); err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}
	matched := loader.MatchSkills("shared prompt")
	if len(matched) != 1 || matched[0] != "root-skill" {
		t.Fatalf("MatchSkills() = %v, want [root-skill]", matched)
	}
}

func TestRulesLoaderEnsureDefaultRuleFiles(t *testing.T) {
	t.Parallel()

	loader := NewRulesLoader(t.TempDir(), filepath.Join(t.TempDir(), "rules"))
	if err := loader.EnsureDefaultRuleFiles(); err != nil {
		t.Fatalf("EnsureDefaultRuleFiles() error = %v", err)
	}

	globalPath := filepath.Join(loader.rulesDir, "global.md")
	overrideExamplePath := filepath.Join(loader.rulesDir, "override.md.example")
	for _, path := range []string{globalPath, overrideExamplePath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Stat(%q) error = %v", path, err)
		}
	}

	globalContent, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("ReadFile(global.md) error = %v", err)
	}
	for _, want := range []string{"No AI attribution", "Conventional Commits", "No hardcoded secrets"} {
		if !strings.Contains(string(globalContent), want) {
			t.Fatalf("global.md missing %q\n%s", want, string(globalContent))
		}
	}
}

func writeRuleFixture(t *testing.T, aiLocalDir, rulesDir string) {
	t.Helper()

	for _, dir := range []string{
		filepath.Join(aiLocalDir, "opencode", "agents"),
		filepath.Join(aiLocalDir, ".claude"),
		filepath.Join(aiLocalDir, "skills", "data-analyst"),
		filepath.Join(aiLocalDir, "skills", "data-visualisation"),
		rulesDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", dir, err)
		}
	}

	agentDoc := `---
description: Go implementation agent
mode: implementor
permission:
  "*": allow
  read:
    "*": allow
    "*.env": ask
---

# @coder-go

Write Go code directly.
`
	if err := os.WriteFile(filepath.Join(aiLocalDir, "opencode", "agents", "coder-go.md"), []byte(agentDoc), 0o600); err != nil {
		t.Fatalf("WriteFile(agent): %v", err)
	}

	coreDoc := `# Core Conventions

Use conventional commits and structured logging.
`
	if err := os.WriteFile(filepath.Join(aiLocalDir, "CLAUDE.md"), []byte(coreDoc), 0o600); err != nil {
		t.Fatalf("WriteFile(CLAUDE.md): %v", err)
	}

	skillRules := `[
  {"pattern":"pandas|dataframe","skill":"data-analyst"},
  {"pattern":"chart|plot","skill":"data-visualisation"}
]`
	if err := os.WriteFile(filepath.Join(aiLocalDir, ".claude", "skill-rules.json"), []byte(skillRules), 0o600); err != nil {
		t.Fatalf("WriteFile(skill-rules.json): %v", err)
	}

	if err := os.WriteFile(filepath.Join(aiLocalDir, "skills", "data-analyst", "SKILL.md"), []byte("# Data Analyst Skill\n\nUse pandas effectively.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(data-analyst): %v", err)
	}
	if err := os.WriteFile(filepath.Join(aiLocalDir, "skills", "data-visualisation", "SKILL.md"), []byte("# Data Visualisation Skill\n\nUse charts carefully.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(data-visualisation): %v", err)
	}

	if err := os.WriteFile(filepath.Join(rulesDir, "global.md"), []byte("# Global Rules\n\nProject-wide rules.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(global.md): %v", err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "override.md"), []byte("# Project Override\n\nLocal override.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(override.md): %v", err)
	}
}
