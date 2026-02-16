#!/usr/bin/env sh
set -eu

NS="${NAMESPACE:-railpush}"
ROOT="${BACKUP_DIR:-/backups}"
RETENTION_DAYS="${RETENTION_DAYS:-7}"
ZSTD_LEVEL="${ZSTD_LEVEL:-6}"
KUBECTL="${KUBECTL:-/usr/local/bin/kubectl}"

log() {
  printf "%s %s\n" "$(date -u +%FT%TZ)" "$*"
}

ensure_kubeconfig() {
  # kubectl does not always auto-detect in-cluster auth. Generate a minimal kubeconfig
  # from the mounted ServiceAccount token so this CronJob works reliably.
  if [ -n "${KUBECONFIG:-}" ] && [ -f "${KUBECONFIG}" ]; then
    return 0
  fi

  sa_dir="/var/run/secrets/kubernetes.io/serviceaccount"
  token_file="${sa_dir}/token"
  ca_file="${sa_dir}/ca.crt"
  ns_file="${sa_dir}/namespace"

  if [ ! -f "${token_file}" ] || [ ! -f "${ca_file}" ]; then
    log "ERROR: serviceaccount token/ca not mounted; cannot talk to Kubernetes API"
    exit 1
  fi
  if [ -z "${KUBERNETES_SERVICE_HOST:-}" ] || [ -z "${KUBERNETES_SERVICE_PORT:-}" ]; then
    log "ERROR: missing KUBERNETES_SERVICE_HOST/PORT; cannot talk to Kubernetes API"
    exit 1
  fi

  kube_ns="${NS}"
  if [ -f "${ns_file}" ]; then
    kube_ns="$(cat "${ns_file}" 2>/dev/null || echo "${NS}")"
  fi

  token="$(cat "${token_file}")"
  server="https://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT}"

  tmp_cfg="/tmp/kubeconfig"
  umask 077
  cat > "${tmp_cfg}" <<EOF
apiVersion: v1
kind: Config
clusters:
- name: in-cluster
  cluster:
    certificate-authority: ${ca_file}
    server: ${server}
users:
- name: sa
  user:
    token: ${token}
contexts:
- name: default
  context:
    cluster: in-cluster
    user: sa
    namespace: ${kube_ns}
current-context: default
EOF

  export KUBECONFIG="${tmp_cfg}"
}

ts="$(date -u +%Y%m%dT%H%M%SZ)"

mkdir -p "${ROOT}/postgres" "${ROOT}/redis"

fail=0

backup_postgres() {
  # Format: podName \t dbID \t workspaceID
  "${KUBECTL}" -n "${NS}" get pods \
    -l "app.kubernetes.io/managed-by=railpush,app.kubernetes.io/component=managed-database" \
    -o jsonpath="{range .items[*]}{.metadata.name}{\"\\t\"}{.metadata.labels['railpush.com/database-id']}{\"\\t\"}{.metadata.labels['railpush.com/workspace-id']}{\"\\n\"}{end}" \
    ;
}

backup_redis() {
  # Format: podName \t kvID \t workspaceID
  "${KUBECTL}" -n "${NS}" get pods \
    -l "app.kubernetes.io/managed-by=railpush,app.kubernetes.io/component=managed-keyvalue" \
    -o jsonpath="{range .items[*]}{.metadata.name}{\"\\t\"}{.metadata.labels['railpush.com/keyvalue-id']}{\"\\t\"}{.metadata.labels['railpush.com/workspace-id']}{\"\\n\"}{end}" \
    ;
}

ensure_kubeconfig

log "Starting tenant backups (ns=${NS} retention=${RETENTION_DAYS}d)"

tmp_pg="$(mktemp)"
backup_postgres > "${tmp_pg}"

pg_count=0
while IFS="$(printf '\t')" read -r pod dbid wsid; do
  [ -n "${pod}" ] || continue
  pg_count=$((pg_count + 1))

  # UUID labels should always exist; fall back to pod name if missing.
  [ -n "${dbid}" ] || dbid="${pod}"
  [ -n "${wsid}" ] || wsid="unknown"

  dir="${ROOT}/postgres/${wsid}/${dbid}"
  mkdir -p "${dir}"

  out="${dir}/${ts}.dump.zst"
  tmp="${out}.partial"

  log "Postgres backup: pod=${pod} workspace=${wsid} db=${dbid}"

  # Stream the dump out of the Postgres container, compress locally, then atomically move into place.
  if "${KUBECTL}" -n "${NS}" exec "${pod}" -c postgres -- sh -lc '
      set -eu
      export PGPASSWORD="$POSTGRES_PASSWORD"
      pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom --no-owner --no-acl
    ' \
    | zstd -T0 "-${ZSTD_LEVEL}" -q -o "${tmp}"; then
    mv -f "${tmp}" "${out}"

    # Basic integrity checks.
    zstd -tq "${out}"
    if [ "$(zstdcat "${out}" 2>/dev/null | head -c 5)" != "PGDMP" ]; then
      log "ERROR: postgres dump header mismatch: ${out}"
      fail=1
    fi
    sha256sum "${out}" > "${out}.sha256"
  else
    log "ERROR: postgres backup failed for pod=${pod}"
    rm -f "${tmp}" 2>/dev/null || true
    fail=1
  fi
done < "${tmp_pg}"
rm -f "${tmp_pg}" 2>/dev/null || true

tmp_kv="$(mktemp)"
backup_redis > "${tmp_kv}"

kv_count=0
while IFS="$(printf '\t')" read -r pod kvid wsid; do
  [ -n "${pod}" ] || continue
  kv_count=$((kv_count + 1))

  [ -n "${kvid}" ] || kvid="${pod}"
  [ -n "${wsid}" ] || wsid="unknown"

  dir="${ROOT}/redis/${wsid}/${kvid}"
  mkdir -p "${dir}"

  out="${dir}/${ts}.rdb.zst"
  tmp="${out}.partial"

  log "Redis backup: pod=${pod} workspace=${wsid} kv=${kvid}"

  # Trigger a background snapshot, wait for completion, then stream the RDB file.
  if ! "${KUBECTL}" -n "${NS}" exec "${pod}" -c redis -- sh -lc '
      set -eu
      redis-cli -a "$REDIS_PASSWORD" bgsave >/dev/null 2>&1 || true
      i=0
      while [ "$i" -lt 120 ]; do
        inprog="$(redis-cli -a "$REDIS_PASSWORD" info persistence | awk -F: "/^rdb_bgsave_in_progress:/{gsub(\"\\r\",\"\",\\$2); print \\$2}")"
        [ "${inprog:-0}" = "0" ] && break
        i=$((i + 1))
        sleep 1
      done
      status="$(redis-cli -a "$REDIS_PASSWORD" info persistence | awk -F: "/^rdb_last_bgsave_status:/{gsub(\"\\r\",\"\",\\$2); print \\$2}")"
      [ "${status:-}" = "ok" ]
    '; then
    log "ERROR: redis bgsave failed or timed out for pod=${pod}"
    fail=1
    continue
  fi

  if "${KUBECTL}" -n "${NS}" exec "${pod}" -c redis -- sh -lc 'set -eu; test -f /data/dump.rdb; cat /data/dump.rdb' \
    | zstd -T0 "-${ZSTD_LEVEL}" -q -o "${tmp}"; then
    mv -f "${tmp}" "${out}"
    zstd -tq "${out}"
    if [ "$(zstdcat "${out}" 2>/dev/null | head -c 5)" != "REDIS" ]; then
      log "ERROR: redis rdb header mismatch: ${out}"
      fail=1
    fi
    sha256sum "${out}" > "${out}.sha256"
  else
    log "ERROR: redis backup failed for pod=${pod}"
    rm -f "${tmp}" 2>/dev/null || true
    fail=1
  fi
done < "${tmp_kv}"
rm -f "${tmp_kv}" 2>/dev/null || true

log "Pruning backups older than ${RETENTION_DAYS}d"
find "${ROOT}/postgres" -type f \( -name "*.dump.zst" -o -name "*.sha256" \) -mtime +"${RETENTION_DAYS}" -delete 2>/dev/null || true
find "${ROOT}/redis" -type f \( -name "*.rdb.zst" -o -name "*.sha256" \) -mtime +"${RETENTION_DAYS}" -delete 2>/dev/null || true

log "Tenant backup run finished"

if [ "${fail}" -ne 0 ]; then
  log "ERROR: one or more backups failed"
  exit 1
fi

exit 0
