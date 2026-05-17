# Security Policy

## Reporting a vulnerability

Please do **not** open a public GitHub issue for security vulnerabilities.

Report security issues by emailing the maintainer directly. You can find
contact information via the GitHub profile at https://github.com/mwigge.

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Any suggested fix (optional)

You will receive a response within 7 days. We ask that you give us reasonable
time to address the issue before any public disclosure.

## Scope

- Credential handling and API key management
- Command allowlisting and shell injection
- Network request handling (MiniMax, Kimi, DeepSeek, local OpenAI-compatible
  HTTP runners, local endpoint selection, MCP client)
- Session store and SQLite data handling

## Runtime Tool Execution Model

MilliWays has two enforcement levels:

- HTTP tool-loop runners (`minimax`, `kimi`, `deepseek`, `local`) execute tools
  through the MilliWays registry. Command firewall checks, workspace scoping,
  output planning, and security audit records are applied before tool results are
  returned to the model.
- External CLI runners (`claude`, `codex`, `copilot`, `gemini`, `pool`) run
  their vendor CLI. MilliWays applies startup checks, client-profile preflight,
  controlled environment variables, and command shims when available, but the
  underlying CLI still owns any tool execution path that does not pass through a
  brokered shim.

Codex is launched with `--sandbox workspace-write` and
`--ask-for-approval on-request` unless the user has explicitly supplied a
different Codex flag set. This preserves writable project work while keeping
human approval in the default path.

For shared or automated deployments, use security mode `strict` or `ci`, enable
command shims, keep scanner coverage current, and treat `preflight-only` or
`unprotected` client labels as residual risk rather than full enforcement.
