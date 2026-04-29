Wezterm integration example

Place milliways.lua in your ~/.wezterm.lua (or require it) to enable a
simple status bar that starts a background `milliwaysctl status --watch`
sidecar and reads the atomic `${state}/status.cur` file produced by the
sidecar.

Steps:
1. Copy cmd/milliwaysctl/milliways.lua into your wezterm config or require it.
2. Ensure milliwaysctl is on PATH and milliwaysd is running (socket in XDG_RUNTIME_DIR or ~/.local/state/milliways/sock).
3. Restart wezterm. The sidecar is started lazily on the first update-status event.

Notes and best practices:
- The sidecar writes state/status.cur atomically (tmp+fsync+rename). The
  Lua config reads it for display. For older systems without XDG_RUNTIME_DIR
  set, the default state path is ~/.local/state/milliways.
- The example is intentionally small — production usage should check that
  status.cur mtime is recent and fall back to a direct RPC call otherwise.
- The watch sidecar ensures it exits if the parent process dies (ppid==1
  heuristic). For stricter lifecycle, consider PDEATHSIG on Linux.

Command palette (Leader + /)
============================

`Leader + /` (Ctrl+Space then `/`) opens a `milliwaysctl` command palette
implemented as a hybrid wezterm `InputSelector` → `PromptInputLine` flow.
Adding a new ctl subcommand keeps it callable via the free-form escape
hatch; updating the curated `ctl_choices` list in `milliways.lua` makes it
discoverable in the picker.

Flow:
1. The picker opens with a curated list of common ctl invocations and
   `fuzzy=true`. Type to filter.
2. Pick a complete verb (e.g., `agents`, `local list-models`) → it
   dispatches immediately in a new tab.
3. Pick a verb that takes args (e.g., `local switch-server …`) → a
   `PromptInputLine` opens prefilled with that verb so you fill in the rest.
4. Pick `… free-form milliwaysctl invocation …` → an empty
   `PromptInputLine` opens for arbitrary `<verb> [args...]` input.
5. Esc at either stage cancels.

Examples (assuming milliwaysctl is on PATH):
- Pick `local list-models` from the list, Enter → new tab with model IDs.
- Pick `local setup-model …`, Enter → prompt with `local setup-model `
  prefilled; type `unsloth/Qwen2.5-Coder-7B-Instruct-GGUF`, Enter.
- Pick free-form, type `local download-model unsloth/Qwen2.5-Coder-1.5B-Instruct-GGUF --quant Q4_K_M --force`, Enter.

Free-form input also accepts a leading `/` for muscle-memory parity
(`/local list-models` is equivalent to `local list-models`).

Limitations:
- Whitespace-only splitting in the prompt; arguments containing spaces are
  not expressible from this dispatcher (run `milliwaysctl <verb>` directly
  in any tab if you need quoting).
- The curated `ctl_choices` list must be edited to surface a new verb in
  the picker; the free-form escape hatch covers everything else without
  Lua changes.

Smoke test:
1. Build & install: `go install ./cmd/milliwaysctl/`.
2. Load this Lua in your wezterm config (or run wezterm with
   `--config-file <path>/milliways.lua`).
3. Press Ctrl+Space then `/`. The palette appears.
4. Type `local` to filter. Pick `local list-models`. Press Enter.
5. A new tab opens running `milliwaysctl local list-models`. If the local
   backend is not running, the tab shows a non-zero exit and a hint to run
   `milliwaysctl local install-server`.
