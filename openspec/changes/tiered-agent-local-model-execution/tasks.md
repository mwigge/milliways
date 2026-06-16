## 1. Gemma 4 Numerical Correctness

- [x] 1.1 In `rs-llmctl`, confirm the exact llama.cpp and Candle GELU implementations used by Gemma 4 GeGLU and document the matching operation
- [x] 1.2 Replace the Gemma 4 MLP SwiGLU path with the matching GeGLU activation in `src/native.rs`
- [x] 1.3 Run the ignored real Gemma 4 generation test and capture coherent-output evidence or the remaining failure
- [x] 1.4 If needed, add temporary layer-zero activation diagnostics for the fixed `[2, 9259]` input and compare tensors in the handoff order
- [x] 1.5 Fix the first material activation divergence and repeat the reference comparison until generation is coherent
- [x] 1.6 Remove or feature-gate all temporary diagnostics and restore formatter-only drift before commit
- [x] 1.7 Run `cargo fmt`, clippy with pedantic warnings denied, library tests, and the real-model generation readiness test

## 2. Four-Language Gemma 4 Readiness

- [x] 2.1 Define the canonical ten-line `1` through `10` output fixture and reproducible readiness evidence schema
- [x] 2.2 Add a Go generation fixture and verify it with the configured Go toolchain
- [x] 2.3 Add a Rust generation fixture and verify it with the configured Rust toolchain
- [x] 2.4 Add a Python generation fixture and verify it with the configured Python interpreter
- [x] 2.5 Add a TypeScript generation fixture and verify it with the configured TypeScript toolchain
- [x] 2.6 Persist artifact digest, runtime revision, prompt, sampling parameters, generated source, toolchain versions, commands, and outputs
- [x] 2.7 Expose `qualified` or `quarantined` Gemma 4 readiness in `rs-llmctl` status and model metadata

## 3. AMD ROCm Host Qualification

- [x] 3.1 Add hardware discovery for PCI device, GFX architecture, VRAM, OS, kernel, driver, `/dev/kfd`, `/dev/dri`, and service-user groups
- [x] 3.2 Add a versioned production ROCm compatibility policy and explicit technology-preview opt-in
- [x] 3.3 Generate package-manager installation plans for supported AMD Linux systems using official ROCm repositories and packages
- [x] 3.4 Add post-install checks using `amd-smi`, `rocminfo` or equivalent device enumeration, runtime versions, and device permissions
- [x] 3.5 Build or install a provenance-checked llama.cpp worker with `GGML_HIP`, the detected GFX target, and backend operation tests
- [x] 3.6 Run a real Gemma 4 HIP smoke request and prove non-zero GPU offload using runtime and memory/utilization evidence
- [x] 3.7 Measure cold load, warm latency, prompt throughput, decode throughput, model memory, KV-cache overhead, and peak VRAM
- [x] 3.8 Implement explicit ROCm to validated Vulkan to CPU fallback with status and telemetry reasons

## 4. Model and Backend Profile Catalog

- [x] 4.1 Define the specialist model profile schema with artifact, digest, license, language capabilities, format, quantization, backend, context, memory, prompts, grammar, gates, and qualification fields
- [x] 4.2 Define the backend service adapter catalog with protocol, accelerator, model-format, tool-call, structured-output, health, lifecycle, and maturity fields
- [x] 4.3 Add candidate importers for local files and selected catalog sources without enabling automatic trust
- [x] 4.4 Add policy checks for license, checksum, source provenance, hardware fit, runtime compatibility, and security maturity
- [x] 4.5 Add Gemma 4 GGUF, compressed-tensors, and native artifact compatibility rules
- [x] 4.6 Seed experimental profiles for `rs-llmctl`, llama.cpp, llama-swap, Ollama, LocalAI, vLLM, SGLang, LM Studio, and `mistral.rs`
- [x] 4.7 Add profile listing, inspection, qualification, quarantine, and removal commands

## 5. Supervisory Agent Capability Contract

- [x] 5.1 Define adapter capabilities for planning, structured delegation, result review, continuation, native protocol, tool calls, and model-tier aliasing
- [x] 5.2 Add capability reporting for Claude, Codex, Gemini, Copilot, Pool, and other existing agentic adapters
- [x] 5.3 Implement normalized internal chat, tool-call, structured-result, model discovery, and health contracts
- [x] 5.4 Add protocol adapters for OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages where supported
- [x] 5.5 Normalize client-specific tier aliases, authentication placeholders, and unsupported experimental headers in adapters
- [x] 5.6 Keep clients lacking supervisory capabilities in direct-run mode unless the operator explicitly selects another supervisor

## 6. Structured Local Execution

- [x] 6.1 Define and version the execution envelope and structured result JSON schemas
- [x] 6.2 Implement scope validation for repository root, base revision, allowed paths, forbidden paths, changed-file count, tokens, retries, and wall time
- [x] 6.3 Implement brokered edit operations with atomic application and rejection of undeclared paths
- [x] 6.4 Run formatter, compiler, test, lint, security, and output-gate commands with captured timeouts and evidence
- [x] 6.5 Return verified diff and command evidence to the active supervisory agent for accept, repair, reroute, or takeover
- [x] 6.6 Apply identical security policy to all model alignment labels, including uncensored or abliterated profiles

## 7. Resumable Tiered Workflow

- [x] 7.1 Add typed workflow nodes for plan, qualify, prewarm, execute, verify, review, repair, reroute, accept, and supervisor takeover
- [x] 7.2 Persist node inputs, outputs, evidence, and status so daemon restart resumes without repeating accepted work
- [x] 7.3 Add non-overlapping path analysis and verification compatibility checks before parallel execution
- [x] 7.4 Serialize or re-plan units with conflicting write scopes
- [x] 7.5 Enforce one default local repair attempt before supervisory takeover
- [x] 7.6 Add a global and per-session switch that disables local execution and safely drains queued units

## 8. Specialist Routing and Hot-Swap

- [x] 8.1 Implement the reserved sommelier local-model router using language, capability, health, qualification, context, memory, warm state, and predicted latency
- [x] 8.2 Add Tier 0 deterministic-tool, Tier 1 specialist, Tier 2 local-generalist, and Tier 3 supervisory-agent decisions with reasons
- [x] 8.3 Implement model states, serialized loading, progress reporting, cancellation, draining, quarantine, and failure recovery
- [x] 8.4 Enforce warm-set RAM/VRAM budgets with pinned models, LRU eviction, and idle TTL
- [x] 8.5 Prewarm the predicted specialist after planning and record cold versus warm request latency
- [x] 8.6 Support native `rs-llmctl` lifecycle first and a managed llama-swap worker adapter where process eviction is not yet available

## 9. Qualification Benchmarks

- [x] 9.1 Build a per-language benchmark suite covering generation, small bug fixes, test creation, and constrained refactors
- [x] 9.2 Run candidate models for Go, Rust, Python, and TypeScript and store quality, latency, memory, and verifier outcomes
- [x] 9.3 Allow one model to qualify for multiple languages and prevent routing for failed capabilities
- [x] 9.4 Define minimum quality and maximum latency thresholds for local delegation versus supervisor-only execution
- [x] 9.5 Run shadow mode that predicts routes and executes isolated evaluations without applying local edits to user worktrees
- [x] 9.6 Promote only profiles that meet thresholds on the target backend and hardware class

## 10. Observability and Operations

- [x] 10.1 Emit planner, executor, tier, model, backend, routing reason, scope digest, and workflow-node telemetry
- [x] 10.2 Emit queue, load, prompt, decode, verification, review, and total duration metrics
- [x] 10.3 Record changed-file count, command outcomes, retries, policy blocks, fallbacks, and supervisor takeovers
- [x] 10.4 Record GPU runtime, GFX target, offload status, and peak memory without exposing prompts, secrets, or private paths
- [x] 10.5 Add status views for supervisor capabilities, specialist readiness, warm set, load progress, backend health, and current workflow
- [x] 10.6 Add runbooks for ROCm failure, model quarantine, stuck load, verification failure, and disabling local execution

## 11. End-to-End Rollout

- [x] 11.1 Add end-to-end tests with fake supervisors and local executors for accept, retry, scope violation, stale revision, fallback, and takeover
- [x] 11.2 Add end-to-end tests for Claude, Codex, Gemini, Copilot, and Pool capability negotiation without hard-coded planner behavior
- [x] 11.3 Add AMD backend tests for supported, unsupported, no-device-access, CPU-only false positive, Vulkan fallback, and measured-fit cases
- [x] 11.4 Run the four-language Gemma 4 workflow through Milliways with a qualified local backend
- [x] 11.5 Enable Tier 1 behind an opt-in feature flag for selected repositories and collect shadow-versus-live metrics
- [x] 11.6 Document promotion criteria, rollback, supported service catalog, model sourcing, and operator controls
- [x] 11.7 Remove the opt-in restriction only after quality, latency, safety, and recovery thresholds are met
