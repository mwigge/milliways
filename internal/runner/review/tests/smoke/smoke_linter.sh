#!/usr/bin/env bash
set -euo pipefail

# Smoke: linter detects a real Go build failure.
# Run from the repository root:
#   bash internal/runner/review/tests/smoke/smoke_linter.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

REPO=$(mktemp -d)
trap 'rm -rf "$REPO"' EXIT

cd "$REPO"
go mod init smoke.test

cat > bad.go << 'EOF'
package main
func main() { undefinedFunc() }
EOF

cd "$SCRIPT_DIR/../../../../../"

go run -tags ignore "${SCRIPT_DIR}/linter_smoke_main.go" "$REPO" 2>&1 || {
  echo "FAIL: linter smoke test"
  exit 1
}

echo "PASS: linter smoke test"
