# Tiered Local Execution Rollout

## Promotion Criteria

Tier 1 remains repository opt-in until the measured target backend and hardware
class pass all quality, latency, safety, and recovery thresholds. Qualification
is per language and task capability; failed capabilities are never routed.

The June 15, 2026 Gemma 4 ROCm benchmark remains unpromoted because the broader
bug-fix, test-creation, and constrained-refactor gates did not all pass.

## Rollback

Set the global or session rollout mode to `disabled`. Queued local units are
canceled, running units drain within their wall-time budget, and subsequent
work remains with the active supervisory client. Preserve workflow and command
evidence for diagnosis.

## Supported Services

The tested catalog includes `rs-llmctl`, llama.cpp, llama-swap, Ollama, LocalAI,
vLLM, SGLang, LM Studio, and `mistral.rs`. Only explicitly qualified adapters
and model/backend combinations may perform unattended coding execution.

## Model Sourcing

Local files and selected HTTPS catalogs are discovery sources, not trust roots.
Activation requires accepted license metadata, SHA-256 identity, source
provenance, hardware fit, runtime compatibility, security review, and measured
qualification on the target backend.

## Operator Controls

Operators can inspect, qualify, quarantine, and remove profiles; select shadow
or live mode for specific repositories; disable local execution globally or per
session; cancel stuck loads; and force supervisor takeover.
