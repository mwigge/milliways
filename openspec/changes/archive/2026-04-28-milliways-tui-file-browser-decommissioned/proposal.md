# Proposal — milliways-tui-file-browser

## Problem Statement

When working in the milliways TUI, users often need to reference specific files in their prompts (e.g., `@file path/to/file.go`). Currently, this requires switching to a terminal file explorer or remembering and typing the path manually. The TUI has no built-in way to browse, navigate, or insert file paths into the prompt.

## Solution

Add a **File Browser** as a 10th side panel in the milliways TUI. Users cycle to it via `⌥]/⌥[` or `Ctrl+J/K`, navigate the file tree, and press `Enter` to insert the selected file path into the prompt. File paths are relative to the current project root.

## Motivation

- Seamless file referencing without leaving the TUI
- Visual file tree navigation for large projects
- Consistent with the panel-based TUI design
- Complements the existing `@file` fuzzy reference feature

## Scope

**In scope:**
- File browser side panel (SidePanelFile type)
- Directory tree rendering with indentation and folder icons
- Arrow key navigation (↑/↓ within dir, →/← enter/leave dir)
- `Enter` inserts selected file path into prompt
- Respects `project.root` (current project repo root)
- Toggle hidden files (.gitignore rules or show-all option)
- Wire into existing side panel cycling system

**Out of scope:**
- File content preview
- In-panel file editing
- git-aware file ordering (can be added later as a separate panel or mode)
- Remote file system support

## Success Criteria

- [ ] `⌥]/⌥[` cycles to the file browser panel
- [ ] Directory tree is rendered with proper indentation
- [ ] ↑/↓ navigates within current directory
- [ ] → enters a subdirectory; ← returns to parent
- [ ] `Enter` inserts the selected file's relative path into the prompt
- [ ] Panel header shows current path
- [ ] No external dependencies beyond stdlib (`os`, `path/filepath`)

## Risks

- Large project roots with many files could be slow to scan
- Mitigation: scan once on panel entry, cache until directory change

## Open Questions

- Should hidden files (. files) be shown by default?
- Should there be a file type filter (e.g., show only `.go`, `.ts` files)?
- Should the file browser be git-aware (show changed files first)?
