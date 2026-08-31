#!/usr/bin/env bash
# Run one Exam Node operational tool inside the deployed Docker service image.
#
# The VPS does not need Go or a source checkout beyond the deployment directory:
# static binaries are mounted read-only and receive the service's Docker-network
# environment from the examnode Compose service.
#
# Usage:
#   ./scripts/maintenance/run-tool.sh bundleload --pull
#   ./scripts/maintenance/run-tool.sh preflight
#   ./scripts/maintenance/run-tool.sh examharvest --force
set -euo pipefail

DEPLOY_DIR="${DEPLOY_DIR:-$(cd "$(dirname "$0")/../.." && pwd)}"

usage() {
  printf '%s\n' \
    'Usage: run-tool.sh [--dir DIR] <bundleload|preflight|examharvest> [args...]' \
    '' \
    'Tools:' \
    '  bundleload --pull       Pull and validate active deployments from Central' \
    '  preflight               Validate bundles, cache, disk, and clock' \
    '  examharvest --force     Sweep and harvest finished attempts now'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir)
      [[ $# -ge 2 ]] || { printf '%s\n' '--dir requires a path' >&2; exit 2; }
      DEPLOY_DIR="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    -*)
      printf 'unknown option: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
    *)
      break
      ;;
  esac
done

TOOL="${1:-}"
if [[ -z "$TOOL" ]]; then
  usage >&2
  exit 2
fi
shift

case "$TOOL" in
  bundleload|preflight|examharvest) ;;
  *)
    printf 'unsupported tool: %s\n' "$TOOL" >&2
    usage >&2
    exit 2
    ;;
esac

cd "$DEPLOY_DIR"
[[ -f .env ]] || {
  printf 'missing %s/.env\n' "$DEPLOY_DIR" >&2
  exit 1
}

BINARY="$DEPLOY_DIR/bin/$TOOL"
[[ -f "$BINARY" ]] || {
  printf 'missing tool binary: %s\n' "$BINARY" >&2
  printf '%s\n' 'Build it with scripts/maintenance/build-tools.sh and copy it to bin/.' >&2
  exit 1
}
[[ -x "$BINARY" ]] || {
  printf 'tool binary is not executable: %s\n' "$BINARY" >&2
  printf 'Run: chmod +x %s\n' "$BINARY" >&2
  exit 1
}

command -v docker >/dev/null 2>&1 || {
  printf '%s\n' 'docker is not installed' >&2
  exit 1
}

IMAGE_ID="$(docker compose images -q examnode 2>/dev/null || true)"
if [[ -z "$IMAGE_ID" ]]; then
  printf '%s\n' 'Exam Node image is missing; building it with Docker Compose...'
  docker compose build examnode
fi

printf 'Running %s in the Exam Node Docker network...\n' "$TOOL"
exec docker compose run --rm --no-deps \
  -v "$BINARY:/tools/$TOOL:ro" \
  --entrypoint "/tools/$TOOL" \
  examnode "$@"
