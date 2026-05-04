#!/usr/bin/env bash
set -euo pipefail

# Smoke: ArchitectEditor runs both phases sequentially.
# Run from the repository root: bash internal/runner/review/tests/smoke/smoke_architect.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../../.." && pwd)"

echo "Building smoke binary..."
go run -tags ignore "${SCRIPT_DIR}/architect_smoke_main.go" 2>&1 || {
  echo "FAIL: architect/editor smoke test"
  exit 1
}

echo "PASS: architect/editor smoke test"
