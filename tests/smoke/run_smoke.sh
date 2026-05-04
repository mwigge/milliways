#!/usr/bin/env bash
set -euo pipefail

# run_smoke.sh — Run ReviewRunner smoke tests.
# Usage: ./tests/smoke/run_smoke.sh [--docker ubuntu|fedora|all] [--no-docker]
# Without --docker: runs tests natively on the current OS.
# With --docker:    builds and runs inside a container.
#
# Environment variables:
#   STUB_PORT   Port for the stub llama-server (default: 8765)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
STUB_PID=""

# Pick a free port automatically unless STUB_PORT is explicitly set.
if [[ -z "${STUB_PORT:-}" ]]; then
  STUB_PORT=$(python3 -c "import socket; s=socket.socket(); s.bind(('',0)); print(s.getsockname()[1]); s.close()" 2>/dev/null || echo 18765)
fi

cleanup() {
  if [[ -n "${STUB_PID}" ]]; then
    kill "${STUB_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# --- assertion helpers ---

assert_file_nonempty() {
  if [[ ! -s "$1" ]]; then
    echo "FAIL: $1 is empty or missing" >&2
    exit 1
  fi
}

assert_contains() {
  if ! grep -q "$2" "$1"; then
    echo "FAIL: '$2' not found in $1" >&2
    echo "--- file contents ---" >&2
    cat "$1" >&2
    exit 1
  fi
}

run_native() {
  echo "[smoke] starting stub llama-server on port ${STUB_PORT} …"
  STUB_PORT="${STUB_PORT}" go run "${REPO_ROOT}/tests/smoke/stub_llama_server.go" &
  STUB_PID=$!

  # Wait until the stub is accepting connections (up to 10 s).
  local deadline=$(( $(date +%s) + 10 ))
  until curl -sf "http://localhost:${STUB_PORT}/v1/models" >/dev/null 2>&1; do
    if [[ $(date +%s) -ge ${deadline} ]]; then
      echo "[smoke FAIL] stub llama-server did not start within 10s" >&2
      exit 1
    fi
    sleep 0.3
  done
  echo "[smoke] stub ready"

  export MILLIWAYS_LOCAL_ENDPOINT="http://localhost:${STUB_PORT}/v1"

  echo "[smoke] building milliwaysctl …"
  go build -o /tmp/milliwaysctl_smoke "${REPO_ROOT}/cmd/milliwaysctl/"

  local fixture_repo="${REPO_ROOT}/tests/smoke/fixtures/go-repo"
  local out_file="/tmp/smoke_review.md"
  rm -f "${out_file}"
  # Remove any leftover scratch file from a previous run so Init doesn't fail.
  rm -f /tmp/review_go-repo.md

  echo "[smoke] running: milliwaysctl local review-code -model devstral-small -no-memory -out ${out_file} ${fixture_repo}"
  /tmp/milliwaysctl_smoke local review-code \
    -model devstral-small \
    -no-memory \
    -out "${out_file}" \
    "${fixture_repo}"

  assert_file_nonempty "${out_file}"
  echo "[smoke] output file present and non-empty: OK"

  assert_contains "${out_file}" "smoke test finding"
  echo "[smoke] seeded finding present in report: OK"

  echo "[smoke] testing Python repo..."
  rm -f /tmp/smoke_python_review.md
  rm -f /tmp/review_python-repo.md
  /tmp/milliwaysctl_smoke local review-code \
    -model devstral-small -no-memory \
    -out /tmp/smoke_python_review.md \
    "${REPO_ROOT}/tests/smoke/fixtures/python-repo"
  assert_file_nonempty /tmp/smoke_python_review.md
  assert_contains /tmp/smoke_python_review.md "smoke test finding"
  echo "[smoke] Python repo: OK"

  echo "[smoke] testing mixed Go+YAML repo..."
  rm -f /tmp/smoke_mixed_review.md
  rm -f /tmp/review_mixed-go-yaml.md
  /tmp/milliwaysctl_smoke local review-code \
    -model devstral-small -no-memory \
    -out /tmp/smoke_mixed_review.md \
    "${REPO_ROOT}/tests/smoke/fixtures/mixed-go-yaml"
  assert_file_nonempty /tmp/smoke_mixed_review.md
  assert_contains /tmp/smoke_mixed_review.md "smoke test finding"
  echo "[smoke] mixed Go+YAML repo: OK"

  echo "PASS: smoke test (native)"
}

run_docker() {
  local os="$1"
  echo "[smoke] building Docker image milliways-smoke-${os} …"
  docker build -f "${REPO_ROOT}/tests/smoke/Dockerfile.${os}" \
    -t "milliways-smoke-${os}" \
    "${REPO_ROOT}"
  echo "[smoke] running container milliways-smoke-${os} …"
  docker run --rm "milliways-smoke-${os}"
  echo "PASS: smoke test (docker/${os})"
}

# --- argument parsing ---
MODE="native"
DOCKER_TARGET="ubuntu"

if [[ $# -eq 0 ]]; then
  MODE="native"
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-docker)
      MODE="native"
      shift
      ;;
    --docker)
      MODE="docker"
      DOCKER_TARGET="${2:-ubuntu}"
      shift 2
      ;;
    *)
      echo "usage: run_smoke.sh [--docker ubuntu|fedora|all] [--no-docker]" >&2
      exit 2
      ;;
  esac
done

case "${MODE}" in
  native)
    run_native
    ;;
  docker)
    if [[ "${DOCKER_TARGET}" == "all" ]]; then
      run_docker ubuntu
      run_docker fedora
    else
      run_docker "${DOCKER_TARGET}"
    fi
    ;;
esac
