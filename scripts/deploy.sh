#!/usr/bin/env bash
set -euo pipefail
# deploy.sh — D-1: pull N deployed bundles and verify. Run after the admin
# selects M exams and clicks "Deploy to node" in central (multi-exam per VPS —
# see Task 6). Idempotent — re-pulling the same bundles is a no-op (checksum +
# ON DUPLICATE KEY on participants). Dashboard displays the mapping exam → VPS
# (exam title / deploymentId / counts) so the operator can verify placement.
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "==> pull bundles from central (one per exam)"
go run ./cmd/bundleload --pull

echo "==> preflight (per-bundle checksums + counts)"
go run ./cmd/preflight

echo "==> liveness"
curl -sf http://127.0.0.1:8080/livez

echo "==> bundle checksums"
go run ./cmd/bundleload --help | head -n 5
# The real checksums are printed by bundleload itself; this is just a smoke.

echo "==> deploy done — D-0 checklist is preflight PASS + liveness 200"
