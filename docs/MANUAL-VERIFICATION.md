# Manual Verification Guide

This guide covers the remaining manual checks for milliways kitchen parity.

## Prerequisites

1. Build the CLI: `go build -o ./bin/milliways ./cmd/milliways`
2. Start the MemPalace fork MCP server with a writable palace.
3. Export substrate env vars:
   - `MILLIWAYS_MEMPALACE_MCP_CMD`
   - `MILLIWAYS_MEMPALACE_MCP_ARGS`
4. Ensure at least two kitchens are authenticated and ready.

## KP-22.1 — TUI `/switch`, `/stick`, `/back`, `/kitchens`

1. Run `./bin/milliways`
2. Submit a prompt that routes to `claude`.
3. Type `/kitchens` and confirm the kitchen list shows availability plus the current kitchen.
4. Type `/switch codex` (or another ready kitchen).
5. Confirm a system line appears with `switch: claude -> codex` and `Use /back to return`.
6. Type `/stick`.
7. Confirm a system line says sticky mode is enabled for the current kitchen.
8. Type `/back`.
9. Confirm the conversation switches back to the previous kitchen and another reversal hint is shown.
10. Type `/stick` again and confirm sticky mode turns off.

## KP-22.2 — Auto-switch on “search the web for X”

1. Start a fresh TUI session.
2. Submit a prompt such as `search the web for the latest Go 1.24 release notes`.
3. Confirm routing starts in the default kitchen, then auto-switches to the search-oriented kitchen.
4. Confirm the process map/runtime activity includes a `switch` event with a hard-signal reason.
5. Confirm the rendered transcript includes the visible `/back` reversal hint.

## KP-22.3 — Headless `--switch-to`

1. Create or resume a paused session named `switch-demo`.
2. Run:
   `./bin/milliways --session switch-demo --switch-to codex "continue"`
3. In `--verbose` mode, confirm stderr prints the switch notice.
4. Confirm the resumed conversation continues in the requested kitchen rather than starting a new conversation.
5. Re-open the session in the TUI and verify the segment history shows the headless switch.

## KP-22.4 — Legacy SQLite upgrade path

1. Back up `~/.config/milliways/milliways.db` from an installation with pre-substrate conversations.
2. Start milliways without `--use-legacy-conversation`.
3. Confirm startup logs a one-time migration completion message.
4. Verify prior conversations are present through the MemPalace-backed session view.
5. Exit and start milliways a second time.
6. Confirm the migration does not run again and legacy data remains readable.
7. Optionally start with `--use-legacy-conversation` and confirm the legacy store stays read-only.
