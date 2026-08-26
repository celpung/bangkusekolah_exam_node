#!/usr/bin/env bash
set -euo pipefail
# provision.sh — D-3: bring up the node, pull the bundles, verify.
# Usage: ./scripts/provision.sh [--bundle /path/bundle.json]
# If --bundle is omitted, bundles are pulled from central via bundleload --pull.
# With multi-exam per VPS (Task 6), provision pulls all deployments for the node;
# dashboard shows the mapping exam → VPS so the admin can verify M exams on one box.
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "==> docker compose up"
docker compose up -d --wait

echo "==> bundleload"
if [[ "${1:-}" == "--bundle" ]]; then
  go run ./cmd/bundleload --bundle "$2"
else
  go run ./cmd/bundleload --pull
fi

echo "==> preflight"
go run ./cmd/preflight

echo "==> readiness"
curl -sf http://127.0.0.1:8080/readyz | jq . || curl -sf http://127.0.0.1:8080/readyz

echo "==> provision done — node is ready for D-0"
