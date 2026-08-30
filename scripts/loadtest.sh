#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/loadtest.sh go
  scripts/loadtest.sh k6 [base-url] [exam-id]

The Go mode requires TEST_DB_DSN for a database whose server-reported
name ends with _test. The k6 mode requires k6 and a running node.
EOF
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

case "${1:-go}" in
  go)
    if [[ -z "${TEST_DB_DSN:-}" ]]; then
      echo "FAIL: TEST_DB_DSN is required for the Go load test" >&2
      exit 2
    fi
    echo "==> Go burst test (1000 students, 3 phases)"
    export DB_DSN="$TEST_DB_DSN"
    go test -tags=load -run '^TestBurst$' -count=1 -v ./tests/load/
    ;;
  k6)
    BASE_URL="${2:-http://127.0.0.1:8080}"
    EXAM_ID="${3:-exam-burst}"
    echo "==> k6 load test against ${BASE_URL}"
    echo "    Requires: k6 installed, node running, bundles loaded"
    k6 run --env "BASE_URL=${BASE_URL}" --env "EXAM_ID=${EXAM_ID}" scripts/loadtest.js
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac
