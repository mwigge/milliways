#!/usr/bin/env bash
# install_local.sh — install llama.cpp + a small Unsloth-quantised coder model
# so milliways' /local runner has something to talk to.
#
# Defaults to qwen2.5-coder-1.5b at Unsloth Q4_K_XL — small enough to fit
# comfortably on a 16GB machine, fast enough to feel snappy, smart enough
# for completions and simple coding tasks. Bigger machines can swap in
# qwen2.5-coder-7b or deepseek-coder-v2:lite by re-running with MODEL=...

set -euo pipefail

BIND_HOST="${BIND_HOST:-127.0.0.1}"
PORT="${PORT:-8080}"
MODEL_REPO="${MODEL_REPO:-unsloth/Qwen2.5-Coder-1.5B-Instruct-GGUF}"
MODEL_QUANT="${MODEL_QUANT:-Q4_K_M}"
MODEL_ALIAS="${MODEL_ALIAS:-qwen2.5-coder-1.5b}"
CTX_SIZE="${CTX_SIZE:-16384}"
LOG_DIR="${LOG_DIR:-$HOME/.local/share/milliways/local}"

color() { printf '\033[1;%sm%s\033[0m\n' "$1" "$2"; }
info()  { color 36 "==> $*"; }
ok()    { color 32 "✓ $*"; }
warn()  { color 33 "! $*"; }
fail()  { color 31 "✗ $*"; exit 1; }

OS="$(uname -s)"

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
        sudo apt-get install -yqq build-essential cmake git curl
      elif command -v dnf >/dev/null 2>&1; then
        sudo dnf install -y gcc-c++ cmake git curl
      fi
      info "Building llama.cpp from source (this takes 1–3 minutes)…"
      tmp="$(mktemp -d)"
      git clone --depth 1 https://github.com/ggml-org/llama.cpp "$tmp/llama.cpp"
      cmake -S "$tmp/llama.cpp" -B "$tmp/llama.cpp/build" -DGGML_CUDA=OFF -DLLAMA_CURL=ON
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
# 2. Pre-fetch the model so the first run isn't a 15-minute wait.
#    llama-server -hf will download into ~/Library/Caches/llama.cpp on macOS
#    or ~/.cache/llama.cpp on Linux. We just trigger the download by asking
#    llama-server to load the model and immediately exit.
# ---------------------------------------------------------------------------
fetch_model() {
  info "Pre-fetching $MODEL_REPO ($MODEL_QUANT)…"
  info "This is a one-time download (~1.1GB for the 1.5B model)."
  # llama-server will download the model on demand. We trigger it here with
  # a tiny prompt so the user doesn't pay the cost on the first /local prompt.
  set +e
  timeout 600 llama-cli \
    --hf-repo "$MODEL_REPO" \
    --hf-file "${MODEL_QUANT}.gguf" \
    -p "ok" -n 1 --no-warmup >/dev/null 2>&1
  rc=$?
  set -e
  if [ $rc -eq 0 ]; then
    ok "model cached"
  else
    warn "model pre-fetch returned $rc — first /local prompt may be slow as the GGUF downloads on demand"
  fi
}

# ---------------------------------------------------------------------------
# 3. Write a launcher script and a launchd/systemd unit (best effort).
# ---------------------------------------------------------------------------
write_launcher() {
  mkdir -p "$LOG_DIR" "$HOME/.local/bin"
  cat > "$HOME/.local/bin/milliways-local-server" <<EOF
#!/usr/bin/env bash
exec llama-server \\
  --hf-repo "$MODEL_REPO" \\
  --hf-file "${MODEL_QUANT}.gguf" \\
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
After=network.target

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
#    answer /v1/models, kill it. So the user knows everything works.
# ---------------------------------------------------------------------------
smoke_test() {
  info "Starting llama-server for a smoke test (10s)…"
  "$HOME/.local/bin/milliways-local-server" >"$LOG_DIR/smoke.log" 2>&1 &
  pid=$!
  trap 'kill $pid 2>/dev/null || true' EXIT

  for i in $(seq 1 30); do
    if curl -sf "http://${BIND_HOST}:${PORT}/v1/models" >/dev/null 2>&1; then
      ok "llama-server responding on http://${BIND_HOST}:${PORT}/v1"
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
      trap - EXIT
      return
    fi
    sleep 1
  done

  warn "smoke test timed out — see $LOG_DIR/smoke.log"
  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  trap - EXIT
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
  smoke_test

  echo
  ok "All set."
  info "To start the server in the foreground:"
  info "  milliways-local-server"
  info "To use it from milliways:"
  info "  /local"
  info "  hello, can you write a fizzbuzz in Go?"
  info ""
  info "The default endpoint is http://${BIND_HOST}:${PORT}/v1 — milliways"
  info "picks this up automatically. Override with MILLIWAYS_LOCAL_ENDPOINT."
}

main "$@"
