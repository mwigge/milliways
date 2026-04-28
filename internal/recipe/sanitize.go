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

package recipe

import "strings"

func sanitizePromptInjection(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		trimmedLower := strings.TrimSpace(lower)
		if strings.Contains(lower, "ignore previous") ||
			strings.Contains(lower, "disregard") ||
			strings.Contains(lower, "forget all") ||
			strings.Contains(lower, "overwrite instructions") ||
			strings.Contains(lower, "you are now") ||
			strings.Contains(lower, "you are a") ||
			strings.HasPrefix(trimmedLower, "> ignore") ||
			strings.Contains(lower, "instead of following the instructions") {
			lines[i] = "# [filtered] " + line
		}
	}
	return strings.Join(lines, "\n")
}
