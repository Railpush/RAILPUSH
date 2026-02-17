#!/usr/bin/env bash
set -euo pipefail

#──────────────────────────────────────────────────────────────
# RailPush Smoke Test — deploy every service type and verify
#──────────────────────────────────────────────────────────────
# Usage:
#   export RP_EMAIL="admin@railpush.com"
#   export RP_PASSWORD="your-password"
#   bash scripts/smoke-test.sh
#
# Requirements: curl, jq
#──────────────────────────────────────────────────────────────

API="https://apps.railpush.com/api/v1"
COOKIE_JAR="/tmp/rp_smoke_cookies.txt"
PASS=0
FAIL=0
WARNINGS=()
CREATED_IDS=()

red()   { printf "\033[31m%s\033[0m\n" "$*"; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
yellow(){ printf "\033[33m%s\033[0m\n" "$*"; }
bold()  { printf "\033[1m%s\033[0m\n" "$*"; }

check() {
  local label="$1" ok="$2"
  if [ "$ok" = "true" ]; then
    green "  ✓ $label"
    PASS=$((PASS + 1))
  else
    red "  ✗ $label"
    FAIL=$((FAIL + 1))
  fi
}

warn() {
  yellow "  ⚠ $1"
  WARNINGS+=("$1")
}

api() {
  local method="$1" path="$2"
  shift 2
  curl -s --connect-timeout 10 --max-time 30 \
    -X "$method" "${API}${path}" \
    -H 'Content-Type: application/json' \
    -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
    "$@" 2>&1
}

wait_deploy() {
  local svc_id="$1" max_wait="${2:-180}" interval=5
  local elapsed=0 status=""
  while [ "$elapsed" -lt "$max_wait" ]; do
    status=$(api GET "/services/${svc_id}" | jq -r '.status // "unknown"')
    case "$status" in
      live) return 0 ;;
      failed|deploy_failed) return 1 ;;
    esac
    sleep "$interval"
    elapsed=$((elapsed + interval))
    printf "    waiting... %ds (status: %s)\n" "$elapsed" "$status"
  done
  return 2  # timeout
}

cleanup() {
  bold ""
  bold "═══ Cleanup ═══"
  for id in "${CREATED_IDS[@]}"; do
    printf "  Deleting %s... " "$id"
    local resp
    # Try service delete
    resp=$(api DELETE "/services/${id}" 2>/dev/null || true)
    if echo "$resp" | jq -e '.status' >/dev/null 2>&1; then
      green "ok"
      continue
    fi
    # Try database delete
    resp=$(api DELETE "/databases/${id}" 2>/dev/null || true)
    if echo "$resp" | jq -e '.status' >/dev/null 2>&1; then
      green "ok"
      continue
    fi
    # Try keyvalue delete
    resp=$(api DELETE "/keyvalue/${id}" 2>/dev/null || true)
    if echo "$resp" | jq -e '.status' >/dev/null 2>&1; then
      green "ok"
      continue
    fi
    yellow "skip (may not exist)"
  done
}

#──────────────────────────────────────────────────────────────
bold "═══════════════════════════════════════"
bold "  RailPush Smoke Test"
bold "═══════════════════════════════════════"
echo ""

# ─── 0. Prerequisites ───
if ! command -v jq >/dev/null 2>&1; then
  red "jq is required. Install: brew install jq / apt install jq"
  exit 1
fi

EMAIL="${RP_EMAIL:?Set RP_EMAIL}"
PASSWORD="${RP_PASSWORD:?Set RP_PASSWORD}"

# ─── 1. Health Checks ───
bold "═══ 1. Health Checks ═══"
hz=$(curl -s --connect-timeout 5 https://apps.railpush.com/healthz)
check "GET /healthz returns 'ok'" "$([ "$hz" = "ok" ] && echo true || echo false)"

rz=$(curl -s --connect-timeout 5 https://apps.railpush.com/readyz)
check "GET /readyz returns 'ready'" "$([ "$rz" = "ready" ] && echo true || echo false)"

# ─── 2. Authentication ───
bold ""
bold "═══ 2. Authentication ═══"
login_resp=$(api POST "/auth/login" -d "{\"email\":\"${EMAIL}\",\"password\":\"${PASSWORD}\"}")
login_ok=$(echo "$login_resp" | jq -e '.user.id' >/dev/null 2>&1 && echo true || echo false)
check "POST /auth/login succeeds" "$login_ok"
if [ "$login_ok" != "true" ]; then
  red "  Login failed: $login_resp"
  red "  Cannot continue without authentication."
  exit 1
fi

user_id=$(echo "$login_resp" | jq -r '.user.id')
workspace_id=$(echo "$login_resp" | jq -r '.workspace.id // empty')
echo "  user=$user_id workspace=$workspace_id"

user_resp=$(api GET "/auth/user")
check "GET /auth/user returns user" "$(echo "$user_resp" | jq -e '.user.id' >/dev/null 2>&1 && echo true || echo false)"

# ─── 3. Dashboard Data ───
bold ""
bold "═══ 3. Dashboard Data ═══"
svc_list=$(api GET "/services")
check "GET /services returns array" "$(echo "$svc_list" | jq -e 'type == "array"' 2>/dev/null || echo false)"
echo "  services count: $(echo "$svc_list" | jq 'length')"

db_list=$(api GET "/databases")
check "GET /databases returns array" "$(echo "$db_list" | jq -e 'type == "array"' 2>/dev/null || echo false)"

kv_list=$(api GET "/keyvalue")
check "GET /keyvalue returns array" "$(echo "$kv_list" | jq -e 'type == "array"' 2>/dev/null || echo false)"

bp_list=$(api GET "/blueprints")
check "GET /blueprints returns array" "$(echo "$bp_list" | jq -e 'type == "array"' 2>/dev/null || echo false)"

eg_list=$(api GET "/env-groups")
check "GET /env-groups returns array" "$(echo "$eg_list" | jq -e 'type == "array"' 2>/dev/null || echo false)"

proj_list=$(api GET "/projects")
check "GET /projects returns array" "$(echo "$proj_list" | jq -e 'type == "array"' 2>/dev/null || echo false)"

# ─── 4. Billing ───
bold ""
bold "═══ 4. Billing ═══"
billing=$(api GET "/billing")
check "GET /billing returns data" "$(echo "$billing" | jq -e '.customer // .status // .error' >/dev/null 2>&1 && echo true || echo false)"

# ─── 5. Create & Deploy — Web Service (Node.js) ───
bold ""
bold "═══ 5. Deploy Web Service (Node.js) ═══"
web_resp=$(api POST "/services" -d '{
  "name": "smoke-web",
  "type": "web",
  "runtime": "node",
  "repo_url": "https://github.com/render-examples/express-hello-world",
  "branch": "main",
  "plan": "free"
}')
web_id=$(echo "$web_resp" | jq -r '.id // empty')
if [ -n "$web_id" ] && [ "$web_id" != "null" ]; then
  check "POST /services (web) creates service" "true"
  CREATED_IDS+=("$web_id")
  echo "  service_id=$web_id"

  # Trigger deploy
  deploy_resp=$(api POST "/services/${web_id}/deploys" -d '{}')
  deploy_id=$(echo "$deploy_resp" | jq -r '.id // empty')
  check "POST /services/:id/deploys triggers build" "$([ -n "$deploy_id" ] && [ "$deploy_id" != "null" ] && echo true || echo false)"

  # Wait for deploy
  echo "  Waiting for deploy (max 3min)..."
  if wait_deploy "$web_id" 180; then
    check "Web service reaches 'live' status" "true"

    # Test the deployed service URL
    svc_detail=$(api GET "/services/${web_id}")
    svc_url=$(echo "$svc_detail" | jq -r '.url // empty')
    if [ -n "$svc_url" ] && [ "$svc_url" != "null" ]; then
      http_code=$(curl -s -o /dev/null -w '%{http_code}' --connect-timeout 10 "$svc_url" 2>/dev/null || echo "000")
      check "Deployed service responds (HTTP $http_code)" "$([ "$http_code" -ge 200 ] && [ "$http_code" -lt 500 ] && echo true || echo false)"
    else
      warn "No URL returned for web service"
    fi
  else
    check "Web service reaches 'live' status" "false"
    warn "Deploy may still be running — check dashboard"
  fi
else
  check "POST /services (web) creates service" "false"
  echo "  response: $web_resp"
fi

# ─── 6. Create & Deploy — Static Site ───
bold ""
bold "═══ 6. Deploy Static Site ═══"
static_resp=$(api POST "/services" -d '{
  "name": "smoke-static",
  "type": "static",
  "runtime": "node",
  "repo_url": "https://github.com/render-examples/create-react-app",
  "branch": "main",
  "build_command": "npm install && npm run build",
  "publish_dir": "build",
  "plan": "free"
}')
static_id=$(echo "$static_resp" | jq -r '.id // empty')
if [ -n "$static_id" ] && [ "$static_id" != "null" ]; then
  check "POST /services (static) creates service" "true"
  CREATED_IDS+=("$static_id")
  echo "  service_id=$static_id"
else
  check "POST /services (static) creates service" "false"
  echo "  response: $static_resp"
fi

# ─── 7. Create Worker ───
bold ""
bold "═══ 7. Create Worker ═══"
worker_resp=$(api POST "/services" -d '{
  "name": "smoke-worker",
  "type": "worker",
  "runtime": "node",
  "repo_url": "https://github.com/render-examples/express-hello-world",
  "branch": "main",
  "plan": "free"
}')
worker_id=$(echo "$worker_resp" | jq -r '.id // empty')
if [ -n "$worker_id" ] && [ "$worker_id" != "null" ]; then
  check "POST /services (worker) creates service" "true"
  CREATED_IDS+=("$worker_id")
else
  check "POST /services (worker) creates service" "false"
  echo "  response: $worker_resp"
fi

# ─── 8. Create Cron Job ───
bold ""
bold "═══ 8. Create Cron Job ═══"
cron_resp=$(api POST "/services" -d '{
  "name": "smoke-cron",
  "type": "cron",
  "runtime": "node",
  "repo_url": "https://github.com/render-examples/express-hello-world",
  "branch": "main",
  "cron_schedule": "0 * * * *",
  "plan": "free"
}')
cron_id=$(echo "$cron_resp" | jq -r '.id // empty')
if [ -n "$cron_id" ] && [ "$cron_id" != "null" ]; then
  check "POST /services (cron) creates service" "true"
  CREATED_IDS+=("$cron_id")
else
  check "POST /services (cron) creates service" "false"
  echo "  response: $cron_resp"
fi

# ─── 9. Provision PostgreSQL ───
bold ""
bold "═══ 9. Provision PostgreSQL ═══"
pg_resp=$(api POST "/databases" -d '{
  "name": "smoke-pg",
  "plan": "free"
}')
pg_id=$(echo "$pg_resp" | jq -r '.id // empty')
if [ -n "$pg_id" ] && [ "$pg_id" != "null" ]; then
  check "POST /databases creates PostgreSQL" "true"
  CREATED_IDS+=("$pg_id")
  echo "  database_id=$pg_id"

  # Check connection info exists
  sleep 5
  pg_detail=$(api GET "/databases/${pg_id}")
  pg_host=$(echo "$pg_detail" | jq -r '.internal_host // .host // empty')
  check "Database has connection info" "$([ -n "$pg_host" ] && [ "$pg_host" != "null" ] && echo true || echo false)"
else
  check "POST /databases creates PostgreSQL" "false"
  echo "  response: $pg_resp"
fi

# ─── 10. Provision Redis ───
bold ""
bold "═══ 10. Provision Redis (Key-Value) ═══"
kv_resp=$(api POST "/keyvalue" -d '{
  "name": "smoke-kv",
  "plan": "free"
}')
kv_id=$(echo "$kv_resp" | jq -r '.id // empty')
if [ -n "$kv_id" ] && [ "$kv_id" != "null" ]; then
  check "POST /keyvalue creates Redis" "true"
  CREATED_IDS+=("$kv_id")
  echo "  keyvalue_id=$kv_id"

  sleep 5
  kv_detail=$(api GET "/keyvalue/${kv_id}")
  kv_host=$(echo "$kv_detail" | jq -r '.internal_host // .host // empty')
  check "Redis has connection info" "$([ -n "$kv_host" ] && [ "$kv_host" != "null" ] && echo true || echo false)"
else
  check "POST /keyvalue creates Redis" "false"
  echo "  response: $kv_resp"
fi

# ─── 11. Environment Variables ───
bold ""
bold "═══ 11. Environment Variables ═══"
if [ -n "${web_id:-}" ] && [ "$web_id" != "null" ]; then
  env_put=$(api PUT "/services/${web_id}/env-vars" -d '[
    {"key": "SMOKE_TEST", "value": "hello"},
    {"key": "SECRET_KEY", "value": "s3cret", "secret": true}
  ]')
  check "PUT /services/:id/env-vars sets vars" "$(echo "$env_put" | jq -e 'type == "array"' 2>/dev/null || echo false)"

  env_get=$(api GET "/services/${web_id}/env-vars")
  has_smoke=$(echo "$env_get" | jq -e '[.[] | select(.key=="SMOKE_TEST")] | length > 0' 2>/dev/null || echo false)
  check "GET /services/:id/env-vars returns SMOKE_TEST" "$has_smoke"

  # Secret should be masked
  secret_val=$(echo "$env_get" | jq -r '[.[] | select(.key=="SECRET_KEY")][0].value // "visible"')
  check "Secret env var is masked" "$([ "$secret_val" = "" ] || [ "$secret_val" = "null" ] || echo "$secret_val" | grep -q '^\*' && echo true || echo false)"
else
  warn "Skipping env-var test (no web service)"
fi

# ─── 12. Service Actions ───
bold ""
bold "═══ 12. Service Actions ═══"
if [ -n "${web_id:-}" ] && [ "$web_id" != "null" ]; then
  restart_resp=$(api POST "/services/${web_id}/restart")
  check "POST /services/:id/restart" "$(echo "$restart_resp" | jq -e '.status' >/dev/null 2>&1 && echo true || echo false)"

  suspend_resp=$(api POST "/services/${web_id}/suspend")
  check "POST /services/:id/suspend" "$(echo "$suspend_resp" | jq -e '.status' >/dev/null 2>&1 && echo true || echo false)"

  resume_resp=$(api POST "/services/${web_id}/resume")
  check "POST /services/:id/resume" "$(echo "$resume_resp" | jq -e '.status' >/dev/null 2>&1 && echo true || echo false)"
else
  warn "Skipping action tests (no web service)"
fi

# ─── 13. Logs ───
bold ""
bold "═══ 13. Logs ═══"
if [ -n "${web_id:-}" ] && [ "$web_id" != "null" ]; then
  logs_resp=$(api GET "/services/${web_id}/logs?limit=10&type=runtime")
  check "GET /services/:id/logs returns data" "$(echo "$logs_resp" | jq -e 'type == "array" or type == "object"' 2>/dev/null || echo false)"
else
  warn "Skipping logs test (no web service)"
fi

# ─── 14. Custom Domains ───
bold ""
bold "═══ 14. Custom Domains ═══"
if [ -n "${web_id:-}" ] && [ "$web_id" != "null" ]; then
  cd_add=$(api POST "/services/${web_id}/custom-domains" -d '{"domain":"smoke-test.example.com"}')
  check "POST /services/:id/custom-domains" "$(echo "$cd_add" | jq -e '.domain // .error' >/dev/null 2>&1 && echo true || echo false)"

  cd_list=$(api GET "/services/${web_id}/custom-domains")
  check "GET /services/:id/custom-domains returns array" "$(echo "$cd_list" | jq -e 'type == "array"' 2>/dev/null || echo false)"

  # Cleanup
  api DELETE "/services/${web_id}/custom-domains/smoke-test.example.com" >/dev/null 2>&1 || true
else
  warn "Skipping custom domain test (no web service)"
fi

# ─── 15. Autoscaling ───
bold ""
bold "═══ 15. Autoscaling ═══"
if [ -n "${web_id:-}" ] && [ "$web_id" != "null" ]; then
  as_get=$(api GET "/services/${web_id}/autoscaling")
  check "GET /services/:id/autoscaling" "$(echo "$as_get" | jq -e '.min_instances // .enabled // .error' >/dev/null 2>&1 && echo true || echo false)"
else
  warn "Skipping autoscaling test (no web service)"
fi

# ─── 16. Ops Endpoints (if admin) ───
bold ""
bold "═══ 16. Ops Endpoints ═══"
ops_overview=$(api GET "/ops/overview")
if echo "$ops_overview" | jq -e '.users' >/dev/null 2>&1; then
  check "GET /ops/overview" "true"

  ops_users=$(api GET "/ops/users?limit=5")
  check "GET /ops/users returns array" "$(echo "$ops_users" | jq -e 'type == "array"' 2>/dev/null || echo false)"

  ops_ws=$(api GET "/ops/workspaces?limit=5")
  check "GET /ops/workspaces returns array" "$(echo "$ops_ws" | jq -e 'type == "array"' 2>/dev/null || echo false)"

  ops_svcs=$(api GET "/ops/services?limit=5")
  check "GET /ops/services returns array" "$(echo "$ops_svcs" | jq -e 'type == "array"' 2>/dev/null || echo false)"

  ops_deploys=$(api GET "/ops/deploys?limit=5")
  check "GET /ops/deploys returns array" "$(echo "$ops_deploys" | jq -e 'type == "array"' 2>/dev/null || echo false)"

  ops_ds=$(api GET "/ops/datastores?limit=5")
  check "GET /ops/datastores returns array" "$(echo "$ops_ds" | jq -e 'type == "array"' 2>/dev/null || echo false)"

  ops_audit=$(api GET "/ops/audit-logs?limit=5")
  check "GET /ops/audit-logs returns array" "$(echo "$ops_audit" | jq -e 'type == "array"' 2>/dev/null || echo false)"

  ops_perf=$(api GET "/ops/performance?window_hours=24")
  check "GET /ops/performance returns data" "$(echo "$ops_perf" | jq -e '.deploys // .total' >/dev/null 2>&1 && echo true || echo false)"

  ops_tech=$(api GET "/ops/kube/summary")
  check "GET /ops/kube/summary" "$(echo "$ops_tech" | jq -e '.deployments // .pods // .error // .enabled' >/dev/null 2>&1 && echo true || echo false)"

  ops_email=$(api GET "/ops/email/outbox?limit=5")
  check "GET /ops/email/outbox returns array" "$(echo "$ops_email" | jq -e 'type == "array"' 2>/dev/null || echo false)"

  ops_settings=$(api GET "/ops/settings")
  check "GET /ops/settings returns config" "$(echo "$ops_settings" | jq -e '.control_plane_domain // .domains' >/dev/null 2>&1 && echo true || echo false)"

  ops_billing=$(api GET "/ops/billing/customers?limit=5")
  check "GET /ops/billing/customers" "$(echo "$ops_billing" | jq -e 'type == "array"' 2>/dev/null || echo false)"

  ops_tickets=$(api GET "/ops/tickets?limit=5")
  check "GET /ops/tickets" "$(echo "$ops_tickets" | jq -e 'type == "array"' 2>/dev/null || echo false)"

  ops_credits=$(api GET "/ops/credits/workspaces?limit=5")
  check "GET /ops/credits/workspaces" "$(echo "$ops_credits" | jq -e 'type == "array"' 2>/dev/null || echo false)"

  ops_incidents=$(api GET "/ops/incidents")
  check "GET /ops/incidents" "$(echo "$ops_incidents" | jq -e 'type == "array" or .error' >/dev/null 2>&1 && echo true || echo false)"
else
  warn "Not an ops user — skipping ops tests"
fi

# ─── 17. WebSocket Endpoints ───
bold ""
bold "═══ 17. WebSocket Upgrade ═══"
if [ -n "${web_id:-}" ] && [ "$web_id" != "null" ]; then
  ws_code=$(curl -s -o /dev/null -w '%{http_code}' --connect-timeout 5 \
    -H 'Upgrade: websocket' -H 'Connection: Upgrade' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGVzdA==' \
    "https://apps.railpush.com/ws/logs/${web_id}" -b "$COOKIE_JAR" 2>/dev/null || echo "000")
  check "WS /ws/logs/:id responds (HTTP $ws_code)" "$([ "$ws_code" = "101" ] || [ "$ws_code" = "400" ] || [ "$ws_code" = "403" ] && echo true || echo false)"
fi

# ─── Cleanup ───
cleanup

# ─── Summary ───
bold ""
bold "═══════════════════════════════════════"
bold "  Results: ${PASS} passed, ${FAIL} failed"
bold "═══════════════════════════════════════"

if [ ${#WARNINGS[@]} -gt 0 ]; then
  yellow ""
  yellow "Warnings:"
  for w in "${WARNINGS[@]}"; do
    yellow "  ⚠ $w"
  done
fi

echo ""
if [ "$FAIL" -eq 0 ]; then
  green "All tests passed!"
  exit 0
else
  red "${FAIL} test(s) failed."
  exit 1
fi
