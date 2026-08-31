#!/usr/bin/env bash
# One-command production setup for a Bangku Sekolah Exam Node VPS.
#
# The script expects the node to have been registered in Central already. It
# creates/updates .env without printing secret values, builds the three one-shot
# tools inside a temporary Go Docker container, starts the Compose stack, enables
# systemd auto-start, pulls bundles, runs preflight, and verifies readiness.
#
# Example:
#   sudo ./setup.sh -central_url https://central.example.id
#
# Secrets can be supplied with -central_node_token and -node_jwt_secret, but a
# hidden prompt is safer and is used when either value is omitted.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
ENV_FILE="$ROOT/.env"
SERVICE_NAME="bangkusekolah-exam-node"
CENTRAL_URL=""
CENTRAL_NODE_TOKEN="${CENTRAL_NODE_TOKEN:-}"
NODE_JWT_SECRET="${NODE_JWT_SECRET:-}"
SKIP_PULL=false
SKIP_PREFLIGHT=false

usage() {
  cat <<'EOF'
Usage:
  sudo ./setup.sh -central_url https://central.example.id

Options:
  -central_url URL             Required HTTPS Central Service base URL
  -central_node_token TOKEN    Node token returned by Central registration
  -node_jwt_secret SECRET      Node-local JWT signing secret (32+ characters)
  -skip_pull                   Start the stack without pulling bundles
  -skip_preflight              Skip preflight (not recommended for production)
  -h, --help                   Show this help

The node must be registered in Central before setup. If secrets are omitted,
setup prompts for the Central node token and generates the node JWT secret.
EOF
}

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

require_value() {
  [[ $# -ge 2 && -n "${2:-}" ]] || fail "$1 requires a value"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -central_url|--central-url|-central-url)
      require_value "$1" "${2:-}"
      CENTRAL_URL="$2"
      shift 2
      ;;
    -central_node_token|--central-node-token|-central-node-token)
      require_value "$1" "${2:-}"
      CENTRAL_NODE_TOKEN="$2"
      shift 2
      ;;
    -node_jwt_secret|--node-jwt-secret|-node-jwt-secret)
      require_value "$1" "${2:-}"
      NODE_JWT_SECRET="$2"
      shift 2
      ;;
    -skip_pull|--skip-pull)
      SKIP_PULL=true
      shift
      ;;
    -skip_preflight|--skip-preflight)
      SKIP_PREFLIGHT=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown option: $1"
      ;;
  esac
done

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  fail 'run as root or with sudo'
fi

[[ -n "$CENTRAL_URL" ]] || fail '-central_url is required'
case "$CENTRAL_URL" in
  https://*) ;;
  *) fail 'Central URL must use HTTPS in production' ;;
esac
CENTRAL_URL="${CENTRAL_URL%/}"

read_env_value() {
  local key="$1"
  local line
  [[ -f "$ENV_FILE" ]] || return 0
  while IFS= read -r line || [[ -n "$line" ]]; do
    case "$line" in
      "$key="*)
        printf '%s' "${line#*=}"
        return 0
        ;;
    esac
  done < "$ENV_FILE"
}

upsert_env_value() {
  local key="$1"
  local value="$2"
  local temp
  local found=0
  local line

  temp="$(mktemp "$ROOT/.env.tmp.XXXXXX")"
  chmod 600 "$temp"
  if [[ -f "$ENV_FILE" ]]; then
    while IFS= read -r line || [[ -n "$line" ]]; do
      case "$line" in
        "$key="*)
          printf '%s=%s\n' "$key" "$value" >> "$temp"
          found=1
          ;;
        *)
          printf '%s\n' "$line" >> "$temp"
          ;;
      esac
    done < "$ENV_FILE"
  fi
  if [[ "$found" -eq 0 ]]; then
    printf '%s=%s\n' "$key" "$value" >> "$temp"
  fi
  chmod 600 "$temp"
  mv "$temp" "$ENV_FILE"
}

export DEBIAN_FRONTEND=noninteractive
if command -v apt-get >/dev/null 2>&1; then
  apt-get update
  apt-get install -y ca-certificates curl git jq openssl
  if ! command -v docker >/dev/null 2>&1; then
    apt-get install -y docker.io
  fi
  if ! docker compose version >/dev/null 2>&1; then
    apt-get install -y docker-compose-plugin
  fi
else
  command -v docker >/dev/null 2>&1 || fail 'Docker is required on this VPS'
  command -v openssl >/dev/null 2>&1 || fail 'openssl is required on this VPS'
fi

command -v docker >/dev/null 2>&1 || fail 'Docker installation failed'
docker compose version >/dev/null 2>&1 || fail 'Docker Compose plugin is required'
if command -v systemctl >/dev/null 2>&1; then
  systemctl enable --now docker >/dev/null 2>&1 || true
fi

if [[ -z "$CENTRAL_NODE_TOKEN" ]]; then
  CENTRAL_NODE_TOKEN="$(read_env_value CENTRAL_NODE_TOKEN || true)"
fi
if [[ -z "$CENTRAL_NODE_TOKEN" ]]; then
  [[ -t 0 ]] || fail 'CENTRAL_NODE_TOKEN is required in non-interactive mode'
  read -r -s -p 'Central node token: ' CENTRAL_NODE_TOKEN
  printf '\n'
fi

if [[ -z "$NODE_JWT_SECRET" ]]; then
  NODE_JWT_SECRET="$(read_env_value NODE_JWT_SECRET || true)"
fi
if [[ -z "$NODE_JWT_SECRET" ]]; then
  NODE_JWT_SECRET="$(openssl rand -hex 32)"
fi
[[ "${#NODE_JWT_SECRET}" -ge 32 ]] || fail 'NODE_JWT_SECRET must contain at least 32 characters'

upsert_env_value HTTP_PORT 8080
upsert_env_value CENTRAL_BASE_URL "$CENTRAL_URL"
upsert_env_value CENTRAL_NODE_TOKEN "$CENTRAL_NODE_TOKEN"
upsert_env_value NODE_JWT_SECRET "$NODE_JWT_SECRET"
chmod 600 "$ENV_FILE"

printf '%s\n' '==> build operational tools inside Docker'
mkdir -p "$ROOT/bin"
case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64) ARCH=arm64 ;;
  *) fail 'unsupported VPS architecture' ;;
esac

docker run --rm \
  -v "$ROOT:/src" \
  -w /src \
  -e CGO_ENABLED=0 \
  -e GOOS=linux \
  -e GOARCH="$ARCH" \
  golang:1.26-alpine \
  sh -c 'set -eu; for tool in bundleload preflight examharvest; do go build -trimpath -ldflags="-s -w" -o "bin/$tool" "./cmd/$tool"; done'
chmod +x "$ROOT/bin/bundleload" "$ROOT/bin/preflight" "$ROOT/bin/examharvest"

printf '%s\n' '==> start Exam Node stack'
cd "$ROOT"
docker compose up -d --build --wait

docker compose ps

if [[ "$SKIP_PULL" != true ]]; then
  printf '%s\n' '==> pull exam bundles'
  scripts/maintenance/run-tool.sh bundleload --pull
fi

if [[ "$SKIP_PREFLIGHT" != true ]]; then
  printf '%s\n' '==> run preflight'
  scripts/maintenance/run-tool.sh preflight
fi

if command -v systemctl >/dev/null 2>&1; then
  cat >/etc/systemd/system/${SERVICE_NAME}.service <<EOF
[Unit]
Description=Bangku Sekolah Exam Node
Requires=docker.service
After=docker.service network-online.target

[Service]
Type=oneshot
WorkingDirectory=$ROOT
ExecStart=/usr/bin/docker compose up -d --wait
ExecStop=/usr/bin/docker compose down
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable "$SERVICE_NAME.service" >/dev/null
fi

printf '%s\n' '==> verify readiness'
if [[ "$SKIP_PREFLIGHT" == true ]]; then
  printf '%s\n' 'WARNING: preflight was skipped; readiness is not a production go/no-go result.'
else
  for _ in {1..30}; do
    if curl -fsS http://127.0.0.1:8080/readyz >/dev/null; then
      printf '%s\n' 'PASS: Exam Node is ready'
      exit 0
    fi
    sleep 1
done
  fail 'Exam Node did not become ready; inspect docker compose logs --tail=200 examnode'
fi

printf '%s\n' 'SETUP COMPLETE: preflight passed; Exam Node is ready'
