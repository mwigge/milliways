# milliways-http-kitchen — HTTP API Kitchen Adapter

> Add any HTTP API model as a first-class milliways kitchen.
> No CLI binary required — just YAML config and an API key.

## Positioning

Milliways is the maitre d' of AI workflows. Kitchens cook; milliways routes and remembers. Today kitchens are CLI binaries (`claude`, `opencode`, `gemini`, etc.). HTTPKitchen adds a new transport class: **REST API providers**.

The `Kitchen` interface (`Name`, `Exec`, `Stations`, `CostTier`, `Status`) already abstracts transport. HTTPKitchen implements it for any SSE-capable HTTP endpoint — OpenAI-compatible, Anthropic, MiniMax, Ollama — without touching routing, switching, memory, TUI, or nvim.

## Why

Three gaps today:

1. **MiniMax** — A strong low-cost model with an HTTP API, but no CLI binary to wrap. It can't be a milliways kitchen today.
2. **Direct API access** — Users with API keys for OpenAI, Groq, Deepseek, Perplexity want milliways routing and memory without installing another CLI tool.
3. **Local models (Ollama)** — Ollama serves models over HTTP locally. Milliways should be able to route to it without an intermediate CLI wrapper.

HTTPKitchen closes all three with a single adapter, driven entirely by `carte.yaml`.

## What Changes

### `KitchenConfig` — add HTTP variant

```go
type KitchenConfig struct {
    // existing CLI fields...
    HTTPClient *HTTPClientConfig  // non-nil → use HTTPKitchen
}

type HTTPClientConfig struct {
    BaseURL       string   `yaml:"base_url"`
    AuthKey       string   `yaml:"auth_key"`        // env var name
    AuthType      string   `yaml:"auth_type"`        // "bearer" (default) or "apikey"
    Model         string   `yaml:"model"`
    Stations      []string `yaml:"stations"`
    Tier          string   `yaml:"tier"`
    ResponseFormat string  `yaml:"response_format"`  // "openai" | "anthropic" | "minimax" | "ollama"
}
```

### `HTTPKitchen` adapter — `internal/kitchen/adapter/http.go`

New file implementing `Kitchen` interface:
- Builds HTTP request with Bearer or API-key auth
- Parses SSE streams in provider-specific formats
- Maps SSE chunks → `task.OnLine` callbacks (same as CLI adapters)
- Returns `Result{ExitCode, Output, Duration}`
- `Status()` returns `NeedsAuth` when `AUTH_KEY` env var is absent

Supported `response_format` values:

| Format | Providers | Notes |
|--------|-----------|-------|
| `openai` (default) | OpenAI, Groq, Deepseek, Perplexity, Mistral, OpenRouter | Standard `/v1/chat/completions` SSE |
| `anthropic` | Claude (api.anthropic.com) | `anthropic` event type, `messages` endpoint |
| `minimax` | MiniMax | Custom SSE chunk format from `M2-her` |
| `ollama` | Ollama (localhost) | `/api/chat` format, no auth |

### `buildRegistry` — detect both CLI and HTTP

In `cmd/milliways/main.go`, `buildRegistry` now branches:
```go
if kc.HTTPClient != nil {
    adapter, err := adapter.NewHTTPKitchen(name, *kc.HTTPClient, kc.Stations, tier)
    // ...
} else {
    adapter := kitchen.NewGeneric(...)
}
```

### `carte.yaml` — default HTTP kitchen entries

Ship default entries for MiniMax, Groq, and Ollama so users get something working out of the box:
```yaml
kitchens:
  minimax:
    http_client:
      base_url: "https://api.minimaxi.com/v1/text"
      auth_key: "MINIMAX_API_KEY"
      auth_type: "bearer"
      model: "M2-her"
      response_format: "minimax"
    stations: [reason, analyze, write]
    cost_tier: cloud

  groq:
    http_client:
      base_url: "https://api.groq.com/openai/v1"
      auth_key: "GROQ_API_KEY"
      model: "mixtral-8x7b-32768"
      response_format: openai
    stations: [fast, simple]
    cost_tier: free

  ollama:
    http_client:
      base_url: "http://localhost:11434"
      auth_key: ""
      model: "llama3"
      response_format: ollama
    stations: [local, private]
    cost_tier: free
```

Users wanting Claude, OpenAI, Deepseek, Perplexity, or Mistral add their own `carte.yaml` entries — milliways ships only the ones with free or low-cost tiers by default.

## Capabilities

### New Capabilities

- `http-kitchen` — Any REST API with SSE streaming becomes a milliways kitchen via YAML config. No CLI binary required.

### Modified Capabilities

- `kitchen-registry` — Registry now accepts both CLI-based (`GenericKitchen`) and HTTP-based (`HTTPKitchen`) adapters.
- `carte-config` — Schema extended with `http_client` block and `response_format` field.

## Explicit Non-Goals

- **Multi-turn session management** — HTTPKitchen is stateless per call, same as CLI kitchens. Session continuity is handled by milliways' orchestrator (continuation prompts), not by the adapter.
- **OpenAI / Anthropic SDK usage** — Direct HTTP calls only, no provider SDK dependencies.
- **Streaming HTTP without SSE** — Only SSE-streaming endpoints supported (all major providers).
- **Non-streaming requests** — milliways is a streaming-first TUI; non-streaming is out of scope.

## Impact

### New Files

- `internal/kitchen/adapter/http.go` — HTTPKitchen implementation
- `internal/kitchen/adapter/http_test.go` — Unit tests with mocked HTTP server

### Modified Files

- `internal/maitre/config.go` — `HTTPClientConfig` struct, `KitchenConfig` extended
- `cmd/milliways/main.go` — `buildRegistry` branches on `HTTPClient != nil`
- `cmd/milliways/commands.go` — `safeEnvKeys` extended with API key env var names

### No Changes To

- `Sommelier`, `Orchestrator`, `HookRunner`, `SkillCatalog` — kitchen-transport-agnostic
- `TUI`, `nvim plugin`, `Pipeline`, `Recipes` — work with HTTPKitchen identically to CLI kitchens

## Success Criteria

1. `carte.yaml` with `minimax` http_client entry routes correctly; MiniMax API key in env; TTY shows streaming output.
2. `@kitchen minimax "prompt"` forces MiniMax; `@kitchen groq "prompt"` forces Groq.
3. MiniMax or Groq kitchen shows `NeedsAuth` status when env var is absent.
4. `go test ./internal/kitchen/...` passes including new HTTP tests.
5. `go build ./cmd/milliways/...` succeeds.
6. Ollama at `localhost:11434` works for local model routing without API key.
