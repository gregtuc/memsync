#!/bin/sh
# memsync installer (template).
# Usage:  curl -fsSL https://memsync.dev/install.sh | sh
#         curl -fsSL https://memsync.dev/install.sh | sh -s 1.2.0   # pin a version
#
# Downloads a prebuilt binary from GitHub Releases and verifies its checksum
# before installing. Until the first tagged release exists, build from source:
#   git clone https://github.com/gregtuc/memsync && cd memsync && go build -o memsync .
set -eu

REPO="gregtuc/memsync"
VERSION="${1:-latest}"
BINDIR="${MEMSYNC_BINDIR:-$HOME/.local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep -o '"tag_name": *"[^"]*"' | cut -d'"' -f4)"
fi
if [ -z "${VERSION:-}" ]; then
  echo "No released version found yet. Build from source:" >&2
  echo "  git clone https://github.com/$REPO && cd memsync && go build -o memsync ." >&2
  exit 1
fi

tarball="memsync_${VERSION}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$VERSION"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $tarball ..."
curl -fsSL "$base/$tarball" -o "$tmp/$tarball"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

echo "Verifying checksum ..."
( cd "$tmp" && grep " $tarball\$" checksums.txt | shasum -a 256 -c - )

tar -xzf "$tmp/$tarball" -C "$tmp"
mkdir -p "$BINDIR"
install -m 0755 "$tmp/memsync" "$BINDIR/memsync"

echo "Installed memsync to $BINDIR/memsync"
echo "Run:  memsync init"
case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *) echo "Note: add $BINDIR to your PATH." ;;
esac
