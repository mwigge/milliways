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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mwigge/milliways/internal/config"
)

const pluginRootVariable = "${CLAUDE_PLUGIN_ROOT}"

// HookSpec defines one concrete hook command.
type HookSpec struct {
	Type    string
	Command string
	Timeout int
	Matcher string
}

// HookFile contains all hook registrations for one plugin.
type HookFile struct {
	Description string
	Hooks       map[string][]HookSpec
}

type rawHookFile struct {
	Description string                    `json:"description"`
	Hooks       map[string][]rawHookGroup `json:"hooks"`
}

type rawHookGroup struct {
	Hooks   []rawHookSpec `json:"hooks"`
	Matcher string        `json:"matcher"`
}

type rawHookSpec struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// LoadHooks loads and resolves hooks/hooks.json for a plugin.
func LoadHooks(pluginRoot string) (HookFile, error) {
	path := filepath.Join(pluginRoot, "hooks", "hooks.json")
	if err := config.GuardReadPath(path); err != nil {
		if os.IsNotExist(err) {
			return HookFile{Hooks: map[string][]HookSpec{}}, nil
		}
		return HookFile{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return HookFile{Hooks: map[string][]HookSpec{}}, nil
		}
		return HookFile{}, fmt.Errorf("read hooks file %q: %w", path, err)
	}

	var raw rawHookFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return HookFile{}, fmt.Errorf("decode hooks file %q: %w", path, err)
	}

	result := HookFile{
		Description: raw.Description,
		Hooks:       make(map[string][]HookSpec, len(raw.Hooks)),
	}

	for event, groups := range raw.Hooks {
		flattened := make([]HookSpec, 0)
		for _, group := range groups {
			for _, hook := range group.Hooks {
				flattened = append(flattened, HookSpec{
					Type:    hook.Type,
					Command: strings.ReplaceAll(hook.Command, pluginRootVariable, pluginRoot),
					Timeout: hook.Timeout,
					Matcher: group.Matcher,
				})
			}
		}
		result.Hooks[event] = flattened
	}

	return result, nil
}
