#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="${MINIMAX_SMOKE_IMAGE:-milliways-minimax-smoke:bookworm}"
DOCKERFILE="${MINIMAX_SMOKE_DOCKERFILE:-$ROOT/local/docker/linux/Dockerfile}"
VERSION="${MINIMAX_SMOKE_VERSION:-minimax-smoke}"
PROOF="${MINIMAX_SMOKE_PROOF:-minimax_smoke_proof.md}"

if [ -z "${MINIMAX_API_KEY:-}" ]; then
  echo "MINIMAX_API_KEY is required for the live MiniMax smoke test" >&2
  exit 2
fi

if [ ! -x "$ROOT/dist/milliwaysd_linux_amd64" ] ||
   [ ! -x "$ROOT/dist/milliwaysctl_linux_amd64" ] ||
   [ ! -x "$ROOT/dist/milliways_linux_amd64" ]; then
  VERSION="$VERSION" "$ROOT/scripts/build-linux-amd64.sh"
fi

docker build --platform linux/amd64 -q -t "$IMAGE" -f "$DOCKERFILE" "$ROOT/local/docker/linux" >/dev/null

docker run --rm \
  --platform linux/amd64 \
  -e MINIMAX_API_KEY \
  -e MINIMAX_MODEL="${MINIMAX_MODEL:-MiniMax-M2.7}" \
  -e MINIMAX_API_URL="${MINIMAX_API_URL:-https://api.minimax.io/v1/chat/completions}" \
  -e MILLIWAYS_WORKSPACE_ROOT=/work \
  -e MILLIWAYS_MAX_TURNS="${MILLIWAYS_MAX_TURNS:-20}" \
  -e PROOF="$PROOF" \
  -v "$ROOT:/work" \
  -w /work \
  "$IMAGE" \
  bash -lc '
    set -euo pipefail

    bin=/work/dist
    state=/tmp/milliways-minimax-smoke
    socket="$state/sock"
    proof="${PROOF:-minimax_smoke_proof.md}"
    response=/tmp/minimax_smoke_response.txt
    daemon_log=/tmp/minimax_smoke_daemon.log
    bridge_log=/tmp/minimax_smoke_bridge.log

    rm -rf "$state"
    mkdir -p "$state"
    rm -f "$proof" "$proof.bak" "$response" "$daemon_log" "$bridge_log"

    "$bin/milliwaysd_linux_amd64" --state-dir "$state" --socket "$socket" --log-level debug >"$daemon_log" 2>&1 &
    daemon_pid=$!
    cleanup() {
      kill "$daemon_pid" >/dev/null 2>&1 || true
      wait "$daemon_pid" >/dev/null 2>&1 || true
      if [ -n "${bridge_pid:-}" ]; then
        kill "$bridge_pid" >/dev/null 2>&1 || true
        wait "$bridge_pid" >/dev/null 2>&1 || true
      fi
    }
    trap cleanup EXIT

    for _ in $(seq 1 100); do
      [ -S "$socket" ] && break
      sleep 0.1
    done
    [ -S "$socket" ] || { echo "daemon socket not created"; cat "$daemon_log"; exit 1; }

    handle="$("$bin/milliwaysctl_linux_amd64" open --socket "$socket" --agent minimax | sed -n "s/.*\"handle\": \([0-9][0-9]*\).*/\1/p")"
    [ -n "$handle" ] || { echo "failed to open minimax session"; cat "$daemon_log"; exit 1; }

    prompt="Use tools for this smoke test. Do not answer only in prose.
1. Use Bash to calculate 2+3+4.
2. Use Bash to verify /bin/bash exists and report its ls -l line.
3. Use WebFetch to fetch http://example.com.
4. Use Write to create minimax_smoke_proof.md in the current workspace.

The file must be markdown and must include:
- math_result: 9
- bash_lookup: the observed /bin/bash line
- url_fetch: evidence from example.com
- write_tool_confirmed: yes

After the file is written, reply with one short sentence."

    {
      printf "%s\n" "$prompt"
      sleep 1
    } | "$bin/milliwaysctl_linux_amd64" bridge --socket "$socket" --handle "$handle" >"$response" 2>"$bridge_log" &
    bridge_pid=$!

    ok=0
    for _ in $(seq 1 "${MINIMAX_SMOKE_WAIT_SECONDS:-240}"); do
      if [ -f "$proof" ] &&
         grep -Eq "(^|[^0-9])9([^0-9]|$)" "$proof" &&
         grep -q "/bin/bash" "$proof" &&
         grep -qi "example" "$proof"; then
        ok=1
        break
      fi
      sleep 1
    done

    kill "$bridge_pid" >/dev/null 2>&1 || true
    wait "$bridge_pid" >/dev/null 2>&1 || true
    bridge_pid=""

    if [ "$ok" != 1 ]; then
      echo "MiniMax smoke proof was not produced or did not contain required evidence" >&2
      echo "--- response ---" >&2
      cat "$response" >&2 || true
      echo "--- bridge log ---" >&2
      cat "$bridge_log" >&2 || true
      echo "--- daemon log ---" >&2
      cat "$daemon_log" >&2 || true
      [ -f "$proof" ] && { echo "--- proof ---" >&2; cat "$proof" >&2; }
      exit 1
    fi

    echo "MiniMax smoke PASS: $proof"
    cat "$proof"
  '
