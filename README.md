# RailPush

Self-hosted PaaS platform. Push code, get production infrastructure — services, databases, domains, secrets, and scaling from a single console.

## What it does

- **Web services, workers, cron jobs, static sites** — deploy from a Git repo or Docker image
- **Managed PostgreSQL & Redis** — one-click provisioning with backups, replicas, and HA
- **Blueprints (IaC)** — declare your entire stack in a `railpush.yaml` file
- **Custom domains & TLS** — automatic certificate provisioning
- **Autoscaling** — CPU/memory-based scaling policies
- **MCP server** — let AI agents (Claude, ChatGPT, Cursor) manage your infrastructure through natural language

## Architecture

- **API**: Go (net/http + gorilla/mux), PostgreSQL (CNPG)
- **Dashboard**: React + TypeScript + Tailwind CSS
- **Runtime**: k3s Kubernetes cluster with Kustomize
- **Builds**: Kaniko (in-cluster image builds)
- **Ingress**: nginx ingress controller with automatic TLS

## Quick start

```bash
# Deploy the control plane
kubectl apply -k deploy/k8s/control-plane/

# Or use the production overlay
kubectl apply -k deploy/k8s/control-plane-overlays/prod-cnpg-cutover/
```

See the [documentation](https://railpush.com/docs) for full setup instructions.

## MCP server

AI agents can manage the platform through the built-in MCP server (50 tools covering services, deploys, databases, env vars, blueprints, and more).

```bash
cd mcp && npm install && npm run build
```

Configure in Claude Desktop, Claude Code, or Cursor:

```json
{
  "mcpServers": {
    "railpush": {
      "command": "node",
      "args": ["/path/to/RAILPUSH/mcp/build/index.js"],
      "env": {
        "RAILPUSH_API_KEY": "your-api-key",
        "RAILPUSH_API_URL": "https://apps.railpush.com"
      }
    }
  }
}
```

## Project structure

```
api/            Go API server (handlers, models, services, middleware)
dashboard/      React frontend (Vite + Tailwind)
mcp/            MCP server for AI agents (TypeScript)
cli/            CLI tool (Go)
deploy/         Kubernetes manifests (Kustomize)
scripts/        Utility scripts
```

## Documentation

- [Docs](https://railpush.com/docs) — guides for services, databases, blueprints, deploys, MCP, and API
- [SPEC.md](SPEC.md) — full technical specification
- [Blueprint reference](https://railpush.com/docs#blueprint-spec) — `railpush.yaml` schema

## License

Proprietary. All rights reserved.
