## ADDED Requirements

### Requirement: Gemma 4 remains quarantined until numerically correct
The system SHALL mark the local Gemma 4 model quarantined for delegated code execution until the native implementation produces coherent prompt-dependent output and resolves material numerical divergences from the llama.cpp reference path.

#### Scenario: Generation remains incoherent
- **WHEN** the real Gemma 4 generation test produces incoherent or garbage output
- **THEN** Gemma 4 SHALL remain unavailable to the local execution router

### Requirement: Gemma 4 FFN matches the reference architecture
The Gemma 4 native implementation SHALL use the GELU-gated GeGLU feed-forward activation and GELU variant demonstrated by the checked-out llama.cpp reference implementation.

#### Scenario: Activation fix is evaluated
- **WHEN** the MLP activation is changed from SwiGLU to the matching GeGLU operation
- **THEN** the ignored real-model generation test SHALL run and its output SHALL be recorded for comparison

### Requirement: Numerical debugging follows first-divergence analysis
If the GeGLU correction does not satisfy readiness, the implementation SHALL compare equivalent activation summaries against llama.cpp in dependency order and fix the first material divergence before investigating later tensors.

#### Scenario: Layer zero diverges after attention
- **WHEN** early tensors match but a later layer-zero tensor differs materially
- **THEN** investigation SHALL focus on the first differing operation and SHALL NOT infer correctness from downstream tensors

### Requirement: Readiness includes four executable language fixtures
Gemma 4 SHALL generate minimal Go, Rust, Python, and TypeScript programs that print the decimal integers `1` through `10`, one integer per line, with no extra output.

#### Scenario: Go fixture passes
- **WHEN** Gemma 4 generates the Go fixture
- **THEN** the system SHALL compile or run it with the configured Go toolchain and compare standard output exactly with the canonical ten-line output

#### Scenario: Rust fixture passes
- **WHEN** Gemma 4 generates the Rust fixture
- **THEN** the system SHALL compile and run it with the configured Rust toolchain and compare standard output exactly with the canonical ten-line output

#### Scenario: Python fixture passes
- **WHEN** Gemma 4 generates the Python fixture
- **THEN** the system SHALL run it with the configured Python interpreter and compare standard output exactly with the canonical ten-line output

#### Scenario: TypeScript fixture passes
- **WHEN** Gemma 4 generates the TypeScript fixture
- **THEN** the system SHALL type-check or execute it with the configured TypeScript toolchain and compare standard output exactly with the canonical ten-line output

### Requirement: Readiness evidence is reproducible
The system SHALL record model artifact digest, runtime revision, prompt template, sampling parameters, toolchain versions, generated source, command results, and output comparison for every Gemma 4 readiness run.

#### Scenario: A readiness result is reviewed later
- **WHEN** an operator inspects a past readiness run
- **THEN** the evidence SHALL identify the exact model, runtime, prompts, generated files, and verification commands used

### Requirement: Gemma 4 profiles identify quantization and runtime compatibility
The system SHALL record whether a Gemma 4 artifact is GGUF, compressed-tensors, mobile, or native weights and SHALL route it only to a runtime qualified for that format.

#### Scenario: QAT GGUF is selected for AMD workstation serving
- **WHEN** a Gemma 4 QAT GGUF profile is activated on a qualified AMD workstation
- **THEN** the system SHALL route it to a qualified llama.cpp HIP-compatible backend and SHALL apply the profile's measured context and memory limits

#### Scenario: Compressed-tensors checkpoint is selected
- **WHEN** a Gemma 4 `w4a16` compressed-tensors profile is activated
- **THEN** the system SHALL require a qualified vLLM or SGLang backend and SHALL NOT pass the artifact to a GGUF-only worker

### Requirement: Diagnostic code is not shipped
Temporary activation dumps, ignored diagnostic tests, and debug logging used during numerical comparison SHALL be removed or feature-gated before a production commit.

#### Scenario: Gemma 4 fix is prepared for commit
- **WHEN** the implementation is ready to commit
- **THEN** formatting, clippy, library tests, and the real-model readiness test SHALL run without unguarded diagnostic instrumentation
