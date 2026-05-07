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

package main

import (
	"strings"
	"testing"
)

func TestSessionDeckRetainsBuffersAcrossSwitches(t *testing.T) {
	deck := newSessionDeck([]string{"codex", "gemini"})
	deck.SetActive("codex")
	deck.BindSession("codex", 41)
	deck.MarkPrompt("codex", "what is 2+3?")
	deck.AppendData("codex", "5", true)
	deck.MarkChunkEnd("codex", 10, 1, 0.001, true)

	deck.SetActive("gemini")
	deck.BindSession("gemini", 42)
	deck.MarkPrompt("gemini", "what is 3+4?")
	deck.AppendData("gemini", "7", true)

	deck.SetActive("codex")
	provider, blocks := deck.ActiveBuffer()
	if provider != "codex" {
		t.Fatalf("active provider = %q, want codex", provider)
	}
	rendered := renderActiveClientBuffer(provider, blocks, 8)
	for _, want := range []string{"codex", "what is 2+3?", "5"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("active buffer missing %q:\n%s", want, rendered)
		}
	}
}

func TestSessionDeckObservabilityTracksBackgroundUnread(t *testing.T) {
	deck := newSessionDeck([]string{"codex", "gemini"})
	deck.SetActive("codex")
	deck.BindSession("gemini", 42)
	deck.MarkThinking("gemini", "checking files")
	deck.AppendData("gemini", "partial response", false)

	snap := deck.Snapshot()
	rendered := renderObservabilityPanel(snap, 120)
	for _, want := range []string{"gemini", "streaming", "unread:2", "checking files"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("observability missing %q:\n%s", want, rendered)
		}
	}
}

func TestSessionStatusPanelShowsActiveModelAndTotals(t *testing.T) {
	deck := newSessionDeck([]string{"codex"})
	deck.SetActive("codex")
	deck.BindSession("codex", 99)
	deck.MarkChunkEnd("codex", 100, 25, 0.25, true)

	rendered := renderSessionStatusPanel(deck.Snapshot(), 160)
	for _, want := range []string{"session codex", "handle 99", "125 tok $0.25", "saved"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("status panel missing %q:\n%s", want, rendered)
		}
	}
}

func TestSessionDeckPreservesObservedModelAcrossActivation(t *testing.T) {
	deck := newSessionDeck([]string{"codex"})
	deck.ApplyDaemonSnapshot(daemonDeckSnapshot{
		Active: "codex",
		Sessions: []daemonDeckSessionStatus{{
			AgentID:     "codex",
			Handle:      99,
			Status:      deckStatusIdle,
			Model:       "gpt-5.5",
			ModelSource: "observed",
		}},
	})

	deck.SetActive("codex")
	deck.BindSession("codex", 99)

	snap := deck.Snapshot()
	if len(snap.States) != 1 {
		t.Fatalf("states = %d, want 1", len(snap.States))
	}
	if got := snap.States[0].Model; got != "gpt-5.5" {
		t.Fatalf("model after activation = %q, want observed gpt-5.5", got)
	}
	if got := snap.States[0].ModelSource; got != "observed" {
		t.Fatalf("model source after activation = %q, want observed", got)
	}
	rendered := renderSessionStatusPanel(snap, 160)
	if !strings.Contains(rendered, "model gpt-5.5 (observed)") {
		t.Fatalf("status panel missing observed model source:\n%s", rendered)
	}
}

func TestSessionDeckNextPrevious(t *testing.T) {
	deck := newSessionDeck([]string{"claude", "codex", "gemini"})
	deck.SetActive("claude")
	if got := deck.Next(1); got != "codex" {
		t.Fatalf("next = %q, want codex", got)
	}
	if got := deck.Next(-1); got != "claude" {
		t.Fatalf("previous = %q, want claude", got)
	}
	if got := deck.Next(-1); got != "gemini" {
		t.Fatalf("previous wrap = %q, want gemini", got)
	}
}
