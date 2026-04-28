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

package memory

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/mempalace"
	"github.com/mwigge/milliways/internal/session"
)

func TestBuildSystemPromptIncludesMemoryHitsAndConversation(t *testing.T) {
	t.Parallel()

	prompt := BuildSystemPrompt(
		[]mempalace.SearchResult{{Wing: "private", Room: "sessions", Content: "screenLineMap implementation", Relevance: 0.91, FactSummary: "mouse selection"}},
		[]MemoryEntry{{Key: "active_branch", Value: "feat/tui", CreatedAt: time.Now()}},
		&session.Session{Messages: []session.Message{{Role: session.RoleUser, Content: "Fix scrolling"}, {Role: session.RoleAssistant, Content: "Working on it"}}},
	)

	for _, want := range []string{
		"[Session Context]",
		"active_branch: feat/tui",
		"private/sessions",
		"screenLineMap implementation",
		"user: Fix scrolling",
		"assistant: Working on it",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildSystemPromptCompactsLongConversation(t *testing.T) {
	t.Parallel()

	messages := make([]session.Message, 0, 25)
	for index := 0; index < 25; index++ {
		messages = append(messages, session.Message{Role: session.RoleUser, Content: fmt.Sprintf("turn-%02d", index)})
	}
	prompt := BuildSystemPrompt(nil, nil, &session.Session{Messages: messages})
	if !strings.Contains(prompt, "recent 5 of 25 turns") {
		t.Fatalf("prompt = %q, want compact conversation heading", prompt)
	}
	if strings.Contains(prompt, "turn-04") {
		t.Fatal("prompt should not include early turns when compacted")
	}
	if !strings.Contains(prompt, "turn-24") {
		t.Fatal("prompt should include recent turns when compacted")
	}
}
