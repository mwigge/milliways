package rules

import (
	"strings"
	"testing"
)

func TestRulesLoaderInjectSkillsAppendsMatchedSkillContent(t *testing.T) {
	t.Parallel()

	aiLocalDir := t.TempDir()
	rulesDir := t.TempDir()
	writeRuleFixture(t, aiLocalDir, rulesDir)

	loader := NewRulesLoader(aiLocalDir, rulesDir)
	if err := loader.LoadSkills(); err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}

	context := loader.InjectSkills("need pandas analysis", "# Agent Context\n\nStart here.")
	if !strings.Contains(context, "# Agent Context") {
		t.Fatalf("InjectSkills() removed agent context:\n%s", context)
	}
	if !strings.Contains(context, "# Data Analyst Skill") {
		t.Fatalf("InjectSkills() missing matched skill:\n%s", context)
	}
}

func TestRulesLoaderInjectSkillsLeavesContextUnchangedWithoutMatches(t *testing.T) {
	t.Parallel()

	loader := NewRulesLoader(t.TempDir(), t.TempDir())
	got := loader.InjectSkills("plain request", "# Agent Context")
	if got != "# Agent Context" {
		t.Fatalf("InjectSkills() = %q, want unchanged context", got)
	}
}
