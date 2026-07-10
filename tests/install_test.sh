#!/bin/sh
set -eu

root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
work="$(mktemp -d "${TMPDIR:-/tmp}/memsync-install-test.XXXXXX")"
trap 'rm -rf "$work"' EXIT HUP INT TERM

fixture="$work/fixture"
fakebin="$work/fake-bin"
mkdir -p "$fixture" "$fakebin"

printf '%s\n' \
  '#!/bin/sh' \
  'case "${1:-}" in' \
  '  --version) printf "%s\n" "memsync 9.9.9" ;;' \
  '  init) printf "%s\n" init >> "$MEMSYNC_INSTALL_FIXTURE/init-calls" ;;' \
  'esac' > "$fixture/memsync"
chmod 0755 "$fixture/memsync"

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *) echo "unsupported test OS" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported test architecture" >&2; exit 1 ;;
esac

archive="memsync_9.9.9_${os}_${arch}.tar.gz"
tar -czf "$fixture/$archive" -C "$fixture" memsync
if command -v sha256sum >/dev/null 2>&1; then
  checksum="$(sha256sum "$fixture/$archive" | awk '{ print $1 }')"
else
  checksum="$(shasum -a 256 "$fixture/$archive" | awk '{ print $1 }')"
fi
printf '%s  %s\n' "$checksum" "$archive" > "$fixture/checksums.txt"

printf '%s\n' \
  '#!/bin/sh' \
  'output=""' \
  'url=""' \
  'while [ "$#" -gt 0 ]; do' \
  '  case "$1" in' \
  '    -o) output="$2"; shift 2 ;;' \
  '    --retry|--connect-timeout) shift 2 ;;' \
  '    -*) shift ;;' \
  '    *) url="$1"; shift ;;' \
  '  esac' \
  'done' \
  'case "$url" in' \
  '  */releases/latest) printf "%s\n" '\''{"tag_name":"v9.9.9"}'\'' ;;' \
  '  */checksums.txt) cp "$MEMSYNC_INSTALL_FIXTURE/checksums.txt" "$output" ;;' \
  '  *.tar.gz) cp "$MEMSYNC_INSTALL_FIXTURE/'"$archive"'" "$output" ;;' \
  '  *) echo "unexpected curl URL: $url" >&2; exit 1 ;;' \
  'esac' > "$fakebin/curl"
chmod 0755 "$fakebin/curl"

printf '%s\n' \
  '#!/bin/sh' \
  'printf "%s\n" "git version 2.0.0"' > "$fakebin/git"
chmod 0755 "$fakebin/git"

base_path="/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
run_install() {
  test_home="$1"
  test_shell="$2"
  output="$3"
  HOME="$test_home" \
    SHELL="$test_shell" \
    ZDOTDIR="$test_home" \
    PATH="$fakebin:$base_path" \
    MEMSYNC_INSTALL_FIXTURE="$fixture" \
    sh "$root/install.sh" > "$output"
}

# A fresh POSIX shell gets a usable command, paths with spaces remain safe, and
# running the installer again does not append another PATH entry.
home="$work/home with spaces"
mkdir -p "$home"
printf '%s' 'umask 077' > "$home/.profile"
run_install "$home" /bin/sh "$work/first.out"
run_install "$home" /bin/sh "$work/second.out"
test "$(awk '/# added by memsync$/ { count++ } END { print count+0 }' "$home/.profile")" -eq 1
found="$(HOME="$home" PATH="$fakebin:$base_path" /bin/sh -c '. "$HOME/.profile"; command -v memsync')"
test "$found" = "$home/.local/bin/memsync"
test -z "$(find "$home/.local/bin" -name '.memsync.install.*' -print)"
! grep -q 'add .* to PATH' "$work/first.out"

# zsh uses its actual startup directory and remains idempotent.
zhome="$work/zsh-home"
mkdir -p "$zhome"
run_install "$zhome" /bin/zsh "$work/zsh-first.out"
run_install "$zhome" /bin/zsh "$work/zsh-second.out"
test "$(awk '/# added by memsync$/ { count++ } END { print count+0 }' "$zhome/.zshrc")" -eq 1

# zsh can set an unexported ZDOTDIR in .zshenv; ask zsh where it really reads
# .zshrc instead of writing a file the user's shell will ignore.
expected_init_calls=9
if real_zsh="$(command -v zsh 2>/dev/null)" && [ -n "$real_zsh" ]; then
  hidden_zdot_home="$work/hidden-zdot-home"
  mkdir -p "$hidden_zdot_home"
  printf '%s\n' 'ZDOTDIR="$HOME/.config/zsh"' > "$hidden_zdot_home/.zshenv"
  (
    unset ZDOTDIR
    HOME="$hidden_zdot_home" \
      SHELL="$real_zsh" \
      PATH="$fakebin:$base_path" \
      MEMSYNC_INSTALL_FIXTURE="$fixture" \
      sh "$root/install.sh" > "$work/hidden-zdot.out"
  )
  test -f "$hidden_zdot_home/.config/zsh/.zshrc"
  test ! -e "$hidden_zdot_home/.zshrc"
  expected_init_calls=10
fi

# Bash uses the startup file appropriate to each supported operating system.
bash_home="$work/bash-home"
mkdir -p "$bash_home"
if [ "$os" = "darwin" ]; then
  printf '%s\n' 'export EXISTING_PROFILE_VALUE=preserved' > "$bash_home/.profile"
fi
run_install "$bash_home" /bin/bash "$work/bash.out"
if [ "$os" = "darwin" ]; then
  bash_profile="$bash_home/.profile"
  test ! -e "$bash_home/.bash_profile"
else
  bash_profile="$bash_home/.bashrc"
fi
test "$(awk '/# added by memsync$/ { count++ } END { print count+0 }' "$bash_profile")" -eq 1

# A custom install directory is safely quoted in the generated shell line.
custom_home="$work/custom-home"
custom_bin="$custom_home/custom 'bin"
mkdir -p "$custom_home"
HOME="$custom_home" \
  SHELL=/bin/sh \
  PATH="$fakebin:$base_path" \
  MEMSYNC_BINDIR="$custom_bin" \
  MEMSYNC_INSTALL_FIXTURE="$fixture" \
  sh "$root/install.sh" > "$work/custom.out"
found="$(HOME="$custom_home" PATH="$fakebin:$base_path" /bin/sh -c '. "$HOME/.profile"; command -v memsync')"
test "$found" = "$custom_bin/memsync"

# csh/tcsh get their native startup syntax instead of an invalid POSIX export.
csh_home="$work/csh-home"
mkdir -p "$csh_home"
run_install "$csh_home" /bin/tcsh "$work/csh.out"
grep -F 'setenv PATH "$HOME/.local/bin":$PATH # added by memsync' "$csh_home/.cshrc" >/dev/null

# Unknown shells are not modified or falsely reported as configured.
unknown_home="$work/unknown-home"
mkdir -p "$unknown_home"
run_install "$unknown_home" /bin/nushell "$work/unknown.out"
test ! -e "$unknown_home/.profile"
grep -F "memsync is installed at $unknown_home/.local/bin/memsync." "$work/unknown.out" >/dev/null
! grep -F 'available in new terminals' "$work/unknown.out" >/dev/null

# If the canonical directory is already on PATH, no startup file is touched.
ready_home="$work/already-ready"
mkdir -p "$ready_home"
HOME="$ready_home" \
  SHELL=/bin/sh \
  PATH="$ready_home/.local/bin:$fakebin:$base_path" \
  MEMSYNC_INSTALL_FIXTURE="$fixture" \
  sh "$root/install.sh" > "$work/already-ready.out"
test ! -e "$ready_home/.profile"

test "$(wc -l < "$fixture/init-calls" | tr -d ' ')" -eq "$expected_init_calls"
echo "installer tests passed"
