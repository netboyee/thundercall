#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

COMPOSE_FILE="${THUNDERCALL_BACKUP_COMPOSE_FILE:-$REPO_ROOT/compose.yaml}"
MYSQL_SERVICE="${THUNDERCALL_BACKUP_MYSQL_SERVICE:-mysql}"
MYSQL_DATABASE="${THUNDERCALL_BACKUP_MYSQL_DATABASE:-thundercall}"
MYSQL_USER="${THUNDERCALL_BACKUP_MYSQL_USER:-thundercall}"
MYSQL_PASSWORD="${THUNDERCALL_BACKUP_MYSQL_PASSWORD:-thundercall}"
BACKUP_DIR="${THUNDERCALL_BACKUP_DIR:-$REPO_ROOT/backups/mysql}"
RETENTION_DAYS="${THUNDERCALL_BACKUP_RETENTION_DAYS:-14}"
FILE_PREFIX="${THUNDERCALL_BACKUP_FILE_PREFIX:-thundercall}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
TMP_OUTPUT="$BACKUP_DIR/.${FILE_PREFIX}-${TIMESTAMP}.sql.gz.part"
FINAL_OUTPUT="$BACKUP_DIR/${FILE_PREFIX}-${TIMESTAMP}.sql.gz"

log() {
  printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    log "missing required command: $1"
    exit 1
  fi
}

cleanup() {
  rm -f "$TMP_OUTPUT"
}

trap cleanup EXIT

require_cmd docker
require_cmd gzip
require_cmd find

mkdir -p "$BACKUP_DIR"

log "starting MySQL backup database=$MYSQL_DATABASE service=$MYSQL_SERVICE output=$FINAL_OUTPUT"

docker compose -f "$COMPOSE_FILE" exec -T \
  -e MYSQL_PWD="$MYSQL_PASSWORD" \
  "$MYSQL_SERVICE" \
  mysqldump \
    -u "$MYSQL_USER" \
    --single-transaction \
    --quick \
    --routines \
    --triggers \
    --events \
    --hex-blob \
    --default-character-set=utf8mb4 \
    "$MYSQL_DATABASE" | gzip -9 > "$TMP_OUTPUT"

mv "$TMP_OUTPUT" "$FINAL_OUTPUT"

if [[ "$RETENTION_DAYS" =~ ^[0-9]+$ ]] && [ "$RETENTION_DAYS" -gt 0 ]; then
  while IFS= read -r old_backup; do
    [ -n "$old_backup" ] || continue
    log "pruning old backup $old_backup"
    rm -f "$old_backup"
  done < <(find "$BACKUP_DIR" -type f -name "${FILE_PREFIX}-*.sql.gz" -mtime "+$RETENTION_DAYS" -print | sort)
fi

SIZE_BYTES="$(wc -c < "$FINAL_OUTPUT" | tr -d '[:space:]')"
log "backup completed file=$FINAL_OUTPUT size_bytes=$SIZE_BYTES"
