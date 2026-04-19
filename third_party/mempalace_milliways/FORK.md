# Fork: mempalace-milliways

Delta from upstream `mempalace` v3.3.1:

## What's included
- `src/mempalace_milliways/conversation.py` — NEW: SQLite-backed conversation/segment/turn/event/checkpoint primitives
- `src/mempalace_milliways/mcp_server.py` — Fork of upstream, adds conversation tools to TOOLS dict
- `src/mempalace_milliways/__init__.py` — Package init (empty)

## What's excluded (upstream features milliways doesn't need)
- CLI commands (`mine`, `wake-up`, `search`, `compress`, `hook`, etc.)
- `diary_*` MCP tools
- `palace_graph` traversal tools
- Auto-save hooks

## Rebase cadence
Target: monthly rebase onto latest upstream `mempalace` release.
After each rebase, run: `pytest tests/`

## Testing
```bash
pip install -e ".[dev]"
PYTHONPATH=src pytest tests/ --tb=short -q
```

## Upstream regression detection

CI clones the upstream mempalace repo at the same version the fork depends on and
runs its test suite (if present). This catches upstream changes that break the
fork's assumptions. If upstream has no tests at the tagged version, the step
passes silently.
