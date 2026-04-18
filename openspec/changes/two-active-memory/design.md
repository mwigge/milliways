## Context

Milliways is a kitchen orchestrator that manages conversations across multiple LLM backends. The `milliways-kitchen-parity` change established conversation state in a forked MemPalace (`mempalace-milliways`), giving milliways persistent memory of *how* conversations happen (turns, segments, checkpoints, runtime events).

However, milliways has no awareness of *what* is being worked on — the project context. Users switch between projects (company/private, or entirely different codebases), and conversations cannot access project-specific knowledge stored in separate MemPalace instances.

**Current state**:
- Conversation palace: `~/.milliways/palace` (mempalace-milliways fork)
- Project palaces: separate instances (e.g., `~/dev/src/docs_local/palace`, `~/projects/acme/.mempalace`)
- No cross-reference between them
- CodeGraph available but not integrated into milliways session context

**Stakeholders**:
- Open-source users who want milliways to work with their own project palaces
- Author with company/private mode separation
- Any user working across multiple repositories

## Goals / Non-Goals

**Goals:**
- Require a git repository context for milliways sessions (ensures CodeGraph availability)
- Auto-detect project palace from `.mempalace/` in repo root (optional, graceful degradation)
- Inject project context into conversations at turn boundaries
- Store palace-qualified citations that survive project switches
- Allow read-only access to non-active palaces when following citations
- Show project/repo status in TUI
- Support optional registry for access rules and aliases

**Non-Goals:**
- Merging conversation and project palaces into one (they remain separate)
- Write access to non-active project palaces
- Automatic project palace creation (user runs `mempalace init`)
- Real-time sync between palaces
- Multi-repo sessions (one active project at a time)

## Decisions

### D1: Repo is required, palace is optional

**Decision**: milliways requires a git repository context but project palace is optional.

**Rationale**: Even without `.mempalace/`, we get CodeGraph for symbol search, git history, and project identity. This makes milliways useful immediately in any repo. Palace adds semantic memory on top.

**Alternatives considered**:
- Palace required: Too much ceremony for quick use
- Neither required: Loses the anchor for project context

### D2: Project resolution by cwd walk

**Decision**: Walk up from cwd looking for `.git/`, then check for `.mempalace/` in repo root.

```
resolve_project(cwd):
  1. Walk up from cwd looking for .git/
     → found: repo_root = parent of .git/
     → not found: error (or use --project-root)
  2. Check {repo_root}/.codegraph/
     → found: load
     → not found: auto-init background index
  3. Check {repo_root}/.mempalace/
     → found: project_palace = path
     → not found: project_palace = nil
  4. Check ~/.milliways/projects.yaml for repo_root match
     → found: apply access rules, aliases
     → not found: use defaults
```

**Rationale**: Mirrors how git, codegraph, and other tools work. Zero config for the common case.

**Alternatives considered**:
- Explicit `--project` flag only: Too much typing
- Registry required: Too much ceremony

### D3: Palace-qualified citation handles

**Decision**: Citations in turn metadata include full palace identification:

```go
type ProjectRef struct {
    PalaceID   string    `json:"palace_id"`   // short name or hash
    PalacePath string    `json:"palace_path"` // absolute path
    DrawerID   string    `json:"drawer_id"`
    Wing       string    `json:"wing"`
    Room       string    `json:"room"`
    FactSummary string   `json:"fact_summary"`
    CapturedAt time.Time `json:"captured_at"`
}
```

**Rationale**: Citations must survive project switches. A drawer ID alone is meaningless without knowing which palace it came from.

**Alternatives considered**:
- Drawer ID only: Breaks on project switch
- Inline content copy: Bloats conversation state, goes stale

### D4: Read-only cross-palace access by default

**Decision**: Following citations to non-active palaces is allowed (read-only). No writes.

**Rationale**: Users expect to follow references. Blocking reads creates friction. Write restriction prevents accidental cross-contamination.

**Alternatives considered**:
- Block all cross-palace: Too restrictive
- Allow writes: Risk of confusion about which palace is being modified

### D5: Context injection at turn boundaries

**Decision**: After each user turn, query the active project palace and inject top-N relevant results into the context bundle.

```
on_user_turn(turn):
  1. Extract entities/topics from turn content
  2. If project_palace != nil:
     results = palace.search(topics, limit=3)
     turn.context_bundle.project_hits = results
     turn.metadata.project_refs = citations(results)
  3. If previous turns have citations to other palaces:
     optionally follow and include (read-only)
```

**Rationale**: Turn boundary is natural injection point. Top-N keeps context manageable.

**Alternatives considered**:
- Explicit `/recall` command: Extra friction
- Every message: Too chatty, context bloat

### D6: Segment records repo context

**Decision**: Each segment records the repo state at start:

```go
type RepoContext struct {
    RepoRoot         string `json:"repo_root"`
    RepoName         string `json:"repo_name"`
    Branch           string `json:"branch"`
    Commit           string `json:"commit"`
    CodeGraphSymbols int    `json:"codegraph_symbols"`
    PalaceDrawers    *int   `json:"palace_drawers"` // nil if no palace
}
```

**Rationale**: Enables "what repos have I worked on this week?" queries. Commit hash enables time-travel debugging.

### D7: Turn records repos accessed

**Decision**: Each turn records which repos were accessed:

```go
type RepoAccess struct {
    Repo           string   `json:"repo"`
    Access         string   `json:"access"` // "active" or "cited"
    Operations     []string `json:"operations"`
    CitationSource *string  `json:"citation_source"` // turn ID if cited
}
```

**Rationale**: Audit trail for cross-repo access. Enables `/repos` command.

### D8: Optional registry for access rules

**Decision**: `~/.milliways/projects.yaml` is optional. Only needed for non-default access rules.

```yaml
projects:
  company:
    paths:
      - ~/dev/src/ghorg
      - ~/dev/src/docs_local
    access:
      read: "project"  # restrict cross-reads
      write: "project"
  default:
    access:
      read: "all"
      write: "project"
```

**Rationale**: Most users don't need it. Power users can lock down access.

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| Breaking change: repo required | Clear error message with `--project-root` escape hatch |
| Performance: palace queries on every turn | Cache active palace connection; top-N limit; skip if no palace |
| Stale citations | Just-in-time verification: check drawer exists at read time |
| Cross-palace access confusion | TUI shows which palace each result came from; `/repos` lists all |
| Registry complexity | Registry is optional; defaults work for most users |
| CodeGraph init delay | Background indexing; show "indexing..." in status |

## Migration Plan

1. **Phase 1: Project resolution** — Add repo detection, fail gracefully if no repo
2. **Phase 2: CodeGraph integration** — Auto-init, status display
3. **Phase 3: Project palace** — Detection, context injection, citations
4. **Phase 4: TUI commands** — `/project`, `/repos`, `/palace`, `/codegraph`
5. **Phase 5: Cross-palace** — Citation following, access rules

Rollback: `--legacy-mode` flag to disable project awareness.

## Open Questions

1. **Citation verification frequency**: Every read, or only on explicit request?
2. **Context injection token budget**: How many tokens for project context before it crowds out conversation?
3. **Palace search ranking**: Use semantic similarity, recency, or hybrid?