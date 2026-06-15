## ADDED Requirements

### Requirement: ROCm eligibility is validated against production support
The system SHALL detect the exact AMD GPU, GFX architecture, VRAM, operating system, kernel, driver, and ROCm runtime and SHALL compare them with AMD's production compatibility information before selecting ROCm.

#### Scenario: Hardware is outside the supported matrix
- **WHEN** the detected GPU and operating-system combination is unsupported by the selected production ROCm release
- **THEN** the system SHALL mark ROCm unavailable and explain the incompatible fields

### Requirement: Production ROCm is pinned and installed explicitly
The installation plan SHALL select a production ROCm release by default, register the documented AMD repositories or packages for the detected operating system, and SHALL NOT select a technology-preview release without explicit operator choice.

#### Scenario: Production and preview releases are available
- **WHEN** AMD publishes both a production release and a newer technology preview
- **THEN** the default install plan SHALL choose the production release and report the preview only as an opt-in option

### Requirement: Device access is verified
The system SHALL verify service-user membership and access required for `/dev/kfd`, `/dev/dri`, and the `render` and `video` groups before starting a ROCm worker.

#### Scenario: Service user lacks device access
- **WHEN** runtime packages are installed but the service user cannot access the required devices
- **THEN** the ROCm health check SHALL fail with a remediation plan and SHALL NOT claim GPU readiness

### Requirement: The production AMD worker uses a HIP-capable runtime
Until the native Candle path has a validated ROCm backend, the production AMD execution plan SHALL use a HIP-enabled llama.cpp worker behind the existing OpenAI-compatible control plane.

#### Scenario: Candle ROCm is requested before support exists
- **WHEN** an operator requests native Candle ROCm execution on a build without validated ROCm device support
- **THEN** the system SHALL fail closed or select the configured HIP llama.cpp backend rather than silently using CPU

### Requirement: GPU offload is proven at runtime
The system SHALL verify that an AMD worker enumerates the intended GPU and performs non-zero model offload using runtime metadata plus device memory or utilization evidence.

#### Scenario: Worker starts but performs CPU-only inference
- **WHEN** a worker reports healthy HTTP status but runtime evidence shows zero GPU offload
- **THEN** the system SHALL mark the ROCm backend degraded or failed and SHALL NOT label requests as ROCm-accelerated

#### Scenario: HIP backend build is qualified
- **WHEN** a llama.cpp HIP worker is built for the detected GFX architecture
- **THEN** the system SHALL run backend operation tests and a real Gemma 4 smoke request before marking the build qualified

### Requirement: AMD capacity planning includes measured budgets
The system SHALL measure model load memory, KV-cache and graph overhead, cold-load latency, warm latency, prompt throughput, decode throughput, and peak VRAM before qualifying a model profile for the AMD host.

#### Scenario: Model fits weights but not runtime overhead
- **WHEN** model weights fit in VRAM but measured runtime overhead exceeds the safety budget
- **THEN** the system SHALL reduce context, choose a smaller quantization or model, or fall back without overcommitting memory

### Requirement: Fallback is explicit and observable
The system SHALL select a validated Vulkan backend when ROCm is unavailable and Vulkan is healthy, otherwise CPU, and SHALL record the selected backend and fallback reason.

#### Scenario: ROCm validation fails and Vulkan passes
- **WHEN** ROCm cannot be qualified but Vulkan passes its smoke and offload checks
- **THEN** the model SHALL run on Vulkan and telemetry SHALL record the ROCm failure and Vulkan selection
