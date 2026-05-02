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

# ── Install mode: native Linux package (tier 1) ───────────────────────────────
# Tries to download and install the distro-native package (.deb / .rpm / .zst).
# Returns 0 on success, 1 if the package is unavailable or install fails.
install_native_pkg() {
  [ "$PLATFORM" = "linux" ] || return 1
  [ "$GOARCH" = "amd64" ]   || return 1   # packages are amd64-only today

  local base_url="${RELEASE_BASE_URL:-https://github.com/${REPO}/releases/download/${VERSION}}"
  local pkg_ver="${VERSION#v}"
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

install_support_scripts() {
  mkdir -p "$SHARE_DIR/scripts"
  for script in install_local.sh install_local_swap.sh; do
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
install_support_scripts
install_python_packages

if [ "$PLATFORM" = "darwin" ]; then
  install_macos_app
  setup_wezterm_config
fi

setup_path

# ── MemPalace — shared project memory ────────────────────────────────────────
# MemPalace is the MCP server that gives every runner the same project memory.
# Without it, context is not shared across runner switches. It is a Python
# package; we install it with pip3 --user so it works without root and does
# not touch the system Python. SKIP_MEMPALACE=1 to opt out.
# install_python_packages installs the Python packages milliways features
# depend on. Soft-failure throughout: if Python or pip is absent the install
# still completes; each missing package is warned but not fatal.
install_python_packages() {
  [ "${SKIP_PYTHON_PKGS:-0}" = "1" ] && return 0
  if ! command -v python3 &>/dev/null; then
    warn "python3 not found — skipping Python packages (MemPalace + /pptx will not work)"
    warn "  Install python3 then run: pip3 install --user mempalace python-pptx"
    return 0
  fi

  local pip_cmd=""
  if command -v pip3 &>/dev/null; then
    pip_cmd="pip3"
  elif python3 -m pip --version &>/dev/null 2>&1; then
    pip_cmd="python3 -m pip"
  else
    warn "pip not found — skipping Python packages"
    warn "  Install pip3 then run: pip3 install --user mempalace python-pptx"
    return 0
  fi

  # ── MemPalace: shared project memory MCP server ───────────────────────────
  if python3 -c "import mempalace" 2>/dev/null; then
    ok "MemPalace already installed"
  else
    info "Installing MemPalace (project memory)..."
    # --break-system-packages required on distros with PEP 668 (Ubuntu 24.04+,
    # Fedora 38+) that block pip --user without it.
    $pip_cmd install --user --quiet --break-system-packages mempalace 2>/dev/null \
      || $pip_cmd install --user --quiet mempalace
    python3 -c "import mempalace" 2>/dev/null \
      && ok "MemPalace installed" \
      || warn "MemPalace install failed — run: pip3 install --user mempalace"
  fi

  # ── python-pptx: required by /pptx artifact command ──────────────────────
  if python3 -c "import pptx" 2>/dev/null; then
    ok "python-pptx already installed"
  else
    info "Installing python-pptx (for /pptx command)..."
    $pip_cmd install --user --quiet --break-system-packages python-pptx 2>/dev/null \
      || $pip_cmd install --user --quiet python-pptx
    python3 -c "import pptx" 2>/dev/null \
      && ok "python-pptx installed" \
      || warn "python-pptx install failed — run: pip3 install --user python-pptx"
  fi

  # Write MemPalace config entry into local.env so milliwaysd auto-connects.
  local cfg_dir="$HOME/.config/milliways"
  local env_file="$cfg_dir/local.env"
  mkdir -p "$cfg_dir"
  if ! grep -q "MILLIWAYS_MEMPALACE_MCP_CMD" "$env_file" 2>/dev/null; then
    printf '\n# MemPalace — project memory (injected before every prompt)\n' >> "$env_file"
    printf 'MILLIWAYS_MEMPALACE_MCP_CMD=python3 -m mempalace.mcp_server\n'  >> "$env_file"
    printf 'MILLIWAYS_MEMPALACE_MCP_ARGS=--palace %s/.mempalace\n' "$HOME"  >> "$env_file"
    ok "MemPalace config written → $env_file"
  fi
}

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
printf '    milliwaysd &              # start the agent daemon\n'
printf '    milliwaysctl status       # check runner availability\n'
printf '    milliways                 # open the terminal\n'
if [ "$PLATFORM" = "darwin" ]; then
  printf '    open /Applications/MilliWays.app   # native terminal\n'
fi
printf '\n'
printf '  Docs: https://github.com/%s\n\n' "$REPO"
