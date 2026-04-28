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

package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Runner executes hook commands loaded from plugin hook configuration files.
type Runner struct {
	pluginsDir string
	hooks      map[Event][]HookConfig
}

// HookConfig describes one configured hook command.
type HookConfig struct {
	Command string
	Matcher string
	Timeout int
}

type hooksFile struct {
	Hooks map[Event][]hooksGroup `json:"hooks"`
}

type hooksGroup struct {
	Matcher string             `json:"matcher"`
	Hooks   []hookCommandEntry `json:"hooks"`
}

type hookCommandEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// NewRunner loads all plugin hook configurations from the given plugin root.
func NewRunner(pluginsDir string) (*Runner, error) {
	r := &Runner{
		pluginsDir: pluginsDir,
		hooks:      make(map[Event][]HookConfig),
	}
	if strings.TrimSpace(pluginsDir) == "" {
		return r, nil
	}
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return r, nil
		}
		return nil, fmt.Errorf("read plugins dir %q: %w", pluginsDir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := r.loadPlugin(filepath.Join(pluginsDir, entry.Name())); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// RunHooks executes all matching hooks for the given event.
func (r *Runner) RunHooks(ctx context.Context, event Event, payload HookPayload) HookResult {
	result := HookResult{}
	if r == nil {
		return result
	}
	current := payload
	for _, hook := range r.hooks[event] {
		if !matchesTool(hook.Matcher, payload.ToolName) {
			continue
		}
		hookResult, err := r.runHook(ctx, hook, current)
		if err != nil {
			return HookResult{Blocked: true, Message: err.Error()}
		}
		if hookResult.Blocked {
			return hookResult
		}
		if hookResult.Modified {
			current = hookResult.ModifiedPayload
			result.Modified = true
			result.ModifiedPayload = current
		}
	}
	return result
}

func (r *Runner) loadPlugin(pluginRoot string) error {
	configPath := filepath.Join(pluginRoot, "hooks", "hooks.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read hook config %q: %w", configPath, err)
	}
	var parsed hooksFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("decode hook config %q: %w", configPath, err)
	}
	for event, groups := range parsed.Hooks {
		for _, group := range groups {
			for _, hook := range group.Hooks {
				if hook.Type != "command" {
					continue
				}
				r.hooks[event] = append(r.hooks[event], HookConfig{
					Command: resolvePluginRoot(hook.Command, pluginRoot),
					Matcher: group.Matcher,
					Timeout: hook.Timeout,
				})
			}
		}
	}
	return nil
}

func (r *Runner) runHook(ctx context.Context, hook HookConfig, payload HookPayload) (HookResult, error) {
	timeout := time.Duration(hook.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	input, err := json.Marshal(payload)
	if err != nil {
		return HookResult{}, fmt.Errorf("marshal hook payload: %w", err)
	}
	cmd := exec.CommandContext(hookCtx, "sh", "-c", hook.Command)
	cmd.Stdin = strings.NewReader(string(input))
	output, err := cmd.Output()
	if err != nil {
		return HookResult{}, fmt.Errorf("execute hook %q: %w", hook.Command, err)
	}
	var result HookResult
	if err := json.Unmarshal(output, &result); err != nil {
		return HookResult{}, fmt.Errorf("decode hook result: %w", err)
	}
	if result.Modified && result.ModifiedPayload.Event == "" {
		result.ModifiedPayload = payload
	}
	return result, nil
}

func matchesTool(matcher, toolName string) bool {
	if strings.TrimSpace(matcher) == "" {
		return true
	}
	re, err := regexp.Compile(matcher)
	if err != nil {
		return false
	}
	return re.MatchString(toolName)
}

func resolvePluginRoot(command, pluginRoot string) string {
	return strings.ReplaceAll(command, "${CLAUDE_PLUGIN_ROOT}", pluginRoot)
}
