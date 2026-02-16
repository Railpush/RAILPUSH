# Control Plane (Staging) on `apps.railpush.com`

This directory deploys the RailPush control plane (API + dashboard) to the k3s cluster.

## Prereqs (Already Done)

- ingress-nginx is running HA (2 replicas) on `cpu` + `xeon` and listening on `:80/:443`.
- cert-manager is installed in `cert-manager/` and has:
  - DNS-01 ClusterIssuers (`deploy/k8s/cluster/clusterissuers-dns01-cloudflare.yaml`)
  - wildcard + console Certificates (`deploy/k8s/control-plane/certificates.yaml`)
  - `Secret/cert-manager/cloudflare-api-token-secret` (do not commit; see `deploy/k8s/cluster/cloudflare-api-token-secret.example.yaml`)

## 1) Build + Push the Control Plane Image (from node `cpu`)

We push to the in-cluster registry on `91.98.183.19:5000` (HTTP).

```bash
ssh cpu

# Example location; use whatever directory you synced the repo to.
cd /opt/railpush

sudo docker build -t 91.98.183.19:5000/railpush/control-plane:dev .
sudo docker push 91.98.183.19:5000/railpush/control-plane:dev
```

## 2) Create Secrets (Do Not Commit)

This creates:
- `JWT_SECRET` (required)
- `ENCRYPTION_KEY` (required, >= 32 chars)
- `DB_PASSWORD` + `POSTGRES_PASSWORD` (same value)

```bash
ssh cpu

JWT_SECRET="$(openssl rand -hex 32)"
ENCRYPTION_KEY="$(openssl rand -hex 32)"
DB_PASSWORD="$(openssl rand -hex 24)"

sudo k3s kubectl -n railpush create secret generic railpush-secrets \
  --from-literal=JWT_SECRET="$JWT_SECRET" \
  --from-literal=ENCRYPTION_KEY="$ENCRYPTION_KEY" \
  --from-literal=DB_PASSWORD="$DB_PASSWORD" \
  --from-literal=POSTGRES_PASSWORD="$DB_PASSWORD" \
  --dry-run=client -o yaml | sudo k3s kubectl apply -f -
```

## 3) Apply Manifests

```bash
ssh cpu
cd /opt/railpush

# Production (CNPG cutover): uses `railpush-postgres-cnpg-rw` (TLS required) and does not deploy the legacy Postgres StatefulSet.
sudo k3s kubectl apply -k deploy/k8s/control-plane-overlays/prod-cnpg-cutover

# (Optional) Legacy "fast MVP" path: deploys a single Postgres StatefulSet named `railpush-postgres`.
# sudo k3s kubectl apply -k deploy/k8s/control-plane
sudo k3s kubectl -n railpush rollout status deploy/railpush-control-plane
sudo k3s kubectl -n railpush get pods -o wide
```

Notes:
- `deploy/k8s/control-plane` now deploys two Deployments:
  - `railpush-control-plane` (API + dashboard, `WORKER_ENABLED=false`, default `replicas=2`)
  - `railpush-worker` (deploy/build executor, `WORKER_ENABLED=true`, default `replicas=1`)
- Both pods mount an `emptyDir` at `/var/lib/railpush` (ephemeral). Persistent state lives in Postgres and Kubernetes resources.
- `DEPLOY_DISABLE_ROUTER=true` is set in the ConfigMap to silence legacy Caddy routing while running on Kubernetes.
- Ops pages (like `/incidents`) are gated to platform admins (`users.role=admin`).
  - On self-hosted installs, the **first** created user is auto-promoted to `admin`.
  - To promote an existing user:

```bash
ssh cpu
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
kubectl -n railpush exec railpush-postgres-0 -- psql -U railpush -d railpush -c "UPDATE users SET role='admin' WHERE id='<user-uuid>';"
```

## 4) Verify

Origin test (bypass Cloudflare DNS by pinning host to ingress IP):

```bash
ssh cpu
curl -fsS --resolve apps.railpush.com:443:91.98.183.19 https://apps.railpush.com/healthz
curl -fsS --resolve apps.railpush.com:443:91.98.183.19 https://apps.railpush.com/readyz

# Optional: test the 2nd ingress node directly.
curl -fsS --resolve apps.railpush.com:443:65.21.134.49 https://apps.railpush.com/healthz
curl -fsS --resolve apps.railpush.com:443:65.21.134.49 https://apps.railpush.com/readyz
```

Public test:

```bash
curl -fsS https://apps.railpush.com/healthz
```
