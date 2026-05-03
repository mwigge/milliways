#!/usr/bin/env bash
# milliways upgrade script
#
# Upgrades milliways binaries (and MilliWays.app on macOS) to the latest
# release, using whatever install method is already in use on this machine.
#
# Install tier priority (mirrors install.sh):
#   1. Native package (.deb / .rpm / .pkg.tar.zst)  — only if milliways was
#      already installed via the distro package manager on this machine.
#   2. Raw binary replacement                         — for PREFIX-based installs.
#   3. Source build                                   — last resort.
#
# Environment:
#   MILLIWAYS_VERSION      target version tag (default: latest)
#   MILLIWAYS_REPO         GitHub repo slug (default: mwigge/milliways)
#   PREFIX                 install prefix for binary installs (default: $HOME/.local)
#   UPGRADE_CHECK=1        print current + latest versions and exit 0/1 (no install)
#   UPGRADE_YES=1          skip the confirmation prompt
#   SKIP_TERM=1            do not upgrade MilliWays.app on macOS
set -euo pipefail

REPO="${MILLIWAYS_REPO:-mwigge/milliways}"
PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
SHARE_DIR="$PREFIX/share/milliways"
TARGET_VERSION="${MILLIWAYS_VERSION:-latest}"
RELEASE_BASE_URL="${MILLIWAYS_RELEASE_BASE_URL:-}"
CHECK_ONLY="${UPGRADE_CHECK:-0}"
YES="${UPGRADE_YES:-0}"

# ── Colours ───────────────────────────────────────────────────────────────────
if [ -t 1 ]; then
  RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
  CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'
else
  RED=''; GREEN=''; YELLOW=''; CYAN=''; BOLD=''; NC=''
fi
info()  { printf "${CYAN}  →${NC} %s\n" "$*"; }
ok()    { printf "${GREEN}  ✓${NC} %s\n" "$*"; }
warn()  { printf "${YELLOW}  !${NC} %s\n" "$*"; }
fatal() { printf "${RED}  ✗${NC} %s\n" "$*" >&2; exit 1; }

# ── Detect platform ───────────────────────────────────────────────────────────
OS="$(uname -s)"; ARCH="$(uname -m)"
case "$OS"   in Darwin) PLATFORM=darwin ;; Linux) PLATFORM=linux ;; *) fatal "unsupported OS: $OS" ;; esac
case "$ARCH" in x86_64|amd64) GOARCH=amd64 ;; arm64|aarch64) GOARCH=arm64 ;; *) fatal "unsupported arch: $ARCH" ;; esac

# ── Detect current installed version ─────────────────────────────────────────
current_version() {
  # Ask the installed binary directly; fall back to "unknown".
  local bin
  bin="$(command -v milliways 2>/dev/null || echo "$BIN_DIR/milliways")"
  if [ -x "$bin" ]; then
    "$bin" --version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+[^[:space:]]*' | head -1 || echo "unknown"
  else
    echo "unknown"
  fi
}

# ── Resolve latest version from GitHub ───────────────────────────────────────
resolve_latest() {
  command -v curl &>/dev/null || fatal "curl not found — cannot resolve latest version"
  local tag
  tag="$(curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | cut -d'"' -f4)"
  [ -n "$tag" ] || fatal "Could not resolve latest version from GitHub"
  echo "$tag"
}

# ── Detect Linux package manager (mirrors install.sh) ────────────────────────
detect_pkg_mgr() {
  [ "$PLATFORM" = "linux" ] || return 0
  command -v dpkg   &>/dev/null && echo "deb"    && return
  command -v rpm    &>/dev/null && echo "rpm"    && return
  command -v pacman &>/dev/null && echo "pacman" && return
}

# ── Detect whether milliways was installed by a package manager ──────────────
# Returns the package manager name if milliways is a managed package, else "".
detect_managed_install() {
  [ "$PLATFORM" = "linux" ] || { echo ""; return; }
  if command -v dpkg &>/dev/null && dpkg -l milliways &>/dev/null 2>&1; then
    echo "deb"; return
  fi
  if command -v rpm &>/dev/null && rpm -q milliways &>/dev/null 2>&1; then
    echo "rpm"; return
  fi
  if command -v pacman &>/dev/null && pacman -Q milliways &>/dev/null 2>&1; then
    echo "pacman"; return
  fi
  echo ""
}

# ── Native package upgrade ────────────────────────────────────────────────────
upgrade_native_pkg() {
  local mgr="$1" version="$2"
  [ "$GOARCH" = "amd64" ] || { warn "native packages are amd64-only; falling back to binary upgrade"; return 1; }

  local base_url="${RELEASE_BASE_URL:-https://github.com/${REPO}/releases/download/${version}}"
  local pkg_ver="${version#v}"
  local url pkg tmp

  case "$mgr" in
    deb)
      pkg="milliways_${pkg_ver}_amd64.deb"
      url="${base_url}/${pkg}"
      tmp="$(mktemp -d)"
      info "Downloading ${pkg}..."
      if curl -sSfL "$url" -o "$tmp/$pkg"; then
        if dpkg -i "$tmp/$pkg" 2>/dev/null || sudo dpkg -i "$tmp/$pkg"; then
          ok "Upgraded via dpkg — binaries at /usr/bin"
          rm -rf "$tmp"; return 0
        fi
      fi
      rm -rf "$tmp"
      warn ".deb not available or dpkg failed — falling back to binary upgrade"
      return 1
      ;;
    rpm)
      pkg="milliways-${pkg_ver}-1.x86_64.rpm"
      url="${base_url}/${pkg}"
      tmp="$(mktemp -d)"
      info "Downloading ${pkg}..."
      if curl -sSfL "$url" -o "$tmp/$pkg"; then
        if rpm -U "$tmp/$pkg" 2>/dev/null || sudo rpm -U "$tmp/$pkg"; then
          ok "Upgraded via rpm — binaries at /usr/bin"
          rm -rf "$tmp"; return 0
        fi
      fi
      rm -rf "$tmp"
      warn ".rpm not available or rpm failed — falling back to binary upgrade"
      return 1
      ;;
    pacman)
      pkg="milliways-${pkg_ver}-1-x86_64.pkg.tar.zst"
      url="${base_url}/${pkg}"
      tmp="$(mktemp -d)"
      info "Downloading ${pkg}..."
      if curl -sSfL "$url" -o "$tmp/$pkg"; then
        if pacman -U --noconfirm "$tmp/$pkg" 2>/dev/null \
           || sudo pacman -U --noconfirm "$tmp/$pkg"; then
          ok "Upgraded via pacman — binaries at /usr/bin"
          rm -rf "$tmp"; return 0
        fi
      fi
      rm -rf "$tmp"
      warn ".pkg.tar.zst not available or pacman failed — falling back to binary upgrade"
      return 1
      ;;
    *)
      return 1
      ;;
  esac
}

# ── Binary upgrade ────────────────────────────────────────────────────────────
upgrade_binary() {
  local version="$1"
  local base_url="${RELEASE_BASE_URL:-https://github.com/${REPO}/releases/download/${version}}"
  local missing=""

  mkdir -p "$BIN_DIR"

  for bin in milliways milliwaysd milliwaysctl; do
    local url="${base_url}/${bin}_${PLATFORM}_${GOARCH}"
    local dest="$BIN_DIR/$bin"
    local tmp_dest="${dest}.upgrade.tmp"
    info "Downloading ${bin} ${version}..."
    if curl -sSfL "$url" -o "$tmp_dest"; then
      chmod +x "$tmp_dest"
      mv "$tmp_dest" "$dest"
      ok "Upgraded ${bin} → ${dest}"
      continue
    fi
    rm -f "$tmp_dest"
    # Architecture fallback (Rosetta 2 / QEMU)
    if [ "$GOARCH" != "amd64" ]; then
      local fb_url="${base_url}/${bin}_${PLATFORM}_amd64"
      warn "${bin} ${GOARCH} not in release — trying amd64 fallback..."
      if curl -sSfL "$fb_url" -o "$tmp_dest"; then
        chmod +x "$tmp_dest"
        mv "$tmp_dest" "$dest"
        ok "Upgraded ${bin} (amd64 — runs under emulation) → ${dest}"
        continue
      fi
      rm -f "$tmp_dest"
    fi
    warn "${bin} not found in release ${version}"
    missing="$missing $bin"
  done

  [ -z "$missing" ] || return 1
  return 0
}

# ── macOS MilliWays.app upgrade ───────────────────────────────────────────────
upgrade_macos_app() {
  [ "${SKIP_TERM:-0}" = "1" ]   && return 0
  [ "$PLATFORM"       = "darwin" ] || return 0

  local version="$1"
  local app_dest="/Applications/MilliWays.app"
  [ -d "$app_dest" ] || { info "MilliWays.app not installed — skipping app upgrade"; return 0; }

  local url="https://github.com/${REPO}/releases/download/${version}/MilliWays.app.zip"
  local tmp; tmp="$(mktemp -d)"
  info "Downloading MilliWays.app ${version}..."
  if curl -sSfL "$url" -o "$tmp/MilliWays.app.zip" 2>/dev/null; then
    unzip -q "$tmp/MilliWays.app.zip" -d "$tmp"
    rm -rf "$app_dest" 2>/dev/null || sudo rm -rf "$app_dest"
    cp -r "$tmp/MilliWays.app" "$app_dest" 2>/dev/null \
      || sudo cp -r "$tmp/MilliWays.app" "$app_dest"
    ok "Upgraded MilliWays.app → /Applications"
  else
    warn "MilliWays.app not available in release ${version} — app not upgraded"
  fi
  rm -rf "$tmp"
}

# ── Also refresh support scripts in share dir ─────────────────────────────────
upgrade_support_scripts() {
  local version="$1"
  mkdir -p "$SHARE_DIR/scripts"

  local base_url="${MILLIWAYS_SUPPORT_BASE_URL:-https://raw.githubusercontent.com/${REPO}/${version}/scripts}"
  for script in install_local.sh install_local_swap.sh install_feature_deps.sh upgrade.sh; do
    local url="${base_url}/${script}"
    local dest="$SHARE_DIR/scripts/${script}"
    if curl -sSfL "$url" -o "${dest}.tmp" 2>/dev/null; then
      mv "${dest}.tmp" "$dest"
      chmod +x "$dest"
      ok "Updated support script → ${dest}"
    else
      rm -f "${dest}.tmp"
      warn "Could not download updated ${script} — keeping existing"
    fi
  done

  # Refresh the wezterm Lua config so per-client theming and URL rules stay current.
  local lua_url="https://raw.githubusercontent.com/${REPO}/${version}/cmd/milliwaysctl/milliways.lua"
  local lua_dest="$SHARE_DIR/wezterm.lua"
  if curl -sSfL "$lua_url" -o "${lua_dest}.tmp" 2>/dev/null; then
    mv "${lua_dest}.tmp" "$lua_dest"
    ok "Updated wezterm config → ${lua_dest}"
    # If ~/.config/wezterm/wezterm.lua is a symlink pointing here it auto-refreshes.
    # If it is a regular file, warn the user.
    local wezterm_cfg="$HOME/.config/wezterm/wezterm.lua"
    if [ -f "$wezterm_cfg" ] && [ ! -L "$wezterm_cfg" ]; then
      warn "~/.config/wezterm/wezterm.lua is not a symlink — restart MilliWays.app"
      warn "  To auto-update in future: ln -sf $lua_dest $wezterm_cfg"
    fi
  else
    rm -f "${lua_dest}.tmp"
    warn "Could not download updated wezterm.lua — keeping existing"
  fi
}

# ── Confirmation prompt ───────────────────────────────────────────────────────
confirm() {
  [ "$YES" = "1" ] && return 0
  [ -t 0 ] || return 0   # non-interactive (piped) — proceed without prompting
  printf "  Upgrade milliways %s → %s? [Y/n] " "$1" "$2"
  read -r reply
  case "${reply:-Y}" in
    [Yy]*) return 0 ;;
    *)     info "Upgrade cancelled."; exit 0 ;;
  esac
}

# ── Main ──────────────────────────────────────────────────────────────────────
printf '\n%bmilliways upgrade%b\n\n' "$BOLD" "$NC"

CURRENT="$(current_version)"
info "Current version: ${CURRENT}"

# Resolve target version.
if [ "$TARGET_VERSION" = "latest" ]; then
  info "Checking latest release..."
  TARGET_VERSION="$(resolve_latest)"
fi
info "Target version:  ${TARGET_VERSION}"

# Normalise versions: strip leading 'v' for comparison.
cur_norm="${CURRENT#v}"
tgt_norm="${TARGET_VERSION#v}"

if [ "$CHECK_ONLY" = "1" ]; then
  if [ "$cur_norm" = "$tgt_norm" ]; then
    ok "Already at latest (${TARGET_VERSION})"
    exit 0
  else
    printf "  Upgrade available: %s → %s\n" "$CURRENT" "$TARGET_VERSION"
    exit 1   # non-zero so callers can test "is upgrade available"
  fi
fi

if [ "$cur_norm" = "$tgt_norm" ] && [ "$cur_norm" != "unknown" ]; then
  ok "Already at latest (${TARGET_VERSION}) — nothing to do"
  printf '\n'
  exit 0
fi

confirm "$CURRENT" "$TARGET_VERSION"
printf '\n'

# Choose upgrade path.
MANAGED="$(detect_managed_install)"

if [ -n "$MANAGED" ]; then
  info "Detected managed install (${MANAGED}) — upgrading via package manager"
  if ! upgrade_native_pkg "$MANAGED" "$TARGET_VERSION"; then
    info "Falling back to binary upgrade..."
    upgrade_binary "$TARGET_VERSION" || fatal "Binary upgrade failed"
  fi
else
  info "Upgrading binaries..."
  upgrade_binary "$TARGET_VERSION" || fatal "Binary upgrade failed"
  upgrade_support_scripts "$TARGET_VERSION"
fi

# macOS: always try to upgrade the app bundle regardless of install tier.
upgrade_macos_app "$TARGET_VERSION"

printf '\n'
ok "milliways upgraded to ${TARGET_VERSION}"
printf '  Run %bmilliways --version%b to confirm.\n\n' "$BOLD" "$NC"
