#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$ROOT/scripts/maintenance/run-tool.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$TMP/deploy/bin" "$TMP/fake-bin"
touch "$TMP/deploy/.env" "$TMP/deploy/bin/preflight"
chmod +x "$TMP/deploy/bin/preflight"

cat >"$TMP/fake-bin/docker" <<'FAKE_DOCKER'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${FAKE_DOCKER_LOG:?}"
if [[ "$*" == "compose images -q examnode" ]]; then
  printf 'local-image-id\n'
fi
FAKE_DOCKER
chmod +x "$TMP/fake-bin/docker"

export FAKE_DOCKER_LOG="$TMP/docker.log"
PATH="$TMP/fake-bin:$PATH" "$SCRIPT" --dir "$TMP/deploy" preflight --help

if ! grep -Fq 'compose run --rm --no-deps' "$FAKE_DOCKER_LOG"; then
  printf '%s\n' 'FAIL: run-tool did not use docker compose run' >&2
  exit 1
fi
if ! grep -Fq -- '--entrypoint /tools/preflight' "$FAKE_DOCKER_LOG"; then
  printf '%s\n' 'FAIL: run-tool did not mount the selected tool entrypoint' >&2
  exit 1
fi

set +e
PATH="$TMP/fake-bin:$PATH" "$SCRIPT" --dir "$TMP/deploy" unsupported >/dev/null 2>&1
status=$?
set -e
if [[ "$status" -eq 0 ]]; then
  printf '%s\n' 'FAIL: unsupported tool was accepted' >&2
  exit 1
fi

printf '%s\n' 'PASS: run-tool allowlist and Docker execution'
