# Spec: memory-architecture

## Overview

Milliways MUST treat memory as a typed system owned by Milliways rather than by any single provider CLI. Memory MUST support provider failover, session replay, and long-lived project continuity without relying on hidden provider-internal state.

## Requirements

### Typed memory model

- Milliways MUST separate memory into at least these categories:
  - working memory
  - episodic memory
  - semantic memory
  - procedural memory
- Each stored memory item MUST have a declared memory type
- Different memory types MUST have different write and retrieval rules

### Working memory

- Working memory MUST represent the current in-flight state of a live conversation
- Working memory MUST include:
  - active goals
  - open questions
  - current progress summary
  - relevant live transcript state
  - current next action
- Working memory MUST be provider-independent
- Working memory MUST be used when reconstructing continuation context for a new provider

### Episodic memory

- Episodic memory MUST capture what happened in a conversation or run over time
- Episodic memory MUST include:
  - conversation transcript
  - runtime events
  - provider lineage
  - failovers
  - checkpoints
  - outcomes and decisions
- Episodic memory MUST be replayable into a coherent conversation timeline after restart

### Semantic memory

- Semantic memory MUST capture stable facts that may be reused across conversations
- Semantic memory MAY include:
  - project facts
  - durable user or team preferences
  - stable repo knowledge
  - accepted spec facts
- Semantic memory MUST NOT be populated from untrusted or low-confidence transient output without an ingestion gate

### Procedural memory

- Procedural memory MUST capture rules and reusable operating patterns
- Procedural memory MUST include:
  - routing and failover policy
  - continuation prompt rules
  - provider capability metadata
  - recipes and workflow patterns
- Procedural memory SHOULD be versioned with specs or configuration rather than inferred only from transcripts

### Memory ingestion gate

- Milliways MUST apply a write policy before persisting information into long-lived memory stores
- The write policy MUST consider:
  - source provenance
  - memory type
  - freshness
  - confidence
  - duplication
  - scope or ownership
- User-controlled or provider-generated text MUST NOT be promoted into semantic memory automatically without passing the write policy

### Retrieval policy

- Retrieval MUST be type-aware
- Provider continuation MUST prioritize:
  - working memory
  - relevant episodic transcript
  - relevant semantic facts
  - applicable procedural constraints
- Retrieval SHOULD prefer the smallest sufficient context needed to continue the task correctly
- Retrieval MUST be able to degrade gracefully when external memory systems are unavailable

### Continuity behavior

- Cross-provider failover MUST reconstruct context from Milliways-owned memory rather than assuming hidden provider-internal state is portable
- If provider-native resume exists, Milliways MAY use it as an optimization
- Native resume MUST NOT replace Milliways-owned memory reconstruction as the continuity baseline

### Observability and auditability

- Memory writes and retrievals SHOULD emit structured runtime events
- Milliways SHOULD be able to explain:
  - what memory was retrieved
  - why it was retrieved
  - what memory was written
  - why it was or was not promoted to long-lived storage

### Security and trust

- Persisted memory MUST be stored with private-by-default filesystem or database access controls
- Sensitive or untrusted text MUST NOT be written into long-lived semantic memory without policy checks
- Memory systems MUST support deduplication and invalidation of stale facts

### Extensibility

- The memory architecture MUST support adding new memory backends later without changing the canonical conversation model
- MemPalace, CodeGraph, and future memory providers MUST be treated as enrichers or backing stores, not as the sole owner of conversation memory
