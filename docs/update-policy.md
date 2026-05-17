# Security Update Policy

MilliWays ships security updates through the public repository release process.
Supported users should track the latest tagged release unless their deployment
has an explicit internal pinning policy.

## Update Delivery

- Security fixes are published as normal GitHub releases with release notes.
- Release notes identify security-relevant changes without publishing exploit
  instructions before users have had reasonable time to update.
- Local installers and package scripts should not silently downgrade a user from
  a newer installed release.
- The project keeps local operation as the default; update checks must not be
  required for offline use.

## Vulnerability Response

Security reports are handled through `SECURITY.md`. Confirmed vulnerabilities
receive a fix plan, affected-version notes, and a release or mitigation path.
Where a fix requires operator action, the release note must include the command
or configuration change needed.

## Evidence Expectations

Before a release is marked ready, maintainers should keep evidence for:

- dependency and SBOM refresh status,
- scanner status for dependency, secret, SAST, and Go vulnerability checks,
- security policy mode and active warning/block counts,
- command-policy audit behavior,
- secure-client profile coverage,
- local-model installer smoke status.
