#!/usr/bin/env bash
set -euo pipefail

# Smoke: Secure MilliWays daemon security flows plus release docs/fixture coverage.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
FIXTURE="${REPO_ROOT}/tests/smoke/fixtures/security-release/README.md"
SMOKE_ROOT="$(mktemp -d)"
DAEMON_PID=""

cleanup() {
  if [[ -n "${DAEMON_PID}" ]]; then
    kill "${DAEMON_PID}" 2>/dev/null || true
    wait "${DAEMON_PID}" 2>/dev/null || true
  fi
  rm -rf "${SMOKE_ROOT}"
}
trap cleanup EXIT

assert_contains() {
  local file="$1"
  local expected="$2"
  if ! grep -q "$expected" "$file"; then
    echo "FAIL: '$expected' not found in $file" >&2
    exit 1
  fi
}

assert_output_contains() {
  local label="$1"
  local expected="$2"
  local file="${SMOKE_ROOT}/${label}.out"
  if ! grep -q "$expected" "$file"; then
    echo "FAIL: '$expected' not found in ${label} output" >&2
    echo "--- ${label} output ---" >&2
    cat "$file" >&2
    exit 1
  fi
}

assert_executable() {
  local path="$1"
  if [[ ! -x "$path" ]]; then
    echo "FAIL: expected executable artifact: $path" >&2
    exit 1
  fi
}

echo "[smoke] building milliwaysd and milliwaysctl"
go build -o "${SMOKE_ROOT}/milliwaysd" "${REPO_ROOT}/cmd/milliwaysd/"
go build -o "${SMOKE_ROOT}/milliwaysctl" "${REPO_ROOT}/cmd/milliwaysctl/"

WORKSPACE="${SMOKE_ROOT}/workspace"
mkdir -p "${WORKSPACE}/.github/workflows"
cat >"${WORKSPACE}/go.mod" <<'EOF'
module example.test/security-smoke

go 1.22
EOF
cat >"${WORKSPACE}/.github/workflows/release.yml" <<'EOF'
name: release
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: go test ./...
EOF

export XDG_RUNTIME_DIR="${SMOKE_ROOT}/runtime"
export MILLIWAYS_WORKSPACE_ROOT="${WORKSPACE}"
mkdir -p "${XDG_RUNTIME_DIR}"

echo "[smoke] starting isolated milliwaysd"
"${SMOKE_ROOT}/milliwaysd" --state-dir "${XDG_RUNTIME_DIR}/milliways" --log-level error >"${SMOKE_ROOT}/daemon.out" 2>"${SMOKE_ROOT}/daemon.err" &
DAEMON_PID=$!

deadline=$(( $(date +%s) + 10 ))
until "${SMOKE_ROOT}/milliwaysctl" ping >"${SMOKE_ROOT}/ping.out" 2>"${SMOKE_ROOT}/ping.err"; do
  if [[ $(date +%s) -ge ${deadline} ]]; then
    echo "FAIL: milliwaysd did not become ready" >&2
    cat "${SMOKE_ROOT}/daemon.err" >&2 || true
    exit 1
  fi
  sleep 0.2
done

echo "[smoke] exercising real security RPC flows"
"${SMOKE_ROOT}/milliwaysctl" security help >"${SMOKE_ROOT}/security-help.out"
assert_output_contains security-help "status"
assert_output_contains security-help "audit"
assert_output_contains security-help "shim-exec"

"${SMOKE_ROOT}/milliwaysctl" security startup-scan --json >"${SMOKE_ROOT}/startup.out"
assert_output_contains startup '"workspace"'
assert_output_contains startup '"scanned_at"'

"${SMOKE_ROOT}/milliwaysctl" security status >"${SMOKE_ROOT}/status.out"
assert_output_contains status "osv-scanner"
assert_output_contains status "last startup scan"
assert_output_contains status "scanners:"

"${SMOKE_ROOT}/milliwaysctl" security client --json codex >"${SMOKE_ROOT}/client.out"
assert_output_contains client '"client": "codex"'

"${SMOKE_ROOT}/milliwaysctl" security scan --json >"${SMOKE_ROOT}/scan.out"
assert_output_contains scan '"findings"'

"${SMOKE_ROOT}/milliwaysctl" security scan --startup --client codex --staged --secrets --sast --json >"${SMOKE_ROOT}/layered-scan.out"
assert_output_contains layered-scan '"startup"'
assert_output_contains layered-scan '"client"'
assert_output_contains layered-scan '"scan"'

"${SMOKE_ROOT}/milliwaysctl" security output-plan --staged .env.local --staged cmd/app/main.go >"${SMOKE_ROOT}/output-plan.out"
assert_output_contains output-plan "secret: .env.local, cmd/app/main.go"
assert_output_contains output-plan "sast: cmd/app/main.go"

echo "[smoke] verifying generated security shim artifacts and audit surface"
SHIM_DIR="${XDG_RUNTIME_DIR}/milliways/security-shims"
for shim in bash sh npm pnpm yarn bun pip uv poetry go cargo curl wget git systemctl launchctl crontab; do
  assert_executable "${SHIM_DIR}/${shim}"
  assert_contains "${SHIM_DIR}/${shim}" "milliwaysctl"
  assert_contains "${SHIM_DIR}/${shim}" "shim-exec"
done

REAL_BIN="${SMOKE_ROOT}/real-bin"
mkdir -p "${REAL_BIN}"
cat >"${REAL_BIN}/git" <<'EOF'
#!/bin/sh
printf 'generated-shim-git:%s\n' "$*"
EOF
chmod +x "${REAL_BIN}/git"

(
  cd "${WORKSPACE}"
  PATH="${SHIM_DIR}:${SMOKE_ROOT}:${REAL_BIN}:${PATH}" \
  MILLIWAYS_WORKSPACE_ROOT="${WORKSPACE}" \
  MILLIWAYS_CLIENT_ID=codex \
  MILLIWAYS_SESSION_ID=release-smoke-generated-shim \
    "${SHIM_DIR}/git" status --short >"${SMOKE_ROOT}/generated-shim.out"
)
assert_output_contains generated-shim "generated-shim-git:status --short"

(
  cd "${WORKSPACE}"
  MILLIWAYS_WORKSPACE_ROOT="${WORKSPACE}" \
  MILLIWAYS_CLIENT_ID=codex \
  MILLIWAYS_SESSION_ID=release-smoke \
  MILLIWAYS_SECURITY_SHIM_COMMAND=true \
  MILLIWAYS_SECURITY_SHIM_CATEGORY=build-tool \
  MILLIWAYS_SECURITY_SHIM_DIR="${SHIM_DIR}" \
    "${SMOKE_ROOT}/milliwaysctl" security shim-exec -- /bin/true >"${SMOKE_ROOT}/shim-exec.out"
)

"${SMOKE_ROOT}/milliwaysctl" security audit --workspace "${WORKSPACE}" --session release-smoke --client codex --limit 5 >"${SMOKE_ROOT}/audit.out"
assert_output_contains audit "policy decision"
assert_output_contains audit "codex/release-smoke"
assert_output_contains audit "true"

"${SMOKE_ROOT}/milliwaysctl" security audit --workspace "${WORKSPACE}" --session release-smoke-generated-shim --client codex --limit 5 >"${SMOKE_ROOT}/generated-shim-audit.out"
assert_output_contains generated-shim-audit "policy decision"
assert_output_contains generated-shim-audit "codex/release-smoke-generated-shim"
assert_output_contains generated-shim-audit "git status --short"

echo "[smoke] checking release docs and observability security chrome"
assert_contains "${REPO_ROOT}/README.md" "Secure MilliWays is the release security theme"
assert_contains "${REPO_ROOT}/README.md" "all clients in one place, shared memory, shared sessions, one security layer"
assert_contains "${REPO_ROOT}/README.md" "control-plane model"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security startup-scan"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security cra"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security cra-scaffold"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security sbom"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security audit"
assert_contains "${REPO_ROOT}/README.md" "/security cra"
assert_contains "${REPO_ROOT}/README.md" "/security cra-scaffold"
assert_contains "${REPO_ROOT}/README.md" "/security sbom"
assert_contains "${REPO_ROOT}/README.md" "/security audit"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security command-check"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security output-plan"
assert_contains "${REPO_ROOT}/README.md" "Generated dependency files should trigger an SBOM refresh recommendation"
assert_contains "${REPO_ROOT}/README.md" "startup scan required/stale"
assert_contains "${REPO_ROOT}/README.md" "scanner gaps"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security quarantine"
assert_contains "${REPO_ROOT}/README.md" "milliwaysctl security rules list"
assert_contains "${REPO_ROOT}/README.md" "osv-scanner"
assert_contains "${REPO_ROOT}/README.md" "gitleaks"
assert_contains "${REPO_ROOT}/README.md" "semgrep"
assert_contains "${REPO_ROOT}/README.md" "govulncheck"
assert_contains "${REPO_ROOT}/cmd/milliwaysctl/milliways.lua" "local function security_badge(sec)"
assert_contains "${REPO_ROOT}/cmd/milliwaysctl/milliways.lua" "SEC OK"
assert_contains "${REPO_ROOT}/cmd/milliwaysctl/milliways.lua" "SEC WARN"
assert_contains "${REPO_ROOT}/cmd/milliwaysctl/milliways.lua" "SEC BLOCK"
assert_contains "${REPO_ROOT}/cmd/milliwaysctl/milliways.lua" "startup required"

assert_contains "$FIXTURE" "Secure MilliWays is release positioning"
assert_contains "$FIXTURE" "security status"
assert_contains "$FIXTURE" "security audit"
assert_contains "$FIXTURE" "security shim-exec"
assert_contains "$FIXTURE" "security cra"
assert_contains "$FIXTURE" "security cra-scaffold"
assert_contains "$FIXTURE" "security sbom"
assert_contains "$FIXTURE" "startup-scan --strict"
assert_contains "$FIXTURE" "command-check --mode strict"
assert_contains "$FIXTURE" "output-plan --generated"
assert_contains "$FIXTURE" "SBOM refresh recommendation"
assert_contains "$FIXTURE" "startup scan required/stale"
assert_contains "$FIXTURE" "scanner gaps"
assert_contains "$FIXTURE" "quarantine --dry-run"
assert_contains "$FIXTURE" "rules list"
assert_contains "$FIXTURE" "osv-scanner"
assert_contains "$FIXTURE" "gitleaks"
assert_contains "$FIXTURE" "semgrep"
assert_contains "$FIXTURE" "govulncheck"
assert_contains "$FIXTURE" "SEC OK"
assert_contains "$FIXTURE" "SEC WARN"
assert_contains "$FIXTURE" "SEC BLOCK"

echo "PASS: Secure MilliWays release smoke"
