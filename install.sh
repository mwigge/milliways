#!/usr/bin/env bash
# milliways installer — builds and installs all four binaries from source.
#
# Builds from a local checkout (run from the repo root). For an end-user
# remote install, the workflow is:
#   git clone https://github.com/mwigge/milliways.git
#   cd milliways
#   ./install.sh
#
# Outputs (default $PREFIX/bin):
#   milliways         — legacy in-host REPL (`milliways --repl`)
#   milliways-term    — wezterm fork (the cockpit terminal)
#   milliwaysd        — long-running JSON-RPC daemon
#   milliwaysctl      — thin client (status, agents, bridge, ...)
#
# Set PREFIX to override (default: $HOME/.local).
# Set SKIP_TERM=1 to skip the Rust build (long; ~30 min on first run).
set -euo pipefail

PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"

cd "$REPO_ROOT"

# ---------------------------------------------------------------------------
# Pre-flight: required toolchains
# ---------------------------------------------------------------------------
need() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "milliways install: $1 not found on PATH." >&2
        echo "  $2" >&2
        exit 1
    fi
}

need go "Install Go 1.22+: https://go.dev/dl/"
if [ "${SKIP_TERM:-0}" != "1" ]; then
    need cargo "Install Rust: https://www.rust-lang.org/tools/install"
fi

# Optional: go-jsonschema is only needed when the schema or generated types
# are out of sync (CI codegen-drift gate). Surface but don't fail.
if ! command -v go-jsonschema >/dev/null 2>&1; then
    if ! [ -x "$(go env GOPATH)/bin/go-jsonschema" ]; then
        echo "milliways install: go-jsonschema not found (only needed for codegen)."
        echo "  install: go install github.com/atombender/go-jsonschema@latest"
    fi
fi

mkdir -p "$BIN_DIR"

# ---------------------------------------------------------------------------
# Go binaries (~10s)
# ---------------------------------------------------------------------------
echo "==> Building Go binaries..."

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
GO_LDFLAGS="-X main.version=$VERSION"

build_go() {
    local out="$1"; shift
    local pkg="$1"; shift
    if [ ! -d "$pkg" ]; then
        echo "  skip $out: $pkg not present"
        return 0
    fi
    echo "  building $out from $pkg"
    go build -ldflags "$GO_LDFLAGS" -o "$BIN_DIR/$out" "./$pkg"
}

build_go milliways      cmd/milliways
build_go milliwaysd     cmd/milliwaysd
build_go milliwaysctl   cmd/milliwaysctl

# ---------------------------------------------------------------------------
# Rust: milliways-term (wezterm fork)
# ---------------------------------------------------------------------------
if [ "${SKIP_TERM:-0}" = "1" ]; then
    echo "==> SKIP_TERM=1 — skipping Rust build."
else
    if [ ! -d crates/milliways-term ]; then
        echo "==> crates/milliways-term not present — skipping milliways-term build."
        echo "    (run \`git submodule\` or rerun the OpenSpec Phase 2 import if you expected it)"
    else
        echo "==> Building milliways-term (Rust, may take 5-30 min on first run)..."
        cargo build --release \
            --manifest-path crates/milliways-term/Cargo.toml \
            -p wezterm-gui
        install -m 0755 \
            crates/milliways-term/target/release/milliways-term \
            "$BIN_DIR/milliways-term"
    fi
fi

# ---------------------------------------------------------------------------
# Wezterm Lua helper — make `require('milliways')` work from the user's
# wezterm config. Installs `etc/milliways.lua` to
# `$PREFIX/share/milliways/milliways.lua` (default
# `~/.local/share/milliways/milliways.lua`). The sample config in
# `crates/milliways-term/milliways/etc/sample-wezterm.lua` extends
# wezterm's package.path to include that directory.
# ---------------------------------------------------------------------------
LUA_SRC="crates/milliways-term/milliways/etc/milliways.lua"
LUA_DEST_DIR="$PREFIX/share/milliways"
if [ -f "$LUA_SRC" ]; then
    echo "==> Installing wezterm Lua helper to $LUA_DEST_DIR/milliways.lua"
    mkdir -p "$LUA_DEST_DIR"
    install -m 0644 "$LUA_SRC" "$LUA_DEST_DIR/milliways.lua"
    if [ -f "crates/milliways-term/milliways/etc/sample-wezterm.lua" ]; then
        install -m 0644 \
            "crates/milliways-term/milliways/etc/sample-wezterm.lua" \
            "$LUA_DEST_DIR/sample-wezterm.lua"
    fi
else
    echo "==> $LUA_SRC missing — skipping wezterm Lua helper install."
fi

# ---------------------------------------------------------------------------
# Verify
# ---------------------------------------------------------------------------
echo
echo "==> Installed:"
for b in milliways milliwaysd milliwaysctl milliways-term; do
    if [ -x "$BIN_DIR/$b" ]; then
        size=$(du -h "$BIN_DIR/$b" | cut -f1)
        printf "    %-15s  %5s  %s\n" "$b" "$size" "$BIN_DIR/$b"
    fi
done
if [ -f "$LUA_DEST_DIR/milliways.lua" ]; then
    printf "    %-15s         %s\n" "milliways.lua" "$LUA_DEST_DIR/milliways.lua"
fi

echo
echo "Make sure $BIN_DIR is in your PATH:"
echo "    export PATH=\"\$PATH:$BIN_DIR\""
echo
echo "Quick start:"
echo "    milliwaysd &                                   # start the daemon"
echo "    milliwaysctl ping                              # smoke test"
echo "    milliwaysctl agents                            # list runners"
echo "    milliways --repl                               # legacy REPL fallback"
echo "    milliways-term                                 # cockpit terminal"
echo
echo "Wezterm cockpit (status bar + keybindings):"
echo "    cp $LUA_DEST_DIR/sample-wezterm.lua \\"
echo "       ~/.config/wezterm/wezterm.lua"
