#!/bin/sh
# Bangku Sekolah Exam Node bootstrap installer.
# This file is intentionally POSIX sh so it can be run with: curl ... | sh
set -eu

REPOSITORY_URL='https://github.com/celpung/bangkusekolah_exam_node.git'
REPOSITORY_REF='main'
INSTALL_DIR='/opt/bangkusekolah/exam-node'

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

as_root() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
    return
  fi
  command -v sudo >/dev/null 2>&1 || fail 'run as root or install sudo before running the installer'
  sudo "$@"
}

is_supported_apt_host() {
  [ -r /etc/os-release ] || return 1
  grep -Eq '^ID=(debian|ubuntu)([[:space:]]|$)' /etc/os-release
}

[ -r /dev/tty ] || fail 'an interactive terminal is required; rerun from a terminal session'
is_supported_apt_host || fail 'this bootstrap currently supports Debian and Ubuntu only'
command -v apt-get >/dev/null 2>&1 || fail 'apt-get is required on the bootstrap host'

printf '%s\n' '==> install bootstrap dependencies'
as_root apt-get update
as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y bash ca-certificates curl git

command -v git >/dev/null 2>&1 || fail 'git installation failed'
command -v bash >/dev/null 2>&1 || fail 'bash installation failed'

printf '%s\n' '==> prepare Exam Node source'
as_root mkdir -p "$(dirname "$INSTALL_DIR")"
if [ -e "$INSTALL_DIR" ]; then
  [ -d "$INSTALL_DIR/.git" ] || fail "install directory exists and is not an Exam Node git checkout: $INSTALL_DIR"
  existing_remote="$(as_root git -C "$INSTALL_DIR" remote get-url origin 2>/dev/null || true)"
  [ "$existing_remote" = "$REPOSITORY_URL" ] || fail "install directory belongs to an unexpected repository: $INSTALL_DIR"
  printf '%s\n' "Using existing Exam Node checkout at $INSTALL_DIR"
else
  as_root git clone --branch "$REPOSITORY_REF" --depth 1 "$REPOSITORY_URL" "$INSTALL_DIR"
fi

[ -f "$INSTALL_DIR/setup.sh" ] || fail "Exam Node setup.sh is missing from $INSTALL_DIR"
for required_file in Dockerfile docker-compose.yml scripts/nginx-config.sh scripts/maintenance/run-tool.sh; do
  [ -f "$INSTALL_DIR/$required_file" ] || fail "required Exam Node file is missing: $required_file"
done
as_root chmod 750 "$INSTALL_DIR/setup.sh"
printf '%s\n' '==> hand off to Exam Node setup.sh'
as_root "$INSTALL_DIR/setup.sh" </dev/tty
