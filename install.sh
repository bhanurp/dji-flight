#!/usr/bin/env bash
# dji-flight installer
# Usage: curl -sSL https://raw.githubusercontent.com/bhanureddy/dji-flight/main/install.sh | bash
#        ./install.sh v0.2.0   (pin a version)

set -e

REPO="bhanureddy/dji-flight"
VERSION="${1:-latest}"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64)        ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

if [ "$VERSION" = "latest" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | sed 's/.*"tag_name": "\(.*\)".*/\1/')
fi

BINARY="dji-flight-${OS}-${ARCH}"
[ "$OS" = "windows" ] && BINARY="${BINARY}.exe"

URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}"
DEST="/usr/local/bin/dji-flight"

echo "Installing dji-flight ${VERSION} (${OS}/${ARCH})..."
curl -fsSL "$URL" -o /tmp/dji-flight-install
chmod +x /tmp/dji-flight-install
sudo mv /tmp/dji-flight-install "$DEST"

echo "Installed: $($DEST version)"
