#!/usr/bin/env bash
set -euo pipefail

# Smoke: CodeGraph client returns gracefully when socket unavailable.
# (socket not present = IsIndexed returns false, planner falls back)
# Run from the repository root:
#   bash internal/runner/review/tests/smoke/smoke_codegraph.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${SCRIPT_DIR}/../../../../../"
SOCKET="/tmp/nonexistent_milliways_codegraph_smoke.sock"

# Build and run the smoke binary from the repo root so go.mod is picked up.
TMPBIN="$(mktemp -t codegraph_smoke_XXXXXX)"
trap 'rm -f "$TMPBIN"' EXIT

go build -o "$TMPBIN" "${SCRIPT_DIR}/codegraph_smoke_main.go" 2>&1 || {
  # go build rejects //go:build ignore files — compile via a temp package.
  TMPDIR_PKG="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR_PKG"; rm -f "$TMPBIN"' EXIT
  cp "${SCRIPT_DIR}/codegraph_smoke_main.go" "${TMPDIR_PKG}/main.go"
  # Strip the //go:build ignore line so it compiles as a normal main.
  sed -i '' '1{/^\/\/go:build ignore/d;}' "${TMPDIR_PKG}/main.go"
  (cd "$REPO_ROOT" && go build -o "$TMPBIN" "$TMPDIR_PKG") || {
    echo "FAIL: could not build codegraph smoke binary"
    exit 1
  }
}

"$TMPBIN" "$SOCKET" || {
  echo "FAIL: codegraph graceful fallback smoke test"
  exit 1
}

echo "PASS: codegraph graceful fallback smoke test"
