## ADDED Requirements

### Requirement: Specialist models use declarative profiles
The system SHALL represent local coding specialists with declarative profiles containing model alias, artifact identity and digest, license metadata, language capabilities, backend, quantization, context size, memory estimate, prompt template, output grammar, verification commands, and qualification status.

#### Scenario: Candidate asset lacks license or digest
- **WHEN** a discovered model asset has no acceptable license metadata or verified artifact digest
- **THEN** the system SHALL keep the asset unqualified and SHALL NOT route coding work to it

### Requirement: Qualification is capability-based
The system SHALL qualify models per language and task capability using executable benchmarks and SHALL allow one model to qualify for multiple languages.

#### Scenario: One model passes Go and Rust only
- **WHEN** a candidate passes the configured Go and Rust gates but fails Python or TypeScript
- **THEN** the model SHALL be eligible only for Go and Rust work

### Requirement: Routing selects a healthy qualified specialist
The local-model routing tier SHALL select a model using requested language, required capabilities, qualification score, health, context capacity, memory fit, warm state, and predicted latency.

#### Scenario: Preferred specialist is unhealthy
- **WHEN** the highest-scoring language specialist is unhealthy or unavailable
- **THEN** the router SHALL select the next qualified candidate or escalate to the local generalist or supervisory-agent tier with a recorded reason

### Requirement: Model lifecycle supports bounded hot-swapping
The system SHALL support warm, standby, loading, ready, draining, failed, and quarantined model states and SHALL enforce a configurable memory budget using pinning, LRU eviction, and idle TTL.

#### Scenario: Loading a specialist would exceed memory budget
- **WHEN** a requested specialist cannot load within the configured RAM or VRAM budget
- **THEN** the system SHALL evict an eligible least-recently-used model or select a fallback without exceeding the budget

#### Scenario: Concurrent requests trigger one cold load
- **WHEN** multiple requests select the same unloaded model
- **THEN** the system SHALL perform one serialized load and queue or reroute the remaining bounded requests

#### Scenario: Operator cancels a model load
- **WHEN** an operator cancels a loading or prewarming model
- **THEN** the system SHALL stop the load, release partial resources, report progress and cancellation state, and preserve other healthy workers

### Requirement: Backend implementations remain interchangeable
The system SHALL expose specialist execution through a normalized internal contract and backend-neutral health metadata so that OpenAI-compatible, Anthropic-compatible, `rs-llmctl`, llama-swap, LocalAI, or future runtimes can implement the worker lifecycle without changing orchestration semantics.

#### Scenario: Runtime backend changes
- **WHEN** an operator changes a model profile from one compatible backend to another
- **THEN** the same model alias, execution envelope, verification flow, and telemetry contract SHALL continue to work

#### Scenario: Agent client uses a different native protocol
- **WHEN** a supervisory client uses Anthropic Messages or OpenAI Responses instead of Chat Completions
- **THEN** its adapter SHALL normalize requests, model-tier aliases, tool calls, structured results, and unsupported headers without changing the execution envelope

### Requirement: Asset discovery is not automatic trust
The system SHALL treat LocalAI galleries, `awesome-local-llm`, Hugging Face, and similar catalogs as candidate discovery sources only.

#### Scenario: New catalog entry is discovered
- **WHEN** a new specialist candidate is imported from a catalog
- **THEN** the system SHALL require local policy, license, checksum, hardware-fit, security, and benchmark approval before activation

### Requirement: Supported services use a capability catalog
The system SHALL maintain a versioned catalog of tested local inference service adapters containing protocol, model-format, accelerator, tool-call, structured-output, health, lifecycle, and maturity metadata.

#### Scenario: Discovered service lacks a tested adapter
- **WHEN** an operator selects a service found in a community catalog but no tested Milliways adapter exists
- **THEN** the system SHALL classify it as experimental and SHALL NOT enable unattended coding execution by default

### Requirement: Model alignment does not alter execution policy
The system SHALL apply the same workspace scope, command authorization, secret protection, output scanning, verification, and audit controls regardless of whether a model is described as uncensored, abliterated, safety-tuned, or unaligned.

#### Scenario: Uncensored model requests a forbidden operation
- **WHEN** a local model proposes an operation forbidden by the execution envelope or Milliways security policy
- **THEN** the system SHALL block the operation and record the policy decision exactly as it would for any other model
