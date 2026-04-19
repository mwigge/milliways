# Tasks — milliways-http-kitchen

## Course HK-1: HTTPKitchen Core [2 SP]

### HK-1.1 HTTPClientConfig [0.5 SP]

- [x] Add `HTTPClientConfig` struct to `internal/maitre/config.go`
- [x] Add `HTTPClient *HTTPClientConfig` field to `KitchenConfig`
- [x] Add YAML tags: `base_url`, `auth_key`, `auth_type`, `model`, `stations`, `tier`, `response_format`, `timeout_seconds`
- [x] Unit tests for config round-trip

### HK-1.2 HTTPKitchen implementation [1 SP]

- [x] Create `internal/kitchen/adapter/http.go`
- [x] Implement `NewHTTPKitchen(name, cfg, stations, tier) (*HTTPKitchen, error)`
- [x] Implement `Name()`, `Stations()`, `CostTier()`, `Status()` accessors
- [x] Implement `Exec(ctx, task)`:
  - Read API key from env var
  - Build request body for OpenAI format
  - POST to base_url with Bearer auth
  - SSE parse loop for OpenAI format (default)
  - Call `task.OnLine` for each content delta
  - Return `Result{ExitCode: 0, Output: full_content, Duration: elapsed}`
- [x] Handle non-200 responses with body read
- [x] Handle context cancellation

### HK-1.3 Response format parsers [0.5 SP]

- [x] Add `parseOpenAIChunk(line) (content, done)` — default SSE parser
- [x] Add `parseAnthropicChunk(line) (content, done)` — `event: anthropic` + content_block_delta
- [x] Add `parseMiniMaxChunk(line) (content, done)` — custom MiniMax format
- [x] Add `parseOllamaChunk(line) (content, done)` — `/api/chat` format
- [x] Select parser based on `ResponseFormat` field

---

## Course HK-2: buildRegistry Integration [0.5 SP]

### HK-2.1 Registry branching [0.5 SP]

- [x] Modify `cmd/milliways/main.go` — `buildRegistry` branches on `kc.HTTPClient != nil`
- [x] Apply defaults: `auth_type=bearer`, `response_format=openai`, `timeout_seconds=300`
- [x] Call `adapter.NewHTTPKitchen` for HTTPClient path
- [x] Call `kitchen.NewGeneric` for CLI path (unchanged)
- [x] Log warning and skip kitchen on error

### HK-2.2 Safe env keys [0 SP]

- [x] N/A — HTTPKitchen reads env vars directly via `os.Getenv`, not subprocess env. No `safeEnvKeys` change needed.

---

## Course HK-3: Tests [1 SP]

### HK-3.1 Unit tests with httptest.Server [0.5 SP]

- [x] Create `internal/kitchen/adapter/http_test.go`
- [x] Test OpenAI SSE format parsing
- [x] Test Anthropic SSE format parsing
- [x] Test MiniMax SSE format parsing
- [x] Test Ollama SSE format parsing
- [x] Test non-200 error response
- [x] Test NeedsAuth status when env var absent
- [x] Test context cancellation mid-stream

### HK-3.2 Config tests [0.5 SP]

- [x] Test `KitchenConfig` round-trip YAML marshal/unmarshal for HTTPClient
- [x] Test default values applied in `buildRegistry`
- [x] Test skip on invalid HTTPClient config

---

## Course HK-4: carte.yaml Defaults [0.5 SP]

### HK-4.1 Default entries [0.5 SP]

- [x] Add MiniMax default entry to `maitre/config.go` `defaultConfig()`:
  `base_url: "https://api.minimaxi.com/v1/text"`, `auth_key: "MINIMAX_API_KEY"`, `model: "M2-her"`, `response_format: "minimax"`, stations: `reason, analyze, write`, tier: `cloud`
- [x] Add Groq default entry: `base_url: "https://api.groq.com/openai/v1"`, `auth_key: "GROQ_API_KEY"`, `model: "mixtral-8x7b-32768"`, stations: `fast, simple`, tier: `free`
- [x] Add Ollama default entry: `base_url: "http://localhost:11434"`, `auth_key: ""`, `model: "llama3"`, `response_format: "ollama"`, stations: `local, private`, tier: `free`
- [x] Add routing keywords: `fast`/`quick` → groq, `local`/`private` → ollama
- [x] Verify `go test ./internal/maitre/...` passes

---

## Course HK-5: Verification [0.5 SP]

### HK-5.1 Build and basic test [0.25 SP]

- [x] `go build ./cmd/milliways/...` succeeds
- [x] `go test ./internal/kitchen/adapter/...` passes
- [x] `go test ./internal/maitre/...` passes
- [x] `go vet ./...` clean
- [x] `go test -race ./...` passes

### HK-5.2 Manual verification [0.25 SP]

- [ ] TUI shows MiniMax, Groq, Ollama in kitchen list with correct status (NeedsAuth or Ready)
- [ ] `@kitchen groq "say hello"` forces Groq routing — Groq API key in env → streams output
- [ ] `@kitchen minimax "say hello"` forces MiniMax — MiniMax API key in env → streams output
- [ ] `@kitchen ollama "say hello"` forces Ollama — no API key needed → streams output

---

## Palate Cleanser

- [x] HTTPKitchen is implemented, tested, and ships MiniMax, Groq, and Ollama as first-class kitchens. All existing kitchen-parity behaviour is unaffected.
