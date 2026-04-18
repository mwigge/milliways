# Design — milliways-workspace

## D1: Event type and Adapter interface

All kitchen communication flows through a normalized event stream. The TUI never parses kitchen-specific protocols — adapters do that.

```go
// EventType enumerates the kinds of events a kitchen adapter can emit.
type EventType int

const (
    EventText       EventType = iota // Plain text line from kitchen
    EventCodeBlock                   // Fenced code block (language + content)
    EventToolUse                     // Kitchen invoked a tool (name + status)
    EventQuestion                    // Kitchen needs free-text answer
    EventConfirm                     // Kitchen needs y/N confirmation
    EventCost                        // Cost/usage data from kitchen
    EventRateLimit                   // Rate limit or quota signal
    EventError                       // Kitchen-side error
    EventDone                        // Kitchen finished (carries exit code)
    EventRouted                      // Sommelier decision made (internal)
)

// Event is a single normalized event from any kitchen adapter.
type Event struct {
    Type      EventType
    Kitchen   string          // source kitchen name
    Text      string          // for Text, Question, Confirm, Error
    Language  string          // for CodeBlock — e.g. "go", "python"
    Code      string          // for CodeBlock — the code content
    ToolName  string          // for ToolUse — e.g. "Edit", "Bash"
    ToolStatus string         // for ToolUse — "started", "done", "failed"
    Cost      *CostInfo       // for EventCost
    RateLimit *RateLimitInfo  // for EventRateLimit
    ExitCode  int             // for EventDone
    Decision  *sommelier.Decision // for EventRouted
}

type CostInfo struct {
    USD          float64
    InputTokens  int
    OutputTokens int
    CacheRead    int
    CacheWrite   int
    DurationMs   int
}

type RateLimitInfo struct {
    Status    string    // "allowed", "exhausted", "warning"
    ResetsAt  time.Time // when quota resets
    Kitchen   string    // which kitchen is affected
}
```

### Adapter interface

```go
// Adapter translates a kitchen's native protocol to the Event stream.
type Adapter interface {
    // Exec starts the kitchen process and returns an event channel.
    // The channel is closed when the kitchen process exits.
    // The caller MUST drain the channel to avoid goroutine leaks.
    Exec(ctx context.Context, task kitchen.Task) (<-chan Event, error)

    // Send writes a message to the kitchen's stdin (for dialogue).
    // Returns ErrNotInteractive if the adapter doesn't support it.
    Send(ctx context.Context, msg string) error

    // SupportsResume returns true if the kitchen supports session continuity.
    SupportsResume() bool

    // SessionID returns the current session ID for resume, or "" if none.
    SessionID() string
}
```

### Adapter selection

```go
// AdapterFor returns the appropriate adapter for a kitchen.
// Falls back to GenericAdapter for unknown kitchens.
func AdapterFor(k kitchen.Kitchen) Adapter {
    switch k.Name() {
    case "claude":
        return NewClaudeAdapter(k)
    case "gemini":
        return NewGeminiAdapter(k)
    case "codex":
        return NewCodexAdapter(k)
    case "opencode":
        return NewOpenCodeAdapter(k)
    default:
        return NewGenericAdapter(k)
    }
}
```

## D2: ClaudeAdapter — the reference implementation

Claude is the highest-tier kitchen and the first that must work with interactive dialogue. The adapter speaks Claude's native `stream-json` protocol.

### Invocation

```
claude --print --verbose \
    --output-format stream-json \
    --input-format stream-json \
    --include-partial-messages
```

With `--input-format stream-json`, messages are sent as:
```json
{"type":"say","content":{"type":"text","text":"the user's prompt"}}
```

### Event mapping

| Claude stream-json event | Milliways Event |
|--------------------------|-----------------|
| `{"type":"system","subtype":"init",...}` | (internal: store session_id, model name) |
| `{"type":"system","subtype":"hook_started",...}` | EventToolUse{ToolName:"hook:NAME", ToolStatus:"started"} |
| `{"type":"system","subtype":"hook_response",...}` | EventToolUse{ToolName:"hook:NAME", ToolStatus:"done"} |
| `{"type":"assistant","message":{"content":[{"type":"text","text":"..."}]}}` | EventText / EventCodeBlock (parsed from text) |
| `{"type":"assistant","message":{"content":[{"type":"tool_use",...}]}}` | EventToolUse{ToolName, ToolStatus:"started"} |
| `{"type":"rate_limit_event","rate_limit_info":{...}}` | EventRateLimit{Status, ResetsAt} |
| `{"type":"result","total_cost_usd":...,"usage":{...}}` | EventCost + EventDone |

### Code block detection

The adapter parses assistant text content for fenced code blocks:

```
Text before code block → EventText
```go                    → EventCodeBlock{Language:"go", Code:"..."}
code here
```                      → (end of code block)
Text after              → EventText
```

This is done at the adapter level so the TUI receives pre-classified events.

### Session resume

The adapter stores the `session_id` from the init event. On subsequent dispatches to the same claude kitchen within the same milliways session, the adapter adds `--resume <session_id>` to maintain conversation context across dispatches.

### Dialogue via stream-json stdin

When the TUI calls `adapter.Send(answer)`, the ClaudeAdapter writes:
```json
{"type":"say","content":{"type":"text","text":"the answer"}}
```

to the claude process stdin. This is native bidirectional communication — no `?MW>` prefix hacks needed.

**Open question**: Claude Code's interactive mode handles tool approval prompts internally. In `--print` mode, `--dangerously-skip-permissions` or scoped `--allowedTools` may be needed. The adapter should accept a permission policy from carte.yaml.

## D3: GeminiAdapter

Gemini also supports `--output-format stream-json`:

```
gemini --prompt "task" --output-format stream-json
```

### Event mapping

| Gemini event | Milliways Event |
|--------------|-----------------|
| `{"type":"init","session_id":"...","model":"..."}` | (store session_id, model) |
| `{"type":"message","role":"model","content":"..."}` | EventText / EventCodeBlock |
| `{"type":"tool_call",...}` | EventToolUse |
| `{"type":"result",...}` | EventDone |
| stderr: `TerminalQuotaError: ...reset after XhYmZs` | EventRateLimit{Status:"exhausted"} |

### Quota detection

Gemini reports quota exhaustion as a stderr error with a parseable message:
```
TerminalQuotaError: You have exhausted your capacity on this model. Your quota will reset after 11h26m32s.
```

The adapter captures stderr, parses the reset duration, and emits EventRateLimit. The reset time is computed as `now + parsed duration`.

### Dialogue limitation

Gemini in `--prompt` mode is fire-and-forget. For interactive dialogue, milliways would need to use Gemini's interactive mode or ACP. For release, Gemini operates in headless mode — questions from Gemini are not expected. If Gemini hits its rate limit mid-dispatch, it fails and the error is captured.

## D4: CodexAdapter

Codex supports `--json` for JSONL event output:

```
codex exec --json "task"
```

### Event mapping

Codex JSONL events need to be mapped on first integration — the exact event schema is not yet documented publicly. The adapter reads stdout line-by-line as JSON objects, mapping them to Event types based on the event type field.

### Dialogue

Codex reads stdin when provided. The adapter opens a stdin pipe and writes answers when the TUI calls `Send()`. Codex's `--full-auto` mode may be preferred for non-interactive dispatch, with explicit mode for dialogue-capable dispatch.

## D5: OpenCodeAdapter

OpenCode supports `--format json`:

```
opencode run --format json "task"
```

### Session resume

OpenCode supports `--continue` and `--session <id>` for session continuity. The adapter stores the session ID and reuses it across dispatches.

### Dialogue

OpenCode's JSON format emits events that can include tool calls and status updates. Interactive dialogue capability depends on the event schema — to be determined on integration.

## D6: GenericAdapter (fallback)

The existing `bufio.Scanner` approach, wrapped in the Adapter interface. Every stdout line becomes EventText. No structured data, no cost tracking, no dialogue.

```go
type GenericAdapter struct {
    kitchen kitchen.Kitchen
    // no stdin pipe — fire and forget
}

func (a *GenericAdapter) Exec(ctx context.Context, task kitchen.Task) (<-chan Event, error) {
    // existing GenericKitchen.Exec logic, but wrapped to emit Events
    // each scanner line → Event{Type: EventText, Kitchen: a.kitchen.Name(), Text: line}
    // on exit → Event{Type: EventDone, ExitCode: code}
}

func (a *GenericAdapter) Send(_ context.Context, _ string) error {
    return ErrNotInteractive
}
```

## D7: Session model

### Section-based viewport

Replace `outputLines []string` with a section-based model:

```go
type Session struct {
    Sections []Section
}

type Section struct {
    Prompt    string
    Kitchen   string
    Decision  sommelier.Decision
    Lines     []OutputLine
    Result    *kitchen.Result     // nil while in progress
    Cost      *CostInfo           // nil if unavailable
    StartedAt time.Time
    Duration  time.Duration
    Rated     *bool               // nil = not rated, true = good, false = bad
}

type OutputLine struct {
    Kitchen  string     // source kitchen name
    Type     LineType   // text, code, tool, system
    Text     string     // raw content
    Language string     // for code lines
}

type LineType int

const (
    LineText LineType = iota
    LineCode
    LineTool
    LineSystem // routing info, quota warnings, etc.
)
```

### Viewport rendering

The viewport renders all sections concatenated:

```
▶ refactor the auth module                          ← prompt echo (muted)
──────────────────────────────────────────────────
[opencode] scanning files...                        ← kitchen-prefixed, color-coded
[opencode] found 3 files to modify:
[opencode] ┌─ auth/handler.go ──────────────────┐  ← syntax-highlighted code block
[opencode] │ func Login(w http.ResponseWriter,   │
[opencode] │-  token := genJWT()                 │
[opencode] │+  token, err := genJWT()            │
[opencode] └────────────────────────────────────┘
[opencode] ✓ done  8.4s

▶ explain what opencode just changed                ← next section
──────────────────────────────────────────────────
[claude] The refactoring replaces bare JWT...       ← different kitchen, different color
[claude] ✓ done  3.1s  $0.14
```

### Kitchen prefix colors

Reuse existing `kitchenColors` from styles.go. Each `[kitchen]` prefix is rendered in that kitchen's color:

```
[claude]   → purple (#7C3AED)
[opencode] → green  (#059669)
[gemini]   → blue   (#2563EB)
[codex]    → amber  (#D97706) (new — reuse aider slot or add)
```

### Syntax highlighting

Code blocks (EventCodeBlock) are highlighted using chroma with a terminal-friendly style (e.g., `monokai` or `dracula`). The adapter has already classified the language.

```go
import "github.com/alecthomas/chroma/v2/quick"

func highlightCode(code, language string) string {
    var buf strings.Builder
    err := quick.Highlight(&buf, code, language, "terminal256", "monokai")
    if err != nil {
        return code // fallback to raw
    }
    return buf.String()
}
```

### Glamour toggle (Ctrl+G)

Two render modes for the viewport:

- **Raw mode** (default): text as-is, code blocks syntax-highlighted, kitchen prefixes color-coded
- **Glamour mode** (Ctrl+G): full markdown rendering via glamour — headings, lists, tables, code blocks all rendered. Kitchen prefixes still visible.

```go
type RenderMode int

const (
    RenderRaw    RenderMode = iota
    RenderGlamour
)
```

The toggle re-renders the current viewport content. No data change — purely a presentation toggle.

## D8: Dispatch state machine

Nine states replacing `dispatching bool`:

```
                    ┌──────────────────────┐
                    │                      │
                    ▼                      │
Idle ──enter──▶ Routing ──▶ Routed ──▶ Streaming ──▶ Done
                    │          │          │    ▲        │
                    │          │          │    │        │
                    │          │          ▼    │        ▼
                    │          │      Awaiting─┘     Failed
                    │          │          │
                    │          │          ▼
                    │          │      Confirming──▶ Streaming
                    │          │
                    └──ctrl+c──┴──────────────────▶ Cancelled
```

```go
type DispatchState int

const (
    StateIdle       DispatchState = iota
    StateRouting
    StateRouted
    StateStreaming
    StateDone
    StateFailed
    StateCancelled
    StateAwaiting    // blocked on kitchen question
    StateConfirming  // blocked on kitchen confirm/deny
)
```

### State transitions from events

| Event | Current State | New State |
|-------|--------------|-----------|
| User presses Enter | Idle | Routing |
| EventRouted | Routing | Routed |
| First EventText/EventCodeBlock | Routed | Streaming |
| EventQuestion | Streaming | Awaiting |
| User submits answer | Awaiting | Streaming |
| EventConfirm | Streaming | Confirming |
| User presses y/n | Confirming | Streaming |
| EventDone (exit 0) | Streaming | Done |
| EventDone (exit != 0) | Streaming | Failed |
| EventRateLimit (exhausted) | any active | Failed |
| Ctrl+C | any active | Cancelled |

### Process map — Tier 1 + Tier 2 feedback

**Tier 1** (always visible):
```
┌─ Dispatch ────────────────┐
│ ● opencode                │  ← kitchen badge
│   keyword "refactor"      │  ← why (from Decision.Reason, truncated)
│   tier: keyword           │  ← routing tier
│   risk: low               │  ← risk level
│   elapsed: 4.2s           │  ← live elapsed
└───────────────────────────┘
```

**Tier 2** (pipeline steps — shown below Tier 1 when space permits):
```
┌─ Pipeline ────────────────┐
│ ✓ sommelier.route    12ms │
│ ● kitchen.exec      4.2s  │
│ · ledger.write       —    │
│ · quota.update       —    │
└───────────────────────────┘
```

Steps are updated as the dispatch progresses. Checkmark when done, spinner when active, dot when pending.

## D9: Dialogue overlays

### Question overlay (Awaiting state)

When EventQuestion arrives from any adapter:

1. State → Awaiting
2. Process map shows `? waiting for you`
3. Yellow-bordered overlay textinput appears above the main input bar
4. Main input is disabled (keys route to overlay only)
5. Enter → `adapter.Send(answer)` → state → Streaming → overlay disappears

### Confirm overlay (Confirming state)

When EventConfirm arrives:

1. State → Confirming
2. Inline prompt appended to output: `[confirm] "Delete 14 files?" [y/N]`
3. Single keypress: `y` → Send("y"), `n`/Enter → Send("n")
4. State → Streaming

### Context injection (Ctrl+I)

During Streaming state:

1. Ctrl+I opens overlay with placeholder `+ context:`
2. User types additional context, presses Enter
3. `adapter.Send(text)` — injected into kitchen stdin
4. `[+context] text` appended to output in muted style
5. Overlay disappears, remains in Streaming

### Headless fallback

When `adapter.Send()` returns `ErrNotInteractive` (GenericAdapter):
- Questions: auto-answer with empty string, log warning
- Confirms: auto-answer "n" (safe default), log warning
- Context injection: silently ignored

## D10: Quota-gated routing

### Configuration

carte.yaml gains per-kitchen quota settings:

```yaml
kitchens:
  claude:
    cmd: claude
    args: ["--print", "--verbose"]
    daily_limit: 50        # max dispatches per day (0 = unlimited)
    daily_minutes: 0       # max total dispatch minutes per day (0 = unlimited)
    warn_threshold: 0.8    # show warning at 80% of limit
  gemini:
    cmd: gemini
    daily_limit: 0         # free tier — tracked via rate_limit_event instead
  opencode:
    cmd: opencode
    daily_limit: 0         # local, no limit
```

### QuotaStore additions

```go
// IsExhausted checks if a kitchen has exceeded its daily limit.
// Returns false if no limit is configured (daily_limit=0).
func (s *QuotaStore) IsExhausted(kitchen string, dailyLimit int) (bool, error)

// ResetsAt returns the time when the kitchen's quota resets.
// For daily limits, this is midnight UTC.
// For rate-limit-detected exhaustion, this is the time reported by the kitchen.
func (s *QuotaStore) ResetsAt(kitchen string) (time.Time, error)

// MarkExhausted records that a kitchen has been externally rate-limited.
// resetsAt is the time the kitchen reported it will accept requests again.
func (s *QuotaStore) MarkExhausted(kitchen string, resetsAt time.Time) error

// UsageRatio returns dispatches/limit as a float (0.0-1.0).
// Returns 0 if no limit configured.
func (s *QuotaStore) UsageRatio(kitchen string, dailyLimit int) (float64, error)
```

### Sommelier quota gate

Every candidate kitchen check in RouteEnriched is wrapped with a quota gate:

```go
func (s *Sommelier) isAvailable(kitchenName string) bool {
    k, ok := s.registry.Get(kitchenName)
    if !ok || k.Status() != kitchen.Ready {
        return false
    }
    if s.quotaStore != nil {
        exhausted, _ := s.quotaStore.IsExhausted(kitchenName, s.quotaLimits[kitchenName])
        if exhausted {
            return false
        }
    }
    return true
}
```

When a kitchen is skipped due to quota, the Decision.Reason captures it:
```
"claude exhausted (50/50 today, resets 00:00 UTC) → fallback opencode"
```

### Auto-detection from adapters

When the TUI receives an EventRateLimit:
1. Call `quotaStore.MarkExhausted(kitchen, event.RateLimit.ResetsAt)`
2. Update the status bar to show `kitchen ✗ (resets HH:MM)`
3. The current dispatch continues to completion or failure
4. Next dispatch: sommelier routes around it (Option C)

### Status bar

Top-right of the TUI shows kitchen availability with quota state:

```
claude ✓  opencode ✓  gemini ✗ (resets 17:20)  codex ✓
```

Coloring: green for ready, red for exhausted, yellow for >80% warning.

## D11: Feedback loop

### TUI keybinding

`Ctrl+F` during Idle state (after a completed dispatch) opens a mini-overlay:

```
Rate last dispatch: [g]ood  [b]ad  [s]kip
```

Single keypress records the rating.

### Storage

Rating is written to `pantry.routing.RecordOutcome()` with the explicit success/failure flag. This feeds the existing learned routing model — the sommelier already uses `BestKitchen()` which considers success rates.

```go
// In Section:
Rated *bool // nil = not rated, true = good, false = bad
```

### CLI command

```
milliways rate good    # rate the last dispatch as good
milliways rate bad     # rate the last dispatch as bad
```

Reads the most recent ledger entry and updates its outcome.

## D12: Cross-kitchen session summary

`Ctrl+S` in the TUI renders a summary panel overlay:

```
┌─ Session Summary ─────────────────────────────────┐
│                                                     │
│ Dispatches: 5                                       │
│ Kitchens:   claude (2), opencode (2), codex (1)    │
│ Duration:   42.3s total                             │
│ Cost:       $0.29 (claude: $0.28, codex: $0.01)   │
│ Success:    4/5 (80%)                               │
│                                                     │
│ Last 5:                                             │
│  14:02 claude   "explain auth flow"    3.1s ✓ $0.14│
│  14:05 opencode "refactor auth"        8.4s ✓      │
│  14:08 claude   "review changes"       5.2s ✓ $0.14│
│  14:12 opencode "add tests"           12.1s ✗      │
│  14:15 codex    "fix failing test"     3.5s ✓ $0.01│
│                                                     │
│ [q] close  [r] report  [f] rate last               │
└─────────────────────────────────────────────────────┘
```

## D13: Testing approach

- **Adapter tests**: each adapter gets table-driven tests with a mock subprocess (echo scripts that emit the expected JSON). Test event mapping, code block detection, error handling.
- **ClaudeAdapter integration test**: actual `claude --print` invocation with a trivial prompt, verify Event stream parsing end-to-end. Guarded by build tag `integration` (skipped in CI without claude binary).
- **Session model tests**: section append, line accumulation, rendering with prefixes. Pure unit tests.
- **FSM tests**: table-driven: for each (state, event) pair, verify the resulting state. Cover all 9 states.
- **Quota gate tests**: sommelier with mock QuotaStore — exhausted kitchen skipped, warning threshold, fallback chain when all preferred exhausted.
- **Dialogue tests**: fake adapter emitting EventQuestion, verify overlay opens, answer sent, state transitions.
- **Goroutine leak detection**: `goleak.VerifyTestMain` in adapter and TUI test packages.
- **TUI rendering tests**: string-match on View() output for key visual elements (kitchen prefix, code highlighting, status bar).
