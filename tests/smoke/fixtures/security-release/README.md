# Secure MilliWays Release Fixture

Secure MilliWays is release positioning, not a binary rename: all clients in one place, shared memory, shared sessions, one security layer.

Smoke coverage should be able to find the documented command surface:

```bash
milliwaysctl security status
milliwaysctl security startup-scan --strict
milliwaysctl security command-check --mode strict -- npm install left-pad
milliwaysctl security output-plan --generated cmd/app/main.go --staged .env.local
milliwaysctl security quarantine --dry-run
milliwaysctl security rules list
```

Expected scanner names for release docs: `osv-scanner`, `gitleaks`, `semgrep`, and `govulncheck`.
