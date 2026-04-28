# Tasks — milliways-tui-file-browser

Estimated: 1 sprint | Priority: Medium

---

## FB-1: SidePanelFile enum + Model fields [1 SP]

- [ ] FB-1.1 Add `SidePanelFile` to `SidePanelMode` enum in `internal/tui/state.go` (after `SidePanelCompare`, before `sidePanelCount`)
- [ ] FB-1.2 Add `fileBrowserRoot string`, `fileBrowserCursor int`, `fileBrowserPath []string`, `fileBrowserCache map[string][]*fileNode` fields to `Model` struct in `internal/tui/app.go`
- [ ] FB-1.3 Initialize file browser fields in `NewModel()`: `fileBrowserRoot: "."`, `fileBrowserCursor: 0`, `fileBrowserPath: []string{}`, `fileBrowserCache: map[string][]*fileNode{}`
- [ ] FB-1.4 Wire `fileBrowserRoot` to `m.projectState.Root` (or "." if empty) when `m.projectState` is populated — add in `Update()` or in `RunWithOpts` after project resolution

---

## FB-2: fileNode type + scanning [1 SP]

- [ ] FB-2.1 Create `internal/tui/file_browser.go`:
  - `fileNode` struct: `Name`, `Path`, `IsDir`, `Items`, `hidden` fields
  - `scanDir(relPath string) []*fileNode` — reads directory, filters hidden files, sorts dirs-first
  - `buildFlatView(m *Model) []*fileNode` — builds navigable flat list including `..` parent entry
  - `getFileBrowserChildren(m *Model, relPath string) []*fileNode` — cache-aware wrapper
- [ ] FB-2.2 Create `internal/tui/file_browser_test.go`:
  - Test `scanDir` on a temp directory with files and subdirs
  - Test `buildFlatView` navigation stack behavior
  - Test hidden file filtering
  - Test `..` parent entry appears when not at root

---

## FB-3: Arrow key handling in file browser [1 SP]

- [ ] FB-3.1 In `handleKey()` in `internal/tui/app.go`: add `tea.KeyRight` / `tea.KeyLeft` cases for `m.sidePanelIdx == int(SidePanelFile) && !m.overlayActive`:
  - `KeyRight` on a directory → enter directory: append `node.Name` to `m.fileBrowserPath`, reset cursor to 0, invalidate cache
  - `KeyLeft` when `len(m.fileBrowserPath) > 0` → go up one level: pop last path segment, reset cursor to 0, invalidate cache
- [ ] FB-3.2 In `isSidePanelKey()` in `internal/tui/openspec_panel.go`: add `SidePanelFile` to the list — `↑`/`↓` navigate via existing cursor logic
- [ ] FB-3.3 On entering `SidePanelFile` via `advanceSidePanel`/`rewindSidePanel`: reset `fileBrowserCursor = 0` and invalidate cache
- [ ] FB-3.4 Unit test: simulate ↑/↓ navigation, → enter dir, ← go up

---

## FB-4: Enter to insert path [1 SP]

- [ ] FB-4.1 In `handleKey()` `"enter"` case in `internal/tui/app.go`: add check `if m.sidePanelIdx == int(SidePanelFile) && !m.overlayActive`:
  - Get selected node from `buildFlatView()`
  - If file (not dir): append `node.Path` to `m.input.Value()` (with space prefix if input not empty)
  - Return to insert mode: `m.vimMode = VimInsert; m.overlayActive = false; m.input.Focus()` (or reuse `setInsertMode()`)
- [ ] FB-4.2 Guard: if cursor is on `..` parent or on a directory, Enter does nothing (or could expand dirs — optional)

---

## FB-5: renderFileBrowserPanel [1 SP]

- [ ] FB-5.1 Create `renderFileBrowserPanel(width, height int, m *Model) string` in `internal/tui/view.go`:
  - Show breadcrumb path at top (e.g. `📁 internal/tui`)
  - Show `▶` cursor on selected item
  - Show `📂` for directories, `📄` for files, `↩` for `..`
  - Apply `width` and `height` constraints
- [ ] FB-5.2 Add `SidePanelFile` case to `renderActiveSidePanel()` dispatch in `internal/tui/view.go`
- [ ] FB-5.3 View test: `TestRenderFileBrowserPanel` — renders with files/dirs, cursor position, empty dir, breadcrumb

---

## FB-6: Integration + cleanup [1 SP]

- [ ] FB-6.1 `go build ./...` → zero errors
- [ ] FB-6.2 `go test ./...` → all tests green
- [ ] FB-6.3 `go vet ./...` → zero warnings
- [ ] FB-6.4 `go test -race ./internal/tui/...` → passes
- [ ] FB-6.5 Smoke scenarios: run `scripts/smoke.sh` → all pass
- [ ] FB-6.6 Update README: add file browser panel to panels list; add `→`/`←` to navigate dirs, `Enter` to insert path
- [ ] FB-6.7 Create feature branch, commit, push, PR
