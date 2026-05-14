# Secure MilliWays Release Fixture

Secure MilliWays is release positioning, not a binary rename: all clients in one place, shared memory, shared sessions, one security layer.

Smoke coverage should be able to find the documented command surface:

```bash
milliwaysctl security status
milliwaysctl security cra
milliwaysctl security cra-scaffold --dry-run
milliwaysctl security sbom --output dist/milliways.spdx.json
milliwaysctl security startup-scan --strict
milliwaysctl security command-check --mode strict -- npm install left-pad
milliwaysctl security output-plan --generated cmd/app/main.go --staged .env.local
milliwaysctl security quarantine --dry-run
milliwaysctl security rules list
```

Release security UX checks include an SBOM refresh recommendation when generated dependency files change, plus compact observability for startup scan required/stale state and scanner gaps.

The terminal slash surface mirrors the core posture controls:

```text
/security status
/security cra
/security cra-scaffold --dry-run
/security sbom --output dist/milliways.spdx.json
/security startup-scan --strict
/security scan
/security mode strict
```

Expected scanner names for release docs: `osv-scanner`, `gitleaks`, `semgrep`, and `govulncheck`.
