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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mwigge/milliways/internal/config"
	"gopkg.in/yaml.v3"
)

// AgentDef describes one agent loaded from the agents directory.
type AgentDef struct {
	Name        string
	Description string
	Mode        string
	Perms       map[string]string
	Content     string
}

// RulesLoader loads agent definitions, skill rules, and static rule files.
type RulesLoader struct {
	aiLocalDir string
	rulesDir   string
	agents     map[string]AgentDef
	skills     map[string]string
	skillRules []skillRule
}

type agentDocument struct {
	Description string         `yaml:"description"`
	Mode        string         `yaml:"mode"`
	Permission  map[string]any `yaml:"permission"`
}

type skillRuleConfig struct {
	Pattern string `json:"pattern"`
	Skill   string `json:"skill"`
}

type skillRule struct {
	Name    string
	Pattern *regexp.Regexp
	Path    string
}

// NewRulesLoader creates a loader for agent and skill rules.
func NewRulesLoader(aiLocalDir, rulesDir string) *RulesLoader {
	return &RulesLoader{
		aiLocalDir: strings.TrimSpace(aiLocalDir),
		rulesDir:   strings.TrimSpace(rulesDir),
		agents:     make(map[string]AgentDef),
		skills:     make(map[string]string),
	}
}

// LoadAgents loads agent definitions from the agents directory (opencode/agents subdir).
func (l *RulesLoader) LoadAgents() error {
	agentsDir := filepath.Join(l.aiLocalDir, "opencode", "agents")
	if err := config.GuardReadPath(agentsDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return fmt.Errorf("read agents dir: %w", err)
	}

	agents := make(map[string]AgentDef, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		path := filepath.Join(l.aiLocalDir, "opencode", "agents", entry.Name())
		if err := config.GuardReadPath(path); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read agent %q: %w", path, err)
		}
		frontmatter, body, err := parseAgentMarkdown(string(data))
		if err != nil {
			return fmt.Errorf("parse agent %q: %w", path, err)
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		agents[name] = AgentDef{
			Name:        name,
			Description: strings.TrimSpace(frontmatter.Description),
			Mode:        strings.TrimSpace(frontmatter.Mode),
			Perms:       flattenPermissions(frontmatter.Permission, ""),
			Content:     strings.TrimSpace(body),
		}
	}

	l.agents = agents
	return nil
}

// LoadSkills loads skill match rules from the agents directory (.claude/skill-rules.json).
func (l *RulesLoader) LoadSkills() error {
	path := filepath.Join(l.aiLocalDir, ".claude", "skill-rules.json")
	if err := config.GuardReadPath(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read skill rules %q: %w", path, err)
	}

	var configs []skillRuleConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return fmt.Errorf("decode skill rules %q: %w", path, err)
	}

	skills := make(map[string]string, len(configs))
	rules := make([]skillRule, 0, len(configs))
	for _, config := range configs {
		if strings.TrimSpace(config.Skill) == "" || strings.TrimSpace(config.Pattern) == "" {
			continue
		}
		compiled, err := regexp.Compile("(?i)" + config.Pattern)
		if err != nil {
			return fmt.Errorf("compile skill pattern for %q: %w", config.Skill, err)
		}
		skillPath, err := resolveSkillPath(filepath.Join(l.aiLocalDir, "skills"), config.Skill)
		if err != nil {
			return err
		}
		if existing, ok := skills[config.Skill]; ok {
			skillPath = existing
		} else {
			skills[config.Skill] = skillPath
		}
		rules = append(rules, skillRule{Name: config.Skill, Pattern: compiled, Path: skillPath})
	}

	l.skills = skills
	l.skillRules = rules
	return nil
}

// GetAgent returns one loaded agent definition.
func (l *RulesLoader) GetAgent(name string) (AgentDef, bool) {
	agent, ok := l.agents[strings.TrimSpace(name)]
	return agent, ok
}

// MatchSkills returns the names of matching skills in definition order.
func (l *RulesLoader) MatchSkills(input string) []string {
	matched := make([]string, 0)
	seen := make(map[string]struct{})
	for _, rule := range l.skillRules {
		if rule.Pattern != nil && rule.Pattern.MatchString(input) {
			if _, ok := seen[rule.Name]; ok {
				continue
			}
			seen[rule.Name] = struct{}{}
			matched = append(matched, rule.Name)
		}
	}
	return matched
}

// BuildContext combines global rules, core conventions, agent role, matching skills, and overrides.
func (l *RulesLoader) BuildContext(mode, agentName, input string) string {
	_ = mode
	parts := make([]string, 0, 5)
	appendIfPresent := func(path string) {
		if content := readOptionalFile(path); strings.TrimSpace(content) != "" {
			parts = append(parts, strings.TrimSpace(content))
		}
	}

	appendIfPresent(filepath.Join(l.rulesDir, "global.md"))
	appendIfPresent(l.coreConventionsPath())
	if agent, ok := l.GetAgent(agentName); ok && strings.TrimSpace(agent.Content) != "" {
		parts = append(parts, strings.TrimSpace(agent.Content))
	}
	for _, name := range l.MatchSkills(input) {
		if content := readOptionalFile(l.skills[name]); strings.TrimSpace(content) != "" {
			parts = append(parts, strings.TrimSpace(content))
		}
	}
	appendIfPresent(filepath.Join(l.rulesDir, "override.md"))

	return strings.Join(parts, "\n\n")
}

// EnsureDefaultRuleFiles creates default rules files when they do not already exist.
func (l *RulesLoader) EnsureDefaultRuleFiles() error {
	if err := os.MkdirAll(l.rulesDir, 0o755); err != nil {
		return fmt.Errorf("create rules dir %q: %w", l.rulesDir, err)
	}
	defaults := map[string]string{
		"global.md": `# Global Rules

- No AI attribution in commits, docs, comments, or code.
- Conventional Commits are required for all repository changes.
- No hardcoded secrets; use environment variables and fail fast when absent.
- Parameterized SQL only.
- Structured logging only; no print-style logging in library code.
- No bare except-style catch-alls.
`,
		"override.md.example": `# Project Override

Add project-specific prompt rules here. Copy this file to override.md to activate local overrides.
`,
	}
	for name, content := range defaults {
		path := filepath.Join(l.rulesDir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat %q: %w", path, err)
		}
		if err := config.GuardWritePath(path); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return fmt.Errorf("write default rule file %q: %w", path, err)
		}
	}
	return nil
}

func (l *RulesLoader) coreConventionsPath() string {
	candidates := []string{
		filepath.Join(l.aiLocalDir, "opencode", "AGENTS.md"),
		filepath.Join(l.aiLocalDir, "AGENTS.md"),
		filepath.Join(l.aiLocalDir, "CLAUDE.md"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func parseAgentMarkdown(content string) (agentDocument, string, error) {
	trimmed := strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(trimmed, "---\n") {
		return agentDocument{}, strings.TrimSpace(trimmed), nil
	}

	rest := strings.TrimPrefix(trimmed, "---\n")
	separator := "\n---\n"
	index := strings.Index(rest, separator)
	if index < 0 {
		return agentDocument{}, "", fmt.Errorf("missing closing frontmatter delimiter")
	}

	var doc agentDocument
	if err := yaml.Unmarshal([]byte(rest[:index]), &doc); err != nil {
		return agentDocument{}, "", fmt.Errorf("decode frontmatter: %w", err)
	}
	return doc, strings.TrimSpace(rest[index+len(separator):]), nil
}

func flattenPermissions(value map[string]any, prefix string) map[string]string {
	result := make(map[string]string)
	for _, key := range sortedKeys(value) {
		raw := value[key]
		nextKey := key
		if prefix != "" {
			nextKey = prefix + "." + key
		}
		switch typed := raw.(type) {
		case string:
			result[nextKey] = typed
		case map[string]any:
			for nestedKey, nestedValue := range flattenPermissions(typed, nextKey) {
				result[nestedKey] = nestedValue
			}
		case map[any]any:
			converted := make(map[string]any, len(typed))
			for k, v := range typed {
				converted[fmt.Sprint(k)] = v
			}
			for nestedKey, nestedValue := range flattenPermissions(converted, nextKey) {
				result[nestedKey] = nestedValue
			}
		}
	}
	return result
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func resolveSkillPath(skillsRoot, skillName string) (string, error) {
	direct := filepath.Join(skillsRoot, skillName, "SKILL.md")
	if _, err := os.Stat(direct); err == nil {
		return direct, nil
	}

	var resolved string
	err := filepath.WalkDir(skillsRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) == skillName {
			resolved = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("resolve skill %q: %w", skillName, err)
	}
	if resolved == "" {
		return "", fmt.Errorf("resolve skill %q: skill file not found", skillName)
	}
	return resolved, nil
}

func readOptionalFile(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	if err := config.GuardReadPath(path); err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
