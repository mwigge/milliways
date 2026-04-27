# proto/

Source of truth for milliways RPC types.

`milliways.json` is a JSON Schema 2020-12 document defining every JSON-RPC 2.0 message shape exchanged between `milliways-term` (Rust) and `milliwaysd` (Go).

## Generating types

Generated outputs (do not edit by hand):

- `internal/rpc/types.go` — Go structs with json struct tags.
- `crates/milliways-term/milliways/src/rpc/types.rs` — Rust structs with serde derives.

Regenerate after editing `milliways.json`:

```bash
make gen-rpc
# or directly:
scripts/gen-rpc-types.sh
```

CI fails any PR where the generated files have drifted from the schema. See `make repl && scripts/gen-rpc-types.sh && git diff --exit-code`.

## Adding a type

1. Add the type to `$defs` in `milliways.json`. Use `additionalProperties: false` everywhere — silent fields drift across language boundaries.
2. Reference it via `$ref: "#/$defs/<TypeName>"` from wherever it's used.
3. Run `make gen-rpc`.
4. Commit `milliways.json`, `internal/rpc/types.go`, `crates/milliways-term/milliways/src/rpc/types.rs` together.

## Conventions

- Snake-case field names in JSON (`session_id`, not `sessionId` or `SessionID`).
- ISO-8601 timestamps as strings with `format: "date-time"`.
- Use enums (`enum`) for closed sets, `const` strings for discriminators in `oneOf` unions.
- Never embed colour hex in the schema. Use `Hint` (semantic name); Rust resolves to `milliways.theme`.
- Counters and gauges use `value`; histograms add `count`/`sum`/`min`/`max`/`p50`/`p95`/`p99`.

## Prerequisites for codegen

- **Go side**: [`go-jsonschema`](https://github.com/atombender/go-jsonschema) — installed via `go install github.com/atombender/go-jsonschema@latest`. Offline-friendly, no npm dependency. The script also looks in `$(go env GOPATH)/bin/`.
- **Rust side**: deferred to Phase 1 of `milliways-emulator-fork`. The pinned choice will be [`typify`](https://github.com/oxidecomputer/typify), invoked from `build.rs` in the milliways-term crate (not from this script).
- macOS/Linux only. Windows path support not validated.

### Why not quicktype

The original proposal called for `quicktype`. As of Node 25 it fails with `Error: s.codePointAt is not a function.` (verified 2026-04-27 with quicktype 25.x and 23.x). Until quicktype is fixed for Node 25, we use go-jsonschema for the Go side. If you want one tool for both languages and are on Node ≤ 22, quicktype still works via:

```bash
quicktype --src proto/milliways.json --src-lang schema --lang go --top-level Envelope --just-types
```

The schema's top-level "registry" envelope was added specifically so quicktype-style walkers can find every `$defs` type.

## Versioning

`PingResult.proto` carries `major.minor`. Major bumps require both binaries to be redeployed (term and daemon refuse to communicate on major mismatch). Minor bumps are forward-compatible additions.

The current version lives in `scripts/gen-rpc-types.sh` as a constant; bump it whenever you add a required field or remove an existing one.
