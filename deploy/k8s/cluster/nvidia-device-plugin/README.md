# NVIDIA GPU Support (k3s)

This installs the NVIDIA device plugin so the `gpu` node advertises `nvidia.com/gpu` resources to Kubernetes.

## Prereqs (on the GPU node)

- NVIDIA driver installed and `nvidia-smi` works.
- NVIDIA container toolkit installed (`nvidia-container-toolkit`) and containerd has an `nvidia` runtime.

On our cluster, `RuntimeClass/nvidia` already exists; GPU pods should set `runtimeClassName: nvidia`.

## Install

1. Label GPU nodes (run on `cpu`):

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
kubectl label node gpu nvidia.com/gpu.present=true --overwrite
```

2. Apply:

```bash
sudo k3s kubectl apply -k /opt/railpush/deploy/k8s/cluster/nvidia-device-plugin
```

## Verify

```bash
kubectl -n kube-system get ds nvidia-device-plugin-daemonset
kubectl get node gpu -o jsonpath='{.status.allocatable.nvidia\\.com/gpu}'; echo
```

## Test Pod

```bash
kubectl -n railpush apply -f - <<'YAML'
apiVersion: v1
kind: Pod
metadata:
  name: cuda-test
spec:
  runtimeClassName: nvidia
  restartPolicy: Never
  containers:
    - name: cuda
      image: nvidia/cuda:12.4.1-base-ubuntu22.04
      command: ["bash", "-lc", "nvidia-smi"]
      resources:
        limits:
          nvidia.com/gpu: 1
YAML

kubectl -n railpush logs cuda-test
kubectl -n railpush delete pod cuda-test
```

