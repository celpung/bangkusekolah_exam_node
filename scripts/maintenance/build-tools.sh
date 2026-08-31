#!/usr/bin/env bash
# Build the Exam Node operational tools for a Linux VPS.
#
# Run this from a developer machine or CI checkout. The VPS only needs Docker;
# Go is used here to produce static binaries that run in the Exam Node image.
#
#   ./scripts/maintenance/build-tools.sh          # linux/amd64
#   ./scripts/maintenance/build-tools.sh arm64   # linux/arm64
set -euo pipefail

ARCH="${1:-amd64}"
case "$ARCH" in
  amd64|arm64) ;;
  *)
    printf 'unsupported architecture: %s (use amd64 or arm64)\n' "$ARCH" >&2
    exit 2
    ;;
esac

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT_DIR="${OUT_DIR:-$ROOT/dist/tools}"
TOOLS=(bundleload preflight examharvest)

command -v go >/dev/null 2>&1 || {
  printf '%s\n' 'go is required on the build machine' >&2
  exit 1
}

mkdir -p "$OUT_DIR"
for tool in "${TOOLS[@]}"; do
  printf 'Building %s for linux/%s...\n' "$tool" "$ARCH"
  CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" \
    go build -trimpath -ldflags='-s -w' \
    -o "$OUT_DIR/$tool" "$ROOT/cmd/$tool"
done

printf '\nBuilt tools:\n'
for tool in "${TOOLS[@]}"; do
  printf '  %s  ' "$OUT_DIR/$tool"
  sha256sum "$OUT_DIR/$tool" | cut -d' ' -f1
done

printf '\nShip to the VPS:\n'
printf '  ssh <USER>@<HOST> "mkdir -p /opt/bangkusekolah/exam-node/bin"\n'
printf '  scp %s/* <USER>@<HOST>:/opt/bangkusekolah/exam-node/bin/\n' "$OUT_DIR"
printf '  scp %s <USER>@<HOST>:/opt/bangkusekolah/exam-node/scripts/maintenance/\n' "$ROOT/scripts/maintenance/run-tool.sh"
printf '  ssh <USER>@<HOST> "chmod +x /opt/bangkusekolah/exam-node/bin/* /opt/bangkusekolah/exam-node/scripts/maintenance/run-tool.sh"\n'
