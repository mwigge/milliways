# SigNoz Integration (Scope B — opt-in)

Milliways injects W3C `TRACEPARENT` and `OTEL_*` environment variables into every CLI subprocess (Claude Code, Codex CLI, etc.), so their native OpenTelemetry spans — `claude_code.interaction`, `claude_code.llm_request`, `claude_code.tool` — are parented to milliways' own dispatch spans and appear in a single unified trace hierarchy. Pointing `signoz_endpoint` in `carte.yaml` at a self-hosted [SigNoz](https://github.com/SigNoz/signoz) instance routes all traces and metrics from both layers to one backend.

## Configuration

Add two lines to `~/.config/milliways/carte.yaml`:

```yaml
[telemetry]
signoz_endpoint = "http://localhost:4318"
enhanced_tracing = false
```

| Field | Required | Description |
|---|---|---|
| `signoz_endpoint` | yes | OTLP HTTP base URL (milliways appends `/v1/traces` and `/v1/metrics` automatically) |
| `enhanced_tracing` | no | Set `true` to enable `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1` in subprocesses (beta) |

Restart the daemon after editing:

```bash
milliwaysctl daemon restart
```

## Running SigNoz locally

See the [SigNoz docker-compose quickstart](https://github.com/SigNoz/signoz/tree/main/deploy/docker) for the fastest local setup. Once the stack is up, the default OTLP HTTP port is `4318`.

## Service name filters

Each dispatched subprocess is tagged with a service name of the form `milliways-{agent_id}`. In the SigNoz service map, filter by:

| Pattern | Covers |
|---|---|
| `milliways-claude` | Claude Code CLI spans |
| `milliways-codex` | Codex CLI spans |
| `milliways-copilot` | GitHub Copilot CLI spans |
| `milliways-gemini` | Gemini CLI spans |
| `milliways-*` | All dispatched agents |

The milliways daemon's own spans (dispatch, routing, tool calls) appear under the service name set by `OTEL_SERVICE_NAME` in the daemon process environment, defaulting to `milliways`.

## Note on `enhanced_tracing`

`enhanced_tracing = true` sets `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1` in subprocess environments. This flag is beta in Claude Code and may export additional span attributes (prompt hashes, session metadata). Review Claude Code's privacy documentation before enabling in shared or production environments.
