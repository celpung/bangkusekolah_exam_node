#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
BOOTSTRAP="$ROOT/scripts/remote-installer.sh"
fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

[ -f "$BOOTSTRAP" ] || fail 'remote installer is missing'
sh -n "$BOOTSTRAP" || fail 'remote installer is not valid POSIX sh'

case "$(grep -F 'apt-get update' "$BOOTSTRAP" || true)" in
  '') fail 'remote installer does not update apt metadata' ;;
esac
case "$(grep -F 'apt-get install' "$BOOTSTRAP" || true)" in
  '') fail 'remote installer does not install bootstrap dependencies' ;;
esac
for dependency in git ca-certificates curl; do
  grep -F "$dependency" "$BOOTSTRAP" >/dev/null 2>&1 || fail "missing dependency: $dependency"
done

grep -F 'git clone' "$BOOTSTRAP" >/dev/null 2>&1 || fail 'remote installer does not clone the Exam Node source'
grep -F '/dev/tty' "$BOOTSTRAP" >/dev/null 2>&1 || fail 'remote installer does not preserve interactive terminal input'
grep -F 'bangkusekolah_exam_node.git' "$BOOTSTRAP" >/dev/null 2>&1 || fail 'repository URL is missing'
grep -F "REPOSITORY_REF='main'" "$BOOTSTRAP" >/dev/null 2>&1 || fail 'remote installer does not use the main branch'
if grep -F "REPOSITORY_REF='dev'" "$BOOTSTRAP" >/dev/null 2>&1; then
  fail 'remote installer still uses the dev branch'
fi
grep -F 'setup.sh' "$BOOTSTRAP" >/dev/null 2>&1 || fail 'remote installer does not hand off to setup.sh'

if grep -Eq '(^|[;&])[[:space:]]*source[[:space:]]' "$BOOTSTRAP"; then
  fail 'remote installer contains Bash-only syntax'
fi

printf '%s\n' 'PASS: remote installer bootstrap contract'
