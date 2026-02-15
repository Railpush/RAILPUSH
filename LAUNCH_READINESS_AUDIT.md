# RailPush Launch Readiness Audit

Date: 2026-02-15

This is a pragmatic "can we launch?" snapshot based on cluster state + the current codebase.

## What You Asked For (Status)

- Login redirect page blank (`/login`): **Fixed** (error boundary + simplified login page shipped).
- Separate Ops Control module/dashboard: **Implemented** (`/ops/*` + incidents + support + outbox).
- Logged-in template too modern: **Simplified** (layout/topbar/sidebar/cards + calmer surfaces).
- `auto_deploy=true` default for all users/services: **Enabled**
  - Backfilled existing services to `auto_deploy=true`.
  - New services default to `auto_deploy=true` in API + UI.

## Production Signals (Cluster / Platform)

- Kubernetes implementation: **k3s** (Kubernetes).
- Control-plane health:
  - `https://railpush.com/healthz` -> `ok`
  - `https://railpush.com/readyz` -> `ready`
- Control-plane availability: 2 replicas across nodes.
- Worker: 1 replica (processing deploy queue).
- Platform Postgres (CloudNativePG): **3 instances, healthy**, anti-affinity enabled.
- Ingress + TLS:
  - nginx ingress running (2 replicas).
  - cert-manager ClusterIssuers present and READY.
  - TLS secrets present for `railpush.com` and `apps.railpush.com`.
- Logging/monitoring:
  - Loki + promtail running.
  - Prometheus + Alertmanager running.
  - Alertmanager -> RailPush webhook is active (events present in `alert_events` table).
  - Alertmanager -> Slack paging enabled for `severity=critical` (drill verified).

## Launch Blockers (Fix Before Public Launch)

1) Transactional email deliverability is not fully production hardened
   - Resend SMTP is configured in K8s and sending works, but you still need:
     - DNS deliverability (SPF/DKIM/DMARC) for `railpush.com` in Resend.
   - Without this, email can land in spam or fail verification.

2) Backups/restore drills are not proven
   - CNPG is healthy, but a launch needs:
     - A documented restore procedure (and a practiced restore drill).
     - Clear RPO/RTO targets and confirmation backups are off-node/off-cluster.

3) k3s HA operational sharp edge: kubectl logs/exec depend on which server you target
   - Observed: `kubectl logs` for pods on node `data` fails via the `xeon` server API with 502,
     but succeeds via the `cpu` server API.
   - This is an ops reliability risk during incidents (debugging becomes "which server has the session").

## Not Blockers, But Should Be Scheduled

- Hardening:
  - Webhook hardening/audit trails beyond signature verification.
  - Abuse controls: stricter rate limits, per-user quotas, anomaly detection.
- Ops discipline:
  - Runbooks, upgrade playbook, failure drills (node loss, registry loss, DB switchover).
- UX polish:
  - Split marketing landing vs app shell cleanly (already mostly done).
  - Add "webhook installed / missing" and "repair webhook" actions for GitHub.

## Recommended Next Steps (Order)

1) Add Resend DNS records (SPF/DKIM/DMARC) for `railpush.com` and verify in Resend.
2) Run a CNPG restore drill and document results (timestamps, duration, pitfalls).
3) Fix the k3s multi-server log/exec behavior (either standardize ops access via one API endpoint, or re-architect HA/LB so logs/exec are reliable).
