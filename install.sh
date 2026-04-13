#!/bin/sh
set -e

# Milliways installer — downloads the latest release binary.
# Usage: curl -fsSL https://raw.githubusercontent.com/mwigge/milliways/master/install.sh | sh

REPO="mwigge/milliways"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)       echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
    darwin|linux) ;;
    *)            echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Check for Go (build from source if no release available)
if ! command -v go >/dev/null 2>&1; then
    echo "Go not found. Install Go first: https://go.dev/dl/"
    echo "Then run: go install github.com/$REPO/cmd/milliways@latest"
    exit 1
fi

echo "Building milliways from source..."
GOBIN="$INSTALL_DIR" go install "github.com/$REPO/cmd/milliways@latest"

# Verify installation
if [ -x "$INSTALL_DIR/milliways" ]; then
    echo ""
    echo "Milliways installed to $INSTALL_DIR/milliways"
    echo ""
    "$INSTALL_DIR/milliways" --version
    echo ""
    echo "Make sure $INSTALL_DIR is in your PATH:"
    echo "  export PATH=\"\$PATH:$INSTALL_DIR\""
    echo ""
    echo "Check available kitchens:"
    echo "  milliways status"
else
    echo "Installation failed."
    exit 1
fi
