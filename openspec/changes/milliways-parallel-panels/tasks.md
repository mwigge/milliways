## 1. SQLite Schema — ParallelGroup Tables

- [x] 1.1 Add `mw_parallel_groups` and `mw_parallel_slots` table DDL to `internal/pantry/schema.go` (version bump)
- [x] 1.2 Implement `ParallelStore` in `internal/pantry/parallel.go` with `InsertGroup`, `InsertSlot`, `UpdateSlotStatus`, `GetGroup`, `ListGroups` methods
- [x] 1.3 Add `ParallelStore` accessor to the pantry `DB` struct (alongside `CheckpointStore`, `JobStore`)
- [x] 1.4 Write unit tests for `ParallelStore` in `internal/pantry/parallel_test.go`

## 2. ParallelGroup Coordinator

- [x] 2.1 Create `internal/parallel/` package; define `Group`, `SlotRecord`, `SlotStatus` types in `internal/parallel/types.go`
- [x] 2.2 Implement `Dispatch` function in `internal/parallel/dispatch.go`: accepts prompt + providers, calls `agent.open` for each concurrently, stores group in `ParallelStore`, returns `DispatchResult{GroupID, Slots}`
- [x] 2.3 Wire MemPalace baseline injection into `Dispatch`: detect file path in prompt, call `kg_query` for prior findings, prepend synthetic context message per slot
- [x] 2.4 Implement daemon restart recovery in `internal/parallel/dispatch.go`: on daemon init, scan `mw_parallel_slots` for rows with `status=running` and mark them `interrupted`
- [x] 2.5 Write unit tests for `Dispatch` including provider-unavailable and MemPalace-down scenarios

## 3. Daemon RPC — parallel.dispatch, group.status, group.list

- [x] 3.1 Register `parallel.dispatch` RPC handler in `cmd/milliwaysd/` wiring; define request/response types in `internal/rpc/types.go` (or regenerate from proto if applicable)
- [x] 3.2 Register `group.status` RPC handler returning `ParallelGroup` with slot states
- [x] 3.3 Register `group.list` RPC handler returning up to 20 recent groups
- [x] 3.4 Write integration tests for all three RPC methods using the daemon test harness

## 4. milliways attach Sub-Command

- [x] 4.1 Create `cmd/milliways/attach.go` with Cobra sub-command `attach <handle>`
- [x] 4.2 Implement streaming output: dial daemon UDS, subscribe to handle, decode base64 deltas, print to stdout without buffering
- [x] 4.3 Add `--json` flag: emit NDJSON events `{type, content/tokens_in/tokens_out, ts}` per event
- [x] 4.4 Implement completed-session replay: if session is already Done, fetch transcript and print then exit 0
- [x] 4.5 Implement `--nav <group-id>` mode: render navigator display (slot list, keyboard handler, group.status polling loop) using raw ANSI
- [x] 4.6 Write unit tests for attach command flag parsing; write integration test for `--json` event format

## 5. MemPalace Post-Session Write Hook

- [x] 5.1 Create `internal/parallel/memwrite.go` with `ExtractFindings(text string) []Finding` using regex `^([\w./:-]+\.go):\s+(.+)$`
- [x] 5.2 Implement `WriteFindings` function: calls `kg_add` for each finding with `source`, `group_id`, `ts` properties via the existing MemPalace MCP client
- [x] 5.3 Register the post-session hook in the parallel coordinator: call `WriteFindings` when a slot transitions to Done
- [x] 5.4 Cap baseline injection at 20 most-recent prior findings; append truncation note when over limit
- [x] 5.5 Write unit tests for `ExtractFindings` with various agent output formats (file:finding, bulleted, narrative)

## 6. Consensus Aggregator

- [x] 6.1 Create `internal/parallel/consensus.go` with `Aggregate(groupID string, mp MemPalaceClient) (Summary, error)`
- [x] 6.2 Implement MemPalace query: `kg_query` with `subject_prefix=file:` and `predicate=has_finding` filtered by `group_id`
- [x] 6.3 Implement Jaccard token-overlap deduplication at threshold 0.65; make threshold a package-level const
- [x] 6.4 Implement confidence bucketing: count distinct source values per deduplicated finding → HIGH (≥3), MEDIUM (2), LOW (1)
- [x] 6.5 Implement `RenderSummary(s Summary) string` producing the structured text output matching the spec format
- [x] 6.6 Register `consensus.aggregate` RPC handler in daemon; plumb into `milliwaysctl parallel consensus <group-id>` command
- [x] 6.7 Wire auto-trigger: when last slot reaches Done, fire aggregator and update navigator status bar
- [x] 6.8 Write unit tests for deduplication logic, confidence bucketing, and render output format

## 7. Parallel Panel Layout — WezTerm Integration

- [x] 7.1 Implement `internal/parallel/layout.go` `Launch(result DispatchResult)`: detect WezTerm via `TERM_PROGRAM` env; if present, call `wezterm cli split-pane` to create navigator pane and N content panes
- [x] 7.2 Wire pane commands: navigator pane runs `milliways attach --nav <group-id>`; each content pane runs `milliways attach <handle>`
- [x] 7.3 Implement headless fallback in `Launch`: print slot handles + `milliways attach` instructions, poll group.status until all Done, print consensus inline
- [x] 7.4 Write integration test for headless fallback path (no WezTerm dependency)

## 8. Global Observability Header Bar

- [x] 8.1 Add `RenderHeader(slots []SlotRecord, quotas map[string]QuotaSummary, totalTokens int, termWidth int) string` to `internal/parallel/layout.go`
- [x] 8.2 Implement per-provider column: `<provider> <tokens>k tok  <pct>% quota ●` with color thresholds (>80% yellow, >95% red, running green, idle/done yellow, error red)
- [x] 8.3 Implement narrow-terminal collapse: when terminal height < 24 rows, collapse to single summary line
- [x] 8.4 Wire header refresh into the navigator's 500ms poll loop: re-render header line in place using ANSI cursor-up
- [x] 8.5 Wire quota data source: call existing pantry quota endpoint per provider to get `used/limit` for the current date
- [x] 8.6 Write unit tests for `RenderHeader` covering normal, narrow, and color-threshold cases

## 9. /parallel Slash Command Integration

- [x] 9.1 Add `/parallel` to the slash command dispatch table in `cmd/milliways/chat.go` (or the commands file)
- [x] 9.2 Parse `--providers <list>` flag and prompt remainder; validate provider names before calling `parallel.dispatch`
- [x] 9.3 Call `parallel.dispatch` RPC, then call `layout.Launch` with the result
- [x] 9.4 On layout exit, print consensus summary to the calling chat session
- [x] 9.5 Add `/parallel` to the slash command picker help text and `milliways --help` output
- [x] 9.6 Write integration test: `/parallel` with unknown provider aborts cleanly; with valid providers dispatches and returns group-id

## 10. milliwaysctl parallel Sub-Command

- [x] 10.1 Add `milliwaysctl parallel list` command: calls `group.list` RPC, renders table of recent groups
- [x] 10.2 Add `milliwaysctl parallel status <group-id>` command: calls `group.status` RPC, renders slot table
- [x] 10.3 Add `milliwaysctl parallel consensus <group-id>` command: calls `consensus.aggregate` RPC, prints summary to stdout
- [x] 10.4 Write unit tests for command flag parsing and output formatting
