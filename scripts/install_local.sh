#!/usr/bin/env bash
# install_local.sh — install llama.cpp + a small Unsloth-quantised coder model
# so milliways' /local runner has something to talk to.
#
# Defaults to qwen2.5-coder-1.5b at Unsloth Q4_K_M — small enough to fit
# comfortably on a 16GB machine, fast enough to feel snappy, smart enough
# for completions and simple coding tasks. Bigger machines can swap in
# qwen2.5-coder-7b or deepseek-coder-v2:lite by re-running with MODEL_REPO=...

set -euo pipefail

BIND_HOST="${BIND_HOST:-127.0.0.1}"
# 8765 — uncommon enough to avoid the usual web/dev-tunnel collisions on 8080.
PORT="${PORT:-8765}"
MODEL_REPO="${MODEL_REPO:-unsloth/Qwen2.5-Coder-1.5B-Instruct-GGUF}"
MODEL_QUANT="${MODEL_QUANT:-Q4_K_M}"
MODEL_ALIAS="${MODEL_ALIAS:-qwen2.5-coder-1.5b}"
CTX_SIZE="${CTX_SIZE:-16384}"
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

if port_in_use "$PORT"; then
  warn "port $PORT is already in use (likely an SSH tunnel or another dev service)"
  PORT="$(pick_free_port $((PORT + 1)))"
  ok "using port $PORT instead"
fi

# ---------------------------------------------------------------------------
# 1. Install llama.cpp
# ---------------------------------------------------------------------------
install_llamacpp() {
  if command -v llama-server >/dev/null 2>&1; then
    ok "llama-server already installed: $(command -v llama-server)"
    return
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
      if command -v apt-get >/dev/null 2>&1; then
        info "Installing build deps via apt-get…"
        sudo apt-get update -qq
        # libcurl4-openssl-dev is required by -DLLAMA_CURL=ON for HF integration
        sudo apt-get install -yqq build-essential cmake git curl libcurl4-openssl-dev ca-certificates
      elif command -v dnf >/dev/null 2>&1; then
        sudo dnf install -y gcc-c++ cmake git curl libcurl-devel ca-certificates
      elif command -v pacman >/dev/null 2>&1; then
        sudo pacman -Sy --noconfirm base-devel cmake git curl
      else
        fail "no supported package manager (apt-get / dnf / pacman). Install build tools manually and re-run."
      fi
      info "Building llama.cpp from source (this takes 1–3 minutes)…"
      tmp="$(mktemp -d)"
      git clone --depth 1 https://github.com/ggml-org/llama.cpp "$tmp/llama.cpp"
      # LLAMA_CURL=OFF — we download the GGUF ourselves with curl so we don't
      # need the dev libcurl headers at build time. Keeps the Linux path simple.
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
  cat > "$HOME/.local/bin/milliways-local-server" <<EOF
#!/usr/bin/env bash
exec llama-server \\
  -m "$MODEL_PATH" \\
  --alias "$MODEL_ALIAS" \\
  --host "$BIND_HOST" \\
  --port "$PORT" \\
  --ctx-size "$CTX_SIZE" \\
  --jinja
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

# ---------------------------------------------------------------------------
main() {
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
  if [ "$PORT" != "8765" ]; then
    warn "milliways defaults to port 8765 — yours runs on $PORT"
    info "Add this to your shell profile so milliways finds it:"
    info "  export MILLIWAYS_LOCAL_ENDPOINT=http://${BIND_HOST}:${PORT}/v1"
  else
    info "Default endpoint http://${BIND_HOST}:${PORT}/v1 — milliways picks this up automatically."
    info "Override with MILLIWAYS_LOCAL_ENDPOINT if you need a different backend."
  fi
}

main "$@"
