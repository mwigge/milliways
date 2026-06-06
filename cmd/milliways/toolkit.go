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
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// toolkitSubDirs is the ordered set of .claude/ subdirectories to bundle.
var toolkitSubDirs = []string{"skills", "rules", "agents", "commands"}

// toolkitMaxFileBytes caps each individual file read to avoid injecting
// unreasonably large documents into every prompt.
const toolkitMaxFileBytes = 8 * 1024

// scanToolkitBundle reads CLAUDE.md and .claude/{skills,rules,agents,commands}
// from dir and returns a single bundled string ready for prompt injection.
// Returns "" if no content is found.
func scanToolkitBundle(dir string) string {
	if dir == "" {
		return ""
	}
	var sections []string

	// Root CLAUDE.md.
	if content := readToolkitFile(filepath.Join(dir, "CLAUDE.md")); content != "" {
		sections = append(sections, "## CLAUDE.md\n\n"+content)
	}

	// .claude/{skills,rules,agents,commands}/*.md
	for _, sub := range toolkitSubDirs {
		subPath := filepath.Join(dir, ".claude", sub)
		entries, err := os.ReadDir(subPath)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".txt") {
				continue
			}
			content := readToolkitFile(filepath.Join(subPath, name))
			if content == "" {
				continue
			}
			sections = append(sections, fmt.Sprintf("## .claude/%s/%s\n\n%s", sub, name, content))
		}
	}

	return strings.Join(sections, "\n\n---\n\n")
}

// readToolkitFile reads path and returns its content trimmed to toolkitMaxFileBytes.
func readToolkitFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, toolkitMaxFileBytes)
	n, _ := f.Read(buf)
	return strings.TrimSpace(string(buf[:n]))
}
