## Why

Milliways can run multiple agentic clients and local models, but it does not yet have a durable contract for using the active agent as a planner/coordinator while delegating bounded implementation work to fast, specialized local models. Gemma 4 also needs a correctness gate before it can be trusted for execution, and AMD users need a validated ROCm path rather than best-effort GPU detection.

## What Changes

- Add a tiered coding workflow in which any qualified agentic Milliways client can own planning, decomposition, coordination, verification, and escalation while local models execute bounded tasks.
- Add a structured execution envelope so delegated work has explicit language, files, constraints, acceptance checks, output format, and retry budget.
- Add local specialist routing for Go, Rust, Python, and TypeScript models with hot-swap, warm-residency, health, capability, and fallback behavior.
- Add a Gemma 4 readiness gate based on a deterministic four-language task: generate and run programs that print the integers 1 through 10 in Go, Rust, Python, and TypeScript.
- Continue the `rs-llmctl` Gemma 4 numerical-correctness work from `/tmp/gemma4-numerics-handoff.md`, beginning with the confirmed GeGLU activation mismatch and activation-level comparison against llama.cpp.
- Add an AMD GPU plan that validates supported hardware and operating systems, installs production ROCm/HIP, builds or selects a HIP-capable local runtime, verifies actual GPU offload, and falls back explicitly to Vulkan or CPU.
- Add observability and policy gates for planner selection, model selection, cold/warm load latency, execution outcome, verification result, fallback reason, and supervisory-agent escalation.

## Capabilities

### New Capabilities

- `tiered-coding-orchestration`: Agent-led planning and coordination with bounded local-model execution, verification, retry, and escalation across supported Milliways clients.
- `local-specialist-models`: Capability discovery, language-based selection, lifecycle management, and hot-swapping for Go, Rust, Python, and TypeScript coding models.
- `gemma4-execution-readiness`: Numerical-correctness remediation and deterministic four-language acceptance gates for the local Gemma 4 model.
- `amd-rocm-local-inference`: Supported-hardware validation, ROCm/HIP installation, GPU-offload verification, capacity planning, and explicit fallback behavior.

### Modified Capabilities

_(none — the current OpenSpec capabilities do not define local model orchestration or GPU execution behavior.)_

## Impact

- `internal/orchestrator/`, `internal/sommelier/`, `internal/kitchen/`, and runner adapters in Milliways.
- `cmd/milliways/` and `cmd/milliwaysctl/` status, routing, local-model, and execution commands.
- `scripts/install_local.sh`, `scripts/install_local_swap.sh`, service configuration, and local model catalog/profile data.
- `rs-llmctl` native Gemma 4 implementation, model lifecycle, device selection, readiness reporting, and OpenAI-compatible serving.
- Local toolchains for Go, Rust, Python, and TypeScript used by the verification gate.
- ROCm/HIP and llama.cpp are production backend dependencies on supported AMD Linux systems; Vulkan and CPU remain explicit fallbacks.
- Kalosm informs the task-oriented and structured-output API design, but is not introduced as a production dependency by this change.
