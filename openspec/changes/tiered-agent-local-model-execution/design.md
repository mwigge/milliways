## Context

Milliways already exposes Claude, Codex, Copilot, Gemini, Pool, and other agentic clients alongside local OpenAI-compatible endpoints. It has a provider-owning orchestrator, a sommelier with a reserved local-model routing tier, a traced delegation helper, and local model install/switch commands. `rs-llmctl` already supports alias-keyed models and lifecycle plans; `llama-swap` already supports TTL-based unload and warm residency.

The missing layer is a coding execution protocol. Today a prompt is routed to one runner, but there is no typed boundary that lets the active supervisory agent retain architectural control while a smaller local model performs a bounded edit and returns machine-verifiable evidence.

Gemma 4 is not ready for that role yet. The current Candle GGUF path produces prompt-dependent but incoherent output. The handoff identifies a confirmed FFN mismatch: llama.cpp uses GeGLU while the Candle port uses SwiGLU. The implementation must fix the first numerical divergence and prove readiness before Gemma 4 enters the execution pool.

For AMD acceleration, current `rs-llmctl` Candle execution fails closed because Candle 0.10.2 has no production ROCm/HIP device backend. The production path is therefore a HIP-enabled llama.cpp worker behind the existing OpenAI-compatible control plane. As of June 15, 2026, AMD documents ROCm 7.2.4 for production while 7.13.0 is a technology preview. Supported GPU, operating-system, kernel, and user-group checks must precede installation.

Kalosm informs the task-oriented streaming and structured-generation shape. LocalAI informs the small control-plane core, on-demand backend loading, declarative model gallery, and common API. `awesome-local-llm` is an asset-discovery source. None of these become an automatic dependency or trust source.

## Goals / Non-Goals

**Goals:**

- Let any agentic client whose adapter advertises planning, delegation, structured-result review, and continuation capabilities act as the supervisory planner.
- Delegate small, explicit implementation units to local models through a structured execution envelope.
- Route by language and demonstrated capability, not model branding alone.
- Keep Go, Rust, Python, and TypeScript specialists warm when memory permits and unload them predictably when it does not.
- Establish a reproducible Gemma 4 correctness gate using generated programs that print `1` through `10`.
- Provide a production AMD ROCm/HIP path with measured GPU offload and explicit fallback.
- Preserve local privacy, workspace security controls, audit events, and deterministic verification.

**Non-Goals:**

- Allowing local models to independently plan broad repository changes.
- Treating generated text quality as sufficient proof of correctness.
- Fine-tuning four specialist models in the first implementation phase.
- Replacing `rs-llmctl` with LocalAI, Kalosm, or another external control plane.
- Making the early Kalosm Fusor WGPU runtime a production dependency.
- Supporting every model or runtime listed in community-curated repositories.

## Decisions

### D1: Use a four-tier policy with the supervisory agent retaining control

The execution tiers are:

1. **Tier 0, deterministic tools:** formatting, compilation, tests, linters, static analysis, and scripted transforms.
2. **Tier 1, local specialist:** bounded single-language edits with explicit files and acceptance commands.
3. **Tier 2, local generalist:** cross-file but still bounded implementation when no healthy specialist qualifies.
4. **Tier 3, supervisory agent:** planning, ambiguous work, architecture, cross-language coordination, failed local retries, review, and final acceptance. The supervisor may be Claude, Codex, Gemini, Copilot, Pool, or another adapter that satisfies the capability contract.

The sommelier selects an executor tier, but the orchestrator owns the workflow state. The supervisory client is not replaced by a local routing decision; it creates and closes the delegated work unit. Codex is one supported supervisor profile, not a hard-coded dependency.

Alternative: let the sommelier route the whole user prompt directly to a specialist. Rejected because small models are weakest at scope control, ambiguity resolution, and recognizing when requirements changed.

### D2: Introduce a structured execution envelope

Each delegated work unit contains:

- stable task and parent-plan IDs;
- repository root and base revision;
- language and required model capabilities;
- allowed files and forbidden paths;
- concise objective and relevant context;
- acceptance commands with timeouts;
- maximum changed files, retries, tokens, and wall time;
- expected response schema containing summary, changed files, commands run, command results, and unresolved risks.

The local executor receives only the context needed for that unit. It cannot expand scope. A scope violation, malformed response, missing evidence, or changed base revision fails the unit and returns control to the supervisory agent.

Kalosm-style typed/structured generation is the API inspiration. Initial enforcement uses JSON Schema or grammar-constrained output over the existing OpenAI-compatible endpoint.

### D3: Verify execution outside the model

The model proposes edits; Milliways applies policy and runs deterministic checks. Success requires:

- changed paths are a subset of the envelope;
- the worktree diff is parseable and within limits;
- configured formatter/compiler/test commands exit successfully;
- security/output gates pass;
- the supervisory agent reviews the resulting diff and evidence.

The verifier runs in the existing workspace security boundary. A model statement such as "tests pass" is never accepted without captured command evidence.

### D4: Use declarative specialist profiles and measured qualification

A specialist profile records alias, language capabilities, artifact source and digest, license, runtime/backend, quantization, context size, memory estimate, warm policy, prompt template, output grammar, toolchain gates, benchmark score, and qualification state.

Initial candidate discovery may use LocalAI galleries and `awesome-local-llm`, but admission requires:

- compatible license and redistributability metadata;
- verified artifact checksum;
- successful language smoke tests;
- repository-edit benchmark results;
- latency and memory measurements on the target backend;
- no regression below the configured acceptance threshold.

One model may qualify for multiple languages. Four distinct weights are not required. Routing is capability-based.

### D5: Prefer `rs-llmctl` lifecycle; use llama-swap as an adapter where needed

`rs-llmctl` remains the source of truth for model inventory, policy, health, usage, and readiness. The local executor calls one OpenAI-compatible endpoint and selects an alias per work unit.

Hot-swap policy:

- keep the most recently used qualified specialist resident when capacity permits;
- optionally pin one generalist;
- use a configurable warm-set memory budget;
- unload least-recently-used specialists after TTL or before an allocation would exceed the budget;
- serialize loads per model alias and queue bounded requests during load;
- expose cold-load and warm-request latency separately.

Where `rs-llmctl` cannot yet provide process-level unload for a backend, a managed llama-swap backend may implement the worker lifecycle without changing the Milliways execution contract.

### D6: Gate Gemma 4 on numerical correctness and executable outputs

Work proceeds in `rs-llmctl` on `fix/gemma4-gguf-tokenizer`, starting with the confirmed GeGLU correction:

1. Confirm the exact GELU variant used by the checked-out llama.cpp and Candle 0.10.2.
2. Replace the Gemma 4 MLP SwiGLU activation with the matching GeGLU operation.
3. Run the ignored real-model generation test.
4. If output remains incoherent, compare layer-0 activations in the handoff order and fix the first divergence.
5. Remove all diagnostic instrumentation before commit.

Readiness has two gates:

- **numerical gate:** coherent, prompt-dependent generation and no unexplained material divergence from llama.cpp reference activations;
- **execution gate:** for each of Go, Rust, Python, and TypeScript, the model returns one minimal program that prints the decimal integers `1` through `10`, one per line, and the corresponding real toolchain compiles/runs it successfully.

Gemma 4 remains `quarantined` for delegated edits until both gates pass.

### D7: Use llama.cpp HIP for production AMD execution

The AMD backend sequence is:

1. Detect exact PCI device, GFX architecture, VRAM, OS, kernel, and current driver/runtime.
2. Compare them with AMD's production ROCm compatibility matrix.
3. Install a pinned production ROCm release using AMD's documented repository/package flow; do not select a technology preview by default.
4. Ensure the service user has required `render` and `video` access and can access `/dev/kfd` and `/dev/dri`.
5. Build or install llama.cpp with HIP support and record build/runtime versions.
6. Run `rocminfo` or equivalent runtime validation, enumerate the device, load a smoke model, and prove non-zero GPU offload through runtime metadata and memory/utilization evidence.
7. Benchmark prompt processing, decode throughput, cold load, warm load, and peak VRAM.

If support validation or offload proof fails, mark ROCm unavailable and select Vulkan when validated, otherwise CPU. The system must never label a request `rocm` solely because an AMD GPU exists.

### D8: Keep backend and asset interfaces open

The canonical internal contract is normalized chat, tool calls, structured output, model discovery, and health metadata. Backend adapters may speak OpenAI Chat Completions, Anthropic Messages, OpenAI Responses, or another explicitly supported protocol. Client-specific model-tier aliases and unsupported experimental headers are handled in the adapter, not leaked into orchestration.

The model profile format is backend-neutral. This allows evaluation of LocalAI backend images, `mistral.rs`, vLLM, SGLang, or Kalosm/Fusor without coupling the orchestration layer to their APIs. LocalAI's on-demand backend pattern is adopted conceptually: backend installation and activation are separate from the core daemon, versioned, health-checked, and removable.

### D9: Treat Gemma 4 artifact format as part of the profile

Gemma 4 profiles record checkpoint family and serving format:

- QAT GGUF for llama.cpp, Ollama, LM Studio, and HIP desktop/workstation serving;
- compressed-tensors `w4a16` for validated vLLM or SGLang server profiles;
- native/safetensors only where the runtime implementation is qualified.

The initial AMD workstation path uses GGUF with llama.cpp HIP. Candidate profiles prefer trusted, benchmarked quantizations and reject ad hoc conversions that fail quality gates. Context is sized from measured KV-cache headroom rather than the model's advertised maximum; initial smoke profiles use 8K, then expand to 16K or 32K only after memory and latency qualification.

The hello-rocm deployment material provides useful smoke commands (`amd-smi`, `test-backend-ops`, `-ngl 99`, explicit GFX targets), but production implementation pins official AMD and upstream runtime revisions rather than copying community prebuilt binaries without provenance checks.

### D10: Record decisions and outcomes as first-class telemetry

Each work unit emits:

- planner and executor identities;
- selected tier, language, model alias, backend, and reason;
- base revision and scope digest, not prompt or secret contents;
- queue, cold-load, prompt, decode, verification, and total durations;
- changed-file count and verifier command outcomes;
- retry, fallback, rejection, and supervisory-agent takeover reasons;
- GPU vendor, runtime, offload status, and peak memory where available.

Metrics must distinguish a model response from a verified successful edit.

### D11: Represent tiered work as a resumable workflow graph

The orchestrator represents each delegated unit as typed nodes:

`plan -> qualify -> prewarm -> execute -> verify -> review -> accept`

Failure edges lead to `repair`, `reroute`, or `supervisor-takeover`. Independent units may execute in parallel only when their allowed path sets do not overlap and their acceptance commands are compatible. Node state, inputs, outputs, and evidence are persisted so a daemon restart can resume without repeating an accepted edit.

This takes inspiration from LazyLLM's pipeline, parallel, switch, and loop primitives while retaining Milliways' existing workflow and conversation state. It is not a general low-code DAG engine.

### D12: Maintain a curated backend service catalog

Milliways maintains a small, versioned catalog of supported self-hosted service adapters. Each entry records protocols, model formats, accelerators, tool-call and structured-output support, health endpoints, lifecycle commands, and maturity. Community lists such as `awesome-llm-services` seed discovery, but only tested adapters enter the supported catalog.

## Risks / Trade-offs

- [Small models produce plausible but invalid edits] -> Require deterministic verification and supervisory-agent acceptance; cap retries at one by default.
- [Four resident models exceed VRAM/RAM] -> Route by capability, allow one model to cover several languages, enforce a warm-set budget, and use LRU/TTL unload.
- [Model load latency erases local speed gains] -> Track cold versus warm latency, prewarm the likely specialist after planning, and fall back when load exceeds the task budget.
- [ROCm support differs by exact GPU and OS] -> Validate against AMD's production matrix and fail closed to Vulkan/CPU.
- [Gemma 4 fixes overfit one prompt] -> Use reference activation comparisons plus executable multi-language fixtures and additional held-out prompts.
- [Community model assets change or disappear] -> Pin source revision, artifact digest, license metadata, and mirror policy before qualification.
- [Structured output grammar reduces model quality] -> Keep the schema minimal and allow one repair attempt before escalation.
- [Supervisory coordination costs more latency/tokens] -> Delegate only units whose estimated local savings exceed orchestration overhead; use Tier 0 tools for mechanical work.
- [Two repositories must evolve together] -> Version the execution envelope and readiness contract; land provider support before enabling Milliways routing.

## Migration Plan

1. Land Gemma 4 numerical fixes and readiness reporting in `rs-llmctl` without enabling delegation.
2. Add specialist profile schema, model qualification commands, and AMD backend validation.
3. Add the execution envelope, verifier, telemetry, and a shadow-mode local router in Milliways.
4. Run shadow mode to compare the proposed local route against supervisor-only outcomes without applying local edits.
5. Enable Tier 1 for the four `1..10` fixtures and then for opt-in repositories.
6. Expand to bounded real tasks after per-language quality, latency, and rollback thresholds are met.
7. Keep a single configuration switch that disables local execution and returns all work to the active supervisory client.

Rollback disables the local execution tier, drains queued work, leaves model inventory intact, and routes subsequent tasks to the active supervisory client. No repository migration is required.

## Open Questions

- Which candidate model or models meet all four language thresholds on the target AMD hardware?
- Should the first production hot-swap implementation be native `rs-llmctl` worker eviction or a managed llama-swap backend?
- What exact latency and quality thresholds make Tier 1 delegation cheaper than supervisor-only execution for each client?
- Which existing adapters can reliably emit and consume the execution envelope, and which need a compatibility shim?
- Should specialist outputs be unified diffs, edit operations, or direct file writes through a broker? The recommended first implementation is brokered edit operations because they are easier to validate than free-form patches.
- Which additional repository benchmark should follow the `1..10` fixture: bug fix, test generation, or constrained refactor?
