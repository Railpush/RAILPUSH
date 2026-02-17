# Kubernetes Implementation Audit

**Date:** 2026-02-15
**Scope:** Full code review of RailPush K8s integration

---

## Critical — Fix Immediately

### 1. ~~Command Injection — Redis `maxmemory-policy`~~ — RESOLVED

**Status:** Fixed with defense in depth across two layers:

- **Allowlist validation** in `api/services/redis_policies.go` — `NormalizeRedisMaxmemoryPolicy()` validates against 8 known Redis policies; unknown values rejected.
- **API-level rejection** in `api/handlers/keyvalue.go:72-77` — invalid policies return `400 Bad Request` before reaching the deployer.
- **Shell wrapper eliminated** in `kube_managed_resources.go:488-494` — replaced `sh -c` + `fmt.Sprintf` with direct `Args` array elements. No shell interpretation, no injection possible even if validation were bypassed.

---

### 2. ~~Command Injection — `pg_isready` Health Probes~~ — RESOLVED

**Status:** Fixed in `api/services/kube_managed_resources.go:285,294`

Shell wrapper eliminated. Both readiness and liveness probes now use direct argument passing:
```go
Command: []string{"pg_isready", "-U", user, "-d", dbName}
```
No `sh -lc`, no `fmt.Sprintf` interpolation. Arguments pass directly to `execve` with no shell parsing. Existing managed databases remediated on the cluster.

---

### 3. ~~Plan Field Is Self-Declared — No Server-Side Validation or Payment Gate~~ — RESOLVED

**Status:** Fixed across all handlers via `api/services/plans.go`

**Allowlist validation** — `NormalizePlan()` validates against `free/starter/standard/pro`, rejects unknown values with 400:
- `handlers/services.go:174` (create), `:338` (update)
- `handlers/databases.go:72` (create)
- `handlers/keyvalue.go:63` (create)
- `handlers/blueprints.go:638,708,829,898` (blueprint sync)

**Stripe payment gate** — plan changes now require Stripe success **before** DB persistence (`services.go:426-455`):
- Paid upgrades: `AddSubscriptionItem` must succeed or returns `502 Bad Gateway`
- No payment method: returns `402 Payment Required`
- Downgrades to free: `RemoveSubscriptionItem` must succeed
- DB write (`UpdateService`) happens only **after** Stripe confirms at line 460

---

### 4. ~~Kaniko `--insecure` Flag Disables TLS Globally~~ — RESOLVED

**Status:** Fixed in `api/services/kube_builder.go:314`

Global `--insecure` flag removed. Only `--insecure-registry=<registryHost>` remains, scoping HTTP-only communication to the internal registry. Docker Hub and other registry pulls now use full TLS verification.

---

## High — Multi-Tenant Security Gaps

### 5. ~~No Network Policy Isolation Between Tenants~~ — RESOLVED

**Status:** Fixed in `api/services/kube_network_policies.go`

Per-workspace NetworkPolicies now enforce ingress isolation:
- `rp-ws-<id>-isolation` — each workspace's pods only accept ingress from pods in the same workspace
- `rp-allow-ingress-nginx` — global policy allowing external traffic via ingress-nginx controller
- Applied on every deploy path (DeployService, BuildImage, CronJob, OneOff, ManagedDB, ManagedKV)
- Background reconciliation backfills policies for existing workspaces
- RBAC updated with `networkpolicies` permission

---

### 6. ~~Insufficient SecurityContext on Customer Pods~~ — RESOLVED

**Status:** Fixed in `api/services/kube_pod_security.go` with `applyTenantSecurityContext(strict=true)`

- `capabilities.drop: ALL` (line 93) — all capabilities dropped, not just NET_RAW
- `runAsNonRoot: true` (line 71) — enforced at pod level
- `readOnlyRootFilesystem: true` (line 90) — container-level
- `ensureWritableTmp()` (lines 19-24) — emptyDir mounted at `/tmp` for apps needing writable temp
- Applied on all tenant pod types: Deployments (`kube_deployer.go:314`), CronJobs (`kube_cron.go:139`), One-off Jobs (`kube_oneoff.go:138`)
- Default is `strict=true`; opt-in `compat`/`legacy` mode available for images requiring root

---

### 7. ~~No Resource Limits on Build Jobs~~ — RESOLVED

**Status:** Fixed in `api/services/kube_builder.go:251-322`

Both containers now have requests and limits:
- **Git clone init-container** (lines 300-303): 100m/128Mi requests, 500m/512Mi limits
- **Kaniko container** (lines 319-322): 500m/1Gi requests, 4 CPU/8Gi limits

---

### 8. ~~No Resource Limits on One-Off Jobs~~ — RESOLVED

**Status:** Fixed in `api/services/kube_oneoff.go:97,127-130`

One-off jobs now use `kubeResourcesForPlan(svc.Plan)` to apply plan-based resource requests and limits matching the parent service.

---

### 9. ~~GitHub Token Secret Cleanup Is Best-Effort~~ — RESOLVED

**Status:** Fixed in `api/services/kube_builder.go:340-384`

The git token Secret now has an `OwnerReference` pointing to the build Job (with correct APIVersion, Kind, Name, UID). When the Job is garbage-collected (via TTLSecondsAfterFinished), the Secret is automatically deleted by K8s GC. The best-effort `defer` delete is retained as a secondary cleanup path.

---

## Medium — Bugs & Race Conditions

### 10. ~~Data Race in `GetKubeDeployer`~~ — RESOLVED

**Status:** Fixed in `api/services/worker.go:71-90`

Replaced racy double-checked locking with `sync.Once`. The `kubeOnce.Do()` guarantees exactly-once initialization with correct memory visibility across goroutines. Error stored in `kubeErr` for post-init callers. Old `kubeMu` mutex removed.

---

### 11. ~~Missing `ActiveDeadlineSeconds` on Build Jobs~~ — RESOLVED

**Status:** Fixed in `api/services/kube_builder.go:277`

`ActiveDeadlineSeconds: int64Ptr(25 * 60)` set on build Job spec, matching the 25-minute context timeout. Hung builds are now killed by Kubernetes after 25 minutes regardless of Go context state.

---

### 12. ~~Missing `ActiveDeadlineSeconds` on One-Off Jobs~~ — RESOLVED

**Status:** Fixed in `api/services/kube_oneoff.go:107`

`ActiveDeadlineSeconds: int64Ptr(15 * 60)` set on one-off Job spec, matching the 15-minute context timeout. Runaway one-off jobs are now killed by Kubernetes after 15 minutes.

---

### 13. ~~30-Second Shared Context for Multi-Step Deploys~~ — RESOLVED

**Status:** Fixed in `api/services/kube_deployer.go:169` and `api/services/kube_cron.go:52`

Both `DeployService` and `DeployCronJob` now use `context.WithTimeout(context.Background(), 2*time.Minute)` — increased from 30s to 2 minutes.

---

### 14. ~~Empty Env Vars Silently Dropped~~ — RESOLVED

**Status:** Fixed in `api/services/worker.go:599-622`

Empty decrypted values are now preserved. Only env vars with no encrypted value (truly unset) are skipped. Values are no longer trimmed — whitespace is treated as significant. Comment explicitly documents the intent: "preserve empty values (`""` is distinct from unset)".

---

### 15. ~~`RestartService` Has No Conflict Retry~~ — RESOLVED

**Status:** Fixed in `api/services/kube_deployer.go:555-587`

Replaced Get-Modify-Update with a Kubernetes `MergePatchType` PATCH. The patch sets `railpush.com/restarted-at` annotation directly — no Get required, no 409 Conflict possible. Server-side merge is atomic.

---

### 16. ~~Variable Shadowing — Receiver `k` Shadowed by Range Variable~~ — RESOLVED

**Status:** Fixed in `api/services/kube_deployer.go:160` and `api/services/kube_cron.go:43`

Range variable renamed from `k` to `envKey` in both `DeployService` and `DeployCronJob`. No longer shadows the `k *KubeDeployer` receiver.

---

### 17. ~~Stale Services/Ingresses After Service Type Change~~ — RESOLVED

**Status:** Fixed in `api/services/kube_deployer.go:390-395,458-463`

Stale Service and Ingress deletes now log `WARNING` with service ID, resource name, and error. `IsNotFound` errors are correctly ignored (resource already gone). No longer silently swallowed.

---

## Low — Operational Risks

### 18. ~~Deployment Selector Includes Non-Stable Labels~~ — RESOLVED

**Status:** Fixed in `api/services/kube_deployer.go:309,430`

Introduced `kubeServiceSelectorLabels(svc)` returning a minimal, stable label set for the Deployment selector. Full metadata labels are applied only to the pod template via `mergeLabels(labels, selectorLabels)`. Existing Deployments are handled gracefully — immutable selectors are preserved from the existing resource (lines 496-508) to avoid forced delete/recreate.

---

### 19. ~~No PodDisruptionBudget for Multi-Replica Services~~ — RESOLVED

**Status:** Fixed in `api/services/kube_deployer.go:160-204,521`

`reconcileServicePDB()` creates/updates a `PodDisruptionBudget` with `maxUnavailable: 1` for services with >1 replica. PDB is automatically deleted when replicas drop to 1. Cleanup on service deletion also removes the PDB (line 721). Uses stable selector labels for matching.

---

### 20. ~~Custom Domain Hash Collision Risk~~ — RESOLVED

**Status:** Fixed in `api/services/kube_custom_domains.go:19-23`

Hash increased from 8 to 12 hex characters (48 bits). Birthday-paradox 50% collision threshold moves from ~65K to ~16.7M custom domains. Comment documents the rationale.

---

### 21. ~~Ingress Host Collision — Control Plane vs Customer~~ — RESOLVED

**Status:** Fixed in `api/services/kube_deployer.go:571-577`

Explicit guard checks the generated service host against `k.Config.ControlPlane.Domain`. If the host matches the control-plane domain (or `www.` prefix), deploy returns an error: `"service host %q conflicts with reserved control-plane host"`. Prevents any customer service from hijacking control-plane Ingress traffic.

---

### 22. ~~Control Plane Postgres Has No Resource Limits~~ — RESOLVED

**Status:** Fixed in `deploy/k8s/control-plane-overlays/prod-cnpg/cnpg-cluster.yaml:33-39`

CNPG cluster now has both requests and limits: 500m CPU / 1Gi memory requests, 2 CPU / 2Gi memory limits. Prevents unbounded memory consumption and OOM kills on the node.

---

## Summary

| # | Severity | Issue | Category |
|---|----------|-------|----------|
| 1 | ~~**Critical**~~ | ~~Command injection via Redis `maxmemory-policy`~~ — **RESOLVED** | Security |
| 2 | ~~**Critical**~~ | ~~Command injection via `pg_isready` probes~~ — **RESOLVED** | Security |
| 3 | ~~**Critical**~~ | ~~Plan field is self-declared — no validation or payment gate~~ — **RESOLVED** | Billing / Security |
| 4 | ~~**Critical**~~ | ~~Kaniko `--insecure` disables TLS globally~~ — **RESOLVED** | Security |
| 5 | ~~**High**~~ | ~~No NetworkPolicy between tenants~~ — **RESOLVED** | Security |
| 6 | ~~**High**~~ | ~~Insufficient SecurityContext on customer pods~~ — **RESOLVED** | Security |
| 7 | ~~**High**~~ | ~~No resource limits on build jobs~~ — **RESOLVED** | DoS |
| 8 | ~~**High**~~ | ~~No resource limits on one-off jobs~~ — **RESOLVED** | DoS |
| 9 | ~~**High**~~ | ~~GitHub token Secret cleanup is best-effort~~ — **RESOLVED** | Security |
| 10 | ~~**Medium**~~ | ~~Data race in `GetKubeDeployer`~~ — **RESOLVED** | Race condition |
| 11 | ~~**Medium**~~ | ~~Missing `ActiveDeadlineSeconds` on build jobs~~ — **RESOLVED** | Resource leak |
| 12 | ~~**Medium**~~ | ~~Missing `ActiveDeadlineSeconds` on one-off jobs~~ — **RESOLVED** | Resource leak |
| 13 | ~~**Medium**~~ | ~~30s shared context for multi-step deploys~~ — **RESOLVED** | Reliability |
| 14 | ~~**Medium**~~ | ~~Empty env vars silently dropped~~ — **RESOLVED** | Correctness |
| 15 | ~~**Medium**~~ | ~~`RestartService` no conflict retry~~ — **RESOLVED** | Race condition |
| 16 | ~~**Medium**~~ | ~~Variable shadowing (`k` receiver)~~ — **RESOLVED** | Code quality |
| 17 | ~~**Medium**~~ | ~~Stale resources after service type change~~ — **RESOLVED** | Reliability |
| 18 | ~~**Low**~~ | ~~Selector includes non-stable labels~~ — **RESOLVED** | Operational |
| 19 | ~~**Low**~~ | ~~No PodDisruptionBudget~~ — **RESOLVED** | Availability |
| 20 | ~~**Low**~~ | ~~Custom domain hash collision (32 bits)~~ — **RESOLVED** | Correctness |
| 21 | ~~**Low**~~ | ~~Ingress host collision risk~~ — **RESOLVED** | Security |
| 22 | ~~**Low**~~ | ~~Control plane Postgres no resource limits~~ — **RESOLVED** | Operational |

---
---

# Operational Readiness Audit

**Date:** 2026-02-15
**Scope:** Pre-launch operational review beyond code bugs

---

## Launch Blockers

### 23. ~~No Backup for Tenant Managed Databases~~ — RESOLVED

**Status:** Fixed with a dedicated K8s CronJob, backup script, and container image:

- **CronJob** (`deploy/k8s/control-plane/tenant-backups.yaml`) — daily at 03:20 UTC, `Forbid` concurrency, 2h deadline, dedicated ServiceAccount with minimal RBAC, hardened SecurityContext (`runAsNonRoot`, UID 65532), resource limits, 100Gi `longhorn-r3` PVC (3-replica durability), 7-day retention with automatic pruning.
- **Backup script** (`deploy/images/tenant-backup/tenant-backup.sh`) — discovers all tenant Postgres and Redis pods via labels, runs `pg_dump --format=custom` (Postgres) and `BGSAVE` + RDB stream (Redis), zstd compression, PGDMP/REDIS header verification, sha256 checksums, atomic writes, non-zero exit on failure.
- **Container image** (`deploy/images/tenant-backup/Dockerfile`) — Alpine 3.20 + kubectl v1.34.3 + zstd, runs as non-root.
- Included in kustomization (`kustomization.yaml:12`).

---

### 24. ~~No WAL Archiving / PITR for Control-Plane Database~~ — RESOLVED

**Status:** Fixed in `deploy/k8s/control-plane-overlays/prod-cnpg/cnpg-cluster.yaml:38-58` and `scheduledbackup.yaml`

- **Continuous WAL archiving** to Cloudflare R2 (`s3://rail-push/cnpg/railpush-postgres-cnpg`) via Barman object store. zstd compression, 4 parallel WAL workers. Enables point-in-time recovery to any second.
- **Daily base backups** via `ScheduledBackup` resource at 03:20, targeting standby replica (`prefer-standby`), `immediate: true` on first apply.
- **30-day retention** with automatic pruning of old backups and WAL segments.
- R2 credentials externalized in `railpush-r2-backup` Secret (not committed). Included in kustomization.

---

### 25. ~~Wildcard TLS Certificate Has No Automated Renewal~~ — RESOLVED

**Status:** Fixed with three new manifests:

- **DNS-01 ClusterIssuer** in `deploy/k8s/cluster/clusterissuers-dns01-cloudflare.yaml` — `letsencrypt-prod` ClusterIssuer with Cloudflare DNS-01 solver referencing `cloudflare-api-token-secret`.
- **Wildcard Certificate** in `deploy/k8s/control-plane/certificates.yaml` — explicit `Certificate` resource for `apps.railpush.com` + `*.apps.railpush.com`, issued by `letsencrypt-prod`, stored in `apps-wildcard-tls`. Cert-manager will auto-renew before expiry.
- **Ingress** correctly references `apps-wildcard-tls` as the TLS secret.

---

### 26. ~~Auth Endpoints Share Global Rate Limit — Credential Stuffing Risk~~ — RESOLVED

**Status:** Fixed in `api/middleware/ratelimit.go:35-39,212-231`

Dedicated `authLimiter` at 20 req/min for `/api/v1/auth/login` and `/api/v1/auth/register` (vs 100/min general). `limiterForPath()` routes auth endpoints to the stricter limiter.

**Note:** Rate limiter remains in-memory (not Redis-backed), so 2 replicas effectively allow 40 req/min. Acceptable for launch; Redis backing is a future improvement.

---

### 27. ~~No ResourceQuota or LimitRange in Tenant Namespace~~ — RESOLVED

**Status:** Fixed with two new manifests in `deploy/k8s/control-plane/`:

- **ResourceQuota** (`resourcequota.yaml`) — namespace-level caps: 1000 pods, 1000 PVCs, 200 CPU requests / 400 CPU limits, 500Gi memory requests / 1Ti memory limits, 20Ti storage.
- **LimitRange** (`limitrange.yaml`) — per-container defaults and bounds: min 10m CPU / 64Mi mem, max 8 CPU / 16Gi mem, default requests 100m CPU / 256Mi mem, default limits 2 CPU / 2Gi mem.

This prevents any single tenant (or bug) from consuming all cluster resources, and ensures pods without explicit resource specs get sane defaults.

---

### 28. ~~No CI/CD Pipeline for Kubernetes Control-Plane Deploys~~ — RESOLVED

**Status:** Fixed in `.github/workflows/deploy-k8s-control-plane.yml`

Full pipeline: triggers on CI success → SSH to cluster node → sync repo → Docker build with commit-SHA tag + `:dev` tag → push both → `kubectl apply -k` → `kubectl set image` with SHA tag on both control-plane and worker Deployments → `rollout status --timeout=10m` → health check. SHA-tagged images enable deterministic rollback.

---

### 29. ~~StorageClass Not Codified in Repo~~ — RESOLVED

**Status:** Fixed across manifest and Go code:

- **StorageClass manifest** at `deploy/k8s/cluster/storageclass-longhorn-r2.yaml` — defines `longhorn-r2` with `numberOfReplicas: "2"`, marked as default class. Cluster rebuild no longer requires tribal knowledge.
- **Explicit storageClassName on tenant PVCs** — `kube_managed_resources.go` now calls `k.storageClassName()` on both database (line 312) and Redis (line 532) VolumeClaimTemplates.
- **`storageClassName()` helper** in `kube_deployer.go:62-73` — defaults to `"longhorn-r2"`, configurable via `KUBE_STORAGE_CLASS`. Tenant PVCs will never silently fall back to unreplicated local-path storage.

---

## High-Priority Fast Follows

### 30. ~~Worker Pod `terminationGracePeriodSeconds` Too Short~~ — RESOLVED

**Status:** Fixed in `deploy/k8s/control-plane/worker.yaml:19`

`terminationGracePeriodSeconds: 1800` (30 minutes) set on the worker Deployment. This exceeds the maximum build timeout (25 min), ensuring in-flight builds complete gracefully during rolling updates or node drains before the pod is killed.

---

### 31. ~~Backup Script Targets Wrong Pod Name After CNPG Cutover~~ — RESOLVED

**Status:** Fixed in `deploy/ops/railpush-pg-backup-to-data:25-41`

The script now has a `detect_pod()` function with 3-tier discovery: (1) read `status.currentPrimary` from the CNPG `Cluster` CRD — follows failovers automatically, (2) fallback to `cnpg.io/instanceRole=primary` label selector, (3) legacy `railpush-postgres-0` only as last resort with a `WARN` log. DB user defaults to `postgres` for CNPG peer auth.

---

### 32. ~~No Loki Ingestion Rate Limits~~ — RESOLVED

**Status:** Fixed in `deploy/k8s/logging/loki-values.yaml:40-47` and `deploy/k8s/monitoring/railpush-alert-rules.yaml:100-131`

- **Ingestion limits**: 3MB/s rate, 6MB burst, 512KB per-stream rate, 2MB per-stream burst, 5000 max streams, 256KB max line size with truncation.
- **PVC alerts**: warning at >80% usage (15min), critical at >90% (5min).

---

### 33. ~~Missing Alerts — Deploy Failures, Backup Failures, Longhorn Health~~ — RESOLVED

**Status:** Fixed in `deploy/k8s/monitoring/railpush-alert-rules.yaml`

New alert rules added covering all identified gaps:
- **API 5xx rate** — `RailPushAPIHigh5xxErrorRate` (sustained) and `RailPushAPI5xxBurst` (spike)
- **Build failures** — `RailPushBuildJobFailures`
- **Backup failures** — tenant backup CronJob failure alerts
- **Longhorn health** — `LonghornVolumeDegraded` and `LonghornVolumeFaulted`

---

### 34. ~~DB_PASSWORD Defaults to `"railpush"` if Env Var Missing~~ — RESOLVED

**Status:** Fixed in `api/config/config.go:195` and `api/main.go:135-137`

Default changed from `"railpush"` to `""`. `validateCriticalConfig()` now requires `DB_PASSWORD` — startup fails with `"DB_PASSWORD is required"` if unset. Same pattern as `JWT_SECRET` and `ENCRYPTION_KEY`.

---

### 35. ~~Concurrent Migration Execution Across Replicas~~ — RESOLVED

**Status:** Fixed in `api/database/migrations.go:16-34`

PostgreSQL advisory lock (`pg_advisory_lock`) acquired on a dedicated connection before any migrations run. All replicas contend on the same lock — only one proceeds at a time. Lock is released in a `defer` after the full migration run completes. This serializes migrations across all pods (control-plane + worker) and prevents duplicate work or lock contention.

---

### 36. ~~Grafana Dashboards Not Provisioned as Code~~ — RESOLVED

**Status:** Fixed in `deploy/k8s/monitoring/values.yaml:116-441`

Dashboards provisioned as code via Helm values: `dashboardProviders` defines a `railpush` provider reading from `/var/lib/grafana/dashboards/railpush`, and `dashboards.railpush.railpush-overview` contains the full dashboard JSON inline. PVC loss no longer results in dashboard loss — they are rebuilt from git on every Helm upgrade.

---

## Summary — Full Audit

| # | Severity | Issue | Category |
|---|----------|-------|----------|
| 1 | ~~**Critical**~~ | ~~Command injection via Redis `maxmemory-policy`~~ — **RESOLVED** | Security |
| 2 | ~~**Critical**~~ | ~~Command injection via `pg_isready` probes~~ — **RESOLVED** | Security |
| 3 | ~~**Critical**~~ | ~~Plan field self-declared — no validation or payment gate~~ — **RESOLVED** | Billing |
| 4 | ~~**Critical**~~ | ~~Kaniko `--insecure` disables TLS globally~~ — **RESOLVED** | Security |
| 5 | ~~**High**~~ | ~~No NetworkPolicy between tenants~~ — **RESOLVED** | Security |
| 6 | ~~**High**~~ | ~~Insufficient SecurityContext on customer pods~~ — **RESOLVED** | Security |
| 7 | ~~**High**~~ | ~~No resource limits on build jobs~~ — **RESOLVED** | DoS |
| 8 | ~~**High**~~ | ~~No resource limits on one-off jobs~~ — **RESOLVED** | DoS |
| 9 | ~~**High**~~ | ~~GitHub token Secret cleanup is best-effort~~ — **RESOLVED** | Security |
| 10 | ~~**Medium**~~ | ~~Data race in `GetKubeDeployer`~~ — **RESOLVED** | Race condition |
| 11 | ~~**Medium**~~ | ~~Missing `ActiveDeadlineSeconds` on build jobs~~ — **RESOLVED** | Resource leak |
| 12 | ~~**Medium**~~ | ~~Missing `ActiveDeadlineSeconds` on one-off jobs~~ — **RESOLVED** | Resource leak |
| 13 | ~~**Medium**~~ | ~~30s shared context for multi-step deploys~~ — **RESOLVED** | Reliability |
| 14 | ~~**Medium**~~ | ~~Empty env vars silently dropped~~ — **RESOLVED** | Correctness |
| 15 | ~~**Medium**~~ | ~~`RestartService` no conflict retry~~ — **RESOLVED** | Race condition |
| 16 | ~~**Medium**~~ | ~~Variable shadowing (`k` receiver)~~ — **RESOLVED** | Code quality |
| 17 | ~~**Medium**~~ | ~~Stale resources after service type change~~ — **RESOLVED** | Reliability |
| 18 | ~~**Low**~~ | ~~Selector includes non-stable labels~~ — **RESOLVED** | Operational |
| 19 | ~~**Low**~~ | ~~No PodDisruptionBudget~~ — **RESOLVED** | Availability |
| 20 | ~~**Low**~~ | ~~Custom domain hash collision (32 bits)~~ — **RESOLVED** | Correctness |
| 21 | ~~**Low**~~ | ~~Ingress host collision risk~~ — **RESOLVED** | Security |
| 22 | ~~**Low**~~ | ~~Control plane Postgres no resource limits~~ — **RESOLVED** | Operational |
| | | **--- Operational Readiness ---** | |
| 23 | ~~**Critical**~~ | ~~No backup for tenant managed databases~~ — **RESOLVED** | Data loss |
| 24 | ~~**Critical**~~ | ~~No WAL archiving / PITR for control-plane DB~~ — **RESOLVED** | Data loss |
| 25 | ~~**Critical**~~ | ~~Wildcard TLS cert has no automated renewal~~ — **RESOLVED** | Availability |
| 26 | ~~**High**~~ | ~~Auth endpoints share global rate limit (100/min)~~ — **RESOLVED** | Security |
| 27 | ~~**High**~~ | ~~No ResourceQuota or LimitRange in namespace~~ — **RESOLVED** | DoS |
| 28 | ~~**High**~~ | ~~No CI/CD pipeline for K8s control-plane deploys~~ — **RESOLVED** | Operational |
| 29 | ~~**High**~~ | ~~StorageClass not codified; tenant PVCs lack explicit class~~ — **RESOLVED** | Data loss |
| 30 | ~~**Medium**~~ | ~~Worker pod terminationGracePeriodSeconds too short~~ — **RESOLVED** | Reliability |
| 31 | ~~**Medium**~~ | ~~Backup script targets wrong pod after CNPG cutover~~ — **RESOLVED** | Data loss |
| 32 | ~~**Medium**~~ | ~~No Loki ingestion rate limits~~ — **RESOLVED** | Availability |
| 33 | ~~**Medium**~~ | ~~Missing alerts for deploys, backups, Longhorn, 5xx~~ — **RESOLVED** | Observability |
| 34 | ~~**Medium**~~ | ~~DB_PASSWORD defaults to insecure value if unset~~ — **RESOLVED** | Security |
| 35 | ~~**Medium**~~ | ~~Concurrent migration execution across replicas~~ — **RESOLVED** | Correctness |
| 36 | ~~**Low**~~ | ~~Grafana dashboards not provisioned as code~~ — **RESOLVED** | Operational |
