#!/usr/bin/env bash
# install_local.sh — install rs-llmctl + a Unsloth-quantised model
# so milliways' /local runner has something to talk to.
#
# Defaults to Qwen2.5 7B GGUF because rs-llmctl's native Candle backend
# supports Qwen-family GGUF loading. Requires roughly 8-12GB RAM.
# Swap model by re-running with MODEL_REPO=... MODEL_ALIAS=...

set -euo pipefail

BIND_HOST="${BIND_HOST:-127.0.0.1}"
# 8765 — uncommon enough to avoid the usual web/dev-tunnel collisions on 8080.
PORT="${PORT:-8765}"
DEFAULT_MODEL_REPO="raaedk/Qwen2.5-7B-Instruct-Q4_K_M-GGUF"
DEFAULT_MODEL_FILE="qwen2.5-7b-instruct-q4_k_m.gguf"
MODEL_REPO="${MODEL_REPO:-$DEFAULT_MODEL_REPO}"
MODEL_QUANT="${MODEL_QUANT:-Q4_K_M}"
MODEL_FILE="${MODEL_FILE:-}"
MODEL_ALIAS="${MODEL_ALIAS:-qwen2.5-7b}"
MODEL_FAMILY="${MODEL_FAMILY:-}"
CTX_SIZE="${CTX_SIZE:-32768}"
LLAMA_CPP_ACCEL="${LLAMA_CPP_ACCEL:-cpu}"
N_GPU_LAYERS="${N_GPU_LAYERS:-99}"
MODEL_TEMP="${MODEL_TEMP:-0.15}"
LOG_DIR="${LOG_DIR:-$HOME/.local/share/milliways/local}"
MODEL_DIR="${MODEL_DIR:-$HOME/.local/share/milliways/models}"
LLAMA_BIN_DIR="${LLAMA_BIN_DIR:-$HOME/.local/bin}"
LLAMA_LIB_DIR="${LLAMA_LIB_DIR:-$HOME/.local/lib/milliways}"
RS_LLMCTL_VERSION="${RS_LLMCTL_VERSION:-latest}"
RS_LLMCTL_REPO="${RS_LLMCTL_REPO:-mwigge/rs-llmctl}"
RS_LLMCTL_LOCAL_REPO="${RS_LLMCTL_LOCAL_REPO:-}"
RS_LLMCTL_CONFIG="${RS_LLMCTL_CONFIG:-${XDG_CONFIG_HOME:-$HOME/.config}/milliways/rs-llmctl.toml}"
RS_LLMCTL_DATA_DIR="${RS_LLMCTL_DATA_DIR:-$HOME/.local/share/milliways/rs-llmctl}"
RS_LLMCTL_SECRET_FILE="${RS_LLMCTL_SECRET_FILE:-${XDG_CONFIG_HOME:-$HOME/.config}/milliways/rs-llmctl-api-key.txt}"
LLMCTL_BIN="${RS_LLMCTL_BIN:-}"
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
MILLIWAYS_ROOT="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"

color() { printf '\033[1;%sm%s\033[0m\n' "$1" "$2"; }
info()  { color 36 "==> $*"; }
ok()    { color 32 "[ok] $*"; }
warn()  { color 33 "[!]  $*"; }
fail()  { color 31 "[x]  $*"; exit 1; }

toml_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '%s' "$value"
}

infer_model_family() {
  local haystack
  haystack="$(printf '%s %s' "$MODEL_ALIAS" "$MODEL_REPO" | tr '[:upper:]' '[:lower:]')"
  case "$haystack" in
    *qwen3*|*qwen*) printf '%s\n' "qwen3" ;;
    *devstral*|*mistral*) printf '%s\n' "mistral" ;;
    *gemma*) printf '%s\n' "gemma4" ;;
    *deepseek*) printf '%s\n' "deepseek" ;;
    *kimi*) printf '%s\n' "kimi" ;;
    *minimax*|*mini-max*) printf '%s\n' "minimax" ;;
    *) printf '%s\n' "qwen3" ;;
  esac
}

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

install_llama_shared_libs() {
  local src_dir="$1"
  local dest_dir="${2:-$LLAMA_LIB_DIR}"
  if ! compgen -G "$src_dir/*.so*" >/dev/null && ! compgen -G "$src_dir/*.dylib" >/dev/null; then
    return 0
  fi
  mkdir -p "$dest_dir"
  local dylib
  cp -a "$src_dir"/*.so* "$dest_dir"/ 2>/dev/null || true
  cp -a "$src_dir"/*.dylib "$dest_dir"/ 2>/dev/null || true
  if command -v readelf >/dev/null 2>&1; then
    local lib soname base
    for lib in "$dest_dir"/lib*.so.*.*; do
      [ -e "$lib" ] || continue
      soname="$(readelf -d "$lib" 2>/dev/null | sed -n 's/.*Library soname: \[\([^]]*\)\].*/\1/p' | head -1)"
      [ -n "$soname" ] || continue
      base="${soname%%.so*}.so"
      ln -sfn "$(basename "$lib")" "$dest_dir/$soname"
      ln -sfn "$soname" "$dest_dir/$base"
    done
  fi
  mkdir -p "$LLAMA_BIN_DIR"
  local backend
  for backend in "$dest_dir"/libggml-cuda.so "$dest_dir"/libggml-hip.so "$dest_dir"/libggml-vulkan.so "$dest_dir"/libggml-kompute.so "$dest_dir"/libggml-metal.dylib "$dest_dir"/libggml-cpu-*.so "$dest_dir"/libggml-rpc.so; do
    [ -e "$backend" ] || continue
    ln -sfn "$backend" "$LLAMA_BIN_DIR/$(basename "$backend")"
  done
  ok "llama.cpp shared libraries installed: $dest_dir"
}

llama_binary_has_missing_libs() {
  local bin="$1"
  command -v ldd >/dev/null 2>&1 || return 1
  LD_LIBRARY_PATH="$LLAMA_LIB_DIR:/usr/lib/milliways:${LD_LIBRARY_PATH:-}" ldd "$bin" 2>/dev/null | grep -q "not found"
}

# Ensure Homebrew and ~/.local/bin are on PATH when launched from a GUI app.
export PATH="/opt/homebrew/bin:$HOME/.local/bin:/usr/local/bin:$PATH"

# ---------------------------------------------------------------------------
# 1. Install rs-llmctl. Legacy llama.cpp helpers remain for swap/setup paths.
# ---------------------------------------------------------------------------
reuse_existing_or_pick_port() {
  # If a compatible backend is already running, reachable, AND already serving
  # the requested model (MODEL_ALIAS as resolved by hardware detection / the
  # caller), reuse its port rather than starting a new instance — this handles
  # repeated installs after the local server is already up.
  #
  # If it is serving a *different* model, this is a model swap: don't reuse —
  # fall through, pick a fresh port, and install the requested model there.
  # write_local_env then points milliways at the new endpoint, replacing the
  # active configuration; the old process is left running but orphaned.
  if port_in_use "$PORT"; then
    local env_file existing_api_key existing_model models_out requested_alias
    local -a auth_args
    requested_alias="$MODEL_ALIAS"
    env_file="${XDG_CONFIG_HOME:-$HOME/.config}/milliways/local.env"
    existing_api_key="$(sed -n 's/^MILLIWAYS_LOCAL_API_KEY=//p' "$env_file" 2>/dev/null | tail -1 || true)"
    existing_model="$(sed -n 's/^MILLIWAYS_LOCAL_MODEL=//p' "$env_file" 2>/dev/null | tail -1 || true)"
    auth_args=()
    if [ -n "$existing_api_key" ]; then
      auth_args=(-H "Authorization: Bearer ${existing_api_key}")
    fi
    models_out="$(mktemp)"
    if curl -sf "${auth_args[@]}" "http://${BIND_HOST}:${PORT}/v1/models" >"$models_out" 2>/dev/null &&
      [ -n "$existing_model" ] && [ "$existing_model" = "$requested_alias" ] &&
      grep -q "\"${existing_model}\"" "$models_out"; then
      rm -f "$models_out"
      ok "OpenAI-compatible local server already running on port $PORT — reusing"
      API_KEY="$existing_api_key"
      MODEL_ALIAS="$existing_model"
      write_local_env
      ok "Endpoint already active: http://${BIND_HOST}:${PORT}/v1"
      exit 0
    fi
    rm -f "$models_out"
    if [ -n "$existing_model" ] && [ "$existing_model" != "$requested_alias" ]; then
      warn "port $PORT is serving '$existing_model' — switching to '$requested_alias' on a new port"
    else
      warn "port $PORT is already in use (likely an SSH tunnel or another dev service)"
    fi
    PORT="$(pick_free_port $((PORT + 1)))"
    ok "using port $PORT instead"
  fi
}

install_rs_llmctl() {
  if [ -n "$LLMCTL_BIN" ] && [ -x "$LLMCTL_BIN" ]; then
    ok "rs-llmctl available: $LLMCTL_BIN"
    return
  fi
  if command -v llmctl >/dev/null 2>&1; then
    LLMCTL_BIN="$(command -v llmctl)"
    ok "rs-llmctl available: $LLMCTL_BIN"
    return
  fi

  if install_rs_llmctl_from_local_repo; then
    return
  fi

  local install_ref
  if [ "$RS_LLMCTL_VERSION" = "latest" ]; then
    install_ref="main"
  else
    install_ref="$RS_LLMCTL_VERSION"
  fi
  info "Installing rs-llmctl ${RS_LLMCTL_VERSION}..."
  if ! curl -fsSL "https://raw.githubusercontent.com/${RS_LLMCTL_REPO}/${install_ref}/install.sh" | \
    PREFIX="$HOME/.local" RS_LLMCTL_VERSION="$RS_LLMCTL_VERSION" RS_LLMCTL_REPO="$RS_LLMCTL_REPO" LLMCTL_INSTALL_SYSTEMD=0 sh; then
    fail "rs-llmctl install failed. Install llmctl manually, set RS_LLMCTL_BIN=/path/to/llmctl, or set RS_LLMCTL_LOCAL_REPO=/path/to/rs-llmctl"
  fi
  LLMCTL_BIN="$HOME/.local/bin/llmctl"
  [ -x "$LLMCTL_BIN" ] || fail "rs-llmctl install completed but $LLMCTL_BIN is missing"
  ok "rs-llmctl installed: $LLMCTL_BIN"
}

install_rs_llmctl_from_local_repo() {
  local repo
  for repo in \
    "$RS_LLMCTL_LOCAL_REPO" \
    "${MILLIWAYS_ROOT}/../rs-llmctl" \
    "$HOME/dev/src/rs-llmctl" \
    "$HOME/src/rs-llmctl"
  do
    [ -n "$repo" ] || continue
    [ -f "$repo/Cargo.toml" ] || continue
    [ -f "$repo/install.sh" ] || continue

    info "Found local rs-llmctl repo: $repo"
    mkdir -p "$HOME/.local/bin"

    if [ -x "$repo/target/release/llmctl" ]; then
      install -m 0755 "$repo/target/release/llmctl" "$HOME/.local/bin/llmctl"
      LLMCTL_BIN="$HOME/.local/bin/llmctl"
      ok "rs-llmctl installed from local release binary"
      return 0
    fi

    local tarball
    tarball="$(find "$repo/dist" -maxdepth 1 -type f -name 'rs-llmctl-*.tar.gz' 2>/dev/null | sort | head -n 1 || true)"
    if [ -n "$tarball" ]; then
      if [ -f "$repo/dist/SHA256SUMS" ]; then
        PREFIX="$HOME/.local" LLMCTL_INSTALL_SYSTEMD=0 RS_LLMCTL_TARBALL="$tarball" RS_LLMCTL_SHA256SUMS="$repo/dist/SHA256SUMS" "$repo/install.sh"
      else
        PREFIX="$HOME/.local" LLMCTL_INSTALL_SYSTEMD=0 RS_LLMCTL_TARBALL="$tarball" "$repo/install.sh"
      fi
      LLMCTL_BIN="$HOME/.local/bin/llmctl"
      [ -x "$LLMCTL_BIN" ] || fail "local rs-llmctl install completed but $LLMCTL_BIN is missing"
      ok "rs-llmctl installed from local repo artifact"
      return 0
    fi

    if command -v cargo >/dev/null 2>&1; then
      info "Building rs-llmctl from local repo..."
      (cd "$repo" && cargo build --release --bin llmctl)
      install -m 0755 "$repo/target/release/llmctl" "$HOME/.local/bin/llmctl"
      LLMCTL_BIN="$HOME/.local/bin/llmctl"
      ok "rs-llmctl built and installed from local repo"
      return 0
    fi

    warn "local rs-llmctl repo found but has no release binary/artifact and cargo is unavailable: $repo"
  done
  return 1
}

install_linux_build_deps() {
  case "$LLAMA_CPP_ACCEL" in
    cuda)
      case "$OS" in
        Linux)
          if command -v pacman >/dev/null 2>&1; then
            sudo pacman -Sy --noconfirm base-devel cmake git curl cuda
          elif command -v apt-get >/dev/null 2>&1; then
            sudo apt-get update -qq
            sudo apt-get install -yqq build-essential cmake git curl ca-certificates nvidia-cuda-toolkit
          elif command -v dnf >/dev/null 2>&1; then
            sudo dnf install -y gcc-c++ cmake git curl ca-certificates cuda-toolkit || \
              fail "CUDA toolkit not available from enabled dnf repos. Install NVIDIA CUDA toolkit, then re-run."
          else
            fail "no supported package manager. Install CUDA toolkit + cmake manually, then re-run."
          fi
          ;;
      esac
      ;;
    hip|rocm)
      case "$OS" in
        Linux)
          if command -v pacman >/dev/null 2>&1; then
            sudo pacman -Sy --noconfirm base-devel cmake git curl rocm-hip-sdk
          elif command -v apt-get >/dev/null 2>&1; then
            if ! apt-cache show rocm-hip-sdk &>/dev/null; then
              fail "AMD ROCm repo not configured. Add it first: https://rocm.docs.amd.com/projects/install-on-linux/en/latest/tutorial/quick-start.html"
            fi
            sudo apt-get update -qq
            sudo apt-get install -yqq build-essential cmake git curl ca-certificates rocm-hip-sdk || \
              fail "ROCm/HIP packages are not available from enabled apt repos. Install AMD ROCm, then re-run."
          elif command -v dnf >/dev/null 2>&1; then
            if ! dnf repolist 2>/dev/null | grep -qi rocm; then
              fail "AMD ROCm repo not configured. Add it first: https://rocm.docs.amd.com/projects/install-on-linux/en/latest/tutorial/quick-start.html"
            fi
            sudo dnf install -y gcc-c++ cmake git curl ca-certificates rocm-hip-devel || \
              fail "ROCm/HIP packages are not available from enabled dnf repos. Install AMD ROCm, then re-run."
          else
            fail "no supported package manager. Install ROCm/HIP + cmake manually, then re-run."
          fi
          ;;
      esac
      ;;
    vulkan)
      case "$OS" in
        Linux)
          if command -v pacman >/dev/null 2>&1; then
            sudo pacman -Sy --noconfirm base-devel cmake git curl shaderc spirv-headers vulkan-headers vulkan-icd-loader
          elif command -v apt-get >/dev/null 2>&1; then
            sudo apt-get update -qq
            sudo apt-get install -yqq build-essential cmake git curl ca-certificates glslc spirv-headers libvulkan-dev
          elif command -v dnf >/dev/null 2>&1; then
            sudo dnf install -y gcc-c++ cmake git curl ca-certificates shaderc-tools spirv-headers vulkan-headers vulkan-loader-devel
          else
            fail "no supported package manager. Install Vulkan SDK/build deps manually, then re-run."
          fi
          ;;
      esac
      ;;
    *)
      case "$OS" in
        Linux)
          if command -v apt-get >/dev/null 2>&1; then
            sudo apt-get update -qq
            sudo apt-get install -yqq build-essential cmake git curl ca-certificates
          elif command -v dnf >/dev/null 2>&1; then
            sudo dnf install -y gcc-c++ cmake git curl ca-certificates
          elif command -v pacman >/dev/null 2>&1; then
            sudo pacman -Sy --noconfirm base-devel cmake git curl
          else
            fail "no supported package manager. Install llama.cpp manually from https://github.com/ggml-org/llama.cpp"
          fi
          ;;
      esac
      ;;
  esac
}

install_llamacpp() {
  local existing_missing_libs=0
  if command -v llama-server >/dev/null 2>&1; then
    local found
    found="$(command -v llama-server)"
    # Reject the smoke-mode Python stub — it's a bash script wrapping python3,
    # not a real llama.cpp binary. Check for the stub marker in the first line.
    if head -1 "$found" 2>/dev/null | grep -q "bash" && grep -q "python3" "$found" 2>/dev/null; then
      warn "Found stub llama-server at $found — replacing with real binary"
      rm -f "$found"
    elif llama_binary_has_missing_libs "$found"; then
      existing_missing_libs=1
      warn "llama-server at $found has missing shared libraries — reinstalling"
    elif [ "${MILLIWAYS_LOCAL_GPU:-0}" = "1" ]; then
      warn "llama-server already installed at $found — building a GPU-enabled launcher binary for ${LLAMA_CPP_ACCEL}"
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
      if [ "${MILLIWAYS_LOCAL_GPU:-0}" = "1" ]; then
        info "GPU install requested (${LLAMA_CPP_ACCEL}); existing llama-server binaries will be reused when present."
      fi
      # Strategy 1: already bundled in the milliways package at /usr/bin/llama-server
      # (set by build-linux-amd64.sh) — nothing to do.
      if [ "${MILLIWAYS_LOCAL_GPU:-0}" != "1" ] && [ -x /usr/bin/llama-server ]; then
        if llama_binary_has_missing_libs /usr/bin/llama-server; then
          warn "bundled /usr/bin/llama-server has missing shared libraries — reinstalling"
        else
          ok "llama-server bundled in package: /usr/bin/llama-server"
          return
        fi
      fi

      # Strategy 2: download pre-built binary from the milliways release (same tag).
      if [ "${MILLIWAYS_LOCAL_GPU:-0}" != "1" ] && [ "$existing_missing_libs" != "1" ]; then
        local milliways_ver
        milliways_ver="$(milliways --version 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
        if [ -n "$milliways_ver" ]; then
          local asset_url="https://github.com/mwigge/milliways/releases/download/${milliways_ver}/llama-server_linux_amd64"
          info "Downloading bundled llama-server from milliways release ${milliways_ver}…"
          if curl -sSfL "$asset_url" -o /tmp/llama-server-dl 2>/dev/null; then
            mkdir -p "$LLAMA_BIN_DIR"
            install -m755 /tmp/llama-server-dl "$LLAMA_BIN_DIR/llama-server"
            rm -f /tmp/llama-server-dl
            ok "llama-server installed from milliways release"
            return
          fi
        fi
      fi

      # Strategy 3: download directly from llama.cpp latest release.
      if [ "${MILLIWAYS_LOCAL_GPU:-0}" != "1" ]; then
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
            tar -xzf "/tmp/${tar_name}" -C /tmp
            mkdir -p "$LLAMA_BIN_DIR"
            install -m755 "/tmp/${entry}" "$LLAMA_BIN_DIR/llama-server"
            install_llama_shared_libs "/tmp/$(dirname "$entry")"
            rm -rf "/tmp/${tar_name}" "/tmp/$(echo "$entry" | cut -d/ -f1)"
            ok "llama-server installed from llama.cpp ${llama_tag}"
            return
          fi
        fi
      fi

      # Strategy 4: build from source (last resort).
      install_linux_build_deps
      info "Building llama.cpp from source (1–3 minutes)…"
      local tmp
      tmp="$(mktemp -d)"
      trap 'rm -rf "$tmp"' EXIT
      git clone --depth 1 --branch b5576 https://github.com/ggml-org/llama.cpp "$tmp/llama.cpp"
      local cmake_args=(-DLLAMA_CURL=OFF)
      case "$LLAMA_CPP_ACCEL" in
        cuda)
          cmake_args+=(-DGGML_CUDA=ON)
          ;;
        hip|rocm)
          cmake_args+=(-DGGML_HIP=ON)
          ;;
        vulkan)
          cmake_args+=(-DGGML_VULKAN=ON)
          ;;
        *)
          cmake_args+=(-DGGML_CUDA=OFF)
          ;;
      esac
      cmake -S "$tmp/llama.cpp" -B "$tmp/llama.cpp/build" "${cmake_args[@]}"
      cmake --build "$tmp/llama.cpp/build" --config Release -j
      mkdir -p "$LLAMA_BIN_DIR"
      install -m 0755 "$tmp/llama.cpp/build/bin/llama-server" "$LLAMA_BIN_DIR/llama-server"
      install -m 0755 "$tmp/llama.cpp/build/bin/llama-cli"    "$LLAMA_BIN_DIR/llama-cli"
      install_llama_shared_libs "$tmp/llama.cpp/build/bin"
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
  local file="${MODEL_FILE}"
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

  info "Downloading $MODEL_REPO ($MODEL_QUANT) → $dest"
  info "This is a one-time model download. Size depends on the selected model."

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
# 3. Configure rs-llmctl, write a launcher script, and install a user unit.
# ---------------------------------------------------------------------------
configure_rs_llmctl() {
  mkdir -p "$(dirname "$RS_LLMCTL_CONFIG")" "$RS_LLMCTL_DATA_DIR" "$(dirname "$RS_LLMCTL_SECRET_FILE")"
  [ -n "$MODEL_FAMILY" ] || MODEL_FAMILY="$(infer_model_family)"
  if [ ! -s "$RS_LLMCTL_CONFIG" ] || [ ! -s "$RS_LLMCTL_SECRET_FILE" ]; then
    "$LLMCTL_BIN" --config "$RS_LLMCTL_CONFIG" first-run --apply \
      --secret-output "$RS_LLMCTL_SECRET_FILE" \
      --data-dir "$RS_LLMCTL_DATA_DIR" \
      --starter-model-path "$MODEL_PATH" \
      --starter-model-alias "$MODEL_ALIAS" \
      --starter-model-family "$MODEL_FAMILY" \
      --base-url "http://${BIND_HOST}:${PORT}" >/dev/null
  else
    info "Reusing existing rs-llmctl config and API key"
  fi

  patch_rs_llmctl_config
  API_KEY="$(cat "$RS_LLMCTL_SECRET_FILE")"
  ok "rs-llmctl configured: $RS_LLMCTL_CONFIG"
}

patch_rs_llmctl_config() {
  local cfg_tmp escaped_alias escaped_path escaped_family
  cfg_tmp="$(mktemp)"
  escaped_alias="$(toml_escape "$MODEL_ALIAS")"
  escaped_path="$(toml_escape "$MODEL_PATH")"
  escaped_family="$(toml_escape "$MODEL_FAMILY")"
  awk -v port="$PORT" -v worker="$((PORT + 10000))" -v ctx="$CTX_SIZE" \
    -v alias="$escaped_alias" -v model_path="$escaped_path" -v family="$escaped_family" '
    /^\[\[models\]\]/ { skip_model = 1; next }
    skip_model && /^\[/ { skip_model = 0 }
    !skip_model {
      if ($0 ~ /^port = /) { print "port = " port; next }
      if ($0 ~ /^worker_base_port = /) { print "worker_base_port = " worker; next }
      # rs-llmctl defaults context_size to 8192 — far too small for agentic
      # coding (tool definitions + system prompt alone can eat 20-40K tokens).
      # Always apply the hardware-computed CTX_SIZE here.
      if ($0 ~ /^context_size = /) { print "context_size = " ctx; next }
      print
    }
    END {
      print ""
      print "[[models]]"
      print "alias = \"" alias "\""
      print "path = \"" model_path "\""
      print "role = \"chat\""
      print "family = \"" family "\""
      print "weight = 1"
    }
  ' "$RS_LLMCTL_CONFIG" > "$cfg_tmp"
  mv "$cfg_tmp" "$RS_LLMCTL_CONFIG"
}

write_launcher() {
  mkdir -p "$LOG_DIR" "$HOME/.local/bin"
  cat > "$HOME/.local/bin/milliways-local-server" <<EOF
#!/usr/bin/env bash
exec "$LLMCTL_BIN" --config "$RS_LLMCTL_CONFIG" server run
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
Description=milliways local model server (rs-llmctl)

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
#    answer /v1/models with the generated API key, kill it. Includes a liveness check so we don't
#    poll forever if the server died on startup.
# ---------------------------------------------------------------------------
smoke_test() {
  info "Starting rs-llmctl for a smoke test (up to 60s)…"
  "$HOME/.local/bin/milliways-local-server" >"$LOG_DIR/smoke.log" 2>&1 &
  pid=$!
  cleanup_smoke_server() {
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  }

  for i in $(seq 1 60); do
    # Liveness: bail early if the process died (no point polling for 60s)
    if ! kill -0 "$pid" 2>/dev/null; then
      warn "rs-llmctl exited during startup. Last 30 lines:"
      tail -30 "$LOG_DIR/smoke.log" >&2 || true
      return 1
    fi
    if curl -sf -H "Authorization: Bearer ${API_KEY}" "http://${BIND_HOST}:${PORT}/v1/models" >/dev/null 2>&1; then
      if [ "${MILLIWAYS_LOCAL_INSTALL_QUERY_SMOKE:-0}" = "1" ]; then
        local query_out
        query_out="$(mktemp)"
        if ! curl -sf \
          -H "Authorization: Bearer ${API_KEY}" \
          -H "Content-Type: application/json" \
          -d '{"model":"'"${MODEL_ALIAS}"'","messages":[{"role":"user","content":"reply local-smoke-ok"}],"stream":false,"max_tokens":8}' \
          "http://${BIND_HOST}:${PORT}/v1/chat/completions" >"$query_out"; then
          rm -f "$query_out"
          cleanup_smoke_server
          return 1
        fi
        if ! grep -q "local-smoke-ok" "$query_out"; then
          warn "local chat smoke response did not contain expected marker"
          cat "$query_out" >&2 || true
          rm -f "$query_out"
          cleanup_smoke_server
          return 1
        fi
        rm -f "$query_out"
      fi
      ok "rs-llmctl responding on http://${BIND_HOST}:${PORT}/v1"
      cleanup_smoke_server
      return 0
    fi
    if [ $((i % 10)) -eq 0 ]; then
      info "still waiting on rs-llmctl (${i}s)…"
    fi
    sleep 1
  done

  warn "smoke test timed out — see $LOG_DIR/smoke.log"
  cleanup_smoke_server
  return 1
}

write_local_env() {
  local endpoint="http://${BIND_HOST}:${PORT}/v1"
  local env_file="${XDG_CONFIG_HOME:-$HOME/.config}/milliways/local.env"
  mkdir -p "$(dirname "$env_file")"

  local tmp
  tmp="$(mktemp "$(dirname "$env_file")/.local.env.XXXXXX")"
  chmod 0600 "$tmp" 2>/dev/null || true
  awk -F= '$1 != "MILLIWAYS_LOCAL_ENDPOINT" && $1 != "MILLIWAYS_LOCAL_MODEL" && $1 != "MILLIWAYS_LOCAL_API_KEY"' "$env_file" 2>/dev/null > "$tmp" || true
  printf 'MILLIWAYS_LOCAL_ENDPOINT=%s\n' "$endpoint" >> "$tmp"
  printf 'MILLIWAYS_LOCAL_MODEL=%s\n' "$MODEL_ALIAS" >> "$tmp"
  printf 'MILLIWAYS_LOCAL_API_KEY=%s\n' "$API_KEY" >> "$tmp"
  mv "$tmp" "$env_file"
  chmod 0600 "$env_file" 2>/dev/null || true
}

smoke_mode() {
  info "milliways local-model installer smoke mode"

  # Use an isolated temp dir — never write the stub to ~/.local/bin where it
  # would persist after the smoke and fool future installs.
  local smoke_tmp
  smoke_tmp="$(mktemp -d)"
  trap 'rm -rf "$smoke_tmp"' EXIT

  mkdir -p "$LOG_DIR" "$MODEL_DIR"
  MODEL_PATH="$MODEL_DIR/smoke-model.gguf"
  : > "$MODEL_PATH"

  cat > "$smoke_tmp/llmctl" <<'EOF'
#!/usr/bin/env bash
config=""
if [ "${1:-}" = "--config" ]; then
  config="$2"
  shift 2
fi
if [ "${1:-}" = "--version" ]; then
  echo "llmctl smoke"
  exit 0
fi
if [ "${1:-}" = "first-run" ]; then
  secret=""
  base_url="http://127.0.0.1:8765"
  alias="smoke-local"
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --secret-output) secret="$2"; shift 2 ;;
      --base-url) base_url="$2"; shift 2 ;;
      --starter-model-alias) alias="$2"; shift 2 ;;
      *) shift ;;
    esac
  done
  port="${base_url##*:}"
  mkdir -p "$(dirname "$config")" "$(dirname "$secret")"
  printf 'port = %s\nworker_base_port = %s\nmodel = "%s"\n' "$port" "$((port + 10000))" "$alias" >"$config"
  printf 'smoke-local-key\n' >"$secret"
  exit 0
fi
if [ "${1:-}" = "server" ] && [ "${2:-}" = "check" ]; then
  exit 0
fi
if [ "${1:-}" = "server" ] && [ "${2:-}" = "run" ]; then
  port="$(sed -n 's/^port = //p' "$config" | head -1)"
  model="$(sed -n 's/^model = "\(.*\)"/\1/p' "$config" | head -1)"
  exec python3 - "$port" "$model" <<'PY'
import json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer

port = int(sys.argv[1])
model = sys.argv[2] or "smoke-local"

class Handler(BaseHTTPRequestHandler):
    def authorized(self):
        return self.headers.get("Authorization") == "Bearer smoke-local-key"

    def do_GET(self):
        if self.path == "/v1/models" and self.authorized():
            data = json.dumps({"data": [{"id": model}]}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            return
        self.send_response(401 if self.path == "/v1/models" else 404); self.end_headers()

    def do_POST(self):
        if self.path == "/v1/chat/completions" and self.authorized():
            length = int(self.headers.get("Content-Length", "0") or "0")
            if length:
                self.rfile.read(length)
            data = json.dumps({
                "id": "smoke-chat",
                "object": "chat.completion",
                "model": model,
                "choices": [{"index": 0, "message": {"role": "assistant", "content": "local-smoke-ok"}, "finish_reason": "stop"}],
                "usage": {"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5},
            }).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            return
        self.send_response(401 if self.path == "/v1/chat/completions" else 404); self.end_headers()

    def log_message(self, *_): return

HTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY
fi
exit 2
EOF
  chmod +x "$smoke_tmp/llmctl"

  # Prepend the temp dir so install/configure/write_launcher/smoke_test use
  # the rs-llmctl stub, but the stub never touches ~/.local/bin.
  PATH="$smoke_tmp:$PATH"
  mkdir -p "$HOME/.local/bin"
  install_rs_llmctl
  configure_rs_llmctl
  write_launcher
  write_local_env
  MILLIWAYS_LOCAL_INSTALL_QUERY_SMOKE=1
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
  if [ "${MILLIWAYS_LOCAL_GPU:-0}" = "1" ]; then
    info "GPU:        ${MILLIWAYS_GPU_NAME:-detected GPU} (${MILLIWAYS_GPU_VENDOR:-unknown}, ${MILLIWAYS_GPU_VRAM_GB:-?}GB VRAM)"
    info "Accel:      $LLAMA_CPP_ACCEL, n-gpu-layers=$N_GPU_LAYERS"
  fi
  echo

  reuse_existing_or_pick_port
  install_rs_llmctl
  fetch_model
  configure_rs_llmctl
  write_launcher
  if ! smoke_test; then
    if [ "${MILLIWAYS_LOCAL_INSTALL_ALLOW_SMOKE_FAIL:-0}" = "1" ]; then
      warn "Smoke test did not pass — continuing because MILLIWAYS_LOCAL_INSTALL_ALLOW_SMOKE_FAIL=1"
    else
      fail "Smoke test did not pass; refusing to report installation success. Set MILLIWAYS_LOCAL_INSTALL_ALLOW_SMOKE_FAIL=1 to bypass."
    fi
  fi

  echo
  ok "All set."
  info "To start the server in the foreground:"
  info "  milliways-local-server"
  info "To use it from milliways:"
  info "  /local"
  info "  hello, can you write a fizzbuzz in Go?"
  info ""
  local env_file="${XDG_CONFIG_HOME:-$HOME/.config}/milliways/local.env"
  write_local_env
  ok "Endpoint written to $env_file — milliways will pick it up automatically."

  if [ "$PORT" != "8765" ]; then
    info "Note: port 8765 was in use, using $PORT instead."
  fi
}

main "$@"
