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

package sommelier

import "strings"

// TaskType classifies a prompt into a high-level task type for routing feedback.
var taskTypeKeywords = []struct {
	taskType string
	keywords []string
}{
	{"review", []string{"review", "audit", "check", "inspect", "verify"}},
	{"think", []string{"think", "plan", "design", "architect", "explore", "explain", "analyze", "why"}},
	{"refactor", []string{"refactor", "rename", "extract", "move", "reorganize", "clean"}},
	{"search", []string{"search", "find", "look up", "research", "compare", "what is"}},
	{"test", []string{"test", "spec", "coverage", "assert", "verify"}},
	{"code", []string{"code", "implement", "build", "add", "create", "write", "fix", "update"}},
}

// ClassifyTaskType returns the best task type for a prompt.
// Returns "general" if no keywords match.
func ClassifyTaskType(prompt string) string {
	lower := strings.ToLower(prompt)
	for _, tt := range taskTypeKeywords {
		for _, kw := range tt.keywords {
			if strings.Contains(lower, kw) {
				return tt.taskType
			}
		}
	}
	return "general"
}
