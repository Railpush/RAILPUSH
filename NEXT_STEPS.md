# RailPush — Next Steps Roadmap

Prioritized feature roadmap to reach and exceed Render.com parity.

---

## Tier 1 — Fix what's broken (this week)

### 1. GitHub PR comment with preview URL
- **Status**: Preview deploys work end-to-end but the URL is never posted back to the PR
- **What**: After a preview deploy goes live, call the GitHub API to comment the preview URL on the PR
- **Where**: `api/handlers/webhooks.go` — after `Worker.Enqueue()` in the `pull_request` handler
- **Impact**: Makes the entire preview system visible to developers without leaving GitHub
- **Effort**: Small

### 2. Build caching (Kaniko `--cache`)
- **Status**: Every deploy rebuilds from scratch — no Docker layer reuse
- **What**: Add `--cache=true` and `--cache-repo` flags to the Kaniko build args
- **Where**: `api/services/kube_builder.go` (lines 335-341, Kaniko args)
- **Impact**: 50-80% faster builds for unchanged layers (npm install, pip install, etc.)
- **Effort**: Small

### 3. Automated database backups
- **Status**: Manual `pg_dump` backups exist (`POST /databases/:id/backup`) but no schedule
- **What**: Add a cron-based scheduler that runs nightly backups for every managed PostgreSQL database. Include retention policy (e.g. keep last 7 daily, 4 weekly).
- **Where**: `api/services/scheduler.go` (add backup job), `api/handlers/databases.go` (existing backup logic)
- **Impact**: Table-stakes for any production database. Render does daily backups with point-in-time recovery.
- **Effort**: Medium

---

## Tier 2 — Competitive parity (this month)

### 4. Deploy notifications (Slack / Discord / Email)
- **Status**: Email infrastructure exists (`api/services/emailer.go`), alert webhook sink exists, but no deploy event notifications
- **What**: Add a "notification channels" model per workspace (Slack webhook URL, Discord webhook URL, email list). Fire notifications on deploy success/failure/rollback.
- **Where**: New model `notification_channels`, hook into deploy completion in `api/services/worker.go`
- **Impact**: Dealbreaker for teams. Every competing PaaS has this.
- **Effort**: Medium

### 5. Historical metrics (Prometheus + charts)
- **Status**: Metrics page shows live snapshot only (`docker stats --no-stream`). No time-series storage or charts.
- **What**: Integrate Prometheus scraping for CPU/memory/network. Add a time-series chart to the service metrics page (e.g. recharts). Show last 1h, 6h, 24h, 7d.
- **Where**: `api/handlers/metrics.go` (add Prometheus query endpoint), `dashboard/src/pages/ServiceMetrics.tsx` (add charts)
- **Impact**: Users need to see trends to debug performance issues. Current point-in-time view is not useful for production.
- **Effort**: Medium

### 6. Build queue visibility
- **Status**: Deploy queue exists internally (`deploy_queue` with lease management) but users can't see it
- **What**: Add an API endpoint returning queue position and estimated wait time. Show in the deploy detail page and deploy list.
- **Where**: `api/handlers/deploys.go` (new endpoint), `dashboard/src/pages/ServiceDeploys.tsx` (queue position badge)
- **Impact**: Removes anxiety during slow deploy queues — users know their deploy isn't stuck.
- **Effort**: Small

### 7. Database restore from backup
- **Status**: Backups can be created but there is no restore functionality
- **What**: Add `POST /databases/:id/restore` that takes a backup ID, runs `pg_restore` or `psql < dump.sql` against the target database.
- **Where**: `api/handlers/databases.go` (new restore handler)
- **Impact**: Backups without restore are useless. This completes the disaster recovery story.
- **Effort**: Medium

---

## Tier 3 — Differentiate from Render (this quarter)

### 8. AI deploy diagnostics
- **Status**: AI auto-fix exists (`api/services/ai_fix.go`) but only attempts automated fixes
- **What**: Before auto-fixing, show users a plain-English explanation of why their deploy failed (parsed from build logs via LLM). Suggest the fix. Let them one-click apply or dismiss. Surface in the deploy detail page.
- **Where**: `api/services/ai_fix.go` (add diagnostic step), `dashboard/src/pages/ServiceDeploys.tsx` (diagnostic UI)
- **Impact**: Major differentiator. Render has nothing like this. Turns failed deploys from frustrating into educational.
- **Effort**: Medium

### 9. Rename `render.yaml` to `railpush.yaml`
- **Status**: Blueprint file is literally called `render.yaml` — same as the competitor
- **What**: Make `railpush.yaml` the primary filename. Keep `render.yaml` as a backwards-compatible fallback (check `railpush.yaml` first, fall back to `render.yaml`).
- **Where**: `api/handlers/blueprints.go` (YAML file detection logic), `dashboard/src/pages/Docs.tsx` (all code examples)
- **Impact**: Brand identity. You can't build a competing product using their config filename.
- **Effort**: Small

### 10. One-click database pooling (PgBouncer sidecar)
- **Status**: No connection pooling exists. Users hit connection limits with serverless/high-concurrency apps.
- **What**: Spin up a PgBouncer container alongside each managed PostgreSQL. Expose a toggle in the database dashboard. Provide a separate `POOLED_DATABASE_URL` connection string.
- **Where**: `api/services/kube_managed_resources.go` or `api/services/worker.go` (container provisioning), `api/handlers/databases.go` (toggle endpoint)
- **Impact**: Solves a common pain point before users ask. Most PaaS users eventually hit connection limits.
- **Effort**: Medium

### 11. Cost explorer
- **Status**: Billing data exists (per-resource items, credit ledger, monthly totals) but there is no spend-over-time visualization
- **What**: Add a cost explorer page showing per-resource spend over time, cost breakdown by resource type, and projected month-end bill with trend line. Data is already in `billing_items` and `workspace_credit_ledger`.
- **Where**: New dashboard page `dashboard/src/pages/CostExplorer.tsx`, new API endpoint for historical billing aggregation
- **Impact**: Render's billing page is minimal — a good cost explorer is a competitive advantage for cost-conscious teams.
- **Effort**: Medium
