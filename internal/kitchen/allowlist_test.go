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

package kitchen

import "testing"

func TestIsCmdAllowed_CanonicalKitchens(t *testing.T) {
	t.Parallel()
	canonical := []string{
		"claude",
		"codex",
		"gpt",
		"opencode",
		"gemini",
		"aider",
		"goose",
		"cline",
	}
	for _, name := range canonical {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !IsCmdAllowed(name) {
				t.Errorf("IsCmdAllowed(%q) = false, want true (canonical kitchen must be in allowlist)", name)
			}
		})
	}
}

func TestIsCmdAllowed_PathBasename(t *testing.T) {
	t.Parallel()
	path := "/tmp/mw-smoke/bin/codex"
	if !IsCmdAllowed(path) {
		t.Errorf("IsCmdAllowed(%q) = false, want true (basename fallback must allow codex)", path)
	}
}

func TestIsCmdAllowed_KitchenNameFallback(t *testing.T) {
	t.Parallel()
	if !IsCmdAllowedKitchen("/tmp/mw-smoke/bin/fake-claude-streaming", "irrelevant-name") {
		t.Fatal("expected smoke test binary basename to be allowlisted")
	}
}
