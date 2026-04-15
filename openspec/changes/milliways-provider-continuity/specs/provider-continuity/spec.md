# Spec: provider-continuity

## Overview

Milliways MUST preserve a live conversation independently of any single provider CLI. When a provider becomes exhausted mid-task, Milliways MUST continue the same logical conversation with another provider in the same block.

## Requirements

### Canonical conversation ownership

- Every live block MUST have a Milliways-owned canonical conversation state
- The canonical conversation MUST outlive any single provider process
- The canonical conversation MUST store transcript, working memory, context bundle, and provider lineage

### Observability spine

- Milliways MUST maintain a structured runtime event stream for each live conversation
- The runtime event stream MUST be sufficient to explain routing, context loading, tool use, checkpointing, failover, and user intervention
- The TUI process map and related status surfaces SHOULD be driven from this structured event stream rather than ad-hoc state only
- The runtime event stream SHOULD be reusable as a central knowledge source for replay, debugging, and later orchestration features such as jobs or subagents

### Provider exhaustion handling

- Provider exhaustion MUST be treated as a recoverable continuity event, not only as a terminal failure
- When a provider becomes exhausted mid-task, Milliways MUST finalize the current provider segment and keep the block alive
- Milliways MUST attempt to continue the same conversation with the next eligible provider
- The user MUST NOT need to manually copy context into the next provider

### Continuation context

- On provider switch, Milliways MUST reconstruct provider-visible context from canonical conversation state
- The continuation context MUST include:
  - original task goal
  - recent transcript
  - working memory summary
  - active specs/design constraints
  - relevant repository context
  - relevant persistent memory recall
  - current next action
- The next provider MUST be instructed to continue the in-progress task rather than restart it

### Provider-native resume

- If the same provider supports native resume and a valid native session ID exists, Milliways MAY use native resume
- Native resume MUST be treated as an optimization, not as the only continuity mechanism
- Cross-provider continuity MUST work even when native provider resume is unavailable

### Same-block continuity

- A provider switch mid-task MUST remain within the same user-visible block
- The block MUST show continuity system messages when failover occurs
- The block MUST preserve prior transcript and output lines across the switch

### Persistence

- Persisted sessions MUST include enough canonical conversation state to resume a multi-provider conversation after Milliways restarts
- Ledger entries MUST record provider segments as part of one logical conversation

### Extensibility

- The continuity model MUST support adding more providers later without redesigning the conversation runtime
- Providers without native resume MUST still be able to participate via reconstructed continuation context
