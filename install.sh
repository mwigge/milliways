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

REPO="mwigge/milliways"
PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
SHARE_DIR="$PREFIX/share/milliways"
VERSION="${MILLIWAYS_VERSION:-latest}"

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

# ── Install mode: remote download ─────────────────────────────────────────────
download_binary() {
  local name="$1" dest="$2"
  local url="https://github.com/${REPO}/releases/download/${VERSION}/${name}_${PLATFORM}_${GOARCH}"
  info "Downloading $name ${VERSION}..."
  if curl -sSfL "$url" -o "$dest"; then
    chmod +x "$dest"
    ok "Installed $(basename "$dest")"
    return 0
  fi
  warn "$name not found in release ($url) — will try building from source"
  return 1
}

install_remote() {
  local missing=""
  for bin in milliways milliwaysd milliwaysctl; do
    download_binary "$bin" "$BIN_DIR/$bin" || missing="$missing $bin"
  done

  if [ -n "$missing" ]; then
    warn "Some binaries missing from release; falling back to source build for:$missing"
    install_from_source "$missing"
  fi
}

# ── Install mode: build from source ──────────────────────────────────────────
install_from_source() {
  local targets="${1:-milliways milliwaysd milliwaysctl}"
  need go "Install Go 1.22+: https://go.dev/dl/"

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
  for bin in $targets; do
    pkg="cmd/${bin}"
    [ -d "${root}/${pkg}" ] || { warn "  $pkg not found, skipping"; continue; }
    info "  building $bin"
    go build -C "$root" -ldflags "$ldflags" -o "$BIN_DIR/$bin" "./$pkg"
    ok "  installed $BIN_DIR/$bin"
  done
  [ -n "$_cloned_tmp" ] && rm -rf "$_cloned_tmp"
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

# ── Wezterm config ────────────────────────────────────────────────────────────
setup_wezterm_config() {
  local wezterm_cfg="$HOME/.config/wezterm/wezterm.lua"
  [ -f "$wezterm_cfg" ] && { info "wezterm config already exists at $wezterm_cfg"; return 0; }

  # Find the sample config in the checkout or share dir.
  local sample=""
  for candidate in \
    "${REPO_ROOT:+$REPO_ROOT/cmd/milliwaysctl/milliways.lua}" \
    "$SHARE_DIR/wezterm.lua"; do
    [ -n "$candidate" ] && [ -f "$candidate" ] && sample="$candidate" && break
  done

  [ -z "$sample" ] && return 0

  mkdir -p "$(dirname "$wezterm_cfg")"
  ln -sf "$sample" "$wezterm_cfg"
  ok "Linked wezterm config → $wezterm_cfg"
}

# ── Install the wezterm Lua config into share dir (for checkout installs) ─────
install_wezterm_lua() {
  [ -z "$REPO_ROOT" ] && return 0
  local src="$REPO_ROOT/cmd/milliwaysctl/milliways.lua"
  [ -f "$src" ] || return 0
  cp "$src" "$SHARE_DIR/wezterm.lua"
  ok "Installed wezterm config → $SHARE_DIR/wezterm.lua"
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

if [ "$PLATFORM" = "darwin" ]; then
  install_macos_app
  setup_wezterm_config
fi

setup_path

# ── Verify ────────────────────────────────────────────────────────────────────
printf '\n'
if "$BIN_DIR/milliways" --version &>/dev/null; then
  ver="$("$BIN_DIR/milliways" --version 2>/dev/null | head -1)"
  ok "milliways ready: $ver"
else
  warn "Installed to $BIN_DIR — restart your shell or run:"
  warn "  export PATH=\"$BIN_DIR:\$PATH\""
fi

printf '\n'
printf '  Get started:\n'
printf '    milliwaysd &              # start the agent daemon\n'
printf '    milliwaysctl status       # check runner availability\n'
printf '    milliways                 # open the terminal\n'
if [ "$PLATFORM" = "darwin" ]; then
  printf '    open /Applications/MilliWays.app   # native terminal\n'
fi
printf '\n'
printf '  Docs: https://github.com/%s\n\n' "$REPO"
