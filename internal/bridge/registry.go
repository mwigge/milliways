package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AccessRules defines read/write permissions.
type AccessRules struct {
	Read  string `yaml:"read"`
	Write string `yaml:"write"`
}

// ProjectAccess configures access for one project group.
type ProjectAccess struct {
	Paths  []string    `yaml:"paths"`
	Access AccessRules `yaml:"access"`
}

// Registry holds project access rules from ~/.milliways/projects.yaml.
type Registry struct {
	projects map[string]ProjectAccess
	defaults AccessRules
}

type registryFile struct {
	Projects map[string]ProjectAccess `yaml:"projects"`
}

// LoadRegistry loads from ~/.milliways/projects.yaml if it exists.
// Returns nil if file doesn't exist (use defaults).
func LoadRegistry() (*Registry, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("lookup home dir: %w", err)
	}
	configPath := filepath.Join(homeDir, ".milliways", "projects.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read projects registry: %w", err)
	}

	var raw registryFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse projects registry: %w", err)
	}

	registry := &Registry{
		projects: make(map[string]ProjectAccess, len(raw.Projects)),
		defaults: defaultAccessRules(),
	}
	for name, project := range raw.Projects {
		project.Paths = normalizeRegistryPaths(project.Paths)
		project.Access = normalizeAccessRules(project.Access)
		if name == "default" {
			registry.defaults = project.Access
			continue
		}
		registry.projects[name] = project
	}

	return registry, nil
}

// GetAccess returns AccessRules for the given palace path.
// Returns default rules if no explicit entry.
func (r *Registry) GetAccess(palacePath string) AccessRules {
	if r == nil {
		return defaultAccessRules()
	}
	normalizedPalace := normalizePalacePath(palacePath)
	for _, project := range r.projects {
		for _, basePath := range project.Paths {
			if matchesRegistryPath(normalizedPalace, basePath) {
				return normalizeAccessRules(project.Access)
			}
		}
	}
	return normalizeAccessRules(r.defaults)
}

func defaultAccessRules() AccessRules {
	return AccessRules{Read: "all", Write: "project"}
}

func normalizeAccessRules(rules AccessRules) AccessRules {
	defaults := defaultAccessRules()
	read := strings.TrimSpace(rules.Read)
	if read == "" {
		read = defaults.Read
	}
	write := strings.TrimSpace(rules.Write)
	if write == "" {
		write = defaults.Write
	}
	return AccessRules{Read: read, Write: write}
}

func normalizeRegistryPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		expanded := expandHome(path)
		if expanded == "" {
			continue
		}
		out = append(out, filepath.Clean(expanded))
	}
	return out
}

func matchesRegistryPath(palacePath, basePath string) bool {
	if palacePath == "" || basePath == "" {
		return false
	}
	if palacePath == basePath {
		return true
	}
	rel, err := filepath.Rel(basePath, palacePath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func expandHome(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return trimmed
		}
		if trimmed == "~" {
			return homeDir
		}
		return filepath.Join(homeDir, strings.TrimPrefix(trimmed, "~/"))
	}
	return trimmed
}
