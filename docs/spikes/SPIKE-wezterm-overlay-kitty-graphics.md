# SPIKE: wezterm overlay surface + kitty graphics protocol

**Status**: NOT YET RUN
**Owner**: _unassigned_
**Blocks**: Phase 5 (`/context` cockpit) of the `milliways-emulator-fork` change.

## Quick start

Three commands to reproduce the spike on a fresh macOS dev machine:

```bash
brew install --cask wezterm
export WEZTERM_CONFIG_FILE="$(mktemp -d)/wezterm.lua" && \
  cp docs/spikes/spike_assets/spike-wezterm-overlay.lua "$WEZTERM_CONFIG_FILE" && \
  docs/spikes/spike_assets/make-test-png.sh > /tmp/spike-test.png
wezterm
```

On Linux replace step 1 with `cargo install --locked --git https://github.com/wez/wezterm wezterm-gui` (or your distro's package). Once wezterm is open, jump straight to "Test procedure" below.

## The question

Does **wezterm's overlay rendering surface** (the surface used by `CommandPalette`, `LaunchMenu`, `CharSelect`, etc.) consume **kitty graphics protocol** escape sequences (`ESC _ G ... ESC \`) the same way regular panes do?

Wezterm renders kitty graphics in panes — that's well-known. But the overlay surface is a separate render path that traditionally has been text-only. If overlays do not render kitty graphics, the `/context` design (Decision 4) cannot use overlays — it must use a real pane (a tab) instead. The visual contract (kitty-graphics donut + sparkline + line chart) is preserved either way; only the surface changes.

## Why we run it now

Cheap to answer. ~30 minutes against upstream wezterm. If we fork first and discover the answer in Phase 5, we waste weeks of overlay implementation and have to redesign the keybinding semantics (overlay→tab is not free). Spike now, fork later.

## Setup (no fork required)

```bash
# 1. Install upstream wezterm (skip if you already have it):
brew install --cask wezterm   # macOS
# or: cargo install --locked --git https://github.com/wez/wezterm wezterm-gui

wezterm --version              # confirm it runs

# 2. Make a temp config dir we won't pollute the user's setup with:
export WEZTERM_CONFIG_FILE="$(mktemp -d)/wezterm.lua"
cp docs/spikes/spike_assets/spike-wezterm-overlay.lua "$WEZTERM_CONFIG_FILE"

# 3. Generate a known PNG payload:
docs/spikes/spike_assets/make-test-png.sh > /tmp/spike-test.png
echo "test PNG: /tmp/spike-test.png  ($(wc -c < /tmp/spike-test.png) bytes)"

# 4. Launch wezterm with the spike config:
wezterm
```

## Test procedure

Once wezterm is open with the spike config loaded, run each of the four tests and record the outcome.

### Test 1 — kitty graphics in a regular pane (control)

Type at the shell prompt:

```bash
docs/spikes/spike_assets/emit-kitty-graphics.sh /tmp/spike-test.png
```

Expected: the test PNG renders inline above the next prompt. This confirms wezterm has working kitty-graphics support in your build. If this fails, fix wezterm before proceeding.

#### What to look for

- **PASS** — the coloured square from `make-test-png.sh` appears above the next shell prompt at roughly its native pixel size. Cursor advances *below* the image.
- **PARTIAL** — image renders but with garbage rows, wrong colour, or the cursor position is wrong (e.g., overlaps the image).
- **FAIL** — terminal prints raw `\x1b_G...` escape characters as text, or the prompt comes back with no image.

### Test 2 — kitty graphics in CommandPalette overlay

Press `Ctrl+Shift+P` to open `CommandPalette`. Wezterm's `CommandPalette` is the canonical overlay surface; if any overlay renders kitty graphics, this one will. The spike config replaces a few command entries with names containing the kitty-graphics escape (see `docs/spikes/spike_assets/spike-wezterm-overlay.lua`).

Expected outcomes:
- **PASS** — the test PNG renders inside the palette dropdown alongside the entry text.
- **PARTIAL** — the PNG renders but with redraw flicker / no caching / size weirdness. Note the symptom.
- **FAIL** — the entry name shows literal escape characters or is mangled. No image.

#### What to look for

- **PASS** confirmation: the coloured square is visible *inside* the palette's dropdown row, not behind it or outside the overlay's clip region. Scroll up/down with arrow keys — the image must clip properly when the row scrolls offscreen.
- **PARTIAL** indicators: image redraws on every keystroke (flicker), image is rendered at the wrong size, image leaks outside the overlay's bounds, or it disappears when the palette filters change.
- **FAIL** indicators: the entry's label shows characters like `_Gf=32,a=T,...` (raw escape leaked into the text rendering pipeline), the palette glitches and refuses to open, or the entry text is replaced by a blank line.

### Test 3 — kitty graphics in a custom overlay pane

The spike config exposes a custom command `milliways-spike-overlay` that opens a tab-floating overlay using `wezterm.action.PromptInputLine` / `InputSelector`. Trigger it via the palette or `Ctrl+Shift+M`. The overlay's prompt label is constructed to contain a kitty-graphics escape.

Same outcomes as Test 2.

#### What to look for

- **PASS** — same image rendering as Test 2, this time inside an `InputSelector` (a different overlay code path). If Test 2 PASSed but this one PARTIALs, suspect the `InputSelector` render path specifically.
- **FAIL** indicator unique to this test: the prompt label shows literal escape characters but Test 2's palette did not. That tells us `CommandPalette` and `InputSelector` use different render paths and only one supports kitty graphics.

### Test 4 — recovery on overlay close

If Tests 2 or 3 PASS, close the overlay (Esc) and re-open it. Does the cached image still render, or does each open re-upload? Note answer for the design's "data_hash invalidation" budget.

#### What to look for

- **PASS** — re-opening the overlay shows the image instantly with no perceptible re-upload latency. Wezterm is caching by `data_hash`.
- **PARTIAL** — there is a brief delay or a visible flash on each open (image re-uploaded). Acceptable but means the cockpit must batch its re-emits, not rely on free caching.
- **FAIL** — image is missing on the second open until something forces a redraw. Cockpit code MUST emit a fresh kitty-graphics frame on every overlay open.

## Recording the outcome

Fill in this section after running the tests. Replace the `_TBD_` markers.

### Outcome

- Date run: _TBD_
- wezterm version: _TBD_  (`wezterm --version`)
- macOS / Linux: _TBD_

| Test | Outcome (PASS / PARTIAL / FAIL) | Notes |
|------|--------------------------------|-------|
| 1 — pane control                | _TBD_ |       |
| 2 — CommandPalette              | _TBD_ |       |
| 3 — custom overlay              | _TBD_ |       |
| 4 — re-open caching             | _TBD_ |       |

Screenshots: attach to `docs/spikes/screenshots/spike-overlay-{1,2,3,4}.png`.

### Verdict

- **PASS** (Tests 2 and 3 both render the image): Decision 4 holds. `/context` proceeds as a wezterm overlay. No follow-on changes needed.
- **PARTIAL** (renders but with caveats): document the caveats in the proposal. Most likely accommodation: re-emit on each open instead of relying on wezterm's image cache.
- **FAIL** (overlay surface does not render kitty graphics): `/context` becomes a real pane (tab) under reserved id `_context`. Follow-on:
  - Edit `openspec/changes/milliways-emulator-fork/specs/context-cockpit/spec.md` — replace "wezterm overlay" with "wezterm pane" in the `Cmd+Shift+C` requirement.
  - Edit `design.md` Decision 4 — "spike-blocked decision" branch resolved to FAIL.
  - Edit `tasks.md` TASK-5.3 — change "Implement a wezterm overlay" to "Implement a wezterm pane backed by AgentDomain reservation `_context`".
  - Esc to close becomes "close tab" rather than "close overlay".

## Spike assets

Companion files to be created alongside this runbook:

- `spike_assets/spike-wezterm-overlay.lua` — minimal wezterm config with palette entries and a custom overlay action.
- `spike_assets/make-test-png.sh` — generates a 200x100 PNG with a recognisable shape (e.g., a coloured square).
- `spike_assets/emit-kitty-graphics.sh` — base64-encodes a PNG file and emits the kitty graphics protocol escape sequence to stdout.

## EXAMPLE — replace before sign-off

The block below is a sample filled-in outcome to give the runner a template. Copy it over the "Outcome" section above when you record real results — and **delete this EXAMPLE block before sign-off**.

### Outcome (EXAMPLE — replace before sign-off)

- Date run: 2026-04-15
- wezterm version: 20240203-110809-5046fc22 (`wezterm --version`)
- macOS / Linux: macOS 14.4 (Apple Silicon)

| Test | Outcome (PASS / PARTIAL / FAIL) | Notes |
|------|--------------------------------|-------|
| 1 — pane control                | PASS    | image renders inline, cursor advances correctly. |
| 2 — CommandPalette              | FAIL    | entry label shows raw `_Gf=32,...` escape characters. |
| 3 — custom overlay              | FAIL    | `InputSelector` prompt also leaks escape; same render path. |
| 4 — re-open caching             | n/a     | Tests 2 & 3 FAILed, caching not exercised. |

### Verdict (EXAMPLE)

FAIL. `/context` cockpit must use a real pane (tab) under reserved id `_context`, not an overlay. Following follow-on edits filed in `openspec/changes/milliways-emulator-fork/` per the FAIL branch above.

## References

- Kitty graphics protocol spec: https://sw.kovidgoyal.net/kitty/graphics-protocol/
- Wezterm overlay system: `wezterm-gui/src/overlay/` in upstream.
- Wezterm kitty graphics support: search for `KittyImage` in upstream.
