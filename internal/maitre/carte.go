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
	"bufio"
	"fmt"
	"os"
	"strings"
)

// CarteEntry maps a task to a kitchen with context injection instructions.
type CarteEntry struct {
	Task           string
	Kitchen        string
	Station        string
	ContextSources string
}

// Carte holds parsed carte.md routing instructions for an OpenSpec change.
type Carte struct {
	entries []CarteEntry
}

// ParseCarte reads a carte.md file and extracts the routing table.
// Expected format: markdown table with columns: Task | Kitchen | Station | Context Injection
func ParseCarte(path string) (*Carte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening carte.md: %w", err)
	}
	defer func() { _ = f.Close() }()

	var entries []CarteEntry
	inTable := false
	headerParsed := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Detect table start
		if strings.HasPrefix(line, "|") && strings.Contains(line, "Task") && strings.Contains(line, "Kitchen") {
			inTable = true
			headerParsed = false
			continue
		}

		if !inTable {
			continue
		}

		// Skip separator line (|---|---|---|...)
		if strings.Contains(line, "---") {
			headerParsed = true
			continue
		}

		if !headerParsed {
			continue
		}

		// Stop at non-table line
		if !strings.HasPrefix(line, "|") {
			inTable = false
			continue
		}

		// Parse table row
		cells := splitTableRow(line)
		if len(cells) < 2 {
			continue
		}

		entry := CarteEntry{
			Task:    strings.TrimSpace(cells[0]),
			Kitchen: strings.TrimSpace(cells[1]),
		}
		if len(cells) > 2 {
			entry.Station = strings.TrimSpace(cells[2])
		}
		if len(cells) > 3 {
			entry.ContextSources = strings.TrimSpace(cells[3])
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading carte.md: %w", err)
	}

	return &Carte{entries: entries}, nil
}

// Route finds the kitchen and context for a task ID.
func (c *Carte) Route(taskID string) *CarteEntry {
	for i, e := range c.entries {
		if strings.Contains(e.Task, taskID) {
			return &c.entries[i]
		}
	}
	return nil
}

// Entries returns all parsed carte entries.
func (c *Carte) Entries() []CarteEntry {
	return c.entries
}

func splitTableRow(line string) []string {
	// Remove leading/trailing pipes and split
	line = strings.Trim(line, "|")
	parts := strings.Split(line, "|")
	var cells []string
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}
