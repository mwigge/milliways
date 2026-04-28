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

import "strings"

// ParseTextToEvents splits text content into EventText and EventCodeBlock events.
// Fenced code blocks (triple backtick) are detected and classified with their
// language tag. Text outside code blocks becomes EventText events (one per line).
func ParseTextToEvents(kitchenName, text string) []Event {
	if text == "" {
		return nil
	}

	var events []Event
	lines := strings.Split(text, "\n")
	var inBlock bool
	var blockLang string
	var blockLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !inBlock && strings.HasPrefix(trimmed, "```") {
			// Opening a code block
			inBlock = true
			blockLang = strings.TrimPrefix(trimmed, "```")
			blockLang = strings.TrimSpace(blockLang)
			blockLines = nil
			continue
		}

		if inBlock && strings.TrimSpace(line) == "```" {
			// Closing the code block
			events = append(events, Event{
				Type:     EventCodeBlock,
				Kitchen:  kitchenName,
				Language: blockLang,
				Code:     strings.Join(blockLines, "\n"),
			})
			inBlock = false
			blockLang = ""
			blockLines = nil
			continue
		}

		if inBlock {
			blockLines = append(blockLines, line)
		} else {
			events = append(events, Event{
				Type:    EventText,
				Kitchen: kitchenName,
				Text:    line,
			})
		}
	}

	// Unclosed code block: emit remaining lines as text
	if inBlock {
		// Emit the opening fence as text
		fenceLine := "```"
		if blockLang != "" {
			fenceLine += blockLang
		}
		events = append(events, Event{
			Type:    EventText,
			Kitchen: kitchenName,
			Text:    fenceLine,
		})
		for _, line := range blockLines {
			events = append(events, Event{
				Type:    EventText,
				Kitchen: kitchenName,
				Text:    line,
			})
		}
	}

	return events
}
