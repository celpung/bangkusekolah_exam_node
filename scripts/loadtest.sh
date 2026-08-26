#!/usr/bin/env bash
set -euo pipefail
# loadtest.sh — Run the burst load test or k6 alternative.
# Usage:
#   ./scripts/loadtest.sh           # Go burst test (default)
#   ./scripts/loadtest.sh k6        # k6 HTTP load test
#   ./scripts/loadtest.sh k6 staging # k6 against staging
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

case "${1:-go}" in
  go)
    echo "==> Go burst test (1000 students, 3 phases)"
    echo "    Requires: TEST_DB_DSN or DB_* env vars pointing to a test MySQL"
    go test -tags=load -run TestBurst -count=1 -v ./tests/load/
    ;;
  k6)
    BASE_URL="${2:-http://127.0.0.1:8080}"
    EXAM_ID="${3:-exam-1}"
    echo "==> k6 load test against ${BASE_URL}"
    echo "    Requires: k6 installed, node running, bundles loaded"
    k6 run --env "BASE_URL=${BASE_URL}" --env "EXAM_ID=${EXAM_ID}" scripts/loadtest.js
    ;;
  *)
    echo "Usage: $0 [go|k6] [base_url] [exam_id]"
    exit 1
    ;;
esac
