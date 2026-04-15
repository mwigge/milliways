# Spec: session-model

## Overview

The TUI MUST maintain a continuous session with scrollback. Each dispatch creates a section in the session. Output is never cleared between dispatches.

## Requirements

### Session continuity

- The output viewport MUST NOT be cleared between dispatches
- Each dispatch MUST create a new Section appended to the session
- The viewport MUST support scrolling back through all previous sections
- Sections MUST store: prompt, kitchen name, routing decision, output lines, result, cost, duration, rating

### Kitchen-prefixed output

- Every output line MUST be prefixed with `[kitchen_name]` where kitchen_name is the source kitchen
- The prefix MUST be color-coded using the kitchen's assigned color from kitchenColors
- System messages (routing info, quota warnings) MUST use a distinct `[milliways]` prefix in muted style

### Syntax highlighting

- Code blocks (EventCodeBlock) MUST be rendered with syntax highlighting using chroma
- The language field from the event MUST be used for highlighting; empty language triggers auto-detection
- Highlighting MUST use a terminal256-compatible theme (monokai or dracula)
- If highlighting fails, the code MUST be displayed without highlighting (graceful fallback)

### Markdown rendering toggle

- The default render mode MUST be raw markdown (plain text with syntax-highlighted code blocks)
- Ctrl+G MUST toggle between raw mode and glamour-rendered mode
- In glamour mode, section content MUST be rendered through glamour with headings, lists, tables styled
- The toggle MUST re-render the current viewport without losing scroll position
- Kitchen prefixes MUST remain visible in both modes

### Prompt echo

- When the user submits a prompt, the prompt text MUST appear immediately in the viewport as `▶ prompt_text` in muted style
- A separator line MUST follow the prompt echo
- The prompt echo MUST appear before any kitchen output

### Cross-kitchen summary

- Ctrl+S MUST display a session summary overlay
- The summary MUST show: total dispatches, kitchens used with counts, total duration, total cost (where available), success rate
- The summary MUST list recent dispatches with kitchen, prompt (truncated), duration, status, and cost
- The overlay MUST be dismissable with `q`
