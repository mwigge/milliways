#!/usr/bin/env bash
set -euo pipefail

# Smoke: Secure MilliWays release docs and release fixture coverage.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
FIXTURE="${REPO_ROOT}/tests/smoke/fixtures/security-release/README.md"

assert_contains() {
  local file="$1"
  local expected="$2"
  if ! grep -q "$expected" "$file"; then
    echo "FAIL: '$expected' not found in $file" >&2
    exit 1
  fi
}

assert_contains "${REPO_ROOT}/README.md" "Secure MilliWays is the release security theme"
assert_contains "${REPO_ROOT}/README.md" "all clients in one place, shared memory, shared sessions, one security layer"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security startup-scan"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security cra"
assert_contains "${REPO_ROOT}/README.md" "/security cra"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security command-check"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security output-plan"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security quarantine"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security rules list"
assert_contains "${REPO_ROOT}/README.md" "osv-scanner"
assert_contains "${REPO_ROOT}/README.md" "gitleaks"
assert_contains "${REPO_ROOT}/README.md" "semgrep"
assert_contains "${REPO_ROOT}/README.md" "govulncheck"

assert_contains "$FIXTURE" "Secure MilliWays is release positioning"
assert_contains "$FIXTURE" "security status"
assert_contains "$FIXTURE" "security cra"
assert_contains "$FIXTURE" "startup-scan --strict"
assert_contains "$FIXTURE" "command-check --mode strict"
assert_contains "$FIXTURE" "output-plan --generated"
assert_contains "$FIXTURE" "quarantine --dry-run"
assert_contains "$FIXTURE" "rules list"
assert_contains "$FIXTURE" "osv-scanner"
assert_contains "$FIXTURE" "gitleaks"
assert_contains "$FIXTURE" "semgrep"
assert_contains "$FIXTURE" "govulncheck"

echo "PASS: Secure MilliWays release smoke"
