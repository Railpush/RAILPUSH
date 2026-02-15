# Logging (Loki + Promtail)

This installs Loki (log storage/query) and Promtail (log shipping from k8s nodes) on the k3s cluster.

Design:
- Loki runs in `logging` namespace in **SingleBinary** mode with Longhorn-backed persistence.
- Promtail runs as a DaemonSet and ships container logs to Loki.
- Loki is **not** exposed publicly (ClusterIP only). Grafana accesses it inside the cluster.

## Install / Upgrade

On `cpu`:

0. Ensure kubectl/helm talk to k3s:

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
```

1. Add Grafana Helm repo (once):

```bash
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update
```

2. Install Loki:

```bash
kubectl create ns logging --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install loki grafana/loki \
  -n logging --create-namespace \
  -f /opt/railpush/deploy/k8s/logging/loki-values.yaml
```

3. Install Promtail:

```bash
helm upgrade --install promtail grafana/promtail \
  -n logging --create-namespace \
  -f /opt/railpush/deploy/k8s/logging/promtail-values.yaml
```

4. Verify:

```bash
kubectl -n logging get pods
kubectl -n logging get svc | grep -i loki
kubectl -n logging logs deploy/loki-gateway --tail=50
kubectl -n logging logs ds/promtail --tail=50
```

## Add Loki Datasource In Grafana

We provision it via `kube-prometheus-stack` values.

- Datasource name: `Loki`
- URL: `http://loki-gateway.logging.svc.cluster.local`

After updating `/opt/railpush/deploy/k8s/monitoring/values.yaml`, run:

```bash
helm upgrade --install monitoring prometheus-community/kube-prometheus-stack \
  -n monitoring --create-namespace \
  -f /opt/railpush/deploy/k8s/monitoring/values.yaml
```

