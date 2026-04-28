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

package maitre

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCarte(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "carte.md")

	content := `## Carte — http-fault-injection

| Task | Kitchen | Station | Context Injection |
|------|---------|---------|-------------------|
| APP-R1 | claude | review | CodeGraph: client.py symbols |
| APP1 | opencode | code | CodeGraph: chaosnetwork probe pattern |
| APP2 | opencode | code | CodeGraph: latency.py, partition.py |
| APP5 | claude+opencode | think+code | CodeGraph: requests.Session calls |
| HTTP-GATE1 | claude | sign-off | Full pantry context |
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	carte, err := ParseCarte(path)
	if err != nil {
		t.Fatalf("ParseCarte: %v", err)
	}

	entries := carte.Entries()
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}

	if entries[0].Task != "APP-R1" {
		t.Errorf("entry 0 task: got %q, want 'APP-R1'", entries[0].Task)
	}
	if entries[0].Kitchen != "claude" {
		t.Errorf("entry 0 kitchen: got %q, want 'claude'", entries[0].Kitchen)
	}
	if entries[0].ContextSources != "CodeGraph: client.py symbols" {
		t.Errorf("entry 0 context: got %q", entries[0].ContextSources)
	}
}

func TestCarte_Route(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "carte.md")

	content := `| Task | Kitchen | Station | Context |
|------|---------|---------|---------|
| MW-6.1 | opencode | code | PantryDB schema |
| MW-7.1 | opencode | code | MCP client |
| MW-11.1 | claude | think | Risk analysis |
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	carte, err := ParseCarte(path)
	if err != nil {
		t.Fatal(err)
	}

	entry := carte.Route("MW-7.1")
	if entry == nil {
		t.Fatal("expected to find MW-7.1")
	}
	if entry.Kitchen != "opencode" {
		t.Errorf("expected opencode, got %q", entry.Kitchen)
	}

	entry = carte.Route("MW-99")
	if entry != nil {
		t.Error("expected nil for nonexistent task")
	}
}

func TestParseCarte_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "carte.md")

	if err := os.WriteFile(path, []byte("# No table here\nJust text."), 0o644); err != nil {
		t.Fatal(err)
	}

	carte, err := ParseCarte(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(carte.Entries()) != 0 {
		t.Errorf("expected 0 entries for file without table, got %d", len(carte.Entries()))
	}
}

func TestParseCarte_FileNotFound(t *testing.T) {
	t.Parallel()
	_, err := ParseCarte("/nonexistent/carte.md")
	if err == nil {
		t.Error("expected error for missing file")
	}
}
