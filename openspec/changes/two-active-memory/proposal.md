## Why

Milliways currently has a single conversation memory (mempalace-milliways fork) but no awareness of project context. Users working across multiple projects have no way to:
1. Get project-specific knowledge injected into conversations
2. Track which repositories they've worked on in a session
3. Follow citations across project boundaries when context switches

This creates a memory gap: the orchestrator knows *how* work is happening (turns, segments, checkpoints) but not *what* is being worked on (project decisions, designs, prior knowledge). Open-source users need this to work with their own project palaces, not just the author's company/private split.

## What Changes

- **Repo-anchored sessions**: milliways requires a git repository context (or explicit `--project-root`), ensuring CodeGraph is always available
- **Optional project palace**: if `.mempalace/` exists in repo root, it becomes the active project memory; graceful degradation if absent
- **Project resolution flow**: walk up from cwd for `.git/`, auto-init CodeGraph, optionally load project palace
- **Citation handles**: turn metadata stores palace-qualified citations that survive project switches
- **Cross-palace reads**: follow citations to non-active palaces (read-only by default)
- **TUI visibility**: show active project, codegraph stats, palace stats, and repos accessed in session
- **New commands**: `/project`, `/repos`, `/palace`, `/codegraph` for project context management
- **Optional registry**: `~/.milliways/projects.yaml` for access rules and project aliases (not required)

## Capabilities

### New Capabilities

- `project-resolution`: Resolve active project from cwd, load CodeGraph (required) and project palace (optional)
- `project-memory-bridge`: Query active project palace at turn boundaries, inject context, store citations
- `cross-palace-citations`: Palace-qualified citation handles that resolve across project switches
- `project-tui-status`: TUI display of active project, codegraph stats, palace stats, repos accessed
- `project-commands`: `/project`, `/repos`, `/palace`, `/codegraph` commands for project inspection

### Modified Capabilities

- `conversation-segments`: Add `repo_context` field (repo_root, branch, commit, codegraph/palace stats)
- `conversation-turns`: Add `repos_accessed` field tracking which repos were queried per turn

## Impact

- **milliways core**: `internal/substrate/`, `internal/orchestrator/`, `internal/tui/`
- **mempalace-milliways**: conversation schema changes (segment.repo_context, turn.repos_accessed)
- **Configuration**: new `~/.milliways/projects.yaml` (optional), carte.yaml project settings
- **Dependencies**: requires CodeGraph MCP to be available; project palace MCP optional
- **Breaking**: milliways will error if not run from a git repository (unless `--project-root` specified)