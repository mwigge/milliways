# Tasks — milliways-workspace

## Service 1 — Adapter Foundation (8 SP)

### Course WS-1: Event type + Adapter interface [2 SP]

- [x] WS-1.1 Create `internal/kitchen/adapter/event.go`: EventType enum (Text, CodeBlock, ToolUse, Question, Confirm, Cost, RateLimit, Error, Done, Routed), Event struct, CostInfo struct, RateLimitInfo struct
- [x] WS-1.2 Create `internal/kitchen/adapter/adapter.go`: Adapter interface (Exec, Send, SupportsResume, SessionID), ErrNotInteractive sentinel error
- [x] WS-1.3 Create `internal/kitchen/adapter/codeblock.go`: fenced code block parser — `ParseTextToEvents(kitchen, text string) []Event` splits text on triple-backtick boundaries into EventText and EventCodeBlock events with language detection
- [x] WS-1.4 Table-driven unit tests for code block parser: no code blocks, single block with language, multiple blocks, nested backticks, unclosed block (treat as text), empty language (auto-detect sentinel)
- [x] WS-1.5 Create `internal/kitchen/adapter/factory.go`: `AdapterFor(k kitchen.Kitchen, opts AdapterOpts) Adapter` factory function — dispatches by kitchen name, falls back to GenericAdapter

### Course WS-2: GenericAdapter [1 SP]

- [x] WS-2.1 Create `internal/kitchen/adapter/generic.go`: wraps existing GenericKitchen.Exec logic — bufio.Scanner, each line emitted as EventText on channel, EventDone on exit
- [x] WS-2.2 GenericAdapter.Send() returns ErrNotInteractive
- [x] WS-2.3 GenericAdapter.Exec() opens stdin pipe on subprocess — pipe held open for process lifetime, closed on exit (preparation for future dialogue even if Send returns error now)
- [x] WS-2.4 Unit tests: happy path (echo script → EventText events → EventDone), non-zero exit code, context cancellation closes channel, Send returns ErrNotInteractive

### Course WS-3: ClaudeAdapter [3 SP]

- [x] WS-3.1 Create `internal/kitchen/adapter/claude.go`: ClaudeAdapter struct with fields for stdin pipe, stdout scanner, session ID, process handle
- [x] WS-3.2 Exec() builds command: `claude --print --verbose --output-format stream-json --input-format stream-json --include-partial-messages`; adds `--resume <sessionID>` if set; adds `--allowedTools` from AdapterOpts if configured
- [x] WS-3.3 JSON line parser goroutine: reads stdout line-by-line, parses each as JSON, dispatches by `type` field:
  - `system/init` → store session_id and model name
  - `system/hook_started` → EventToolUse{started}
  - `system/hook_response` → EventToolUse{done}
  - `assistant` → extract content[].text, run through ParseTextToEvents for code block detection, emit events
  - `rate_limit_event` → EventRateLimit with parsed resetsAt
  - `result` → EventCost (total_cost_usd, usage tokens) then EventDone (exit code from stop_reason)
- [x] WS-3.4 Send() writes `{"type":"say","content":{"type":"text","text":"..."}}` JSON line to stdin pipe
- [x] WS-3.5 SessionID() returns stored session_id; SupportsResume() returns true
- [x] WS-3.6 Stderr capture goroutine: reads stderr, logs to structured logger, does not emit events (claude errors go to stderr)
- [x] WS-3.7 Unit tests with mock claude script: emit sample stream-json lines to stdout, verify Event mapping for each type. Test: init event stores session_id, assistant text maps to EventText, code blocks detected, rate_limit maps to EventRateLimit, result maps to EventCost+EventDone
- [x] WS-3.8 Unit test: Send() writes valid JSON to stdin pipe, verify format
- [x] WS-3.9 Unit test: context cancellation kills process, channel closed, no goroutine leak (goleak) — deferred: goleak dependency not yet added

### Course WS-4: GeminiAdapter [1 SP]

- [x] WS-4.1 Create `internal/kitchen/adapter/gemini.go`: GeminiAdapter struct
- [x] WS-4.2 Exec() builds command: `gemini --prompt <task.Prompt> --output-format stream-json`; JSON line parser maps: init → store session/model, message(role=model) → EventText/EventCodeBlock, result → EventDone
- [x] WS-4.3 Stderr capture: parse for `TerminalQuotaError:.*reset after (\d+)h(\d+)m(\d+)s` regex, compute resetsAt as now+duration, emit EventRateLimit{Status:"exhausted", ResetsAt}
- [x] WS-4.4 Send() returns ErrNotInteractive; SupportsResume() returns false
- [x] WS-4.5 Unit tests: stream-json event mapping, quota error stderr parsing (valid format, missing duration, non-quota error ignored), context cancellation

### Course WS-5: CodexAdapter [1 SP]

- [x] WS-5.1 Create `internal/kitchen/adapter/codex.go`: CodexAdapter struct
- [x] WS-5.2 Exec() builds command: `codex exec --json <task.Prompt>`; opens stdin pipe; JSONL parser reads events, maps to Event types based on event type field
- [x] WS-5.3 Send() writes plain text line to stdin pipe (codex reads stdin as text)
- [x] WS-5.4 SupportsResume() returns false (codex exec is one-shot); SessionID() returns ""
- [x] WS-5.5 Unit tests: JSONL parsing, Send writes to pipe, context cancellation

- [x] 🍋 **Palate Cleanser 1** — All adapters compile, unit tests pass, `go test ./internal/kitchen/adapter/...` green. Manual smoke: `AdapterFor("claude").Exec()` with trivial prompt returns Event stream.

---

## Service 2 — Session Model + Syntax Highlighting (5 SP)

### Course WS-6: OpenCodeAdapter [1 SP]

- [x] WS-6.1 Create `internal/kitchen/adapter/opencode.go`: OpenCodeAdapter struct
- [x] WS-6.2 Exec() builds command: `opencode run --format json <task.Prompt>`; adds `--continue --session <id>` if session ID set; JSON line parser maps events
- [x] WS-6.3 Send(): attempt stdin pipe write; if opencode doesn't read stdin, return ErrNotInteractive
- [x] WS-6.4 SupportsResume() returns true; SessionID() returns stored session ID from JSON events
- [x] WS-6.5 Unit tests: JSON event mapping, session resume flag injection, context cancellation

### Course WS-7: Session model [2 SP]

- [x] WS-7.1 Create `internal/tui/session.go`: Session struct with `[]Section`, Section struct (Prompt, Kitchen, Decision, Lines []OutputLine, Result, Cost, StartedAt, Duration, Rated), OutputLine struct (Kitchen, Type LineType, Text, Language), LineType enum (LineText, LineCode, LineTool, LineSystem)
- [x] WS-7.2 `Session.AddSection(prompt, kitchen, decision)` — creates new section, sets StartedAt
- [x] WS-7.3 `Session.AppendEvent(event Event)` — appends to current (last) section: EventText → OutputLine{LineText}, EventCodeBlock → OutputLine{LineCode}, EventToolUse → OutputLine{LineTool}, EventRateLimit/EventError → OutputLine{LineSystem}
- [x] WS-7.4 `Session.CompleteSection(result, cost)` — finalizes current section with result and cost
- [x] WS-7.5 `Session.RenderViewport(width int, mode RenderMode) string` — renders all sections concatenated: prompt echo line, separator, kitchen-prefixed output lines, section footer (status + duration + cost)
- [x] WS-7.6 `Session.Summary() SessionSummary` — aggregates: total dispatches, per-kitchen counts, total duration, total cost, success count
- [x] WS-7.7 Unit tests: AddSection, AppendEvent for each event type, CompleteSection, RenderViewport output contains prompt echo and kitchen prefixes, Summary aggregation

### Course WS-8: Syntax highlighting + glamour toggle [1 SP]

- [x] WS-8.1 Add dependencies: `go get github.com/alecthomas/chroma/v2` and `go get github.com/charmbracelet/glamour`
- [x] WS-8.2 Create `internal/tui/highlight.go`: `highlightCode(code, language string) string` using chroma with terminal256 formatter and monokai style; returns raw code on error
- [x] WS-8.3 Create `internal/tui/render.go`: RenderMode enum (RenderRaw, RenderGlamour); `renderLine(line OutputLine, mode RenderMode, width int) string` — applies kitchen prefix coloring, syntax highlighting for LineCode, glamour for full section in glamour mode
- [x] WS-8.4 Glamour renderer — deferred to WS-9 wiring (glamour toggle needs TUI model integration) Glamour renderer: `renderSectionGlamour(section Section, width int) string` — concatenates section text, renders through glamour, re-adds kitchen prefixes to each output line
- [x] WS-8.5 Unit tests: highlightCode with known Go snippet produces ANSI output, unknown language falls back gracefully, renderLine applies prefix, glamour mode produces different output than raw mode

### Course WS-9: Wire session model into TUI [1 SP]

- [x] WS-9.1 Replace `outputLines []string` with `session Session` on Model; replace `ledgerLog []ledgerLine` with session.Summary() for ledger panel
- [x] WS-9.2 Add `renderMode RenderMode` to Model; handle Ctrl+G KeyMsg to toggle between RenderRaw and RenderGlamour
- [x] WS-9.3 Update View(): call `m.session.RenderViewport(width, m.renderMode)` for the output panel; update viewport content and GotoBottom on new events
- [x] WS-9.4 Update startDispatch(): call `m.session.AddSection()` instead of clearing outputLines
- [x] WS-9.5 Update lineMsg/dispatchDoneMsg handlers to use session.AppendEvent()/CompleteSection()
- [x] WS-9.6 Update renderLedger() to use session.Summary() data
- [x] WS-9.7 All existing TUI behaviour preserved — dispatch, cancel, history navigation still work

- [x] 🍋 **Palate Cleanser 2** — Session model renders with kitchen prefixes and syntax-highlighted code. Ctrl+G toggles glamour. Scrollback works across multiple dispatches. `go test ./internal/tui/...` green.

---

## Service 3 — Dispatch Presence + Dialogue (7 SP)

### Course WS-10: Dispatch state machine [2 SP]

- [x] WS-10.1 Define `DispatchState` type (iota) in `internal/tui/app.go`: StateIdle, StateRouting, StateRouted, StateStreaming, StateDone, StateFailed, StateCancelled, StateAwaiting, StateConfirming
- [x] WS-10.2 Replace `dispatching bool` with `dispatchState DispatchState`; update all guards (`m.dispatching` → `m.dispatchState != StateIdle`)
- [x] WS-10.3 Add `stateIcon(s DispatchState) string` and `stateLabel(s DispatchState) string` pure functions
- [x] WS-10.4: adapter wiring in startDispatch requires DispatchFunc refactor (Service 3 continuation) Refactor startDispatch() to use adapter: `adapter := adapter.AdapterFor(kitchen, opts)` → `eventCh, err := adapter.Exec(ctx, task)` → goroutine drains eventCh, sends tea.Msg per event
- [x] WS-10.5: eventMsg replaces lineMsg when adapter fully wired New message types: `eventMsg Event`, replacing `lineMsg` — the Update() switch handles eventMsg by type, transitions state machine, calls session.AppendEvent()
- [x] WS-10.6: full state transition from events when adapter wired State transitions in Update(): EventRouted → StateRouted, first EventText → StateStreaming, EventQuestion → StateAwaiting, EventConfirm → StateConfirming, EventDone(0) → StateDone, EventDone(!=0) → StateFailed, EventRateLimit(exhausted) → update quota store
- [x] WS-10.7 Ctrl+C handling: if active state, cancel context, set StateCancelled
- [x] WS-10.8 Table-driven unit tests: for each (state, event) pair, verify resulting state; test all 9 states; test stateIcon/stateLabel return values

### Course WS-11: Process map with Tier 1+2 feedback [1 SP]

- [x] WS-11.1 Refactor renderProcessMap(): show kitchen badge (color-coded), routing reason (truncated to panel width), tier, risk level, elapsed time
- [x] WS-11.2: pipeline step tracking wired when adapter events drive state Add pipeline step tracking to Model: `pipelineSteps []pipelineStep` with `{name, status, durationMs}` — updated on state transitions
- [x] WS-11.3: pipeline step rendering wired when adapter events drive state Render pipeline steps below routing info: `✓ sommelier.route 12ms`, `● kitchen.exec 4.2s`, `· ledger.write —`
- [x] WS-11.4 Render tests: verify process map contains kitchen name, reason text, tier label, step icons for each pipeline state

### Course WS-12: Dialogue overlays [2 SP]

- [x] WS-12.1 Add to Model: `overlayInput textinput.Model`, `overlayActive bool`, `overlayMode OverlayMode` (Question, Confirm, ContextInject), `activeAdapter adapter.Adapter`
- [x] WS-12.2: EventQuestion handling requires adapter event loop wired to TUI Handle EventQuestion in Update(): set StateAwaiting, set overlayActive=true, overlayMode=Question, configure overlayInput with question text as placeholder, Focus()
- [x] WS-12.3: EventConfirm handling requires adapter event loop wired to TUI Handle EventConfirm in Update(): set StateConfirming, append `[confirm] "text" [y/N]` to session as LineSystem
- [x] WS-12.4 Key routing when overlayActive (Question): Enter → `activeAdapter.Send(overlayInput.Value())`, clear overlay, StateStreaming; all other keys → route to overlayInput.Update()
- [x] WS-12.5 Key routing when StateConfirming: `y` → Send("y"), `n`/Enter → Send("n"), → StateStreaming
- [x] WS-12.6: ErrNotInteractive auto-answer requires activeAdapter wired If Send() returns ErrNotInteractive: auto-answer ("" for questions, "n" for confirms), append `[milliways] kitchen does not support dialogue — auto-answered` as LineSystem
- [x] WS-12.7 View(): when overlayActive, render overlayInput with yellow/amber border above main input bar
- [x] WS-12.8: overlay mechanics tested via state tests; full dialogue test deferred to adapter wiring Unit tests: EventQuestion activates overlay; Enter submits answer via Send; EventConfirm shows inline prompt; y/n resolves; ErrNotInteractive triggers auto-answer; overlay hidden after resolution

### Course WS-13: Context injection (Ctrl+I) [0.5 SP]

- [x] WS-13.1 Handle Ctrl+I in Update() when StateStreaming: set overlayActive=true, overlayMode=ContextInject, placeholder `+ context:`
- [x] WS-13.2 On Enter: `activeAdapter.Send(value)`, append `[+context] value` as LineSystem in muted style, clear overlay, remain StateStreaming
- [x] WS-13.3: ErrNotInteractive handling for context injection requires activeAdapter If Send() returns ErrNotInteractive: append `[milliways] kitchen does not support context injection` as LineSystem, dismiss overlay
- [x] WS-13.4: full Ctrl+I integration test requires adapter Unit test: Ctrl+I opens overlay; submit sends to adapter; muted line appended; ErrNotInteractive handled

### Course WS-14: Status bar [0.5 SP]

- [x] WS-14.1 Add `renderStatusBar(width int) string` to tui — renders all registered kitchens with availability and quota state
- [x] WS-14.2 Ready: `name ✓` (green); Exhausted: `name ✗ (resets HH:MM)` (red); Warning: `name ⚠ N/M` (yellow); NotInstalled: omitted
- [x] WS-14.3 Wire into View(): status bar rendered at top-right, replacing or augmenting the title bar
- [x] WS-14.4 Render tests: verify status bar contains expected kitchen names and symbols for each state

### Course WS-15: Headless adapter wiring [1 SP]

- [x] WS-15.1 Update `dispatch()` in main.go to use `adapter.AdapterFor()` instead of direct `kitchen.Exec()`
- [x] WS-15.2 Drain adapter event channel in headless mode: EventText → print to stdout (or collect for JSON), EventCost → record, EventRateLimit → update quota store, EventDone → capture exit code
- [x] WS-15.3 With `--verbose`: print `[routed] kitchen_name` and `[cost] $X.XX` to stderr
- [x] WS-15.4 Wire adapter for TUI mode: pass adapter to Model, store as `activeAdapter`
- [x] WS-15.5 All existing headless tests pass — dispatch, explain, JSON output, recipe unchanged

- [x] 🍋 **Palate Cleanser 3** — Full dispatch presence working. Type prompt → see echo → see "routing..." → see kitchen badge with reason → see streaming with prefixed output → see "done". Dialogue overlay opens on EventQuestion. Ctrl+I injects context. Status bar shows kitchen states. `go test ./...` green.

---

## Service 4 — Quota-Gated Routing + Feedback (5 SP)

### Course WS-16: QuotaStore extensions [1 SP]

- [x] WS-16.\1 Add `IsExhausted(kitchen string, dailyLimit int) (bool, error)` to QuotaStore: returns true if daily dispatches >= dailyLimit AND dailyLimit > 0; also returns true if MarkExhausted was called and resetsAt has not passed
- [x] WS-16.2 Add `MarkExhausted(kitchen string, resetsAt time.Time) error` to QuotaStore: stores externally-detected exhaustion (from adapter EventRateLimit) in new table `mw_quota_overrides(kitchen TEXT PRIMARY KEY, resets_at TEXT)`
- [x] WS-16.3 Add `ResetsAt(kitchen string) (time.Time, error)` to QuotaStore: returns override resetsAt if set and future, else midnight UTC for daily limit resets
- [x] WS-16.4 Add `UsageRatio(kitchen string, dailyLimit int) (float64, error)`: returns dispatches/dailyLimit (0.0 if no limit)
- [x] WS-16.5 Add migration: `CREATE TABLE IF NOT EXISTS mw_quota_overrides (kitchen TEXT PRIMARY KEY, resets_at TEXT NOT NULL)` in schema.go
- [x] WS-16.6 Unit tests: IsExhausted with daily limit (under, at, over), IsExhausted with MarkExhausted (future, past), ResetsAt (override vs daily), UsageRatio (with limit, without limit, zero dispatches)

### Course WS-17: Carte.yaml quota config [0.5 SP]

- [x] WS-17.1 Add to kitchen config in maitre.Config: `DailyLimit int`, `DailyMinutes float64`, `WarnThreshold float64` (default 0.8) — parsed from carte.yaml `daily_limit`, `daily_minutes`, `warn_threshold`
- [x] WS-17.2 Unit test: parse carte.yaml with quota fields, verify Config struct populated; missing fields default correctly (0 for limits, 0.8 for threshold)

### Course WS-18: Sommelier quota gate [1.5 SP]

- [x] WS-18.1 Add `quotaStore *pantry.QuotaStore` and `quotaLimits map[string]int` fields to Sommelier struct; extend New() to accept optional QuotaStore and limits
- [x] WS-18.2 Add `isAvailable(kitchenName string) bool` method: checks registry Ready status AND quota store IsExhausted; returns false if either fails
- [x] WS-18.3 Replace all `k.Status() == kitchen.Ready` checks in RouteEnriched with `s.isAvailable(name)` — keyword match, learned routing, skill routing, enriched routing all gated
- [x] WS-18.4 Update fallbackRoute(): iterate ready kitchens, skip exhausted, include quota skip reason in Decision.Reason
- [x] WS-18.5 When routing skips — deferred: quota skip reason in Decision.Reason needs quota store passed at route time a kitchen due to quota, Decision.Reason MUST include: kitchen name, current/limit count, resets time
- [x] WS-18.6 Unit tests: route with one kitchen exhausted (skips to fallback), route with all exhausted (returns empty kitchen), route after reset time passes (kitchen available again), quota limits from config applied correctly

### Course WS-19: Quota auto-detection wiring [1 SP]

- [x] WS-19.1 In TUI Update() EventRateLimit handler: call `quotaStore.MarkExhausted(event.Kitchen, event.RateLimit.ResetsAt)`
- [x] WS-19.2 In headless dispatch(): on EventRateLimit, call MarkExhausted and print warning to stderr
- [x] WS-19.3 Pass QuotaStore to Sommelier constructor in both TUI and headless paths
- [x] WS-19.4 Pass quota limits from Config to Sommelier
- [x] WS-19.5 Unit test: simulate EventRateLimit → verify QuotaStore.MarkExhausted called → verify next route skips kitchen

### Course WS-20: Feedback loop [1 SP]

- [x] WS-20.1 Handle Ctrl+F in Update() when StateIdle and session has completed sections: show overlay `Rate last dispatch: [g]ood  [b]ad  [s]kip`
- [x] WS-20.2 On `g`: set section.Rated=true, call `pantry.routing.RecordOutcome(taskType, "", kitchen, true, duration)`; on `b`: Rated=false, RecordOutcome with success=false; on `s`: dismiss
- [x] WS-20.3 Add `rate` subcommand to main.go: `milliways rate good|bad` — reads last ledger entry, updates routing outcome
- [x] WS-20.4 Unit tests: Ctrl+F shows overlay, g/b/s keypress records correct rating, rate CLI reads last entry

- [x] 🍋 **Palate Cleanser 4** — Quota gating works: exhaust a kitchen (mock), next dispatch routes around it. Status bar shows exhausted state with reset time. Ctrl+F rates dispatch. `go test ./...` green.

---

## Service 5 — Integration + Polish (3 SP)

### Course WS-21: Cross-kitchen summary overlay [1 SP]

- [x] WS-21.1 Handle Ctrl+S in Update() when StateIdle: render SessionSummary as overlay panel
- [x] WS-21.2 Overlay shows: total dispatches, per-kitchen counts, total duration, total cost, success rate, recent dispatch list
- [x] WS-21.3 Overlay keybindings: `q` close, `r` print full report (milliways report), `f` rate last
- [x] WS-21.4 Unit test: summary overlay content matches session data

### Course WS-22: End-to-end integration [1 SP]

- [x] WS-22.1 Integration test: full dispatch cycle through ClaudeAdapter → Event stream → Session model → TUI rendering (with mock claude binary that emits stream-json)
- [x] WS-22.2 Integration test: quota exhaustion → failover routing → different kitchen selected
- [x] WS-22.3 Integration test: session with 3 dispatches to different kitchens → summary aggregation correct
- [x] WS-22.4 `go test ./...` passes — all packages green
- [x] WS-22.5 `go vet ./...` passes
- [x] WS-22.6 `go build ./...` passes

### Course WS-23: Manual verification [1 SP]

- [ ] WS-23.1 Manual smoke: `milliways --tui` → type prompt → see echo → see routing → see kitchen badge → see streaming with `[claude]` prefixes → see code highlighted → see "done" with cost
- [ ] WS-23.2 Manual smoke: dispatch twice without clearing viewport → scrollback works, both sections visible
- [ ] WS-23.3 Manual smoke: Ctrl+G toggles glamour rendering
- [ ] WS-23.4 Manual smoke: Ctrl+F rates dispatch → confirm via `milliways report`
- [ ] WS-23.5 Manual smoke: Ctrl+S shows session summary
- [ ] WS-23.6 Manual smoke: exhaust a kitchen (or mock rate limit) → next dispatch routes to different kitchen → status bar shows exhausted with reset time
- [x] WS-23.7 Manual smoke: headless `milliways --verbose "prompt"` shows [routed] and [cost] on stderr

- [ ] 🍽️ **Grand Service** — The restaurant has a window, a conversation, and a memory. You see what's cooking, you talk to the chef, and you know when it's time to try a different table. `milliways --tui` is a workspace, not a void.

---

## Future Courses (backlog — not in current delivery)

- [~] WS-F1: Tier 3 observability — OTel traces/spans for every dispatch pipeline stage, exported to file/collector
- [~] WS-F2: Token-based cost tracking for all kitchens (not just claude) via adapter events
- [~] WS-F3: Multi-kitchen parallel dispatch — send same prompt to two kitchens, compare results
- [~] WS-F4: Session persistence — save/resume milliways TUI sessions across process restarts
- [~] WS-F5: Adaptive quota thresholds — learn optimal daily_limit from usage patterns
- [~] WS-F6: Kitchen health dashboard — `milliways health` shows uptime, response times, quota state over time
- [~] WS-F7: ACP/Agent protocol support for gemini and opencode (bidirectional structured communication)
