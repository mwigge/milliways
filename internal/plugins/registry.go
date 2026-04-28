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

package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mwigge/milliways/internal/commands"
	"github.com/mwigge/milliways/internal/hooks"
)

// Registry tracks all loaded plugins.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]*Plugin
	loader  PluginLoader
}

// NewRegistry creates an empty plugin registry.
func NewRegistry() *Registry {
	return &Registry{plugins: make(map[string]*Plugin)}
}

// Load loads one plugin directory into the registry.
func (r *Registry) Load(path string) (*Plugin, error) {
	plugin, err := r.loader.Load(path)
	if err != nil {
		return nil, err
	}

	namespace := pluginNamespace(path)
	for index := range plugin.Commands {
		plugin.Commands[index].Namespace = namespace
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[plugin.Name] = plugin
	return plugin, nil
}

// LoadAll loads every plugin found beneath dirs.
func (r *Registry) LoadAll(dirs []string) error {
	for _, dir := range dirs {
		resolved := expandHome(strings.TrimSpace(dir))
		if resolved == "" {
			continue
		}
		if err := r.loadDirectory(resolved); err != nil {
			return err
		}
	}
	return nil
}

// ListCommands returns all registered commands.
func (r *Registry) ListCommands() []commands.Command {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]commands.Command, 0)
	for _, plugin := range r.plugins {
		result = append(result, plugin.Commands...)
	}
	sort.Slice(result, func(i, j int) bool {
		leftNamespace := result[i].Namespace
		rightNamespace := result[j].Namespace
		if leftNamespace != rightNamespace {
			return namespaceRank(leftNamespace) < namespaceRank(rightNamespace)
		}
		return result[i].Name < result[j].Name
	})
	return result
}

// ListAgents returns all registered agents.
func (r *Registry) ListAgents() []Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Agent, 0)
	for _, plugin := range r.plugins {
		result = append(result, plugin.Agents...)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// ListSkills returns all registered skills.
func (r *Registry) ListSkills() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Skill, 0)
	for _, plugin := range r.plugins {
		result = append(result, plugin.Skills...)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// ListHooks returns all hooks grouped by event.
func (r *Registry) ListHooks() map[string][]hooks.HookSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]hooks.HookSpec)
	for _, plugin := range r.plugins {
		for event, specs := range plugin.Hooks {
			result[event] = append(result[event], specs...)
		}
	}
	return result
}

func (r *Registry) loadDirectory(dir string) error {
	if hasPluginManifest(dir) {
		_, err := r.Load(dir)
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read plugin root %q: %w", dir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(dir, entry.Name())
		if !hasPluginManifest(candidate) {
			continue
		}
		if _, err := r.Load(candidate); err != nil {
			return err
		}
	}
	return nil
}

func hasPluginManifest(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".claude-plugin", "plugin.json"))
	return err == nil
}

func pluginNamespace(path string) string {
	cleaned := filepath.ToSlash(path)
	if strings.Contains(cleaned, "/.claude/plugins/") {
		return "project"
	}
	return "user"
}

func expandHome(path string) string {
	if path == "" || !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

func namespaceRank(namespace string) int {
	switch namespace {
	case "user":
		return 0
	case "project":
		return 1
	default:
		return 2
	}
}
