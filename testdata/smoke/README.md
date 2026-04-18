# Milliways smoke rig

End-to-end smoke scenarios for the milliways binary. Each scenario exercises
a provider-continuity code path using POSIX-shell fakes stubbed in for the
real kitchen CLIs, so the test has no network dependency and deterministic
output.

## Layout

```
testdata/smoke/
├── bin/
│   ├── fake-claude-exhausted   # emits init + rate_limit exhausted, exits 1
│   └── fake-codex-ok           # emits one item.completed JSONL, exits 0
└── config/
    └── carte.yaml.tmpl         # template; {{SMOKE_ROOT}} + {{RUN_DIR}} are
                                # substituted by scripts/smoke.sh at runtime
```

Everything in this directory is committable. Runtime state (ledger.ndjson,
milliways.db, session files) is written to a per-run temp dir by
`scripts/smoke.sh` and removed on exit.

## Running

From the repo root:

```
make smoke            # builds the binary into $TMPDIR and runs the rig
scripts/smoke.sh      # run the rig against an already-built binary
```

`scripts/smoke.sh` expects the milliways binary at `$TMPDIR/milliways` by
default; override with `MILLIWAYS_BIN=/path/to/milliways scripts/smoke.sh`.

## Scenarios

### PC-21.1 — claude exhausts, codex continues

The `explain` keyword routes to `claude`. The fake claude emits a
`rate_limit_event status=exhausted` and exits 1, which milliways treats as
a provider-exhaustion signal and fails over to the configured
`budget_fallback` kitchen (codex). Success is defined by:

- exit code 0
- stdout/stderr contains `claude exhausted, continuing with the next provider`
- stdout/stderr contains `[routed] codex`

## Why this exists

PC-21 (milliways-provider-continuity) added the failover path. This rig is
the closeout smoke that CI can run without credentials and without hitting
real provider APIs. The fuller scenario matrix (crash, hang, malformed
output, partial-budget, quota-reset) belongs to the follow-up
milliways-kitchen-parity change (KP-18/19/20) and is not covered here.
