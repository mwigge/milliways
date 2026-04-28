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
	"strings"

	"github.com/mwigge/milliways/internal/commands"
	"github.com/mwigge/milliways/internal/config"
	"github.com/mwigge/milliways/internal/hooks"
	"gopkg.in/yaml.v3"
)

// Plugin groups commands, agents, skills, and hooks under one root.
type Plugin struct {
	Name        string
	Root        string
	Description string
	Commands    []commands.Command
	Agents      []Agent
	Skills      []Skill
	Hooks       map[string][]hooks.HookSpec
}

// PluginLoader loads plugin assets from disk.
type PluginLoader struct{}

type pluginManifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Load reads a plugin manifest and all plugin assets rooted at root.
func (l PluginLoader) Load(root string) (*Plugin, error) {
	manifest, err := loadManifest(root)
	if err != nil {
		return nil, err
	}

	loadedCommands, err := commands.LoadCommands(filepath.Join(root, "commands"))
	if err != nil {
		return nil, err
	}

	loadedAgents, err := loadAgents(root)
	if err != nil {
		return nil, err
	}

	loadedSkills, err := LoadSkills(root)
	if err != nil {
		return nil, err
	}

	loadedHooks, err := hooks.LoadHooks(root)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(manifest.Name)
	if name == "" {
		name = filepath.Base(root)
	}

	return &Plugin{
		Name:        name,
		Root:        root,
		Description: strings.TrimSpace(manifest.Description),
		Commands:    loadedCommands,
		Agents:      loadedAgents,
		Skills:      loadedSkills,
		Hooks:       loadedHooks.Hooks,
	}, nil
}

func loadManifest(root string) (pluginManifest, error) {
	path := filepath.Join(root, ".claude-plugin", "plugin.json")
	if err := config.GuardReadPath(path); err != nil {
		return pluginManifest{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return pluginManifest{}, fmt.Errorf("read plugin manifest %q: %w", path, err)
	}

	var manifest pluginManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return pluginManifest{}, fmt.Errorf("decode plugin manifest %q: %w", path, err)
	}
	return manifest, nil
}

func loadAgents(root string) ([]Agent, error) {
	dir := filepath.Join(root, "agents")
	if err := config.GuardReadPath(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read agents dir %q: %w", dir, err)
	}

	agents := make([]Agent, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		agent, err := loadAgentFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

type agentFrontmatter struct {
	Name        string  `yaml:"name"`
	Description string  `yaml:"description"`
	Model       string  `yaml:"model"`
	MaxTokens   int     `yaml:"max_tokens"`
	Temperature float64 `yaml:"temperature"`
}

func loadAgentFile(path string) (Agent, error) {
	if err := config.GuardReadPath(path); err != nil {
		return Agent{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Agent{}, fmt.Errorf("read agent %q: %w", path, err)
	}

	frontmatter, body, err := parseAgentDocument(string(data))
	if err != nil {
		return Agent{}, fmt.Errorf("parse agent %q: %w", path, err)
	}

	name := strings.TrimSpace(frontmatter.Name)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	return Agent{
		Name:        name,
		Description: strings.TrimSpace(frontmatter.Description),
		Model:       strings.TrimSpace(frontmatter.Model),
		MaxTokens:   frontmatter.MaxTokens,
		Temperature: frontmatter.Temperature,
		Prompt:      body,
	}, nil
}

func parseAgentDocument(content string) (agentFrontmatter, string, error) {
	trimmed := strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(trimmed, "---\n") {
		return agentFrontmatter{}, strings.TrimSpace(trimmed), nil
	}

	rest := strings.TrimPrefix(trimmed, "---\n")
	separator := "\n---\n"
	index := strings.Index(rest, separator)
	if index < 0 {
		return agentFrontmatter{}, "", fmt.Errorf("missing closing frontmatter delimiter")
	}

	frontmatterText := rest[:index]
	body := rest[index+len(separator):]

	var frontmatter agentFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatterText), &frontmatter); err != nil {
		return agentFrontmatter{}, "", fmt.Errorf("decode frontmatter: %w", err)
	}

	return frontmatter, strings.TrimSpace(body), nil
}
