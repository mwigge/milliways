#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repo_root="$(CDPATH= cd -- "$script_dir/.." && pwd)"
dist_dir="${DIST_DIR:-$repo_root/dist}"
version="${MILLIWAYS_VERSION:-$(git -C "$repo_root" describe --tags --always --dirty 2>/dev/null || echo dev)}"
install_script="${INSTALL_SCRIPT:-$repo_root/install.sh}"
repo="${MILLIWAYS_REPO:-mwigge/milliways}"

require_asset() {
  local name="$1"
  [ -x "$dist_dir/$name" ] || {
    printf 'missing executable release asset: %s\n' "$dist_dir/$name" >&2
    exit 1
  }
}

for bin in milliways milliwaysd milliwaysctl; do
  require_asset "${bin}_linux_amd64"
done

run_case() {
  local image="$1"
  local label="$2"
  local release_dir="$3"
  local expect_fallback="$4"
  local docker_args=(--rm --platform linux/amd64 --security-opt seccomp=unconfined)

  if [ -n "${MILLIWAYS_SMOKE_FILTER:-}" ] && [[ "$label" != *"$MILLIWAYS_SMOKE_FILTER"* ]]; then
    return 0
  fi

  if [ "$expect_fallback" = "yes" ]; then
    host_arch="$(docker version --format '{{.Server.Arch}}' 2>/dev/null || uname -m)"
    case "$host_arch" in
      amd64|x86_64) ;;
      *)
        printf 'SKIP %s: linux/amd64 source fallback requires an amd64 Docker host; current Docker arch is %s\n' "$label" "$host_arch"
        return 0
        ;;
    esac
  fi

  printf '\n==> %s: %s\n' "$label" "$image"
  docker run "${docker_args[@]}" \
    -v "$install_script:/tmp/install.sh:ro" \
    -v "$repo_root/scripts/smoke-features.sh:/tmp/smoke-features.sh:ro" \
    -v "$release_dir:/release:ro" \
    -v "$support_release:/support:ro" \
    "$image" \
    bash -lc '
      set -euo pipefail
      trap '"'"'status=$?; if [ "$status" -ne 0 ]; then
        for log in /tmp/install.log /tmp/mw-daemon.log /tmp/ping.json /tmp/status.json /tmp/mw-smoke-daemon.log /tmp/mw-smoke-metrics.txt; do
          if [ -f "$log" ]; then
            echo "----- $log -----" >&2
            cat "$log" >&2
          fi
        done
      fi; exit "$status"'"'"' EXIT
      install_prereqs() {
        if command -v apt-get >/dev/null 2>&1; then
          export DEBIAN_FRONTEND=noninteractive
          export TZ=UTC
          apt-get update -qq
          if [ "'"$expect_fallback"'" = "yes" ]; then
            apt-get install -yqq --no-install-recommends ca-certificates curl git golang gcc libc6-dev
          else
            apt-get install -yqq --no-install-recommends ca-certificates curl
          fi
        elif command -v dnf >/dev/null 2>&1; then
          if [ "'"$expect_fallback"'" = "yes" ]; then
            dnf install -y ca-certificates curl git golang gcc glibc-devel
          else
            dnf install -y ca-certificates curl
          fi
        elif command -v pacman >/dev/null 2>&1; then
          sed -i "s/^#DisableSandbox/DisableSandbox/" /etc/pacman.conf 2>/dev/null || true
          if [ "'"$expect_fallback"'" = "yes" ]; then
            pacman -Sy --noconfirm ca-certificates curl git go gcc glibc
          else
            command -v curl >/dev/null 2>&1 || pacman -Sy --noconfirm ca-certificates curl
          fi
        fi
      }
      install_prereqs
      export PREFIX=/tmp/mw-install
      export SKIP_TERM=1
      export MILLIWAYS_REPO="'"$repo"'"
      export MILLIWAYS_VERSION="'"$version"'"
      export MILLIWAYS_RELEASE_BASE_URL=file:///release
      export MILLIWAYS_SUPPORT_BASE_URL=file:///support
      export MILLIWAYS_WEZTERM_LUA_URL=file:///support/wezterm.lua
      bash /tmp/install.sh > /tmp/install.log 2>&1
      if [ "'"$expect_fallback"'" = "yes" ]; then
        grep -q "Building Go binaries" /tmp/install.log
      else
        if grep -q "Building Go binaries" /tmp/install.log; then
          cat /tmp/install.log >&2
          exit 1
        fi
      fi
      for bin in milliways milliwaysd milliwaysctl; do
        test -x "$PREFIX/bin/$bin"
      done
      test -f "$PREFIX/share/milliways/wezterm.lua"
      grep -q "set_left_status" "$PREFIX/share/milliways/wezterm.lua"
      grep -q "set_right_status" "$PREFIX/share/milliways/wezterm.lua"
      "$PREFIX/bin/milliways" --version
      "$PREFIX/bin/milliwaysd" -state-dir /tmp/mw-state -log-level error >/tmp/mw-daemon.log 2>&1 &
      pid=$!
      for i in $(seq 1 50); do
        [ -S /tmp/mw-state/sock ] && break
        sleep 0.1
      done
      test -S /tmp/mw-state/sock
      "$PREFIX/bin/milliwaysctl" ping --socket /tmp/mw-state/sock >/tmp/ping.json
      "$PREFIX/bin/milliwaysctl" status --socket /tmp/mw-state/sock >/tmp/status.json
      MILLIWAYS_BIN="$PREFIX/bin" MILLIWAYS_STATE_DIR=/tmp/mw-feature-state bash /tmp/smoke-features.sh
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
      printf "PASS %s\n" "'"$label"'"
    '
}

run_deep_case() {
  local image="$1"
  local label="$2"

  if [ -n "${MILLIWAYS_SMOKE_FILTER:-}" ] && [[ "$label" != *"$MILLIWAYS_SMOKE_FILTER"* ]]; then
    return 0
  fi

  printf '\n==> %s: %s\n' "$label" "$image"
  docker run --rm \
    --platform linux/amd64 \
    --security-opt seccomp=unconfined \
    -v "$install_script:/tmp/install.sh:ro" \
    -v "$full_release:/release:ro" \
    -v "$support_release:/support:ro" \
    "$image" \
    bash -lc '
      set -euo pipefail
      trap '"'"'status=$?; if [ "$status" -ne 0 ]; then
        for log in /tmp/deep-stage /tmp/carte.yaml /tmp/install.log /tmp/install-gemini.log /tmp/install-copilot.log /tmp/install-local.log /tmp/turn1.log /tmp/turn2.log; do
          if [ -f "$log" ]; then
            echo "----- $log -----" >&2
            cat "$log" >&2
          fi
        done
      fi; exit "$status"'"'"' EXIT
      if command -v apt-get >/dev/null 2>&1; then
        export DEBIAN_FRONTEND=noninteractive
        export TZ=UTC
        apt-get update -qq
        apt-get install -yqq --no-install-recommends ca-certificates curl python3
      elif command -v dnf >/dev/null 2>&1; then
        dnf install -y ca-certificates curl python3
      elif command -v pacman >/dev/null 2>&1; then
        sed -i "s/^#DisableSandbox/DisableSandbox/" /etc/pacman.conf 2>/dev/null || true
        pacman -Sy --noconfirm ca-certificates curl python
      fi

      export PREFIX=/tmp/mw-install
      export SKIP_TERM=1
      export MILLIWAYS_REPO="'"$repo"'"
      export MILLIWAYS_VERSION="'"$version"'"
      export MILLIWAYS_RELEASE_BASE_URL=file:///release
      export MILLIWAYS_SUPPORT_BASE_URL=file:///support
      export MILLIWAYS_WEZTERM_LUA_URL=file:///support/wezterm.lua
      export SKIP_FEATURE_DEPS=1
      bash /tmp/install.sh >/tmp/install.log 2>&1
      test -f "$PREFIX/share/milliways/wezterm.lua"
      grep -q "set_left_status" "$PREFIX/share/milliways/wezterm.lua"
      grep -q "set_right_status" "$PREFIX/share/milliways/wezterm.lua"

      export HOME=/tmp/mw-home
      export XDG_CONFIG_HOME=/tmp/mw-home/.config
      mkdir -p "$HOME" "$XDG_CONFIG_HOME" /tmp/fake-bin /tmp/install-record
      export PATH="/tmp/fake-bin:$PREFIX/bin:$PATH"

      cat >/tmp/fake-bin/npm <<'"'"'EOF'"'"'
#!/usr/bin/env bash
set -euo pipefail
if [ "$1" = "install" ] && [ "$2" = "-g" ] && [ "$3" = "@google/gemini-cli" ]; then
  cat >/tmp/fake-bin/gemini <<'"'"'GEMINI'"'"'
#!/usr/bin/env bash
if [ "${1:-}" = "--version" ]; then
  echo "gemini smoke 1.0.0"
  exit 0
fi
if [ "$#" -gt 0 ]; then prompt="$*"; else prompt="$(cat)"; fi
case "$prompt" in
  *"2+3"*|*"2 + 3"*) echo "The sum is 5." ;;
  *"add 2"*|*"Add 2"*) echo "The previous sum was 5; adding 2 gives 7." ;;
  *) echo "gemini saw: $prompt" ;;
esac
GEMINI
  chmod +x /tmp/fake-bin/gemini
  echo gemini >/tmp/install-record/gemini
  exit 0
fi
echo "unexpected npm args: $*" >&2
exit 2
EOF
      chmod +x /tmp/fake-bin/npm

      cat >/tmp/fake-bin/gh <<'"'"'EOF'"'"'
#!/usr/bin/env bash
set -euo pipefail
if [ "$1" = "extension" ] && [ "$2" = "list" ]; then
  [ -f /tmp/install-record/copilot ] && echo "github/gh-copilot"
  exit 0
fi
if [ "$1" = "extension" ] && [ "$2" = "install" ] && [ "$3" = "github/gh-copilot" ]; then
  cat >/tmp/fake-bin/copilot <<'"'"'COPILOT'"'"'
#!/usr/bin/env bash
if [ "${1:-}" = "--version" ]; then
  echo "copilot smoke 1.0.0"
  exit 0
fi
if [ "$#" -gt 0 ]; then prompt="$*"; else prompt="$(cat)"; fi
case "$prompt" in
  *"taking over"*|*"previous sum"*|*"sum of that"*|*"add 2"*) echo "Taking over: the prior sum is 5, and adding 2 gives 7." ;;
  *) echo "copilot saw: $prompt" ;;
esac
COPILOT
  chmod +x /tmp/fake-bin/copilot
  echo copilot >/tmp/install-record/copilot
  exit 0
fi
echo "unexpected gh args: $*" >&2
exit 2
EOF
      chmod +x /tmp/fake-bin/gh

      milliwaysctl install gemini >/tmp/install-gemini.log 2>&1
      test -x /tmp/fake-bin/gemini
      milliwaysctl install copilot >/tmp/install-copilot.log 2>&1
      test -x /tmp/fake-bin/copilot

      MILLIWAYS_LOCAL_INSTALL_SMOKE=1 milliwaysctl local install-server >/tmp/install-local.log 2>&1
      grep -q "smoke local server installed" /tmp/install-local.log

      echo "creating carte" >/tmp/deep-stage
      mkdir -p /tmp/project/.git /tmp/project/.codegraph
      printf "%s\n" \
        "kitchens:" \
        "  gemini:" \
        "    cmd: /tmp/fake-bin/gemini" \
        "    stations: [math]" \
        "    cost_tier: free" \
        "  copilot:" \
        "    cmd: /tmp/fake-bin/copilot" \
        "    stations: [code]" \
        "    cost_tier: free" \
        "routing:" \
        "  keywords:" \
        "    sum: gemini" \
        "  default: gemini" \
        "ledger:" \
        "  ndjson: /tmp/mw-ledger.ndjson" \
        "  db: /tmp/mw-ledger.db" \
        > /tmp/carte.yaml

      echo "turn1" >/tmp/deep-stage
      milliways -c /tmp/carte.yaml --project-root /tmp/project --use-legacy-conversation --session arithmetic --kitchen gemini --timeout 15s "what is 2+3?" >/tmp/turn1.log 2>&1
      grep -q "5" /tmp/turn1.log
      echo "turn2" >/tmp/deep-stage
      milliways -c /tmp/carte.yaml --project-root /tmp/project --use-legacy-conversation --session arithmetic --switch-to copilot --timeout 15s "takeover: if you add 2 to the sum of that what will you get?" >/tmp/turn2.log 2>&1
      grep -q "7" /tmp/turn2.log
      grep -q "\[switch\] session=arithmetic gemini -> copilot" /tmp/turn2.log
      printf "PASS %s\n" "'"$label"'"
    '
}

tmp_root="$(mktemp -d "$repo_root/.mw-install-smoke-XXXXXX")"
cleanup() {
  chmod -R u+w "$tmp_root" 2>/dev/null || true
  rm -rf "$tmp_root"
}
trap cleanup EXIT

full_release="$tmp_root/full-release"
empty_release="$tmp_root/empty-release"
partial_release="$tmp_root/partial-release"
support_release="$tmp_root/support-release"
mkdir -p "$full_release" "$empty_release" "$partial_release" "$support_release"
cp "$dist_dir"/milliways*_linux_amd64 "$full_release"/
cp "$dist_dir/milliways_linux_amd64" "$partial_release"/
cp "$repo_root/scripts/install_local.sh" "$repo_root/scripts/install_local_swap.sh" "$repo_root/scripts/install_feature_deps.sh" "$repo_root/scripts/upgrade.sh" "$support_release"/
cp "$repo_root/cmd/milliwaysctl/milliways.lua" "$support_release/wezterm.lua"

images=(
  "ubuntu:24.04|Ubuntu binary install"
  "fedora:41|Fedora binary install"
  "archlinux:latest|Arch binary install"
)

for entry in "${images[@]}"; do
  image="${entry%%|*}"
  label="${entry#*|}"
  run_case "$image" "$label" "$full_release" "no"
done

run_case "ubuntu:24.04" "Ubuntu full source fallback from remote repo" "$empty_release" "yes"
run_case "fedora:41" "Fedora full source fallback from remote repo" "$empty_release" "yes"
run_deep_case "ubuntu:24.04" "Ubuntu local server plus two CLI takeover smoke"
run_deep_case "fedora:41" "Fedora local server plus two CLI takeover smoke"
run_deep_case "archlinux:latest" "Arch local server plus two CLI takeover smoke"
