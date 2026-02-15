#!/usr/bin/env bash
set -euo pipefail

NS="${NS:-railpush}"
CNPG_CLUSTER="${CNPG_CLUSTER:-railpush-postgres-cnpg}"

# If POD is unset (or POD=auto), we prefer backing up from the CNPG primary.
POD="${POD:-auto}"

DB="${DB:-railpush}"
DB_USER="${DB_USER:-railpush}"
CNPG_DUMP_USER="${CNPG_DUMP_USER:-postgres}"

REMOTE_HOST="${REMOTE_HOST:-142.132.255.45}"
REMOTE_USER="${REMOTE_USER:-railpush-backup}"
REMOTE_DIR="${REMOTE_DIR:-/var/backups/railpush/postgres}"
SSH_KEY="${SSH_KEY:-/root/.ssh/railpush_backup_to_data_ed25519}"

LOCAL_DIR="${LOCAL_DIR:-/var/backups/railpush/postgres}"
RETENTION_DAYS_LOCAL="${RETENTION_DAYS_LOCAL:-7}"
RETENTION_DAYS_REMOTE="${RETENTION_DAYS_REMOTE:-30}"

log() {
  printf "%s %s\n" "$(date -u +%FT%TZ)" "$*"
}

select_pod() {
  local any primary

  if [ -n "${POD}" ] && [ "${POD}" != "auto" ]; then
    return 0
  fi

  any="$(kubectl -n "$NS" get pod -l "cnpg.io/cluster=${CNPG_CLUSTER}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  primary="$(kubectl -n "$NS" get pod -l "cnpg.io/cluster=${CNPG_CLUSTER},cnpg.io/instanceRole=primary" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"

  if [ -n "$primary" ]; then
    POD="$primary"
    log "Selected CNPG primary pod: ${NS}/${POD} (cluster=${CNPG_CLUSTER})"
    return 0
  fi

  if [ -n "$any" ]; then
    log "ERROR: CNPG cluster ${CNPG_CLUSTER} exists but no primary pod found"
    exit 1
  fi

  POD="railpush-postgres-0"
  log "Selected legacy Postgres pod: ${NS}/${POD}"
}

dump_user_for_pod() {
  # CNPG pods are named like <clusterName>-<n> and also carry a cnpg.io/cluster label.
  # We treat either signal as "this is a CNPG pod" so we can use the superuser role (postgres)
  # and avoid peer-auth issues with the app user.
  if [[ "$POD" == "${CNPG_CLUSTER}-"* ]]; then
    printf "%s" "$CNPG_DUMP_USER"
    return 0
  fi
  if kubectl -n "$NS" get pod "$POD" -o jsonpath='{.metadata.labels.cnpg\\.io/cluster}' 2>/dev/null | grep -q .; then
    printf "%s" "$CNPG_DUMP_USER"
  else
    printf "%s" "$DB_USER"
  fi
}

mkdir -p "$LOCAL_DIR"
chmod 0750 "$LOCAL_DIR"

if [ ! -f "$SSH_KEY" ]; then
  log "ERROR: SSH key not found at $SSH_KEY"
  exit 1
fi

# Ensure the data server host key is pinned (no interactive prompts during the timer run).
if ! ssh-keygen -F "$REMOTE_HOST" >/dev/null 2>&1; then
  ssh-keyscan -H "$REMOTE_HOST" >> /root/.ssh/known_hosts 2>/dev/null || true
fi

SSH_OPTS=(
  -i "$SSH_KEY"
  -o BatchMode=yes
  -o StrictHostKeyChecking=yes
)

umask 077

select_pod
dump_user="$(dump_user_for_pod)"

ts="$(date -u +%Y%m%dT%H%M%SZ)"
base="railpush_${ts}"
tmp="${LOCAL_DIR}/${base}.dump.zst.partial"
out="${LOCAL_DIR}/${base}.dump.zst"
sha="${out}.sha256"

log "Starting pg_dump (${NS}/${POD} db=${DB} user=${dump_user})"
# Use custom format so restores are fast and flexible.
kubectl -n "$NS" exec "$POD" -- \
  pg_dump -U "$dump_user" -d "$DB" --format=custom --no-owner --no-acl \
  | zstd -T0 -6 -q -o "$tmp"

mv -f "$tmp" "$out"

# Integrity check.
zstd -tq "$out"
if [ "$(zstdcat "$out" 2>/dev/null | head -c 5)" != "PGDMP" ]; then
  log "ERROR: dump header mismatch (expected PGDMP)"
  exit 1
fi

sha256sum "$out" > "$sha"

size_h="$(du -h "$out" | awk '{print $1}')"
log "Dump complete: $out ($size_h)"

log "Syncing to data: ${REMOTE_USER}@${REMOTE_HOST}:${REMOTE_DIR}"
rsync -av \
  --chmod=F600,D750 \
  -e "ssh ${SSH_OPTS[*]}" \
  "$out" "$sha" \
  "${REMOTE_USER}@${REMOTE_HOST}:${REMOTE_DIR}/"

log "Pruning remote backups older than ${RETENTION_DAYS_REMOTE}d"
ssh "${SSH_OPTS[@]}" "${REMOTE_USER}@${REMOTE_HOST}" \
  "find '${REMOTE_DIR}' -type f -name 'railpush_*.dump.zst' -mtime +${RETENTION_DAYS_REMOTE} -delete; \
   find '${REMOTE_DIR}' -type f -name 'railpush_*.sha256' -mtime +${RETENTION_DAYS_REMOTE} -delete"

log "Pruning local backups older than ${RETENTION_DAYS_LOCAL}d"
find "$LOCAL_DIR" -type f -name 'railpush_*.dump.zst' -mtime +"${RETENTION_DAYS_LOCAL}" -delete
find "$LOCAL_DIR" -type f -name 'railpush_*.sha256' -mtime +"${RETENTION_DAYS_LOCAL}" -delete

log "Backup finished"
