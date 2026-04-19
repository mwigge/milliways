# milliways Development SOP

## Session Management

Follow the **Session Management** section in `~/.config/opencode/AGENTS.md` — it is the canonical definition of this workflow.

**In short** (always see AGENTS.md for the full rule):

1. At end of every session: write `YYYY-MM-DD-session.md` to `pprojects/docs_private/session_logs/`
2. File the AAAK-compressed version to MemPalace: `wing=private, room=sessions`
3. Clean up `/tmp/delegate_spec_*.md` before ending

---

## Project-Specific Conventions

### OpenSpec
- `openspec` CLI must be run from `pprojects/milliways` — NOT `docs_local`
- Change definitions live in `openspec/changes/<change-name>/`
- Archive completed changes to `openspec/changes/archive/YYYY-MM-DD-<change-name>/`

### Go
- Run `go build ./...` and `go vet ./...` to verify clean compilation before committing
- Pre-existing LSP errors (`StartSegment`, `NewOTelSink`) are stale — verify with `go build ./...` not the editor
- Do NOT run tests requiring interactive TTY (`go test ./...` skips these by convention)

### Delegation
- Lua nvim plugin work: use `--spec-file` on `delegate.sh`, NOT `--prompt` (zsh multiline parse errors)
- `--prompt` and `--spec-file` are mutually exclusive in `delegate.sh`
- One logical unit per delegation; max 3 files per delegation

### Branches
- One branch per logical unit of work
- Never commit directly to `master` — use feature branches, PRs, squash-merge

---

## Active Changes

| Change | Status | Notes |
|--------|--------|-------|
| `milliways-nvim-context` | ✅ Archived | All 61/61 tasks done |
| `milliways-tui-presence` | 🔄 41/44 | TP-9.4/9.5 need TTY |
| `milliways-kitchen-parity` | 🔄 89/83 | KP-22.1/22.2 need TTY |
| `milliways-http-kitchen` | 🔄 43/47 | HK-5.2 needs live API + TTY |
| `two-active-memory` | 🔄 62/66 | TAM-10.1/10.3/10.4/10.7 need TTY |
| `milliways-tui-panels` | 🆕 Proposed | SPS-1..SPS-9, 35 subtasks |
| `milliways-jobs-panel` | 🔴 Proposal stale | `pantry/jobs.go` already exists |
