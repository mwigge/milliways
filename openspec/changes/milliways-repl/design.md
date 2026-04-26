## Context

The current milliways TUI (Bubble Tea-based) has been failing for months. Streaming is broken (fire-and-forget buffering), the architecture is complex (panels, vim mode, mouse, PTY), and none of the TUI features work reliably. The tiering auto-router was abandoned. All TUI-related OpenSpec changes are stuck in-progress or broken.

The milliways use case is clear: route between claude, codex, and minimax with shared memory. The interface should be a simple, fast, terminal-native REPL — not a complex TUI.

## Goals / Non-Goals

**Goals:**
- Lightweight, fast REPL that starts instantly and streams output in real-time
- Sequential runner execution: one runner at a time, switch explicitly
- Shared memory across runners via mempalace persistence
- Green phosphor terminal aesthetic (Adobe palette)
- claude and codex use CLI subprocess execution; minimax uses HTTP API (MiniMax-M2.7 model)
- Full REPL command set for routing, session, context, and auth

**Non-Goals:**
- No TUI (panels, vim mode, mouse, concurrent blocks)
- No auto-routing (manual `/switch` only)
- No file browser (use yazi/lazygit externally)
- No nvim plugin
- No Bubble Tea
- Headless mode is not a priority

## Decisions

### 1. Input: `liner` library

`liner` is a pure Go readline replacement used by `git`, `docker`, and many other tools. Single dependency, battle-tested.

**Alternatives considered:**
- `readline` library: older, less maintained
- Raw terminal mode: more control but lose history/completion

### 2. Output: Raw ANSI escape sequences

Direct stdout writes with ANSI escape sequences for colors and cursor movement. No intermediate rendering layer.

**Alternatives considered:**
- Bubble Tea: was already tried and failed (slow, complex)
- `termbox`: lighter than Bubble Tea but still an extra layer

### 3. Mixed execution: CLI for claude/codex, HTTP for minimax

Claude and codex execute as `exec.CommandContext` subprocesses with piped stdout. Minimax uses direct HTTP API calls to the MiniMax endpoint (MiniMax-M2.7 model) with SSE streaming.

**Alternatives considered:**
- mmx CLI for minimax: does not have interactive prompt mode, command-per-invocation only
- Unified HTTP adapter: adds HTTP overhead for CLI kitchens that stream stdout directly

### 4. Auth: PTY only for login flows

PTY (pseudo-terminal) allocated only when running `claude auth login` or `codex login`. All other execution uses piped stdin/stdout without PTY.

**Alternatives considered:**
- Always PTY: adds complexity for no benefit during normal execution
- Never PTY: breaks interactive auth flows

### 5. Streaming: line-by-line subprocess stdout

Each line from subprocess stdout is written to terminal immediately. No buffering.

**Alternatives considered:**
- Buffered rendering: was the current fire-and-forget problem
- Chunked rendering: adds complexity without benefit

### 6. Session persistence: mempalace fork

Mempalace fork already exists. Verify it has the conversation primitive (start/end conversation, append turn, checkpoint/resume).

**Alternatives considered:**
- SQLite directly: loses mempalace semantic search benefits
- Redis: adds external dependency

### 7. Color palette: Adobe "Monochromatic CPU Terminal Green"

```
Background: #000000 (pure black)
Primary:   #4FB522 (bright phosphor green — main text)
Secondary: #2E6914 (medium green — secondary text)
Muted:     #466D35 (dark green — borders, inactive)
Error:     #FF4444 (red)
Warning:   #FFAA00 (amber)
```

### 8. Parallelism = multiple terminal sessions

If user wants parallel execution, they run multiple `milliways` processes in separate terminal sessions. No concurrent dispatch within a single REPL.

## Risks / Trade-offs

- **[Risk] Mempalace conversation primitive not ready**
  → Mitigation: Spike first to verify fork has the required primitives (mempalace_conversation_start/end, append_turn, checkpoint/resume)

- **[Risk] liner compatibility on different terminals**
  → Mitigation: Test on macOS Terminal, iTerm2, Alacritty. Raw ANSI is well-supported.

- **[Risk] HTTP streaming vs CLI streaming normalization**
  → Mitigation: Adapter per kitchen normalizes output to a common streaming format before terminal write.

- **[Risk] Quota data from claude/codex unavailable**
  → Mitigation: If runner doesn't expose quota, show "unknown" or cache from last known usage.

- **[Trade-off] No concurrent dispatch**
  → Benefit: Simpler architecture, no race conditions, no panel synchronization
  → Cost: User must run multiple milliways sessions for parallel work

- **[Trade-off] Sequential runners lose per-runner session**
  → Each runner starts fresh on `/switch`. Mempalace provides cross-runner memory.
  → Runner's native session features (resume, fork) become less relevant.

## Open Questions

1. Does the mempalace fork have the conversation primitive fully implemented?
2. Do `claude` and `codex` have quota commands to get day/week/month usage?
3. Should `/limit` query runners live, or cache and refresh periodically?
4. How to handle Ctrl+C (interrupt current runner vs exit REPL)?
