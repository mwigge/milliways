# Tasks — milliways-tui-panels

> Note: `milliways-jobs-panel` (separate change) becomes JP-extra in this change.
> Its tasks are subsumed here as `SidePanelJobs`.

## Prerequisites

- [ ] JP-extra: **Depends on `milliways-jobs-panel` JP-3 being complete.** Once `renderJobsPanel()` exists (written in JP-3, NOT in SPS-1), extract it as a `renderJobsPanel(width, height int)` method and wire it into `renderActiveSidePanel` as `case SidePanelJobs`. The milliways-tui-panels change does NOT own the rendering logic — it only integrates the rendering already built in JP-3.

---

## Course SPS-1: Panel system core [2 SP]

### SPS-1.1 — SidePanelMode enum + state

In `internal/tui/state.go`, add:
```go
type SidePanelMode int

const (
    SidePanelLedger SidePanelMode = iota
    SidePanelJobs
    SidePanelCost
    SidePanelRouting
    SidePanelSystem
    SidePanelSnippets
    SidePanelDiff
    SidePanelCompare
    sidePanelCount
)

var sidePanelNames = []string{
    "Ledger", "Jobs", "Cost", "Routing", "System", "Snippets", "Diff", "Compare",
}
```

### SPS-1.2 — Model fields

In `internal/tui/app.go` Model struct, add:
```go
sidePanelIdx  int  // which panel is active (0 = Ledger)

costByKitchen map[string]costAccumulator
costTotalUSD  float64

routingHistory []routingEntry

procStats map[string]procInfo  // kitchen name → live stats

snippetIndex []snippet
snippetFilter string
snippetSelected int

changedFiles []diffFile

compareResults map[string][]compareResult  // prompt → per-kitchen results
compareSelectedKitchen string
```

### SPS-1.3 — Panel renderer dispatch

Add to `internal/tui/view.go`:
```go
func (m Model) renderActiveSidePanel(width, height int) string {
    if height < 4 {
        return ""
    }
    contentHeight := height - 2  // reserve 2 for header
    header := fmt.Sprintf("┌─ %s \u2387 ctrl+[/ctrl+] ─", sidePanelNames[m.sidePanelIdx])
    content := m.renderCurrentPanel(width, contentHeight)
    footer := strings.Repeat("─", width-2)
    return lipgloss.JoinVertical(lipgloss.Left,
        panelBorder.Width(width).Render(header),
        panelBorder.Width(width).Height(contentHeight).Render(content),
    )
}

func (m Model) renderCurrentPanel(width, height int) string {
    switch m.sidePanelIdx {
    case SidePanelLedger:   return m.renderLedgerPanel(width, height)
    case SidePanelJobs:     return m.renderJobsPanel(width, height)
    case SidePanelCost:     return m.renderCostPanel(width, height)
    case SidePanelRouting: return m.renderRoutingPanel(width, height)
    case SidePanelSystem:   return m.renderSystemPanel(width, height)
    case SidePanelSnippets: return m.renderSnippetsPanel(width, height)
    case SidePanelDiff:     return m.renderDiffPanel(width, height)
    case SidePanelCompare:  return m.renderComparePanel(width, height)
    }
    return mutedStyle.Render("(no panel)")
}
```

### SPS-1.4 — View() restructure

In `internal/tui/view.go`, replace the ledger panel section:
```go
// OLD:
ledgerPanel := panelBorder.Width(sideWidth).Height(ledgerHeight).Render(m.renderLedger())
// NEW:
bottomPanelHeight := (m.height - 6) - blockListHeight
bottomPanel := m.renderActiveSidePanel(sideWidth, bottomPanelHeight)
```

And update the `lipgloss.JoinVertical` to use `bottomPanel` instead of `ledgerPanel`.

### SPS-1.5 — Keybindings for panel cycling

In `app.go` `handleKey()`, add:
```go
case "ctrl+]":
    m.sidePanelIdx = (m.sidePanelIdx + 1) % int(sidePanelCount)
    return nil
case "ctrl+[":
    m.sidePanelIdx--
    if m.sidePanelIdx < 0 {
        m.sidePanelIdx = int(sidePanelCount) - 1
    }
    return nil
case "ctrl+j":  // alias for ctrl+]
    m.sidePanelIdx = (m.sidePanelIdx + 1) % int(sidePanelCount)
    return nil
case "ctrl+k":  // alias for ctrl+[
    m.sidePanelIdx--
    if m.sidePanelIdx < 0 {
        m.sidePanelIdx = int(sidePanelCount) - 1
    }
    return nil
```

### SPS-1.6 — renderLedgerPanel extraction

Rename existing `renderLedger()` to `renderLedgerPanel(width, height int)` — the existing implementation accepts no width/height; update it to respect width constraint and height limit.

### SPS-1.7 — Panel stubs

Add stub implementations to `view.go`:
```go
func (m Model) renderJobsPanel(width, height int) string   // delegates to existing jobs rendering
func (m Model) renderCostPanel(width, height int) string    // stubs
func (m Model) renderRoutingPanel(width, height int) string
func (m Model) renderSystemPanel(width, height int) string
func (m Model) renderOpenSpecPanel(width, height int) string
func (m Model) renderSnippetsPanel(width, height int) string
func (m Model) renderDiffPanel(width, height int) string
func (m Model) renderComparePanel(width, height int) string
```

Each stub returns `mutedStyle.Render("(not implemented)")`. This ensures View() compiles while each panel is implemented course-by-course.

### SPS-1.8 — Tests

- Table-driven: cycling `ctrl+]/ctrl+[` wraps correctly at both ends
- `renderActiveSidePanel` returns "" when height < 4
- `sidePanelNames` length == sidePanelCount
- Each stub renders non-empty string

---

## Course SPS-2: Cost panel [1 SP]

### SPS-2.1 — costAccumulator type

In `internal/tui/app.go`:
```go
type costAccumulator struct {
    Calls, InputToks, OutputToks, CacheRead, CacheWrite int
    TotalUSD float64
}

func (a *costAccumulator) add(c *adapter.CostInfo) {
    if c == nil {
        return
    }
    a.Calls++
    a.InputToks += c.InputTokens
    a.OutputToks += c.OutputTokens
    a.CacheRead += c.CacheRead
    a.CacheWrite += c.CacheWrite
    a.TotalUSD += c.USD
}
```

### SPS-2.2 — Collect cost events

In `app.go` `Update()`, handle `blockDoneMsg`:
```go
case blockDoneMsg:
    // existing handling...
    if msg.Result.ExitCode == 0 && msg.Decision.Kitchen != "" {
        // cost data comes from runtime events; listen for EventCost
    }
// Also listen for runtimeEventMsg where Event.Kind == "cost":
case runtimeEventMsg:
    if msg.Event.Kind == "cost" {
        if usd, ok := msg.Event.Fields["usd"]; ok {
            m.costTotalUSD += usd.(float64)
        }
    }
```

Note: The orchestrator emits `adapter.EventCost` via `onEvent` — these arrive as `blockEventMsg`. Intercept in `Update()`:
```go
case blockEventMsg:
    if msg.Event.Type == adapter.EventCost && msg.Event.Cost != nil {
        kitchen := msg.Event.Kitchen
        if m.costByKitchen == nil {
            m.costByKitchen = make(map[string]costAccumulator)
        }
        m.costByKitchen[kitchen].add(msg.Event.Cost)
    }
    // ... existing blockEventMsg handling
```

### SPS-2.3 — renderCostPanel implementation

```go
func (m Model) renderCostPanel(width, height int) string {
    if len(m.costByKitchen) == 0 {
        return mutedStyle.Render("(no cost data yet)")
    }
    lines := []string{}
    for kitchen, acc := range m.costByKitchen {
        badge := KitchenBadge(kitchen)
        usd := fmt.Sprintf("$%.2f", acc.TotalUSD)
        toks := fmt.Sprintf("%dK/%dK", acc.InputToks/1000, acc.OutputToks/1000)
        lines = append(lines, fmt.Sprintf("%s %s %s tok", badge, usd, toks))
    }
    lines = append(lines, "", fmt.Sprintf("Total  $%.2f", m.costTotalUSD))
    return strings.Join(lines, "\n")
}
```

### SPS-2.4 — Tests

- `TestCostPanelAccumulates`: dispatch 2 events to same kitchen → cumulative USD correct
- `TestCostPanelEmpty`: no events → "(no cost data yet)"
- `TestCostPanelRoundsUSD`: 1.555 → "$1.56"

---

## Course SPS-3: Routing audit panel [1 SP]

### SPS-3.1 — routingEntry type + collection

```go
type routingEntry struct {
    Kitchen string
    Tier    string
    Reason  string
    Signals map[string]float64
    At      time.Time
}
```

On `blockRoutedMsg` in `Update()`:
```go
case blockRoutedMsg:
    // ... existing handling ...
    entry := routingEntry{
        Kitchen: msg.Decision.Kitchen,
        Tier:    msg.Decision.Tier,
        Reason:  msg.Decision.Reason,
        Signals: msg.Decision.SignalScores,  // if Decision has this field
        At:      time.Now(),
    }
    m.routingHistory = append([]routingEntry{entry}, m.routingHistory...)
    if len(m.routingHistory) > 20 {
        m.routingHistory = m.routingHistory[:20]
    }
```

Note: If `sommelier.Decision` doesn't yet have `SignalScores`, add it — it's needed for the audit panel to show the full picture.

### SPS-3.2 — renderRoutingPanel implementation

```go
func (m Model) renderRoutingPanel(width, height int) string {
    if len(m.routingHistory) == 0 {
        return mutedStyle.Render("(no routing decisions yet)")
    }
    lines := []string{}
    for _, e := range m.routingHistory {
        tierBadge := tierBadge(e.Tier)  // color-coded by tier
        reason := truncateString(e.Reason, width-20)
        lines = append(lines, fmt.Sprintf("%s %s  %s", tierBadge, KitchenBadge(e.Kitchen), reason))
    }
    return strings.Join(lines, "\n")
}

func tierBadge(tier string) string {
    switch tier {
    case "forced":   return badgeStyle.Render("[forced]")
    case "keyword":  return badgeStyle.Render("[kw]")
    case "enriched": return badgeStyle.Render("[enr]")
    case "learned": return badgeStyle.Render("[lrnd]")
    case "fallback": return mutedStyle.Render("[fallbk]")
    default:         return mutedStyle.Render("[" + tier + "]")
    }
}
```

### SPS-3.3 — Tests

- `TestRoutingHistoryGrowsAndTrims`: push 25 entries → len == 20
- `TestRoutingPanelEmpty`: empty history → "(no routing decisions yet)"
- `TestTierBadge`: each tier maps to non-empty string

---

## Course SPS-4: System resources panel [1 SP]

### SPS-4.1 — procInfo type + refresh goroutine

```go
type procInfo struct {
    PID   int
    CPU   float64
    MemMB float64
    Exe   string
}

// startSystemMonitor launches a background goroutine that refreshes
// m.procStats every 3s while the TUI is running.
func (m *Model) startSystemMonitor() {
    go func() {
        tick := time.NewTicker(3 * time.Second)
        defer tick.Stop()
        for {
            <-tick.C
            m.mu.Lock()  // if Model has a mutex, or use atomic swap
            m.refreshProcStats()
            m.mu.Unlock()
        }
    }()
}

func (m *Model) refreshProcStats() {
    // For each active block with a running process, shell out to ps:
    // ps -p <pid> -o %cpu=,%mem=,comm=
    // psutil would be cleaner but adds a C dep; shell-out is consistent with existing patterns.
    for _, b := range m.blocks {
        if b.PID > 0 && !b.isDone() {
            if stats, err := fetchProcStats(b.PID); err == nil {
                m.procStats[b.Kitchen] = stats
            }
        }
    }
}
```

Add `procStats map[string]procInfo` and `mu sync.Mutex` to Model.

### SPS-4.2 — Block.PID tracking

`Block` struct needs a `PID int` field. Set in `adapterDispatchCmd` — the orchestrator knows the PID from the adapter. Or: track via `runtimeEventMsg` where `Kind == "segment_start"` and `Fields["pid"]` is present.

### SPS-4.3 — renderSystemPanel implementation

```go
func (m Model) renderSystemPanel(width, height int) string {
    m.mu.Lock()
    defer m.mu.Unlock()
    if len(m.procStats) == 0 {
        return mutedStyle.Render("(idle)")
    }
    lines := []string{}
    for kitchen, p := range m.procStats {
        cpuStr := fmt.Sprintf("%.0f%%", p.CPU)
        memStr := fmt.Sprintf("%.0fM", p.MemMB)
        if p.CPU > 80 || p.MemMB > 500 {
            cpuStr = warningStyle.Render(cpuStr)
            memStr = warningStyle.Render(memStr)
        }
        lines = append(lines,
            fmt.Sprintf("%s  PID %d", KitchenBadge(kitchen), p.PID),
            fmt.Sprintf("CPU %s  MEM %s", cpuStr, memStr),
        )
    }
    return strings.Join(lines, "\n")
}
```

### SPS-4.4 — Tests

- Mock `ps` output → correct parsing
- Empty procStats → "(idle)"
- CPU > 80 → warning style applied

---

## Course SPS-5: Prompt library (snippets) panel [1 SP]

### SPS-5.1 — snippet data model + loader

```go
type snippet struct {
    Name string
    Body string
    Tags []string
    Lang string
}

var defaultSnippets = []snippet{
    {Name: "explain", Body: "Explain this code:\n$FILE", Tags: []string{"read", "explain"}, Lang: "en"},
    {Name: "test for", Body: "Write pytest tests for:\n$CODE\n---\nRequirements:\n$REQ", Tags: []string{"test", "pytest"}, Lang: "en"},
    {Name: "refactor", Body: "Refactor this code:\n$CODE\n---\nGoals:\n$GOALS", Tags: []string{"refactor"}, Lang: "en"},
    {Name: "review", Body: "Review this code for bugs and style:\n$FILE", Tags: []string{"review", "security"}, Lang: "en"},
}
```

`loadSnippets(path string) []snippet`: reads `snippets.toml` if exists, merges with defaults, writes default file if absent.

TOML format (github.com/BurntSushi/toml):
```toml
[[snippet]]
name = "explain"
body = "Explain this code:\n$FILE"
tags = ["read", "explain"]
lang = "en"
```

### SPS-5.2 — Filter navigation

In `Update()` key handling for `SidePanelSnippets`:
- `↑/↓` moves `m.snippetSelected`
- `Enter` inserts `m.snippetIndex[m.snippetSelected].Body` into `m.input`

### SPS-5.3 — renderSnippetsPanel implementation

```go
func (m Model) renderSnippetsPanel(width, height int) string {
    if m.snippetFilter != "" {
        m.snippetIndex = filterSnippets(allSnippets, m.snippetFilter)
    }
    if len(m.snippetIndex) == 0 {
        return mutedStyle.Render("(no snippets — ctrl+s to create)")
    }
    lines := []string{}
    for i, s := range m.snippetIndex {
        sel := "  "
        if i == m.snippetSelected {
            sel = "> "
        }
        lines = append(lines, sel+mutedStyle.Render(s.Name))
    }
    lines = append(lines, "", mutedStyle.Render("[enter] insert  [ctrl+s] save"))
    return strings.Join(lines, "\n")
}
```

### SPS-5.4 — Tests

- `loadSnippets` creates default file if absent
- Filter narrows results
- Enter key inserts snippet body

## Course SPS-6: OpenSpec panel [1 SP]

### SPS-6.1 — Data types + refresh

In `internal/tui/openspec_panel.go`:
```go
type openSpecChange struct {
    Name       string
    Done, Total int
    IsActive   bool
}

type openSpecCourse struct {
    ID      string
    Name    string
    Done, Total int
    Tasks   []openSpecTask
}

type openSpecTask struct {
    ID    string  // "NC-15.1"
    Done  bool
}

func (m *Model) refreshOpenSpecData() error {
    // 1. Parse openspec list --json
    out, err := exec.Command("openspec", "list", "--json").Output()
    if err != nil {
        return err  // openspec not installed — panel shows "(openspec not found)"
    }
    var changes []openSpecChange
    if err := json.Unmarshal(out, &changes); err != nil {
        return err
    }
    m.openSpecChanges = changes

    // 2. For active change, parse tasks.md
    for _, c := range changes {
        if c.IsActive {
            tasksPath := "openspec/changes/" + c.Name + "/tasks.md"
            courses, err := parseTasksMD(tasksPath)
            if err == nil {
                m.openSpecCourses = courses
            }
            break
        }
    }
    return nil
}
```

Refresh via `tea.Tick(30*time.Second)` — separate from block tick. Also refresh on panel activation (when `sidePanelIdx == SidePanelOpenSpec`).

### SPS-6.2 — parseTasksMD

```go
func parseTasksMD(path string) ([]openSpecCourse, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    // Regex: ## Course <ID>: <name>\n([ -[\[x\]].*\n)+
    courseRe := regexp.MustCompile(`(?m)^## Course (\S+): (.+)\n((?:  - \[.\] .+\n)*)`)
    taskRe := regexp.MustCompile(`(?m)^  - \[([ x])\] (\S+)`)
    // ...
}
```

### SPS-6.3 — renderOpenSpecPanel implementation

```go
func (m Model) renderOpenSpecPanel(width, height int) string {
    if len(m.openSpecChanges) == 0 {
        return mutedStyle.Render("(openspec not found)")
    }
    if !m.openSpecExpanded {
        // Change-level view
        lines := []string{}
        for i, c := range m.openSpecChanges {
            sel := "  "
            if i == m.openSpecSelected {
                sel = "> "
            }
            bar := progressBar(float64(c.Done)/float64(c.Total), width-25)
            pct := "0%"
            if c.Total > 0 {
                pct = fmt.Sprintf("%d%%", 100*c.Done/c.Total)
            }
            active := ""
            if c.IsActive {
                active = " ★"
            }
            lines = append(lines,
                fmt.Sprintf("%s%s%s  %d/%d %s", sel, c.Name, active, c.Done, c.Total, pct),
            )
        }
        lines = append(lines, "", mutedStyle.Render("[enter] expand  [↑/↓] navigate"))
        return strings.Join(lines, "\n")
    } else {
        // Course-level view for selected change
        lines := []string{fmt.Sprintf("%s — courses", m.openSpecChanges[m.openSpecSelected].Name), ""}
        for i, c := range m.openSpecCourses {
            sel := "  "
            if i == m.openSpecCourseSelected {
                sel = "> "
            }
            bar := progressBar(float64(c.Done)/float64(c.Total), width-20)
            lines = append(lines,
                fmt.Sprintf("%s[%s] %s  %d/%d %s", sel, c.ID, c.Name, c.Done, c.Total, bar),
            )
        }
        lines = append(lines, "", mutedStyle.Render("[b] back  [enter] expand course"))
        return strings.Join(lines, "\n")
    }
}
```

### SPS-6.4 — Interaction key handling

In `app.go` key handling when `m.sidePanelIdx == SidePanelOpenSpec`:
- `↑/↓`: move `m.openSpecSelected` (or `m.openSpecCourseSelected` when expanded)
- `Enter`: toggle `m.openSpecExpanded`; when expanded show courses
- `b`: when expanded, set `m.openSpecExpanded = false`
- `ctrl+o`: global shortcut to jump to OpenSpec panel

### SPS-6.5 — Tests

- `parseTasksMD` correctly counts `- [x]` vs `- [ ]` across 3+ courses
- Course with all tasks done shows 100%
- Empty tasks.md → "(no tasks)"
- `refreshOpenSpecData` handles missing openspec CLI gracefully


---

## Course SPS-7: Diff / changeset panel [1 SP]

### SPS-6.1 — changedFiles collection

On each `blockDoneMsg`, shell out to `git diff --name-only` and store results:
```go
func (m *Model) refreshChangedFiles() error {
    out, err := exec.Command("git", "diff", "--name-only", "HEAD~"+strconv.Itoa(m.activeCount)).Output()
    if err != nil {
        return nil  // not a git repo — silently skip
    }
    m.changedFiles = parseDiffNameOutput(string(out))
    return nil
}
```

Also track untracked files: `git ls-files --others --exclude-standard`.

### SPS-6.2 — renderDiffPanel implementation

```go
func (m Model) renderDiffPanel(width, height int) string {
    if len(m.changedFiles) == 0 {
        return mutedStyle.Render("(no changes in this session)")
    }
    lines := []string{}
    for _, f := range m.changedFiles {
        prefix := "  "
        if f.Selected {
            prefix = "> "
        }
        status := mutedStyle.Render(f.Status)  // M, A, D, ??
        lines = append(lines, prefix+status+"  "+truncateString(f.Path, width-6))
    }
    return strings.Join(lines, "\n")
}
```

### SPS-6.3 — Tests

- Mock `git diff` output → correct parsing of M/A/D/??
- No git repo → "(no changes)"
- Selected file changes on ↑/↓ navigation

---

## Course SPS-8: Multi-model compare panel [1.5 SP]

### SPS-7.1 — Parallel dispatch trigger

In `app.go` key handling, when `Enter` is pressed with shift held (`ctrl+shift+enter` — use `tea.KeyModifiers` to detect):
```go
case tea.KeyEnter:
    if msg.Modifiers == tea.ModShift {
        m.startCompareDispatch(m.input.Value())
        return nil
    }
    // ... normal Enter handling ...
```

`startCompareDispatch` dispatches to ALL available kitchens in parallel using the existing adapter infrastructure — creates one `adapterDispatchCmd` per kitchen, all stored in `m.compareResults[prompt]`.

### SPS-7.2 — compareResult accumulation

Each `blockDoneMsg` with a prompt that matches an in-flight compare gets its output appended to `m.compareResults[prompt]`. Progress tracking: `compareProgress map[string]map[string]float64` — percentage of lines received.

### SPS-7.3 — renderComparePanel implementation

```go
func (m Model) renderComparePanel(width, height int) string {
    if len(m.compareResults) == 0 {
        return mutedStyle.Render("ctrl+shift+enter to compare all kitchens")
    }
    // Show one active compare at a time (most recent)
    var prompt string
    var results []compareResult
    for p, r := range m.compareResults {
        prompt = p
        results = r
    }
    lines := []string{model.RenderPrompt(truncateString(prompt, width-2))}
    for _, r := range results {
        bar := progressBar(r.Percent, width-10)
        icon := "░"
        if r.Done {
            icon = "✓"
        } else if r.Error != "" {
            icon = "✗"
        }
        sel := "  "
        if r.Kitchen == m.compareSelectedKitchen {
            sel = "> "
        }
        lines = append(lines, fmt.Sprintf("%s%s %s %s", sel, icon, KitchenBadge(r.Kitchen), bar))
    }
    return strings.Join(lines, "\n")
}
```

### SPS-7.4 — Tests

- `ctrl+shift+enter` starts compare dispatch
- All available kitchens receive dispatches
- Switching selected kitchen updates preview
- `ctrl+shift+enter` with no prompt → no-op

---

## Course SPS-9: Integration + cleanup [0.5 SP]

- [ ] SPS-9.1 `go test ./internal/tui/...` passes — all panel render tests green
- [ ] SPS-9.2 `go vet ./...` passes
- [ ] SPS-9.3 `go test -race ./internal/tui/...` passes — procStats goroutine access is safe
- [ ] SPS-9.4 Panel cycling keybindings work in all overlay modes (except when input is active)
- [ ] SPS-9.5 Manual: `milliways --tui` → `ctrl+]` cycles through all 10 panels with correct names
- [ ] SPS-9.6 Manual: Cost panel accumulates across 3 dispatches
- [ ] SPS-9.7 Manual: `ctrl+shift+enter` triggers compare mode, all kitchens show
- [ ] SPS-9.8 Manual: OpenSpec panel shows correct change list and course progress

---

## Dependencies between courses

```
SPS-1 (core) must complete before all others
SPS-2 (Cost)          ← depends on SPS-1
SPS-3 (Routing)      ← depends on SPS-1 + Decision.SignalScores
SPS-4 (System)        ← depends on SPS-1
SPS-5 (OpenSpec)     ← depends on SPS-1
SPS-6 (Snippets)     ← depends on SPS-1
SPS-7 (Diff)         ← depends on SPS-1
SPS-8 (Compare)       ← depends on SPS-1
SPS-9 (Integration)   ← depends on all above
```

---

## 🍽️ **Service check** — Palate cleanser

The sidebar is alive. `ctrl+]` takes you from Ledger → Jobs → Cost → Routing → System → Snippets → Diff → Compare → Ledger. Every panel tells you something true about the state of your session. The right edge of the screen is now a control room.
