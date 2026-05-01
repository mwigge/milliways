#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
repo_root="$(CDPATH= cd -- "$script_dir/.." && pwd)"

image="${MILLIWAYS_BUILD_LINUX_IMAGE:-milliways-build-linux:bookworm}"
version="${VERSION:-$(git -C "$repo_root" describe --tags --always --dirty 2>/dev/null || echo dev)}"
out_dir="${OUT_DIR:-$repo_root/dist}"

docker build \
  -t "$image" \
  -f "$repo_root/local/docker/build-linux/Dockerfile" \
  "$repo_root/local/docker/build-linux"

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
    mkdir -p dist /tmp/mw-gocache /tmp/mw-gomodcache
    export PATH=/usr/local/go/bin:$PATH
    export CGO_ENABLED=1
    export GOOS=linux
    export GOARCH=amd64
    case "$(uname -m)" in
      x86_64|amd64) export CC=gcc ;;
      *) export CC=x86_64-linux-gnu-gcc ;;
    esac
    export GOTOOLCHAIN=auto
    export GOSUMDB=sum.golang.org
    export GOCACHE=/tmp/mw-gocache
    export GOMODCACHE=/tmp/mw-gomodcache
    for bin in milliways milliwaysd milliwaysctl; do
      echo "building ${bin}_linux_amd64 (${VERSION})"
      go build -ldflags "-X main.version=${VERSION}" -o "dist/${bin}_linux_amd64" "./cmd/${bin}"
      file "dist/${bin}_linux_amd64"
    done
  '

printf 'Built Linux amd64 artifacts in %s\n' "$out_dir"
