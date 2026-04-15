# Tasks — milliways-tui-presence

## TP-1: Dispatch state machine [1 SP]

- [ ] TP-1.1 Define `DispatchState` type (iota) with 9 constants in `internal/tui/app.go`: `StateIdle`, `StateRouting`, `StateRouted`, `StateStreaming`, `StateDone`, `StateFailed`, `StateCancelled`, `StateAwaiting`, `StateConfirming`
- [ ] TP-1.2 Replace `dispatching bool` on `Model` with `dispatchState DispatchState`; update all guards (`m.dispatching` → `m.dispatchState != StateIdle`)
- [ ] TP-1.3 Add `stateIcon(s DispatchState) string` and `stateLabel(s DispatchState) string` pure functions mapping each state to its icon and label (see design D1)
- [ ] TP-1.4 Update `renderProcessMap()` to use `stateIcon`/`stateLabel` and apply correct lipgloss colour per state
- [ ] TP-1.5 Table-driven unit tests with named subtests: verify stateIcon and stateLabel for all 9 states; verify dispatchState transitions on KeyMsg enter/ctrl+c

## TP-2: DispatchOptions + routedMsg [1 SP]

- [ ] TP-2.1 Define `DispatchOptions` struct in `internal/tui/app.go` with `OnRouted func(sommelier.Decision)` field; zero value must be valid (nil = no-op)
- [ ] TP-2.2 Update `DispatchFunc` type signature to accept `opts DispatchOptions` as fourth parameter (replaces any ad-hoc onRouted parameter)
- [ ] TP-2.3 Add `routedMsg` struct: `{ kitchen string; decision sommelier.Decision }`
- [ ] TP-2.4 Store `*tea.Program` on `Model` (set via a `WithProgram(*tea.Program)` option or passed at `Run` time); `startDispatch()` wires `opts.OnRouted = func(d) { m.prog.Send(routedMsg{...}) }` — `tea.Program.Send` is goroutine-safe
- [ ] TP-2.5 In `startDispatch()`, prepend `"▶ " + prompt` and a separator line to `m.outputLines` *before* calling `m.input.SetValue("")`
- [ ] TP-2.6 Handle `routedMsg` in `Update()`: set `m.dispatchState = StateRouted`, update `m.processMap.kitchen`
- [ ] TP-2.7 Update `cmd/milliways/main.go` dispatch wiring to pass `DispatchOptions{}` (zero value) for headless paths; TUI path passes options with `OnRouted` set
- [ ] TP-2.8 Unit tests: echo line is first outputLine on startDispatch; routedMsg sets correct kitchen and state; DispatchOptions zero value is safe to call

## TP-3: Process map state display [1 SP]

- [ ] TP-3.1 `renderProcessMap()` shows kitchen badge only when `dispatchState >= StateRouted`; shows "routing..." in muted style during `StateRouting`
- [ ] TP-3.2 Status line: `stateIcon(m.dispatchState) + " " + stateLabel(m.dispatchState) + "  " + elapsed`
- [ ] TP-3.3 Handle `lineMsg` in `Update()`: if `dispatchState == StateRouted`, transition to `StateStreaming`
- [ ] TP-3.4 Render tests (string-match on View output): Routing, Routed, Streaming, Done, Failed states produce expected icon and label

## TP-4: Dialogue protocol constants + Task fields [1 SP]

- [ ] TP-4.1 Create `internal/kitchen/dialogue.go`:
  - Exported constants `QuestionPrefix = "?MW> "` and `ConfirmPrefix = "!MW> "`
  - `IsQuestion(line string) bool`, `IsConfirm(line string) bool`, `StripPrefix(line string) string` pure helpers
- [ ] TP-4.2 Add to `Task` in `internal/kitchen/kitchen.go`:
  ```go
  OnQuestion func(question string)  // nil = headless
  OnConfirm  func(question string)  // nil = headless
  AnswerCh   <-chan string           // nil = headless; receive-only enforces TUI owns the send end
  ```
- [ ] TP-4.3 Unit tests for dialogue.go: IsQuestion/IsConfirm/StripPrefix cover prefix match, no-match, empty string, prefix-only (no body)

## TP-5: GenericKitchen stdin pipe + dialogue interception [2 SP]

- [ ] TP-5.1 In `GenericKitchen.Exec()`, always create a stdin pipe via `cmd.StdinPipe()`; close it with `defer stdinPipe.Close()` — pipe held open for duration of process
- [ ] TP-5.2 In the stdout scanner loop, call `dialogue.IsQuestion` and `dialogue.IsConfirm` before `task.OnLine`:
  - If `?MW>` and `OnQuestion != nil`: call `OnQuestion(dialogue.StripPrefix(line))`, then `select { case ans := <-task.AnswerCh: fmt.Fprintln(stdinPipe, ans); case <-ctx.Done(): return ctx.Err() }`
  - If `?MW>` and `OnQuestion == nil` (headless): write `""` to stdin, continue
  - Same pattern for `!MW>` / `OnConfirm`
  - All other lines: call `task.OnLine(line)` as before
- [ ] TP-5.3 Add `goleak.VerifyTestMain` to `TestMain` in `internal/kitchen/` test package to catch goroutine leaks
- [ ] TP-5.4 Unit tests — fake kitchen via an `io.Pipe`: (a) emits `?MW> Which runner?`, verifies `OnQuestion` fires, answer written to stdin; (b) emits `!MW> Delete? `, verifies `OnConfirm` fires; (c) ctx cancel while awaiting answer — goroutine exits, no leak; (d) nil OnQuestion/AnswerCh (headless) — auto-answers `""`, no block

## TP-6: TUI overlay + Awaiting/Confirming states [2 SP]

- [ ] TP-6.1 Add to `Model`: `overlayInput textinput.Model`, `overlayActive bool`, `answerCh chan string` (bidirectional; sent to Task as `<-chan string`)
- [ ] TP-6.2 Add `questionMsg { text string }` and `confirmMsg { text string }` message types
- [ ] TP-6.3 `startDispatch()` creates `m.answerCh = make(chan string, 1)` and assigns `task.AnswerCh = m.answerCh`
- [ ] TP-6.4 Handle `questionMsg` in `Update()`: `m.dispatchState = StateAwaiting`, set `m.overlayActive = true`, configure `m.overlayInput` with question as placeholder, `m.overlayInput.Focus()`
- [ ] TP-6.5 Handle `confirmMsg` in `Update()`: `m.dispatchState = StateConfirming`, append `"[confirm] " + text + " [y/N]"` to outputLines in highlight style
- [ ] TP-6.6 Key routing when `m.overlayActive`: `enter` → `m.answerCh <- m.overlayInput.Value()`, clear overlay, set `m.dispatchState = StateStreaming`; all other keys → route to `m.overlayInput.Update(msg)` only
- [ ] TP-6.7 Key routing when `m.dispatchState == StateConfirming`: `y` → send `"y"`, `n` / `enter` → send `"n"`, both → `m.dispatchState = StateStreaming`
- [ ] TP-6.8 `View()`: when `m.overlayActive`, render `m.overlayInput` styled with amber/yellow border above the main input bar
- [ ] TP-6.9 Unit tests (table-driven, named subtests): questionMsg activates overlay; enter submits to answerCh; confirmMsg appends prompt line; y resolves "y", enter resolves "n"; overlay hidden after resolution

## TP-7: Ctrl+I context injection [0.5 SP]

- [ ] TP-7.1 Handle `ctrl+i` in `Update()` when `m.dispatchState == StateStreaming`: set `m.overlayActive = true`, configure `m.overlayInput` with placeholder `"+ context"`, focus
- [ ] TP-7.2 On overlay submit during context injection: send value to `m.answerCh`, append `"[+context] " + value` to outputLines in mutedStyle, clear overlay, remain in StateStreaming
- [ ] TP-7.3 Unit test: ctrl+i opens overlay; submit appends muted context line and writes to channel

## TP-8: Headless --verbose routing line [0.5 SP]

- [ ] TP-8.1 In headless dispatch path in `cmd/milliways/main.go`, pass `DispatchOptions{OnRouted: func(d sommelier.Decision) { if verbose { fmt.Fprintf(os.Stderr, "[routed] %s\n", d.Kitchen) } }}`
- [ ] TP-8.2 Unit test: with verbose=true, OnRouted prints `[routed] <kitchen>` to stderr; with verbose=false, nothing printed

## TP-9: Integration + cleanup [0.5 SP]

- [ ] TP-9.1 All existing unit tests pass after DispatchFunc signature change (DispatchOptions added)
- [ ] TP-9.2 `go test -race ./internal/tui/... ./internal/kitchen/...` passes — race detector validates goroutine-safe p.Send usage and AnswerCh ownership
- [ ] TP-9.3 `go vet ./...` passes — no shadow variables, no unused fields
- [ ] TP-9.4 Manual smoke test: `milliways --tui` → type prompt → see echo line + "routing..." → see kitchen badge appear → see "streaming" → see output → see "done"
- [ ] TP-9.5 Manual dialogue test: run a test script that writes `?MW> Which test runner?` to stdout, verify TUI shows overlay, answer flows back to stdin

- [ ] 🍽️ **Service check** — Palate cleanser: TUI feels present. Prompt echoes. Kitchen named before output starts. Questions answered inline. `milliways --tui` is a conversation, not a void.
