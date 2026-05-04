#!/usr/bin/env bash
set -euo pipefail

# run_smoke.sh — Run ReviewRunner smoke tests.
# Usage: ./tests/smoke/run_smoke.sh [--docker ubuntu|fedora|all] [--no-docker]
# Without --docker: runs tests natively on the current OS.
# With --docker:    builds and runs inside a container.
#
# Environment variables:
#   STUB_PORT   Port for the stub llama-server (default: 8765)

STUB_PORT="${STUB_PORT:-8765}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
STUB_PID=""

cleanup() {
  if [[ -n "${STUB_PID}" ]]; then
    kill "${STUB_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

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

  # Assert: output file exists and is non-empty.
  if [[ ! -s "${out_file}" ]]; then
    echo "[smoke FAIL] ${out_file} does not exist or is empty" >&2
    exit 1
  fi
  echo "[smoke] output file present and non-empty: OK"

  # Assert: output contains the seeded finding from the stub.
  if ! grep -q "smoke test finding" "${out_file}"; then
    echo "[smoke FAIL] 'smoke test finding' not found in ${out_file}" >&2
    echo "--- file contents ---" >&2
    cat "${out_file}" >&2
    exit 1
  fi
  echo "[smoke] seeded finding present in report: OK"

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
