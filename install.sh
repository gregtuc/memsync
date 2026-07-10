#!/bin/sh
# Install a released memsync binary on macOS or Linux.
#
# Latest release:
#   curl -fsSL https://raw.githubusercontent.com/gregtuc/memsync/main/install.sh | sh
# Pinned release:
#   curl -fsSL https://raw.githubusercontent.com/gregtuc/memsync/main/install.sh | sh -s -- v0.1.1
set -eu

REPO="gregtuc/memsync"
REQUESTED_VERSION="${1:-latest}"
BINDIR="${MEMSYNC_BINDIR:-$HOME/.local/bin}"

for command in curl tar awk sed install git mktemp mv; do
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

if ! git --version >/dev/null 2>&1; then
  echo "error: Git is installed but is not ready" >&2
  if [ "$os" = "darwin" ]; then
    echo "Run: xcode-select --install" >&2
  else
    echo "Install Git with your system package manager, then retry." >&2
  fi
  exit 1
fi

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
staged_bin=""
trap 'rm -rf "$tmp"; [ -z "${staged_bin:-}" ] || rm -f "$staged_bin"' EXIT HUP INT TERM

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
staged_bin="$(mktemp "$BINDIR/.memsync.install.XXXXXX")"
install -m 0755 "$tmp/memsync" "$staged_bin"
mv -f "$staged_bin" "$BINDIR/memsync"
staged_bin=""

echo "Installed $BINDIR/memsync"
"$BINDIR/memsync" --version

case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *)
    shell_path="${SHELL:-/bin/sh}"
    shell_name="${shell_path##*/}"
    path_configured=0
    path_style="posix"
    case "$shell_name" in
      zsh)
        zsh_startup_dir="${ZDOTDIR:-}"
        if [ -z "$zsh_startup_dir" ] && [ -x "$shell_path" ]; then
          zsh_startup_dir="$(HOME="$HOME" "$shell_path" -c \
            'print -r -- "${ZDOTDIR:-$HOME}"' 2>/dev/null | sed -n '$p')"
        fi
        if [ -n "$zsh_startup_dir" ]; then
          profile="$zsh_startup_dir/.zshrc"
        else
          profile=""
        fi
        ;;
      bash)
        if [ "$os" = "darwin" ]; then
          if [ -e "$HOME/.bash_profile" ]; then
            profile="$HOME/.bash_profile"
          elif [ -e "$HOME/.bash_login" ]; then
            profile="$HOME/.bash_login"
          elif [ -e "$HOME/.profile" ]; then
            profile="$HOME/.profile"
          else
            profile="$HOME/.bash_profile"
          fi
        else
          profile="$HOME/.bashrc"
        fi
        ;;
      fish)
        if ! MEMSYNC_INSTALL_PATH="$BINDIR" "$shell_path" -c \
          'fish_add_path -U "$MEMSYNC_INSTALL_PATH"'; then
          echo "error: could not add $BINDIR to the fish PATH" >&2
          exit 1
        fi
        profile=""
        path_configured=1
        ;;
      csh|tcsh)
        profile="$HOME/.cshrc"
        path_style="csh"
        ;;
      sh|dash|ksh)
        profile="$HOME/.profile"
        ;;
      *)
        profile=""
        ;;
    esac

    if [ -n "$profile" ]; then
      if [ "$path_style" = "csh" ]; then
        if [ "$BINDIR" = "$HOME/.local/bin" ]; then
          path_line='setenv PATH "$HOME/.local/bin":$PATH # added by memsync'
        else
          profile=""
        fi
      else
        case "$BINDIR" in
          "$HOME/.local/bin")
            path_line='export PATH="$HOME/.local/bin:$PATH" # added by memsync'
            ;;
          *)
            escaped_bindir="$(printf '%s' "$BINDIR" | sed "s/'/'\\\\''/g")"
            path_line="export PATH='$escaped_bindir':\$PATH # added by memsync"
            ;;
        esac
      fi
    fi

    if [ -n "$profile" ]; then
      path_present=0
      if [ -f "$profile" ]; then
        while IFS= read -r line || [ -n "$line" ]; do
          if [ "$line" = "$path_line" ]; then
            path_present=1
            break
          fi
        done < "$profile"
      fi
      if [ "$path_present" -eq 0 ]; then
        profile_dir="${profile%/*}"
        if ! mkdir -p "$profile_dir"; then
          echo "error: could not create the shell config directory $profile_dir" >&2
          exit 1
        fi
        if [ -s "$profile" ] && ! printf '\n' >> "$profile"; then
          echo "error: could not update PATH in $profile" >&2
          exit 1
        fi
        if ! printf '%s\n' "$path_line" >> "$profile"; then
          echo "error: could not add $BINDIR to PATH in $profile" >&2
          exit 1
        fi
      fi
      path_configured=1
    fi
    PATH="$BINDIR:$PATH"
    export PATH
    if [ "$path_configured" -eq 1 ]; then
      echo "memsync is available in new terminals."
    else
      echo "memsync is installed at $BINDIR/memsync."
      echo "Add $BINDIR to PATH to use it by name in $shell_name."
    fi
    ;;
esac

if [ "${MEMSYNC_SKIP_INIT:-0}" != "1" ]; then
  if ! "$BINDIR/memsync" init; then
    echo "error: memsync was installed, but automatic setup needs attention" >&2
    echo "Retry with: $BINDIR/memsync init" >&2
    exit 1
  fi
fi
