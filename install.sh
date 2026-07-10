#!/bin/sh
# Install a released memsync binary on macOS or Linux.
#
# Latest release:
#   curl -fsSL https://raw.githubusercontent.com/gregtuc/memsync/main/install.sh | sh
# Pinned release:
#   curl -fsSL https://raw.githubusercontent.com/gregtuc/memsync/main/install.sh | sh -s -- v0.1.0
set -eu

REPO="gregtuc/memsync"
REQUESTED_VERSION="${1:-latest}"
BINDIR="${MEMSYNC_BINDIR:-$HOME/.local/bin}"

for command in curl tar awk sed install; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "error: required command not found: $command" >&2
    exit 1
  fi
done

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *)
    echo "error: memsync releases support macOS and Linux (including WSL)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "error: unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

if [ "$REQUESTED_VERSION" = "latest" ]; then
  if ! release_json="$(curl -fsSL --retry 3 --connect-timeout 10 \
    "https://api.github.com/repos/$REPO/releases/latest")"; then
    echo "error: no published memsync release was found" >&2
    echo "Build from source with: go install github.com/$REPO@latest" >&2
    exit 1
  fi
  tag="$(printf '%s\n' "$release_json" | \
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | \
    awk 'NR == 1 { print; exit }')"
  if [ -z "$tag" ]; then
    echo "error: no published memsync release was found" >&2
    echo "Build from source with: go install github.com/$REPO@latest" >&2
    exit 1
  fi
else
  case "$REQUESTED_VERSION" in
    v*) tag="$REQUESTED_VERSION" ;;
    *) tag="v$REQUESTED_VERSION" ;;
  esac
fi

version="${tag#v}"
case "$version" in
  ""|*[!0-9A-Za-z.-]*)
    echo "error: invalid release version: $tag" >&2
    exit 1
    ;;
esac

archive="memsync_${version}_${os}_${arch}.tar.gz"
base_url="https://github.com/$REPO/releases/download/$tag"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

echo "Downloading memsync $version for $os/$arch..."
curl -fsSL --retry 3 --connect-timeout 10 \
  "$base_url/$archive" -o "$tmp/$archive"
curl -fsSL --retry 3 --connect-timeout 10 \
  "$base_url/checksums.txt" -o "$tmp/checksums.txt"

expected="$(awk -v file="$archive" '$2 == file { print $1; exit }' "$tmp/checksums.txt")"
if [ -z "$expected" ]; then
  echo "error: $archive is missing from checksums.txt" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$archive" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$tmp/$archive" | awk '{ print $1 }')"
else
  echo "error: sha256sum or shasum is required to verify the download" >&2
  exit 1
fi

if [ "$actual" != "$expected" ]; then
  echo "error: checksum verification failed for $archive" >&2
  exit 1
fi
echo "Checksum verified."

tar -xzf "$tmp/$archive" -C "$tmp" memsync
mkdir -p "$BINDIR"
install -m 0755 "$tmp/memsync" "$BINDIR/memsync"

echo "Installed $BINDIR/memsync"
"$BINDIR/memsync" --version

case ":$PATH:" in
  *":$BINDIR:"*) echo "Next: memsync init" ;;
  *)
    echo "Next: $BINDIR/memsync init"
    echo "Tip: add $BINDIR to PATH to run memsync from anywhere."
    ;;
esac
