# Tasks — milliways-http-kitchen

## Course HK-1: HTTPKitchen Core [2 SP]

### HK-1.1 HTTPClientConfig [0.5 SP]

- [ ] Add `HTTPClientConfig` struct to `internal/maitre/config.go`
- [ ] Add `HTTPClient *HTTPClientConfig` field to `KitchenConfig`
- [ ] Add YAML tags: `base_url`, `auth_key`, `auth_type`, `model`, `stations`, `tier`, `response_format`, `timeout_seconds`
- [ ] Unit tests for config round-trip

### HK-1.2 HTTPKitchen implementation [1 SP]

- [ ] Create `internal/kitchen/adapter/http.go`
- [ ] Implement `NewHTTPKitchen(name, cfg, stations, tier) (*HTTPKitchen, error)`
- [ ] Implement `Name()`, `Stations()`, `CostTier()`, `Status()` accessors
- [ ] Implement `Exec(ctx, task)`:
  - Read API key from env var
  - Build request body for OpenAI format
  - POST to base_url with Bearer auth
  - SSE parse loop for OpenAI format (default)
  - Call `task.OnLine` for each content delta
  - Return `Result{ExitCode: 0, Output: full_content, Duration: elapsed}`
- [ ] Handle non-200 responses with body read
- [ ] Handle context cancellation

### HK-1.3 Response format parsers [0.5 SP]

- [ ] Add `parseOpenAIChunk(line) (content, done)` — default SSE parser
- [ ] Add `parseAnthropicChunk(line) (content, done)` — `event: anthropic` + content_block_delta
- [ ] Add `parseMiniMaxChunk(line) (content, done)` — custom MiniMax format
- [ ] Add `parseOllamaChunk(line) (content, done)` — `/api/chat` format
- [ ] Select parser based on `ResponseFormat` field

---

## Course HK-2: buildRegistry Integration [0.5 SP]

### HK-2.1 Registry branching [0.5 SP]

- [ ] Modify `cmd/milliways/main.go` — `buildRegistry` branches on `kc.HTTPClient != nil`
- [ ] Apply defaults: `auth_type=bearer`, `response_format=openai`, `timeout_seconds=300`
- [ ] Call `adapter.NewHTTPKitchen` for HTTPClient path
- [ ] Call `kitchen.NewGeneric` for CLI path (unchanged)
- [ ] Log warning and skip kitchen on error

### HK-2.2 Safe env keys [0 SP]

- [ ] Add HTTP API key env vars to `safeEnvKeys` in `cmd/milliways/commands.go`:
  `MINIMAX_API_KEY`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GROQ_API_KEY`, `DEEPSEEK_API_KEY`, `PERPLEXITY_API_KEY`, `MISTRAL_API_KEY`, `OPENROUTER_API_KEY`

---

## Course HK-3: Tests [1 SP]

### HK-3.1 Unit tests with httptest.Server [0.5 SP]

- [ ] Create `internal/kitchen/adapter/http_test.go`
- [ ] Test OpenAI SSE format parsing
- [ ] Test Anthropic SSE format parsing
- [ ] Test MiniMax SSE format parsing
- [ ] Test Ollama SSE format parsing
- [ ] Test non-200 error response
- [ ] Test NeedsAuth status when env var absent
- [ ] Test context cancellation mid-stream

### HK-3.2 Config tests [0.5 SP]

- [ ] Test `KitchenConfig` round-trip YAML marshal/unmarshal for HTTPClient
- [ ] Test default values applied in `buildRegistry`
- [ ] Test skip on invalid HTTPClient config

---

## Course HK-4: carte.yaml Defaults [0.5 SP]

### HK-4.1 Default entries [0.5 SP]

- [ ] Add MiniMax default entry to `maitre/config.go` `defaultConfig()`:
  `base_url: "https://api.minimaxi.com/v1/text"`, `auth_key: "MINIMAX_API_KEY"`, `model: "M2-her"`, `response_format: "minimax"`, stations: `reason, analyze, write`, tier: `cloud`
- [ ] Add Groq default entry: `base_url: "https://api.groq.com/openai/v1"`, `auth_key: "GROQ_API_KEY"`, `model: "mixtral-8x7b-32768"`, stations: `fast, simple`, tier: `free`
- [ ] Add Ollama default entry: `base_url: "http://localhost:11434"`, `auth_key: ""`, `model: "llama3"`, `response_format: "ollama"`, stations: `local, private`, tier: `free`
- [ ] Verify `go test ./internal/maitre/...` passes

---

## Course HK-5: Verification [0.5 SP]

### HK-5.1 Build and basic test [0.25 SP]

- [ ] `go build ./cmd/milliways/...` succeeds
- [ ] `go test ./internal/kitchen/adapter/...` passes
- [ ] `go test ./internal/maitre/...` passes
- [ ] `go vet ./...` clean

### HK-5.2 Manual verification [0.25 SP]

- [ ] TUI shows MiniMax, Groq, Ollama in kitchen list with correct status (NeedsAuth or Ready)
- [ ] `@kitchen groq "say hello"` forces Groq routing — Groq API key in env → streams output
- [ ] `@kitchen minimax "say hello"` forces MiniMax — MiniMax API key in env → streams output
- [ ] `@kitchen ollama "say hello"` forces Ollama — no API key needed → streams output

---

## Palate Cleanser

- [x] HTTPKitchen is implemented, tested, and ships MiniMax, Groq, and Ollama as first-class kitchens. All existing kitchen-parity behaviour is unaffected.
