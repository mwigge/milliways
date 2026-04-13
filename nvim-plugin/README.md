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
