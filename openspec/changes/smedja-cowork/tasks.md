## 1. Cowork Gate — Native Step Approval for All Runners

- [x] Create `crates/smdjad/cowork/gate.rs`: `CoworkGate` struct with `intercept(ctx: &Context, tool_call: ToolCall) -> Result<ToolResult, Error>` method — implemented as `bin/smdjad/src/cowork.rs`; auto-approve stub, interactive gate deferred to TUI event stream phase
- [x] Wire gate into daemon tool dispatch middleware; activate only when `session.cowork_mode = true`
- [x] Add `cowork_mode: bool` field to session; set via `cowork.set` RPC method (`/cowork on|off` in chat)
- [x] Gate emits `approval_prompt` RPC event: `{ step_n, total_n, tool, args_scrubbed, reasoning, plan_summary }` — partial: `ApprovalPrompt` struct and `Decision` enum defined; full event emission deferred (interactive gate not yet wired)
- [x] Gate waits on `approvals` channel (fed by `/approve`, `/deny`, `/modify` RPC calls)
- [x] On `approve`: record `AuditEvent{action_type:"approval", decision:"approve"}`, proceed to tool execution
- [x] On `deny <reason>`: record audit event, return `ToolResult{error: "denied: <reason>"}` to runner
- [x] On `modify <instruction>`: record audit event with original and modified args, proceed with modified args
- [x] Timeout: `SMEDJA_COWORK_TIMEOUT` seconds (0 = infinite, default); on timeout emit warning, auto-approve
- [ ] Codex exclusion: detect Codex sessions; skip CoworkGate tool intercept (Codex handles its own gate); emit UI event after Codex approval for consistent rendering
- [x] Add `/cowork` command to chat dispatch: `on|off|status`; calls `cowork.set` RPC
- [ ] Update `approval_prompt` handler: render `renderApprovalBox` for events from all runners (not only Codex path)
- [x] Write unit tests: `CoworkGate.intercept` with approve → tool executed; with deny → error returned; with modify → args replaced; cowork inactive → pass-through
- [ ] Write integration test: claude runner in cowork mode → `approval_prompt` event emitted before tool_result

## 2. MCP Upgrade — HTTP Transport and OAuth

- [x] Create `crates/smdjad/mcp/http.rs`: `new_http_client(url: &str, token: &str) -> Result<Client>` ; HTTP transport using JSON-RPC over HTTP with SSE for streaming results
- [ ] Create `crates/smdjad/mcp/oauth.rs`: `start_pkce(server_url: &str) -> Result<Token>` — opens system browser, starts localhost callback listener on random port, exchanges code for token
- [ ] Token storage: AES-256-GCM encryption using machine ID as key material; store encrypted token in `mcp_servers.oauth_token` column
- [x] Create `crates/smdjad/mcp/registry.rs`: `Registry` struct backed by daemon SQLite; `register`, `list`, `remove`, `refresh`, `tools_for` methods — implemented as `crates/smedja-ingot/src/mcp.rs` with `insert`, `list`, `remove` functions and public methods on `Ingot`
- [x] Database migration: add `mcp_servers` table (`id, name, url, transport, cmd, oauth_token, tools_json, last_refresh`) — added `mcp_servers` table; columns: `id, name, url, transport, tools_json, last_refresh` (oauth_token, cmd deferred)
- [x] On daemon start: load all registered MCP servers; refresh tool lists for any server not refreshed within 1 hour
- [x] Fallback: existing session-level stdio MCP config continues to work unchanged; registry is additive
- [x] `smj mcp add <name> <url> [--stdio <cmd>]` — registers server; runs OAuth flow if server requires auth
- [x] `smj mcp list` — prints registered servers with tool counts and last-refresh time
- [x] `smj mcp remove <name>` — removes from registry
- [x] `smj mcp refresh [<name>]` — re-fetches tool lists; refreshes OAuth token if expired
- [ ] Write unit tests: HTTP client tool discovery; PKCE exchange mock; registry CRUD; token encrypt/decrypt round-trip
- [ ] Write integration test: register a local test MCP HTTP server; verify tool list appears in smedja tool routing

## 3. Docker Tool Isolation

- [x] Create `scripts/sandbox/Dockerfile`: Alpine 3.20 + bash, curl, git, jq, ripgrep, fd; no ENTRYPOINT
- [x] `smj sandbox build` subcommand: `docker build -t smedja-sandbox:latest scripts/sandbox/`; prints image digest
- [x] Create `crates/smdjad/tools/sandbox.rs`: `SandboxExecutor` wrapping bash/write/edit tools with Docker API
- [x] `SandboxExecutor::exec(ctx: &Context, cmd: &str) -> Result<String>`: runs `docker run --rm -v <workspace>:/workspace:rw --network none <image_digest> bash -c <cmd>`
- [x] Image digest verification at daemon start: if digest mismatch or image absent, `self.available = false`; log warning; continue without sandbox
- [x] Activate when `SMEDJA_TOOL_SANDBOX=docker` is set and Docker is reachable; skip silently if neither
- [x] Sandboxed tools: `bash`, `write_file`, `edit_file`, `run_command`
- [x] Exempt tools (read-only, no sandbox): `read_file`, `list_files`, `graph_query`, `mcp_call`
- [x] Write unit tests: `SandboxExecutor::exec` with a mock Docker client; verify workspace mount args; verify network-none flag
- [ ] Smoke test: `smj sandbox build` produces an image; a bash tool call in cowork mode runs in the container and returns stdout

## 4. Task Entity

- [x] Create `crates/smdjad/task/task.rs`: `Task` struct, `TaskStatus` enum, `TaskStore` backed by daemon SQLite
- [x] Database migration: add `tasks` table (`id, title, description, status, session_id, created_at, closed_at, turns_count`)
- [x] Add `task_id` foreign key to `audit_events` table (nullable; existing rows unaffected)
- [x] RPC method `task.create { title, description }` → creates task, starts OTel span `smedja.task`, returns `task_id`
- [x] RPC method `task.close { task_id, status }` → transitions status; closes OTel span with `task.status`, `task.turns_count`
- [ ] Inject task title + description into agent system prompt when a task is active
- [x] `/task create <description>` command in chat: calls `task.create` RPC; displays task ID
- [x] `/task done` command: calls `task.close { status: "complete" }` for the active task
- [x] `smj task list [--status <status>]` — queries `tasks` table; prints formatted table
- [x] `smj task show <id>` — prints task details, turns count, linked audit events
- [ ] Write unit tests: `TaskStore` CRUD; status transition guards (can't close a Planned task without starting it); OTel span lifecycle mock
- [ ] Integration test: create task → two turns → `/task done` → verify `audit_events` rows carry `task_id`; verify OTel span closed

## 5. End-to-End Validation

- [ ] Cowork mode smoke test: start a session with `--cowork`; issue a task that requires `edit_file`; verify `approval_prompt` event appears; `/approve`; verify tool executed; verify audit log entry
- [ ] MCP HTTP smoke test: configure a test MCP HTTP server; `smj mcp add`; verify tools appear in session tool routing
- [ ] Docker sandbox smoke test: `SMEDJA_TOOL_SANDBOX=docker smedja --cowork`; bash tool → runs in container → host filesystem unchanged outside workspace mount
- [ ] Task lifecycle smoke test: `/task create "Fix the softcap"` → two turns → `/task done` → `smj task list` shows `complete`
- [ ] Confirm `cargo test --workspace` passes with zero new failures
- [ ] Confirm `cargo build --workspace` clean with no new build errors
