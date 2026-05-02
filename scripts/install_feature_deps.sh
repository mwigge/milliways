#!/usr/bin/env bash
# install_feature_deps.sh — install README-promised Milliways feature deps.
#
# Installs into Milliways-owned prefixes instead of relying on user-site pip or
# globally writable npm. This makes one-liner installs and package installs
# deterministic across fresh distro containers.
set -euo pipefail

PREFIX="${PREFIX:-$HOME/.local}"
SHARE_DIR="${SHARE_DIR:-$PREFIX/share/milliways}"
PY_VENV="${MILLIWAYS_PY_VENV:-$SHARE_DIR/python}"
NODE_PREFIX="${MILLIWAYS_NODE_PREFIX:-$SHARE_DIR/node}"
WRITE_LOCAL_ENV="${MILLIWAYS_WRITE_LOCAL_ENV:-1}"
INSTALL_SYSTEM_DEPS="${MILLIWAYS_INSTALL_SYSTEM_DEPS:-1}"
CODEGRAPH_PACKAGE="${MILLIWAYS_CODEGRAPH_NPM_PACKAGE:-@colbymchenry/codegraph}"

if [ -t 1 ]; then
  RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
else
  RED=''; GREEN=''; YELLOW=''; CYAN=''; NC=''
fi

info()  { printf "${CYAN}  →${NC} %s\n" "$*"; }
ok()    { printf "${GREEN}  ✓${NC} %s\n" "$*"; }
warn()  { printf "${YELLOW}  !${NC} %s\n" "$*" >&2; }
fatal() { printf "${RED}  ✗${NC} %s\n" "$*" >&2; exit 1; }

run_privileged() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    return 1
  fi
}

need_system_deps() {
  command -v python3 >/dev/null 2>&1 || return 0
  python3 -m venv --help >/dev/null 2>&1 || return 0
  command -v git >/dev/null 2>&1 || return 0
  command -v npm >/dev/null 2>&1 || return 0
  return 1
}

install_system_deps() {
  [ "$INSTALL_SYSTEM_DEPS" = "1" ] || return 0
  need_system_deps || return 0

  info "Installing feature prerequisites (python3, pip/venv, git, npm)..."
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    run_privileged apt-get update -qq
    run_privileged apt-get install -yqq --no-install-recommends python3 python3-venv python3-pip git nodejs npm
  elif command -v dnf >/dev/null 2>&1; then
    run_privileged dnf install -y python3 python3-pip git nodejs npm
  elif command -v pacman >/dev/null 2>&1; then
    run_privileged pacman -Sy --noconfirm python python-pip git nodejs npm
  elif command -v brew >/dev/null 2>&1; then
    brew install python git node
  else
    warn "No supported package manager found for feature prerequisites"
  fi
}

ensure_python_features() {
  command -v python3 >/dev/null 2>&1 || fatal "python3 not found; cannot install MemPalace or python-pptx"
  python3 -m venv --help >/dev/null 2>&1 || fatal "python3 venv support not found; install python3-venv"

  mkdir -p "$SHARE_DIR"
  if [ ! -x "$PY_VENV/bin/python" ]; then
    info "Creating Milliways Python environment..."
    python3 -m venv "$PY_VENV"
  fi

  local py="$PY_VENV/bin/python"
  info "Installing Python feature packages (MemPalace, python-pptx)..."
  "$py" -m pip install --quiet --upgrade pip setuptools wheel
  "$py" -m pip install --quiet --upgrade mempalace python-pptx

  "$py" -c "import mempalace, pptx" >/dev/null 2>&1 \
    || fatal "Python feature packages installed but cannot be imported"
  ok "Python feature packages ready in $PY_VENV"
}

ensure_codegraph() {
  mkdir -p "$NODE_PREFIX"
  if [ -x "$NODE_PREFIX/bin/codegraph" ]; then
    ok "CodeGraph already installed in $NODE_PREFIX"
    return 0
  fi
  if command -v codegraph >/dev/null 2>&1; then
    ok "CodeGraph already available: $(command -v codegraph)"
    return 0
  fi
  command -v npm >/dev/null 2>&1 || fatal "npm not found; cannot install CodeGraph"

  info "Installing CodeGraph..."
  npm install --prefix "$NODE_PREFIX" --global --silent "$CODEGRAPH_PACKAGE"
  [ -x "$NODE_PREFIX/bin/codegraph" ] || fatal "CodeGraph install did not produce $NODE_PREFIX/bin/codegraph"
  ok "CodeGraph ready in $NODE_PREFIX"
}

set_local_env() {
  local key="$1" value="$2" env_file="$3"
  mkdir -p "$(dirname "$env_file")"
  touch "$env_file"
  local tmp="${env_file}.tmp.$$"
  awk -F= -v key="$key" '$1 != key { print }' "$env_file" > "$tmp"
  printf '%s=%s\n' "$key" "$value" >> "$tmp"
  mv "$tmp" "$env_file"
  chmod 0600 "$env_file" 2>/dev/null || true
}

write_local_env() {
  [ "$WRITE_LOCAL_ENV" = "1" ] || return 0
  local cfg_dir="$HOME/.config/milliways"
  local env_file="$cfg_dir/local.env"
  local py="$PY_VENV/bin/python"
  local codegraph_cmd="$NODE_PREFIX/bin/codegraph"
  command -v codegraph >/dev/null 2>&1 && codegraph_cmd="$(command -v codegraph)"
  [ -x "$codegraph_cmd" ] || codegraph_cmd="$NODE_PREFIX/bin/codegraph"

  set_local_env "MILLIWAYS_MEMPALACE_MCP_CMD" "$py" "$env_file"
  set_local_env "MILLIWAYS_MEMPALACE_MCP_ARGS" "-m mempalace.mcp_server --palace $HOME/.mempalace" "$env_file"
  set_local_env "MILLIWAYS_CODEGRAPH_MCP_CMD" "$codegraph_cmd" "$env_file"
  set_local_env "MILLIWAYS_CODEGRAPH_MCP_ARGS" "serve" "$env_file"
  set_local_env "MILLIWAYS_PATH" "$NODE_PREFIX/bin:$PY_VENV/bin" "$env_file"
  ok "Feature config written -> $env_file"
}

install_system_deps
ensure_python_features
ensure_codegraph
write_local_env
