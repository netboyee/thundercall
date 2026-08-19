#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

SERVICE_NAME="${THUNDERCALL_SYSTEMD_SERVICE_NAME:-thundercall-compose.service}"
SYSTEMD_DIR="${THUNDERCALL_SYSTEMD_DIR:-/etc/systemd/system}"
DOCKER_BIN="${THUNDERCALL_DOCKER_BIN:-/usr/bin/docker}"
COMPOSE_FILE="${THUNDERCALL_COMPOSE_FILE:-$REPO_ROOT/compose.yaml}"
COMPOSE_PROFILES="${THUNDERCALL_COMPOSE_PROFILES:-nwws}"
SERVICE_PATH="$SYSTEMD_DIR/$SERVICE_NAME"
TMP_SERVICE="$(mktemp)"

log() {
  printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    log "missing required command: $1"
    exit 1
  fi
}

require_cmd sudo
require_cmd systemctl

if [ ! -x "$DOCKER_BIN" ]; then
  log "docker binary not found or not executable at $DOCKER_BIN"
  exit 1
fi

if [ ! -f "$COMPOSE_FILE" ]; then
  log "compose file not found: $COMPOSE_FILE"
  exit 1
fi

if [ ! -f "$REPO_ROOT/.env.docker" ]; then
  log "missing $REPO_ROOT/.env.docker"
  log "copy .env.docker.example to .env.docker and fill it in before installing the service"
  exit 1
fi

cat >"$TMP_SERVICE" <<EOF
[Unit]
Description=ThunderCall Docker Compose Stack
Requires=docker.service
After=docker.service network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=$REPO_ROOT
Environment=COMPOSE_FILE=$COMPOSE_FILE
Environment=COMPOSE_PROFILES=$COMPOSE_PROFILES
ExecStart=$DOCKER_BIN compose up -d --remove-orphans
ExecStop=$DOCKER_BIN compose down
TimeoutStartSec=0

[Install]
WantedBy=multi-user.target
EOF

log "installing systemd unit to $SERVICE_PATH"
sudo install -D -m 0644 "$TMP_SERVICE" "$SERVICE_PATH"
rm -f "$TMP_SERVICE"

log "reloading systemd"
sudo systemctl daemon-reload

log "enabling $SERVICE_NAME"
sudo systemctl enable "$SERVICE_NAME"

cat <<EOF

Installed $SERVICE_NAME.

Next steps:
  1. Start Docker on boot:
       sudo systemctl enable --now docker
  2. Start the ThunderCall stack now:
       sudo systemctl start $SERVICE_NAME
  3. Check service state:
       sudo systemctl status $SERVICE_NAME
  4. Check containers:
       docker compose ps

The service uses:
  repo root:        $REPO_ROOT
  compose file:     $COMPOSE_FILE
  compose profiles: $COMPOSE_PROFILES

Edit the installed unit if you want different profiles or compose args:
  $SERVICE_PATH
EOF
