# Documentation Audit — 2026-02-17

Cross-reference of `dashboard/src/pages/Docs.tsx` against the actual codebase.

---

## Incorrect

| # | Claim | Docs Say | Reality | File |
|---|-------|----------|---------|------|
| 1 | Starter Plan Price | $0/mo | **$7/mo** (700 cents). The $0 tier is "Free", not "Starter" | `api/services/pricing.go`, `dashboard/src/lib/plans.ts` |

---

## Incomplete / Missing from Docs

| # | Feature | Status | Details | Files |
|---|---------|--------|---------|-------|
| 2 | Deploy Triggers | Incomplete | Docs list 4 (`manual`, `auto`, `rollback`, `blueprint`). Codebase has 8: `manual`, `github_push`, `rollback`, `blueprint`, `preview`, `cron`, `autoscale`, `ai_fix` | `api/handlers/deploys.go`, `api/handlers/webhooks.go`, `api/services/scheduler.go`, `api/services/autoscaler.go`, `api/services/ai_fix.go` |
| 3 | Autoscaling | Undocumented | Fully implemented — model, handler, background service, API endpoints (`GET/PUT /services/:id/autoscaling`). Docs only mention static `numInstances` | `api/models/autoscaling_policy.go`, `api/handlers/autoscaling.go`, `api/services/autoscaler.go` |
| 4 | One-off Jobs | Undocumented | Fully working (`POST /services/:id/jobs`, `GET /jobs/:id`) with Docker and K8s execution. Not mentioned anywhere in docs | `api/handlers/one_off_jobs.go`, `api/models/one_off_job.go`, `api/services/kube_oneoff.go` |
| 5 | Preview Environments | Undocumented | List endpoint exists (`GET /preview-environments`), types defined in dashboard. Not in docs | `api/handlers/preview_environments.go`, `api/models/preview_environment.go` |
| 6 | Persistent Disks | Partially documented | Docs show blueprint YAML config but there is no REST API to create/manage disks outside of blueprints | `api/models/disk.go`, `api/handlers/blueprints.go` (lines 1354-1396) |
| 7 | PostgreSQL Version Range | Unvalidated | Docs claim "PG 13 – 18" but the code accepts any integer. Default is 16, no range check | `api/handlers/databases.go` (line 66) |

---

## Verified Correct

| Claim | Status | Notes |
|-------|--------|-------|
| Supported Runtimes (Node, Python, Go, Ruby, Rust, Elixir, Java, Docker) | All 8 implemented | `api/services/builder.go` DetectRuntime (lines 107-125) |
| Service Types (web, pserv, worker, cron, static) | Correct | Used as string values throughout handlers |
| Standard plan $25/mo | Correct | 2500 cents in `api/services/pricing.go` |
| Pro plan $85/mo | Correct | 8500 cents in `api/services/pricing.go` |
| Blueprint spec fields (rootDir, dockerCommand, domains, disk, image, envVarGroups, fromGroup) | All exist | `api/handlers/blueprints.go` RenderService struct (lines 481-530) |
| AES-256-GCM encryption for env vars | Correct | `api/utils/crypto.go` (lines 18-34), applied in `api/models/envvar.go` |
| API endpoints (services, databases, blueprints CRUD) | All exist | `api/main.go` router (lines 260-310) |
| Let's Encrypt / TLS | Correct | Caddy automatic HTTPS in Docker mode; cert-manager in K8s mode |
| Blueprint fromService keyvalue connectionString | Correct | Generates `redis://host:port` URLs (`api/handlers/blueprints.go` lines 1638-1650) |
| Custom Domains with auto TLS | Correct | `api/handlers/domains.go`, `api/services/kube_custom_domains.go` |
| Env Groups (shared config) | Correct | `api/handlers/envgroups.go`, blueprint `fromGroup` support |

---

## Recommendations

1. **Fix Starter price** — change `$0/mo` to `$7/mo` in the Scaling section, or add the Free tier explicitly.
2. **Add Autoscaling section** — document `GET/PUT /services/:id/autoscaling` with CPU/memory targets, cooldowns, and min/max instances.
3. **Add One-off Jobs section** — document `POST /services/:id/jobs` and `GET /jobs/:id` for running ad-hoc commands.
4. **Expand Deploy Triggers** — replace the generic "auto" trigger with the actual types: `github_push`, `preview`, `cron`, `autoscale`, `ai_fix`.
5. **Add Preview Environments section** — document the listing endpoint and how PR-based previews work.
6. **Clarify Persistent Disks** — note that disks are currently blueprint-only (no standalone REST API).
7. **Validate PostgreSQL versions** — either add range validation (13-18) in the API or update docs to say "any version available on Docker Hub".
