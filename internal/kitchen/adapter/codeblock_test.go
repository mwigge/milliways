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

package adapter

import (
	"testing"
)

func TestParseTextToEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		kitchen   string
		text      string
		wantCount int
		wantTypes []EventType
		wantLang  string // expected language on first CodeBlock (if any)
		wantCode  string // expected code on first CodeBlock (if any)
	}{
		{
			name:      "empty text",
			kitchen:   "claude",
			text:      "",
			wantCount: 0,
		},
		{
			name:      "plain text no code blocks",
			kitchen:   "claude",
			text:      "line one\nline two\nline three",
			wantCount: 3,
			wantTypes: []EventType{EventText, EventText, EventText},
		},
		{
			name:      "single code block with language",
			kitchen:   "opencode",
			text:      "before\n```go\nfmt.Println(\"hi\")\n```\nafter",
			wantCount: 3, // text, codeblock, text
			wantTypes: []EventType{EventText, EventCodeBlock, EventText},
			wantLang:  "go",
			wantCode:  "fmt.Println(\"hi\")",
		},
		{
			name:      "code block without language",
			kitchen:   "gemini",
			text:      "```\nsome code\n```",
			wantCount: 1,
			wantTypes: []EventType{EventCodeBlock},
			wantLang:  "",
			wantCode:  "some code",
		},
		{
			name:      "multiple code blocks",
			kitchen:   "claude",
			text:      "intro\n```python\nprint(1)\n```\nmiddle\n```rust\nfn main() {}\n```\nend",
			wantCount: 5,
			wantTypes: []EventType{EventText, EventCodeBlock, EventText, EventCodeBlock, EventText},
			wantLang:  "python",
			wantCode:  "print(1)",
		},
		{
			name:      "unclosed code block treated as text",
			kitchen:   "codex",
			text:      "before\n```go\nfn leaked()\nmore stuff",
			wantCount: 4, // "before", "```go", "fn leaked()", "more stuff"
			wantTypes: []EventType{EventText, EventText, EventText, EventText},
		},
		{
			name:      "empty code block",
			kitchen:   "claude",
			text:      "```go\n```",
			wantCount: 1,
			wantTypes: []EventType{EventCodeBlock},
			wantLang:  "go",
			wantCode:  "",
		},
		{
			name:      "multiline code block",
			kitchen:   "claude",
			text:      "```go\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```",
			wantCount: 1,
			wantTypes: []EventType{EventCodeBlock},
			wantLang:  "go",
			wantCode:  "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			events := ParseTextToEvents(tt.kitchen, tt.text)

			if len(events) != tt.wantCount {
				t.Fatalf("got %d events, want %d; events: %+v", len(events), tt.wantCount, events)
			}

			for i, wantType := range tt.wantTypes {
				if events[i].Type != wantType {
					t.Errorf("event[%d].Type = %v, want %v", i, events[i].Type, wantType)
				}
				if events[i].Kitchen != tt.kitchen {
					t.Errorf("event[%d].Kitchen = %q, want %q", i, events[i].Kitchen, tt.kitchen)
				}
			}

			if tt.wantLang != "" || tt.wantCode != "" {
				for _, e := range events {
					if e.Type == EventCodeBlock {
						if e.Language != tt.wantLang {
							t.Errorf("CodeBlock.Language = %q, want %q", e.Language, tt.wantLang)
						}
						if e.Code != tt.wantCode {
							t.Errorf("CodeBlock.Code = %q, want %q", e.Code, tt.wantCode)
						}
						break
					}
				}
			}
		})
	}
}
