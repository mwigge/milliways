#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repo_root="$(CDPATH= cd -- "$script_dir/.." && pwd)"

image="${MILLIWAYS_BUILD_LINUX_IMAGE:-milliways-build-linux:bookworm}"
version="${VERSION:-$(git -C "$repo_root" describe --tags --always --dirty 2>/dev/null || echo dev)}"
out_dir="${OUT_DIR:-$repo_root/dist}"
go_version="${GO_VERSION:-$(awk '/^go / { print $2; exit }' "$repo_root/go.mod")}"

docker build \
  -t "$image" \
  --build-arg "GO_VERSION=$go_version" \
  -f "$repo_root/build/docker/build-linux/Dockerfile" \
  "$repo_root/build/docker/build-linux"

mkdir -p "$out_dir"

mounts=(
  -v "$repo_root:/src/milliways"
)

for sibling in milliways-wezterm agent-toolkit-bundle; do
  sibling_dir="$(CDPATH= cd -- "$repo_root/.." && pwd)/$sibling"
  if [ -d "$sibling_dir" ]; then
    mounts+=(-v "$sibling_dir:/src/$sibling:ro")
  fi
done

docker run --rm \
  --user "$(id -u):$(id -g)" \
  "${mounts[@]}" \
  -e "VERSION=$version" \
  "$image" \
  bash -lc '
    set -euo pipefail
    cd /src/milliways
    mkdir -p dist /tmp/mw-gocache /tmp/mw-gomodcache /tmp/mw-pkg
    export PATH=/usr/local/go/bin:$PATH
    export CGO_ENABLED=1
    export GOOS=linux
    export GOARCH=amd64
    case "$(uname -m)" in
      x86_64|amd64) export CC=gcc ;;
      *) export CC=x86_64-linux-gnu-gcc ;;
    esac
    export GOTOOLCHAIN=local
    export GOSUMDB=sum.golang.org
    export GOCACHE=/tmp/mw-gocache
    export GOMODCACHE=/tmp/mw-gomodcache

    # ── 1. Compile milliways binaries ────────────────────────────────────────
    for bin in milliways milliwaysd milliwaysctl; do
      echo "building ${bin}_linux_amd64 (${VERSION})"
      go build -ldflags "-X main.version=${VERSION}" -o "dist/${bin}_linux_amd64" "./cmd/${bin}"
      file "dist/${bin}_linux_amd64"
    done

    # ── 1b. Fetch llama-server (plain CPU build — works on every amd64 host) ─
    # LLAMA_TAG can override the release tag. Missing llama-server is non-fatal:
    # the local-server installer can still fetch or install it later.
    llama_tag="${LLAMA_TAG:-}"
    if [ -z "$llama_tag" ]; then
      if curl -sSf https://api.github.com/repos/ggml-org/llama.cpp/releases/latest -o /tmp/llama-release.json; then
        llama_tag="$(sed -n "s/.*\"tag_name\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" /tmp/llama-release.json | head -1)"
      else
        echo "WARNING: could not resolve latest llama.cpp release — skipping bundle"
      fi
    fi
    if [ -n "$llama_tag" ]; then
      llama_tar="llama-${llama_tag}-bin-ubuntu-x64.tar.gz"
      llama_url="https://github.com/ggml-org/llama.cpp/releases/download/${llama_tag}/${llama_tar}"
      echo "fetching llama-server ${llama_tag} from ${llama_url}"
      if curl -sSfL "${llama_url}" -o "/tmp/${llama_tar}"; then
        # List the tarball first, find the llama-server entry, then extract it.
        llama_entry="$(tar -tzf "/tmp/${llama_tar}" | grep "/llama-server$" | head -1 || true)"
        if [ -n "$llama_entry" ]; then
          tar -xzf "/tmp/${llama_tar}" -C /tmp "$llama_entry"
          cp "/tmp/${llama_entry}" dist/llama-server_linux_amd64
          chmod +x dist/llama-server_linux_amd64
          rm -f "/tmp/${llama_tar}" "/tmp/${llama_entry%/*}" 2>/dev/null || true
          echo "llama-server bundled: $(file dist/llama-server_linux_amd64)"
        else
          echo "WARNING: llama-server not found in ${llama_tar} — skipping bundle"
        fi
      else
        echo "WARNING: could not fetch llama-server — skipping bundle (users can run /install-local-server)"
      fi
    fi

    # ── 2. Stage the package tree ────────────────────────────────────────────
    # Binaries go to /usr/bin (system-wide, always on PATH).
    # Support scripts go to /usr/share/milliways (found by milliwaysctl local).
    pkg_root=/tmp/mw-pkg/root
    rm -rf "$pkg_root"
    install -Dm755 dist/milliways_linux_amd64       "$pkg_root/usr/bin/milliways"
    install -Dm755 dist/milliwaysd_linux_amd64      "$pkg_root/usr/bin/milliwaysd"
    install -Dm755 dist/milliwaysctl_linux_amd64    "$pkg_root/usr/bin/milliwaysctl"
    # Bundle llama-server when available — removes the need for brew/cmake on first use.
    [ -f dist/llama-server_linux_amd64 ] && \
      install -Dm755 dist/llama-server_linux_amd64 "$pkg_root/usr/bin/llama-server"
    install -Dm755 scripts/install_local.sh         "$pkg_root/usr/share/milliways/scripts/install_local.sh"
    install -Dm755 scripts/install_local_swap.sh    "$pkg_root/usr/share/milliways/scripts/install_local_swap.sh"
    install -Dm755 scripts/install_feature_deps.sh  "$pkg_root/usr/share/milliways/scripts/install_feature_deps.sh"
    install -Dm644 cmd/milliwaysctl/milliways.lua   "$pkg_root/usr/share/milliways/wezterm.lua"

    # Agent toolkit bundle (if the sibling directory was mounted by the caller)
    if [ -d /src/agent-toolkit-bundle ] && [ -f /src/agent-toolkit-bundle/skill-rules.json ]; then
      mkdir -p "$pkg_root/usr/share/milliways/agent-toolkit"
      cp -r /src/agent-toolkit-bundle/. "$pkg_root/usr/share/milliways/agent-toolkit/"
    fi

    # Post-install script: runs after the package is placed on disk.
    # Uses printf to avoid heredoc quoting issues inside single-quoted docker exec.
    printf "%s\n" \
      "#!/bin/sh" \
      "SHARE_DIR=/usr/share/milliways MILLIWAYS_WRITE_LOCAL_ENV=0 MILLIWAYS_INSTALL_SYSTEM_DEPS=0 /usr/share/milliways/scripts/install_feature_deps.sh \\" \
      "  || echo \"warning: Milliways feature dependency install failed; run /usr/share/milliways/scripts/install_feature_deps.sh\"" \
      > /tmp/mw-pkg/postinstall.sh
    chmod +x /tmp/mw-pkg/postinstall.sh

    # Normalise version for package managers: strip leading "v" and any dirty
    # suffix. RPM/DEB versions must be purely numeric + dots.
    pkg_ver="${VERSION#v}"
    pkg_ver="${pkg_ver%%-dirty}"
    # Replace any remaining non-numeric/dot chars (e.g. git hash suffix) with ~
    pkg_ver="$(echo "$pkg_ver" | sed "s/-/~/g")"
    case "$pkg_ver" in
      [0-9]*) ;;
      *) pkg_ver="0.0.0~${pkg_ver}" ;;
    esac

    # fpm requires all flags before the positional input argument.
    # fpm_meta holds the common metadata flags; each package type appends its
    # own flags then the input-type / chdir / source at the end.
    fpm_meta=(
      --name         milliways
      --version      "$pkg_ver"
      --architecture amd64
      --maintainer   "milliways authors <noreply@github.com>"
      --description  "AI terminal — routes prompts to claude, codex, gemini, copilot, and more"
      --url          "https://github.com/mwigge/milliways"
      --license      "Apache-2.0"
      --category     utils
    )
    # Input spec — must come last on every fpm invocation.
    fpm_input=(--input-type dir --chdir "$pkg_root" .)

    # ── 3. Build .deb (Debian / Ubuntu) ─────────────────────────────────────
    echo "packaging milliways_${pkg_ver}_amd64.deb"
    fpm "${fpm_meta[@]}" \
      --output-type deb \
      --depends ca-certificates \
      --depends git \
      --depends nodejs \
      --depends npm \
      --depends python3 \
      --depends python3-pip \
      --depends python3-venv \
      --after-install /tmp/mw-pkg/postinstall.sh \
      --package "dist/milliways_${pkg_ver}_amd64.deb" \
      "${fpm_input[@]}"

    # ── 4. Build .rpm (Fedora / RHEL / openSUSE) ────────────────────────────
    echo "packaging milliways-${pkg_ver}-1.x86_64.rpm"
    fpm "${fpm_meta[@]}" \
      --output-type rpm \
      --depends ca-certificates \
      --depends git \
      --depends nodejs \
      --depends npm \
      --depends python3 \
      --depends python3-pip \
      --after-install /tmp/mw-pkg/postinstall.sh \
      --rpm-summary "AI terminal" \
      --package "dist/milliways-${pkg_ver}-1.x86_64.rpm" \
      "${fpm_input[@]}"

    # ── 5. Build .pkg.tar.zst (Arch Linux) ──────────────────────────────────
    # fpm -t pacman produces a .pkg.tar.gz; repack as .zst so pacman -U
    # accepts it on modern Arch without extra flags.
    echo "packaging milliways-${pkg_ver}-1-x86_64.pkg.tar.zst"
    fpm "${fpm_meta[@]}" \
      --output-type pacman \
      --depends ca-certificates \
      --depends git \
      --depends nodejs \
      --depends npm \
      --depends python \
      --depends python-pip \
      --after-install /tmp/mw-pkg/postinstall.sh \
      --pacman-compression gz \
      --package "/tmp/mw-pkg/milliways-${pkg_ver}-1-x86_64.pkg.tar.gz" \
      "${fpm_input[@]}"
    # Repack gz → zst (Arch default since 2020)
    cd /tmp/mw-pkg
    gunzip -c "milliways-${pkg_ver}-1-x86_64.pkg.tar.gz" \
      | zstd -q -o "/src/milliways/dist/milliways-${pkg_ver}-1-x86_64.pkg.tar.zst"
    cd /src/milliways

    echo ""
    echo "Packages built:"
    ls -lh dist/milliways*.deb dist/milliways*.rpm dist/milliways*.zst 2>/dev/null || true
  '

printf 'Built Linux amd64 artifacts in %s\n' "$out_dir"
