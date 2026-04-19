# Design — milliways-tui-panels

## D1: Panel interchange architecture

### SidePanelMode enum

```go
// SidePanelMode identifies which panel is shown in the bottom-right sidebar.
type SidePanelMode int

const (
    SidePanelLedger SidePanelMode = iota
    SidePanelJobs
    SidePanelCost
    SidePanelRouting
    SidePanelSystem
    SidePanelOpenSpec
    SidePanelSnippets
    SidePanelDiff
    SidePanelCompare
    sidePanelCount // internal sentinel
)
```

Panel order is deliberate: most-used first (Ledger, Jobs), diagnostic second (Cost, Routing, System), dev workflow (OpenSpec), utility last (Snippets, Diff, Compare).

### Model additions

```go
type Model struct {
    // ... existing fields ...

    // Side panel (bottom-right).
    sidePanelIdx  int                    // index into panel order
    sidePanelMeta map[SidePanelMode]sidePanelState  // per-panel state

    // Cost accumulation (Cost panel).
    costByKitchen map[string]costAccumulator
    costTotalUSD  float64

    // Routing history (Routing panel).
    routingHistory []routingEntry  // last 20 decisions

    // Subprocess monitoring (System panel).
    procStats map[string]procInfo  // kitchen → resource usage

    // OpenSpec (OpenSpec panel).
    openSpecChanges          []openSpecChange
    openSpecSelected         int
    openSpecExpanded         bool
    openSpecCourses          []openSpecCourse
    openSpecCourseSelected    int

    // Snippets (Snippets panel).
    snippetIndex  []snippet
    snippetFilter string

    // Diff/changeset (Diff panel).
    changedFiles []diffFile

    // Compare (Compare panel).
    compareResults map[string][]compareResult  // prompt → per-kitchen result
}
```

### Keybindings

| Key | Action |
|-----|--------|
| `ctrl+]` | Advance to next panel (wraps) |
| `ctrl+[` | Previous panel (wraps) |
| `ctrl+j` | Alias for `ctrl+]` |
| `ctrl+k` | Alias for `ctrl+[` |

### Panel header

Every swappable panel renders with a header line:

```
┌─ Cost ╌╌ ctrl+[/ctrl+] ─┐   ← panelBorder with panel name + nav hint
│ ... content ...          │
└──────────────────────────┘
```

`panelBorder` style is reused. Panel name uses `mutedStyle`. Nav hint uses a dimmer color.

### View() restructure

Current:
```go
ledgerHeight := (m.height - 6) - blockListHeight
ledgerPanel := panelBorder.Width(sideWidth).Height(ledgerHeight).Render(m.renderLedger())
```

New:
```go
bottomPanelHeight := (m.height - 6) - blockListHeight
bottomPanel := m.renderActiveSidePanel(sideWidth, bottomPanelHeight)

mainArea := lipgloss.JoinHorizontal(lipgloss.Top,
    outputPanel,
    lipgloss.JoinVertical(lipgloss.Left, blockListPanel, bottomPanel),
)
```

`renderActiveSidePanel` dispatches to the correct renderer:
```go
func (m Model) renderActiveSidePanel(width, height int) string {
    panelName := sidePanelNames[m.sidePanelIdx]
    header := panelBorder.Width(width).Render(panelName + " \u2387 ctrl+[/ctrl+]")
    content := m.renderCurrentPanel(width, height)
    return lipgloss.JoinVertical(lipgloss.Left, header, content)
}

func (m Model) renderCurrentPanel(width, height int) string {
    switch m.sidePanelIdx {
    case SidePanelLedger:   return m.renderLedgerPanel(width, height)
    case SidePanelJobs:     return m.renderJobsPanel(width, height)
    case SidePanelCost:     return m.renderCostPanel(width, height)
    case SidePanelRouting:  return m.renderRoutingPanel(width, height)
    case SidePanelSystem:   return m.renderSystemPanel(width, height)
    case SidePanelOpenSpec: return m.renderOpenSpecPanel(width, height)
    case SidePanelSnippets: return m.renderSnippetsPanel(width, height)
    case SidePanelDiff:     return m.renderDiffPanel(width, height)
    case SidePanelCompare:  return m.renderComparePanel(width, height)
    }
    return ""
}
```

### Panel state persistence

`sidePanelIdx` is saved/restored with the TUI session (same as `focusedIdx`, `history`). No extra persistence needed — panel selection survives across deserializations.

---

## D2: Cost panel

### Data collection

`adapter.EventCost` fires at end of each dispatch:
```go
type Event struct {
    Cost *CostInfo  // USD, InputTokens, OutputTokens, CacheRead, CacheWrite, DurationMs
}
```

Accumulator in Model:
```go
type costAccumulator struct {
    Calls     int
    InputToks int
    OutputToks int
    CacheRead  int
    CacheWrite int
    TotalUSD   float64
}
```

On `adapter.EventCost`: `m.costByKitchen[kitchen].add(evt.Cost)`, `m.costTotalUSD += evt.Cost.USD`

### Renderer

```
┌─ Cost ╌╌ ctrl+[/ctrl+] ─┐
│ claude   $0.42  12K/8K tok│
│ codex    $0.18   4K/3K tok│
│ gemini   $0.07   2K/1K tok│
│ ──────────────────────────│
│ Total    $0.67  18K/12K   │
└───────────────────────────┘
```

Columns: kitchen badge, USD, input/output token counts. Total row highlighted.

### Tests

- Cost accumulates correctly across multiple dispatches
- Per-kitchen breakdown accurate
- Empty state: "No dispatches yet"
- USD rounds to 2 decimal places

---

## D3: Routing audit panel

### Data collection

`sommelier.Decision` fires on each routing. Capture in `routingHistory`:
```go
type routingEntry struct {
    Prompt   string
    Kitchen  string
    Tier     string  // keyword, enriched, learned, forced, fallback
    Reason   string  // human-readable
    Signals  map[string]float64  // signal name → score
    At       time.Time
}
```

On `blockRoutedMsg`: push `routingEntry{prompt, decision.Kitchen, decision.Tier, decision.Reason, signals, now}` to front of `m.routingHistory`; trim to 20.

### Renderer

```
┌─ Routing ╌ ctrl+[/ctrl+] ─┐
│ [forced] claude  sticky    │
│ [keyword] gemini  web search│
│ [enriched] codex  ctx match │
│ ...                         │
└─────────────────────────────┘
```

Each entry: tier badge + kitchen + reason (truncated to panel width). Clicking/Enter on a routing entry expands to show full signal scores in a popup overlay.

### Tests

- History grows to 20, then oldest dropped
- Tier badge colors: forced=amber, keyword=purple, enriched=blue, learned=green, fallback=muted
- Empty state: "No routing decisions yet"

---

## D4: System resources panel

### Data collection

Background goroutine (separate from TUI tick loop — not a tea.Cmd, a `sysrefresh.Tick`) every 3s:
```go
// psutil-free: shell out to `ps -p <pid> -o %cpu,%mem=` for each active kitchen subprocess
type procInfo struct {
    PID    int
    CPU    float64  // percent
    MemMB  float64
    Exe    string   // basename
}
```

Active subprocesses tracked via `runtimeEvents` — when a kitchen starts, record its PID; when it ends, remove it.

### Renderer

```
┌─ System ╌ ctrl+[/ctrl+] ─┐
│ claude   PID 12345         │
│         CPU  24%  MEM 180M│
│ codex    PID 12346         │
│         CPU  12%  MEM  90M│
│ (idle)   no active procs   │
└────────────────────────────┘
```

Only shows running processes. If no active procs: "(idle)". CPU/Mem highlighted yellow if >80%.

### Tests

- Mock `ps` output → correct parsing
- Processes gone from list when they exit
- Empty state: "(idle)"

---

## D5: Prompt library (snippets) panel

### Storage

`~/.config/milliways/snippets.toml`:
```toml
[[snippet]]
name = "explain"
body = "Explain this code: \n$FILE"
tags = ["read", "explain"]
lang = "en"

[[snippet]]
name = "test for"
body = "Write pytest tests for:\n$CODE\n---\nRequirements:\n$REQ"
tags = ["test", "pytest"]
lang = "en"
```

### Loader

On panel activation: parse `snippets.toml` (created with defaults if absent). Fuzzy filter on `name` and `tags`.

### Renderer

```
┌─ Snippets ╌ ctrl+[/ctrl+] ─┐
│ > explain                   │  ← > = selected
│   test for                  │
│   refactor                  │
│   review                    │
│ [enter] insert  [ctrl+s] save│
└─────────────────────────────┘
```

### Interaction

- `↑/↓` navigates snippet list (filtered)
- `Enter` inserts snippet text into input bar (with `$VAR` placeholders preserved as-is)
- `[ctrl+s]` saves edited snippet (opens inline edit mode)

### Tests

- Loads snippets.toml on panel activation
- Creates default file if absent
- Fuzzy filter matches name and tags
- Insert places text in input bar

---

## D6: Diff / changeset panel

### Data collection

On each dispatch completion, capture git diff:
```bash
git diff --name-only HEAD~{n}  # n = number of dispatches in session
git diff HEAD -- <file>        # for selected file
```

Also track file list from `runtimeEvents` where `Kind == "file_changed"`.

### Renderer

```
┌─ Diff ╌ ctrl+[/ctrl+] ──────┐
│ M  internal/tui/app.go     │
│ M  internal/tui/view.go     │
│ A  internal/tui/cost.go     │
│ ?? testdata/smoke/output   │
│ ───────────────────────────│
│ [↓] internal/tui/app.go    │  ← ↓ = selected for preview
└─────────────────────────────┘
```

### Expanded view (on Enter)

Full unified diff for selected file rendered inline in a scrollable overlay within the panel area.

### Tests

- Parses git diff output correctly
- Shows staged + unstaged + untracked
- Empty state: "No changes in this session"

---

## D7: Multi-model compare panel

### Trigger

User types a prompt, then hits `ctrl+shift+enter` (instead of Enter) to dispatch to ALL available kitchens in parallel rather than just the selected one. The compare panel activates automatically.

### Data collection

For each kitchen in the dispatch set:
```go
type compareResult struct {
    Kitchen string
    Output  string  // streaming text
    Done    bool
    Error   string
}
```

All results stored in `m.compareResults[prompt]`. Streaming updates as lines arrive from each kitchen.

### Renderer

```
┌─ Compare ╌ ctrl+[/ctrl+] ──┐
│ claude  ██████████░░ 75%     │
│ codex   ████████████ 100% ✓  │
│ gemini  ████░░░░░░░ 30%     │
│ ────────────────────────────│
│ [claude ▾] selected         │
│ ────────────────────────────│
│ package main                │
│                             │
└─────────────────────────────┘
```

Shows per-kitchen progress bar (% of output received). Below: the selected kitchen's full output. Clicking a kitchen in the list switches the preview to that kitchen's output.

### Tests

- Parallel dispatch to N kitchens produces N results
- Each kitchen streams independently
- Switching preview updates correctly
- Empty state: "ctrl+shift+enter to compare"

---

## D8: Jobs panel (extract from jobs-panel proposal)

Jobs panel renders `m.jobTickets` (already populated by `jobsRefreshMsg`):

```
┌─ Jobs ╌ ctrl+[/ctrl+] ──────┐
│ ⠿ migrate schema  running  │  ⠿ = active spinner
│ ✓ lint passed    complete   │
│ ✗ test suite     failed     │
│ ○ deploy        pending     │
└─────────────────────────────┘
```

Status icons: running=⠿, complete=✓ (green), failed=✗ (red), pending=○ (muted).

---

## D9: Ledger panel (extract from existing renderLedger)

Existing `renderLedger()` extracted to `renderLedgerPanel()` with same behaviour.

---

## D10: OpenSpec panel

### Data collection

Two sources: `openspec list --json` (all changes + completion %) and direct `tasks.md` parsing for the selected active change.

```go
type openSpecChange struct {
    Name       string
    TasksDone  int
    TasksTotal int
    IsActive   bool
    IsArchived bool
    Schema     string
}

type openSpecCourse struct {
    Name       string
    Done, Total int
}
```

Refreshed every 30 seconds (not every tick — openspec CLI is slow) via a dedicated `tea.Tick` command separate from the 2s block tick.

On panel activation: `openspec list --json` → parse into `[]openSpecChange`. The active change is the one without `archived_at`. Its `tasks.md` is parsed to extract course-level task counts.

### Course-level parsing

`tasks.md` format:
```
## Course NC-1: ...
- [x] NC-1.1 ...
- [ ] NC-1.2 ...

## Course NC-2: ...
- [x] NC-2.1 ...
```

Parse to `[]openSpecCourse` for the active change. Show each course with `Done/Total` and a mini progress bar.

### Renderer

Default view (shows change-level overview):
```
┌─ OpenSpec ╌ ctrl+[/ctrl+] ─┐
│ ★ milliways-nvim-context   │
│   ████████████░░░ 19/22 86%│
│   [NC-15 ▶ NC-17]          │
│ ───────────────────────────│
│ mill iways-tui-panels       │
│   ██████████░░░░░░  5/17 29%│
│   [SPS-1 SPS-2 ▶ SPS-3]   │
│ ───────────────────────────│
│ mill iways-kitchen-parity   │
│   ██████████████░  89/94 94%│
│ two-active-memory          │
│   ▐░░░░░░░░░░░░░   2/15 13%│
│ [↑/↓] select  [enter] expand│
└────────────────────────────┘
```

Expanded view (when a change is selected, Enter):
```
┌─ OpenSpec: SPS-3 ╌ ctrl+[/ctrl+] ─┐
│ Course SPS-3: Routing audit         │
│   ✓ SPS-3.1 SignalScores field     │
│   ✓ SPS-3.2 routingEntry collection│
│   ◯ SPS-3.3 renderRoutingPanel    │
│   ◯ SPS-3.4 Tests                  │
│ ────────────────────────────────────│
│ Course SPS-4: System resources       │
│   ◯ SPS-4.1 procInfo + monitor     │
│   ◯ SPS-4.2 Block.PID tracking     │
│   ◯ SPS-4.3 renderSystemPanel      │
│   ◯ SPS-4.4 Tests                  │
│ [↑/↓]  [enter] toggle expand  [b] back│
└──────────────────────────────────────┘
```

### Interaction

- `↑/↓` navigates the change list (or course list when expanded)
- `Enter` on a change → expand/collapse course list for that change
- `b` when expanded → back to change list
- `ctrl+o` jumps directly to the OpenSpec panel from anywhere (global shortcut)
- `Enter` on a course item → opens the relevant spec file in `$EDITOR` (optional, nice-to-have)

### Model state

```go
openSpecChanges  []openSpecChange
openSpecSelected  int  // index into openSpecChanges
openSpecExpanded  bool // true = showing courses for selected change
openSpecCourses   []openSpecCourse  // courses for selected change
openSpecCourseSelected int
```

### Data source: openspec CLI

```bash
openspec list --json
# Returns:
# [{"name": "...", "status": "active", "archived_at": null, ...}]

openspec status --change <name> --json
# Returns per-artifact status for the named change.
```

Tasks parsed from `openspec/changes/<name>/tasks.md` by regex:
- Course: `## Course <ID>: <name>`
- Task: `- [x]` / `- [ ]` lines under a course section

### Tests

- `openspec list --json` output parsed correctly
- Course task counts match actual `-[x]` / `- [ ]` lines in tasks.md
- `↑/↓` navigation wraps at boundaries
- Expand/collapse toggles correctly
- Empty state (no changes): "No active changes — run `openspec propose`"

---

## D11: Visibility rules

| Panel | Requires |
|-------|---------|
| Cost | ≥1 completed dispatch with cost data |
| Routing | ≥1 routing decision |
| System | ≥1 active subprocess |
| Snippets | `snippets.toml` parsed (always available) |
| Diff | ≥1 file changed or git diff available |
| Compare | ≥1 compare result in `m.compareResults` |
| Jobs | `m.jobTickets` non-empty |
| Ledger | Always available |
| OpenSpec | Always available (shows "(openspec not found)" if CLI absent) |
| OpenSpec | Always available (openspec CLI installed) |

Panels that have no data show a muted "(no data)" line rather than an empty panel.
