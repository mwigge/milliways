## ADDED Requirements

### Requirement: HTTPClientConfig struct

The `HTTPClientConfig` struct in `internal/maitre/config.go` SHALL contain all fields needed to configure an HTTP API kitchen:

```go
type HTTPClientConfig struct {
    BaseURL        string   `yaml:"base_url"`          // required — full endpoint URL
    AuthKey        string   `yaml:"auth_key"`          // required — env var name holding API key
    AuthType       string   `yaml:"auth_type"`         // "bearer" or "apikey"; default "bearer"
    Model          string   `yaml:"model"`              // required — model identifier
    Stations       []string `yaml:"stations"`           // optional — overrides top-level Stations
    Tier           string   `yaml:"tier"`               // optional — overrides cost tier
    ResponseFormat string   `yaml:"response_format"`    // "openai"|"anthropic"|"minimax"|"ollama"; default "openai"
    Timeout        int      `yaml:"timeout_seconds"`   // request timeout; default 300
}
```

#### Scenario: KitchenConfig discriminated union
- **WHEN** `carte.yaml` has a kitchen with `http_client:` block
- **THEN** `kc.HTTPClient != nil` SHALL be true and `kc.Cmd` SHALL be empty
- **WHEN** `carte.yaml` has a kitchen with `cmd:` field
- **THEN** `kc.HTTPClient` SHALL be nil (CLI kitchen)

#### Scenario: AuthKey env var name preserved
- **WHEN** `HTTPClientConfig.AuthKey = "MINIMAX_API_KEY"`
- **THEN** the value of env var `MINIMAX_API_KEY` SHALL be read at request time, never the literal string

#### Scenario: ResponseFormat selects SSE parser
- **WHEN** `ResponseFormat = "openai"` **THEN** OpenAI SSE format SHALL be used
- **WHEN** `ResponseFormat = "anthropic"` **THEN** Anthropic SSE format SHALL be used
- **WHEN** `ResponseFormat = "minimax"` **THEN** MiniMax SSE format SHALL be used
- **WHEN** `ResponseFormat = "ollama"` **THEN** Ollama SSE format SHALL be used

### Requirement: KitchenConfig discriminated by transport

The `KitchenConfig` struct SHALL support exactly one transport:

```go
type KitchenConfig struct {
    Cmd        string
    Args       []string
    Stations   []string
    CostTier   string
    Enabled    *bool
    Env        map[string]string
    DailyLimit   int
    DailyMinutes float64
    WarnThreshold float64
    HTTPClient    *HTTPClientConfig  // non-nil → HTTPKitchen; nil → GenericKitchen
}
```

#### Scenario: HTTPClient non-nil excludes Cmd
- **WHEN** `kc.HTTPClient` is non-nil
- **THEN** `kc.Cmd` SHALL be ignored by `buildRegistry`

#### Scenario: Default values applied by buildRegistry
- **WHEN** `buildRegistry` processes an `HTTPClient` kitchen
- **THEN** it SHALL apply defaults: `auth_type = "bearer"`, `response_format = "openai"`, `timeout_seconds = 300`
- **AND** if `auth_key` is empty it SHALL be set to `strings.ToUpper(name) + "_API_KEY"`

### Requirement: Env var safe list

HTTPKitchen SHALL only pass the named auth env var to the HTTP request. The following env var names SHALL be the only ones allowed for HTTP API auth:

- `MINIMAX_API_KEY`
- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `GROQ_API_KEY`
- `DEEPSEEK_API_KEY`
- `PERPLEXITY_API_KEY`
- `MISTRAL_API_KEY`
- `OPENROUTER_API_KEY`

#### Scenario: Only named env vars are transmitted
- **WHEN** HTTPKitchen makes a request
- **THEN** only the env var named in `AuthKey` SHALL be included in the request headers
- **AND** no other environment variables SHALL be transmitted
