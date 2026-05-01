# Design — milliways-http-kitchen

## D1: HTTPKitchen Architecture

### D1.1 Interface contract

HTTPKitchen implements the `Kitchen` interface:

```go
type Kitchen interface {
    Name() string
    Exec(ctx context.Context, task kitchen.Task) (kitchen.Result, error)
    Stations() []string
    CostTier() kitchen.CostTier
    Status() kitchen.Status
}
```

The `Exec` method is the hot path. It:
1. Reads the API key from the env var named by `auth_key`
2. Builds a POST request with the prompt as a chat message
3. Sends it over HTTP with SSE streaming
4. Parses SSE chunks in provider-specific format
5. Calls `task.OnLine(content)` for each text chunk
6. Returns the full output as `Result.Output`

### D1.2 Request body formats

**OpenAI format** (default, for OpenAI, Groq, Deepseek, Perplexity, Mistral, OpenRouter):

```json
{
  "model": "gpt-4o",
  "stream": true,
  "messages": [{"role": "user", "content": "<task.Prompt>"}]
}
```

**Anthropic format** (`response_format: anthropic`):

```json
{
  "model": "claude-sonnet-4-20250514",
  "stream": true,
  "max_tokens": 8192,
  "messages": [{"role": "user", "content": [{"type": "text", "text": "<task.Prompt>"}]}]
}
```

**Ollama format** (`response_format: ollama`):

```json
{
  "model": "llama3",
  "stream": true,
  "messages": [{"role": "user", "content": "<task.Prompt>"}]
}
```

**MiniMax format** (`response_format: minimax`):

```json
{
  "model": "M2-her",
  "stream": true,
  "messages": [{"role": "user", "content": "<task.Prompt>"}]
}
```

### D1.3 SSE chunk parsing

**OpenAI SSE** — each chunk:
```
data: {"choices":[{"delta":{"content":"Hello"}}]}
data: {"choices":[{"delta":{"content":" world"}}]}
data: [DONE]
```

**Anthropic SSE** — each chunk:
```
event: anthropic
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}
event: anthropic
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}
event: anthropic
data: {"type":"message_stop"}
```

**MiniMax SSE** — each chunk:
```
data: {"choices":[{"delta":{"content":"Hello"}}...]}
data: {"choices":[{"delta":{"content":" world"}}...]}
```

**Ollama SSE** — each chunk:
```
data: {"message":{"content":"Hello"},"done":false}
data: {"message":{"content":" world"},"done":true}
```

### D1.4 Auth handling

| Auth type | Header |
|-----------|--------|
| `bearer` (default) | `Authorization: Bearer <api_key>` |
| `apikey` | `X-API-Key: <api_key>` |

Env var name is stored in `auth_key` field — never the key itself. `Status()` returns `NeedsAuth` when the env var is empty.

### D1.5 Error handling

- HTTP non-200 response: read body (up to 1 KB), wrap as `Result.Error`
- SSE parse error on chunk: skip chunk, continue
- Network error: return error via `Result.Error`
- Context cancellation: propagate via `context.WithTimeout` on request

### D1.6 Timeout

HTTP request uses `http.DefaultClient` with a 5-minute timeout (configurable via `HTTPClientConfig.Timeout`, default `5 * time.Minute`).

## D2: Config Schema

### D2.1 Full `HTTPClientConfig`

```go
type HTTPClientConfig struct {
    BaseURL        string   `yaml:"base_url"`          // required
    AuthKey        string   `yaml:"auth_key"`           // env var name; required
    AuthType       string   `yaml:"auth_type"`          // "bearer" or "apikey"; default "bearer"
    Model          string   `yaml:"model"`              // required
    Stations       []string `yaml:"stations"`           // overrides top-level Stations
    Tier           string   `yaml:"tier"`               // cost tier override
    ResponseFormat string   `yaml:"response_format"`    // "openai"|"anthropic"|"minimax"|"ollama"; default "openai"
    Timeout        int      `yaml:"timeout_seconds"`   // request timeout; default 300
}
```

### D2.2 Env var safe list

HTTPKitchen only passes the named auth env var to the subprocess. The following are explicitly allowed in `safeEnvKeys`:

```go
"MINIMAX_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY",
"GROQ_API_KEY", "DEEPSEEK_API_KEY", "PERPLEXITY_API_KEY",
"MISTRAL_API_KEY", "OPENROUTER_API_KEY", "OLLAMA_API_KEY"
```

This mirrors the `GenericKitchen` safe env var approach.

## D3: buildRegistry Changes

```go
func buildRegistry(cfg *maitre.Config) *kitchen.Registry {
    reg := kitchen.NewRegistry()
    for name, kc := range cfg.Kitchens {
        tier := kitchen.ParseCostTier(kc.CostTier)

        if kc.HTTPClient != nil {
            // Apply defaults
            httpCfg := kc.HTTPClient
            if httpCfg.AuthKey == "" {
                httpCfg.AuthKey = strings.ToUpper(name) + "_API_KEY"
            }
            if httpCfg.AuthType == "" {
                httpCfg.AuthType = "bearer"
            }
            if httpCfg.ResponseFormat == "" {
                httpCfg.ResponseFormat = "openai"
            }
            adapter, err := adapter.NewHTTPKitchen(name, *httpCfg, kc.Stations, tier)
            if err != nil {
                slog.Warn("skipping http kitchen", "name", name, "err", err)
                continue
            }
            reg.Register(adapter)
            continue
        }

        // CLI kitchen (existing path)
        reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
            Name:  name,
            Cmd:   kc.Cmd,
            Args:  kc.Args,
            Tier:  tier,
        }))
    }
    return reg
}
```

## D4: TUI Integration

### D4.1 RunTargetChooser

The TUI's kitchen selector overlay (`run_targets.go`) calls `kitchen.Status()` on each registered kitchen. HTTPKitchen returns:
- `kitchen.Ready` — API key present
- `kitchen.NeedsAuth` — API key absent (env var empty)

The TUI already has a `NeedsAuth` render path for kitchens that need interactive login. No TUI changes required — HTTPKitchen's `Status()` surfaces this correctly.

### D4.2 Force kitchen

Typing `@minimax "prompt"` in the TUI forces the `minimax` kitchen regardless of routing. This works for HTTPKitchen since `Registry.Get("minimax")` returns the HTTPKitchen instance.

## D5: nvim Plugin Compatibility

The nvim plugin calls:
```
milliways --kitchen <name> --json --context-file <path> <prompt>
```

`--kitchen minimax` routes to the HTTPKitchen adapter. No plugin changes required.

## D6: File Map

```
internal/kitchen/adapter/http.go       NEW — HTTPKitchen implementation
internal/kitchen/adapter/http_test.go NEW — test with httptest.Server
internal/maitre/config.go              MOD — HTTPClientConfig struct, KitchenConfig extension
cmd/milliways/main.go                 MOD — buildRegistry branches on HTTPClient != nil
cmd/milliways/commands.go             MOD — safeEnvKeys for HTTP API key env vars
```

## D7: Risks

| Risk | Mitigation |
|------|-----------|
| Provider changes SSE format | Version the `response_format` field; add new format values as needed |
| API key env var name collision | Explicit env var name per kitchen in config; no magic |
| Long responses timeout | Configurable `timeout_seconds`; default 5 min covers most cases |
| Non-streaming providers | Return error with clear message; streaming is required |
| MiniMax API changes | MiniMax-specific `response_format: minimax` isolates this case |
