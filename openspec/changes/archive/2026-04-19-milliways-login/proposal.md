## Why

Milliways routes tasks to different AI providers, but there is no unified way to authenticate to those providers. Users must know provider-specific auth mechanisms (e.g., `claude auth login`, `gemini auth login`, `opencode providers`, or manually editing `carte.yaml` for API keys). This fragments the onboarding experience and makes it hard to discover what needs auth.

## What Changes

- New `/login` slash command in the TUI that walks through provider authentication interactively.
- New `milliways login <kitchen>` CLI command for headless environments.
- For MiniMax: an interactive API key prompt that updates `carte.yaml` directly.
- Auth status surfaced in `milliways status` output.
- `maitre.UpdateKitchenAuth(name, key)` function that safely patches `carte.yaml`.

## Capabilities

### New Capabilities

- `kitchen-auth`: Unified authentication for all milliways kitchens. Covers interactive login flows, API key management, env var instructions, and session validation for each kitchen type (CLI OAuth, interactive TUI, env var, API key).

## Impact

- `cmd/milliways/main.go` — new `login` subcommand.
- `internal/tui/app.go` — new `/login` command handler in `executePaletteCommand`.
- `internal/maitre/onboard.go` — `UpdateKitchenAuth` for API key patching, `LoginKitchen` function.
- `internal/kitchen/generic.go` — `Setupable` interface already exists; extend if needed for status refresh after auth.
- `nvim-plugin/` — no changes needed.
