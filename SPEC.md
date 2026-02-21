# SPEC.md — RailPush: Self-Hosted PaaS Platform

> A production-ready, self-hosted clone of Render.com for deploying web services, background workers, cron jobs, static sites, and managed datastores from GitHub — with Blueprint (IaC) support.

---

## 1. Overview

**RailPush** is a single-server PaaS that replicates the core Render.com experience on your own Linux machine. Developers connect a GitHub repo, define a build/start command (or Dockerfile), and RailPush handles building, deploying, TLS termination, routing, zero-downtime rollouts, and log aggregation — all from a web dashboard or a `railpush.yaml` Blueprint file.

### 1.1 Design Principles

- **Docker-native**: Every service runs as an OCI container. Builds produce images; deploys run containers.
- **Single-server first, cluster-ready later**: v1 targets one Linux host. Architecture uses abstractions (container runtime interface, reverse-proxy config API) that allow future migration to Kubernetes/Nomad.
- **Convention over configuration**: Sensible defaults (port 10000, auto-detect language, zero-config TLS).
- **Git-native**: Push to deploy. Every commit on the watched branch triggers a build pipeline.
- **Blueprint-driven**: A `railpush.yaml` in the repo root defines the entire infrastructure stack.

### 1.2 Target Environment

| Property | Value |
|---|---|
| OS | Ubuntu 22.04+ / Debian 12+ |
| Runtime | Docker Engine 24+ with BuildKit |
| Reverse Proxy | Caddy 2 (automatic HTTPS via ACME) |
| Orchestration | Docker Compose (generated) + systemd |
| Database | PostgreSQL 16 (system DB + tenant DBs) |
| Cache/KV | Redis 7 (Valkey compatible) |
| Language | Go (control plane), Shell (build scripts) |
| Dashboard | React + TypeScript SPA |

---

## 2. Architecture

```
┌──────────────────────────────────────────────────────────┐
│                        INTERNET                          │
└──────────────┬───────────────────────────┬───────────────┘
               │ HTTPS :443               │ SSH :22 (git)
       ┌───────▼───────┐          ┌───────▼───────┐
       │   Caddy        │          │  GitHub       │
       │   (TLS + L7    │          │  Webhooks     │
       │    routing)     │          │               │
       └───────┬───────┘          └───────┬───────┘
               │                          │
       ┌───────▼──────────────────────────▼───────┐
       │            RailPush API Server          │
       │  ┌─────────┐ ┌──────────┐ ┌───────────┐  │
       │  │ Router   │ │ Builder  │ │ Scheduler │  │
       │  │ Manager  │ │ Queue    │ │ (cron)    │  │
       │  └─────────┘ └──────────┘ └───────────┘  │
       │  ┌─────────┐ ┌──────────┐ ┌───────────┐  │
       │  │ Deploy   │ │ Blueprint│ │ Log       │  │
       │  │ Engine   │ │ Parser   │ │ Collector │  │
       │  └─────────┘ └──────────┘ └───────────┘  │
       └──────────┬───────────────────┬────────────┘
                  │                   │
       ┌──────────▼─────┐   ┌────────▼────────┐
       │  Docker Engine  │   │  System DB      │
       │  (containers)   │   │  (PostgreSQL)   │
       └──────────┬─────┘   └─────────────────┘
                  │
    ┌─────────────┼─────────────────┐
    │             │                 │
┌───▼───┐  ┌─────▼─────┐  ┌───────▼───────┐
│ Web   │  │ Background│  │ Managed       │
│Service│  │ Worker    │  │ Postgres/Redis│
│ :10000│  │ (no port) │  │ containers    │
└───────┘  └───────────┘  └───────────────┘
```

### 2.1 Core Components

| Component | Responsibility |
|---|---|
| **API Server** | REST + WebSocket API. Serves dashboard SPA. Handles GitHub OAuth and webhook ingestion. |
| **Builder** | Clones repo, detects runtime/Dockerfile, builds OCI image via Docker BuildKit. Caches layers. |
| **Deploy Engine** | Pulls built image, runs new container, performs health check, swaps traffic (zero-downtime), tears down old container. |
| **Router Manager** | Generates and hot-reloads Caddy configuration (upstream mappings, TLS certs, custom domains). |
| **Scheduler** | Cron engine. Triggers container runs on schedule. Enforces single-run guarantee. |
| **Blueprint Parser** | Parses `railpush.yaml`, diffs against current state, produces a deploy plan. |
| **Log Collector** | Tails container stdout/stderr, stores in rotating log files, streams to dashboard via WebSocket. |
| **System DB** | PostgreSQL database storing all platform metadata (services, deploys, env vars, users). |

---

## 3. Service Types

### 3.1 Web Service

- Receives public HTTP/HTTPS traffic.
- Must bind to `0.0.0.0:$PORT` (default `PORT=10000`).
- Gets a subdomain: `<service-name>.<base-domain>`.
- Supports custom domains with automatic TLS.
- Zero-downtime deploys with health check verification.
- Auto-deploy on git push (configurable).
- Optional persistent disk mount.
- WebSocket support.

### 3.2 Private Service

- Identical to web service but only reachable on the internal Docker network.
- No public URL or Caddy route.
- Addressable by other services via `<service-name>:$PORT` on the shared Docker bridge network.

### 3.3 Static Site

- Build step produces static assets (HTML/CSS/JS).
- Assets served by Caddy directly from a volume (no running container needed post-build).
- Supports SPA routing (fallback to `index.html`).
- Optional custom headers and redirect rules.
- CDN cache headers automatically applied.

### 3.4 Background Worker

- Runs continuously. No inbound network traffic.
- Typically polls a task queue (Redis/KV-backed).
- Zero-downtime deploy with graceful shutdown (SIGTERM → SIGKILL after configurable delay).

### 3.5 Cron Job

- Runs on a cron schedule (standard cron syntax).
- Single-run guarantee: only one instance active at a time.
- Maximum run time: 12 hours (configurable).
- Manual trigger via dashboard or API.
- Logs and exit code captured per run.

### 3.6 Managed PostgreSQL

- Provisions a PostgreSQL database in a dedicated container.
- Provides internal connection URL (via private network) and optional external access.
- Supports multiple databases per instance.
- Automated daily backups with configurable retention.
- Connection pooling via PgBouncer sidecar.
- IP allow-list for external access.

### 3.7 Managed Key Value (Redis-compatible)

- Provisions a Redis 7 / Valkey container.
- Internal-only by default; optional external access with IP allow-list.
- Persistence via RDB + AOF.
- Automated backups.

---

## 4. Build System

### 4.1 Build Pipeline

```
GitHub Push → Webhook → Enqueue Build → Clone Repo → Detect Build Strategy
    │
    ├── Dockerfile found ────→ docker build (BuildKit)
    │
    ├── railpush.yaml.buildCommand ────→ Buildpack-style build in runtime image
    │
    └── Auto-detect language ────→ Select base image → Install deps → Build
           │
           ├── package.json → Node.js (node:20-slim)
           ├── requirements.txt / Pipfile → Python (python:3.12-slim)
           ├── Gemfile → Ruby (ruby:3.3-slim)
           ├── go.mod → Go (golang:1.22)
           ├── Cargo.toml → Rust (rust:1.77)
           └── mix.exs → Elixir (elixir:1.16)

Build Output → OCI Image → Tagged: registry.local/<service>:<commit-sha>
```

### 4.2 Build Configuration

| Field | Description | Default |
|---|---|---|
| `buildCommand` | Shell command to build the app | Auto-detected |
| `startCommand` | Shell command to start the app | Auto-detected |
| `dockerfilePath` | Path to Dockerfile | `./Dockerfile` |
| `dockerContext` | Docker build context directory | `.` |
| `buildFilter.paths` | Only trigger builds when these paths change | `["**"]` |
| `buildFilter.ignoredPaths` | Never trigger builds for these paths | `[]` |
| `runtime` | `docker`, `image`, `node`, `python`, `go`, `ruby`, `rust`, `elixir` | Auto-detected |
| `preDeployCommand` | Run before starting new container (e.g., migrations) | None |

### 4.3 Build Cache

- Docker BuildKit layer caching persisted in `/var/lib/railpush/buildkit-cache`.
- npm/pip/gem caches mounted as Docker cache mounts.
- Build cache per-service, prunable via dashboard.

### 4.4 Pre-built Image Deployment

- Support `runtime: image` with `image.url` pointing to any Docker registry.
- Pull image on deploy trigger (manual or API-triggered; no auto-deploy for registry images).
- Support private registries with stored credentials.

---

## 5. Deploy System

### 5.1 Zero-Downtime Deploy Sequence

```
1. Build image (or pull pre-built image)
2. Run pre-deploy command (if configured) in ephemeral container
3. Start NEW container from new image
4. Wait for health check to pass (HTTP GET to health check path, or TCP port open)
   - Timeout: 300s (configurable)
   - Interval: 5s
5. Update Caddy upstream to point to NEW container
6. Wait 10s for in-flight requests to drain
7. Send SIGTERM to OLD container
8. Wait for graceful shutdown (default 30s, max 300s)
9. Send SIGKILL if still running
10. Remove OLD container
11. Record deploy as successful
```

### 5.2 Deploy Failure Handling

- If health check fails → abort deploy, keep old container running, mark deploy as failed.
- If pre-deploy command fails → abort deploy, do not start new container.
- Automatic rollback: old container remains active throughout; no action needed on failure.

### 5.3 Deploy Triggers

| Trigger | Description |
|---|---|
| Git push | Auto-deploy on push to watched branch (configurable) |
| Manual | Dashboard button or API call |
| Blueprint sync | Changes to `railpush.yaml` trigger redeploy of affected services |
| Image update | Manual trigger for `runtime: image` services |
| Rollback | Redeploy a previous successful deploy's image |

### 5.4 Deploy Policies

- **Cancel in-progress**: New deploy cancels any running deploy (default).
- **Wait**: New deploy queues behind in-progress deploy.
- Configurable per-workspace.

### 5.5 Skip Deploys

Commits containing `[skip render]` or `[render skip]` in the message skip auto-deploy.

---

## 6. Networking

### 6.1 Public Routing

- **Caddy** is the edge reverse proxy handling all inbound HTTPS traffic.
- Each web service gets: `https://<service-name>.<base-domain>`
- Custom domains: user adds CNAME/A record → verifies in dashboard → Caddy provisions TLS cert via ACME.
- HTTP → HTTPS redirect enforced globally.
- WebSocket upgrade supported transparently.
- Request timeout: 100 minutes (configurable per-service).

### 6.2 Private Networking

- All service containers join a shared Docker bridge network: `railpush-private`.
- Services address each other by service name: `http://<service-name>:<port>`.
- Managed Postgres and Redis also on this network.
- Optional: per-project network isolation (separate Docker networks per project/environment).

### 6.3 Caddy Configuration Management

The Router Manager generates a Caddyfile dynamically and reloads Caddy via its admin API:

```caddyfile
# Auto-generated by RailPush — do not edit manually
{
    email {acme_email}
    acme_ca https://acme-v02.api.letsencrypt.org/directory
}

# Web service: my-api
my-api.example.com {
    reverse_proxy localhost:{container_host_port} {
        health_uri /healthz
        health_interval 10s
    }
    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains"
        X-Content-Type-Options "nosniff"
    }
}

# Custom domain for my-api
api.customdomain.com {
    reverse_proxy localhost:{container_host_port}
}

# Static site: my-frontend
my-frontend.example.com {
    root * /var/lib/railpush/static/my-frontend/current
    file_server
    try_files {path} /index.html  # SPA fallback
    header Cache-Control "public, max-age=3600"
}
```

### 6.4 Port Management

- Default `PORT=10000` injected as environment variable.
- If service binds a different port, auto-detection via container inspection.
- Reserved ports (not usable by services): 22, 80, 443, 5432, 6379, 2019 (Caddy admin).

---

## 7. Blueprint (Infrastructure as Code)

### 7.1 Overview

A `railpush.yaml` file in the repo root defines the full stack. On push, the Blueprint Parser:

1. Reads and validates `railpush.yaml` (falls back to `render.yaml` if not found).
2. If neither file exists, the system can auto-generate one via AI analysis of the repo.
3. Performs preflight validation (duplicate names, domain conflicts, service adoption).
4. Provisions resources atomically: databases first, then key-value stores, then services.
5. Resolves all cross-service env var references before deploying.
6. On any failure, the entire sync is rolled back (no partial infrastructure).

### 7.2 railpush.yaml Schema

The YAML has four top-level keys. All are optional arrays. The field names below match
the exact `yaml:` struct tags in the Go parser — these are the only fields accepted.

```yaml
# railpush.yaml — Complete reference
# Every field shown. See defaults and notes below.

services:
  # ── Web Service (publicly accessible, gets an Ingress + TLS) ──
  - type: web                                    # required: web, pserv, worker, cron, static
    name: my-api                                 # required: unique within the blueprint
    runtime: node                                # node, python, go, ruby, rust, docker, elixir, static, image
    repo: https://github.com/user/my-app         # optional: defaults to blueprint repo
    branch: main                                 # optional: defaults to blueprint branch
    buildCommand: npm install && npm run build    # optional
    startCommand: npm start                      # optional (ignored for static sites)
    port: 3000                                   # optional: default 10000
    plan: starter                                # optional: free, starter, standard, pro (default: starter)
    numInstances: 1                              # optional: default 1 (0 = suspended)
    healthCheckPath: /healthz                    # optional: enables HTTP health probes
    preDeployCommand: npx prisma migrate deploy  # optional: runs before each deploy
    autoDeploy: true                             # optional: auto-deploy on push (default: true)
    dockerfilePath: ./Dockerfile                 # optional: custom Dockerfile path
    dockerContext: .                              # optional: Docker build context directory
    dockerCommand: node server.js                # optional: overrides container CMD
    rootDir: ./services/api                      # optional: monorepo subdirectory
    domains:                                     # optional: custom domains
      - api.example.com
    disk:                                        # optional: persistent disk
      name: uploads
      mountPath: /var/data/uploads
      sizeGB: 10                                 # default: 10
    buildFilter:                                 # optional: only trigger builds on matching paths
      paths:
        - src/**
      ignoredPaths:
        - docs/**
    image:                                       # optional: deploy a prebuilt image (skips build)
      url: docker.io/myorg/myapp:latest
    buildInclude:                                 # optional: whitelist files for build context
      - src/
      - package.json
      - package-lock.json
    buildExclude:                                 # optional: exclude files from build context
      - "*.md"
      - docs/
    baseImage: node:20-slim                       # optional: override default base image for auto-generated Dockerfile
    envVars:
      - key: NODE_ENV
        value: production
      - key: DATABASE_URL
        fromDatabase:
          name: my-db
          property: connectionString             # connectionString, host, port, user, password, database
      - key: REDIS_URL
        fromService:
          type: keyvalue
          name: my-cache
          property: connectionString             # host, port, hostport, connectionString
      - key: SECRET_KEY
        generateValue: true                      # auto-generates a 32-char random string
      - key: SHARED_SECRET
        fromService:
          type: web
          name: other-service
          envVarKey: SECRET_KEY                   # copies the value of another service's env var
      - fromGroup: shared-config                 # imports all vars from a shared env group

  # ── Private Service (ClusterIP only, no Ingress — internal access only) ──
  - type: pserv
    name: internal-api
    runtime: docker
    dockerfilePath: ./services/internal/Dockerfile
    dockerContext: ./services/internal
    port: 3000
    envVars:
      - key: PORT
        value: "3000"

  # ── Background Worker (no Service, no Ingress — runs start command only) ──
  - type: worker
    name: task-processor
    runtime: python
    baseImage: python:3.12-slim                   # override default base (e.g. pre-install Node.js)
    buildCommand: pip install -r requirements.txt
    startCommand: celery -A tasks worker --loglevel=info
    buildInclude:                                 # only include worker files
      - worker.py
      - requirements.txt
    envVars:
      - key: CELERY_BROKER_URL
        fromService:
          type: keyvalue
          name: my-cache
          property: connectionString

  # ── Cron Job (K8s CronJob, concurrencyPolicy: Forbid, backoffLimit: 0) ──
  - type: cron
    name: daily-cleanup
    runtime: node
    buildCommand: npm install
    startCommand: node scripts/cleanup.js
    schedule: "0 3 * * *"                        # required for cron: standard cron expression
    plan: starter

  # ── Static Site (built and served via nginx, gets Ingress + TLS) ──
  - type: static
    name: my-frontend
    buildCommand: npm install && npm run build
    staticPublishPath: ./dist                    # required for static: build output directory

databases:
  - name: my-db                                  # required: unique identifier
    plan: standard                               # optional: free, starter, standard, pro (default: starter)
    postgresMajorVersion: 16                     # optional: default 16
    databaseName: myapp                          # optional: custom DB name (defaults to resource name)
    user: myapp                                  # optional: custom username (defaults to resource name)
    initScript: schema.sql                       # optional: SQL file to run once on first provision

keyValues:
  - name: my-cache                               # required: unique identifier
    plan: standard                               # optional: free, starter, standard, pro (default: starter)
    maxmemoryPolicy: allkeys-lru                 # optional: default allkeys-lru

envVarGroups:
  - name: shared-config                          # required: unique identifier
    envVars:
      - key: APP_ENV
        value: production
      - key: SENTRY_DSN
        value: https://example@sentry.io/123
```

### 7.3 Service Type Reference

Key-value stores and databases are NOT defined under `services`. They have their own
top-level sections (`keyValues` and `databases`).

| Field | web | pserv | worker | cron | static |
|---|---|---|---|---|---|
| `name` | required | required | required | required | required |
| `type` | required | required | required | required | required |
| `runtime` | yes | yes | yes | yes | n/a |
| `repo` | optional | optional | optional | optional | optional |
| `branch` | optional | optional | optional | optional | optional |
| `buildCommand` | yes | yes | yes | yes | yes |
| `startCommand` | yes | yes | yes | yes | ignored |
| `port` | yes (default: 10000) | yes (default: 10000) | n/a | n/a | n/a |
| `plan` | yes | yes | yes | yes | yes |
| `numInstances` | yes (default: 1) | yes (default: 1) | yes (default: 1) | n/a | n/a |
| `healthCheckPath` | yes | yes | n/a | n/a | n/a |
| `preDeployCommand` | yes | yes | yes | n/a | n/a |
| `autoDeploy` | yes | yes | yes | yes | yes |
| `schedule` | n/a | n/a | n/a | required | n/a |
| `staticPublishPath` | n/a | n/a | n/a | n/a | required |
| `dockerfilePath` | yes | yes | yes | yes | n/a |
| `dockerContext` | yes | yes | yes | yes | n/a |
| `dockerCommand` | yes | yes | yes | yes | n/a |
| `rootDir` | yes | yes | yes | yes | yes |
| `domains` | yes | n/a | n/a | n/a | yes |
| `disk` | yes | yes | yes | n/a | n/a |
| `buildFilter` | yes | yes | yes | yes | yes |
| `image` | yes | yes | yes | n/a | n/a |
| `buildInclude` | yes | yes | yes | yes | yes |
| `buildExclude` | yes | yes | yes | yes | yes |
| `baseImage` | yes | yes | yes | yes | yes |
| `envVars` | yes | yes | yes | yes | yes |

### 7.4 Kubernetes Resources Generated

Understanding what K8s resources each service type creates:

| K8s Resource | web | pserv | worker | cron | static |
|---|---|---|---|---|---|
| Deployment | yes | yes | yes | no | yes |
| CronJob | no | no | no | yes | no |
| Service (ClusterIP) | yes | yes | no | no | yes |
| Ingress (public URL) | yes | no | no | no | yes |
| Secret (env vars) | yes | yes | yes | yes | yes |
| PVC (persistent disk) | if disk set | if disk set | if disk set | no | no |
| PodDisruptionBudget | if instances > 1 | if instances > 1 | if instances > 1 | no | no |
| NetworkPolicy | yes (per workspace) | yes (per workspace) | yes (per workspace) | yes | yes |

Managed databases create: StatefulSet + 2 Services (ClusterIP + Headless) + 2 Secrets (auth + TLS) + PVC.
Managed key-value stores create: StatefulSet + 2 Services (ClusterIP + Headless) + Secret (auth) + PVC.

### 7.5 Resource Limits by Plan

| Plan | CPU Request | CPU Limit | Memory Request | Memory Limit | Monthly Cost |
|---|---|---|---|---|---|
| `free` | 100m | 250m | 256Mi | 512Mi | $0 |
| `starter` | 500m | 1 | 512Mi | 1Gi | $7 |
| `standard` | 1 | 2 | 2Gi | 4Gi | $25 |
| `pro` | 2 | 4 | 4Gi | 8Gi | $85 |

Database storage: free=1Gi, starter=5Gi, standard=20Gi, pro=100Gi.
Redis storage: free=1Gi, starter=2Gi, standard=5Gi, pro=10Gi.

### 7.6 Cross-Service References

```yaml
# Reference a database property
- key: DATABASE_URL
  fromDatabase:
    name: <database-name>
    property: connectionString  # or: host, port, user, password, database

# Reference another service's property
- key: API_URL
  fromService:
    type: web                   # web, pserv, worker, keyvalue
    name: <service-name>
    property: host              # or: port, hostport, connectionString

# Copy an env var value from another service
- key: SHARED_SECRET
  fromService:
    type: web
    name: my-api
    envVarKey: SECRET_KEY

# Auto-generate a random 32-char secret
- key: JWT_SECRET
  generateValue: true

# Import all vars from a shared env group
- fromGroup: shared-config
```

### 7.7 Defaults Applied During Sync

| Field | Default Value |
|---|---|
| `type` | `web` |
| `port` | `10000` |
| `plan` | `starter` |
| `numInstances` | `1` |
| `branch` | inherited from blueprint |
| `repo` | inherited from blueprint |
| `autoDeploy` | `true` |
| `disk.sizeGB` | `10` |
| `postgresMajorVersion` | `16` |
| `maxmemoryPolicy` | `allkeys-lru` |

Plan aliases are accepted: `hobby`/`basic`/`small` -> `starter`, `medium` -> `standard`,
`professional`/`business`/`enterprise`/`team` -> `pro`, `trial` -> `free`.

### 7.8 Per-Service Build Context

By default, the entire repo is included in the Docker build context. For monorepos or
multi-service repos where services share a directory, use `buildInclude` or `buildExclude`
to control which files each service sees during `COPY . .`.

**buildInclude** (whitelist mode): Only the listed files/directories are included.
Everything else is excluded. A `.dockerignore` is generated with `*` at the top,
then `!<path>` for each included path.

```yaml
services:
  - type: worker
    name: foreflight-sync
    buildInclude:
      - worker.py
      - requirements.txt
      - schema.sql
```

**buildExclude** (blacklist mode): The listed files/directories are excluded.
Everything else is included. A `.dockerignore` is generated with the listed paths.

```yaml
services:
  - type: web
    name: foreflight-viewer
    buildExclude:
      - worker.py
      - sync.log
      - "*.md"
```

If both are set, `buildInclude` takes precedence. The generated `.dockerignore` is
shown in build logs for debugging.

### 7.9 Custom Base Images

Auto-generated Dockerfiles use default base images (`node:20-alpine`, `python:3.12-slim`,
`golang:1.24-alpine`). Override these with `baseImage` for multi-runtime builds or
when you need system dependencies pre-installed:

```yaml
services:
  # Python + Node.js: use a base image with both runtimes
  - type: web
    name: fullstack-app
    runtime: python
    baseImage: nikolaik/python-nodejs:python3.12-nodejs20
    buildCommand: pip install -r requirements.txt && npm install && npm run build
    startCommand: uvicorn api:app --host 0.0.0.0 --port $PORT
```

If `dockerfilePath` is set (custom Dockerfile), `baseImage` is ignored — you control
the FROM instruction directly.

### 7.10 Database Init Scripts

Run a SQL script once when a managed database is first provisioned. This eliminates
the need for services to self-bootstrap schema on first run:

```yaml
databases:
  - name: my-db
    plan: starter
    initScript: schema.sql    # path to SQL file in the repo (run once on first provision)
```

The init script runs as a one-time K8s pod using `psql` against the newly created
database. If it fails, the database is still marked available (the error is logged).
The script is NOT re-run on subsequent syncs.

### 7.11 Build Log Improvements

Auto-generated Dockerfiles are now printed in build logs before Kaniko runs, making
it easy to debug build failures:

```
=== Dockerfile (Dockerfile) ===
  FROM python:3.12-slim
  WORKDIR /app
  COPY . .
  RUN pip install --no-cache-dir -r requirements.txt
  ...
=== End Dockerfile ===
```

The `.dockerignore` contents (if generated from `buildInclude`/`buildExclude`) are
also printed in build logs.

---

## 8. Environment Variables & Secrets

### 8.1 Storage

- Env vars stored encrypted at rest in the system database (AES-256-GCM).
- Encryption key stored in `/etc/railpush/master.key` (file permissions `0600`, owned by root).
- Secrets (marked `generateValue: true` or `sync: false`) never exposed in logs or API responses.

### 8.2 Injection

- All env vars injected into the container at runtime via Docker `--env`.
- Special auto-injected variables:
  - `PORT` — the port the service should bind to.
  - `RENDER` — always `true` (compatibility).
  - `RENDER_SERVICE_NAME` — the service name.
  - `RENDER_SERVICE_TYPE` — `web`, `pserv`, `worker`, `cron`, `static`.
  - `RENDER_GIT_COMMIT` — the SHA of the deployed commit.
  - `RENDER_GIT_BRANCH` — the branch name.
  - `IS_PULL_REQUEST` — `true` if deployed from a PR preview.
  - `RENDER_DISCOVERY_SERVICE` — URL of the internal service discovery endpoint.

### 8.3 Environment Groups

- Named groups of env vars shareable across multiple services.
- A service can reference one or more `envGroups` by name.
- Changes to a group trigger redeploy of all referencing services (configurable).

### 8.4 Secret Files

- Mount arbitrary files (e.g., service account JSON, TLS certs) into the container.
- Stored encrypted in system DB.
- Mounted as Docker tmpfs secrets at the specified path.

---

## 9. GitHub Integration

### 9.1 OAuth App

- Users authenticate via GitHub OAuth.
- On first login, RailPush requests `repo` and `admin:repo_hook` scopes.
- Access tokens stored encrypted in system DB.
- Supports both personal repos and organization repos.

### 9.2 Webhook Flow

```
1. User connects repo → RailPush creates webhook on GitHub (push events, PR events)
2. GitHub push → POST /api/webhooks/github
3. Verify webhook signature (HMAC-SHA256 with webhook secret)
4. Extract: repo, branch, commit SHA, commit message, changed files
5. Match to registered services watching this repo+branch
6. For each matched service:
   a. Check build filters (path-based)
   b. Check for skip phrases in commit message
   c. If passes → enqueue build job
```

### 9.3 PR Preview Environments

- When a PR is opened against the watched branch, RailPush can spin up a preview environment.
- Preview gets a unique URL: `pr-<number>-<service-name>.<base-domain>`.
- Preview destroyed when PR is closed/merged.
- Blueprint-level previews: spin up an entire stack copy for a PR.
- Configurable: enabled per-service or per-Blueprint.

### 9.4 Deploy Status

- RailPush posts GitHub Commit Status / Check Runs for each deploy.
- Status: `pending` → `success` / `failure` / `error`.
- Links back to the deploy log in the dashboard.

---

## 10. Dashboard (Web UI)

### 10.1 Technology

- React 18 + TypeScript + Vite
- TailwindCSS for styling
- React Query for data fetching
- WebSocket for real-time log streaming and deploy status

### 10.2 Pages & Features

| Page | Description |
|---|---|
| **Login** | GitHub OAuth login |
| **Workspace Overview** | List all services, grouped by project. Status indicators. |
| **Service Detail** | Current deploy status, recent deploys, environment, logs, settings |
| **Create Service** | Wizard: choose repo → configure build → set env vars → deploy |
| **Deploy Detail** | Build log (streaming), deploy timeline, rollback button |
| **Logs** | Real-time log viewer with search, filter by service, time range |
| **Blueprints** | List of Blueprints, sync status, change history |
| **Blueprint Detail** | Rendered YAML, linked services, sync/apply controls |
| **Databases** | List managed Postgres instances, connection info, backups |
| **Key Value** | List managed Redis instances, connection info |
| **Environment Groups** | Manage shared env var groups |
| **Settings** | Custom domains, webhook config, deploy policies, notifications |
| **Shell** | Web-based terminal (SSH into running container via docker exec) |

### 10.3 Real-time Features

- **Build logs**: streamed line-by-line via WebSocket during active builds.
- **Service logs**: tail -f style streaming from running containers.
- **Deploy status**: live status transitions on all dashboard views.
- **Notifications**: Slack webhook + email (optional) for deploy success/failure.

---

## 11. API

### 11.1 REST API

Base URL: `https://<base-domain>/api/v1`

Authentication: Bearer token (API key generated in dashboard, or GitHub OAuth token).

#### Core Endpoints

```
# Services
GET    /services                       # List all services
POST   /services                       # Create service
GET    /services/:id                   # Get service details
PATCH  /services/:id                   # Update service
DELETE /services/:id                   # Delete service
POST   /services/:id/deploys           # Trigger manual deploy
GET    /services/:id/deploys           # List deploys
GET    /services/:id/deploys/:deployId # Deploy details
POST   /services/:id/deploys/:deployId/rollback  # Rollback
POST   /services/:id/restart           # Restart service
POST   /services/:id/suspend           # Suspend service
POST   /services/:id/resume            # Resume service

# Environment Variables
GET    /services/:id/env-vars          # List env vars
PUT    /services/:id/env-vars          # Bulk update env vars

# Logs
GET    /services/:id/logs              # Query logs (time range, search)
WS     /services/:id/logs/stream       # WebSocket log stream

# Custom Domains
POST   /services/:id/custom-domains    # Add custom domain
DELETE /services/:id/custom-domains/:domain  # Remove
GET    /services/:id/custom-domains    # List

# Databases
GET    /databases                      # List
POST   /databases                      # Create
GET    /databases/:id                  # Details + connection info
DELETE /databases/:id                  # Delete
GET    /databases/:id/backups          # List backups
POST   /databases/:id/backups          # Trigger backup

# Key Value
GET    /keyvalue                       # List
POST   /keyvalue                       # Create
GET    /keyvalue/:id                   # Details
DELETE /keyvalue/:id                   # Delete

# Blueprints
GET    /blueprints                     # List
POST   /blueprints                     # Create from repo
GET    /blueprints/:id                 # Details + sync status
POST   /blueprints/:id/sync           # Force sync
GET    /blueprints/:id/changes         # Pending changes

# Environment Groups
GET    /env-groups                     # List
POST   /env-groups                     # Create
PATCH  /env-groups/:id                 # Update
DELETE /env-groups/:id                 # Delete

# Webhooks (GitHub)
POST   /webhooks/github               # GitHub webhook receiver

# User
GET    /user                           # Current user info
POST   /user/api-keys                  # Generate API key
DELETE /user/api-keys/:id              # Revoke API key
```

### 11.2 WebSocket API

- `/ws/logs/:serviceId` — Real-time log stream
- `/ws/builds/:deployId` — Real-time build output
- `/ws/events` — Global event stream (deploys, failures, scaling events)

---

## 12. Persistent Storage

### 12.1 Persistent Disks

- Attach a Docker volume to a service at a specified mount path.
- Data persists across deploys.
- Disk survives container replacement (volume is re-mounted to new container).
- Constraint: services with persistent disks cannot use zero-downtime deploys (old container must stop before new one starts, to avoid mount conflicts).
- Configurable size limit (enforced via Docker volume driver or quota).

### 12.2 Directory Structure

```
/var/lib/railpush/
├── buildkit-cache/           # BuildKit layer cache
├── builds/                   # Cloned repos during build (ephemeral)
├── images/                   # Local Docker registry storage
├── static/                   # Static site assets
│   └── <service-name>/
│       ├── current -> deploy-abc123/
│       └── deploy-abc123/
├── disks/                    # Persistent disk volumes
│   └── <service-id>/
├── backups/                  # Database backups
│   ├── postgres/
│   └── redis/
├── logs/                     # Rotated log files
│   └── <service-name>/
└── secrets/                  # Encrypted env var store (system DB handles this)

/etc/railpush/
├── config.yaml               # Main configuration file
├── master.key                # Encryption key (0600 root:root)
└── caddy/
    └── Caddyfile             # Auto-generated
```

---

## 13. Logging & Monitoring

### 13.1 Log Collection

- Container stdout/stderr captured via Docker log driver (json-file).
- Log Collector tails Docker logs and indexes into a local log store.
- Retention: configurable (default 7 days, max 90 days).
- Searchable by service, time range, and keyword.

### 13.2 Metrics

- **Per-service**: CPU %, memory usage, network I/O, disk I/O — collected via Docker stats API.
- **Per-deploy**: build duration, deploy duration, success/failure.
- **System-level**: total CPU, memory, disk usage of the host.
- Metrics stored in a time-series table in system DB (or optional Prometheus export).

### 13.3 Health Checks

| Type | Description |
|---|---|
| HTTP | `GET` to `healthCheckPath`, expect 2xx response |
| TCP | Connection to service port succeeds |
| Startup | Grace period before health checks begin (default 30s) |

- Failed health checks during deploy → deploy aborted.
- Failed health checks on running service → alert + optional auto-restart.

### 13.4 Alerting

- **Slack webhook**: deploy success/failure, service crash, health check failure.
- **Email** (optional, via SMTP config): same events.
- **Webhook** (generic): POST to configurable URL with event payload.

---

## 14. Security

### 14.1 Network Security

- All public traffic over HTTPS (TLS 1.2+, auto-provisioned via Caddy/Let's Encrypt).
- Private services not exposed to the internet.
- Managed databases default to internal-only access.
- Optional: UFW/iptables rules auto-configured during install.

### 14.2 Container Isolation

- Each service runs in its own container with:
  - Read-only root filesystem (where possible).
  - No privileged mode.
  - Dropped Linux capabilities (no `NET_RAW`, `SYS_ADMIN`, etc.).
  - Memory and CPU limits enforced via Docker resource constraints.
  - No access to Docker socket.
- Containers run as non-root user (configurable, default UID 1000).

### 14.3 Secrets Management

- Env vars encrypted at rest (AES-256-GCM).
- Master key generated during install, stored with strict file permissions.
- Secrets never logged, never returned in API responses (masked).
- Secret files mounted as tmpfs (never written to disk in container layer).

### 14.4 Authentication & Authorization

- GitHub OAuth for user authentication.
- API keys (scoped: read-only, deploy-only, full-access) for programmatic use.
- Role-based access: Owner, Admin, Member (for multi-user workspaces).
- CSRF protection on all state-changing endpoints.
- Rate limiting: 100 req/min per API key (configurable).

### 14.5 DDoS Mitigation

- Caddy rate limiting plugin for per-IP request throttling.
- Optional: Cloudflare or other CDN in front for production deployments.

---

## 15. CLI Tool

A command-line interface (`railpush`) for managing services without the dashboard.

```bash
# Authentication
railpush login                      # Open browser for GitHub OAuth
railpush logout

# Services
railpush services list
railpush services create --name my-app --type web --repo https://github.com/...
railpush services info my-app
railpush services logs my-app --tail --since 1h
railpush services restart my-app
railpush services delete my-app

# Deploys
railpush deploys list --service my-app
railpush deploys trigger --service my-app
railpush deploys rollback --service my-app --deploy <deploy-id>
railpush deploys cancel --service my-app

# Environment
railpush env list --service my-app
railpush env set --service my-app KEY=value KEY2=value2
railpush env unset --service my-app KEY

# Blueprints
railpush blueprint validate railpush.yaml
railpush blueprint apply railpush.yaml
railpush blueprint sync

# Shell
railpush shell my-app               # Interactive shell in running container

# Databases
railpush db list
railpush db create --name my-db --plan standard
railpush db connect my-db            # Opens psql session
railpush db backup my-db
```

---

## 16. Configuration

### 16.1 `/etc/railpush/config.yaml`

```yaml
# RailPush Configuration

server:
  host: 0.0.0.0
  port: 8080
  baseDomain: deploy.example.com      # All services get <name>.deploy.example.com
  acmeEmail: admin@example.com        # For Let's Encrypt

database:
  host: localhost
  port: 5432
  name: railpush
  user: railpush
  password: ${RAILPUSH_DB_PASSWORD}  # from env or master.key derived

github:
  clientId: ${GITHUB_CLIENT_ID}
  clientSecret: ${GITHUB_CLIENT_SECRET}
  webhookSecret: ${GITHUB_WEBHOOK_SECRET}
  allowedOrgs: []                      # empty = all orgs allowed

docker:
  host: unix:///var/run/docker.sock
  networkName: railpush-private
  registryMirror: ""                   # optional local registry mirror
  buildkitCacheDir: /var/lib/railpush/buildkit-cache
  maxConcurrentBuilds: 2
  defaultMemoryLimit: "512m"
  defaultCpuLimit: "1.0"

deploy:
  healthCheckTimeout: 300              # seconds
  healthCheckInterval: 5               # seconds
  defaultShutdownDelay: 30             # seconds
  maxShutdownDelay: 300                # seconds
  overlappingPolicy: cancel            # cancel | wait

logging:
  retentionDays: 7
  maxSizePerService: "1GB"

backups:
  enabled: true
  schedule: "0 2 * * *"               # 2 AM daily
  retentionDays: 30
  storageDir: /var/lib/railpush/backups

notifications:
  slack:
    webhookUrl: ""
  email:
    enabled: false
    smtpHost: ""
    smtpPort: 587
    smtpUser: ""
    smtpPassword: ""
    fromAddress: ""

security:
  rateLimitPerMinute: 100
  apiKeyMaxAge: 365                    # days, 0 = no expiry
  containerRunAsUser: 1000
  containerReadOnlyRoot: true
```

---

## 17. Installation

### 17.1 Prerequisites

- Linux server (Ubuntu 22.04+ or Debian 12+) with root access.
- Public IP with ports 80 and 443 open.
- Domain name with wildcard DNS pointing to the server: `*.deploy.example.com → <server-ip>`.
- Minimum: 4 CPU cores, 8 GB RAM, 50 GB disk (for the platform itself; services need more).

### 17.2 Install Script

```bash
curl -fsSL https://get.railpush.com | bash
```

The installer:

1. Installs Docker Engine + BuildKit (if not present).
2. Installs PostgreSQL 16 and Redis 7 (for platform use).
3. Installs Caddy 2 with necessary plugins.
4. Creates system user `railpush`.
5. Generates master encryption key.
6. Initializes system database and runs migrations.
7. Creates systemd services: `railpush-api`, `railpush-builder`, `railpush-scheduler`.
8. Configures Caddy with base domain.
9. Opens firewall ports (80, 443).
10. Prompts for GitHub OAuth app credentials.
11. Starts all services.
12. Prints dashboard URL.

### 17.3 Systemd Services

```
railpush-api.service        # API server + dashboard
railpush-builder.service    # Build worker (consumes build queue)
railpush-scheduler.service  # Cron scheduler
railpush-logd.service       # Log collector daemon
caddy.service                 # Reverse proxy (managed by Caddy)
```

---

## 18. Data Model (System Database)

### 18.1 Core Tables

```sql
-- Users & Auth
users (id, github_id, username, email, avatar_url, role, created_at)
api_keys (id, user_id, name, key_hash, scopes[], expires_at, created_at)

-- Workspaces
workspaces (id, name, owner_id, deploy_policy, created_at)
workspace_members (workspace_id, user_id, role)

-- Projects & Environments
projects (id, workspace_id, name, created_at)
environments (id, project_id, name, is_protected, block_cross_env_network, created_at)

-- Services
services (
  id, workspace_id, project_id, environment_id,
  name, type, runtime,
  repo_url, branch, build_command, start_command,
  dockerfile_path, docker_context, image_url,
  health_check_path, health_check_timeout,
  port, auto_deploy, is_suspended,
  max_shutdown_delay, pre_deploy_command,
  build_filter_paths[], build_filter_ignored[],
  static_publish_path, headers_json, routes_json,
  schedule,  -- cron jobs only
  plan, instances,
  created_at, updated_at
)

-- Environment Variables
env_vars (id, owner_type, owner_id, key, encrypted_value, is_secret, sync, generate_value, created_at)

-- Environment Groups
env_groups (id, workspace_id, name, project_id, environment_id, created_at)
env_group_memberships (service_id, env_group_id)

-- Persistent Disks
disks (id, service_id, name, mount_path, size_gb, created_at)

-- Custom Domains
custom_domains (id, service_id, domain, verified, tls_provisioned, created_at)

-- Deploys
deploys (
  id, service_id,
  trigger,  -- git_push, manual, blueprint, rollback
  status,   -- pending, building, deploying, live, failed, cancelled
  commit_sha, commit_message, branch,
  image_tag, build_log_path,
  started_at, finished_at,
  created_by
)

-- Databases (Managed Postgres)
managed_databases (
  id, workspace_id, name, plan,
  pg_version, container_id,
  host, port, db_name, username, encrypted_password,
  ip_allow_list[],
  created_at
)

-- Key Value (Managed Redis)
managed_keyvalue (
  id, workspace_id, name, plan,
  container_id,
  host, port, encrypted_password,
  maxmemory_policy,
  ip_allow_list[],
  created_at
)

-- Blueprints
blueprints (
  id, workspace_id, name,
  repo_url, branch, file_path,
  last_synced_at, last_sync_status,
  created_at
)
blueprint_resources (blueprint_id, resource_type, resource_id, resource_name)

-- Database Backups
backups (id, resource_type, resource_id, file_path, size_bytes, started_at, finished_at, status)

-- Audit Log
audit_log (id, workspace_id, user_id, action, resource_type, resource_id, details_json, created_at)

-- Notifications
notification_channels (id, workspace_id, type, config_json, created_at)
```

---

## 19. Implementation Phases

### Phase 1 — Core Platform (MVP)

**Goal**: Deploy a web service from a GitHub repo with auto-deploy on push.

- [ ] System database schema + migrations
- [ ] GitHub OAuth login
- [ ] GitHub webhook receiver
- [ ] Service CRUD (web type only)
- [ ] Build pipeline: clone → detect → docker build → tag
- [ ] Deploy engine: run container → health check → swap → teardown
- [ ] Caddy integration: dynamic upstream management
- [ ] Environment variable management (encrypted)
- [ ] Basic dashboard: login, service list, create service, deploy log viewer
- [ ] Real-time build/deploy log streaming (WebSocket)
- [ ] CLI: login, services, deploys, env, logs

### Phase 2 — Full Service Types

- [ ] Private services
- [ ] Background workers
- [ ] Cron jobs (with scheduler + single-run guarantee)
- [ ] Static sites (build + Caddy file_server)
- [ ] Persistent disk support
- [ ] Custom domains with auto TLS

### Phase 3 — Managed Datastores

- [ ] Managed PostgreSQL (provision, connect, backup, restore)
- [ ] Managed Key Value / Redis (provision, connect, backup)
- [ ] Connection string injection via `fromDatabase` / `fromService`

### Phase 4 — Blueprints

- [ ] `railpush.yaml` parser + validator
- [ ] Blueprint CRUD in dashboard
- [ ] Diff engine (current state vs desired state)
- [ ] Apply engine (create/update/delete resources)
- [ ] Cross-service references (`fromService`, `fromDatabase`)
- [ ] Environment groups
- [ ] Auto-sync on git push

### Phase 5 — Advanced Features

- [ ] PR preview environments
- [ ] Projects & environments (with network isolation)
- [ ] Rollback to any previous deploy
- [ ] Pre-deploy commands
- [ ] Deploy skip phrases and build filters
- [ ] Shell access (web terminal)
- [ ] Metrics dashboard (CPU, memory, network)
- [ ] Slack/email/webhook notifications
- [ ] API keys with scoped permissions
- [ ] Audit log
- [ ] Multi-user workspace with RBAC

### Phase 6 — Hardening

- [ ] Automated database backups with retention
- [ ] Log rotation and retention policies
- [ ] Rate limiting
- [ ] Container security hardening (read-only root, capability drops)
- [ ] Resource quotas per service / per workspace
- [ ] Build cache management and pruning
- [ ] Graceful host restart (restart all services in order)
- [ ] Monitoring alerts for host resource exhaustion
- [ ] Documentation site

---

## 20. Non-Goals (v1)

These are explicitly out of scope for the initial release:

- **Multi-node / clustering**: v1 is single-server only.
- **Autoscaling**: manual instance count only (always 1 in v1).
- **Global CDN**: static sites served from the single server, not a CDN edge.
- **Multi-region**: single region (the server's location).
- **Billing/metering**: no usage tracking or payment processing.
- **GitLab/Bitbucket support**: GitHub only in v1.
- **Kubernetes backend**: Docker only in v1.
- **SOC 2 / HIPAA compliance**: not a goal for self-hosted v1.
- **Marketplace/templates**: no one-click deploy templates.

---

## 21. Technology Choices & Rationale

| Choice | Rationale |
|---|---|
| **Go** for API server | Fast, single binary, excellent Docker SDK, low resource usage |
| **Docker + BuildKit** | Industry standard containerization, excellent build caching, no Kubernetes overhead |
| **Caddy** | Automatic HTTPS, hot-reloadable config via API, HTTP/2+3, simpler than Nginx+Certbot |
| **PostgreSQL** | System DB and managed DB offering — one technology to maintain |
| **Redis/Valkey** | Build queue, pub/sub for log streaming, managed KV offering |
| **React + Vite** | Fast dashboard development, good ecosystem for real-time UIs |
| **systemd** | Native Linux service management, auto-restart, logging integration |

---

## Appendix A: Render.com Compatibility

RailPush aims for **behavioral compatibility** with Render.com:

- Blueprint files written for Render.com should work on RailPush with minimal changes.
- Environment variable names (`PORT`, `RENDER`, `RENDER_*`) match Render's conventions.
- Deploy lifecycle (build → pre-deploy → start → health check) matches Render's sequence.
- Service types and their behaviors match Render's documentation.

**Known differences**:
- No `onrender.com` subdomain — uses your configured `baseDomain` instead.
- No Render-managed DNS — you manage your own DNS.
- No integrated CI checks gating — deploy on push only (or manual).
- No usage-based billing — it's your server, use what you have.
