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
