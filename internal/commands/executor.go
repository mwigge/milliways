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

package commands

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var bashFencePattern = regexp.MustCompile("(?s)```bash\\s*\\n(.*?)\\n```")

// Executor runs shell snippets embedded in command markdown.
type Executor struct {
	workDir string
}

// NewExecutor creates a command executor for workDir.
func NewExecutor(workDir string) Executor {
	return Executor{workDir: workDir}
}

// Execute substitutes named arguments and runs embedded bash fences.
func (e Executor) Execute(cmd Command, args map[string]string) (string, error) {
	rendered, err := substituteArguments(cmd.Content, args)
	if err != nil {
		return "", err
	}

	matches := bashFencePattern.FindAllStringSubmatch(rendered, -1)
	if len(matches) == 0 {
		return "", nil
	}

	outputs := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		script := strings.TrimSpace(match[1])
		if script == "" {
			continue
		}

		execCmd := exec.Command("sh", "-c", script)
		if e.workDir != "" {
			execCmd.Dir = e.workDir
		}
		output, err := execCmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("execute command %q: %w: %s", cmd.Name, err, strings.TrimSpace(string(output)))
		}
		outputs = append(outputs, strings.TrimRight(string(output), "\n"))
	}

	return strings.Join(outputs, "\n---\n"), nil
}

func substituteArguments(content string, args map[string]string) (string, error) {
	missing := make([]string, 0)
	seenMissing := make(map[string]struct{})

	rendered := argumentPattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := argumentPattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		name := parts[1]
		value, ok := args[name]
		if !ok {
			if _, seen := seenMissing[name]; !seen {
				missing = append(missing, name)
				seenMissing[name] = struct{}{}
			}
			return match
		}
		return value
	})

	if len(missing) > 0 {
		return "", fmt.Errorf("missing required arguments: %s", strings.Join(missing, ", "))
	}

	return rendered, nil
}
