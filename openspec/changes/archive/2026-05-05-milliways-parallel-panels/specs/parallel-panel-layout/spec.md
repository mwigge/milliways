## ADDED Requirements

### Requirement: Parallel panel layout renders an agent-deck-style split-pane dashboard

When `/parallel` launches, the system SHALL create a WezTerm split-pane layout consisting of: a left navigator pane (~30% terminal width) showing all slots, and right content pane(s) showing the live stream of the selected slot. A full-width status bar SHALL appear at the bottom of the navigator pane. The visual design SHALL match the agent-deck terminal aesthetic: dark background, monospace font, thin box-drawing borders around the slot list and selected slot, ANSI color restricted to what the existing milliways chat uses (no additional color palette).

#### Scenario: Layout opens with all slots in Running state

- **WHEN** `/parallel` successfully dispatches 3 slots
- **THEN** the system SHALL call `wezterm cli split-pane --percent 30 -- milliways attach --nav <group-id>` to create the navigator pane
- **AND** SHALL call `wezterm cli split-pane` for each slot to create content panes showing `milliways attach <handle>`
- **AND** the navigator pane SHALL display:
  ```
  milliways parallel — 3 slot(s)
  ──────────────────────────────
  ▶ 1  claude   Running   0s ago   0 tok
  ──
    2  codex    Running   0s ago   0 tok
  ──
    3  local    Running   0s ago   0 tok
  ──────────────────────────────
  3 running | 0 done | <group-id-short>
  1–3 select · Tab cycle · c consensus · q exit
  ```
- **AND** slot 1 SHALL be selected by default (bright border, `▶` indicator)

#### Scenario: Navigator updates slot status as agents complete

- **WHEN** a slot transitions from Running to Done
- **THEN** the navigator pane SHALL update that slot's row to show `Done` status and elapsed time
- **AND** the status bar SHALL update the running/done counts
- **AND** the slot's token counts SHALL be updated from the final `group.status` response

#### Scenario: User selects a different slot

- **WHEN** the user presses `2` in the navigator pane
- **THEN** the `▶` indicator and bright border SHALL move to slot 2
- **AND** the corresponding content pane SHALL be brought to focus in WezTerm

#### Scenario: Tab cycles through slots

- **WHEN** the user presses Tab in the navigator pane
- **THEN** the selection SHALL advance to the next slot (wrapping from last to first)

#### Scenario: q exits the parallel view

- **WHEN** the user presses `q` or Ctrl+d in the navigator pane
- **THEN** all `milliways attach` sub-processes SHALL be terminated
- **AND** the WezTerm panes SHALL be closed
- **AND** control SHALL return to the original calling chat session

### Requirement: milliways attach sub-command tails a session's output stream

The `milliways attach <handle>` sub-command SHALL connect to the daemon via UDS, subscribe to the session identified by `<handle>`, and print streamed content deltas to stdout as they arrive. It SHALL exit when the session completes or the connection is closed.

#### Scenario: Attach streams content deltas in real time

- **WHEN** `milliways attach <handle>` is run for a running session
- **THEN** it SHALL print each content delta to stdout as it is received from the daemon
- **AND** output SHALL be identical in format to normal chat streaming (base64-decoded, printed without buffering)

#### Scenario: Attach with --json flag emits NDJSON events

- **WHEN** `milliways attach --json <handle>` is run
- **THEN** each event SHALL be emitted as a single-line JSON object: `{"type":"delta","content":"...","ts":"..."}` or `{"type":"done","tokens_in":N,"tokens_out":N,"ts":"..."}`
- **AND** the navigator process SHALL parse these events to update slot status in the display

#### Scenario: Attach to completed session replays transcript

- **WHEN** `milliways attach <handle>` is run for a session that has already completed
- **THEN** it SHALL print the full transcript of that session and exit immediately with status 0

#### Scenario: Attach to unknown handle

- **WHEN** `milliways attach <handle>` is called with a handle that does not exist in the daemon
- **THEN** it SHALL print `unknown handle: <handle>` to stderr and exit with status 1

#### Scenario: Attach --nav mode for navigator pane

- **WHEN** `milliways attach --nav <group-id>` is run
- **THEN** the navigator SHALL render the slot list for the given group using the existing group.status polling loop
- **AND** SHALL respond to keyboard input (1–N, Tab, c, q) as specified in the layout requirement
- **AND** SHALL poll `group.status` every 500ms to refresh slot states

### Requirement: Global observability header bar shows token usage and quota across all active providers

A thin, persistent header pane SHALL span the full terminal width above all other panels in the parallel layout. It SHALL show per-provider token consumption, percentage of quota consumed, and a compact agent health overview. The header SHALL refresh every 2 seconds by polling the daemon's `status.get` and per-provider quota endpoints.

#### Scenario: Header renders token and quota data for each active provider

- **WHEN** the parallel layout is open with 3 active slots (claude, codex, local)
- **THEN** the header SHALL display one column per provider in the format:
  ```
  claude 12.4k tok  34% quota ● | codex 8.1k tok  12% quota ● | local 3.2k tok  — quota ●
  ```
- **AND** the `●` indicator SHALL be green when the provider is streaming, yellow when idle/done, red when errored
- **AND** providers without quota tracking (e.g., local) SHALL show `—` for the quota field

#### Scenario: Header quota bar changes color as limit approaches

- **WHEN** a provider's quota consumption exceeds 80%
- **THEN** the quota percentage SHALL be rendered in yellow
- **WHEN** it exceeds 95%
- **THEN** it SHALL be rendered in red

#### Scenario: Header shows cumulative group token total

- **WHEN** any slots are active
- **THEN** the right end of the header SHALL show the cumulative token total across all slots: e.g., `total: 23.7k tok`

#### Scenario: Header persists in headless fallback mode

- **WHEN** WezTerm is unavailable and headless mode is active
- **THEN** the header SHALL not be rendered (stdout has no pane concept)
- **AND** the consensus summary SHALL include a per-provider token and quota section instead

#### Scenario: Header pane is thin (1–2 rows)

- **WHEN** the terminal height is less than 24 rows
- **THEN** the header SHALL collapse to a single summary line: `parallel group <id> · N running · total Xk tok`
- **AND** per-provider breakdown SHALL be omitted to preserve usable screen space

### Requirement: Graceful fallback when WezTerm is unavailable

When `wezterm` is not on PATH or `TERM_PROGRAM` is not `WezTerm`, `/parallel` SHALL run in headless mode.

#### Scenario: Headless fallback prints progress to calling session

- **WHEN** `/parallel` is invoked outside a WezTerm session
- **THEN** the system SHALL print `[parallel] WezTerm not detected — running headless. Use: milliways attach <handle> to follow each slot.` 
- **AND** SHALL print each slot's handle so the user can attach manually
- **AND** SHALL wait for all slots to complete and then print the consensus summary inline
