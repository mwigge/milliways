# Tasks — milliways-tui-vim

Estimated: 1 sprint | Priority: High

---

## VT-1: VimMode state [1 SP]

- [ ] VT-1.1 Add `VimMode` type (`VimInsert=0`, `VimNormal=1`) and `vimMode VimMode` field to `Model` in `internal/tui/state.go`; default zero value `VimInsert` is correct
- [ ] VT-1.2 Initialize `m.vimMode = VimInsert` in `NewModel()` in `internal/tui/app.go`
- [ ] VT-1.3 Unit test: `VimInsert` and `VimNormal` have correct iota values; default model has `VimInsert`

---

## VT-2: Esc / i key handling [1 SP]

- [ ] VT-2.1 In `handleKey()` in `internal/tui/app.go`:
  - `tea.KeyEsc` in INSERT mode → `m.vimMode = VimNormal; m.overlayActive = true; return nil`
  - `tea.KeyEsc` in NORMAL mode → `m.vimMode = VimInsert; m.overlayActive = false; m.input.Focus(); return nil`
- [ ] VT-2.2 `'i'` rune in NORMAL mode (non-alt, non-overlay) → `m.vimMode = VimInsert; m.overlayActive = false; m.input.Focus(); return nil`
- [ ] VT-2.3 Remove `OverlayPanel` from `handleKey` `Ctrl+O` branch (or keep as deprecated alias that also sets `vimMode=VimNormal`)
- [ ] VT-2.4 Update `Update()` case `tea.KeyMsg`: rename `inPanelMode` to `inVimNormal`, use `m.vimMode == VimNormal` instead of overlay check
- [ ] VT-2.5 Unit test: Esc in insert mode → vimMode becomes VimNormal; Esc in normal mode → vimMode becomes VimInsert; `i` in normal mode → vimMode becomes VimInsert

---

## VT-3: h/j/k/l panel cycling in normal mode [1 SP]

- [ ] VT-3.1 In `handleKey()`, `tea.KeyRunes` case: when `m.vimMode == VimNormal` and `!msg.Alt` and single rune:
  - `'h'` or `'k'` → `m.rewindSidePanel(); return nil`
  - `'l'` or `'j'` → `m.advanceSidePanel(); return nil`
- [ ] VT-3.2 Remove the old `(!m.overlayActive || ...)` condition from the existing `h`/`l` handling (replaced by vim mode guard)
- [ ] VT-3.3 Unit test: in VimNormal mode, `h` → sidePanelIdx decrements; `l` → sidePanelIdx increments; `j`/`k` same; in VimInsert mode, `h`/`l` → no panel change (input update runs)

---

## VT-4: Update() skipInputUpdate uses vimMode [1 SP]

- [ ] VT-4.1 In `Update()` `tea.KeyMsg` case: `skipInputUpdate = m.vimMode == VimNormal && isSidePanelKey(...)`
- [ ] VT-4.2 Remove `inPanelMode := ...` and the old `(!m.overlayActive || ...)` expression
- [ ] VT-4.3 In the input update section: `if skipInputUpdate { inputCmd = nil }` takes priority; `else if m.overlayActive { ... overlayInput.Update ... } else { ... m.input.Update ... }` — logic unchanged but condition simplified
- [ ] VT-4.4 Verify: existing panel navigation tests (`TestHandleKey_CyclesSidePanelsForward`, `TestHandleKey_CyclesSidePanelsBackward`) still pass

---

## VT-5: Normal mode indicator in view [1 SP]

- [ ] VT-5.1 In `renderInputBar()` in `internal/tui/view.go`:
  - Add case `m.vimMode == VimNormal`: show `[N] h/l switch panels · ↑↓ navigate · i or Esc to type` in green
  - Move `OverlayPanel` case to a "deprecated" note (or remove if VT-2.3 keeps it as alias)
- [ ] VT-5.2 Unit test: insert mode → input bar shows textinput.View; normal mode → input bar contains `[N]`
- [ ] VT-5.3 Integration: run `milliways` interactively, press Esc, verify `[N]` indicator appears and `h`/`l` cycle panels without typing

---

## VT-6: Overlay dismiss restores insert mode [1 SP]

- [ ] VT-6.1 After each overlay dismissal in `handleKey()` (palette selection at line ~725, search selection at ~740, feedback `g`/`b`/`s` at ~855, summary `q` at ~874), add `m.vimMode = VimInsert; m.input.Focus()` before clearing overlay flags
- [ ] VT-6.2 Verify palette/search/feedback/summary dismissals all restore insert mode
- [ ] VT-6.3 Unit test: open palette (`/`), select command, close → vimMode is VimInsert

---

## VT-7: Ctrl+U / Ctrl+A / Ctrl+E line editing [1 SP]

- [ ] VT-7.1 In `handleKey()` in `internal/tui/app.go`:
  - `"ctrl+u"` (when `!m.overlayActive`) → `m.input.SetValue(""); return nil`
  - `"ctrl+a"` (when `!m.overlayActive`) → `m.input.SetCursor(0); return nil`
  - `"ctrl+e"` (when `!m.overlayActive`) → `m.input.SetCursor(len(m.input.Value())); return nil`
- [ ] VT-7.2 Guard: all three also check `m.vimMode == VimInsert` (line editing only makes sense in insert mode)
- [ ] VT-7.3 Create `internal/tui/lineedit_test.go`: table-driven tests for each key with text present, empty text, cursor positions
- [ ] VT-7.4 Verify: pressing `Ctrl+U` with "hello world" clears to ""; `Ctrl+A` moves cursor to 0; `Ctrl+E` moves cursor to 11

---

## VT-8: Mouse selection state [1 SP]

- [ ] VT-8.1 Create `internal/tui/mouse.go`:
  - `mouseState` struct with `selecting bool`, `selStartRow`, `selStartCol`, `selEndRow`, `selEndCol`, `lastMouseRow`, `lastMouseCol`
  - `handleMouse(msg tea.MouseMsg) tea.Cmd` — handles `MouseLeft` (down/up), `MouseMotion`
  - `extractTextSelection(r1, c1, r2, c2 int) string` — extracts plain text from `m.renderedLines`
- [ ] VT-8.2 Add `renderedLines []string` field to `Model` in `internal/tui/app.go`
- [ ] VT-8.3 Add `tea.MouseMsg` case in `Update()`: `cmds = append(cmds, m.handleMouse(msg)...)`
- [ ] VT-8.4 Create `internal/tui/mouse_test.go`: unit tests for selection state machine

---

## VT-9: Clipboard write on mouse up [1 SP]

- [ ] VT-9.1 Add `github.com/atotto/clipboard` to `go.mod` (`go get github.com/atotto/clipboard`)
- [ ] VT-9.2 In `handleMouse()`: on `MouseLeft.Up` with non-empty selection → `clipboard.WriteAll(text)`
- [ ] VT-9.3 Add `renderedLines` update to `Update()`: after processing `lineMsg` and `blockEventMsg`, call `m.renderedLines = buildRenderedLines(...)`
- [ ] VT-9.4 `buildRenderedLines(blocks []Block, outputLines []string) []string` — extracts plain text from block events (EventText, EventCodeBlock) and outputLines, strips markdown/glamour rendering, returns flat []string
- [ ] VT-9.5 Mouse test: simulate left-down at (r1,c1), drag to (r2,c2), left-up → verify clipboard write was called with expected text
- [ ] VT-9.6 Integration: select text in milliways output with mouse → paste elsewhere → text appears

---

## VT-10: Enable mouse in program [1 SP]

- [ ] VT-10.1 In `cmd/milliways/main.go`: add `tea.WithMouseAllMotion()` to `tea.NewProgram` options
- [ ] VT-10.2 Verify: mouse scroll wheel works in output viewport; mouse click to position cursor in input (if applicable)
- [ ] VT-10.3 Document mouse shortcuts in README: "click and drag to select · release to copy"

---

## VT-11: Backward compat pass + cleanup [1 SP]

- [ ] VT-11.1 Remove `OverlayPanel` from `handleKey` `Ctrl+O` branch if kept as alias (or keep it — see VT-2.3 note); clean up `if m.overlayActive && m.overlayMode == OverlayPanel` scattered checks
- [ ] VT-11.2 `go build ./...` → must pass with zero errors
- [ ] VT-11.3 `go test ./...` → all tests green
- [ ] VT-11.4 `go vet ./...` → zero warnings
- [ ] VT-11.5 Run smoke scenarios (`scripts/smoke.sh`) → all pass
- [ ] VT-11.6 Update README: new shortcuts table row for vi mode; update panel navigation row to mention `[N] mode`
