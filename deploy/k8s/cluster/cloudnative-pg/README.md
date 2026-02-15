# CloudNativePG (CNPG)

This installs the CloudNativePG operator to manage PostgreSQL clusters in Kubernetes.

## Install / Upgrade

Run from `cpu`:

```bash
sudo k3s kubectl apply --server-side --force-conflicts -k /opt/railpush/deploy/k8s/cluster/cloudnative-pg
```

## Verify

```bash
sudo k3s kubectl -n cnpg-system get deploy,pods
sudo k3s kubectl api-resources | grep -i cnpg
```

Notes:
- We use server-side apply because the CNPG CRDs can be too large for client-side apply's
  `kubectl.kubernetes.io/last-applied-configuration` annotation limit.
- The kustomization also creates an empty `cnpg-controller-manager-config` ConfigMap + Secret in
  `cnpg-system` because some upstream release manifests reference them but don't create them.
