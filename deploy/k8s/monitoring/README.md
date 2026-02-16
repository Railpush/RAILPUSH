# Monitoring (Prometheus + Grafana + Alertmanager)

This installs `kube-prometheus-stack` on the k3s cluster and exposes Grafana.

Notes:
- Our `values.yaml` disables node-exporter `hostNetwork` to avoid port `9100` conflicts on hosts that already run a node exporter.
- Alert delivery is configured as a **self-hosted webhook** into RailPush, with optional **Slack** delivery for `severity=critical`.
- Grafana uses a dedicated TLS secret `monitoring/grafana-tls` issued by cert-manager via HTTP-01 (`letsencrypt-http01-prod`).

## Install / Upgrade

On `cpu`:

0. Ensure kubectl/helm talk to k3s:

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
```

1. Create namespace + Grafana admin secret (password printed once; store it).

```bash
kubectl create ns monitoring --dry-run=client -o yaml | kubectl apply -f -

ADMIN_PASS="$(openssl rand -base64 32)"
kubectl -n monitoring create secret generic monitoring-grafana-admin \
  --from-literal=admin-user=admin \
  --from-literal=admin-password="$ADMIN_PASS" \
  --dry-run=client -o yaml | kubectl apply -f -
unset ADMIN_PASS
```

2. Install chart:

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

helm upgrade --install monitoring prometheus-community/kube-prometheus-stack \
  -n monitoring --create-namespace \
  -f /opt/railpush/deploy/k8s/monitoring/values.yaml
```

`values.yaml` now provisions a baseline Grafana dashboard (`RailPush Platform Overview`) as code, so new clusters get it automatically on install/upgrade.

3. Apply extra ServiceMonitors (ingress-nginx, cert-manager, longhorn):

```bash
kubectl apply -f /opt/railpush/deploy/k8s/monitoring/extra-servicemonitors.yaml
```

4. Apply RailPush platform alert rules (basic: ingress down, Postgres down, cert issues):

```bash
kubectl apply -f /opt/railpush/deploy/k8s/monitoring/railpush-alert-rules.yaml
```

## Enable Alerts (Self-Hosted Webhook) (Recommended)

This routes Alertmanager -> RailPush API and stores each delivery in Postgres (`alert_events` table).

1. Create the Alertmanager bearer token secret (key must be `token`):

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

TOKEN="$(openssl rand -hex 32)"
kubectl -n monitoring create secret generic alertmanager-webhook-token \
  --from-literal=token="$TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -
unset TOKEN
```

2. Add the same token to `railpush-secrets` as `ALERT_WEBHOOK_TOKEN`, then restart the control-plane:

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

TOKEN="$(kubectl -n monitoring get secret alertmanager-webhook-token -o jsonpath='{.data.token}' | base64 -d)"
TOKEN_B64="$(printf %s "$TOKEN" | base64 | tr -d '\n')"
kubectl -n railpush patch secret railpush-secrets -p "{\"data\":{\"ALERT_WEBHOOK_TOKEN\":\"$TOKEN_B64\"}}"
unset TOKEN TOKEN_B64

kubectl -n railpush rollout restart deploy/railpush-control-plane
kubectl -n railpush rollout status deploy/railpush-control-plane
```

3. Apply:

```bash
helm upgrade --install monitoring prometheus-community/kube-prometheus-stack \
  -n monitoring --create-namespace \
  -f /opt/railpush/deploy/k8s/monitoring/values.yaml
```

4. Send a test alert:

```bash
kubectl -n monitoring run -i --rm --restart=Never alert-test \
  --image=curlimages/curl:8.5.0 \
  -- curl -sS -XPOST -H 'Content-Type: application/json' \
  http://monitoring-kube-prometheus-alertmanager.monitoring.svc:9093/api/v2/alerts \
  -d '[{"labels":{"alertname":"RailPushTest","severity":"warning"},"annotations":{"summary":"Test alert"},"startsAt":"2026-02-13T00:00:00Z"}]'
```

## Enable Alerts (Email via SMTP) (Optional)

1. Update `/opt/railpush/deploy/k8s/monitoring/values.yaml`:

- set `alertmanager.config.global.smtp_*`
- add an `ops-email` (or similar) receiver with `email_configs`
- set `alertmanager.config.route.receiver` to that receiver (instead of `railpush-webhook`)

2. Create the SMTP password secret (key must be `password`):

```bash
read -s SMTP_PASS
echo

kubectl -n monitoring create secret generic alertmanager-smtp \
  --from-literal=password="$SMTP_PASS" \
  --dry-run=client -o yaml | kubectl apply -f -

unset SMTP_PASS
```

3. Apply:

```bash
helm upgrade --install monitoring prometheus-community/kube-prometheus-stack \
  -n monitoring --create-namespace \
  -f /opt/railpush/deploy/k8s/monitoring/values.yaml
```

## Enable External Notifications (Slack) (Optional)

This uses an optional Helm values overlay (`values-slack.yaml`) to send **critical** alerts to Slack
in addition to delivering everything to RailPush.

1. Create the Slack webhook secret (key must be `url`):

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

read -s SLACK_WEBHOOK_URL
echo

kubectl -n monitoring create secret generic alertmanager-slack-webhook \
  --from-literal=url="$SLACK_WEBHOOK_URL" \
  --dry-run=client -o yaml | kubectl apply -f -

unset SLACK_WEBHOOK_URL
```

2. Apply (or re-apply) the chart:

```bash
helm upgrade --install monitoring prometheus-community/kube-prometheus-stack \
  -n monitoring --create-namespace \
  -f /opt/railpush/deploy/k8s/monitoring/values.yaml \
  -f /opt/railpush/deploy/k8s/monitoring/values-slack.yaml
```

3. Send a critical drill alert (this should show up in RailPush and also post to Slack):

```bash
kubectl apply -f /opt/railpush/deploy/k8s/monitoring/railpush-alert-drill-critical.yaml
```

Delete the drill once verified:

```bash
kubectl delete -f /opt/railpush/deploy/k8s/monitoring/railpush-alert-drill-critical.yaml
```

## Access Grafana

- URL: `https://grafana.apps.railpush.com`
- Username: `admin`
- Password:

```bash
kubectl -n monitoring get secret monitoring-grafana-admin -o jsonpath='{.data.admin-password}' | base64 -d
echo
```

## Quick Health Checks

```bash
kubectl -n monitoring get pods
kubectl -n monitoring get prometheus,alertmanager
kubectl -n monitoring get ingress
```
