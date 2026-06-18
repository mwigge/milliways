#!/usr/bin/env bash
# install_local_amd_hip.sh — AMD GPU local model stack using llama.cpp HIP.
#
# This is intentionally separate from the rs-llmctl/Candle installer. Candle's
# current Linux GPU path builds NVIDIA PTX through nvcc; AMD ROCm machines use
# llama.cpp's HIP backend until rs-llmctl has a ROCm-native Candle backend.

set -euo pipefail

BIND_HOST="${BIND_HOST:-127.0.0.1}"
PORT="${PORT:-8765}"
DEFAULT_MODEL_REPO="unsloth/Qwen3-14B-GGUF"
DEFAULT_MODEL_FILE="Qwen3-14B-Q4_K_M.gguf"
MODEL_REPO="${MODEL_REPO:-$DEFAULT_MODEL_REPO}"
MODEL_QUANT="${MODEL_QUANT:-Q4_K_M}"
MODEL_FILE="${MODEL_FILE:-}"
MODEL_ALIAS="${MODEL_ALIAS:-qwen3-14b}"
CTX_SIZE="${CTX_SIZE:-32768}"
N_GPU_LAYERS="${N_GPU_LAYERS:-99}"
N_PARALLEL="${N_PARALLEL:-1}"
BATCH_SIZE="${BATCH_SIZE:-512}"
UBATCH_SIZE="${UBATCH_SIZE:-256}"
CACHE_TYPE_K="${CACHE_TYPE_K:-q8_0}"
MODEL_TEMP="${MODEL_TEMP:-0.60}"
LOG_DIR="${LOG_DIR:-$HOME/.local/share/milliways/local}"
MODEL_DIR="${MODEL_DIR:-$HOME/.local/share/milliways/models}"
LLAMA_BIN_DIR="${LLAMA_BIN_DIR:-$HOME/.local/bin}"
LLAMA_LIB_DIR="${LLAMA_LIB_DIR:-$HOME/.local/lib/milliways}"
LLAMA_CPP_REF="${LLAMA_CPP_REF:-24bba7b98ea1544cc89352c7a573baedcb831a64}"
OTEL_EXPORTER_OTLP_ENDPOINT="${OTEL_EXPORTER_OTLP_ENDPOINT:-http://127.0.0.1:4318}"

color() { printf '\033[1;%sm%s\033[0m\n' "$1" "$2"; }
info()  { color 36 "==> $*"; }
ok()    { color 32 "[ok] $*"; }
warn()  { color 33 "[!]  $*"; }
fail()  { color 31 "[x]  $*"; exit 1; }

export PATH="/opt/rocm/bin:/opt/rocm/llvm/bin:$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
export HIP_PLATFORM="${HIP_PLATFORM:-amd}"

port_in_use() {
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

require_rocm() {
  command -v hipcc >/dev/null 2>&1 || fail "hipcc not found. Install ROCm HIP SDK and ensure /opt/rocm/bin is on PATH."
  command -v cmake >/dev/null 2>&1 || fail "cmake not found. Install cmake."
  command -v git >/dev/null 2>&1 || fail "git not found. Install git."
}

install_llama_shared_libs() {
  local src_dir="$1"
  mkdir -p "$LLAMA_LIB_DIR"
  cp -a "$src_dir"/*.so* "$LLAMA_LIB_DIR"/ 2>/dev/null || true
  if command -v readelf >/dev/null 2>&1; then
    local lib soname base
    for lib in "$LLAMA_LIB_DIR"/lib*.so.*.*; do
      [ -e "$lib" ] || continue
      soname="$(readelf -d "$lib" 2>/dev/null | sed -n 's/.*Library soname: \[\([^]]*\)\].*/\1/p' | head -1)"
      [ -n "$soname" ] || continue
      base="${soname%%.so*}.so"
      ln -sfn "$(basename "$lib")" "$LLAMA_LIB_DIR/$soname"
      ln -sfn "$soname" "$LLAMA_LIB_DIR/$base"
    done
  fi
}

llama_server_is_hip() {
  local bin="$1"
  LD_LIBRARY_PATH="$LLAMA_LIB_DIR:/opt/rocm/lib:/opt/rocm/lib64:${LD_LIBRARY_PATH:-}" \
    ldd "$bin" 2>/dev/null | grep -Eq 'libamdhip64|libhipblas|librocblas'
}

install_llamacpp_hip() {
  mkdir -p "$LLAMA_BIN_DIR" "$LLAMA_LIB_DIR"
  if command -v llama-server >/dev/null 2>&1 && llama_server_is_hip "$(command -v llama-server)"; then
    ok "HIP-enabled llama-server already available: $(command -v llama-server)"
    return
  fi
  if [ -x "$LLAMA_BIN_DIR/llama-server" ] && llama_server_is_hip "$LLAMA_BIN_DIR/llama-server"; then
    ok "HIP-enabled llama-server already installed: $LLAMA_BIN_DIR/llama-server"
    return
  fi

  require_rocm
  info "Building llama.cpp with HIP backend (${LLAMA_CPP_REF})..."
  local tmp
  tmp="$(mktemp -d)"
  trap "rm -rf '$tmp'" EXIT
  git clone --depth 1 https://github.com/ggml-org/llama.cpp "$tmp/llama.cpp"
  (cd "$tmp/llama.cpp" && git fetch --depth 1 origin "$LLAMA_CPP_REF" && git checkout --detach FETCH_HEAD)
  cmake -S "$tmp/llama.cpp" -B "$tmp/llama.cpp/build" \
    -DGGML_HIP=ON \
    -DGGML_NATIVE=OFF \
    -DAMDGPU_TARGETS="${AMDGPU_TARGETS:-gfx1100}" \
    -DCMAKE_BUILD_TYPE=Release \
    -DLLAMA_CURL=OFF \
    -DLLAMA_BUILD_UI=OFF \
    -DLLAMA_USE_PREBUILT_UI=OFF \
    -DLLAMA_BUILD_TESTS=OFF \
    -DLLAMA_BUILD_EXAMPLES=OFF
  cmake --build "$tmp/llama.cpp/build" --config Release --target llama-server -j
  install -m 0755 "$tmp/llama.cpp/build/bin/llama-server" "$LLAMA_BIN_DIR/llama-server"
  install -m 0755 "$tmp/llama.cpp/build/bin/llama-cli" "$LLAMA_BIN_DIR/llama-cli" 2>/dev/null || true
  install_llama_shared_libs "$tmp/llama.cpp/build/bin"
  rm -rf "$tmp"
  trap - EXIT
  ok "llama-server installed: $LLAMA_BIN_DIR/llama-server"
}

fetch_model() {
  local file="$MODEL_FILE"
  if [ -z "$file" ]; then
    if [ "$MODEL_REPO" = "$DEFAULT_MODEL_REPO" ]; then
      file="$DEFAULT_MODEL_FILE"
    else
      file="$(basename "$MODEL_REPO" | sed 's/-GGUF$//')-${MODEL_QUANT}.gguf"
    fi
  fi
  local url="https://huggingface.co/${MODEL_REPO}/resolve/main/${file}"
  local dest="$MODEL_DIR/${file}"
  mkdir -p "$MODEL_DIR"
  if [ -s "$dest" ]; then
    ok "model already cached: $dest"
    MODEL_PATH="$dest"
    return
  fi
  info "Downloading $MODEL_REPO ($MODEL_QUANT) -> $dest"
  if ! curl -fL -C - --retry 3 --retry-delay 5 -o "$dest" "$url"; then
    rm -f "$dest"
    fail "download failed. Check network/proxy and try manually: curl -fL -o '$dest' '$url'"
  fi
  ok "model cached at $dest"
  MODEL_PATH="$dest"
}

write_launcher() {
  mkdir -p "$LOG_DIR" "$HOME/.local/bin"
  cat > "$HOME/.local/bin/milliways-local-server" <<EOF
#!/usr/bin/env bash
export PATH="/opt/rocm/bin:/opt/rocm/llvm/bin:\$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:\${PATH:-}"
export LD_LIBRARY_PATH="$LLAMA_LIB_DIR:/opt/rocm/lib:/opt/rocm/lib64:\${LD_LIBRARY_PATH:-}"
export HIP_PLATFORM="${HIP_PLATFORM:-amd}"
export OTEL_EXPORTER_OTLP_ENDPOINT="${OTEL_EXPORTER_OTLP_ENDPOINT}"
exec "$LLAMA_BIN_DIR/llama-server" \\
  -m "$MODEL_PATH" \\
  --alias "$MODEL_ALIAS" \\
  --host "$BIND_HOST" \\
  --port "$PORT" \\
  --ctx-size "$CTX_SIZE" \\
  --parallel "$N_PARALLEL" \\
  --batch-size "$BATCH_SIZE" \\
  --ubatch-size "$UBATCH_SIZE" \\
  --n-gpu-layers "$N_GPU_LAYERS" \\
  --temp "$MODEL_TEMP" \\
  --cache-type-k "$CACHE_TYPE_K" \\
  --jinja \\
  --metrics \\
  --flash-attn off
EOF
  chmod +x "$HOME/.local/bin/milliways-local-server"
  ok "wrote $HOME/.local/bin/milliways-local-server"

  case "$(uname -s)" in
    Linux)
      local unit="$HOME/.config/systemd/user/milliways-local.service"
      mkdir -p "$(dirname "$unit")"
      cat > "$unit" <<EOF
[Unit]
Description=milliways AMD HIP local model server (llama.cpp)

[Service]
ExecStart=$HOME/.local/bin/milliways-local-server
Restart=on-failure
StandardOutput=append:$LOG_DIR/server.log
StandardError=append:$LOG_DIR/server.err

[Install]
WantedBy=default.target
EOF
      ok "wrote systemd unit: $unit"
      ;;
    *)
      warn "non-Linux AMD HIP install wrote launcher only"
      ;;
  esac
}

write_local_env() {
  local env_file="${XDG_CONFIG_HOME:-$HOME/.config}/milliways/local.env"
  local endpoint="http://${BIND_HOST}:${PORT}/v1"
  mkdir -p "$(dirname "$env_file")"
  local tmp
  tmp="$(mktemp "$(dirname "$env_file")/.local.env.XXXXXX")"
  chmod 0600 "$tmp" 2>/dev/null || true
  awk -F= '$1 != "MILLIWAYS_LOCAL_ENDPOINT" && $1 != "MILLIWAYS_LOCAL_MODEL" && $1 != "MILLIWAYS_LOCAL_API_KEY" && $1 != "MILLIWAYS_LOCAL_BACKEND"' "$env_file" 2>/dev/null > "$tmp" || true
  printf 'MILLIWAYS_LOCAL_ENDPOINT=%s\n' "$endpoint" >> "$tmp"
  printf 'MILLIWAYS_LOCAL_MODEL=%s\n' "$MODEL_ALIAS" >> "$tmp"
  printf 'MILLIWAYS_LOCAL_BACKEND=llama.cpp-hip\n' >> "$tmp"
  mv "$tmp" "$env_file"
  chmod 0600 "$env_file" 2>/dev/null || true
  ok "endpoint written to $env_file"
}

main() {
  info "milliways AMD local-model installer (llama.cpp HIP)"
  info "Model:    $MODEL_REPO ($MODEL_QUANT) -> alias '$MODEL_ALIAS'"
  info "Endpoint: http://${BIND_HOST}:${PORT}/v1"
  info "Context:  $CTX_SIZE tokens"
  info "OTLP:     $OTEL_EXPORTER_OTLP_ENDPOINT"
  echo

  if port_in_use "$PORT"; then
    warn "port $PORT is already in use"
    PORT="$(pick_free_port $((PORT + 1)))"
    ok "using port $PORT instead"
  fi

  install_llamacpp_hip
  fetch_model
  write_launcher
  write_local_env

  echo
  ok "All set."
  info "Start with: milliwaysctl local server-start"
}

main "$@"
