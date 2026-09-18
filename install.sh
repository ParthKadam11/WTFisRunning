#!/usr/bin/env bash
# Install wtf to /usr/local/bin (or ~/.local/bin).
#   curl -fsSL https://github.com/ParthKadam11/WTFisRunning/releases/latest/download/i | sh
set -euo pipefail

REPO="ParthKadam11/WTFisRunning"
BIN="wtf"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *)
    echo "unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

case "$os" in
  linux|darwin) ;;
  mingw*|msys*|cygwin*)
    echo "on Windows, download the .zip from:" >&2
    echo "  https://github.com/${REPO}/releases/latest" >&2
    exit 1
    ;;
  *)
    echo "unsupported OS: $os" >&2
    exit 1
    ;;
esac

asset="${BIN}_${os}_${arch}.tar.gz"
url="https://github.com/${REPO}/releases/latest/download/${asset}"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

echo "downloading ${asset}…"
curl -fsSL "$url" -o "$tmpdir/$asset"
tar -xzf "$tmpdir/$asset" -C "$tmpdir"

dest="/usr/local/bin"
if [[ ! -w "$dest" ]]; then
  dest="${HOME}/.local/bin"
  mkdir -p "$dest"
fi

install -m 755 "$tmpdir/$BIN" "$dest/$BIN"
echo "installed: $dest/$BIN"
echo
echo "try:  wtf"
echo "  or: wtf user@host"
