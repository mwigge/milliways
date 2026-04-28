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

package maitre

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSkillDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create fake skill directories with SKILL.md
	skill1 := filepath.Join(dir, "python-patterns")
	if err := os.MkdirAll(skill1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill1, "SKILL.md"), []byte(`---
name: python-patterns
description: "Idiomatic Python patterns and best practices"
---
# Python Patterns
`), 0o644); err != nil {
		t.Fatal(err)
	}

	skill2 := filepath.Join(dir, "security-review")
	if err := os.MkdirAll(skill2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill2, "SKILL.md"), []byte(`---
name: security-review
description: Auth, secrets, input validation
---
`), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := scanSkillDir(dir, "claude")
	if len(entries) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(entries))
	}

	names := map[string]string{}
	for _, e := range entries {
		names[e.Name] = e.Description
	}

	if desc, ok := names["python-patterns"]; !ok {
		t.Error("missing python-patterns")
	} else if desc != "Idiomatic Python patterns and best practices" {
		t.Errorf("unexpected description: %q", desc)
	}

	if _, ok := names["security-review"]; !ok {
		t.Error("missing security-review")
	}
}

func TestScanPluginDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create fake .ts plugin files
	for _, name := range []string{"quality-gate.ts", "security-guard.ts", "README.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("// plugin"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	entries := scanPluginDir(dir, "opencode")
	if len(entries) != 2 {
		t.Fatalf("expected 2 plugins (not README.md), got %d", len(entries))
	}

	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
		if e.Kitchen != "opencode" {
			t.Errorf("expected kitchen opencode, got %q", e.Kitchen)
		}
	}

	if !names["quality-gate"] {
		t.Error("missing quality-gate plugin")
	}
	if !names["security-guard"] {
		t.Error("missing security-guard plugin")
	}
}

func TestScanSkillDir_NonExistent(t *testing.T) {
	t.Parallel()
	entries := scanSkillDir("/nonexistent/path", "claude")
	if entries != nil {
		t.Errorf("expected nil for nonexistent dir, got %d entries", len(entries))
	}
}

func TestSkillCatalog_HasSkill(t *testing.T) {
	t.Parallel()
	catalog := &SkillCatalog{
		entries: []SkillEntry{
			{Name: "security-review", Description: "Auth, secrets, OWASP", Kitchen: "claude"},
			{Name: "quality-gate", Description: "OpenCode plugin", Kitchen: "opencode"},
			{Name: "python-patterns", Description: "Idiomatic Python", Kitchen: "claude"},
		},
	}

	// Whole word match on skill name
	kitchenName, skill := catalog.HasSkill("security")
	if kitchenName != "claude" {
		t.Errorf("expected claude for 'security', got %q", kitchenName)
	}
	if skill == nil || skill.Name != "security-review" {
		t.Errorf("expected security-review skill, got %v", skill)
	}

	// "python" matches the word "python" in "python-patterns"
	kitchenName, skill = catalog.HasSkill("python")
	if kitchenName != "claude" {
		t.Errorf("expected claude for 'python', got %q", kitchenName)
	}
	if skill == nil || skill.Name != "python-patterns" {
		t.Errorf("expected python-patterns skill, got %v", skill)
	}

	// Short queries (< 3 chars) are rejected to avoid overly broad matches
	kitchenName, _ = catalog.HasSkill("se")
	if kitchenName != "" {
		t.Errorf("expected empty for short query 'se', got %q", kitchenName)
	}

	// Nonexistent skill returns empty
	kitchenName, _ = catalog.HasSkill("nonexistent-xyz")
	if kitchenName != "" {
		t.Errorf("expected empty for nonexistent skill, got %q", kitchenName)
	}

	// Substring of a word should not match (e.g., "sec" is not a whole word in "security")
	kitchenName, _ = catalog.HasSkill("sec")
	if kitchenName != "" {
		t.Errorf("expected empty for partial word 'sec', got %q", kitchenName)
	}
}

func TestSkillCatalog_ForKitchen(t *testing.T) {
	t.Parallel()
	catalog := &SkillCatalog{
		entries: []SkillEntry{
			{Name: "a", Kitchen: "claude"},
			{Name: "b", Kitchen: "opencode"},
			{Name: "c", Kitchen: "claude"},
		},
	}

	claude := catalog.ForKitchen("claude")
	if len(claude) != 2 {
		t.Errorf("expected 2 claude skills, got %d", len(claude))
	}

	opencode := catalog.ForKitchen("opencode")
	if len(opencode) != 1 {
		t.Errorf("expected 1 opencode skill, got %d", len(opencode))
	}
}

func TestReadSkillDescription(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	if err := os.WriteFile(path, []byte(`---
name: test-skill
description: "This is a test skill"
user-invocable: true
---
# Test
`), 0o644); err != nil {
		t.Fatal(err)
	}

	desc := readSkillDescription(path)
	if desc != "This is a test skill" {
		t.Errorf("expected 'This is a test skill', got %q", desc)
	}
}
