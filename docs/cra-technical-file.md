# CRA Technical File

This document is the local evidence index for MilliWays Cyber Resilience Act
readiness. It is not a declaration of conformity by itself; it records where the
project keeps the evidence needed to make and review that claim.

## Product Scope

MilliWays is a local terminal and daemon that broker AI coding clients, local
model endpoints, observability, memory handoff, and security controls for a
developer workstation.

## Security Architecture Evidence

- `README.md`: user-facing architecture, security control plane, local model
  installation, observability, and secure-client behavior.
- `SECURITY.md`: vulnerability reporting and supported security surface.
- `SUPPORT.md`: support and maintenance contact expectations.
- `docs/update-policy.md`: update delivery and release evidence expectations.
- `internal/security/`: command firewall, startup scanning, output planning,
  CRA adapter, SBOM generation, rule packs, and client profile checks.
- `internal/daemon/runners/`: secure runner integration and HTTP tool-loop
  enforcement.

## Secure Defaults

MilliWays defaults to warn-mode visibility for interactive work, strict/CI modes
for fail-closed deployments, brokered command shims where available, local
workspace scoping, generated local API keys for the built-in local server, and
redacted security audit output.

## Scanner Coverage

CRA readiness expects evidence for dependency scanning, secret scanning, SAST,
and Go vulnerability scanning. A single installed scanner is partial evidence
only. The observability and security status surfaces should expose which scanner
classes are missing.

## Residual Risk

External CLI clients can execute tools inside their own process. MilliWays can
preflight their profile, set controlled environment variables, and broker command
paths when shims are active, but it does not claim full command control unless
the runner executes tools through the MilliWays HTTP tool registry.
