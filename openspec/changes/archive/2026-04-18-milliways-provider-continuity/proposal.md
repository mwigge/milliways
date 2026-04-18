# milliways-provider-continuity — Provider Failover Without Losing the Conversation

> Milliways should own the meal.
> Kitchens can change mid-course.
> The table, the order, the notes, and the memory stay put.

## Why

Milliways currently treats each CLI kitchen as the owner of its live session. That breaks down at the exact moment the user most needs resilience: when a provider hits a quota or context limit mid-task.

Today, if `claude` reaches a limit such as:

```text
You've hit your limit · resets 10pm (Europe/Stockholm)
/upgrade to increase your usage limit.
```

the live work effectively freezes. The block remains visible, but the actual thinking loop, gathered context, and momentum stop with the provider process.

That is not acceptable for Milliways as a primary AI workspace.

The higher-priority requirement is:

1. A live block must survive provider exhaustion.
2. The conversation must continue in the same block with another provider.
3. Milliways must preserve its own canonical transcript, working memory, specs, and gathered context across the switch.
4. The existing workspace model should remain: blocks, ongoing tasks, ledger/history, jobs, prompting, and TUI flow stay recognizable.

This priority is higher than completing tiered-setup analysis or reporting.

## What Changes

### Canonical conversation runtime

Introduce a Milliways-owned `Conversation` model behind each block. The conversation becomes the source of truth, not the active provider CLI.

The conversation stores:

- Full transcript of user + provider-visible messages
- Working memory summary
- Retrieved context bundle (CodeGraph, MemPalace, specs, file context, prior outputs)
- Active goals, open questions, and pending actions
- Provider lineage (`claude -> codex -> gemini`)
- Provider-native session IDs when available
- Handoff checkpoints for recovery and resume

### Observability as the knowledge spine

Milliways should use observability as a transparent internal event spine, not only as logging.

Every meaningful runtime event should be capturable as structured data:

- provider output events
- tool-use events
- routing decisions
- failover decisions
- checkpoint writes
- context retrieval from CodeGraph and MemPalace
- job/subagent lifecycle events
- user interventions (answers, confirms, context injection)

This gives Milliways a central node of knowledge it can build on for:

- continuity reconstruction
- live process-map transparency
- better ledgering
- jobs/subagent panels
- replay/debugging
- future analytics and tuning

### Mid-block provider failover

When a provider becomes exhausted mid-dispatch, Milliways must:

1. Detect exhaustion from structured events or plain terminal text
2. Freeze the provider segment, not the block
3. Write a checkpoint from canonical conversation state
4. Select the next eligible provider
5. Start a continuation segment in the same block
6. Reconstruct the next provider's context from the conversation checkpoint

The user sees one continuous task, not multiple unrelated dispatches.

### Conversation continuity over provider-native continuity

Provider-native resume remains useful but becomes optional optimization:

- If the same provider is available and supports resume, Milliways may reuse its native session
- If the provider changes, Milliways reconstructs context from canonical state
- The conversation must remain intact even when native provider session state is unavailable

### Workspace-preserving UI

The current workspace interaction model remains:

- Blocks stay the main task container
- Ongoing tasks remain visible
- Ledger/history remain visible
- Jobs panel remains
- Prompting and chat flow remain

The visible change is that a block may contain multiple provider segments over time while still reading as one continuous session.

## Capabilities

### New Capabilities

- `provider-continuity`: canonical conversation model, provider lineage, failover checkpoints, and same-block continuation after provider exhaustion
- `exhaustion-detection`: detect provider limits from both structured protocol events and human-readable CLI output

### Modified Capabilities

- `dialogue-adapters`: adapters now emit exhaustion signals suitable for failover, not only quota display
- `session-model`: persisted sessions must include conversation state and provider lineage, not only rendered output lines
- `quota-gated-routing`: failover is no longer "next dispatch only"; exhaustion may trigger immediate continuation within the current block
- `feedback-loop`: ledger must attribute multiple provider segments inside one logical conversation
- `dispatch-presence`: process-map and block UI should read from structured runtime events rather than ad-hoc state

## Impact

### New Packages

- `internal/conversation/` — canonical conversation state, checkpoints, continuity builder
- `internal/orchestrator/` — live dispatch orchestrator that can attach/detach providers from the same block

### Modified Packages

- `internal/kitchen/adapter/` — provider exhaustion detection, provider capabilities metadata
- `internal/tui/` — block model gains provider segments and continuity status
- `internal/pantry/` — persist conversation checkpoints, lineage, and segment ledgering
- `internal/sommelier/` — choose next provider for continuation with exclusion rules
- `cmd/milliways/` — wire TUI/headless dispatch through the orchestrator

## Conflicts / Supersedes

This change supersedes the current `quota-gated-routing` failover assumption that exhaustion only affects the next dispatch and that there is no mid-stream kitchen switching.

Where the current workspace spec says:

- current dispatch completes or fails
- quota store is updated
- next dispatch routes around the kitchen

this change replaces that with:

- current provider segment ends
- block stays alive
- same logical conversation continues with the next provider

## Success Criteria

Milliways is successful when a user can start a task in one provider, hit a real limit, and continue in the same block with another provider while preserving:

- transcript
- gathered context
- retrieved specs
- working memory
- visible task history
- ledger trail

without manual copy/paste or manual re-prompting.
