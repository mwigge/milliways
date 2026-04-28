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

package textproc

import (
	"strings"
	"testing"
)

func TestExtractCodeBlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []CodeBlock
	}{
		{
			name: "no fences",
			in:   "just some text\nwith no code blocks",
			want: nil,
		},
		{
			name: "simple python",
			in:   "before\n```python\nprint(1)\n```\nafter",
			want: []CodeBlock{
				{Language: "python", Content: "print(1)"},
			},
		},
		{
			name: "filename info string (path-like)",
			in:   "```go internal/foo/bar.go\npackage foo\n```",
			want: []CodeBlock{
				{Language: "go", Filename: "internal/foo/bar.go", Content: "package foo"},
			},
		},
		{
			name: "filename via title= info-string",
			in:   "```python title=script.py\nprint(2)\n```",
			want: []CodeBlock{
				{Language: "python", Filename: "script.py", Content: "print(2)"},
			},
		},
		{
			name: "filename via filename= info-string",
			in:   "```go filename=cmd/main.go\npackage main\n```",
			want: []CodeBlock{
				{Language: "go", Filename: "cmd/main.go", Content: "package main"},
			},
		},
		{
			name: "two blocks",
			in:   "```go\npackage a\n```\n\n```python\nprint(3)\n```",
			want: []CodeBlock{
				{Language: "go", Content: "package a"},
				{Language: "python", Content: "print(3)"},
			},
		},
		{
			name: "tilde fence",
			in:   "~~~python\nprint(4)\n~~~",
			want: []CodeBlock{
				{Language: "python", Content: "print(4)"},
			},
		},
		{
			name: "no language",
			in:   "```\nplain text\n```",
			want: []CodeBlock{
				{Content: "plain text"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractCodeBlocks(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractCodeBlocks() returned %d blocks, want %d (%+v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i].Language != tt.want[i].Language {
					t.Errorf("blocks[%d].Language = %q, want %q", i, got[i].Language, tt.want[i].Language)
				}
				if got[i].Filename != tt.want[i].Filename {
					t.Errorf("blocks[%d].Filename = %q, want %q", i, got[i].Filename, tt.want[i].Filename)
				}
				if !strings.Contains(got[i].Content, tt.want[i].Content) {
					t.Errorf("blocks[%d].Content = %q, want contains %q", i, got[i].Content, tt.want[i].Content)
				}
			}
		})
	}
}
