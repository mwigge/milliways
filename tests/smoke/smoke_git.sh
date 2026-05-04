#!/usr/bin/env bash
set -euo pipefail
# Smoke test: GitIntegration on a real git repo
# Usage: run inside a Docker container (Ubuntu 22.04 or Fedora 39)
REPO=$(mktemp -d)
git init "$REPO"
git -C "$REPO" config user.email "smoke@test"
git -C "$REPO" config user.name "Smoke"
echo "hello" > "$REPO/file.go"
# Run the git integration smoke binary
./git_smoke_test "$REPO"
echo "PASS: git integration smoke test"
