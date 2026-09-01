#!/usr/bin/env bash
# Shared, side-effect-free helpers for the Exam Node Nginx setup.

normalize_nginx_host() {
  local value="${1:-}"

  # Trim surrounding whitespace before validating user input.
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"

  case "$value" in
    https://*) value="${value#https://}" ;;
    http://*) value="${value#http://}" ;;
    *://*) return 1 ;;
  esac

  value="${value%/}"
  [[ -n "$value" ]] || return 1
  [[ "$value" != */* && "$value" != *'?'* && "$value" != *'#'* ]] || return 1
  [[ "$value" != *'@'* && "$value" != *':'* ]] || return 1

  # Accept DNS hostnames (including localhost) but not paths, ports, shell
  # metacharacters, underscores, or other Nginx directive syntax.
  if [[ ! "$value" =~ ^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$ ]]; then
    return 1
  fi

  printf '%s\n' "$value"
}

validate_certbot_email() {
  local email="${1:-}"
  [[ "$email" =~ ^[^[:space:]@]+@[^[:space:]@]+\.[^[:space:]@]+$ ]]
}

render_proxy_location() {
  local upstream="${1:?upstream is required}"

  cat <<EOF
    location / {
        proxy_pass http://$upstream;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
EOF
}

render_nginx_config() {
  local host="${1:?hostname is required}"
  local upstream="${2:-127.0.0.1:8080}"
  local tls_enabled="${3:-false}"

  [[ "$tls_enabled" == true || "$tls_enabled" == false ]] || return 1

  cat <<EOF
# Managed by Bangku Sekolah Exam Node setup.sh. Do not edit by hand.
server {
    listen 80;
    listen [::]:80;
    server_name $host;

    client_max_body_size 2m;
    proxy_connect_timeout 10s;
    proxy_read_timeout 60s;
    proxy_send_timeout 60s;

    location ^~ /.well-known/acme-challenge/ {
        root /var/www/certbot;
        try_files \$uri =404;
    }

    location ^~ /internal/v1/ {
        return 404;
    }
EOF

  if [[ "$tls_enabled" == true ]]; then
    cat <<'EOF'
    location / {
        return 301 https://$host$request_uri;
    }
}
EOF
  else
    render_proxy_location "$upstream"
    printf '%s\n' '}'
  fi

  if [[ "$tls_enabled" == true ]]; then
    cat <<EOF
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name $host;

    ssl_certificate /etc/letsencrypt/live/$host/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$host/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    add_header Strict-Transport-Security "max-age=31536000" always;

    client_max_body_size 2m;
    proxy_connect_timeout 10s;
    proxy_read_timeout 60s;
    proxy_send_timeout 60s;

    location ^~ /internal/v1/ {
        return 404;
    }
EOF
    render_proxy_location "$upstream"
    printf '%s\n' '}'
  fi
}
