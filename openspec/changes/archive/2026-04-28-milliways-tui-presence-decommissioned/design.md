# Design — milliways-tui-presence

## D1: Dispatch pipeline state machine

The TUI tracks dispatch state as an explicit enum, not an ad-hoc boolean:

```
DispatchState:
  Idle        → user can type
  Routing     → sommelier is running (< 50ms usually)
  Routed      → kitchen chosen, subprocess starting
  Streaming   → kitchen output lines arriving
  Done        → exit code 0
  Failed      → exit code non-zero
  Cancelled   → Ctrl+C during dispatch
  Awaiting    → blocked on kitchen question (see D3)
  Confirming  → blocked on kitchen confirm/deny (see D3)
```

Visual mapping in the process map panel:

```
State        Badge colour   Icon    Label
──────────   ────────────   ────    ─────────────
Routing      muted          ⠋       routing...
Routed       kitchen colour ▶       claude
Streaming    kitchen colour ⠿       streaming
Done         green          ✓       done
Failed       red            ✗       failed
Cancelled    yellow         ⊘       cancelled
Awaiting     yellow         ?       waiting for you
Confirming   yellow         !       confirm required
```

Process map updates on every state transition — no polling needed.

## D2: Prompt echo + routedMsg

**Problem**: startDispatch() calls m.input.SetValue("") and m.outputLines = nil — the user's words vanish with zero feedback.

**Fix**: Two changes to startDispatch():

1. Append the prompt as the first output line immediately:
```
▶ give me a hello world in go
────────────────────────────────
```
This is written to m.outputLines *before* clearing the input.

2. The dispatch goroutine emits a routedMsg as soon as the sommelier decides, *before*
calling kitchen.Exec(). Rather than changing the DispatchFunc signature (breaking
callers), use a DispatchOptions struct — idiomatic Go extensibility without breaking
the existing call sites:

```go
// DispatchOptions carries optional hooks for a single dispatch call.
// Zero value is valid: all fields are nil (no-ops).
type DispatchOptions struct {
    // OnRouted is called synchronously on the dispatch goroutine as soon as
    // the sommelier decision is made, before kitchen.Exec() starts.
    // MUST be non-blocking (use p.Send, not a channel write that could block).
    OnRouted func(sommelier.Decision)
}

// DispatchFunc — opts replaces the former onRouted parameter.
// Callers that don't need routing notification pass DispatchOptions{}.
type DispatchFunc func(
    ctx   context.Context,
    prompt string,
    force  string,
    opts   DispatchOptions,
) (kitchen.Result, sommelier.Decision, error)
```

The TUI wires OnRouted to call p.Send(routedMsg{...}).
tea.Program.Send is goroutine-safe; it MUST be used here (not a channel write)
because the Bubble Tea event loop owns the Model and is not safe to access
concurrently from the dispatch goroutine.

## D3: Kitchen dialogue — bidirectional stdin/stdout

### Protocol (stdout side — kitchen → TUI)

| Prefix    | Meaning                          | TUI action                          |
|-----------|----------------------------------|-------------------------------------|
| ?MW> text | Question — needs free-text reply | Enter Awaiting state, overlay input |
| !MW> text | Confirm/deny — needs y/N         | Enter Confirming state, [y/N] prompt|

All other lines stream to viewport as normal.

### Task struct additions

Per golang-structs-interfaces: use directional channel types and keep the zero
value safe. AnswerCh is receive-only from GenericKitchen's perspective; the TUI
is the sender. OnQuestion and OnConfirm are callbacks (not interfaces) because
they are single-use and carry no state beyond the call.

```go
type Task struct {
    // existing fields ...
    OnQuestion func(question string)   // called when ?MW> line received; nil = headless
    OnConfirm  func(question string)   // called when !MW> line received; nil = headless
    AnswerCh   <-chan string           // TUI sends answers here; nil = headless auto-answer
}
```

The TUI creates a bidirectional channel internally and exposes only the send end
to itself and the receive end (<-chan string) to Task. This enforces ownership:
only the TUI writes to AnswerCh.

```go
// In startDispatch():
answerCh := make(chan string, 1)  // buffered: TUI can send without blocking if kitchen is slow
m.answerCh = answerCh            // TUI holds the send end (chan string)
task.AnswerCh = answerCh         // Kitchen receives read-only end (<-chan string)
```

### Goroutine leak prevention

GenericKitchen.Exec() MUST select on both AnswerCh and ctx.Done() when blocking
for an answer — never block on AnswerCh alone. If the context is cancelled while
waiting for an answer (user hits Ctrl+C), the goroutine must exit cleanly:

```go
// In the scanner loop when a ?MW> line is received:
task.OnQuestion(question)
select {
case answer, ok := <-task.AnswerCh:
    if !ok {
        return // channel closed, exit
    }
    fmt.Fprintln(stdinPipe, answer)
case <-ctx.Done():
    return ctx.Err()
}
```

When AnswerCh is nil (headless mode), skip the select entirely and write "" to
stdin immediately — no goroutine blocking, no leak risk.

### TUI overlay states

Awaiting (question):
- Process map shows "? waiting for you" + question text
- Main input replaced by yellow-bordered overlay textinput.Model
- Enter sends answer to m.answerCh, clears overlay, returns to Streaming

Confirming (!MW>):
- Inline [y/N] prompt appended to outputLines
- y / n / Enter(=no) writes to m.answerCh, returns to Streaming
- No full overlay — single keypress

### Ctrl+I: mid-task context injection

During Streaming, Ctrl+I opens a one-line overlay "+ context: _".
User types and hits Enter → written to m.answerCh immediately (no blocking).
Appears in viewport as "[+context] text" in muted style.

## D4: Visual design

Updated output viewport per dispatch:

```
▶ give me a hello world in go          ← prompt echo, muted
──────────────────────────────────────
⠿ claude  1.4s                         ← badge + elapsed

package main
...

✓ claude  done  3.1s
```

Process map during routing:

```
┌─ Process ──────────────┐
│ ⠋ routing...  0.0s     │   ← while sommelier runs
└────────────────────────┘

┌─ Process ──────────────┐
│ ▶ claude               │   ← routedMsg arrives
│   streaming  1.4s      │
└────────────────────────┘

┌─ Process ──────────────┐
│ ? claude               │   ← question received
│   "Which test runner?" │
│ > ▶ pytest_            │   ← overlay input (yellow)
└────────────────────────┘
```

## D5: Headless compatibility

- DispatchOptions{} zero value is valid — OnRouted is nil (no-op)
- OnQuestion/OnConfirm nil check → auto-answer "" immediately, no blocking
- AnswerCh nil check → skip select, write "" to stdin
- --verbose flag prints [routed] <kitchen> to stderr

## D6: No protocol changes to kitchen CLIs

?MW> and !MW> are opt-in conventions documented in internal/kitchen/dialogue.go
with exported constants and helper funcs. Kitchens that don't use the protocol
are unaffected — their stdout lines pass through OnLine unchanged.

## D7: Testing approach

Per golang-testing skill:
- All new behaviour covered by table-driven unit tests with named subtests
- internal/tui tests use t.Parallel() where safe
- Goroutine leak detection: TestMain in internal/kitchen uses goleak.VerifyTestMain
  because GenericKitchen.Exec() now spawns a goroutine for stdin; leaks must be caught
- Mock DispatchFunc in TUI tests — accepts DispatchFunc as a dependency, easy to stub
- Dialogue protocol tested with a fake kitchen that writes ?MW>/!MW> lines to a pipe
  and an AnswerCh that the test controls
