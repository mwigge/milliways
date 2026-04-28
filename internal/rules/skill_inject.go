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

import "strings"

// InjectSkills appends matched skill content to an agent context.
func (l *RulesLoader) InjectSkills(input string, agentCtx string) string {
	context := strings.TrimSpace(agentCtx)
	for _, skillName := range l.MatchSkills(input) {
		content := strings.TrimSpace(readOptionalFile(l.skills[skillName]))
		if content == "" {
			continue
		}
		if context == "" {
			context = content
			continue
		}
		context += "\n\n" + content
	}
	return context
}
