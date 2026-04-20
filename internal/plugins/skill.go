package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill stores plugin skill metadata and content.
type Skill struct {
	Name        string
	Description string
	AutoInvoke  bool
	Trigger     []string
	Content     string
}

type skillFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	AutoInvoke  bool     `yaml:"auto_invoke"`
	Trigger     []string `yaml:"trigger"`
}

// LoadSkills loads every skill markdown file in pluginRoot/skills.
func LoadSkills(pluginRoot string) ([]Skill, error) {
	dir := filepath.Join(pluginRoot, "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read skills dir %q: %w", dir, err)
	}

	skills := make([]Skill, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		skill, err := loadSkillFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		skills = append(skills, skill)
	}
	return skills, nil
}

func loadSkillFile(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, fmt.Errorf("read skill %q: %w", path, err)
	}

	frontmatter, body, err := parseSkillDocument(string(data))
	if err != nil {
		return Skill{}, fmt.Errorf("parse skill %q: %w", path, err)
	}

	name := strings.TrimSpace(frontmatter.Name)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	return Skill{
		Name:        name,
		Description: strings.TrimSpace(frontmatter.Description),
		AutoInvoke:  frontmatter.AutoInvoke,
		Trigger:     append([]string(nil), frontmatter.Trigger...),
		Content:     body,
	}, nil
}

func parseSkillDocument(content string) (skillFrontmatter, string, error) {
	trimmed := strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(trimmed, "---\n") {
		return skillFrontmatter{}, strings.TrimSpace(trimmed), nil
	}

	rest := strings.TrimPrefix(trimmed, "---\n")
	separator := "\n---\n"
	index := strings.Index(rest, separator)
	if index < 0 {
		return skillFrontmatter{}, "", fmt.Errorf("missing closing frontmatter delimiter")
	}

	frontmatterText := rest[:index]
	body := rest[index+len(separator):]

	var frontmatter skillFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatterText), &frontmatter); err != nil {
		return skillFrontmatter{}, "", fmt.Errorf("decode frontmatter: %w", err)
	}

	return frontmatter, strings.TrimSpace(body), nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
