## ADDED Requirements

### Requirement: Welcome block on TUI startup

When the TUI starts, it SHALL create a welcome block as the first block in the block list. The welcome block SHALL auto-collapse after the user submits their first prompt, and SHALL NOT appear again in that session.

#### Scenario: Welcome block shows on startup

- **WHEN** the TUI finishes initialization (`Init()` completes)
- **THEN** a welcome block SHALL be added to the block list as the focused block
- **AND** the block SHALL display: milliways version, current mode (company/private/neutral), list of kitchens with status icons, keyboard shortcuts hint, and palace/codegraph availability

#### Scenario: Welcome block content

```
┌─ Milliways v0.2.0 — mode: private ──────────────────┐
│                                                     │
│  Kitchens:                                          │
│  ● claude    ○ gemini    ○ minimax    ○ aider      │
│  ● codex     ○ opencode  ○ groq       ○ goose       │
│                                                     │
│  Palace: 3 wings, 142 drawers | CodeGraph: indexed  │
│                                                     │
│  Keyboard: Enter=send  Ctrl+D=exit  /=commands      │
│            Ctrl+R=history  Tab=switch block        │
└─────────────────────────────────────────────────────┘
```

#### Scenario: Welcome block auto-collapses

- **WHEN** the user submits their first prompt (presses Enter with a non-empty input)
- **THEN** the welcome block SHALL be collapsed (height set to 1 line or hidden)
- **AND** subsequent blocks SHALL render in the viewport without the welcome block taking space

#### Scenario: Welcome block skipped on resume

- **WHEN** milliways is started with `--resume` or `--session` flag
- **THEN** the welcome block SHALL NOT be shown — the session restore flow takes precedence