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

package conversation

import (
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/editorcontext"
)

func TestRenderEditorContext(t *testing.T) {
	t.Parallel()

	t.Run("nil bundle", func(t *testing.T) {
		t.Parallel()

		if got := RenderEditorContext(nil); got != "" {
			t.Fatalf("RenderEditorContext(nil) = %q, want empty string", got)
		}
	})

	t.Run("includes lsp counts", func(t *testing.T) {
		t.Parallel()

		bundle := &editorcontext.Bundle{
			Collectors: map[string]*editorcontext.Collector{
				"nvim": {
					LSP: &editorcontext.LSPState{Errors: 3, Warnings: 1},
				},
			},
		}

		got := RenderEditorContext(bundle)
		if !strings.Contains(got, "LSP: 3 error(s), 1 warning(s)") {
			t.Fatalf("expected LSP summary in %q", got)
		}
	})

	t.Run("includes buffer git cursor and selection details", func(t *testing.T) {
		t.Parallel()

		bundle := &editorcontext.Bundle{
			Collectors: map[string]*editorcontext.Collector{
				"nvim": {
					Buffer: &editorcontext.BufferState{
						Path:     "internal/conversation/continue.go",
						Filetype: "go",
						Modified: true,
					},
					Git: &editorcontext.GitState{
						Branch:       "feature-branch",
						Dirty:        true,
						FilesChanged: 2,
					},
					Cursor: &editorcontext.CursorState{
						Line:   42,
						Column: 8,
						Scope:  "RenderEditorContext",
					},
					Selection: &editorcontext.Selection{
						StartLine: 10,
						EndLine:   14,
					},
				},
			},
		}

		got := RenderEditorContext(bundle)
		for _, want := range []string{
			"Editor Context:",
			"nvim: internal/conversation/continue.go [go] modified=true",
			"Git: feature-branch (dirty) 2 files changed",
			"Cursor: line 42, col 8 in RenderEditorContext",
			"Selection: 5 lines",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("expected %q in %q", want, got)
			}
		}
	})

	t.Run("truncates oversized content", func(t *testing.T) {
		t.Parallel()

		bundle := &editorcontext.Bundle{Collectors: map[string]*editorcontext.Collector{}}
		for i := 0; i < 30; i++ {
			bundle.Collectors[strings.Repeat("c", i+1)] = &editorcontext.Collector{
				Buffer: &editorcontext.BufferState{
					Path:     strings.Repeat("very/long/path/segment/", 20),
					Filetype: "go",
					Modified: true,
				},
			}
		}

		got := RenderEditorContext(bundle)
		if len(got) > maxRenderedEditorContextChars {
			t.Fatalf("expected rendered context length <= %d, got %d", maxRenderedEditorContextChars, len(got))
		}
		if !strings.Contains(got, "[...truncated...]") {
			t.Fatalf("expected truncation marker in %q", got)
		}
	})
}

func TestBuildContinuationPrompt(t *testing.T) {
	t.Parallel()

	t.Run("includes editor context when present", func(t *testing.T) {
		t.Parallel()

		c := New("conv-1", "b1", "review milliways")
		c.Memory.WorkingSummary = "We already inspected routing and adapters."
		c.Memory.NextAction = "Patch the failover orchestrator."
		c.Context.SpecRefs = []string{"openspec/provider-continuity"}
		c.Context.CodeGraphText = "Relevant code: sommelier, tui, adapters."
		c.Context.MemPalaceText = "Stored memory about previous handoff."
		c.AppendTurn(RoleAssistant, "claude", "I found a routing issue.")

		out := BuildContinuationPrompt(ContinueInput{
			Conversation: c,
			NextProvider: "codex",
			Reason:       "Claude became exhausted.",
			EditorContext: &editorcontext.Bundle{
				Collectors: map[string]*editorcontext.Collector{
					"nvim": {
						Buffer: &editorcontext.BufferState{Path: "internal/conversation/continue.go", Filetype: "go"},
					},
				},
			},
		})

		for _, want := range []string{
			"Original goal:",
			"Claude became exhausted.",
			"We already inspected routing and adapters.",
			"Patch the failover orchestrator.",
			"openspec/provider-continuity",
			"Relevant code: sommelier, tui, adapters.",
			"Stored memory about previous handoff.",
			"Editor context:\nEditor Context:",
			"nvim: internal/conversation/continue.go [go]",
			"Continue from the current state in codex.",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("continuation prompt missing %q", want)
			}
		}
	})

	t.Run("omits editor context when nil", func(t *testing.T) {
		t.Parallel()

		c := New("conv-2", "b2", "review milliways")
		out := BuildContinuationPrompt(ContinueInput{
			Conversation: c,
			NextProvider: "codex",
		})

		if strings.Contains(out, "Editor context:") {
			t.Fatalf("did not expect editor context section in %q", out)
		}
	})
}

func TestBuildContinuationPrompt_BoundsTranscript(t *testing.T) {
	t.Parallel()

	c := New("conv-1", "b1", "review milliways")
	for i := 0; i < 60; i++ {
		c.AppendTurn(RoleAssistant, "claude", strings.Repeat("x", 400))
	}

	out := BuildContinuationPrompt(ContinueInput{
		Conversation: c,
		NextProvider: "codex",
	})

	if !strings.Contains(out, "omitted to keep the continuation payload bounded") {
		t.Fatalf("expected bounded transcript notice, got %q", out)
	}
	if strings.Count(out, "[assistant:claude]") >= 60 {
		t.Fatalf("expected truncated transcript window, got full transcript")
	}
}
