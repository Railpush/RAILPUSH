#!/usr/bin/env bash
set -euo pipefail

SERVICE_ID="${1:-}"
if [ -z "$SERVICE_ID" ]; then
  echo "usage: $0 <service_id> [db_host]" >&2
  exit 2
fi

DB_HOST="${2:-railpush-postgres}"
NS="${NS:-railpush}"
POD="${POD:-railpush-postgres-0}"
export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"

secret="rp-svc-${SERVICE_ID}-env"

old_b64="$(kubectl -n "$NS" get secret "$secret" -o jsonpath='{.data.DATABASE_URL}' 2>/dev/null || true)"
if [ -z "$old_b64" ]; then
  echo "ERROR: missing ${NS}/${secret} or DATABASE_URL key" >&2
  exit 1
fi

old_url="$(printf '%s' "$old_b64" | base64 -d)"
if [ -z "$old_url" ]; then
  echo "ERROR: DATABASE_URL decoded to empty string" >&2
  exit 1
fi

new_url="$(
  URL="$old_url" HOST="$DB_HOST" node -e "
    const u = new URL(process.env.URL);
    u.hostname = process.env.HOST;
    if (!u.port) u.port = '5432';
    process.stdout.write(u.toString());
  "
)"

db_user="$(URL="$new_url" node -e "const u=new URL(process.env.URL); process.stdout.write(decodeURIComponent(u.username||''));")"
db_pass="$(URL="$new_url" node -e "const u=new URL(process.env.URL); process.stdout.write(decodeURIComponent(u.password||''));")"
db_name="$(URL="$new_url" node -e "const u=new URL(process.env.URL); process.stdout.write((u.pathname||'').replace(/^\\//,''));")"

if [ -z "$db_user" ] || [ -z "$db_pass" ] || [ -z "$db_name" ]; then
  echo "ERROR: could not parse DATABASE_URL components (user/pass/db)" >&2
  exit 1
fi

# 1) Ensure role exists.
role_exists="$(
  kubectl -n "$NS" exec "$POD" -- \
    psql -U railpush -d postgres -tAc "SELECT 1 FROM pg_roles WHERE rolname='${db_user}';" \
    | tr -d '[:space:]'
)"
if [ "$role_exists" != "1" ]; then
  kubectl -n "$NS" exec "$POD" -- \
    psql -U railpush -d postgres -v ON_ERROR_STOP=1 \
      -c "CREATE ROLE \"${db_user}\" LOGIN PASSWORD '${db_pass}';"
fi

# 2) Ensure database exists.
db_exists="$(
  kubectl -n "$NS" exec "$POD" -- \
    psql -U railpush -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${db_name}';" \
    | tr -d '[:space:]'
)"
if [ "$db_exists" != "1" ]; then
  kubectl -n "$NS" exec "$POD" -- \
    psql -U railpush -d postgres -v ON_ERROR_STOP=1 \
      -c "CREATE DATABASE \"${db_name}\" OWNER \"${db_user}\";"
fi

# 3) Update RailPush env_vars (source of truth).
key_b64="$(kubectl -n "$NS" get secret railpush-secrets -o jsonpath='{.data.ENCRYPTION_KEY}')"
if [ -z "$key_b64" ]; then
  echo "ERROR: missing railpush-secrets.ENCRYPTION_KEY" >&2
  exit 1
fi

enc="$(
  KEY_B64="$key_b64" PLAINTEXT="$new_url" node -e "
    const crypto = require('crypto');
    const keyStr = Buffer.from(process.env.KEY_B64, 'base64').toString('utf8');
    const key = crypto.createHash('sha256').update(keyStr, 'utf8').digest();
    const nonce = crypto.randomBytes(12);
    const cipher = crypto.createCipheriv('aes-256-gcm', key, nonce);
    const ct = Buffer.concat([cipher.update(process.env.PLAINTEXT, 'utf8'), cipher.final()]);
    const tag = cipher.getAuthTag();
    process.stdout.write(Buffer.concat([nonce, ct, tag]).toString('base64'));
  "
)"

kubectl -n "$NS" exec "$POD" -- \
  psql -U railpush -d railpush -v ON_ERROR_STOP=1 \
    -c "UPDATE env_vars SET encrypted_value='${enc}', is_secret=true WHERE owner_type='service' AND owner_id='${SERVICE_ID}' AND key='DATABASE_URL';" \
    -c "UPDATE env_vars SET is_secret=true WHERE owner_type='service' AND owner_id='${SERVICE_ID}' AND key IN ('JWT_SECRET','DATABASE_URL');"

# 4) Patch the live k8s Secret + restart Deployment.
new_b64="$(printf '%s' "$new_url" | base64 -w0)"
kubectl -n "$NS" patch secret "$secret" --type merge -p "{\"data\":{\"DATABASE_URL\":\"${new_b64}\"}}" >/dev/null

kubectl -n "$NS" rollout restart "deploy/rp-svc-${SERVICE_ID}" >/dev/null
kubectl -n "$NS" rollout status "deploy/rp-svc-${SERVICE_ID}" --timeout=180s >/dev/null

echo "ok: updated DATABASE_URL host to ${DB_HOST} for service ${SERVICE_ID}"
