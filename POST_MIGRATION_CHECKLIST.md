# RailPush Post-Migration Checklist (Product + Ops)

Last updated: 2026-02-15

This is the follow-on checklist after the Kubernetes migration is stable.

## 1) Transactional Email (Users + Deploy Notifications)

Goal: users receive email for account/workspace lifecycle and deploy outcomes, and optionally for incidents.

- [x] MVP implemented in codebase:
  - Outbox table + retrying worker loop
  - SMTP sender (`EMAIL_PROVIDER=smtp`)
  - Templates: welcome + deploy success/failure
- [x] (Prod) Decide email provider:
  - Resend (SMTP)
- [ ] (Prod) Set up deliverability DNS for `railpush.com` in Resend:
  - SPF
  - DKIM
  - DMARC (start `p=none`, tighten later)
- [x] (Prod) Set secrets in K8s `railpush/railpush-secrets`:
  - `EMAIL_PROVIDER=smtp`
  - `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM` (optional `SMTP_REPLY_TO`)
- [ ] (Later) Notification preferences + incident emails (optional)
- [x] (Later) Ops-only “Send test email” action

## 2) GitHub Push Automation (Auto-Deploy)

Goal: when a customer pushes to GitHub, RailPush automatically triggers deploys.

Current capabilities already in the codebase:
- GitHub webhook receiver: `POST /api/v1/webhooks/github` (push + pull_request preview deploys).
- Auto-deploy lookup is indexed (`idx_services_repo_branch_autodeploy`).
- Service create attempts to auto-register the webhook via GitHub API when the user has a stored token.

Checklist:
- [x] Verified end-to-end: create service -> webhook exists -> push commit triggers deploy -> worker runs -> service updates.
- [x] Set `GITHUB_WEBHOOK_SECRET` in `railpush/railpush-secrets` (signature verification enabled).
- [x] Default `auto_deploy=true` for all services (new + existing backfilled).
- [ ] (GitHub UX) Surface “webhook installed / missing / permission denied” on the service page.
- [ ] (GitHub UX) Provide “Repair webhook” action (calls GitHub API).
- [ ] (GitHub Backfill) One-off admin job: iterate GitHub services and attempt to create webhooks.
- [ ] (GitHub Hardening) Rate limit webhook endpoint by IP + signature-required once secret is set.
- [ ] (GitHub Hardening) Persist webhook delivery audit events (minimal: timestamp, repo, branch, matched services, result).

## 3) Full Ops Dashboard (Internal)

Goal: a “platform operator” dashboard for support + operations (technical health + customer/account context).

- [x] (Ops Access) `users.role` supports `admin` + `ops`; ops endpoints require ops role.
- [x] (Ops API) Core endpoints implemented:
  - overview, users, workspaces, services, deploys, email outbox, settings
  - ops-scoped service logs endpoint (`/ops/services/:id/logs`)
- [x] (Ops UI) Core pages implemented + wired into routes/sidebar:
  - Overview, Customers, Services, Deployments, Billing, Tickets, Credits, Technical, Performance
  - Email (outbox), Settings, Service Logs, Incidents
- [ ] (Ops API) Add “Impersonate workspace” (optional; gated; auditable).
- [ ] (Ops Audit) Record all ops actions (who, what, target, timestamp, metadata).
- [ ] (Ops Audit) Redact secrets/tokens in logs + audit payloads.
