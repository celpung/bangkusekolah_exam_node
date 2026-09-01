#!/usr/bin/env bash
# One-command production setup for a Bangku Sekolah Exam Node VPS.
#
# The script expects the node to have been registered in Central already. It
# creates/updates .env without printing secret values, installs/verifies Docker
# and Nginx, obtains a Let's Encrypt certificate with Certbot, generates the
# managed reverse-proxy site, builds the three one-shot tools inside a temporary
# Go Docker container, starts the Compose stack, enables systemd auto-start,
# pulls bundles, runs preflight, and verifies readiness.
#
# Example:
#   sudo ./setup.sh -central_url https://central.example.id -domain node.example.com -email ops@example.com
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
NODE_DOMAIN="${NODE_DOMAIN:-}"
CERTBOT_EMAIL="${CERTBOT_EMAIL:-}"
SKIP_PULL=false
SKIP_PREFLIGHT=false

# shellcheck source=./scripts/nginx-config.sh
source "$ROOT/scripts/nginx-config.sh"

usage() {
  cat <<'EOF'
Usage:
  sudo ./setup.sh -central_url https://central.example.id -domain node.example.com -email ops@example.com

Options:
  -central_url URL             Required HTTPS Central Service base URL
  -central_node_token TOKEN    Node token returned by Central registration
  -node_jwt_secret SECRET      Node-local JWT signing secret (32+ characters)
  -domain HOST_OR_URL          Public Exam Node hostname/URL for Nginx
  -email ADDRESS               ACME/Let's Encrypt renewal email address
  -skip_pull                   Start the stack without pulling bundles
  -skip_preflight              Skip preflight (not recommended for production)
  -h, --help                   Show this help

The node must be registered in Central before setup. In interactive mode, the
Central URL, node token, Nginx domain, and ACME email are prompted when omitted.
The node JWT secret is generated when it is not already present in .env.
EOF
}

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

require_value() {
  [[ $# -ge 2 && -n "${2:-}" ]] || fail "$1 requires a value"
}

read_tty() {
  local prompt="$1"
  local variable="$2"
  local secret="${3:-false}"

  [[ -r /dev/tty ]] || fail "$variable is required; run setup from an interactive terminal or provide it through the environment"
  if [[ "$secret" == true ]]; then
    if ! IFS= read -r -s -p "$prompt" "$variable" < /dev/tty; then
      fail "could not read $variable from the terminal"
    fi
    printf '\n'
  else
    if ! IFS= read -r -p "$prompt" "$variable" < /dev/tty; then
      fail "could not read $variable from the terminal"
    fi
  fi
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
    -domain|--domain|--node-domain|--node-url)
      require_value "$1" "${2:-}"
      NODE_DOMAIN="$2"
      shift 2
      ;;
    -email|--email|--certbot-email|--tls-email)
      require_value "$1" "${2:-}"
      CERTBOT_EMAIL="$2"
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

if [[ -z "$CENTRAL_URL" ]]; then
  read_tty 'Central base URL: ' CENTRAL_URL
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

if [[ -z "$CENTRAL_NODE_TOKEN" ]]; then
  CENTRAL_NODE_TOKEN="$(read_env_value CENTRAL_NODE_TOKEN || true)"
fi
if [[ -z "$CENTRAL_NODE_TOKEN" ]]; then
  read_tty 'Central node token: ' CENTRAL_NODE_TOKEN true
fi
[[ -n "$CENTRAL_NODE_TOKEN" ]] || fail 'CENTRAL_NODE_TOKEN is required in non-interactive mode'

if [[ -z "$NODE_DOMAIN" ]]; then
  NODE_DOMAIN="$(read_env_value NODE_DOMAIN || true)"
fi
if [[ -z "$NODE_DOMAIN" ]]; then
  read_tty 'Exam Node domain/URL for Nginx: ' NODE_DOMAIN
fi
NODE_DOMAIN="$(normalize_nginx_host "$NODE_DOMAIN")" || fail 'Exam Node domain/URL is invalid'

if [[ -z "$CERTBOT_EMAIL" ]]; then
  CERTBOT_EMAIL="$(read_env_value CERTBOT_EMAIL || true)"
fi
if [[ -z "$CERTBOT_EMAIL" ]]; then
  read_tty "ACME/Let's Encrypt email: " CERTBOT_EMAIL
fi
validate_certbot_email "$CERTBOT_EMAIL" || fail 'ACME email is invalid'

configure_nginx() {
  local tls_enabled="${1:-false}"
  local site_dir='/etc/nginx/sites-available'
  local enabled_dir='/etc/nginx/sites-enabled'
  local site_path="$site_dir/$NODE_DOMAIN"
  local enabled_path="$enabled_dir/$NODE_DOMAIN"
  local temp_path
  local managed_marker='# Managed by Bangku Sekolah Exam Node setup.sh. Do not edit by hand.'

  mkdir -p "$site_dir" "$enabled_dir"

  if [[ -e "$site_path" || -L "$site_path" ]] && ! grep -Fq "$managed_marker" "$site_path"; then
    fail "refusing to overwrite unmanaged Nginx site: $site_path"
  fi
  if [[ -e "$enabled_path" || -L "$enabled_path" ]]; then
    [[ -L "$enabled_path" ]] || fail "refusing to overwrite unmanaged Nginx link: $enabled_path"
    [[ "$(readlink "$enabled_path")" == "$site_path" ]] || fail "Nginx link points elsewhere: $enabled_path"
  fi

  temp_path="$(mktemp "$site_dir/.${NODE_DOMAIN}.tmp.XXXXXX")"
  render_nginx_config "$NODE_DOMAIN" '127.0.0.1:8080' "$tls_enabled" > "$temp_path"
  chmod 644 "$temp_path"
  mv "$temp_path" "$site_path"

  if [[ ! -e "$enabled_path" && ! -L "$enabled_path" ]]; then
    ln -s "$site_path" "$enabled_path"
  fi

  nginx -t
  if command -v systemctl >/dev/null 2>&1; then
    systemctl enable nginx >/dev/null
    if systemctl is-active --quiet nginx; then
      systemctl reload nginx
    else
      systemctl start nginx
    fi
  else
    nginx -s reload >/dev/null 2>&1 || nginx
  fi
  printf '%s\n' "PASS: Nginx configured for $NODE_DOMAIN"
}

configure_tls() {
  local webroot='/var/www/certbot'
  local certificate="/etc/letsencrypt/live/$NODE_DOMAIN/fullchain.pem"
  local private_key="/etc/letsencrypt/live/$NODE_DOMAIN/privkey.pem"
  local hook_dir='/etc/letsencrypt/renewal-hooks/deploy'
  local hook_path="$hook_dir/bangkusekolah-exam-node-nginx-reload.sh"
  local hook_temp

  mkdir -p "$webroot"
  chmod 755 "$webroot"
  printf '%s\n' '==> obtain/renew TLS certificate with Certbot'
  certbot certonly \
    --webroot \
    --webroot-path "$webroot" \
    --cert-name "$NODE_DOMAIN" \
    --domain "$NODE_DOMAIN" \
    --email "$CERTBOT_EMAIL" \
    --agree-tos \
    --non-interactive \
    --keep-until-expiring \
    --no-eff-email \
    --preferred-challenges http

  [[ -s "$certificate" && -s "$private_key" ]] || fail 'Certbot did not produce the expected certificate files'
  configure_nginx true

  command -v systemctl >/dev/null 2>&1 || fail 'systemd is required for automatic Certbot renewal'
  mkdir -p "$hook_dir"
  if [[ -e "$hook_path" || -L "$hook_path" ]] && ! grep -Fq '# Managed by Bangku Sekolah Exam Node setup.sh.' "$hook_path"; then
    fail "refusing to overwrite unmanaged Certbot deploy hook: $hook_path"
  fi
  hook_temp="$(mktemp "$hook_dir/.bangkusekolah-exam-node-nginx-reload.tmp.XXXXXX")"
  cat > "$hook_temp" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
# Managed by Bangku Sekolah Exam Node setup.sh. Do not edit by hand.
systemctl reload nginx
EOF
  chmod 755 "$hook_temp"
  mv "$hook_temp" "$hook_path"
  systemctl enable --now certbot.timer >/dev/null 2>&1 || fail 'failed to enable certbot renewal timer'
  printf '%s\n' "PASS: TLS certificate is installed for $NODE_DOMAIN"
}

export DEBIAN_FRONTEND=noninteractive
if command -v apt-get >/dev/null 2>&1; then
  apt-get update
  apt-get install -y ca-certificates curl git jq openssl
  if ! command -v docker >/dev/null 2>&1; then
    apt-get install -y docker.io
  fi
  if ! docker compose version >/dev/null 2>&1; then
    if apt-cache show docker-compose-v2 >/dev/null 2>&1; then
      apt-get install -y docker-compose-v2
    elif apt-cache show docker-compose-plugin >/dev/null 2>&1; then
      apt-get install -y docker-compose-plugin
    else
      fail 'Docker Compose package is unavailable; expected docker-compose-v2 or docker-compose-plugin'
    fi
  fi
  if ! command -v nginx >/dev/null 2>&1; then
    apt-get install -y nginx
  fi
  if ! command -v certbot >/dev/null 2>&1; then
    apt-get install -y certbot
  fi
else
  command -v docker >/dev/null 2>&1 || fail 'Docker is required on this VPS'
  command -v nginx >/dev/null 2>&1 || fail 'Nginx is required on this VPS'
  command -v certbot >/dev/null 2>&1 || fail 'Certbot is required on this VPS'
  command -v openssl >/dev/null 2>&1 || fail 'openssl is required on this VPS'
fi

command -v docker >/dev/null 2>&1 || fail 'Docker installation failed'
docker compose version >/dev/null 2>&1 || fail 'Docker Compose plugin is required'
command -v nginx >/dev/null 2>&1 || fail 'Nginx installation failed'
command -v certbot >/dev/null 2>&1 || fail 'Certbot installation failed'
if command -v systemctl >/dev/null 2>&1; then
  systemctl enable --now docker >/dev/null || fail 'failed to start Docker'
fi

configure_nginx false
configure_tls

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
upsert_env_value NODE_DOMAIN "$NODE_DOMAIN"
upsert_env_value CERTBOT_EMAIL "$CERTBOT_EMAIL"
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

printf '%s\n' '==> start Exam Node database'
cd "$ROOT"
docker compose up -d --wait mysql

printf '%s\n' '==> build Exam Node image'
docker compose build examnode

if [[ "$SKIP_PULL" != true ]]; then
  printf '%s\n' '==> pull exam bundles'
  scripts/maintenance/run-tool.sh bundleload --pull
fi

if [[ "$SKIP_PREFLIGHT" != true ]]; then
  printf '%s\n' '==> run preflight'
  scripts/maintenance/run-tool.sh preflight
fi

printf '%s\n' '==> start Exam Node service'
docker compose up -d --wait examnode
docker compose ps

printf '%s\n' '==> verify public HTTPS readiness'
curl -fsS --resolve "$NODE_DOMAIN:443:127.0.0.1" "https://$NODE_DOMAIN/readyz" >/dev/null || fail 'Exam Node HTTPS readiness check failed'
printf '%s\n' "PASS: HTTPS endpoint is ready for $NODE_DOMAIN"

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
