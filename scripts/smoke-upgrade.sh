#!/usr/bin/env bash
# scripts/smoke-upgrade.sh — smoke tests for scripts/upgrade.sh
#
# Cross-platform scenarios run directly (UG-1..4, UG-8..11).
# Linux package-manager scenarios run inside Docker containers, matching the
# exact pattern used by smoke-linux-install.sh (ubuntu:24.04 / fedora:41 /
# archlinux:latest via `docker run --rm --platform linux/amd64`).
#
# Scenarios:
#   UG-1   --check, already at latest              → exit 0, "already at latest"
#   UG-2   --check, upgrade available              → exit 1, prints both versions
#   UG-3   binary upgrade replaces all 3 binaries  → new binary reports new version
#   UG-4   same-version no-op                      → exit 0, binaries not re-downloaded
#   UG-5   deb managed install (Docker, Ubuntu)    → dpkg -i taken, not raw binary
#   UG-6   rpm managed install (Docker, Fedora)    → rpm -U taken, not raw binary
#   UG-7   pacman managed install (Docker, Arch)   → pacman -U taken, not raw binary
#   UG-8   no leftover .upgrade.tmp files          → clean bin dir after upgrade
#   UG-9   GitHub API version resolution           → mocked curl, latest tag used
#   UG-10  macOS app / SKIP_TERM=1 respected       → app replaced or skipped
#   UG-11  support scripts refreshed               → share/scripts/* updated
#
# Environment:
#   UPGRADE_SCRIPT   path to upgrade.sh (default: next to this script)
#   DIST_DIR         pre-built linux_amd64 binaries for UG-5..7
#                    (default: <repo>/dist — same as smoke-linux-install.sh)
#   SMOKE_KEEP       if non-empty, preserve the per-run temp dir on exit
#
# Exit: 0 = all pass, non-zero = one or more failures.
set -uo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repo_root="$(CDPATH= cd -- "$script_dir/.." && pwd)"
upgrade_script="${UPGRADE_SCRIPT:-$script_dir/upgrade.sh}"
dist_dir="${DIST_DIR:-$repo_root/dist}"

[ -f "$upgrade_script" ] || { printf 'ERROR: upgrade.sh not found at %s\n' "$upgrade_script" >&2; exit 1; }
[ -x "$upgrade_script" ] || chmod +x "$upgrade_script"

# ── Per-run temp dir ──────────────────────────────────────────────────────────
tmp_base="${TMPDIR:-/tmp}"; tmp_base="${tmp_base%/}"
run_dir="$(mktemp -d "$tmp_base/mw-upgrade-smoke-XXXXXX")"

cleanup() {
  if [ -n "${SMOKE_KEEP:-}" ]; then
    printf '[smoke-upgrade] SMOKE_KEEP set; preserving %s\n' "$run_dir" >&2; return
  fi
  chmod -R u+w "$run_dir" 2>/dev/null || true
  rm -rf "$run_dir"
}
trap cleanup EXIT HUP INT TERM

# ── Helpers ───────────────────────────────────────────────────────────────────
PASS=0; FAIL=0; SKIP=0

pass()    { printf '  \033[32m✓\033[0m %s\n' "$*"; PASS=$((PASS+1)); }
fail()    { printf '  \033[31m✗\033[0m %s\n' "$*"; FAIL=$((FAIL+1)); }
skip()    { printf '  \033[33m-\033[0m %s (skipped)\n' "$*"; SKIP=$((SKIP+1)); }
section() { printf '\n\033[1m%s\033[0m\n' "$*"; }

# run_upgrade: execute upgrade.sh with extra env overrides, capture combined
# stdout+stderr in $ug_out and exit code in $ug_rc.
ug_out=""; ug_rc=0
run_upgrade() {
  ug_out="$(env "$@" bash "$upgrade_script" 2>&1)" && ug_rc=0 || ug_rc=$?
}

# make_fake_bin <dir> <name> <version>
make_fake_bin() {
  local dir="$1" name="$2" ver="$3"
  mkdir -p "$dir"
  printf '#!/usr/bin/env bash\n[ "${1:-}" = "--version" ] && printf '"'"'%s\n'"'"' '"'"'%s'"'"' && exit 0\nexit 0\n' "$ver" >"$dir/$name"
  chmod +x "$dir/$name"
}

# make_release_dir <dir> <version> <platform> <arch>
make_release_dir() {
  local dir="$1" ver="$2" platform="$3" arch="$4"
  mkdir -p "$dir"
  for _bin in milliways milliwaysd milliwaysctl; do
    printf '#!/usr/bin/env bash\n[ "${1:-}" = "--version" ] && printf '"'"'%s\n'"'"' '"'"'%s'"'"' && exit 0\nexit 0\n' "$ver" >"$dir/${_bin}_${platform}_${arch}"
    chmod +x "$dir/${_bin}_${platform}_${arch}"
  done
}

HOST_PLATFORM="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$HOST_PLATFORM" in darwin) HOST_PLATFORM=darwin ;; *) HOST_PLATFORM=linux ;; esac
HOST_ARCH="$(uname -m)"
case "$HOST_ARCH" in x86_64|amd64) HOST_ARCH=amd64 ;; arm64|aarch64) HOST_ARCH=arm64 ;; esac

# ── UG-1: --check, already at latest ─────────────────────────────────────────
section "UG-1: --check mode — already at latest"
ug1_prefix="$run_dir/ug1"
make_fake_bin "$ug1_prefix/bin" milliways "v1.2.0"
run_upgrade PATH="$ug1_prefix/bin:$PATH" PREFIX="$ug1_prefix" \
  MILLIWAYS_VERSION="v1.2.0" UPGRADE_CHECK="1"
[ "$ug_rc" -eq 0 ] \
  && pass "UG-1: exits 0 when already at latest" \
  || fail "UG-1: expected exit 0, got $ug_rc"
echo "$ug_out" | grep -qi "already at latest\|nothing to do" \
  && pass "UG-1: output confirms no upgrade needed" \
  || fail "UG-1: expected 'already at latest' in output; got: $ug_out"

# ── UG-2: --check, upgrade available ─────────────────────────────────────────
section "UG-2: --check mode — upgrade available"
ug2_prefix="$run_dir/ug2"
make_fake_bin "$ug2_prefix/bin" milliways "v1.1.0"
run_upgrade PATH="$ug2_prefix/bin:$PATH" PREFIX="$ug2_prefix" \
  MILLIWAYS_VERSION="v1.2.0" UPGRADE_CHECK="1"
[ "$ug_rc" -eq 1 ] \
  && pass "UG-2: exits 1 when upgrade available" \
  || fail "UG-2: expected exit 1, got $ug_rc"
echo "$ug_out" | grep -q "1.1.0" \
  && pass "UG-2: current version shown" \
  || fail "UG-2: current version (1.1.0) not in output; got: $ug_out"
echo "$ug_out" | grep -q "1.2.0" \
  && pass "UG-2: target version shown" \
  || fail "UG-2: target version (1.2.0) not in output; got: $ug_out"

# ── UG-3: binary upgrade replaces all three binaries ─────────────────────────
section "UG-3: binary upgrade — replaces milliways, milliwaysd, milliwaysctl"
ug3_prefix="$run_dir/ug3"
ug3_release="$run_dir/ug3-release"
for _b in milliways milliwaysd milliwaysctl; do make_fake_bin "$ug3_prefix/bin" "$_b" "v1.1.0"; done
make_release_dir "$ug3_release" "v1.2.0" "$HOST_PLATFORM" "$HOST_ARCH"
run_upgrade PATH="$ug3_prefix/bin:$PATH" PREFIX="$ug3_prefix" \
  MILLIWAYS_VERSION="v1.2.0" MILLIWAYS_RELEASE_BASE_URL="file://$ug3_release" UPGRADE_YES="1"
[ "$ug_rc" -eq 0 ] \
  && pass "UG-3: exits 0" \
  || fail "UG-3: expected exit 0, got $ug_rc (output: $ug_out)"
for _b in milliways milliwaysd milliwaysctl; do
  _got="$("$ug3_prefix/bin/$_b" --version 2>/dev/null || true)"
  echo "$_got" | grep -q "1.2.0" \
    && pass "UG-3: $_b reports new version ($_got)" \
    || fail "UG-3: $_b still at old version after upgrade (got: $_got)"
done

# ── UG-4: same-version no-op ─────────────────────────────────────────────────
section "UG-4: same-version no-op — binaries not re-downloaded"
ug4_prefix="$run_dir/ug4"
ug4_release="$run_dir/ug4-release"
for _b in milliways milliwaysd milliwaysctl; do make_fake_bin "$ug4_prefix/bin" "$_b" "v1.2.0"; done
make_release_dir "$ug4_release" "v1.2.0" "$HOST_PLATFORM" "$HOST_ARCH"
ug4_mtime_before="$(stat -c %Y "$ug4_prefix/bin/milliways" 2>/dev/null \
  || stat -f %m "$ug4_prefix/bin/milliways" 2>/dev/null || echo 0)"
sleep 1
run_upgrade PATH="$ug4_prefix/bin:$PATH" PREFIX="$ug4_prefix" \
  MILLIWAYS_VERSION="v1.2.0" MILLIWAYS_RELEASE_BASE_URL="file://$ug4_release" UPGRADE_YES="1"
[ "$ug_rc" -eq 0 ] \
  && pass "UG-4: exits 0 for same-version run" \
  || fail "UG-4: expected exit 0, got $ug_rc"
ug4_mtime_after="$(stat -c %Y "$ug4_prefix/bin/milliways" 2>/dev/null \
  || stat -f %m "$ug4_prefix/bin/milliways" 2>/dev/null || echo 0)"
[ "$ug4_mtime_before" = "$ug4_mtime_after" ] \
  && pass "UG-4: milliways binary not re-written (mtime unchanged)" \
  || fail "UG-4: milliways binary was re-written when already up to date"
echo "$ug_out" | grep -qi "nothing to do\|already at latest" \
  && pass "UG-4: 'nothing to do' message printed" \
  || fail "UG-4: expected 'nothing to do' in output; got: $ug_out"

# ── UG-5..7: Linux package-manager upgrade (Docker) ──────────────────────────
#
# Mirrors smoke-linux-install.sh exactly:
#   docker run --rm --platform linux/amd64 \
#     -v <script>:/tmp/upgrade.sh:ro \
#     -v <release>:/release:ro \
#     <image> bash -lc '...'
#
# Inside the container we stub the package manager so the test does not
# require a real installable package, then assert the stub was invoked.

run_docker_upgrade_case() {
  local image="$1" label="$2" pkg_mgr="$3"

  if ! command -v docker &>/dev/null; then
    skip "$label (Docker not available)"; return 0
  fi

  local _asset
  for _asset in milliways_linux_amd64 milliwaysd_linux_amd64 milliwaysctl_linux_amd64; do
    if [ ! -x "$dist_dir/$_asset" ]; then
      skip "$label (release asset missing: $dist_dir/$_asset — run 'make build-linux-amd64' first)"
      return 0
    fi
  done

  local host_arch
  host_arch="$(docker version --format '{{.Server.Arch}}' 2>/dev/null || uname -m)"
  case "$host_arch" in
    amd64|x86_64) ;;
    *)
      skip "$label (linux/amd64 Docker requires an amd64 host; current server arch: $host_arch)"
      return 0
      ;;
  esac

  local release_dir="$run_dir/docker-release-${pkg_mgr}"
  mkdir -p "$release_dir"
  for _b in milliways milliwaysd milliwaysctl; do
    cp "$dist_dir/${_b}_linux_amd64" "$release_dir/${_b}_linux_amd64"
  done
  case "$pkg_mgr" in
    deb)    touch "$release_dir/milliways_1.2.0_amd64.deb" ;;
    rpm)    touch "$release_dir/milliways-1.2.0-1.x86_64.rpm" ;;
    pacman) touch "$release_dir/milliways-1.2.0-1-x86_64.pkg.tar.zst" ;;
  esac

  printf '\n==> %s: %s\n' "$label" "$image"
  local docker_log="$run_dir/docker-${pkg_mgr}.log"

  docker run --rm --platform linux/amd64 \
    -v "$upgrade_script:/tmp/upgrade.sh:ro" \
    -v "$release_dir:/release:ro" \
    "$image" \
    bash -lc "
set -euo pipefail
PREFIX=/tmp/mw-prefix
BIN_DIR=\$PREFIX/bin
FAKE_BIN=/tmp/fake-bin
INSTALL_LOG=/tmp/pkg-install.log
mkdir -p \"\$BIN_DIR\" \"\$FAKE_BIN\"

for _b in milliways milliwaysd milliwaysctl; do
  printf '#!/usr/bin/env bash\n[ \"\${1:-}\" = \"--version\" ] && printf \"%s\\n\" v1.1.0 && exit 0\nexit 0\n' \
    >\"\$BIN_DIR/\$_b\" && chmod +x \"\$BIN_DIR/\$_b\"
done

case '$pkg_mgr' in
  deb)
    cat >\"\$FAKE_BIN/dpkg\" <<'DPKG'
#!/usr/bin/env bash
if [ \"\${1:-}\" = \"-l\" ] && [ \"\${2:-}\" = \"milliways\" ]; then exit 0; fi
if [ \"\${1:-}\" = \"-i\" ]; then echo \"dpkg -i \$*\" >>/tmp/pkg-install.log; exit 0; fi
exit 0
DPKG
    chmod +x \"\$FAKE_BIN/dpkg\"
    ;;
  rpm)
    printf '#!/usr/bin/env bash\nexit 1\n' >\"\$FAKE_BIN/dpkg\"; chmod +x \"\$FAKE_BIN/dpkg\"
    cat >\"\$FAKE_BIN/rpm\" <<'RPM'
#!/usr/bin/env bash
if [ \"\${1:-}\" = \"-q\" ] && [ \"\${2:-}\" = \"milliways\" ]; then exit 0; fi
if [ \"\${1:-}\" = \"-U\" ]; then echo \"rpm -U \$*\" >>/tmp/pkg-install.log; exit 0; fi
exit 0
RPM
    chmod +x \"\$FAKE_BIN/rpm\"
    ;;
  pacman)
    for _s in dpkg rpm; do printf '#!/usr/bin/env bash\nexit 1\n' >\"\$FAKE_BIN/\$_s\"; chmod +x \"\$FAKE_BIN/\$_s\"; done
    cat >\"\$FAKE_BIN/pacman\" <<'PACMAN'
#!/usr/bin/env bash
if [ \"\${1:-}\" = \"-Q\" ] && [ \"\${2:-}\" = \"milliways\" ]; then exit 0; fi
if [ \"\${1:-}\" = \"-U\" ]; then echo \"pacman -U \$*\" >>/tmp/pkg-install.log; exit 0; fi
exit 0
PACMAN
    chmod +x \"\$FAKE_BIN/pacman\"
    ;;
esac

export PATH=\"\$FAKE_BIN:\$BIN_DIR:\$PATH\"
export PREFIX MILLIWAYS_VERSION=v1.2.0 MILLIWAYS_RELEASE_BASE_URL=file:///release UPGRADE_YES=1
bash /tmp/upgrade.sh >/tmp/upgrade.log 2>&1
test -f /tmp/pkg-install.log || { echo 'FAIL: package manager stub not invoked'; exit 1; }
echo 'PASS: package manager stub invoked'
cat /tmp/pkg-install.log
" >"$docker_log" 2>&1 && _drc=0 || _drc=$?

  if [ "$_drc" -eq 0 ]; then
    pass "$label"
    grep -q "PASS: package manager stub invoked" "$docker_log" \
      && pass "$label: managed install path taken (not raw binary)" \
      || fail "$label: managed install path not confirmed in container log"
  else
    fail "$label (docker exit $_drc)"
    printf '[smoke-upgrade] --- container log ---\n' >&2
    cat "$docker_log" >&2
    printf '[smoke-upgrade] --- end container log ---\n' >&2
  fi
}

section "UG-5: deb managed install — Ubuntu 24.04"
run_docker_upgrade_case "ubuntu:24.04" "UG-5 deb managed install" "deb"

section "UG-6: rpm managed install — Fedora 41"
run_docker_upgrade_case "fedora:41" "UG-6 rpm managed install" "rpm"

section "UG-7: pacman managed install — Arch Linux"
run_docker_upgrade_case "archlinux:latest" "UG-7 pacman managed install" "pacman"

# ── UG-8: no leftover .upgrade.tmp files ─────────────────────────────────────
section "UG-8: no leftover .upgrade.tmp files after successful upgrade"
ug8_prefix="$run_dir/ug8"
ug8_release="$run_dir/ug8-release"
for _b in milliways milliwaysd milliwaysctl; do make_fake_bin "$ug8_prefix/bin" "$_b" "v1.1.0"; done
make_release_dir "$ug8_release" "v1.2.0" "$HOST_PLATFORM" "$HOST_ARCH"
run_upgrade PATH="$ug8_prefix/bin:$PATH" PREFIX="$ug8_prefix" \
  MILLIWAYS_VERSION="v1.2.0" MILLIWAYS_RELEASE_BASE_URL="file://$ug8_release" UPGRADE_YES="1"
ug8_tmps="$(find "$ug8_prefix/bin" -name '*.upgrade.tmp' 2>/dev/null | wc -l | tr -d ' ')"
[ "$ug8_tmps" -eq 0 ] \
  && pass "UG-8: no .upgrade.tmp files left behind" \
  || fail "UG-8: found $ug8_tmps leftover .upgrade.tmp file(s) in $ug8_prefix/bin"

# ── UG-9: GitHub API version resolution (mocked curl) ────────────────────────
section "UG-9: GitHub API version resolution via mocked curl"
ug9_prefix="$run_dir/ug9"
ug9_release="$run_dir/ug9-release"
ug9_fakebin="$run_dir/ug9-fakebin"
mkdir -p "$ug9_fakebin"
for _b in milliways milliwaysd milliwaysctl; do make_fake_bin "$ug9_prefix/bin" "$_b" "v1.1.0"; done
make_release_dir "$ug9_release" "v1.2.0" "$HOST_PLATFORM" "$HOST_ARCH"

# Find the real curl by walking PATH, skipping our fake_bin dir.
ug9_real_curl=""
IFS=: read -ra _path_parts <<< "$PATH"
for _d in "${_path_parts[@]}"; do
  [ "$_d" = "$ug9_fakebin" ] && continue
  [ -x "$_d/curl" ] && { ug9_real_curl="$_d/curl"; break; }
done
unset _path_parts _d

if [ -z "$ug9_real_curl" ]; then
  skip "UG-9: curl not found on PATH"
else
  cat >"$ug9_fakebin/curl" <<CURLSTUB
#!/usr/bin/env bash
for _a in "\$@"; do
  case "\$_a" in
    *api.github.com*releases/latest*)
      printf '{"tag_name":"v1.2.0","name":"v1.2.0"}\n'; exit 0 ;;
  esac
done
exec "$ug9_real_curl" "\$@"
CURLSTUB
  chmod +x "$ug9_fakebin/curl"

  # Omit MILLIWAYS_VERSION intentionally so upgrade.sh resolves via API.
  run_upgrade PATH="$ug9_fakebin:$ug9_prefix/bin:$PATH" PREFIX="$ug9_prefix" \
    MILLIWAYS_RELEASE_BASE_URL="file://$ug9_release" UPGRADE_YES="1"

  [ "$ug_rc" -eq 0 ] \
    && pass "UG-9: exits 0 after API-resolved upgrade" \
    || fail "UG-9: expected exit 0, got $ug_rc (output: $ug_out)"
  echo "$ug_out" | grep -q "1.2.0" \
    && pass "UG-9: resolved version v1.2.0 shown in output" \
    || fail "UG-9: v1.2.0 not in output; got: $ug_out"
  ug9_got="$("$ug9_prefix/bin/milliways" --version 2>/dev/null || true)"
  echo "$ug9_got" | grep -q "1.2.0" \
    && pass "UG-9: binary reports resolved version after upgrade" \
    || fail "UG-9: binary version mismatch after upgrade (got: $ug9_got)"
fi

# ── UG-10: macOS app upgrade / SKIP_TERM=1 respected ─────────────────────────
section "UG-10: macOS app upgrade / SKIP_TERM=1"
ug10_prefix="$run_dir/ug10"
ug10_release="$run_dir/ug10-release"
for _b in milliways milliwaysd milliwaysctl; do make_fake_bin "$ug10_prefix/bin" "$_b" "v1.1.0"; done
make_release_dir "$ug10_release" "v1.2.0" "$HOST_PLATFORM" "$HOST_ARCH"

case "$HOST_PLATFORM" in
  darwin)
    if [ -d "/Applications/MilliWays.app" ]; then
      ug10_tmpapp="$run_dir/ug10-app"
      mkdir -p "$ug10_tmpapp/MilliWays.app/Contents"
      echo "fake" >"$ug10_tmpapp/MilliWays.app/Contents/Info.plist"
      (cd "$ug10_tmpapp" && zip -qr "$ug10_release/MilliWays.app.zip" MilliWays.app)
      run_upgrade PATH="$ug10_prefix/bin:$PATH" PREFIX="$ug10_prefix" \
        MILLIWAYS_VERSION="v1.2.0" MILLIWAYS_RELEASE_BASE_URL="file://$ug10_release" UPGRADE_YES="1"
      [ "$ug_rc" -eq 0 ] \
        && pass "UG-10 (macOS): exits 0 with app upgrade" \
        || fail "UG-10 (macOS): expected exit 0, got $ug_rc"
      echo "$ug_out" | grep -qi "MilliWays.app" \
        && pass "UG-10 (macOS): MilliWays.app mentioned in output" \
        || fail "UG-10 (macOS): MilliWays.app not mentioned; got: $ug_out"
    else
      skip "UG-10 (macOS app): /Applications/MilliWays.app not installed"
    fi
    ;;
  *)
    run_upgrade PATH="$ug10_prefix/bin:$PATH" PREFIX="$ug10_prefix" \
      MILLIWAYS_VERSION="v1.2.0" MILLIWAYS_RELEASE_BASE_URL="file://$ug10_release" UPGRADE_YES="1"
    [ "$ug_rc" -eq 0 ] \
      && pass "UG-10 (Linux): exits 0 — macOS app step silent" \
      || fail "UG-10 (Linux): expected exit 0, got $ug_rc"
    echo "$ug_out" | grep -qi "MilliWays.app" \
      && fail "UG-10 (Linux): MilliWays.app should not appear on Linux; got: $ug_out" \
      || pass "UG-10 (Linux): MilliWays.app correctly absent from output"
    ;;
esac

# SKIP_TERM=1 must suppress app upgrade on any platform.
for _b in milliways milliwaysd milliwaysctl; do make_fake_bin "$ug10_prefix/bin" "$_b" "v1.1.0"; done
run_upgrade PATH="$ug10_prefix/bin:$PATH" PREFIX="$ug10_prefix" \
  MILLIWAYS_VERSION="v1.2.0" MILLIWAYS_RELEASE_BASE_URL="file://$ug10_release" \
  UPGRADE_YES="1" SKIP_TERM="1"
[ "$ug_rc" -eq 0 ] \
  && pass "UG-10: SKIP_TERM=1 exits 0" \
  || fail "UG-10: SKIP_TERM=1 expected exit 0, got $ug_rc"

# ── UG-11: support scripts refreshed after binary upgrade ────────────────────
section "UG-11: support scripts refreshed after binary upgrade"
ug11_prefix="$run_dir/ug11"
ug11_release="$run_dir/ug11-release"
ug11_share="$ug11_prefix/share/milliways"
ug11_scripts="$ug11_share/scripts"
ug11_server="$run_dir/ug11-scripts-server"

for _b in milliways milliwaysd milliwaysctl; do make_fake_bin "$ug11_prefix/bin" "$_b" "v1.1.0"; done
make_release_dir "$ug11_release" "v1.2.0" "$HOST_PLATFORM" "$HOST_ARCH"

mkdir -p "$ug11_scripts" "$ug11_server"
for _s in install_local.sh install_local_swap.sh install_feature_deps.sh upgrade.sh; do
  printf '#!/usr/bin/env bash\n# old version\n' >"$ug11_scripts/$_s"; chmod +x "$ug11_scripts/$_s"
  printf '#!/usr/bin/env bash\n# new version v1.2.0\n' >"$ug11_server/$_s"; chmod +x "$ug11_server/$_s"
done
printf "window:set_left_status('new')\nwindow:set_right_status('')\n" >"$ug11_server/wezterm.lua"

run_upgrade PATH="$ug11_prefix/bin:$PATH" PREFIX="$ug11_prefix" \
  MILLIWAYS_VERSION="v1.2.0" MILLIWAYS_RELEASE_BASE_URL="file://$ug11_release" \
  MILLIWAYS_SUPPORT_BASE_URL="file://$ug11_server" \
  MILLIWAYS_WEZTERM_LUA_URL="file://$ug11_server/wezterm.lua" UPGRADE_YES="1"

[ "$ug_rc" -eq 0 ] \
  && pass "UG-11: exits 0 after upgrade with script refresh" \
  || fail "UG-11: expected exit 0, got $ug_rc (output: $ug_out)"
for _s in install_local.sh install_local_swap.sh install_feature_deps.sh upgrade.sh; do
  grep -q "new version v1.2.0" "$ug11_scripts/$_s" 2>/dev/null \
    && pass "UG-11: $_s refreshed to new version" \
    || fail "UG-11: $_s not updated (still contains old content)"
done
grep -q "set_left_status" "$ug11_share/wezterm.lua" 2>/dev/null \
  && grep -q "set_right_status('')" "$ug11_share/wezterm.lua" 2>/dev/null \
  && pass "UG-11: wezterm.lua refreshed with header status fix" \
  || fail "UG-11: wezterm.lua not refreshed with header status fix"

# ── Summary ───────────────────────────────────────────────────────────────────
printf '\n\033[1mResults:\033[0m  %s passed  %s failed  %s skipped\n\n' "$PASS" "$FAIL" "$SKIP"
[ "$FAIL" -eq 0 ]
