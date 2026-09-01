#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# This file is expected to be provided by the setup implementation.
# shellcheck source=/dev/null
source "$ROOT/scripts/nginx-config.sh"

fail_test() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

assert_eq() {
  local expected="$1"
  local actual="$2"
  local label="$3"
  [[ "$expected" == "$actual" ]] || fail_test "$label: got '$actual', want '$expected'"
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local label="$3"
  [[ "$haystack" == *"$needle"* ]] || fail_test "$label: missing '$needle'"
}

assert_not_contains() {
  local haystack="$1"
  local needle="$2"
  local label="$3"
  [[ "$haystack" != *"$needle"* ]] || fail_test "$label: unexpected '$needle'"
}

assert_eq "node.example.com" "$(normalize_nginx_host 'node.example.com')" 'plain hostname'
assert_eq "node.example.com" "$(normalize_nginx_host 'https://node.example.com/')" 'HTTPS hostname URL'
assert_eq "node.school.example" "$(normalize_nginx_host 'http://node.school.example')" 'HTTP hostname URL'
if ! validate_certbot_email 'ops@example.com'; then
  fail_test 'valid Certbot email rejected'
fi
if validate_certbot_email 'ops@example' >/dev/null 2>&1; then
  fail_test 'invalid Certbot email accepted'
fi

for invalid in '' 'https://' 'node.example.com/path' 'node.example.com:8443' 'node.example.com?x=1' 'node.example.com#fragment' 'user@node.example.com' 'node;example.com' '../node.example.com'; do
  if normalize_nginx_host "$invalid" >/dev/null 2>&1; then
    fail_test "invalid hostname accepted: $invalid"
  fi
done

config="$(render_nginx_config 'node.example.com' '127.0.0.1:8080')"
assert_contains "$config" 'server_name node.example.com;' 'server name'
assert_contains "$config" 'proxy_pass http://127.0.0.1:8080;' 'upstream'
assert_contains "$config" 'proxy_set_header X-Forwarded-Proto $scheme;' 'forwarded protocol'
assert_contains "$config" 'location ^~ /internal/v1/' 'internal route guard'
assert_contains "$config" 'return 404;' 'internal route rejection'
assert_contains "$config" '/.well-known/acme-challenge/' 'ACME challenge path'
assert_not_contains "$config" 'CENTRAL_NODE_TOKEN' 'central secret isolation'
assert_not_contains "$config" 'NODE_JWT_SECRET' 'node secret isolation'

tls_config="$(render_nginx_config 'node.example.com' '127.0.0.1:8080' true)"
assert_contains "$tls_config" 'listen 443 ssl;' 'TLS listener'
assert_contains "$tls_config" 'ssl_certificate /etc/letsencrypt/live/node.example.com/fullchain.pem;' 'certificate path'
assert_contains "$tls_config" 'return 301 https://$host$request_uri;' 'HTTP redirect'

printf '%s\n' 'PASS: Nginx hostname validation and config rendering'
