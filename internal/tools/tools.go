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

package tools

import (
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/provider"
)

// NewBuiltInRegistry returns a registry populated with all built-in tools.
func NewBuiltInRegistry() *Registry {
	return NewBuiltInRegistryWithStore(nil)
}

// NewBuiltInRegistryWithStore returns a registry populated with all built-in
// tools, backed by the given SecurityStore for the security_scan tool.
func NewBuiltInRegistryWithStore(store *pantry.SecurityStore) *Registry {
	r := NewRegistry()
	r.Register("Bash", handleBash, provider.ToolDef{
		Name:        "Bash",
		Description: "Execute a shell command",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
				"timeout": map[string]any{"type": "number"},
			},
			"required": []string{"command"},
		},
	})
	r.Register("Edit", handleEdit, provider.ToolDef{
		Name:        "Edit",
		Description: "Apply a unified diff to a file",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
				"diff": map[string]any{"type": "string"},
			},
			"required": []string{"path", "diff"},
		},
	})
	r.Register("Glob", handleGlob, provider.ToolDef{
		Name:        "Glob",
		Description: "Match files by glob pattern",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"pattern": map[string]any{"type": "string"},
			},
			"required": []string{"pattern"},
		},
	})
	r.Register("Grep", handleGrep, provider.ToolDef{
		Name:        "Grep",
		Description: "Search files with a regular expression",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"pattern": map[string]any{"type": "string"},
				"include": map[string]any{"type": "string"},
			},
			"required": []string{"pattern"},
		},
	})
	r.Register("Read", handleRead, provider.ToolDef{
		Name:        "Read",
		Description: "Read file contents",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":      map[string]any{"type": "string"},
				"file_path": map[string]any{"type": "string"},
				"limit":     map[string]any{"type": "number"},
			},
		},
	})
	r.Register("WebFetch", handleWebFetch, provider.ToolDef{
		Name:        "WebFetch",
		Description: "Fetch a URL over HTTP",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":     map[string]any{"type": "string"},
				"timeout": map[string]any{"type": "number"},
			},
			"required": []string{"url"},
		},
	})
	r.Register("Write", handleWrite, provider.ToolDef{
		Name:        "Write",
		Description: "Write file contents",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":      map[string]any{"type": "string"},
				"file_path": map[string]any{"type": "string"},
				"content":   map[string]any{"type": "string"},
			},
			"required": []string{"content"},
		},
	})
	r.Register("security_scan", securityScanHandler(store), securityScanToolDef())
	return r
}
