# Design — milliways-provider-continuity

## D1: Conversation belongs to Milliways

Introduce a canonical conversation object behind every live block.

```go
type Conversation struct {
    ID             string
    BlockID         string
    UserPrompt      string
    Status          ConversationStatus
    Transcript      []Turn
    Memory          MemoryState
    ContextBundle   ContextBundle
    Segments        []ProviderSegment
    ActiveSegmentID string
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

type Turn struct {
    Role      string // user, assistant, system, tool
    Provider  string // claude, codex, gemini, opencode, milliways
    Text      string
    Timestamp time.Time
}

type MemoryState struct {
    WorkingSummary string
    OpenQuestions  []string
    ActiveGoals    []string
    Facts          []string
}

type ContextBundle struct {
    SpecRefs       []string
    CodeGraphText  string
    MemPalaceText  string
    FileContext    []string
    PriorOutputs   []string
}

type ProviderSegment struct {
    ID                string
    Provider          string
    NativeSessionID   string
    Status            SegmentStatus
    StartedAt         time.Time
    EndedAt           time.Time
    EndReason         string // done, failed, exhausted, cancelled
    ExhaustedResetsAt *time.Time
}
```

The key rule is:

- provider CLIs are execution engines
- `Conversation` is the durable state owner

## D1b: Observability is a first-class runtime model

Introduce a structured runtime event stream owned by Milliways.

```go
type RuntimeEvent struct {
    ID             string
    ConversationID string
    SegmentID      string
    BlockID        string
    Kind           string // route, provider_output, tool_use, failover, checkpoint, context_fetch, job, user_input
    Provider       string
    Payload        map[string]any
    At             time.Time
}
```

This event stream serves three roles at once:

1. transparency for the UI
2. replay/debuggability for operators
3. a central knowledge feed for continuity and future orchestration

The design goal is that Milliways should be able to answer:

- what was the provider doing
- what context was loaded
- what tool was running
- why did failover happen
- what state was checkpointed

without scraping terminal text after the fact.

## D2: Failover happens at segment boundaries, not block boundaries

The TUI block remains the visible task shell. A block may now contain multiple provider segments.

Example:

```text
Block b17
  Conversation conv-17
  Segment 1: claude   -> exhausted at 2026-04-14 22:00 Europe/Stockholm
  Segment 2: codex    -> active
```

This preserves the current UI model:

- same block
- same prompt
- same visible transcript
- same jobs/ledger/history placement

but changes the runtime semantics:

- adapter process dies
- conversation does not die

## D3: Orchestrator replaces one-shot dispatch

Current dispatch creates one adapter and drains it to completion. Replace that with a long-lived orchestrator.

```go
type Orchestrator interface {
    RunConversation(ctx context.Context, req StartRequest, sink EventSink) error
}

type StartRequest struct {
    Prompt       string
    ForcedKitchen string
    SessionName  string
}

type EventSink interface {
    Emit(evt adapter.Event)
    EmitSystem(text string)
}
```

Algorithm:

1. Build or load `Conversation`
2. Resolve initial provider
3. Start provider segment
4. Drain adapter events into:
   - TUI/headless output
   - transcript
   - memory state
   - checkpoint store
5. If provider finishes normally, mark conversation done
6. If provider is exhausted:
   - finalize current segment as `exhausted`
   - checkpoint conversation
   - choose next provider excluding exhausted one
   - build continuation prompt from conversation state
   - attach next provider in same block

Every step above should also emit `RuntimeEvent`s so the TUI, ledger, and jobs panel consume the same truth source.

## D4: Exhaustion detection must support structured and plain-text signals

Adapters must emit a normalized exhaustion event whenever they detect provider exhaustion.

Extend rate limit info:

```go
type RateLimitInfo struct {
    Status        string    // allowed, warning, exhausted
    ResetsAt      time.Time
    Kitchen       string
    IsExhaustion  bool
    RawText       string
    DetectionKind string // structured, stdout_text, stderr_text
}
```

Detection sources:

- Structured protocol events
- Stdout human-readable messages
- Stderr human-readable messages

Claude must detect messages like:

```text
You've hit your limit · resets 10pm (Europe/Stockholm)
```

These messages must be parsed into an absolute timestamp using the timezone named in the text when present. If the provider prints only a local clock time and timezone label, Milliways converts it to a concrete reset time for that date context.

## D5: Continuation prompt builder reconstructs provider context

When switching providers, Milliways cannot preserve hidden provider-internal state. It must reconstruct the next provider context from canonical conversation state.

Build a continuation payload with these sections:

1. Original user goal
2. Current progress summary
3. Full recent transcript window
4. Working memory summary
5. Active specs and design constraints
6. CodeGraph context snapshot
7. MemPalace recall snapshot
8. Open questions / next action
9. Explanation of why the provider was switched

Example shape:

```text
Continue an in-progress Milliways conversation.

Original goal:
...

Why you are taking over:
Previous provider claude became exhausted at 2026-04-14 22:00 Europe/Stockholm.

Current working memory:
...

## D6: Memory is typed, not generic

Milliways memory must be split into four explicit layers:

1. working memory
2. episodic memory
3. semantic memory
4. procedural memory

This is required because provider failover and long-lived continuity have different needs:

- working memory keeps an in-flight task moving
- episodic memory preserves what happened
- semantic memory preserves durable facts
- procedural memory preserves reusable operating rules

### D6.1 Working memory

Working memory lives inside the canonical `Conversation` and is the first input to continuation.

```go
type MemoryState struct {
    WorkingSummary string
    OpenQuestions  []string
    ActiveGoals    []string
    NextAction     string
}
```

Rules:

- provider-independent
- small enough to inject on continuation
- updated during execution, failover, and user intervention

### D6.2 Episodic memory

Episodic memory is the replayable record of what happened.

It is built from:

- transcript turns
- runtime events
- provider segments
- checkpoints
- ledger lineage

Rules:

- append-oriented
- replayable after restart
- sufficient to rebuild a coherent conversation timeline

### D6.3 Semantic memory

Semantic memory stores durable facts outside the current conversation.

Current likely backends:

- MemPalace for durable facts and semantic recall
- CodeGraph-derived stable repo context where appropriate

Rules:

- facts must pass an ingestion gate
- do not auto-promote arbitrary provider output
- support invalidation and dedupe

### D6.4 Procedural memory

Procedural memory stores operating knowledge such as:

- provider capability matrix
- continuation prompt rules
- routing/failover policy
- recipes
- spec constraints

This should primarily live in:

- specs
- config
- code-defined policy

not only in transcript-derived memory.

## D7: Long-lived memory requires an ingestion gate

Not all observed text should become durable memory.

Introduce a `MemoryIngestionPolicy` decision point:

```go
type MemoryCandidate struct {
    SourceKind   string // user, provider_output, tool_result, spec, repo_context
    MemoryType   string // semantic, episodic, procedural
    Text         string
    Scope        string // conversation, project, user
    Confidence   float64
    FreshUntil   *time.Time
}

type MemoryDecision struct {
    Accept bool
    Reason string
}
```

The policy should evaluate:

- provenance
- freshness
- duplication
- trust
- scope
- sensitivity

This keeps failover continuity useful without polluting long-lived memory with low-quality or hostile text.

## D8: Retrieval is type-aware and continuity-first

When a provider is swapped, retrieval order should be:

1. working memory from `Conversation`
2. relevant episodic transcript and runtime events
3. relevant specs and procedural constraints
4. semantic recall from MemPalace
5. repo/task context from CodeGraph

This avoids over-hydrating every continuation with everything known.

The system should prefer:

- smallest sufficient context
- most recent and relevant memory
- stable facts over noisy inferred text

## D9: Observability must explain memory behavior

Memory reads and writes should emit runtime events such as:

- `memory.retrieve`
- `memory.promote`
- `memory.reject`
- `memory.invalidate`

These events should make it possible to answer:

- what memory was loaded into a continuation
- what facts were promoted to semantic memory
- why a candidate memory was rejected
- which backend contributed which context

Relevant specs:
...

Relevant repository context:
...

Recent transcript:
...

Continue from the current state. Do not restart the task from scratch.
```

Provider-native session resume is still used when possible:

- same provider + session support -> use native resume
- different provider or no native resume -> use continuation payload

## D6: Context bundle reuses existing pantry assets

Use existing pantry integrations to rebuild context:

- CodeGraph provides codebase/task context
- MemPalace provides semantic memory recall
- Session persistence provides prior rendered transcript
- Ledger provides prior outcomes and lineage

This is not optional enrichment. It is part of the continuity contract.

Recommended context load order on failover:

1. current conversation checkpoint
2. recent transcript window
3. active spec files
4. codegraph task context
5. mempalace recall
6. recent file/tool outputs

Each load should emit a structured `context_fetch` event so the process map and later analytics can show exactly what was recovered.

## D7: Persistence model must store continuity, not just rendering

Current session persistence stores only completed block lines. Extend persistence to store conversation state.

```go
type PersistedConversation struct {
    ID           string                 `json:"id"`
    Prompt       string                 `json:"prompt"`
    Memory       MemoryState            `json:"memory"`
    ContextBundle ContextBundle         `json:"context_bundle"`
    Segments     []PersistedSegment     `json:"segments"`
    Transcript   []Turn                 `json:"transcript"`
}
```

Ledger also needs conversation-aware entries:

```go
type LedgerEntry struct {
    ...
    ConversationID string
    SegmentID      string
    SegmentIndex   int
    ParentSegmentID *string
    EndReason      string // success, failure, exhausted, cancelled
}
```

This enables:

- resume after Milliways restart
- visible provider lineage
- reporting on failover patterns
- audit trail derived from the same runtime event spine

## D8: TUI preserves blocks while exposing segment switches

The TUI should keep the current block-oriented UX, with a small amount of extra continuity visibility.

Block additions:

- active provider badge
- segment lineage badge (`claude -> codex`)
- system line when failover occurs
- continuity status in process map

Example lines:

```text
[milliways] claude exhausted at 22:00 Europe/Stockholm
[milliways] continuing in codex with restored conversation context
[milliways] restored context: transcript + specs + codegraph + mempalace
```

The upper-right ongoing tasks area remains block-based. Jobs or subagents can remain in their existing panel. This change should not force a UI redesign.

Jobs and future subagents should also hang off the same observability model:

- a job is a stream of structured runtime events
- a subagent is a child execution stream attached to a parent conversation or block

This keeps the lower-right jobs/subagent panel compatible with the continuity architecture instead of becoming a separate subsystem.

## D9: Provider capability matrix

Each adapter should expose continuity-relevant capabilities.

```go
type Capabilities struct {
    NativeResume      bool
    InteractiveSend   bool
    StructuredEvents  bool
    ExhaustionSignals bool
}
```

Expected initial matrix:

- `claude`: native resume yes, structured yes, exhaustion detection yes
- `opencode`: native resume yes, structured yes, exhaustion detection yes
- `codex`: native resume no, structured partial, exhaustion detection required
- `gemini`: native resume no, interactive no in prompt mode, exhaustion detection yes
- generic/future providers: best-effort text detection, no continuity guarantees until adapter added

This keeps the design extensible for more CLIs later.

## D10: Spec priority over tiered setup

Tiered CLI reporting remains useful, but it is downstream of continuity.

Order of implementation:

1. canonical conversation
2. exhaustion detection
3. orchestrator failover
4. persistence + ledger lineage
5. TUI continuity indicators
6. reporting improvements

If a tradeoff is required, continuity wins over tiered analysis and reporting.
