package maitre

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// SkillEntry represents a skill available in a kitchen.
type SkillEntry struct {
	Name        string
	Description string
	Kitchen     string
}

// SkillCatalog maps kitchens to their available skills.
type SkillCatalog struct {
	entries []SkillEntry
}

// ScanSkills builds a skill catalog by scanning known skill directories.
func ScanSkills() *SkillCatalog {
	home, err := os.UserHomeDir()
	if err != nil {
		return &SkillCatalog{}
	}

	var entries []SkillEntry

	// Claude Code skills: ~/.claude/skills/*/SKILL.md
	claudeSkills := filepath.Join(home, ".claude", "skills")
	entries = append(entries, scanSkillDir(claudeSkills, "claude")...)

	// OpenCode plugins: ~/.config/opencode/plugins/*.ts
	openCodePlugins := filepath.Join(home, ".config", "opencode", "plugins")
	entries = append(entries, scanPluginDir(openCodePlugins, "opencode")...)

	return &SkillCatalog{entries: entries}
}

// ForKitchen returns all skills available in a specific kitchen.
func (c *SkillCatalog) ForKitchen(kitchen string) []SkillEntry {
	var result []SkillEntry
	for _, e := range c.entries {
		if e.Kitchen == kitchen {
			result = append(result, e)
		}
	}
	return result
}

// HasSkill checks if any kitchen has a skill matching the query.
// The query must be at least 3 characters and must match a whole word
// in the skill name (not a substring of the description).
// Returns the kitchen name and skill entry, or empty if not found.
func (c *SkillCatalog) HasSkill(query string) (string, *SkillEntry) {
	lower := strings.ToLower(query)
	if len([]rune(lower)) < 3 {
		return "", nil
	}
	for i, e := range c.entries {
		if containsWholeWord(strings.ToLower(e.Name), lower) {
			return e.Kitchen, &c.entries[i]
		}
	}
	return "", nil
}

// containsWholeWord checks if target contains query as a whole word,
// where word boundaries are defined by '-', '_', and spaces.
func containsWholeWord(name, query string) bool {
	// Split the skill name into words by common separators
	words := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	for _, w := range words {
		if w == query {
			return true
		}
	}
	// Also check if query matches the full name
	return name == query
}

// Total returns the total number of skills across all kitchens.
func (c *SkillCatalog) Total() int {
	return len(c.entries)
}

// scanSkillDir reads SKILL.md files from a skills directory.
func scanSkillDir(dir, kitchen string) []SkillEntry {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var entries []SkillEntry
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		skillFile := filepath.Join(dir, de.Name(), "SKILL.md")
		desc := readSkillDescription(skillFile)
		entries = append(entries, SkillEntry{
			Name:        de.Name(),
			Description: desc,
			Kitchen:     kitchen,
		})
	}
	return entries
}

// scanPluginDir reads .ts files from a plugins directory.
func scanPluginDir(dir, kitchen string) []SkillEntry {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var entries []SkillEntry
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".ts") {
			continue
		}
		name := strings.TrimSuffix(de.Name(), ".ts")
		entries = append(entries, SkillEntry{
			Name:        name,
			Description: "OpenCode plugin",
			Kitchen:     kitchen,
		})
	}
	return entries
}

// readSkillDescription extracts the description from SKILL.md frontmatter.
func readSkillDescription(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	inFrontmatter := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			if inFrontmatter {
				return "" // end of frontmatter, no description found
			}
			inFrontmatter = true
			continue
		}
		if inFrontmatter && strings.HasPrefix(line, "description:") {
			desc := strings.TrimPrefix(line, "description:")
			desc = strings.TrimSpace(desc)
			desc = strings.Trim(desc, `"'`)
			return desc
		}
	}
	return ""
}
