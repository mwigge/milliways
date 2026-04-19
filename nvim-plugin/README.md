# milliways.nvim

Neovim plugin for [Milliways](https://github.com/mwigge/milliways) — route AI coding tasks to the best CLI tool.

## Requirements

- Neovim 0.10+
- `milliways` binary in PATH

## Install

### lazy.nvim

```lua
{
  "mwigge/milliways",
  config = function()
    require("milliways").setup({
      bin = "milliways",       -- path to binary
      keybindings = true,      -- register default keybindings
      leader = "<leader>m",    -- keybinding prefix
      float_width = 0.8,       -- floating window width (0-1)
      float_height = 0.8,      -- floating window height (0-1)
    })
  end,
}
```

## Commands

| Command | Description |
|---------|-------------|
| `:Milliways {prompt}` | Route task to best kitchen, show result in floating window |
| `:MilliwaysExplain {prompt}` | Show routing decision without executing |
| `:MilliwaysKitchen {prompt}` | Pick a kitchen, then dispatch |
| `:MilliwaysRecipe {prompt}` | Pick a recipe, then dispatch multi-course |
| `:MilliwaysStatus` | Show kitchen availability |
| `:MilliwaysDetached` | List async/detached dispatches |
| `:MilliwaysSwitch [kitchen]` | Switch active kitchen. Without args opens picker |
| `:MilliwaysStick [kitchen]` | Pin a kitchen as sticky — all subsequent dispatches use it until unstuck |
| `:MilliwaysBack` | Return to previous kitchen after a switch |
| `:MilliwaysReroute` | Force sommelier re-evaluation on the current conversation |
| `:MilliwaysKitchens` | Open kitchen picker with Telescope (if installed) or `vim.ui.select` |

## Keybindings

Default keybindings (with `<leader>m` prefix):

| Key | Mode | Action |
|-----|------|--------|
| `<leader>mm` | Normal | Open prompt, dispatch |
| `<leader>mm` | Visual | Dispatch with selection as context |
| `<leader>me` | Normal | Explain routing |
| `<leader>ms` | Normal | Kitchen status |
| `<leader>mr` | Normal | Pick recipe |
| `<leader>mk` | Normal | Pick kitchen |
| `<leader>mK` | Normal | Open kitchen picker with Telescope (if installed) or `vim.ui.select` |
| `<leader>m.` | Normal | Reroute current conversation |

## Floating Window

Output appears in a floating window with:

| Key | Action |
|-----|--------|
| `q` | Close window |
| `y` | Yank output to clipboard |

## Context Injection

The plugin automatically passes context from your editor:

- **Current file** → `--context-file` flag
- **Visual selection** → injected as code block in prompt

## L2 Context Hydration

The plugin collects rich editor context and passes it to milliways via `--context-stdin` for sommelier routing decisions. Context is collected automatically on every dispatch and includes:

### Collectors

| Collector | What it gathers | Opt-out |
|-----------|----------------|---------|
| `buffer` | file path, filetype, modified flag, total lines, visible range | `opts.context_collectors = false` |
| `cursor` | line, column, treesitter scope (function/class/block) | `opts.context_collectors = false` |
| `selection` | start/end lines + text (visual mode only) | always opt-in |
| `project` | git root, primary language, open buffers, recent files | `opts.context_collectors = false` |
| `lsp` | diagnostics filtered by severity | `opts.context_collectors = false` |
| `git` | branch, dirty flag, files changed, ahead/behind | `opts.context_collectors = false` |
| `quickfix` | quickfix list entries | `opts.context_collectors = false` |
| `loclist` | location list entries | `opts.context_collectors = false` |

### Budget

Each collector has a 15ms timeout. The total bundle is capped at 64 KB and 50ms wall clock. Graceful degradation: absent LSP or non-git directory returns nil — routing continues without that signal.

### Configuration

```lua
require("milliways").setup({
  -- L2 context
  context_collectors = true,   -- enable/disable all collectors
  context_timeout_ms = 15,    -- per-collector timeout
  context_budget_kb = 64,     -- total bundle size cap
})
```

## Privacy

The editor context bundle is sent to milliways alongside your prompt. It is:
- **Not stored permanently** — sent per-dispatch, used for routing only
- **Not shared with third parties** — goes only to the kitchen (LLM provider) you dispatched to
- **Subject to your kitchen's data policy** — check your provider's terms

### What is collected

The bundle includes: file paths, git branch, LSP diagnostics (errors/warnings by severity), cursor position, and (in visual mode) selected text. No file contents beyond the selection.

### Opting out

Set `context_collectors = false` in setup to disable all collectors. The bundle will be empty and routing falls back to prompt-only heuristics.

## Troubleshooting

### LSP diagnostics not appearing in context
- Ensure `vim.lsp.start()` has been called for the buffer's filetype before dispatching
- Check `:Mold` or `:LspInfo` to confirm the client is attached
- The collector returns nil gracefully if no LSP client is active — this is normal

### Git state is absent or wrong
- Context requires being inside a git repository (`git rev-parse --is-inside-work-tree`)
- Check that `git` is on PATH and the working directory is a repo root or subdirectory
- Dirty/ahead/behind counts shell out to `git status` and `git rev-list` — these can be slow in large repos

### Telescope picker not appearing for `:MilliwaysKitchens`
- Confirm `telescope.nvim` is installed and loaded: `require('telescope').load_extension()` should not error
- The plugin detects Telescope at runtime — no explicit configuration needed
- Falls back to `vim.ui.select` automatically

### Floating window does not autoscroll
- If you move the cursor inside the float window, autoscroll pauses
- Press `q` to close and reopen, or use `:MilliwaysReroute` to reset
- `<leader>m.` (dot) is the default reroute keybinding

### `milliways` binary not found
- Ensure the `milliways` binary is on your PATH: `which milliways`
- Or set the path explicitly: `require("milliways").setup({ bin = "/path/to/milliways" })`
