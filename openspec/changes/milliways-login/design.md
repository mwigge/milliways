## Context

Milliways routes tasks to multiple AI provider kitchens. Each kitchen has its own authentication mechanism:

| Kitchen | Auth Type | Login Method |
|---------|-----------|--------------|
| claude | Browser OAuth | `claude auth login` |
| gemini | Browser OAuth | `gemini auth login` |
| opencode | Interactive TUI | `opencode providers` |
| minimax | API key | Interactive prompt → writes to `carte.yaml` |
| groq | Env var | Instructions only |
| ollama | None | Not applicable |
| aider/goose/cline | Env/API key | Instructions only |

The existing `SetupKitchen` function (`internal/maitre/onboard.go`) handles install+auth but is CLI-only and prints instructions rather than running the auth process interactively. The TUI has no equivalent.

## Goals / Non-Goals

**Goals:**
- Unified `/login` command in the TUI that walks through auth for any kitchen
- `milliways login <kitchen>` CLI subcommand for headless use
- MiniMax API key flow that interactively prompts and updates `carte.yaml`
- Auth status visible in `milliways status`
- Graceful degradation: show clear instructions when interactive auth isn't possible

**Non-Goals:**
- Implementing OAuth flows — delegate to each provider's own CLI
- Storing credentials anywhere other than the provider's native location (env, clave.yaml)
- Multi-provider simultaneous login

## Decisions

**1. One function per kitchen type, categorized by auth method**

```
LoginCLI OAuth(claude, gemini)     → exec.Command(cli, "auth", "login")
LoginInteractiveTUI(opencode)      → exec.Command(cli, "providers")
LoginAPIKey(minimax)              → prompt → UpdateCarteYAML()
LoginEnvVar(groq, aider, ...)      → print env var instructions
LoginNone(ollama)                  → check if running, print status
```

Rationale: Each category has a distinct user experience. No shared interface needed — the auth method is deterministic per kitchen.

**2. `maitre.UpdateKitchenAuth(name, key)` patches carte.yaml in-place**

```go
func UpdateKitchenAuth(kitchen, apiKey string) error
```

- Reads existing `carte.yaml`
- Finds the named kitchen's `http_client.auth_key` field
- Writes back the file preserving all other content (YAML merge)
- Does NOT overwrite `auth_key` for non-HTTPClient kitchens

Rationale: MiniMax and Groq store credentials in `carte.yaml`. The alternative (env vars) works but requires users to know to set them. Storing in config is more transparent and debuggable.

**3. TTY detection for interactive flows**

If stdin is not a TTY, skip interactive auth and show instructions instead.

```go
func isTTY() bool { return term.IsTerminal(int(os.Stdout.Fd())) }
```

Rationale: Piped input (e.g., from nvim) should not block waiting for an auth prompt.

**4. Auth status refresh after login**

After successful auth, re-check `kitchen.Status()` to update the registry.

Rationale: The GenericKitchen status check validates the binary is installed and allowed. After OAuth, the session may now be valid even if the binary path hasn't changed.

## Risks / Trade-offs

- [Risk] OAuth flows (`claude auth login`, `gemini auth login`) open browser windows — not all environments have browsers.
  → Mitigation: Detect headless environments and show instructions instead.
- [Risk] Writing to `carte.yaml` could corrupt the file.
  → Mitigation: Write to a temp file first, then rename (atomic). Keep a `.bak` backup.
- [Risk] API key stored in plain text in `carte.yaml`.
  → Mitigation: This is standard for CLI tools (like `netrc`). Document that the file should have restricted permissions (`chmod 600`).

## Open Questions

1. Should we support `GOOGLE_API_KEY` for gemini in addition to the OAuth flow?
2. Should `milliways login --all` walk through all kitchens sequentially?
3. For groq/aider — should we also prompt for env vars and offer to write to shell profile?
