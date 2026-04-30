# Tasks — two-active-memory

## 1. Project Resolution (Core)

- [x] 1.1 Create `internal/project/` package with ProjectContext struct
- [x] 1.2 Implement git repo detection: walk up from cwd looking for `.git/`
- [x] 1.3 Add `--project-root` flag to milliways CLI with validation
- [x] 1.4 Implement CodeGraph detection: check for `.codegraph/` in repo root
- [x] 1.5 Implement CodeGraph auto-init: trigger background indexing if missing
- [x] 1.6 Implement project palace detection: check for `.mempalace/` in repo root
- [x] 1.7 Unit tests: repo detection, flag override, graceful degradation
- [x] 1.8 Integration test: milliways startup with/without repo, with/without palace

## 2. Schema Changes (mempalace-milliways)

- [x] 2.1 Add RepoContext struct to segment schema (repo_root, branch, commit, codegraph_symbols, palace_drawers)
- [x] 2.2 Add RepoAccess struct (repo, access, operations, citation_source)
- [x] 2.3 Add ProjectRef struct (palace_id, palace_path, drawer_id, wing, room, fact_summary, captured_at)
- [x] 2.4 Add repos_accessed field to turn schema
- [x] 2.5 Add project_refs field to turn.metadata
- [x] 2.6 Migration: add new columns without breaking existing data
- [x] 2.7 Unit tests: schema round-trip, migration on existing conversation DB

## 3. Project Memory Bridge

- [x] 3.1 Create `internal/bridge/` package for project palace integration
- [x] 3.2 Implement topic extraction from user message (entity/keyword detection)
- [x] 3.3 Implement context injection: query palace at turn boundary, limit to top N
- [x] 3.4 Implement citation handle creation from palace search results
- [x] 3.5 Wire context injection into orchestrator turn flow
- [x] 3.6 Add `project_context_limit` to carte.yaml config
- [x] 3.7 Unit tests: topic extraction, context injection, citation creation
- [x] 3.8 Integration test: turn with palace results injected

## 4. Cross-Palace Citations

- [x] 4.1 Implement palace-qualified citation storage in turn metadata
- [x] 4.2 Implement cross-palace citation resolution (follow citation to non-active palace)
- [x] 4.3 Enforce read-only access for non-active palaces
- [x] 4.4 Implement conversation-scoped palace set (track all cited palaces)
- [x] 4.5 Implement just-in-time citation verification (check drawer exists at resolve time)
- [x] 4.6 Unit tests: citation resolution, read-only enforcement, stale citation handling

## 5. Optional Registry

- [x] 5.1 Define `~/.milliways/projects.yaml` schema (paths, access rules)
- [x] 5.2 Implement registry loader with defaults for missing file
- [x] 5.3 Implement access rule enforcement (read: all/project/none, write: project/none)
- [x] 5.4 Unit tests: registry parsing, access rule application

## 6. TUI Status Display

- [x] 6.1 Add project status to TUI header (project name, codegraph, palace)
- [x] 6.2 Implement compact status bar format for narrow terminals
- [x] 6.3 Track repos accessed per session in orchestrator state
- [x] 6.4 Display "indexing..." status during CodeGraph init
- [x] 6.5 Display "(none)" for missing palace with init hint
- [x] 6.6 Render tests: various project states display correctly

## 7. TUI Commands

- [x] 7.1 Implement `/project` command: display detailed project info
- [x] 7.2 Implement `/repos` command: list all repos accessed in conversation
- [x] 7.3 Implement `/palace` command: status, init, search subcommands
- [x] 7.4 Implement `/codegraph` command: status, reindex, search subcommands
- [x] 7.5 Add command parsing to TUI input handler
- [x] 7.6 Error handling: unknown subcommands, missing arguments
- [x] 7.7 Unit tests: command parsing, output formatting
- [x] 7.8 TUI render tests: command output displays correctly

## 8. Segment Repo Context

- [x] 8.1 Capture RepoContext when segment starts
- [x] 8.2 Store repo_context in segment record
- [x] 8.3 Query helper: unique repos from segments in date range
- [x] 8.4 Unit tests: segment with repo context, query repos by date

## 9. Turn Repo Tracking

- [x] 9.1 Track repos_accessed during turn execution
- [x] 9.2 Store project_refs from context injection in turn metadata
- [x] 9.3 Persist repos_accessed and project_refs with turn
- [x] 9.4 Unit tests: turn with multi-repo access, citation persistence

## 10. End-to-End Verification

- [ ] 10.1 E2E (manual): start milliways in repo with palace, verify context injection works — requires interactive TTY + MCP server
- [x] 10.2 E2E: start milliways in repo without palace, verify graceful degradation — smoke test `scripts/smoke.sh::TAM-10.2`
- [ ] 10.3 E2E (manual): `/project`, `/repos`, `/palace`, `/codegraph` commands work — requires interactive TTY
- [ ] 10.4 E2E (manual): citation to non-active palace resolves correctly — requires interactive TTY + MCP server
- [x] 10.5 E2E: segment and turn track repo context — unit tests: `TestOrchestratorStoresRepoContextOnSegmentStart`, `TestOrchestratorTracksReposAccessedAndProjectRefsPerTurn`, `TestSessionWriter_StartSegment_PassesRepoContext`, `TestSessionWriter_AppendTurn_PassesReposAccessedAndProjectRefs`
- [x] 10.6 E2E: error when starting outside any git repo — unit test `TestResolveProjectWithoutRepository`; smoke test `scripts/smoke.sh::TAM-10.6` has pre-existing issue (error message format mismatch)
- [ ] 10.7 E2E (manual): TUI status updates correctly as project state changes — requires interactive TTY

## 11. Documentation

- [x] 11.1 Update milliways README with project context features
- [x] 11.2 Document `~/.milliways/projects.yaml` schema and examples
- [x] 11.3 Document new TUI commands in README
- [x] 11.4 Add CHANGELOG entry for two-active-memory feature
