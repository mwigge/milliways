#!/usr/bin/env bash
# install_local.sh — install llama.cpp + a Unsloth-quantised model
# so milliways' /local runner has something to talk to.
#
# Defaults to Devstral-Small-2505 (Mistral AI, France) — EU-developed, Apache 2.0,
# native OpenAI tool_calls JSON, top SWE-bench score in class. Requires 16GB RAM.
# Swap model by re-running with MODEL_REPO=... MODEL_ALIAS=...

set -euo pipefail

BIND_HOST="${BIND_HOST:-127.0.0.1}"
# 8765 — uncommon enough to avoid the usual web/dev-tunnel collisions on 8080.
PORT="${PORT:-8765}"
MODEL_REPO="${MODEL_REPO:-unsloth/Devstral-Small-2505-GGUF}"
MODEL_QUANT="${MODEL_QUANT:-Q4_K_M}"
MODEL_ALIAS="${MODEL_ALIAS:-devstral-small}"
CTX_SIZE="${CTX_SIZE:-32768}"
LOG_DIR="${LOG_DIR:-$HOME/.local/share/milliways/local}"
MODEL_DIR="${MODEL_DIR:-$HOME/.local/share/milliways/models}"

color() { printf '\033[1;%sm%s\033[0m\n' "$1" "$2"; }
info()  { color 36 "==> $*"; }
ok()    { color 32 "[ok] $*"; }
warn()  { color 33 "[!]  $*"; }
fail()  { color 31 "[x]  $*"; exit 1; }

OS="$(uname -s)"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
port_in_use() {
  # Bash builtin /dev/tcp avoids the BSD-vs-Linux netcat divergence and the
  # missing-lsof case on minimal containers.
  (echo > "/dev/tcp/127.0.0.1/$1") >/dev/null 2>&1
}

pick_free_port() {
  local p="$1"
  for _ in $(seq 1 20); do
    if ! port_in_use "$p"; then
      echo "$p"
      return
    fi
    p=$((p + 1))
  done
  fail "could not find a free port near $1 — set PORT=NNNN and re-run"
}

# Ensure Homebrew and ~/.local/bin are on PATH when launched from a GUI app.
export PATH="/opt/homebrew/bin:/usr/local/bin:$HOME/.local/bin:$PATH"

# If a milliways llama-server is already running and reachable, reuse its port
# rather than starting a new instance. This handles the case where the user
# runs /install-local-server again after the server is already up.
if port_in_use "$PORT"; then
  if curl -sf "http://${BIND_HOST}:${PORT}/v1/models" >/dev/null 2>&1; then
    ok "llama-server already running on port $PORT — reusing"
    # Write the endpoint to local.env and exit successfully.
    env_file="${XDG_CONFIG_HOME:-$HOME/.config}/milliways/local.env"
    mkdir -p "$(dirname "$env_file")"
    tmp="$(mktemp)"
    grep -v "^MILLIWAYS_LOCAL_ENDPOINT=" "$env_file" 2>/dev/null > "$tmp" || true
    printf 'MILLIWAYS_LOCAL_ENDPOINT=http://%s:%s/v1\n' "$BIND_HOST" "$PORT" >> "$tmp"
    mv "$tmp" "$env_file" && chmod 0600 "$env_file"
    ok "Endpoint already active: http://${BIND_HOST}:${PORT}/v1"
    exit 0
  fi
  warn "port $PORT is already in use (likely an SSH tunnel or another dev service)"
  PORT="$(pick_free_port $((PORT + 1)))"
  ok "using port $PORT instead"
fi

# ---------------------------------------------------------------------------
# 1. Install llama.cpp
# ---------------------------------------------------------------------------
install_llamacpp() {
  if command -v llama-server >/dev/null 2>&1; then
    local found
    found="$(command -v llama-server)"
    # Reject the smoke-mode Python stub — it's a bash script wrapping python3,
    # not a real llama.cpp binary. Check for the stub marker in the first line.
    if head -1 "$found" 2>/dev/null | grep -q "bash" && grep -q "python3" "$found" 2>/dev/null; then
      warn "Found stub llama-server at $found — replacing with real binary"
      rm -f "$found"
    else
      ok "llama-server already installed: $found"
      return
    fi
  fi

  case "$OS" in
    Darwin)
      if ! command -v brew >/dev/null 2>&1; then
        fail "Homebrew not found. Install from https://brew.sh first, then re-run this script."
      fi
      info "Installing llama.cpp via Homebrew (Metal-enabled)…"
      brew install llama.cpp
      ;;
    Linux)
      # Strategy 1: already bundled in the milliways package at /usr/bin/llama-server
      # (set by build-linux-amd64.sh) — nothing to do.
      if [ -x /usr/bin/llama-server ]; then
        ok "llama-server bundled in package: /usr/bin/llama-server"
        return
      fi

      # Strategy 2: download pre-built binary from the milliways release (same tag).
      local milliways_ver
      milliways_ver="$(milliways --version 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
      if [ -n "$milliways_ver" ]; then
        local asset_url="https://github.com/mwigge/milliways/releases/download/${milliways_ver}/llama-server_linux_amd64"
        info "Downloading bundled llama-server from milliways release ${milliways_ver}…"
        if curl -sSfL "$asset_url" -o /tmp/llama-server-dl 2>/dev/null; then
          run_privileged install -m 0755 /tmp/llama-server-dl /usr/local/bin/llama-server
          rm -f /tmp/llama-server-dl
          ok "llama-server installed from milliways release"
          return
        fi
      fi

      # Strategy 3: download directly from llama.cpp latest release.
      info "Fetching llama-server from llama.cpp releases…"
      local llama_tag
      llama_tag="$(curl -sSf https://api.github.com/repos/ggml-org/llama.cpp/releases/latest \
        | grep '"tag_name"' | cut -d'"' -f4 2>/dev/null)" || llama_tag=""
      if [ -n "$llama_tag" ]; then
        local tar_name="llama-${llama_tag}-bin-ubuntu-x64.tar.gz"
        local tar_url="https://github.com/ggml-org/llama.cpp/releases/download/${llama_tag}/${tar_name}"
        if curl -sSfL "$tar_url" -o "/tmp/${tar_name}" 2>/dev/null; then
          local entry
          entry="$(tar -tzf "/tmp/${tar_name}" | grep '/llama-server$' | head -1)"
          tar -xzf "/tmp/${tar_name}" -C /tmp "$entry"
          run_privileged install -m 0755 "/tmp/${entry}" /usr/local/bin/llama-server
          rm -rf "/tmp/${tar_name}" "/tmp/$(echo "$entry" | cut -d/ -f1)"
          ok "llama-server installed from llama.cpp ${llama_tag}"
          return
        fi
      fi

      # Strategy 4: build from source (last resort).
      if command -v apt-get >/dev/null 2>&1; then
        info "Installing build deps via apt-get…"
        sudo apt-get update -qq
        sudo apt-get install -yqq build-essential cmake git curl ca-certificates
      elif command -v dnf >/dev/null 2>&1; then
        sudo dnf install -y gcc-c++ cmake git curl ca-certificates
      elif command -v pacman >/dev/null 2>&1; then
        sudo pacman -Sy --noconfirm base-devel cmake git curl
      else
        fail "no supported package manager. Install llama.cpp manually from https://github.com/ggml-org/llama.cpp"
      fi
      info "Building llama.cpp from source (1–3 minutes)…"
      local tmp
      tmp="$(mktemp -d)"
      git clone --depth 1 https://github.com/ggml-org/llama.cpp "$tmp/llama.cpp"
      cmake -S "$tmp/llama.cpp" -B "$tmp/llama.cpp/build" -DGGML_CUDA=OFF -DLLAMA_CURL=OFF
      cmake --build "$tmp/llama.cpp/build" --config Release -j
      sudo install -m 0755 "$tmp/llama.cpp/build/bin/llama-server" /usr/local/bin/llama-server
      sudo install -m 0755 "$tmp/llama.cpp/build/bin/llama-cli"    /usr/local/bin/llama-cli
      rm -rf "$tmp"
      ;;
    *)
      fail "Unsupported OS: $OS — install llama.cpp manually from https://github.com/ggml-org/llama.cpp"
      ;;
  esac

  ok "llama-server installed: $(command -v llama-server)"
}

# ---------------------------------------------------------------------------
# 2. Download the GGUF directly via curl.
#    The reviewer flagged that llama-cli pre-fetch wastes RAM (it loads the
#    whole model). Plain curl on the resolve URL hits HF's CDN, bypasses any
#    proxy that intercepts the api/models endpoint, and is portable.
# ---------------------------------------------------------------------------
fetch_model() {
  local file="${MODEL_QUANT}.gguf"
  local url="https://huggingface.co/${MODEL_REPO}/resolve/main/$(basename "$MODEL_REPO" | sed 's/-GGUF$//')-${MODEL_QUANT}.gguf"
  local dest="$MODEL_DIR/$(basename "$MODEL_REPO")-${MODEL_QUANT}.gguf"

  mkdir -p "$MODEL_DIR"

  if [ -s "$dest" ]; then
    ok "model already cached: $dest"
    MODEL_PATH="$dest"
    return
  fi

  info "Downloading $MODEL_REPO ($MODEL_QUANT) → $dest"
  info "This is a one-time download (~1.1GB for the 1.5B model)."

  # -L follow redirects (HF → cas-bridge.xethub.hf.co → S3)
  # -f fail on HTTP error (so we don't write a 404 page as a fake .gguf)
  # -C - resume partial downloads
  if ! curl -fL -C - --retry 3 --retry-delay 5 -o "$dest" "$url"; then
    rm -f "$dest"
    fail "download failed. Check network/proxy and try: curl -fL -o $dest '$url'"
  fi

  ok "model cached at $dest"
  MODEL_PATH="$dest"
}

# ---------------------------------------------------------------------------
# 3. Write a launcher script and a launchd/systemd unit (best effort).
# ---------------------------------------------------------------------------
write_launcher() {
  mkdir -p "$LOG_DIR" "$HOME/.local/bin"
  # Resolve the full path to llama-server so the launcher works under launchd
  # and systemd, which do not inherit the user's shell PATH.
  local llama_bin
  llama_bin="$(command -v llama-server 2>/dev/null)" || llama_bin="llama-server"
  cat > "$HOME/.local/bin/milliways-local-server" <<EOF
#!/usr/bin/env bash
exec "$llama_bin" \\
  -m "$MODEL_PATH" \\
  --alias "$MODEL_ALIAS" \\
  --host "$BIND_HOST" \\
  --port "$PORT" \\
  --ctx-size "$CTX_SIZE" \\
  --jinja \\
  -fa on
EOF
  chmod +x "$HOME/.local/bin/milliways-local-server"
  ok "wrote $HOME/.local/bin/milliways-local-server"

  case "$OS" in
    Darwin)
      plist="$HOME/Library/LaunchAgents/dev.milliways.local.plist"
      mkdir -p "$(dirname "$plist")"
      cat > "$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>dev.milliways.local</string>
  <key>ProgramArguments</key><array>
    <string>$HOME/.local/bin/milliways-local-server</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>$LOG_DIR/server.log</string>
  <key>StandardErrorPath</key><string>$LOG_DIR/server.err</string>
</dict>
</plist>
EOF
      ok "wrote launchd unit: $plist"
      info "Load with:   launchctl load -w $plist"
      info "Unload with: launchctl unload  $plist"
      ;;
    Linux)
      unit="$HOME/.config/systemd/user/milliways-local.service"
      mkdir -p "$(dirname "$unit")"
      cat > "$unit" <<EOF
[Unit]
Description=milliways local model server (llama-server)

[Service]
ExecStart=$HOME/.local/bin/milliways-local-server
Restart=on-failure
StandardOutput=append:$LOG_DIR/server.log
StandardError=append:$LOG_DIR/server.err

[Install]
WantedBy=default.target
EOF
      ok "wrote systemd unit: $unit"
      info "Enable with: systemctl --user enable --now milliways-local"
      ;;
  esac
}

# ---------------------------------------------------------------------------
# 4. Smoke test — start the server in the background, wait for it to
#    answer /v1/models, kill it. Includes a liveness check so we don't
#    poll forever if the server died on startup.
# ---------------------------------------------------------------------------
smoke_test() {
  info "Starting llama-server for a smoke test (up to 60s)…"
  "$HOME/.local/bin/milliways-local-server" >"$LOG_DIR/smoke.log" 2>&1 &
  pid=$!
  trap 'kill $pid 2>/dev/null || true' EXIT

  for i in $(seq 1 60); do
    # Liveness: bail early if the process died (no point polling for 60s)
    if ! kill -0 "$pid" 2>/dev/null; then
      warn "llama-server exited during startup. Last 30 lines:"
      tail -30 "$LOG_DIR/smoke.log" >&2 || true
      trap - EXIT
      return 1
    fi
    if curl -sf "http://${BIND_HOST}:${PORT}/v1/models" >/dev/null 2>&1; then
      ok "llama-server responding on http://${BIND_HOST}:${PORT}/v1"
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
      trap - EXIT
      return 0
    fi
    if [ $((i % 10)) -eq 0 ]; then
      info "still waiting on llama-server (${i}s)…"
    fi
    sleep 1
  done

  warn "smoke test timed out — see $LOG_DIR/smoke.log"
  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  trap - EXIT
  return 1
}

smoke_mode() {
  info "milliways local-model installer smoke mode"

  # Use an isolated temp dir — never write the stub to ~/.local/bin where it
  # would persist after the smoke and fool install_llamacpp into thinking the
  # real server is already installed.
  local smoke_tmp
  smoke_tmp="$(mktemp -d)"
  trap 'rm -rf "$smoke_tmp"' EXIT

  mkdir -p "$LOG_DIR" "$MODEL_DIR"
  MODEL_PATH="$MODEL_DIR/smoke-model.gguf"
  : > "$MODEL_PATH"

  cat > "$smoke_tmp/llama-server" <<'EOF'
#!/usr/bin/env bash
while [ "$#" -gt 0 ]; do
  case "$1" in
    --host) host="$2"; shift 2 ;;
    --port) port="$2"; shift 2 ;;
    *) shift ;;
  esac
done
host="${host:-127.0.0.1}"
port="${port:-8765}"
python3 - "$host" "$port" <<'PY'
import json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/v1/models":
            data = json.dumps({"data": [{"id": "smoke-local"}]}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            return
        self.send_response(404); self.end_headers()
    def log_message(self, *_): return

HTTPServer((sys.argv[1], int(sys.argv[2])), Handler).serve_forever()
PY
EOF
  chmod +x "$smoke_tmp/llama-server"

  # Prepend the temp dir so write_launcher and smoke_test use the stub,
  # but the stub never touches ~/.local/bin.
  PATH="$smoke_tmp:$PATH"
  mkdir -p "$HOME/.local/bin"
  write_launcher
  smoke_test || fail "smoke local server did not respond"

  # Clean up: remove the stub launcher — it used the temp stub, not the real binary.
  rm -f "$HOME/.local/bin/milliways-local-server"
  trap - EXIT
  rm -rf "$smoke_tmp"

  ok "smoke local server installed"
}

# ---------------------------------------------------------------------------
main() {
  if [ "${MILLIWAYS_LOCAL_INSTALL_SMOKE:-0}" = "1" ]; then
    smoke_mode
    return
  fi

  info "milliways local-model installer"
  info "OS:         $OS"
  info "Model:      $MODEL_REPO ($MODEL_QUANT) → alias '$MODEL_ALIAS'"
  info "Endpoint:   http://${BIND_HOST}:${PORT}/v1"
  info "Context:    $CTX_SIZE tokens"
  echo

  install_llamacpp
  fetch_model
  write_launcher
  smoke_test || warn "Smoke test did not pass — server may still work. Try: milliways-local-server"

  echo
  ok "All set."
  info "To start the server in the foreground:"
  info "  milliways-local-server"
  info "To use it from milliways:"
  info "  /local"
  info "  hello, can you write a fizzbuzz in Go?"
  info ""
  local endpoint="http://${BIND_HOST}:${PORT}/v1"
  local env_file="${XDG_CONFIG_HOME:-$HOME/.config}/milliways/local.env"
  mkdir -p "$(dirname "$env_file")"
  # Write endpoint to local.env so milliways picks it up without shell profile changes.
  local tmp
  tmp="$(mktemp)"
  grep -v "^MILLIWAYS_LOCAL_ENDPOINT=" "$env_file" 2>/dev/null > "$tmp" || true
  printf 'MILLIWAYS_LOCAL_ENDPOINT=%s\n' "$endpoint" >> "$tmp"
  mv "$tmp" "$env_file"
  chmod 0600 "$env_file" 2>/dev/null || true
  ok "Endpoint written to $env_file — milliways will pick it up automatically."

  if [ "$PORT" != "8765" ]; then
    info "Note: port 8765 was in use, using $PORT instead."
  fi
}

main "$@"
