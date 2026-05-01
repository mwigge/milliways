# Design — milliways-nvim-context

## D1: Editor-context bundle shape

The shared language between the nvim plugin and milliways is a JSON document.

```json
{
  "schema_version": "1",
  "captured_at": "2026-04-18T12:34:56Z",
  "buffer": {
    "path": "/abs/path/to/file.go",
    "filetype": "go",
    "modified": true,
    "total_lines": 382,
    "visible_range": { "top": 340, "bottom": 382 }
  },
  "cursor": {
    "line": 382,
    "column": 4,
    "scope": { "kind": "function", "name": "runSmoke" }
  },
  "selection": {
    "start_line": 370,
    "end_line": 380,
    "text": "..."
  },
  "lsp_diagnostics": [
    {
      "line": 382,
      "column": 4,
      "severity": "error",
      "source": "gopls",
      "message": "cancel declared but not used"
    }
  ],
  "git": {
    "branch": "chore/provider-continuity-closeout",
    "dirty": true,
    "files_changed": 3,
    "ahead": 0,
    "behind": 0
  },
  "project": {
    "root": "/abs/path/to/repo",
    "primary_language": "go",
    "open_buffers": ["/path/a.go", "/path/b.go"],
    "recent_files": ["/path/c.go"]
  },
  "quickfix": null,
  "loclist": null
}
```

`schema_version` is mandatory. Additive changes increment the minor; breaking changes increment the major and the Go side rejects unknown major versions with a helpful error.

Null or absent fields are allowed — the sommelier treats missing data as "no signal," not "empty signal." A bare-minimum bundle might only have `buffer.path`.

## D2: Transport

Two flags, depending on payload size:

```
milliways --context-json '<json>' "prompt"     # small payloads (<4kb)
echo '<json>' | milliways --context-stdin "prompt"
```

Rule of thumb: the plugin uses `--context-stdin` unconditionally. Humans typing prompts on the CLI use the file-based `--context-file` (unchanged). `--context-json` exists mostly for scripting and tests.

The existing `--context-file` stays as a convenience and is reconstructed into a minimal bundle server-side.

## D3: Lua collectors

The plugin grows from one file (`init.lua`) to a small module:

```
nvim-plugin/lua/milliways/
├── init.lua        # setup + user commands (refactored)
├── context.lua     # bundle builder + individual collectors
├── commands.lua    # :MilliwaysSwitch / :MilliwaysStick / :MilliwaysBack
├── float.lua       # floating window (extracted from init.lua)
└── kitchens.lua    # :MilliwaysKitchens picker, Telescope integration
```

### D3.1 Collector signatures

```lua
local Context = {}

function Context.build(opts)
  opts = opts or {}
  return {
    schema_version = "1",
    captured_at    = os.date("!%Y-%m-%dT%H:%M:%SZ"),
    buffer         = Context.collect_buffer(),
    cursor         = Context.collect_cursor(),
    selection      = opts.include_selection and Context.collect_selection() or nil,
    lsp_diagnostics = Context.collect_lsp(opts.lsp_scope or "visible"),
    git            = Context.collect_git(),
    project        = Context.collect_project(),
    quickfix       = opts.include_quickfix and Context.collect_quickfix() or nil,
    loclist        = opts.include_loclist and Context.collect_loclist() or nil,
  }
end
```

### D3.2 Graceful degradation

Every collector has a try/catch boundary — a missing LSP server, a repo outside git, a treesitter parser not installed — all resolve to `nil` without breaking the dispatch. Each silent skip is logged via `vim.notify` at `DEBUG` level only.

### D3.3 Performance budget

Total collection must stay under **50ms** on a typical project (< 10k LOC, LSP warm). LSP diagnostics read is the slowest piece; the plugin restricts to the visible range by default and switches to file-wide only when the user opts in (`:MilliwaysContext full`).

No background workers, no persistent state in the plugin. Each dispatch rebuilds from scratch.

## D4: Go-side ingestion

### D4.1 Typed struct

```go
package editorcontext

type Bundle struct {
    SchemaVersion string           `json:"schema_version"`
    CapturedAt    time.Time        `json:"captured_at"`
    Buffer        *BufferState     `json:"buffer,omitempty"`
    Cursor        *CursorState     `json:"cursor,omitempty"`
    Selection     *Selection       `json:"selection,omitempty"`
    LSPDiagnostics []Diagnostic    `json:"lsp_diagnostics,omitempty"`
    Git           *GitState        `json:"git,omitempty"`
    Project       *ProjectMetadata `json:"project,omitempty"`
    Quickfix      []QuickfixEntry  `json:"quickfix,omitempty"`
    Loclist       []LoclistEntry   `json:"loclist,omitempty"`
}
```

### D4.2 Sommelier integration

The sommelier gains an `editorcontext.Bundle` input at the pantry-signals tier. Example signal derivations:

| Signal | Derivation |
|---|---|
| `editor.lsp_error_count` | `len(bundle.LSPDiagnostics)` filtered to severity=error |
| `editor.in_test_file` | `bundle.Cursor.Scope.Name` starts with `test_` or file ends with `_test.go` |
| `editor.dirty_churn` | `bundle.Git.FilesChanged` count |
| `editor.language` | `bundle.Project.PrimaryLanguage` or `bundle.Buffer.Filetype` |

Signals feed the existing tier-2 pantry evaluator and can be weighted per kitchen in `carte.yaml`:

```yaml
routing:
  kitchens:
    claude:
      weight_on:
        editor.lsp_error_count_gt_3: +0.15
    codex:
      weight_on:
        editor.in_test_file: +0.20
```

### D4.3 Continuation payload

The continuation-payload builder (from `milliways-kitchen-parity`) is extended to optionally include a condensed editor-context section:

```
Editor context:
- File: internal/tui/app.go (go, modified)
- Cursor: line 382, in function runSmoke
- LSP errors (3): "cancel declared but not used" at line 382 ...
- Git: branch chore/… (dirty, 3 files changed)
```

Condensed to stay under 500 tokens. Raw LSP messages truncated to their first line.

## D5: Command parity with TUI

Kitchen-parity's TUI commands become nvim commands with the same semantics:

| TUI command | Nvim command | Notes |
|---|---|---|
| `/switch <kitchen>` | `:MilliwaysSwitch <kitchen>` | Tab-completion on kitchen names via `complete=customlist` |
| `/stick` | `:MilliwaysStick` | Displays `[stuck: claude]` in the float header when active |
| `/back` | `:MilliwaysBack` | No-op with notice if no prior switch |
| `/kitchens` | `:MilliwaysKitchens` | Telescope picker if Telescope installed, `vim.ui.select` otherwise |
| `/reroute` | `:MilliwaysReroute` | Forces sommelier re-evaluation on current conversation |

Keybindings under `<leader>m`:

```lua
vim.keymap.set("n", "<leader>ms", ":MilliwaysSwitch ",    { desc = "Switch kitchen" })
vim.keymap.set("n", "<leader>m.", ":MilliwaysStick<CR>",  { desc = "Stick kitchen" })
vim.keymap.set("n", "<leader>m,", ":MilliwaysBack<CR>",   { desc = "Back (undo switch)" })
vim.keymap.set("n", "<leader>mK", ":MilliwaysKitchens<CR>", { desc = "Pick kitchen" })
```

Existing `<leader>mm` / `<leader>me` / etc. bindings unchanged.

## D6: Dependency on MemPalace substrate

Every operation that touches conversation state goes through the MemPalace MCP client (`internal/substrate/`), not direct SQLite:

- Dispatch: read current conversation via `mempalace_conversation_get`, start new segment via `mempalace_conversation_start_segment`, stream turns via `mempalace_conversation_append_turn`.
- Switch: end current segment, start new segment, emit `runtime_event` with kind=`switch`.
- Resume (future): list conversations via `mempalace_conversation_list`, pick one, restore.

If the substrate isn't configured, the nvim plugin falls back to calling milliways with the current `--session` semantics. This keeps existing users unaffected even if they haven't adopted kitchen-parity yet.

## D7: UX polish

Three small improvements to the floating window, landed together because they're cheap:

### D7.1 Line-by-line streaming

Switch `jobstart` from `stdout_buffered = true` to per-line streaming. Each stdout line appended to the float buffer as it arrives. Autoscroll to bottom unless the user has moved the cursor up.

### D7.2 Lineage header

First line of the float shows the current provider lineage:

```
claude → codex  |  sticky  |  <Tab> recent  <leader>mK kitchens
```

Updated in place on every segment change.

### D7.3 `<Tab>` for recent conversations

Pressing `<Tab>` inside the float cycles through the three most recent conversations in MemPalace, previewing each. Selection with `<CR>` resumes that conversation in the float.

## D8: Risks

| Risk | Mitigation |
|---|---|
| Collection latency balloons to >50ms on large projects | Per-collector timeouts; LSP restricted to visible range by default; context size capped at 64kb of JSON |
| LSP not installed for user's language | Collector returns `nil`; sommelier treats as no-signal; no warning noise |
| JSON bundle leaks sensitive content (tokens, keys) from buffer | Document: current-buffer text is only sent if the user explicitly invokes a buffer-including command. Selection and LSP metadata are the defaults. Never log the full bundle. |
| plenary.nvim tests are flaky in CI headless nvim | Pin nvim version in CI; run specs with `--headless -l` convention; start small (5 specs) and grow |
| Schema drift between plugin and binary | `schema_version` field; binary rejects unknown major versions with actionable error |

## D9: Open questions

- **Should the context bundle include treesitter AST snippets or just scope names?** Leaning scope-names-only for this change; AST snippets are fatter and the kitchen can re-parse.
- **How much of the bundle enters the continuation payload vs. staying sommelier-only?** Draft answer: sommelier sees the whole bundle; continuation payload gets a condensed summary (D4.3). Measure and tune.
- **Is there a "quiet mode" where collection runs but doesn't transmit?** For measurement / privacy review. Probably yes; keep the flag out of v1.
