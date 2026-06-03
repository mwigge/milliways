# Policy Operations Runbook

This runbook records release documentation contracts for policy, router, and governance operations. It is intentionally passive: it describes expected posture and evidence without prescribing service start, stop, or restart commands.

## External Database

External Postgres support is planned as a contracted server deployment option for shared policy and metrics deployments. The contract must preserve SQLite-compatible metric rollups and policy audit semantics, including raw, hourly, daily, weekly, and monthly rollups and the same workspace/client/session decision filters.

SQLite remains the local default. Postgres-backed deployments should keep local/offline behavior understandable and should not change the meaning of existing audit rows, accepted-risk records, or scanner posture summaries.

## Router Maturity

Router maturity controls should be observable before a deployment is called shared or server-ready:

- admission and backpressure decisions should be explicit, bounded, and attributable to capacity, policy, or downstream availability.
- timeout budgets should be defined for the inbound request, queue wait, downstream runner call, and response assembly.
- non-secret failure responses should preserve operator-useful classifications without echoing prompts, credentials, headers, or tool output.

## Governance Exports

AQE/OpenAI-compatible governance contract exports should provide policy and compliance consumers with a stable, non-secret view of control-plane posture. The export set should include policy decisions, scanner posture, router maturity posture, SBOM state, CRA evidence, accepted-risk metadata, and request/response classifications suitable for OpenAI-compatible model gateway environments.

Exports should be additive to existing CLI and terminal views. They should not require exposing raw prompts, secrets, credentials, or generated tool output to prove the control decision.
