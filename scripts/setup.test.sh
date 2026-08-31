#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SETUP="$ROOT/setup.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

help_output="$($SETUP --help)"
case "$help_output" in
  *"-central_url"* ) ;;
  *)
    printf '%s\n' 'FAIL: help does not document -central_url' >&2
    exit 1
    ;;
esac

set +e
$SETUP -central_url http://central.example.test >/dev/null 2>&1
status=$?
set -e
if [[ "$status" -eq 0 ]]; then
  printf '%s\n' 'FAIL: production setup accepted an HTTP Central URL' >&2
  exit 1
fi

set +e
$SETUP -central_url https://central.example.test -unknown value >/dev/null 2>&1
status=$?
set -e
if [[ "$status" -eq 0 ]]; then
  printf '%s\n' 'FAIL: setup accepted an unknown option' >&2
  exit 1
fi

printf '%s\n' 'PASS: setup parser and production guards'
