#!/usr/bin/env bash
# milliways installer
#
# Remote (curl) install — downloads pre-built binaries from GitHub releases:
#   curl -sSf https://raw.githubusercontent.com/mwigge/milliways/master/install.sh | sh
#
# Local (from checkout) install — builds from source:
#   ./install.sh
#
# Environment:
#   PREFIX              install prefix (default: $HOME/.local)
#   MILLIWAYS_VERSION   release tag to install (default: latest)
#   SKIP_TERM=1         skip the wezterm / MilliWays.app install
set -euo pipefail

REPO="${MILLIWAYS_REPO:-mwigge/milliways}"
PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
SHARE_DIR="$PREFIX/share/milliways"
VERSION="${MILLIWAYS_VERSION:-latest}"
RELEASE_BASE_URL="${MILLIWAYS_RELEASE_BASE_URL:-}"
SUPPORT_BASE_URL="${MILLIWAYS_SUPPORT_BASE_URL:-}"
INSTALLED_NATIVE=0

# ── Colours ───────────────────────────────────────────────────────────────────
if [ -t 1 ]; then
  RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
  CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'
else
  RED=''; GREEN=''; YELLOW=''; CYAN=''; BOLD=''; NC=''
fi
info()    { printf "${CYAN}  →${NC} %s\n" "$*"; }
ok()      { printf "${GREEN}  ✓${NC} %s\n" "$*"; }
warn()    { printf "${YELLOW}  !${NC} %s\n" "$*"; }
fatal()   { printf "${RED}  ✗${NC} %s\n" "$*" >&2; exit 1; }
need()    { command -v "$1" &>/dev/null || fatal "$1 not found — $2"; }

# ── Detect platform ───────────────────────────────────────────────────────────
OS="$(uname -s)"; ARCH="$(uname -m)"
case "$OS"   in Darwin) PLATFORM=darwin ;; Linux) PLATFORM=linux ;; *) fatal "unsupported OS: $OS" ;; esac
case "$ARCH" in x86_64|amd64) GOARCH=amd64 ;; arm64|aarch64) GOARCH=arm64 ;; *) fatal "unsupported arch: $ARCH" ;; esac

# Detect whether we're running from a git checkout or via curl.
REPO_ROOT=""
if [ -f "$(dirname "$0")/go.mod" ] 2>/dev/null; then
  REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
fi

mkdir -p "$BIN_DIR" "$SHARE_DIR"

# ── Resolve version ───────────────────────────────────────────────────────────
if [ "$VERSION" = "latest" ] && [ -z "$REPO_ROOT" ]; then
  need curl "Install curl"
  info "Resolving latest release..."
  VERSION="$(curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | cut -d'"' -f4)"
  [ -z "$VERSION" ] && fatal "Could not resolve latest release"
fi

# ── Detect Linux package manager ─────────────────────────────────────────────
# Returns: "deb", "rpm", "pacman", or "" (unknown / macOS).
detect_pkg_mgr() {
  [ "$PLATFORM" = "linux" ] || return 0
  command -v dpkg   &>/dev/null && echo "deb"    && return
  command -v rpm    &>/dev/null && echo "rpm"    && return
  command -v pacman &>/dev/null && echo "pacman" && return
}

package_version() {
  local pkg_ver="${VERSION#v}"
  pkg_ver="${pkg_ver%%-dirty}"
  pkg_ver="$(printf '%s' "$pkg_ver" | sed 's/-/~/g')"
  case "$pkg_ver" in
    [0-9]*) printf '%s' "$pkg_ver" ;;
    *)      printf '0.0.0~%s' "$pkg_ver" ;;
  esac
}

repair_shadowing_user_bins() {
  [ "$PLATFORM" = "linux" ] || return 0
  [ "$PREFIX" = "$HOME/.local" ] || return 0
  for bin in milliways milliwaysd milliwaysctl; do
    [ -x "/usr/bin/$bin" ] || continue
    if [ -e "$BIN_DIR/$bin" ] && [ "$BIN_DIR/$bin" != "/usr/bin/$bin" ]; then
      ln -sf "/usr/bin/$bin" "$BIN_DIR/$bin"
      ok "Pointed $BIN_DIR/$bin → /usr/bin/$bin"
    fi
  done
}

# ── Install mode: native Linux package (tier 1) ───────────────────────────────
# Tries to download and install the distro-native package (.deb / .rpm / .zst).
# Returns 0 on success, 1 if the package is unavailable or install fails.
install_native_pkg() {
  [ "$PLATFORM" = "linux" ] || return 1
  [ "$GOARCH" = "amd64" ]   || return 1   # packages are amd64-only today

  local base_url="${RELEASE_BASE_URL:-https://github.com/${REPO}/releases/download/${VERSION}}"
  local pkg_ver; pkg_ver="$(package_version)"
  local mgr; mgr="$(detect_pkg_mgr)"
  local url pkg tmp

  case "$mgr" in
    deb)
      pkg="milliways_${pkg_ver}_amd64.deb"
      url="${base_url}/${pkg}"
      tmp="$(mktemp -d)"
      info "Downloading $pkg..."
      if curl -sSfL "$url" -o "$tmp/$pkg"; then
        if dpkg -i "$tmp/$pkg" 2>/dev/null || sudo dpkg -i "$tmp/$pkg"; then
          ok "Installed via dpkg — binaries at /usr/bin"
          INSTALLED_NATIVE=1
          repair_shadowing_user_bins
          rm -rf "$tmp"
          return 0
        fi
      fi
      rm -rf "$tmp"
      warn ".deb not available or dpkg failed — trying binary download"
      return 1
      ;;
    rpm)
      pkg="milliways-${pkg_ver}-1.x86_64.rpm"
      url="${base_url}/${pkg}"
      tmp="$(mktemp -d)"
      info "Downloading $pkg..."
      if curl -sSfL "$url" -o "$tmp/$pkg"; then
        if rpm -i "$tmp/$pkg" 2>/dev/null || sudo rpm -i "$tmp/$pkg"; then
          ok "Installed via rpm — binaries at /usr/bin"
          INSTALLED_NATIVE=1
          repair_shadowing_user_bins
          rm -rf "$tmp"
          return 0
        fi
      fi
      rm -rf "$tmp"
      warn ".rpm not available or rpm failed — trying binary download"
      return 1
      ;;
    pacman)
      pkg="milliways-${pkg_ver}-1-x86_64.pkg.tar.zst"
      url="${base_url}/${pkg}"
      tmp="$(mktemp -d)"
      info "Downloading $pkg..."
      if curl -sSfL "$url" -o "$tmp/$pkg"; then
        if pacman -U --noconfirm "$tmp/$pkg" 2>/dev/null \
           || sudo pacman -U --noconfirm "$tmp/$pkg"; then
          ok "Installed via pacman — binaries at /usr/bin"
          INSTALLED_NATIVE=1
          repair_shadowing_user_bins
          rm -rf "$tmp"
          return 0
        fi
      fi
      rm -rf "$tmp"
      warn ".pkg.tar.zst not available or pacman failed — trying binary download"
      return 1
      ;;
    *)
      return 1
      ;;
  esac
}

# ── Install mode: raw binary download (tier 2) ────────────────────────────────
download_binary() {
  local name="$1" dest="$2"
  local base_url="${RELEASE_BASE_URL:-https://github.com/${REPO}/releases/download/${VERSION}}"
  local url="${base_url}/${name}_${PLATFORM}_${GOARCH}"
  info "Downloading $name ${VERSION}..."
  if curl -sSfL "$url" -o "$dest"; then
    chmod +x "$dest"
    ok "Installed $(basename "$dest")"
    return 0
  fi
  # Architecture fallback: if the native arch binary is missing (e.g. arm64
  # build wasn't included in a release), try the amd64 binary — it runs under
  # Rosetta 2 on macOS and QEMU on Linux arm64 systems.
  if [ "$GOARCH" != "amd64" ]; then
    local fallback_url="${base_url}/${name}_${PLATFORM}_amd64"
    warn "$name ${GOARCH} not in release — trying amd64 fallback..."
    if curl -sSfL "$fallback_url" -o "$dest"; then
      chmod +x "$dest"
      ok "Installed $(basename "$dest") (amd64 — runs under emulation)"
      return 0
    fi
  fi
  warn "$name not found in release ($url) — will try building from source"
  return 1
}

install_remote() {
  # Tier 1: native package (.deb / .rpm / .pkg.tar.zst) — integrates with the
  # distro package manager, installs to /usr/bin, no PATH setup needed.
  if install_native_pkg; then
    return 0
  fi

  # Tier 2: raw binary download — works on any Linux/macOS with curl.
  local missing=""
  for bin in milliways milliwaysd milliwaysctl; do
    download_binary "$bin" "$BIN_DIR/$bin" || missing="$missing $bin"
  done

  if [ -n "$missing" ]; then
    # Tier 3: build from source — last resort, requires git + go + gcc.
    warn "Some binaries missing from release; falling back to source build for:$missing"
    install_from_source "$missing"
  fi
}

# ── Install mode: build from source ──────────────────────────────────────────
install_from_source() {
  local targets="${1:-milliways milliwaysd milliwaysctl}"
  need go "Install Go 1.22+: https://go.dev/dl/"
  need cc "Install a C compiler for SQLite support (Debian/Ubuntu: build-essential; Fedora: gcc; Arch: base-devel)"

  local root="$REPO_ROOT"
  local _cloned_tmp=""

  # When installed via curl (no local checkout), clone the repo so we have
  # source to build from. Without this, install_from_source looks for
  # ./cmd/milliways in the user's cwd and finds nothing.
  if [ -z "$root" ] || [ ! -d "$root/cmd/milliways" ]; then
    need curl "Install curl"
    need git  "Install git"
    _cloned_tmp="$(mktemp -d)"
    info "Cloning milliways source (${VERSION})..."
    git clone --depth 1 --branch "$VERSION" \
        "https://github.com/${REPO}.git" "$_cloned_tmp" 2>/dev/null \
      || git clone --depth 1 "https://github.com/${REPO}.git" "$_cloned_tmp"
    root="$_cloned_tmp"
  fi

  local ver="$VERSION"
  # Prefer the tag we know we're building rather than git-describe, which
  # can return old pre-restructure tags if the repo has deep history.
  [ "$ver" = "latest" ] && ver="$(git -C "$root" describe --tags --always 2>/dev/null || echo dev)"
  local ldflags="-X main.version=$ver"
  info "Building Go binaries (${ver})..."
  # GOTOOLCHAIN=auto: if the local Go is older than the module's go directive,
  # Go downloads the right toolchain automatically (requires internet).
  # This lets source builds succeed on distros with older packaged Go versions
  # (e.g. Fedora 41 ships Go 1.24, module requires 1.25).
  #
  # Some distros (Fedora) set GOSUMDB=off in their Go packaging, which breaks
  # toolchain verification. Unset it so Go can use sum.golang.org (the default)
  # to verify the downloaded toolchain's checksum.
  export GOTOOLCHAIN=auto
  export CGO_ENABLED=1
  # Suppress -Wdiscarded-qualifiers from sqlite3-binding.c in go-sqlite3.
  export CGO_CFLAGS="${CGO_CFLAGS:-} -Wno-discarded-qualifiers"
  # Fedora (and some other distros) ship a system-level /usr/lib/golang/go.env
  # that sets GOSUMDB=off and GOTOOLCHAIN=local. GOENV=off only suppresses the
  # *user* env file (~/.config/go/env), not the system one. The only way to
  # override a system go.env setting is to explicitly set the variable in the
  # shell environment (Go's rule: "environment overrides everything else").
  export GOSUMDB=sum.golang.org   # re-enable the checksum DB so toolchain download can be verified
  export GONOSUMDB=""             # clear any per-module exceptions
  for bin in $targets; do
    pkg="cmd/${bin}"
    [ -d "${root}/${pkg}" ] || { warn "  $pkg not found, skipping"; continue; }
    info "  building $bin"
    go build -C "$root" -ldflags "$ldflags" -o "$BIN_DIR/$bin" "./$pkg"
    ok "  installed $BIN_DIR/$bin"
  done
  if [ -n "$_cloned_tmp" ]; then
    rm -rf "$_cloned_tmp"
  fi
}

# ── macOS: MilliWays.app ──────────────────────────────────────────────────────
install_macos_app() {
  [ "${SKIP_TERM:-0}" = "1" ] && return 0
  local app_dest="/Applications/MilliWays.app"
  [ -d "$app_dest" ] && { info "MilliWays.app already installed"; return 0; }

  local url="https://github.com/${REPO}/releases/download/${VERSION}/MilliWays.app.zip"
  local tmp; tmp="$(mktemp -d)"
  info "Downloading MilliWays.app..."
  if curl -sSfL "$url" -o "$tmp/MilliWays.app.zip" 2>/dev/null; then
    unzip -q "$tmp/MilliWays.app.zip" -d "$tmp"
    cp -r "$tmp/MilliWays.app" "$app_dest" 2>/dev/null \
      || sudo cp -r "$tmp/MilliWays.app" "$app_dest"
    ok "Installed MilliWays.app → /Applications"
  else
    warn "MilliWays.app not available in release (build wezterm-gui manually)"
  fi
  rm -rf "$tmp"
}

# ── Linux desktop app: patched terminal GUI + .desktop entry ─────────────────
install_linux_desktop_app() {
  [ "$PLATFORM" = "linux" ] || return 0
  [ "${SKIP_TERM:-0}" = "1" ] && return 0
  [ "$INSTALLED_NATIVE" = "1" ] && [ -x /usr/bin/milliways-term ] && return 0
  [ "$GOARCH" = "amd64" ] || { warn "Linux desktop app is amd64-only today"; return 0; }

  local base_url="${RELEASE_BASE_URL:-https://github.com/${REPO}/releases/download/${VERSION}}"
  local url="${base_url}/MilliWays-linux-amd64.tar.gz"
  local tmp; tmp="$(mktemp -d)"
  info "Downloading MilliWays Linux desktop app..."
  if curl -sSfL "$url" -o "$tmp/MilliWays-linux-amd64.tar.gz" 2>/dev/null; then
    tar -xzf "$tmp/MilliWays-linux-amd64.tar.gz" -C "$tmp"
    local root="$tmp/MilliWays-linux-amd64"
    install -Dm755 "$root/bin/milliways-term" "$BIN_DIR/milliways-term"
    install -Dm755 "$root/bin/wezterm-mux-server" "$BIN_DIR/wezterm-mux-server"
    sed -e "s|^Exec=.*|Exec=$BIN_DIR/milliways-term|" \
        -e "s|^TryExec=.*|TryExec=$BIN_DIR/milliways-term|" \
        "$root/share/applications/dev.milliways.MilliWays.desktop" > "$tmp/dev.milliways.MilliWays.desktop"
    install -Dm644 "$tmp/dev.milliways.MilliWays.desktop" \
      "$HOME/.local/share/applications/dev.milliways.MilliWays.desktop"
    install -Dm644 "$root/share/icons/hicolor/scalable/apps/dev.milliways.MilliWays.svg" \
      "$HOME/.local/share/icons/hicolor/scalable/apps/dev.milliways.MilliWays.svg"
    command -v update-desktop-database >/dev/null 2>&1 \
      && update-desktop-database "$HOME/.local/share/applications" 2>/dev/null || true
    command -v gtk-update-icon-cache >/dev/null 2>&1 \
      && gtk-update-icon-cache -q "$HOME/.local/share/icons/hicolor" 2>/dev/null || true
    ok "Installed MilliWays desktop app → ~/.local/share/applications"
  else
    warn "MilliWays Linux desktop app not available in release"
  fi
  rm -rf "$tmp"
}

# ── Wezterm config ────────────────────────────────────────────────────────────
setup_wezterm_config() {
  local wezterm_cfg="$HOME/.config/wezterm/wezterm.lua"

  # Find the canonical config source (checkout takes priority over share dir).
  local sample=""
  for candidate in \
    "${REPO_ROOT:+$REPO_ROOT/cmd/milliwaysctl/milliways.lua}" \
    "$SHARE_DIR/wezterm.lua"; do
    [ -n "$candidate" ] && [ -f "$candidate" ] && sample="$candidate" && break
  done
  [ -z "$sample" ] && return 0

  mkdir -p "$(dirname "$wezterm_cfg")"

  if [ -L "$wezterm_cfg" ]; then
    # Already a symlink — refresh it to point at the current source.
    ln -sf "$sample" "$wezterm_cfg"
    ok "Updated wezterm config symlink → $wezterm_cfg"
  elif [ ! -f "$wezterm_cfg" ]; then
    # Fresh install — create symlink so future upgrades propagate automatically.
    ln -sf "$sample" "$wezterm_cfg"
    ok "Linked wezterm config → $wezterm_cfg"
  else
    # User has a hand-edited regular file — never overwrite it, just warn.
    warn "wezterm config is a regular file (not a symlink) — skipping auto-update"
    warn "  To receive future updates automatically, run:"
    warn "  ln -sf $sample $wezterm_cfg"
  fi
}

# ── Install the wezterm Lua config into share dir (for checkout installs) ─────
install_wezterm_lua() {
  if [ -n "$REPO_ROOT" ] && [ -f "$REPO_ROOT/cmd/milliwaysctl/milliways.lua" ]; then
    # Checkout install — copy from repo.
    cp "$REPO_ROOT/cmd/milliwaysctl/milliways.lua" "$SHARE_DIR/wezterm.lua"
    ok "Installed wezterm config → $SHARE_DIR/wezterm.lua"
  elif [ -z "$REPO_ROOT" ]; then
    # Binary / curl install — download from release.
    local url="${MILLIWAYS_WEZTERM_LUA_URL:-https://raw.githubusercontent.com/${REPO}/${VERSION}/cmd/milliwaysctl/milliways.lua}"
    if curl -sSfL "$url" -o "$SHARE_DIR/wezterm.lua.tmp" 2>/dev/null; then
      mv "$SHARE_DIR/wezterm.lua.tmp" "$SHARE_DIR/wezterm.lua"
      ok "Downloaded wezterm config → $SHARE_DIR/wezterm.lua"
    else
      rm -f "$SHARE_DIR/wezterm.lua.tmp"
      warn "Could not download wezterm config — MilliWays.app terminal theme unavailable"
    fi
  fi
}

install_support_scripts() {
  mkdir -p "$SHARE_DIR/scripts"
  for script in install_local.sh install_local_swap.sh install_feature_deps.sh upgrade.sh; do
    if [ -n "$REPO_ROOT" ] && [ -f "$REPO_ROOT/scripts/$script" ]; then
      cp "$REPO_ROOT/scripts/$script" "$SHARE_DIR/scripts/$script"
      chmod +x "$SHARE_DIR/scripts/$script"
      ok "Installed support script → $SHARE_DIR/scripts/$script"
      continue
    fi

    need curl "Install curl"
    local base_url="${SUPPORT_BASE_URL:-https://raw.githubusercontent.com/${REPO}/${VERSION}/scripts}"
    local url="${base_url}/${script}"
    info "Downloading support script $script..."
    curl -sSfL "$url" -o "$SHARE_DIR/scripts/$script" \
      || fatal "Could not download support script: $url"
    chmod +x "$SHARE_DIR/scripts/$script"
    ok "Installed support script → $SHARE_DIR/scripts/$script"
  done
}

# ── Linux: milliwaysd systemd user service ───────────────────────────────────
install_milliwaysd_service() {
  [ "$PLATFORM" = "linux" ] || return 0
  [ "${SKIP_DAEMON_SERVICE:-0}" = "1" ] && return 0
  command -v systemctl >/dev/null 2>&1 || {
    warn "systemctl not found — start daemon manually with: milliwaysd &"
    return 0
  }

  local daemon_bin
  if [ -x "$BIN_DIR/milliwaysd" ]; then
    daemon_bin="$BIN_DIR/milliwaysd"
  else
    daemon_bin="$(command -v milliwaysd 2>/dev/null || true)"
  fi
  [ -x "$daemon_bin" ] || {
    warn "milliwaysd binary not found — service not installed"
    return 0
  }

  local unit_dir="$HOME/.config/systemd/user"
  local unit="$unit_dir/milliwaysd.service"
  mkdir -p "$unit_dir"
  cat > "$unit" <<EOF
[Unit]
Description=MilliWays daemon
Documentation=https://github.com/${REPO}

[Service]
Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=$daemon_bin
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
EOF
  ok "Installed systemd user service → $unit"

  systemctl --user daemon-reload >/dev/null 2>&1 || {
    warn "Could not reload systemd user units — run: systemctl --user daemon-reload"
    return 0
  }

  if ! systemctl --user is-active --quiet milliwaysd >/dev/null 2>&1; then
    command -v milliwaysctl >/dev/null 2>&1 && milliwaysctl daemon stop >/dev/null 2>&1 || true
  fi

  if systemctl --user enable --now milliwaysd >/dev/null 2>&1; then
    ok "Enabled and started milliwaysd.service"
  else
    warn "Installed milliwaysd.service, but could not start it automatically"
    warn "  Try: systemctl --user enable --now milliwaysd"
  fi
}

# ── PATH setup ────────────────────────────────────────────────────────────────
add_to_path() {
  local profile="$1"
  [ -f "$profile" ] || return 0
  grep -q "$BIN_DIR" "$profile" 2>/dev/null && return 0
  printf '\n# milliways\nexport PATH="%s:$PATH"\n' "$BIN_DIR" >> "$profile"
  ok "Added $BIN_DIR to PATH in $profile"
}

setup_path() {
  case "${SHELL:-}" in
    */zsh)  add_to_path "$HOME/.zshrc" ;;
    */bash) add_to_path "$HOME/.bashrc"; add_to_path "$HOME/.bash_profile" ;;
    */fish)
      local fish_conf="$HOME/.config/fish/conf.d/milliways.fish"
      mkdir -p "$(dirname "$fish_conf")"
      printf 'fish_add_path "%s"\n' "$BIN_DIR" > "$fish_conf"
      ok "Added $BIN_DIR to PATH in $fish_conf"
      ;;
  esac
}

# ── Feature dependencies: MemPalace, CodeGraph, python-pptx, git ─────────────
# SKIP_FEATURE_DEPS=1 opts out. The support script creates Milliways-owned
# Python/npm prefixes instead of relying on user-site packages.
install_feature_dependencies() {
  [ "${SKIP_FEATURE_DEPS:-0}" = "1" ] && return 0
  local installer="$SHARE_DIR/scripts/install_feature_deps.sh"
  [ -x "$installer" ] || fatal "feature dependency installer missing: $installer"
  PREFIX="$PREFIX" SHARE_DIR="$SHARE_DIR" "$installer"
}

# ── Main ──────────────────────────────────────────────────────────────────────
printf '\n%bmilliways installer%b\n\n' "$BOLD" "$NC"

if [ -n "$REPO_ROOT" ]; then
  info "Local checkout detected — building from source"
  install_from_source "milliways milliwaysd milliwaysctl"
else
  info "Remote install — version ${VERSION}"
  install_remote
fi

install_wezterm_lua
install_support_scripts
install_feature_dependencies

install_milliwaysd_service

if [ "${SKIP_TERM:-0}" != "1" ]; then
  setup_wezterm_config
fi

if [ "$PLATFORM" = "darwin" ]; then
  install_macos_app
fi
install_linux_desktop_app

setup_path

# ── Verify ────────────────────────────────────────────────────────────────────
verify_install() {
  local missing=""
  for bin in milliways milliwaysd milliwaysctl; do
    # Native package installs to /usr/bin; binary/source install to $BIN_DIR.
    if ! command -v "$bin" &>/dev/null && [ ! -x "$BIN_DIR/$bin" ]; then
      missing="$missing $bin"
    fi
  done
  [ -z "$missing" ] || fatal "Install incomplete; missing executable(s):$missing"

  local expected="${VERSION#v}"
  expected="${expected%%-dirty}"
  if [ -n "$expected" ] && [ "$expected" != "latest" ]; then
    local mw_bin actual actual_norm
    mw_bin="$(command -v milliways 2>/dev/null || echo "$BIN_DIR/milliways")"
    actual="$("$mw_bin" --version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+[^[:space:]]*' | head -1 || true)"
    actual_norm="${actual#v}"
    if [ -z "$actual" ]; then
      fatal "Install verification failed: $mw_bin did not report a version"
    fi
    if [ "$actual_norm" != "$expected" ]; then
      fatal "Install verification failed: command resolves to $actual, expected ${VERSION}. Check PATH for stale milliways binaries."
    fi
  fi
}

verify_install

printf '\n'
mw_bin="$(command -v milliways 2>/dev/null || echo "$BIN_DIR/milliways")"
if "$mw_bin" --version &>/dev/null; then
  ver="$("$mw_bin" --version 2>/dev/null | head -1)"
  ok "milliways ready: $ver"
else
  warn "Installed to $BIN_DIR — restart your shell or run:"
  warn "  export PATH=\"$BIN_DIR:\$PATH\""
fi

printf '\n'
printf '  Get started:\n'
if [ "$PLATFORM" = "linux" ]; then
  printf '    systemctl --user status milliwaysd   # check daemon service\n'
fi
printf '    milliwaysctl status                  # check runner availability\n'
printf '    milliways                 # open the terminal\n'
if [ "$PLATFORM" = "darwin" ]; then
  printf '    open /Applications/MilliWays.app   # native terminal\n'
fi
printf '\n'
printf '  Docs: https://github.com/%s\n\n' "$REPO"
