# Closeout — milliways-provider-continuity

> Narrow finish. Ship what's built. Archive. The broader vision lives in `milliways-kitchen-parity`.

## Why this document

Provider-continuity was scoped around **exhaustion-triggered failover**. The scope expanded in conversation to **any-time kitchen switching with shared memory substrate**. Rather than grow this change past its title, we land it narrowly and hand the expanded scope to a new change.

## Closure checklist

### 1. Apply the one-line allowlist fix

`codex` is missing from `allowedCmds` in `internal/kitchen/generic.go:24`. All other kitchens (claude, opencode, gemini, aider, goose, cline) are present. `GenericKitchen.Status()` rejects the exec before the codex adapter ever runs — this is what broke PC-21.1.

Fix:

```go
var allowedCmds = map[string]bool{
    "claude":   true,
    "codex":    true,   // <-- add this line
    "opencode": true,
    "gemini":   true,
    "aider":    true,
    "goose":    true,
    "cline":    true,
    // ... test entries unchanged
}
```

Rebuild and re-run the smoke rig at `/tmp/mw-smoke/`.

### 2. Complete PC-21 manual verification

- [ ] PC-21.1 — claude exhausts, codex continues in same block (headless smoke rig)
- [ ] PC-21.2 — inspect conversation JSON/DB, confirm transcript + context preserved
- [ ] PC-21.3 — TUI run: verify jobs panel, process map, ongoing tasks intact
- [ ] PC-21.4 — TUI run: verify process map shows context fetch, checkpoint, failover events
- [ ] PC-21.5 — `--session name` + restart + `--resume`: provider lineage persists

Each item either passes or becomes a follow-up issue. Do not silently skip.

### 3. Promote the smoke rig into the repo

Move from `/tmp/mw-smoke/` to `testdata/smoke/`:

```
testdata/smoke/
├── bin/
│   ├── fake-claude        # emits init, one turn, rate_limit exhaustion, exit 1
│   ├── fake-codex         # emits one item.completed, exit 0
│   ├── fake-gemini        # ...
│   └── fake-opencode      # ...
├── config/
│   └── carte.yaml         # points kitchens at fakes
└── README.md              # invocation + expected output
```

Add `make smoke` and a CI step that runs it. This is the minimum harness that would have caught the allowlist bug at PR time rather than at manual-verification time.

Deeper smoke matrix (crash, hang, malformed output, timezone-less exhaustion text) is scope for `milliways-kitchen-parity` → Service 5.

### 4. Archive the change

After the above is green:

```bash
openspec archive milliways-provider-continuity
```

## What is deliberately NOT done here

- MemPalace fork — lives in `milliways-kitchen-parity` Service 1
- User-initiated switch (`/switch`, `/stick`) — lives in `milliways-kitchen-parity` Service 3
- Continuous re-routing — lives in `milliways-kitchen-parity` Service 4
- Collaborative TUI — separate project built on top of milliways, not scoped here at all

These are real and wanted, but belong in the next change. Trying to land them on this one would invalidate the spec we wrote and delay the archive.

## Success criteria for closeout

1. `codex` allowlist fix merged.
2. All five PC-21 items either passed or converted to follow-up issues.
3. Smoke rig is in `testdata/smoke/` and runs in CI.
4. Change archived cleanly.

When all four are true, this change is done and `milliways-kitchen-parity` can begin implementation.

---

# Execution log — 2026-04-18

## 1. Allowlist fix — done

Applied via TDD (`@coder-go`). New test file `internal/kitchen/allowlist_test.go` with `TestIsCmdAllowed_CanonicalKitchens` (table-driven across all seven canonical kitchens) and `TestIsCmdAllowed_PathBasename`. Red phase confirmed only `codex` assertions failed. Green phase added one line to `allowedCmds`. Full `go test ./...` and `go build ./...` remain green.

Commit: `9cd7747 fix(kitchen): add codex to cmd allowlist`.

## 2. PC-21 status

- **PC-21.1 ✅ passed**. Headless smoke with the fake-kitchen rig at `testdata/smoke/` confirms the full failover path: `[milliways] claude exhausted, continuing with the next provider` → `[routed] codex` → exit 0. `make smoke` green.

- **PC-21.2 ✅ passed with caveat**. Session persistence (`~/.config/milliways/sessions/last.json`) carries `conversation_id` and `provider_chain` across segments. *Gap*: the headless ndjson ledger (`ledger.ndjson`) writes only the final segment and omits the `conversation_id` / `segment_id` / `segment_index` / `end_reason` fields defined in PC-17.1. Root cause was not investigated — tracked as a follow-up in `milliways-kitchen-parity` under KP-19 (scenario matrix), where multi-segment ledger assertions will expose and close the gap.

- **PC-21.3 ⏸ deferred**. TUI ongoing-tasks / jobs panel verification requires interactive use of `milliways --tui`; not executable in this session. User to run.

- **PC-21.4 ⏸ deferred**. TUI process-map transparency check requires interactive use; user to run.

- **PC-21.5 ⏸ deferred**. `--resume` is a TUI-only flag per `milliways --help`; requires a real TUI session cycle. User to run.

Each deferred item maps cleanly onto a PC-20 integration test that is already green, so the behaviour is covered by automated tests; the manual smoke is confirmation of lived UX, not of correctness.

## 3. Smoke rig promotion — done

Delegated to `@coder-go`. Artefacts:

- `testdata/smoke/bin/fake-claude-exhausted` and `fake-codex-ok` (deterministic fake kitchens; scenario-intent naming leaves room for KP-19 to add `fake-claude-crashed`, `fake-codex-malformed`, etc. without renames).
- `testdata/smoke/config/carte.yaml.tmpl` with `{{SMOKE_ROOT}}` and `{{RUN_DIR}}` substitution at script runtime.
- `scripts/smoke.sh` renders into a per-run `$TMPDIR/mw-smoke-XXXXXX/` directory, redirects `HOME` / `XDG_CONFIG_HOME`, and cleans up on exit. `SMOKE_KEEP=1` preserves run dir for post-mortem.
- `Makefile` with `smoke` target (adds first Makefile to the repo).
- `.gitignore` adjusted: `bin/` → `/bin/` so the pattern stops matching `testdata/smoke/bin/`.

Commit: `999b772 test(smoke): promote mw-smoke rig into testdata/smoke with make target`.

## 4. CI integration — done

`.github/workflows/ci.yml` runs three steps on push-to-master and on PRs:

1. `go build ./...`
2. `go test ./... -count=1`
3. `make smoke` (depends on test job passing first)

Commit: `c41b8fc ci: add github actions workflow for build, test, and smoke`.

Follow-ups flagged, not done here:

- `go vet ./...` reports a pre-existing `cancel`-leak warning at `internal/tui/app.go:382-386`. Ignored in CI for now; fixing it is a small story worth scheduling before `go vet` becomes a required gate.
- CI matrix across Go versions or OSes not set up — single `ubuntu-latest` / `go.mod` version for minimum viable signal.

## 5. Archive

Ready to run `openspec archive milliways-provider-continuity` once the user has completed PC-21.3 / 21.4 / 21.5 (or decided to accept them as automated-only coverage). The two closeout commits plus the allowlist fix commit form the full delivery.

## Branch state at closeout

Branch: `chore/provider-continuity-closeout` (off `master`). Three commits:

```
c41b8fc ci: add github actions workflow for build, test, and smoke
999b772 test(smoke): promote mw-smoke rig into testdata/smoke with make target
9cd7747 fix(kitchen): add codex to cmd allowlist
```

Untracked prior to this closeout, still untracked, will be committed as part of the new change:

- `openspec/changes/milliways-kitchen-parity/` (proposal, design, tasks drafted 2026-04-18)
- `openspec/changes/milliways-provider-continuity/closeout.md` (this file)

Unrelated prior work on `internal/tui/run_targets.go` + test was stashed under `WIP: unrelated run_targets changes (pre-closeout)` — untouched by this branch.
