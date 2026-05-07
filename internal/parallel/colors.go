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

package parallel

import "github.com/mwigge/milliways/internal/termcolor"

// ProviderColor returns the ANSI escape prefix for a provider label.
// Reset with "\033[0m" after use.
func ProviderColor(provider string) string {
	if !ColorEnabled() {
		return ""
	}
	switch provider {
	case "claude":
		return "\033[97m" // pearl white
	case "codex":
		return "\033[38;5;214m" // amber
	case "copilot":
		return "\033[38;5;69m" // cornflower blue
	case "gemini":
		return "\033[38;5;208m" // orange
	case "local":
		return "\033[38;5;160m" // red
	case "minimax":
		return "\033[38;5;141m" // soft purple
	case "pool":
		return "\033[38;5;117m" // light blue
	default:
		return ""
	}
}

// ColorEnabled reports whether provider labels should emit ANSI color.
func ColorEnabled() bool {
	return termcolor.Enabled()
}

// ColorProvider wraps provider name in its color + reset.
func ColorProvider(provider string) string {
	c := ProviderColor(provider)
	if c == "" {
		return provider
	}
	return c + provider + "\033[0m"
}
