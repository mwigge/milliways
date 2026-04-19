# milliways-nvim-context — L2 Context Hydration for the Nvim Plugin

> The maitre d' already knows your table. Now let them know what you were reading, where your cursor was, and what the compiler is complaining about — before you even finish the sentence.

## Positioning

Under the maitre d' umbrella established in `milliways-kitchen-parity`: milliways is a terminal-first, keyboard-driven router that seats tasks at the right kitchen and preserves shared memory across them. The bubble-tea TUI is the canonical UX, living as a tmux pane beside nvim. This change is the *other* terminal entry point — the nvim plugin — growing a layer of nvim-native awareness.

## Why

Today's `milliways.nvim` (≈280 LOC Lua) is a **thin command wrapper**. It spawns the binary, renders output in a floating window, passes `--context-file` for the current file path, and pastes a visual selection into the prompt. That's the entire extent of its editor awareness.

Nvim users accrue rich context just by working: LSP diagnostics, treesitter-parsed scopes, git status, cursor position, visible range, open buffers, project root, quickfix contents. None of it reaches milliways today. The user compensates by manually explaining *"I'm in this function, getting these errors, the branch is dirty because…"* — which is exactly the kind of context-assembly the sommelier should be doing for them.

L2 closes this gap. The plugin collects nvim-native context automatically and ships it to milliways as a structured bundle. The sommelier uses it for routing. The kitchens receive it as part of the continuation payload.

Why now: `milliways-kitchen-parity` introduces the shared-memory substrate (MemPalace fork). The nvim plugin needs to read and write conversation state from that substrate; otherwise it's going to be rebuilt when the substrate lands anyway. L2 is best built *after* kitchen-parity Service 1 and before the TUI reaches feature completeness, so the two surfaces stay in step.

## What Changes

### Structured context bundle

The plugin collects, on every dispatch:

- **Buffer state**: absolute path, filetype, modified flag, total lines, visible range (top/bottom of window).
- **Cursor position**: line + column, treesitter-named scope (function/class/block).
- **Visual selection**: if one exists, with file-relative line numbers (not just text).
- **LSP diagnostics**: for the current buffer, filtered by severity, within the visible range by default or file-wide on request.
- **Git state**: branch, dirty flag, files changed, ahead/behind counts against upstream.
- **Project metadata**: detected project root, primary language, list of open buffers, recent files.
- **Editor state**: current quickfix entries, current loclist entries (when the user invokes a quickfix-aware command).

This bundle is shipped to milliways as JSON — either through a new `--context-json` flag or on stdin with `--context-stdin`.

### Milliways-side: structured context ingestion

The milliways binary learns to accept the structured bundle:

- `--context-json '<json>'` for small payloads.
- `--context-stdin` for any size.
- Parsed into a typed `EditorContext` struct available to the sommelier and the continuation payload builder.
- Existing `--context-file` stays (it's still used by non-plugin callers) but is demoted to a subset of the structured bundle.

Sommelier uses the bundle for tier-2 pantry signals:

- LSP error density → prefer a debugging-oriented kitchen.
- Treesitter scope name ("test_*", "spec_*") → prefer test-oriented routing.
- Git dirty flag → maybe favour a kitchen the user has already trusted for similar churn.

### Nvim-side command parity with TUI

Kitchen-parity adds `/switch`, `/stick`, `/back` as TUI commands. This change ships the nvim-plugin equivalents:

- `:MilliwaysSwitch <kitchen>` — mid-conversation kitchen switch.
- `:MilliwaysStick` — pin the current kitchen, disable auto-switching.
- `:MilliwaysBack` — reverse the most recent switch.
- `:MilliwaysKitchens` — fuzzy-pick a kitchen (via Telescope when present, `vim.ui.select` otherwise).

Both surfaces talk to the same MemPalace substrate via MCP; switches made in nvim are visible to anyone reading the substrate elsewhere.

### Floating-window UX polish

Small quality-of-life changes to the existing float:

- Streaming output line-by-line instead of buffered-until-exit.
- A segment/provider-lineage header line (`claude → codex`) matching kitchen-parity's TUI indicators.
- `<Tab>` to cycle recent conversations.
- `<CR>` on a kitchen name in `:MilliwaysKitchens` output to switch to it.

Full block-oriented UX (splits, multi-buffer transcripts, dedicated process-map buffer) remains **out of scope** — that's L3 territory and would be a separate change.

## Capabilities

### New Capabilities

- `editor-context-bundle`: structured editor-state payload format consumed by milliways.
- `nvim-context-collector`: Lua collectors producing the bundle from nvim state.
- `nvim-switch-commands`: nvim-plugin equivalents of kitchen-parity's TUI switch commands.

### Modified Capabilities

- `sommelier-routing`: tier-2 pantry signals gain editor-context awareness.
- `continuation-payload`: continuation builder includes editor context in the payload given to a kitchen.

## Non-Goals

- **Replacing the bubble-tea TUI.** The nvim plugin remains one of two first-class surfaces. Feature parity is maintained; dominance is not.
- **L3 block UX in nvim.** No split-based or multi-buffer conversation layout. A separate future change.
- **Collab via `live-share.nvim`.** Delegated to the ecosystem; milliways does not bundle or require it.
- **Native Avante / Agentic.nvim integration.** Out of scope. Those are peer tools; interoperability via MemPalace MCP is the only anticipated integration.
- **Hosted sync, subscription management, plugin marketplace.** Never — same discipline as kitchen-parity.

## Impact

### New Packages

- `internal/editorcontext/` — typed `EditorContext` struct, JSON codec, integration points for sommelier and continuation builder.

### Modified Packages

- `cmd/milliways/` — `--context-json` and `--context-stdin` flags, context ingestion.
- `internal/sommelier/` — tier-2 pantry signal evaluator gains editor-context inputs.
- `internal/conversation/continue.go` — continuation payload embeds editor context.
- `nvim-plugin/lua/milliways/` — grows from one file to a small module (`init.lua`, `context.lua`, `commands.lua`, `float.lua`, `kitchens.lua`).

### New Test Assets

- `nvim-plugin/tests/` — plenary.nvim spec files for the context collectors.
- `testdata/smoke/scenarios/nvim-context.sh` — end-to-end scenario that invokes the milliways binary with a representative JSON bundle and asserts routing picks up the signals.

## Dependencies

- **`milliways-kitchen-parity` Service 1 (MemPalace fork + substrate)** must be landed first. The plugin reads and writes conversation state via MemPalace MCP; without the substrate it would be built against the legacy SQLite store and redone later.
- **`milliways-kitchen-parity` Service 3 (user-initiated switch)** must be landed for the nvim command parity capability to have something to parallel. The TUI commands land first; the nvim commands follow.

This change does NOT block kitchen-parity. It is designed to *follow* kitchen-parity.

## Success Criteria

1. A dispatch from the nvim plugin automatically includes LSP diagnostics, git status, treesitter scope, and buffer state — without the user pasting anything.
2. The sommelier's routing decision visibly changes based on editor context (e.g., LSP errors → debugging-oriented kitchen preferred).
3. `:MilliwaysSwitch codex` in nvim mid-conversation produces the same substrate-level switch that `/switch codex` produces in the TUI — readable from both surfaces and from a second milliways instance.
4. `plenary.nvim` spec coverage for context collectors runs green in CI (headless nvim run).
5. Existing users of the current thin plugin see no regression — all current commands keep working with the same keybindings.
