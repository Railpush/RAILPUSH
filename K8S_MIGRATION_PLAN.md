# RailPush Kubernetes Migration Plan (Self-Hosted, No AWS)

Last updated: 2026-02-15

## Goals (What “Done” Means)

- Control plane (API + dashboard) runs on your self-managed k3s cluster.
- Public console stays on:
  - `https://railpush.com` (apex)
  - `https://www.railpush.com` redirects to apex
- Default service hostnames live on:
  - `https://<service>.apps.railpush.com` (wildcard)
- TLS is automatic via cert-manager.
- No AWS / managed cloud dependencies.
- Can add more servers and scale horizontally without losing deploy jobs.

## Non-Goals (For MVP)

- A single-VIP / active health-checked load balancer (Cloudflare LB, provider failover IP, etc.). We run ingress on 2 nodes, but DNS/LB health checks are still a separate step.
- True multi-tenant isolation (namespaces per workspace, network policies) beyond basic best practices.
- Fully “managed DB/KV as a product” with real HA/backup SLAs (we’ll lay the foundations).

## Current State (Already Completed)

- Servers (SSH aliases):
  - `ssh data` = former production box (post-cutover) now joined to k3s as an **agent** node; legacy services disabled.
  - `ssh cpu` = k3s bootstrap + ingress node, public IP `91.98.183.19`.
  - `ssh Xeon` = k3s server + ingress node, public IP `65.21.134.49`.
  - `ssh gpu` = k3s server node (has nginx on :80; ingress pinned to `cpu`).
- k3s HA cluster is up on `cpu/xeon/gpu` (servers, embedded etcd) + `data` (agent).
- ingress-nginx installed (hostNetwork) running HA (2 replicas) on nodes labeled `ingress-ready=true`:
  - `cpu`
  - `xeon`
  - Note: `cpu` + `xeon` must allow inbound `80/tcp` + `443/tcp` (UFW).
- cert-manager installed.
- Longhorn installed:
  - default StorageClass is `longhorn-r2` (replicas=2)
  - optional StorageClass `longhorn-r3` exists for higher durability volumes (replicas=3)
  - default Longhorn backup target is NFS on `data` (`nfs://142.132.255.45:/var/backups/longhorn`)
- Internal container registry is running on `cpu` at `91.98.183.19:5000` (HTTP, node-only access via UFW).
- Kubernetes runtime (MVP) for customer services is working:
  - supports `image_url` deploys (no git builds yet)
  - creates per-service: `Secret` (env), `Deployment`, `Service`, `Ingress`
  - verified end-to-end with a public image behind `https://<service>.apps.railpush.com`
- Ops email for ACME: `ops@railpush.com`
- DNS provider decision: move `railpush.com` DNS from GoDaddy to Cloudflare.

## Critical Constraints

- Do not break existing production on `data` until explicit cutover.
- Avoid secret leaks in logs/process args.
- Assume service count will grow; avoid designs that require one cert per service hostname.

## Phase 1: Move DNS Hosting to Cloudflare (User-Driven, Blocking For DNS-01)

### 1.1 Create/Import Zone

- [x] Create Cloudflare account (or use existing).
- [x] Add site: `railpush.com`.
- [x] In Cloudflare, review imported DNS records (Cloudflare will try to import).
- [x] Ensure these records exist (values may be adjusted later):
  - [x] `railpush.com` A -> `91.98.183.19` + `65.21.134.49` (k8s ingress on `cpu` + `xeon`, rollback target pre-Phase 7 was `142.132.255.45` on `data`).
  - [x] `www` CNAME -> `railpush.com` (or A record matching current).
  - [x] `apps` A -> `91.98.183.19` + `65.21.134.49` (k8s ingress on `cpu` + `xeon`, DNS only).
  - [x] `*.apps` A -> `91.98.183.19` + `65.21.134.49` (k8s ingress on `cpu` + `xeon`, DNS only).

Important note:
- Your zone currently has `*.railpush.com` -> prod IP (proxied). This will catch `apps.railpush.com` unless an explicit `apps` record exists. We fixed this by creating explicit `apps` + `*.apps` records.

### 1.1.1 Enable Ingress HA At DNS (Multi-A)

Kubernetes ingress is running on **two** public nodes (`cpu` + `xeon`), and DNS should return both public IPs.

- [x] Add a 2nd A record for `apps` -> `65.21.134.49` (k8s ingress on `xeon`). Keep **DNS only**.
- [x] Add a 2nd A record for `*.apps` -> `65.21.134.49` (k8s ingress on `xeon`). Keep **DNS only**.
- [x] (Optional) Add a 2nd A record for `railpush.com` -> `65.21.134.49` as well.
- [x] Verify:

```bash
ssh cpu
dig +short A apps.railpush.com
dig +short A grafana.apps.railpush.com
dig +short A railpush.com | head
```

### 1.1.2 Wildcard `*` Record Strategy [REVIEW]

Cloudflare DNS record `*` matches `<anything>.railpush.com` (it does **not** affect `*.apps.railpush.com`).

- If `*` is set to **DNS only** and points at k8s ingress, random subdomains will fail TLS unless you also provision a wildcard cert for `*.railpush.com` and configure a catch-all Ingress.
- Recommended: delete the `*` record unless you intentionally use `*.railpush.com`.
- Current status to watch for: `curl https://doesnotexist.railpush.com` should not be part of your happy path; if it is, we need to fix wildcard TLS/ingress.

- [x] Deleted Cloudflare `A *` record (`*.railpush.com`) to avoid accidental routing + TLS errors on random subdomains.

### 1.2 Switch Nameservers at GoDaddy

- [x] In Cloudflare: copy the 2 Cloudflare nameservers assigned to the zone.
- [x] In GoDaddy: update `railpush.com` nameservers to Cloudflare’s.
- [x] Wait for propagation (typically minutes to hours; can be up to 24h).
- [x] Verify from a server (recommended):
  - `ssh data "dig +short NS railpush.com"`
  - Expect Cloudflare NS, not `domaincontrol.com`.

### 1.3 Cloudflare Proxy Strategy (Recommendation)

Start DNS-only everywhere to reduce variables, then selectively enable proxy:
- [x] Initially set `apps` and `*.apps` to **DNS only** (grey cloud) until certs + ingress are confirmed.
- [x] After stable:
  - [x] Enable **proxy** (orange cloud) for `railpush.com` and `www` (console only) for WAF/DDoS.
  - [x] Keep `*.apps` DNS-only unless you explicitly want Cloudflare in front of user services.

## Phase 2: Cert-Manager DNS-01 With Cloudflare

### 2.1 Create Cloudflare API Token

- [x] In Cloudflare: create API token with permissions:
  - `Zone:Read`
  - `DNS:Edit`
  - Scope restricted to zone `railpush.com`
- [x] Store token in your password manager.

### 2.2 Create K8s Secret + ClusterIssuers (staging + prod)

On the k3s cluster (via `ssh cpu`):

- [x] Create secret in `cert-manager` namespace:
  - name: `cloudflare-api-token-secret`
  - key: `api-token`
- [x] Create:
  - [x] `ClusterIssuer/letsencrypt-staging` (email `ops@railpush.com`)
  - [x] `ClusterIssuer/letsencrypt-prod` (email `ops@railpush.com`)
  - solver: DNS-01 Cloudflare, using the secret above.

Suggested manifests (apply after you create the secret):

```yaml
# cloudflare-token-secret.yaml (DO NOT COMMIT)
apiVersion: v1
kind: Secret
metadata:
  name: cloudflare-api-token-secret
  namespace: cert-manager
type: Opaque
stringData:
  api-token: "<CLOUDFLARE_API_TOKEN>"
```

```yaml
# clusterissuers.yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-staging
spec:
  acme:
    email: ops@railpush.com
    server: https://acme-staging-v02.api.letsencrypt.org/directory
    privateKeySecretRef:
      name: letsencrypt-staging-account-key
    solvers:
    - dns01:
        cloudflare:
          apiTokenSecretRef:
            name: cloudflare-api-token-secret
            key: api-token
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    email: ops@railpush.com
    server: https://acme-v02.api.letsencrypt.org/directory
    privateKeySecretRef:
      name: letsencrypt-prod-account-key
    solvers:
    - dns01:
        cloudflare:
          apiTokenSecretRef:
            name: cloudflare-api-token-secret
            key: api-token
```

Commands:

```bash
ssh cpu

sudo k3s kubectl apply -f cloudflare-token-secret.yaml
sudo k3s kubectl apply -f clusterissuers.yaml

sudo k3s kubectl get clusterissuer
```

### 2.3 Issue Certificates We Need

- [x] `Certificate/apps-wildcard`:
  - DNS names: `apps.railpush.com`, `*.apps.railpush.com`
  - issuer: `letsencrypt-prod`
  - secret: `apps-wildcard-tls`
- [x] `Certificate/railpush-console` (for later cutover):
  - DNS names: `railpush.com`, `www.railpush.com`
  - issuer: `letsencrypt-prod`
  - secret: `railpush-console-tls`

Suggested manifests:

```yaml
# certificates.yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: apps-wildcard
  namespace: railpush
spec:
  secretName: apps-wildcard-tls
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
  - apps.railpush.com
  - "*.apps.railpush.com"
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: railpush-console
  namespace: railpush
spec:
  secretName: railpush-console-tls
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
  - railpush.com
  - www.railpush.com
```

Commands:

```bash
ssh cpu

sudo k3s kubectl create ns railpush --dry-run=client -o yaml | sudo k3s kubectl apply -f -
sudo k3s kubectl apply -f certificates.yaml

sudo k3s kubectl -n railpush get certificate
sudo k3s kubectl -n railpush describe certificate apps-wildcard
```

### 2.4 Rotation (After You Rotated the Cloudflare Token)

Token rotation is safe as long as you update the Kubernetes secret before renewals are needed.

- [ ] Create a new Cloudflare API token with the same permissions.
- [ ] Update the k8s secret:

```bash
ssh cpu
read -s CF_API_TOKEN
echo
sudo k3s kubectl -n cert-manager create secret generic cloudflare-api-token-secret \
  --from-literal=api-token="$CF_API_TOKEN" \
  --dry-run=client -o yaml | sudo k3s kubectl apply -f -
unset CF_API_TOKEN
```

- [ ] Delete the old Cloudflare API token.

## Phase 3: Deploy Control Plane to Kubernetes (Staging Console on apps.railpush.com)

### 3.1 Ship a Reproducible Kubernetes Deployment (Repo-Tracked)

We implemented a `kubectl apply -k` bundle under:

- [x] `/Users/munir/Desktop/Render/deploy/k8s/control-plane`

It includes:
- [x] Postgres StatefulSet + Service (Longhorn-backed PVC)
- [x] Control plane Deployment + Service + Ingress (`apps.railpush.com`, TLS `apps-wildcard-tls`)
  - stateless (mounts `emptyDir` at `/var/lib/railpush`)
  - safe to scale horizontally (default `replicas=2`)
- [x] Worker Deployment (no Service/Ingress):
  - executes deploys/builds (`WORKER_ENABLED=true`)
  - scales independently of the API (`Deployment/railpush-worker`)
- [x] ConfigMap for non-secret config
- [x] Secret management is done via `kubectl create secret ...` (do not commit secrets)

Chart requirements:
- API listens on `0.0.0.0:8080` in cluster.
- Readiness probe hits `/api/v1/auth/user` or a new `/healthz`.
- Liveness probe hits `/healthz`.

### 3.2 Provide Postgres for Control Plane (Staging)

Two acceptable options:

Option A (fast MVP):
- [x] Postgres StatefulSet + PVC on Longhorn.

Option B (better product):
- [x] CloudNativePG operator, HA cluster (3 instances), proper backups/restore.
  - Operator install (pinned): `/opt/railpush/deploy/k8s/cluster/cloudnative-pg`
  - Postgres cluster overlay: `/opt/railpush/deploy/k8s/control-plane-overlays/prod-cnpg`
  - Cutover overlay (sets `DB_HOST=railpush-postgres-cnpg-rw`, `DB_SSLMODE=require`): `/opt/railpush/deploy/k8s/control-plane-overlays/prod-cnpg-cutover`
  - Backups: `/usr/local/bin/railpush-pg-backup-to-data` auto-selects the CNPG primary pod (falls back to legacy only if CNPG is not installed).

### 3.3 Build + Push Control Plane Image

- [x] Build image on `cpu` (so it can push to the node-only registry allowlist).
- [x] Configure Docker on `cpu` to allow pushing to the HTTP registry (`/etc/docker/daemon.json` insecure-registries).
- [x] Tag and push to internal registry `91.98.183.19:5000`.
- [x] Deploy via `kubectl apply -k`.

Suggested commands (example):

```bash
# Build on your workstation (or any machine that can push to 91.98.183.19:5000)
docker build -t 91.98.183.19:5000/railpush/control-plane:dev .
docker push 91.98.183.19:5000/railpush/control-plane:dev

# Deploy from a node with kubectl (cpu)
ssh cpu
sudo k3s kubectl -n railpush create secret generic railpush-secrets --dry-run=client -o yaml \
  --from-literal=JWT_SECRET="..." \
  --from-literal=ENCRYPTION_KEY="..." \
  --from-literal=GITHUB_CLIENT_ID="..." \
  --from-literal=GITHUB_CLIENT_SECRET="..." \
  --from-literal=DB_PASSWORD="..." \
  | sudo k3s kubectl apply -f -

sudo k3s kubectl -n railpush rollout status deploy/railpush-control-plane
sudo k3s kubectl -n railpush rollout status deploy/railpush-worker
```

### 3.4 Validate Staging Console

- [x] `https://apps.railpush.com` loads dashboard.
- [x] Login/register works (email/password).
- [x] API works through ingress.
- [x] DB migrations ran.

### 3.5 Enable Kubernetes Runtime For Customer Services (MVP: image_url)

This is the first “real” Kubernetes data-plane step.

- [x] Add Kubernetes config env vars to control-plane config:
  - `KUBE_ENABLED=true`
  - `KUBE_NAMESPACE=railpush`
  - `KUBE_INGRESS_CLASS=nginx`
  - `KUBE_TLS_SECRET=apps-wildcard-tls`
- [x] Add RBAC so the control plane can create/update:
  - Deployments, Services, Ingresses, Secrets
- [x] Smoke test (manual):
  - create a service with `type=web`, `runtime=image`, `image_url=nginxdemos/hello:plain-text`, `port=80`
  - trigger a deploy
  - verify it is reachable at `https://<service>.apps.railpush.com`

## Phase 4: Code Changes Required Before Production Cutover (railpush.com + *.apps split)

### 4.1 Split Control Plane Domain vs Deploy Domain

Implement config split:
- [x] Add env `CONTROL_PLANE_DOMAIN` (ex: `railpush.com`).
- [x] Keep `DEPLOY_DOMAIN` for service hostnames (ex: `apps.railpush.com`).
- [x] Update:
  - [x] webhook URL generation to use `CONTROL_PLANE_DOMAIN`
  - [x] CORS allowed origins to include `CONTROL_PLANE_DOMAIN` + `www.`
  - [x] Stripe billing return URLs to use `CONTROL_PLANE_DOMAIN`
  - [x] SAML default ACS URL to use `CONTROL_PLANE_DOMAIN`
  - [x] audit for any remaining “console URLs” using `DEPLOY_DOMAIN`
  - [x] keep service public URLs using `DEPLOY_DOMAIN`

### 4.2 Fix Rate Limiting Client IP Handling (Done in repo)

- [x] Parse RemoteAddr host (not host:port).
- [x] Only trust forwarded headers when direct peer is loopback/private.
- [x] Prefer `X-Real-IP` / `CF-Connecting-IP` when present.
- [x] If you enable Cloudflare proxy (orange cloud), configure **ingress** for real client IPs (recommended):
  - [x] Configure `ingress-nginx` to set real IP from `CF-Connecting-IP` **only** when the direct peer is Cloudflare.
  - [x] Verify `ingress-nginx` access logs show the real client IP (not Cloudflare IPs).
  - [x] Leave `TRUSTED_PROXY_CIDRS` **unset** for the API in this topology (the API’s direct peer is `ingress-nginx`, not Cloudflare).
    - Optional hardening later: set `TRUSTED_PROXY_CIDRS` to your cluster/ingress node CIDR(s) instead of relying on “any private IP”.
  - [x] API middleware should prefer `X-Real-IP` (set by ingress) before Cloudflare-specific headers.

### 4.3 Ensure Auto-Deploy Works Without Scanning Whole DB

- [x] Add indexed query for `(repo_url, branch, auto_deploy)` instead of listing all services in webhook.

## Phase 5: Cutover Console From data -> Kubernetes (railpush.com)

### 5.1 Pre-Cutover Checklist

- [x] `apps.railpush.com` staging is stable for 24h (recommended for future cutovers; current cutover happened earlier).
- [x] cert-manager has issued `railpush-console-tls`.
- [x] Kubernetes Ingress exists for `railpush.com` + `www.railpush.com`:
  - Ingress: `railpush-control-plane-console`
  - TLS secret: `railpush-console-tls`
- [x] You have a rollback plan:
  - (pre-Phase 7) set Cloudflare `railpush.com` A record back to `data` IP (`142.132.255.45`)
  - Note: after Phase 7, `data` was repurposed into a k3s agent and legacy console services were disabled.

### 5.2 DB Migration Plan (Control Plane DB)

Choose one:

Option A (fastest):
- [x] Keep using the existing Postgres on `data` temporarily. (skipped; we migrated into k8s Postgres)
- [x] Expose DB securely to cluster (VPN or allowlist). (skipped; we migrated into k8s Postgres)

Option B (best long-term):
- [x] Dump/restore Postgres from `data` into k8s Postgres.
- [x] Validate integrity.
- [x] Freeze writes during cutover window.

### 5.3 DNS Switch

- [x] In Cloudflare DNS:
  - [x] Update `railpush.com` A record -> `91.98.183.19` (k8s ingress on `cpu`)
  - [x] Keep `www` as CNAME -> `railpush.com` (recommended)
- [x] Validate from a server:
  - `ssh cpu "dig +short A railpush.com"`
  - expect `91.98.183.19`
- [x] Validate HTTPS to k8s ingress directly (no DNS dependence):
  - `ssh cpu "curl -I --resolve railpush.com:443:91.98.183.19 https://railpush.com/ | head"`

### 5.4 GitHub OAuth Cutover (Required If You Use GitHub Login)

Because RailPush uses host-only session cookies, your GitHub OAuth callback **must** be the same hostname users log in on.

- [x] In GitHub OAuth App settings:
  - [x] Set callback URL to: `https://railpush.com/api/v1/auth/github/callback`
- [x] Ensure Kubernetes has the GitHub OAuth secrets:
  - [x] `GITHUB_CLIENT_ID`
  - [x] `GITHUB_CLIENT_SECRET`
  - [x] Quick verification:

```bash
ssh cpu
curl -sS -D - -o /dev/null https://railpush.com/api/v1/auth/github | sed -n '1,30p'
# Expect a 307/302 Location header to github.com with client_id=... (not empty)
```
- [x] In Kubernetes config:
  - [x] Update `CONTROL_PLANE_DOMAIN=railpush.com`
  - [x] Update `GITHUB_CALLBACK_URL=https://railpush.com/api/v1/auth/github/callback`
  - [x] Apply + restart:

```bash
ssh cpu
sudo k3s kubectl apply -k /opt/railpush/deploy/k8s/control-plane-overlays/prod
sudo k3s kubectl -n railpush rollout restart deploy/railpush-control-plane
sudo k3s kubectl -n railpush rollout status deploy/railpush-control-plane
```

### 5.5 Rollback

- [ ] (Rollback only; pre-Phase 7) In Cloudflare DNS:
  - [ ] Set `railpush.com` A record back to the `data` IP (currently `142.132.255.45`) and re-enable legacy console services on `data`.

## Phase 6: “Real Kubernetes Runtime” For Customer Services (The Big Migration)

This replaces Docker+Caddy orchestration with Kubernetes-native build + deploy.

### 6.1 Durable Deploy Queue + Separate Workers

- [x] Replace in-memory queue with DB-backed leasing:
  - deploys stored in Postgres with lease fields (`lease_owner`, `lease_expires_at`, `attempts`).
  - worker polls and claims work via `FOR UPDATE SKIP LOCKED` (crash-safe + horizontally scalable).
  - optional knobs: `WORKER_CONCURRENCY`, `WORKER_POLL_INTERVAL_MS`, `WORKER_LEASE_SECONDS`, `WORKER_MAX_ATTEMPTS`.
- [x] Run worker as separate Deployment (scale it independently).
  - `Deployment/railpush-control-plane` runs with `WORKER_ENABLED=false` (API only).
  - `Deployment/railpush-worker` runs with `WORKER_ENABLED=true` (executes deploys/builds).
- [x] Cron deploys execute via the same durable mechanism (scheduler only inserts deploy rows; worker picks them up).

### 6.2 Build System (No Docker Socket)

- [x] Build jobs as Kubernetes Jobs using Kaniko:
  - clones repo in an init container (GitHub token stored in a short-lived Secret)
  - builds via repo Dockerfile when present
  - for `static` runtime, RailPush can auto-generate a Dockerfile when missing (Kaniko-compatible):
    - Stage 1: run `svc.BuildCommand` in a Node builder image
    - Stage 2: copy `svc.StaticPublishPath` into `nginx:alpine`
    - Writes an SPA-friendly nginx config (`try_files ... /index.html`) and listens on `svc.Port`
  - pushes to internal registry prefix `DOCKER_REGISTRY` (currently `91.98.183.19:5000/railpush`)
- [x] Stream build logs to a log store (Loki) and keep only metadata in Postgres.

### 6.3 Deploy System

- [x] For `web/static/pserv` (image-based deploys):
  - Deployment + Service + Ingress (host `<svc>.apps.railpush.com`)
  - Health probes:
    - default: TCP probes on the container port (most compatible; avoids HTTP->HTTPS redirect issues)
    - if `health_check_path` is set: HTTP GET probes with `X-Forwarded-Proto: https`
- [x] For `worker`:
  - Deployment only (no Service/Ingress)
  - Optional: `start_command` is applied via `sh -lc ...` (when set)
- [x] For `cron`:
  - CronJob (no in-process scheduler execution when `KUBE_ENABLED=true`)
- [x] For one-off commands:
  - Job (instead of `docker exec`)
- [x] Scaling (MVP):
  - map `svc.Instances` -> Deployment replicas (applies immediately on service update; no deploy required)
  - later add HPA/KEDA

### 6.4 Routing + TLS

- [x] Default hostnames:
  - wildcard cert `apps-wildcard-tls` reused for `*.apps.railpush.com`
  - default host label comes from `services.subdomain` (not `services.name`)
    - `subdomain` is auto-generated from the name and made globally unique (auto-suffixes like `-2` when needed)
    - reserved labels (cannot be claimed by customer services): `grafana`, `prometheus`, `alertmanager`, `loki`
- [x] Custom domains:
  - Ingress per domain (host = custom domain)
  - per-domain TLS via cert-manager ingress-shim + **HTTP-01** ClusterIssuer
    - issuer: `letsencrypt-http01-prod` (created from `/deploy/k8s/cluster/clusterissuers-http01.yaml`)
    - config: `KUBE_CUSTOM_DOMAIN_ISSUER=letsencrypt-http01-prod`

Notes:
- Customer-owned domains are not in your Cloudflare zone, so DNS-01 via your Cloudflare token cannot work for them.
- For custom domains, the user must point their domain to your ingress IP (or a CNAME to your platform hostname) so HTTP-01 can validate.

### 6.5 Observability + Ops

- [x] Prometheus + Grafana (kube-prometheus-stack)
  - Namespace: `monitoring`
  - Release: `monitoring` (Helm)
  - Grafana URL: `https://grafana.apps.railpush.com`
    - Ingress TLS is terminated with a dedicated cert in `monitoring`:
      - Secret: `monitoring/grafana-tls`
      - Issuance: cert-manager via HTTP-01 ClusterIssuer `letsencrypt-http01-prod` (set by Grafana ingress annotation in Helm values)
  - Grafana admin creds live in secret: `monitoring/monitoring-grafana-admin`
    - Get password:

```bash
ssh cpu
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
kubectl -n monitoring get secret monitoring-grafana-admin -o jsonpath='{.data.admin-password}' | base64 -d; echo
```

  - Node exporter fix (required on this cluster): disable `hostNetwork` to avoid port `9100` conflicts on some nodes.
    - Implemented via Helm values: `prometheus-node-exporter.hostNetwork=false` and `hostPID=false`
  - Extra scraping enabled (ServiceMonitors):
    - ingress-nginx controller metrics (`/metrics` on port `10254`)
    - cert-manager metrics
    - Longhorn manager metrics
- [x] Loki for logs (MVP):
  - Installed in `logging` namespace (Loki SingleBinary + Promtail DaemonSet).
  - Grafana datasource `Loki` is provisioned automatically.
  - Files:
    - `/opt/railpush/deploy/k8s/logging/loki-values.yaml`
    - `/opt/railpush/deploy/k8s/logging/promtail-values.yaml`
- [x] Runtime logs (basic): API can read `kubectl logs` for service pods (non-streaming, tail only)
- [x] Alerts delivery (self-hosted webhook):
  - Alertmanager routes to RailPush webhook receiver:
    - URL: `/api/v1/webhooks/alertmanager`
    - Auth: `Authorization: Bearer <token>` (mounted from `monitoring/alertmanager-webhook-token`)
  - RailPush stores deliveries in Postgres table `alert_events` (for future UI/ops workflows).
  - Note: This is intentionally self-hosted (no external notifications yet). Next step is still to add an external channel (Slack/Email/Pager) if you want out-of-cluster paging.
- [x] Dashboard incidents page (MVP):
  - Route: `/incidents`
  - Uses `alert_events` to show Active (firing) + Resolved incidents and a per-incident timeline.
  - Supports internal **Acknowledge** (RailPush-only) and **Silence** (creates Alertmanager v2 silence).
    - Alertmanager API base URL is `ALERTMANAGER_URL` (defaults to `http://monitoring-kube-prometheus-alertmanager.monitoring:9093`).
- [ ] External notifications (optional):
  - Slack (recommended): add `monitoring/alertmanager-slack-webhook` secret and apply Helm overlay values:
    - `/opt/railpush/deploy/k8s/monitoring/values-slack.yaml`
  - SMTP email: see `/opt/railpush/deploy/k8s/monitoring/README.md`
  - Add product-specific alert rules later:
    - `railpush-control-plane` Deployment not ready
    - `railpush-postgres` down / high restart rate
    - ingress-nginx 5xx spike for `railpush.com` and `*.apps.railpush.com`
    - cert expiry (< 14d)
    - registry disk usage high

### 6.6 Managed Databases/Key-Value (Customer Runtime Dependencies)

Goal: users can deploy apps that expect a `DATABASE_URL` / redis host inside the cluster, without needing external managed services.

- [x] Managed Postgres (MVP) runs as k8s primitives:
  - Creates (per DB): `Secret`, `Service` (`sr-db-<idPrefix>`), headless `Service` (`sr-db-<idPrefix>-headless`), `StatefulSet`, PVC.
  - Compatibility hostname is preserved: apps connect to `sr-db-<idPrefix>:5432`.
  - Postgres uses `PGDATA=/var/lib/postgresql/data/pgdata` (avoids `lost+found` initdb failure on mounted volumes).
- [x] Managed Redis (MVP) runs as k8s primitives:
  - Creates (per KV): `Secret`, `Service` (`sr-kv-<idPrefix>`), headless `Service`, `StatefulSet`, PVC.
  - Apps connect to `sr-kv-<idPrefix>:6379`.
- [x] Validate delete flows:
  - deleting a managed DB/KV in RailPush deletes the associated k8s resources (StatefulSet/Services/Secret/PVCs).

## Phase 7: Add the 4th Server (`data`) Into the Cluster (After Cutover)

- [x] Drain/stop legacy services on `data`.
  - Disabled/stopped: `railpush-api`, `caddy`, `postgresql`, `redis-server` (and any legacy `sr-*` docker containers).
- [x] Join `data` to k3s as an **agent** node (not a server/control-plane node).
- [x] Ensure pods on agent nodes can reach the Kubernetes API:
  - Because `default/kubernetes` service endpoints are the k3s server public IPs (`:6443`), pods will connect from the Pod CIDR (k3s default: `10.42.0.0/16`).
  - UFW must allow `6443/tcp` from `10.42.0.0/16` on all k3s server nodes (`cpu`, `xeon`, `gpu`) or in-cluster controllers will timeout to `https://10.43.0.1:443`.
  - Example (run on each server node):

```bash
sudo ufw allow from 10.42.0.0/16 to any port 6443 proto tcp
```
- [x] Move stateful storage planning:
  - Longhorn:
    - disk layout: keep default `/var/lib/longhorn` (all nodes have sufficient capacity)
    - replicas: keep default `longhorn-r2` (2 replicas); add `longhorn-r3` (3 replicas) for critical PVs if/when needed
    - backup strategy: NFS backup target on `data` (`nfs://142.132.255.45:/var/backups/longhorn`)
  - k3s default StorageClass:
    - ensure only `longhorn-r2` is default (remove default from `local-path`)

## Ops Checklist (Ongoing)

- [x] Daily k8s Postgres backups to `data` (separate host; `data` is a k3s agent node post-Phase 7):
  - Runs on `cpu` via `systemd` timer: `railpush-pg-backup.timer`
  - Script: `/usr/local/bin/railpush-pg-backup-to-data`
  - Local staging dir on `cpu`: `/var/backups/railpush/postgres` (keeps 7 days)
  - Remote backup dir on `data`: `/var/backups/railpush/postgres` (keeps 30 days)
  - Verify:

```bash
ssh cpu
systemctl list-timers --all | grep -F railpush-pg-backup
journalctl -u railpush-pg-backup.service -n 120 --no-pager
```

```bash
ssh data
ls -la /var/backups/railpush/postgres | tail -n 20
cd /var/backups/railpush/postgres
sha256sum -c *.sha256 | tail
```

- [x] Restore drill (completed 2026-02-13):
  - pick a dump on `data`
  - restore into a temporary DB in k8s Postgres
  - validate a few key tables + app login

Example restore drill (safe: restores into a new DB, does not touch prod DB):

```bash
ssh cpu

# If you're using CloudNativePG (CNPG) for the control-plane DB:
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
PRIMARY_POD="$(kubectl -n railpush get pod \
  -l cnpg.io/cluster=railpush-postgres-cnpg,cnpg.io/instanceRole=primary \
  -o jsonpath='{.items[0].metadata.name}')"

# Pick the dump you want to test.
ssh -i /root/.ssh/railpush_backup_to_data_ed25519 railpush-backup@142.132.255.45 \
  'ls -1 /var/backups/railpush/postgres/railpush_*.dump.zst | tail -n 5'

FILE="railpush_YYYYMMDDTHHMMSSZ.dump.zst"

# Recreate a test DB.
kubectl -n railpush exec "$PRIMARY_POD" -- psql -U postgres -d railpush -v ON_ERROR_STOP=1 \
  -c "DROP DATABASE IF EXISTS railpush_restore_test;" \
  -c "CREATE DATABASE railpush_restore_test OWNER railpush;"

# Stream the dump from data -> cpu -> k8s Postgres and restore.
ssh -i /root/.ssh/railpush_backup_to_data_ed25519 -o BatchMode=yes -o StrictHostKeyChecking=yes \
  railpush-backup@142.132.255.45 "cat /var/backups/railpush/postgres/$FILE" \
  | zstd -dc \
  | kubectl -n railpush exec -i "$PRIMARY_POD" -- pg_restore \
      -U postgres -d railpush_restore_test \
      --no-owner --no-privileges --exit-on-error

# Sanity check (list first 30 tables).
kubectl -n railpush exec "$PRIMARY_POD" -- psql -U postgres -d railpush_restore_test -v ON_ERROR_STOP=1 \
  -c "SELECT table_name FROM information_schema.tables WHERE table_schema='public' ORDER BY table_name LIMIT 30;"
```

- [x] In Cloudflare: set `SSL/TLS` mode to **Full (strict)** once Let’s Encrypt certs are live.
- [x] In Cloudflare: enable `Always Use HTTPS` for console zones (optional).
- [x] Decide whether to firewall :80/:443 on ingress to Cloudflare IP ranges (only if proxying through Cloudflare).
  - Decision: do **not** restrict at this time because `apps` / `*.apps` are DNS-only and custom domains may point directly to ingress. Revisit only if you proxy all user traffic through Cloudflare or split console ingress onto separate IPs.
- [x] Add basic alerting (at minimum: node disk pressure + ingress down + cert renew failures).
- [x] If ingress-nginx runs `hostNetwork: true`, allow admission webhook port `8443/tcp` from all k3s nodes to all ingress-nginx nodes (otherwise Ingress creation can fail with webhook timeouts).
  - Symptom: `failed calling webhook "validate.nginx.ingress.kubernetes.io" ... context deadline exceeded`
  - Example (UFW): on each node that runs `ingress-nginx-controller` (currently `cpu` + `xeon`):

```bash
# Allow from other k3s server IPs (adjust as your node list changes).
sudo ufw allow from 91.98.183.19 to any port 8443 proto tcp
sudo ufw allow from 65.21.134.49 to any port 8443 proto tcp
sudo ufw allow from 5.9.147.137 to any port 8443 proto tcp
```

## Stripe Billing (Production Setup)

Stripe webhooks will fail until RailPush is configured with live keys and webhook signing secret.

### Status (K8s)

- [x] Stripe keys are configured in `railpush/railpush-secrets` (`STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`).
- [x] Stripe price IDs are configured in `railpush/railpush-config` (`STRIPE_PRICE_STARTER`, `STRIPE_PRICE_STANDARD`, `STRIPE_PRICE_PRO`).
- [x] Verify Stripe Dashboard webhook delivery is green for `https://railpush.com/api/v1/webhooks/stripe` (no WAF/bot blocks, no signature errors).

### 1) Create Products + Prices (Live Mode)

In Stripe Dashboard (Live mode):
- Create 3 recurring monthly prices (or reuse existing):
  - Starter
  - Standard
  - Pro

Record the `price_...` IDs.

### 2) Configure RailPush Stripe Env Vars (k3s)

Run on `cpu` (do not paste secrets into chat; use `read -s` prompts):

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

read -s STRIPE_SECRET_KEY; echo
read -s STRIPE_WEBHOOK_SECRET; echo

STRIPE_SECRET_KEY_B64="$(printf %s "$STRIPE_SECRET_KEY" | base64 | tr -d '\n')"
STRIPE_WEBHOOK_SECRET_B64="$(printf %s "$STRIPE_WEBHOOK_SECRET" | base64 | tr -d '\n')"

kubectl -n railpush patch secret railpush-secrets -p "{\"data\":{\"STRIPE_SECRET_KEY\":\"$STRIPE_SECRET_KEY_B64\",\"STRIPE_WEBHOOK_SECRET\":\"$STRIPE_WEBHOOK_SECRET_B64\"}}"

unset STRIPE_SECRET_KEY STRIPE_WEBHOOK_SECRET STRIPE_SECRET_KEY_B64 STRIPE_WEBHOOK_SECRET_B64

# Non-secret IDs can live in the configmap (or keep them in the secret if you prefer).
kubectl -n railpush patch configmap railpush-config --type merge -p "$(cat <<'JSON'
{
  "data": {
    "STRIPE_PRICE_STARTER": "price_...",
    "STRIPE_PRICE_STANDARD": "price_...",
    "STRIPE_PRICE_PRO": "price_..."
  }
}
JSON
)"

kubectl -n railpush rollout restart deploy/railpush-control-plane deploy/railpush-worker
kubectl -n railpush rollout status deploy/railpush-control-plane --timeout=180s
kubectl -n railpush rollout status deploy/railpush-worker --timeout=180s
```

### 3) Stripe Webhook Endpoint

In Stripe Dashboard:
- Webhook endpoint URL: `https://railpush.com/api/v1/webhooks/stripe`
- Events to send:
  - `checkout.session.completed`
  - `customer.subscription.created`
  - `customer.subscription.updated`
  - `customer.subscription.deleted`
  - `invoice.payment_succeeded`
  - `invoice.payment_failed`
- Copy the endpoint **Signing secret** (`whsec_...`) into `STRIPE_WEBHOOK_SECRET` above.

### 4) Cloudflare: Avoid Bot/WAF Blocking Webhooks (Recommended)

If `railpush.com` is proxied through Cloudflare, add a WAF rule to **Skip** security features for:
- URI Path starts with `/api/v1/webhooks/stripe`

Otherwise Stripe may see challenges/blocks and mark the endpoint as failing.

## Smoke Tests (Completed)

- [x] Custom domain ingress + per-domain cert issuance works (HTTP-01):
  - created a custom-domain ingress for `cdtest.apps.railpush.com`
  - cert-manager issued a per-domain cert and secret became `READY=True`
- [x] Cron services deploy as Kubernetes CronJobs and execute on schedule.
- [x] One-off service commands run as Kubernetes Jobs (store logs + exit code).
