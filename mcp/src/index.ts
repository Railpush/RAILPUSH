#!/usr/bin/env node
/**
 * RailPush MCP Server
 *
 * Exposes the full RailPush PaaS platform as MCP tools so AI agents
 * (Claude, ChatGPT, etc.) can create services, trigger deploys,
 * manage databases, configure env vars, and more — all through
 * natural language.
 *
 * Environment variables:
 *   RAILPUSH_API_URL  — API base URL  (default: https://apps.railpush.com)
 *   RAILPUSH_API_KEY  — API key       (required)
 *   RAILPUSH_API_VERSION_PIN — Version pin header value (default: v1)
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { RailPushClient, RailPushAPIError } from "./client.js";

// ── Helpers ────────────────────────────────────────────────────────────

function text(data: unknown): { content: Array<{ type: "text"; text: string }> } {
  return { content: [{ type: "text" as const, text: JSON.stringify(data, null, 2) }] };
}

function err(e: unknown): { content: Array<{ type: "text"; text: string }>; isError: true } {
  if (e instanceof RailPushAPIError) {
    return { content: [{ type: "text" as const, text: `API Error ${e.status}: ${e.body}` }], isError: true };
  }
  return { content: [{ type: "text" as const, text: String(e) }], isError: true };
}

type LogFilterOptions = {
  search?: string;
  regex?: boolean;
  since?: string;
  until?: string;
  level?: string;
};

function parseFilterTime(raw?: string): Date | null {
  if (!raw) return null;
  const parsed = new Date(raw);
  if (Number.isNaN(parsed.getTime())) {
    throw new Error(`Invalid timestamp '${raw}'. Use RFC3339 format (example: 2026-02-24T10:00:00Z).`);
  }
  return parsed;
}

function getLogTimestamp(entry: Record<string, unknown>): Date | null {
  const candidates = [entry.timestamp, entry.started_at, entry.created_at, entry.updated_at];
  for (const value of candidates) {
    if (typeof value === "string" || typeof value === "number") {
      const parsed = new Date(value);
      if (!Number.isNaN(parsed.getTime())) return parsed;
    }
  }
  return null;
}

function getLogText(entry: Record<string, unknown>): string {
  if (typeof entry.message === "string") return entry.message;
  if (typeof entry.log === "string") return entry.log;
  return JSON.stringify(entry);
}

function getLogLevel(entry: Record<string, unknown>): string {
  if (typeof entry.level === "string" && entry.level.trim() !== "") {
    return entry.level.toLowerCase();
  }
  if (typeof entry.status === "string") {
    const status = entry.status.toLowerCase();
    if (status.includes("error") || status.includes("fail")) return "error";
    if (status.includes("warn")) return "warn";
  }
  const textValue = getLogText(entry).toLowerCase();
  if (textValue.includes("panic") || textValue.includes("fatal") || textValue.includes("error")) return "error";
  if (textValue.includes("warn")) return "warn";
  if (textValue.includes("debug")) return "debug";
  return "info";
}

function filterLogEntries(logs: unknown, options: LogFilterOptions): Array<Record<string, unknown>> {
  if (!Array.isArray(logs)) return [];

  const search = options.search?.trim() ?? "";
  const useRegex = Boolean(options.regex);
  const searchLower = search.toLowerCase();
  const regex = (search !== "" && useRegex) ? new RegExp(search, "i") : null;
  const since = parseFilterTime(options.since);
  const until = parseFilterTime(options.until);
  const level = options.level?.trim().toLowerCase();

  if (since && until && since.getTime() > until.getTime()) {
    throw new Error("'since' must be earlier than or equal to 'until'.");
  }

  const out: Array<Record<string, unknown>> = [];
  for (const item of logs) {
    if (!item || typeof item !== "object" || Array.isArray(item)) continue;
    const entry = item as Record<string, unknown>;
    const textValue = getLogText(entry);

    if (search !== "") {
      const matches = regex ? regex.test(textValue) : textValue.toLowerCase().includes(searchLower);
      if (!matches) continue;
    }

    const ts = getLogTimestamp(entry);
    if (since && (!ts || ts.getTime() < since.getTime())) continue;
    if (until && (!ts || ts.getTime() > until.getTime())) continue;

    if (level) {
      const entryLevel = getLogLevel(entry);
      if (entryLevel !== level) continue;
    }

    out.push(entry);
  }

  return out;
}

// ── Bootstrap ──────────────────────────────────────────────────────────

const apiUrl = process.env.RAILPUSH_API_URL ?? "https://apps.railpush.com";
const apiKey = process.env.RAILPUSH_API_KEY ?? "";
const apiVersionPin = process.env.RAILPUSH_API_VERSION_PIN ?? "v1";

if (!apiKey) {
  console.error("RAILPUSH_API_KEY is required. Set it as an environment variable.");
  process.exit(1);
}

const client = new RailPushClient({ apiUrl, apiKey, apiVersionPin });

const server = new McpServer({
  name: "railpush",
  version: "1.0.0",
});

// ════════════════════════════════════════════════════════════════════════
//  AUTH / IDENTITY
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "whoami",
  "Get the currently authenticated user's profile — name, email, workspace, and role. Useful for verifying API key validity and checking permissions.",
  {},
  async () => {
    try { return text(await client.getCurrentUser()); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_api_version_info",
  "Get API version metadata (current version, supported pins, and pinning headers/query parameters).",
  {},
  async () => {
    try { return text(await client.getAPIVersionInfo()); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_api_version_changelog",
  "Get API version changelog entries used for compatibility planning and migrations.",
  {},
  async () => {
    try { return text(await client.getAPIVersionChangelog()); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_rate_limit",
  "Get current API rate-limit state for this API key (limit, remaining, reset_at, window).",
  {},
  async () => {
    try { return text(await client.getRateLimitInfo()); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  SERVICES
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_services",
  "List services in the workspace with optional server-side filters (type/status/runtime/plan/name/repo/project/query/suspended). Supports cursor pagination via limit/cursor.",
  {
    workspace_id: z.string().optional().describe("Workspace ID (uses default if omitted)"),
    type: z.enum(["web", "worker", "cron", "static", "pserv"]).optional().describe("Filter by service type"),
    status: z.string().optional().describe("Filter by status (e.g. live, deploy_failed, suspended)"),
    runtime: z.string().optional().describe("Filter by runtime (e.g. node, python, go, docker)"),
    plan: z.enum(["free", "starter", "standard", "pro"]).optional().describe("Filter by plan"),
    name: z.string().optional().describe("Filter by name (substring match)"),
    repo_url: z.string().optional().describe("Filter by repository URL (substring match)"),
    project_id: z.string().optional().describe("Filter by project ID"),
    query: z.string().optional().describe("Generic search over service name/repo/runtime/type/branch"),
    suspended: z.boolean().optional().describe("Filter by suspension state"),
    limit: z.number().int().positive().max(100).optional().describe("Page size for cursor pagination (max 100)"),
    cursor: z.string().optional().describe("Opaque cursor returned by previous page"),
  },
  async ({ workspace_id, type, status, runtime, plan, name, repo_url, project_id, query, suspended, limit, cursor }) => {
    try {
      return text(await client.listServices({
        workspace_id,
        type,
        status,
        runtime,
        plan,
        name,
        repo_url,
        project_id,
        query,
        suspended: suspended !== undefined ? String(suspended) : undefined,
        limit,
        cursor,
      }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_service",
  "Get full details of a service by ID, including its configuration, status, public URL, and deploy settings.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.getService(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_service",
  "Create a new service. Specify at minimum name + type. The service can be a web server, worker, cron job, or static site. Provide repo_url to deploy from a Git repo.",
  {
    name: z.string().describe("Service name (must be unique, lowercase, alphanumeric + hyphens)"),
    type: z.enum(["web", "worker", "cron", "static", "cron_job"]).describe("Service type"),
    runtime: z.enum(["node", "python", "go", "docker", "static"]).optional().describe("Runtime (auto-detected if not set)"),
    repo_url: z.string().optional().describe("Git repository URL"),
    branch: z.string().optional().describe("Git branch (default: main)"),
    build_command: z.string().optional().describe("Build command"),
    start_command: z.string().optional().describe("Start command"),
    dockerfile_path: z.string().optional().describe("Path to Dockerfile (relative to repo root)"),
    docker_context: z.string().optional().describe("Build context directory (alias: build_context)"),
    build_context: z.string().optional().describe("Build context directory (alias for docker_context)"),
    image_url: z.string().optional().describe("Pre-built Docker image URL (skips build)"),
    health_check_path: z.string().optional().describe("HTTP health check path (e.g. /healthz)"),
    port: z.number().optional().describe("Port the service listens on (default: 10000)"),
    plan: z.enum(["free", "starter", "standard", "pro"]).optional().describe("Plan tier (default: starter)"),
    instances: z.number().optional().describe("Number of instances (default: 1)"),
    static_publish_path: z.string().optional().describe("For static sites: directory to serve (default: dist)"),
    schedule: z.string().optional().describe("Cron schedule expression (for cron/cron_job types)"),
    auto_deploy: z.boolean().optional().describe("Auto-deploy on push (default: true)"),
    pre_deploy_command: z.string().optional().describe("Command to run before deploy (e.g. migrations)"),
    base_image: z.string().optional().describe("Custom base Docker image"),
    workspace_id: z.string().optional().describe("Workspace ID"),
  },
  async ({ build_context, ...args }) => {
    if (build_context && !args.docker_context) (args as Record<string, unknown>).docker_context = build_context;
    try { return text(await client.createService(args as Record<string, unknown>)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_service",
  "Update a service's configuration. Only provided fields are changed. Use this to change branch, build/start commands, scaling plan, port, project assignment, and more.",
  {
    service_id: z.string().describe("Service ID"),
    name: z.string().optional().describe("New service name"),
    project_id: z.string().nullable().optional().describe("Project ID to assign the service to (set to null to unassign)"),
    environment_id: z.string().nullable().optional().describe("Environment ID within the project (set to null to unassign)"),
    branch: z.string().optional().describe("Git branch"),
    build_command: z.string().optional().describe("Build command"),
    start_command: z.string().optional().describe("Start command"),
    port: z.number().optional().describe("Port"),
    auto_deploy: z.boolean().optional().describe("Auto-deploy on push"),
    plan: z.enum(["free", "starter", "standard", "pro"]).optional().describe("Plan tier"),
    instances: z.number().optional().describe("Number of instances"),
    dockerfile_path: z.string().optional().describe("Dockerfile path"),
    docker_context: z.string().optional().describe("Build context directory (alias: build_context)"),
    build_context: z.string().optional().describe("Build context directory (alias for docker_context)"),
    image_url: z.string().optional().describe("Pre-built image URL"),
    health_check_path: z.string().optional().describe("Health check path"),
    pre_deploy_command: z.string().optional().describe("Pre-deploy command"),
    static_publish_path: z.string().optional().describe("Static publish path"),
    schedule: z.string().optional().describe("Cron schedule"),
    max_shutdown_delay: z.number().optional().describe("Max shutdown delay seconds"),
    deletion_protection: z.boolean().optional().describe("Enable/disable deletion protection for this service"),
  },
  async ({ service_id, build_context, ...updates }) => {
    if (build_context && !updates.docker_context) (updates as Record<string, unknown>).docker_context = build_context;
    try {
      // Keep null values for project_id/environment_id (they mean "unassign"), but filter out undefined
      const data: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(updates)) {
        if (v !== undefined) data[k] = v;
      }
      return text(await client.updateService(service_id, data));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_service_retention",
  "Get retention policy for service logs. Build log retention is enforced; runtime/request retention values are stored for policy tracking.",
  {
    service_id: z.string().describe("Service ID"),
  },
  async ({ service_id }) => {
    try { return text(await client.getServiceRetention(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "set_service_retention",
  "Update service log retention windows. Values accept day numbers (e.g. 30) or strings like 30d, 12w, 6m, 1y.",
  {
    service_id: z.string().describe("Service ID"),
    runtime_logs: z.union([z.number().int().positive(), z.string()]).optional().describe("Runtime log retention (e.g. 30d)"),
    build_logs: z.union([z.number().int().positive(), z.string()]).optional().describe("Build log retention (e.g. 90d)"),
    request_logs: z.union([z.number().int().positive(), z.string()]).optional().describe("Request log retention (e.g. 14d)"),
  },
  async ({ service_id, runtime_logs, build_logs, request_logs }) => {
    try {
      return text(await client.setServiceRetention(service_id, {
        runtime_logs,
        build_logs,
        request_logs,
      }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_service",
  "Delete a service using a safeguarded flow. First confirmation soft-deletes the service into a 72-hour recovery window. Optional hard_delete=true is only for permanent deletion after that window.",
  {
    service_id: z.string().describe("Service ID"),
    confirm_destructive: z.boolean().describe("Must be true to proceed with delete"),
    hard_delete: z.boolean().optional().describe("Set true only when you intend permanent deletion after recovery window"),
  },
  async ({ service_id, confirm_destructive, hard_delete }) => {
    if (!confirm_destructive) {
      return text({
        status: "blocked",
        reason: "confirm_destructive must be true",
      });
    }
    try { return text(await client.deleteService(service_id, hard_delete)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "restore_service",
  "Restore a soft-deleted service from the recovery window. Restored services come back suspended and must be resumed to run.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.restoreService(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "restart_service",
  "Restart a running service. The existing containers are stopped and new ones started with the current image.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.restartService(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "suspend_service",
  "Suspend a service, stopping all its containers. The service remains configured and can be resumed later.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.suspendService(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "resume_service",
  "Resume a suspended service, triggering a new deploy.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.resumeService(service_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  SEARCH & BULK OPERATIONS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "search_services",
  "Search and filter services. All filters are optional and combined with AND. Name filter uses substring match. Supports cursor pagination via limit/cursor.",
  {
    workspace_id: z.string().optional().describe("Workspace ID (uses default if omitted)"),
    type: z.enum(["web", "worker", "cron", "static", "pserv"]).optional().describe("Filter by service type"),
    status: z.string().optional().describe("Filter by status (e.g. live, created, suspended)"),
    runtime: z.string().optional().describe("Filter by runtime (e.g. node, python, go, docker)"),
    plan: z.enum(["free", "starter", "standard", "pro"]).optional().describe("Filter by plan tier"),
    name: z.string().optional().describe("Filter by name (substring match, case-insensitive)"),
    repo_url: z.string().optional().describe("Filter by repository URL (substring match, case-insensitive)"),
    project_id: z.string().optional().describe("Filter by project ID"),
    query: z.string().optional().describe("Generic search across service name/repo/runtime/type/branch"),
    suspended: z.boolean().optional().describe("Filter by suspension state"),
    limit: z.number().int().positive().max(100).optional().describe("Page size for cursor pagination (max 100)"),
    cursor: z.string().optional().describe("Opaque cursor returned by previous page"),
  },
  async ({ workspace_id, type, status, runtime, plan, name, repo_url, project_id, query, suspended, limit, cursor }) => {
    try {
      return text(await client.searchServices({
        workspace_id,
        type,
        status,
        runtime,
        plan,
        name,
        repo_url,
        project_id,
        query,
        suspended: suspended !== undefined ? String(suspended) : undefined,
        limit,
        cursor,
      }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "search_workspace_resources",
  "Search services, databases, and key-value stores in one call using a workspace-scoped query.",
  {
    q: z.string().describe("Search query text"),
    workspace_id: z.string().optional().describe("Workspace ID (uses default if omitted)"),
    limit: z.number().optional().describe("Max matches per resource type (default: 20, max: 100)"),
  },
  async ({ q, workspace_id, limit }) => {
    try { return text(await client.searchWorkspaceResources({ q, workspace_id, limit })); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  RELATIONSHIPS / TOPOLOGY
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "get_workspace_topology",
  "Return a dependency graph for a workspace (nodes + edges) covering services, databases, and key-value stores.",
  {
    workspace_id: z.string().optional().describe("Workspace ID (uses default if omitted)"),
  },
  async ({ workspace_id }) => {
    try { return text(await client.getWorkspaceTopology(workspace_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_service_dependencies",
  "Return databases, key-value stores, and services a service depends on (detected via explicit links and env-var references).",
  {
    service_id: z.string().describe("Service ID"),
  },
  async ({ service_id }) => {
    try { return text(await client.getServiceDependencies(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_database_connected_services",
  "List services connected to a database, including the env var / link source used to detect each connection.",
  {
    database_id: z.string().describe("Database ID"),
  },
  async ({ database_id }) => {
    try { return text(await client.getDatabaseConnectedServices(database_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_database_impact",
  "Estimate blast radius for a database by listing affected services if that database becomes unavailable.",
  {
    database_id: z.string().describe("Database ID"),
  },
  async ({ database_id }) => {
    try { return text(await client.getDatabaseImpact(database_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "bulk_update_services",
  "Apply the same service configuration updates to multiple services in one API call. Returns per-service status and errors.",
  {
    service_ids: z.array(z.string()).min(1).max(200).describe("Array of service IDs to update"),
    changes: z.object({
      name: z.string().optional(),
      project_id: z.string().nullable().optional(),
      environment_id: z.string().nullable().optional(),
      branch: z.string().optional(),
      build_command: z.string().optional(),
      start_command: z.string().optional(),
      port: z.number().optional(),
      auto_deploy: z.boolean().optional(),
      plan: z.enum(["free", "starter", "standard", "pro"]).optional(),
      instances: z.number().optional(),
      dockerfile_path: z.string().optional(),
      docker_context: z.string().optional(),
      build_context: z.string().optional(),
      image_url: z.string().optional(),
      health_check_path: z.string().optional(),
      pre_deploy_command: z.string().optional(),
      static_publish_path: z.string().optional(),
      schedule: z.string().optional(),
      max_shutdown_delay: z.number().optional(),
    }).passthrough().describe("Partial update payload applied to each service"),
    dry_run: z.boolean().optional().describe("Validate only; do not apply changes"),
    transaction_mode: z.enum(["best_effort", "all_or_nothing"]).optional().describe("best_effort (default) or all_or_nothing preflight"),
    transactional: z.boolean().optional().describe("Alias for transaction_mode=all_or_nothing when true"),
  },
  async ({ service_ids, changes, dry_run, transaction_mode, transactional }) => {
    const payload = { ...changes } as Record<string, unknown>;
    if (typeof payload.build_context === "string" && !payload.docker_context) {
      payload.docker_context = payload.build_context;
    }
    return text(await client.bulkUpdateServices({
      ids: service_ids,
      changes: payload,
      dry_run,
      transaction_mode,
      transactional,
    }));
  },
);

server.tool(
  "bulk_restart",
  "Restart multiple services at once. Returns results for each service.",
  {
    service_ids: z.array(z.string()).describe("Array of service IDs to restart"),
    dry_run: z.boolean().optional().describe("Validate only; do not restart services"),
    transaction_mode: z.enum(["best_effort", "all_or_nothing"]).optional().describe("best_effort (default) or all_or_nothing preflight"),
    transactional: z.boolean().optional().describe("Alias for transaction_mode=all_or_nothing when true"),
  },
  async ({ service_ids, dry_run, transaction_mode, transactional }) => {
    try {
      return text(await client.bulkRestartServices({
        ids: service_ids,
        dry_run,
        transaction_mode,
        transactional,
      }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "bulk_deploy",
  "Trigger deploys for multiple services at once. Returns results for each service.",
  {
    service_ids: z.array(z.string()).describe("Array of service IDs to deploy"),
    commit_sha: z.string().optional().describe("Optional commit SHA to deploy across all selected services"),
    branch: z.string().optional().describe("Optional branch override to deploy across all selected services"),
    dry_run: z.boolean().optional().describe("Validate only; do not create deploys"),
    transaction_mode: z.enum(["best_effort", "all_or_nothing"]).optional().describe("best_effort (default) or all_or_nothing preflight"),
    transactional: z.boolean().optional().describe("Alias for transaction_mode=all_or_nothing when true"),
  },
  async ({ service_ids, commit_sha, branch, dry_run, transaction_mode, transactional }) => {
    try {
      return text(await client.bulkDeployServices({
        ids: service_ids,
        commit_sha,
        branch,
        dry_run,
        transaction_mode,
        transactional,
      }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "bulk_set_env_vars",
  "Update environment variables for multiple services in one API call. Supports merge mode (upsert/delete) and replace mode.",
  {
    service_ids: z.array(z.string()).min(1).max(200).describe("Array of service IDs"),
    env_vars: z.array(z.object({
      key: z.string().describe("Variable name"),
      value: z.string().describe("Variable value"),
      is_secret: z.boolean().optional().describe("Mark as secret"),
    })).describe("Environment variables payload"),
    mode: z.enum(["merge", "replace"]).optional().describe("merge (default) or replace"),
    delete: z.array(z.string()).optional().describe("In merge mode, keys to delete"),
    confirm_destructive: z.boolean().optional().describe("Required for replace mode when existing keys would be removed"),
    dry_run: z.boolean().optional().describe("Validate only; do not change env vars"),
    transaction_mode: z.enum(["best_effort", "all_or_nothing"]).optional().describe("best_effort (default) or all_or_nothing preflight"),
    transactional: z.boolean().optional().describe("Alias for transaction_mode=all_or_nothing when true"),
  },
  async ({ service_ids, env_vars, mode, delete: deleteKeys, confirm_destructive, dry_run, transaction_mode, transactional }) => {
    try {
      return text(await client.bulkSetServiceEnvVars({
        ids: service_ids,
        env_vars,
        mode,
        delete: deleteKeys,
        confirm_destructive,
        dry_run,
        transaction_mode,
        transactional,
      }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "bulk_suspend",
  "Suspend multiple services at once. Returns results for each service.",
  {
    service_ids: z.array(z.string()).describe("Array of service IDs to suspend"),
  },
  async ({ service_ids }) => {
    const results: Array<{ id: string; status: string; error?: string }> = [];
    for (const id of service_ids) {
      try { await client.suspendService(id); results.push({ id, status: "suspended" }); }
      catch (e) { results.push({ id, status: "failed", error: e instanceof Error ? e.message : String(e) }); }
    }
    return text(results);
  },
);

server.tool(
  "bulk_resume",
  "Resume multiple suspended services at once. Returns results for each service.",
  {
    service_ids: z.array(z.string()).describe("Array of service IDs to resume"),
  },
  async ({ service_ids }) => {
    const results: Array<{ id: string; status: string; error?: string }> = [];
    for (const id of service_ids) {
      try { await client.resumeService(id); results.push({ id, status: "resumed" }); }
      catch (e) { results.push({ id, status: "failed", error: e instanceof Error ? e.message : String(e) }); }
    }
    return text(results);
  },
);

// ════════════════════════════════════════════════════════════════════════
//  DEPLOYS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "trigger_deploy",
  "Trigger a new deploy for a service. Optionally specify a commit SHA or branch. The deploy builds the image from the repo and rolls it out.",
  {
    service_id: z.string().describe("Service ID"),
    commit_sha: z.string().optional().describe("Specific commit SHA to deploy"),
    branch: z.string().optional().describe("Branch to deploy (defaults to service branch)"),
  },
  async ({ service_id, ...opts }) => {
    try { return text(await client.triggerDeploy(service_id, opts)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "list_deploys",
  "List deploy history for a service with optional status/branch/time filtering. Supports cursor pagination via limit/cursor.",
  {
    service_id: z.string().describe("Service ID"),
    status: z.string().optional().describe("Filter by deploy status (e.g. pending, building, live, failed)"),
    branch: z.string().optional().describe("Filter by git branch"),
    since: z.string().optional().describe("Only include deploys at/after this RFC3339 timestamp"),
    until: z.string().optional().describe("Only include deploys at/before this RFC3339 timestamp"),
    limit: z.number().int().positive().max(100).optional().describe("Page size for cursor pagination (max 100)"),
    cursor: z.string().optional().describe("Opaque cursor returned by previous page"),
  },
  async ({ service_id, status, branch, since, until, limit, cursor }) => {
    try { return text(await client.listDeploys(service_id, { status, branch, since, until, limit, cursor })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "wait_for_deploy",
  "Block until a deploy reaches a terminal state (live/failed/canceled) or timeout expires.",
  {
    service_id: z.string().describe("Service ID"),
    deploy_id: z.string().describe("Deploy ID"),
    timeout_seconds: z.number().optional().describe("Wait timeout in seconds (default: 300, max: 1800)"),
  },
  async ({ service_id, deploy_id, timeout_seconds }) => {
    try { return text(await client.waitForDeploy(service_id, deploy_id, timeout_seconds)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_deploy",
  "Get detailed information about a specific deploy, including build log, status, timing, and any Dockerfile override.",
  {
    service_id: z.string().describe("Service ID"),
    deploy_id: z.string().describe("Deploy ID"),
  },
  async ({ service_id, deploy_id }) => {
    try { return text(await client.getDeploy(service_id, deploy_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "rollback_deploy",
  "Rollback a service to a previous deploy. Creates a new deploy using the image from the specified deploy ID.",
  {
    service_id: z.string().describe("Service ID"),
    deploy_id: z.string().describe("Deploy ID to roll back to"),
  },
  async ({ service_id, deploy_id }) => {
    try { return text(await client.rollbackDeploy(service_id, deploy_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_deploy_queue_position",
  "Check a deploy's position in the build queue. Useful when a deploy is queued and you want to know how long until it starts building.",
  {
    service_id: z.string().describe("Service ID"),
    deploy_id: z.string().describe("Deploy ID"),
  },
  async ({ service_id, deploy_id }) => {
    try { return text(await client.getDeployQueuePosition(service_id, deploy_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "enable_github_actions_deploy_gate",
  "Enable GitHub Actions-gated auto-deploy for a service. Ensures auto_deploy is enabled and ignores push webhooks for this service until a successful workflow_run event is received.",
  {
    service_id: z.string().describe("Service ID"),
    workflows: z.array(z.string()).optional().describe("Optional workflow names allowlist (e.g. ['CI', 'Release'])"),
  },
  async ({ service_id, workflows }) => {
    try {
      const envVars = [{ key: "RAILPUSH_GITHUB_ACTIONS_AUTO_DEPLOY", value: "true", is_secret: false }];
      const workflowNames = (workflows ?? []).map((w) => w.trim()).filter(Boolean);
      if (workflowNames.length > 0) {
        envVars.push({ key: "RAILPUSH_GITHUB_ACTIONS_WORKFLOWS", value: workflowNames.join(", "), is_secret: false });
      }
      const deleteKeys = [
        "RAILPUSH_GITHUB_ACTIONS_ENABLED",
        "RAILPUSH_DEPLOY_ON_GITHUB_ACTIONS",
        "RAILPUSH_GITHUB_ACTIONS_WORKFLOW",
      ];
      if (workflowNames.length === 0) {
        deleteKeys.push("RAILPUSH_GITHUB_ACTIONS_WORKFLOWS");
      }
      const service = await client.updateService(service_id, { auto_deploy: true });
      const env_update = await client.mergeEnvVars(service_id, envVars, deleteKeys);
      return text({ service, env_update, workflows: workflowNames });
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "disable_github_actions_deploy_gate",
  "Disable GitHub Actions-gated auto-deploy for a service and return to push-webhook based auto-deploy behavior.",
  {
    service_id: z.string().describe("Service ID"),
  },
  async ({ service_id }) => {
    try {
      return text(await client.mergeEnvVars(service_id, [], [
        "RAILPUSH_GITHUB_ACTIONS_AUTO_DEPLOY",
        "RAILPUSH_GITHUB_ACTIONS_ENABLED",
        "RAILPUSH_DEPLOY_ON_GITHUB_ACTIONS",
        "RAILPUSH_GITHUB_ACTIONS_WORKFLOW",
        "RAILPUSH_GITHUB_ACTIONS_WORKFLOWS",
      ]));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_github_actions_deploy_gate",
  "Get GitHub Actions deploy-gate status for a service, including effective deploy mode and workflow allowlist.",
  {
    service_id: z.string().describe("Service ID"),
  },
  async ({ service_id }) => {
    try {
      const service = await client.getService(service_id) as Record<string, unknown>;
      const envVars = await client.listEnvVars(service_id) as Array<Record<string, unknown>>;

      const parseBool = (raw: string): boolean => {
        const normalized = raw.trim().toLowerCase();
        return ["1", "true", "yes", "on", "y"].includes(normalized);
      };

      const byKey = new Map<string, string>();
      for (const row of envVars) {
        const key = typeof row.key === "string" ? row.key.trim().toUpperCase() : "";
        if (!key) continue;
        const value = typeof row.value === "string" ? row.value.trim() : "";
        byKey.set(key, value);
      }

      const gateEnabled =
        parseBool(byKey.get("RAILPUSH_GITHUB_ACTIONS_AUTO_DEPLOY") ?? "") ||
        parseBool(byKey.get("RAILPUSH_GITHUB_ACTIONS_ENABLED") ?? "") ||
        parseBool(byKey.get("RAILPUSH_DEPLOY_ON_GITHUB_ACTIONS") ?? "");

      const workflowRaw = (byKey.get("RAILPUSH_GITHUB_ACTIONS_WORKFLOWS") || byKey.get("RAILPUSH_GITHUB_ACTIONS_WORKFLOW") || "").trim();
      const workflows = workflowRaw.split(",").map((w) => w.trim()).filter(Boolean);
      const autoDeploy = Boolean(service["auto_deploy"]);
      const mode = !autoDeploy ? "off" : (gateEnabled ? "workflow_success" : "push");

      return text({
        service_id,
        mode,
        auto_deploy: autoDeploy,
        gate_enabled: gateEnabled,
        workflows,
        env_flags: {
          RAILPUSH_GITHUB_ACTIONS_AUTO_DEPLOY: byKey.get("RAILPUSH_GITHUB_ACTIONS_AUTO_DEPLOY") || null,
          RAILPUSH_GITHUB_ACTIONS_ENABLED: byKey.get("RAILPUSH_GITHUB_ACTIONS_ENABLED") || null,
          RAILPUSH_DEPLOY_ON_GITHUB_ACTIONS: byKey.get("RAILPUSH_DEPLOY_ON_GITHUB_ACTIONS") || null,
          RAILPUSH_GITHUB_ACTIONS_WORKFLOW: byKey.get("RAILPUSH_GITHUB_ACTIONS_WORKFLOW") || null,
          RAILPUSH_GITHUB_ACTIONS_WORKFLOWS: byKey.get("RAILPUSH_GITHUB_ACTIONS_WORKFLOWS") || null,
        },
      });
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "set_deploy_automation_mode",
  "Set a service deploy automation mode: off, push, or GitHub Actions workflow-success gated deploys. Optionally provide workflow allowlist for workflow_success mode; if omitted, existing workflow allowlist is preserved.",
  {
    service_id: z.string().describe("Service ID"),
    mode: z.enum(["off", "push", "workflow_success"]).describe("Deploy automation mode"),
    workflows: z.array(z.string()).optional().describe("Optional workflow names allowlist for workflow_success mode. Omit to keep existing allowlist, pass [] to clear it."),
  },
  async ({ service_id, mode, workflows }) => {
    try {
      const workflowsProvided = workflows !== undefined;
      const seen = new Set<string>();
      let workflowNames = (workflows ?? [])
        .map((w) => w.trim())
        .filter(Boolean)
        .filter((w) => {
          const key = w.toLowerCase();
          if (seen.has(key)) return false;
          seen.add(key);
          return true;
        });

      const auto_deploy = mode !== "off";
      const service = await client.updateService(service_id, { auto_deploy });

      let envVars: Array<{ key: string; value: string; is_secret: boolean }> = [];
      const deleteKeys = [
        "RAILPUSH_GITHUB_ACTIONS_AUTO_DEPLOY",
        "RAILPUSH_GITHUB_ACTIONS_ENABLED",
        "RAILPUSH_DEPLOY_ON_GITHUB_ACTIONS",
        "RAILPUSH_GITHUB_ACTIONS_WORKFLOW",
      ];

      if (mode === "workflow_success") {
        if (!workflowsProvided) {
          const currentEnvVars = await client.listEnvVars(service_id) as Array<Record<string, unknown>>;
          const byKey = new Map<string, string>();
          for (const row of currentEnvVars) {
            const key = typeof row.key === "string" ? row.key.trim().toUpperCase() : "";
            if (!key) continue;
            const value = typeof row.value === "string" ? row.value.trim() : "";
            byKey.set(key, value);
          }

          const existingRaw = (byKey.get("RAILPUSH_GITHUB_ACTIONS_WORKFLOWS") || byKey.get("RAILPUSH_GITHUB_ACTIONS_WORKFLOW") || "").trim();
          const existingSeen = new Set<string>();
          workflowNames = existingRaw
            .split(",")
            .map((w) => w.trim())
            .filter(Boolean)
            .filter((w) => {
              const key = w.toLowerCase();
              if (existingSeen.has(key)) return false;
              existingSeen.add(key);
              return true;
            });
        }

        envVars = [{ key: "RAILPUSH_GITHUB_ACTIONS_AUTO_DEPLOY", value: "true", is_secret: false }];
        if (workflowNames.length > 0) {
          envVars.push({ key: "RAILPUSH_GITHUB_ACTIONS_WORKFLOWS", value: workflowNames.join(", "), is_secret: false });
        } else {
          deleteKeys.push("RAILPUSH_GITHUB_ACTIONS_WORKFLOWS");
        }
      } else {
        deleteKeys.push("RAILPUSH_GITHUB_ACTIONS_WORKFLOWS");
      }

      const env_update = await client.mergeEnvVars(service_id, envVars, deleteKeys);
      return text({ service_id, mode, auto_deploy, workflows: workflowNames, service, env_update });
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "set_github_actions_deploy_workflows",
  "Set or clear the workflow allowlist used by GitHub Actions-gated deploys for a service. This updates allowlist env vars only and does not change deploy mode.",
  {
    service_id: z.string().describe("Service ID"),
    workflows: z.array(z.string()).describe("Workflow names allowlist. Pass an empty array to clear the allowlist."),
  },
  async ({ service_id, workflows }) => {
    try {
      const seen = new Set<string>();
      const workflowNames = workflows
        .map((w) => w.trim())
        .filter(Boolean)
        .filter((w) => {
          const key = w.toLowerCase();
          if (seen.has(key)) return false;
          seen.add(key);
          return true;
        });

      const envVars = workflowNames.length > 0
        ? [{ key: "RAILPUSH_GITHUB_ACTIONS_WORKFLOWS", value: workflowNames.join(", "), is_secret: false }]
        : [];

      const deleteKeys = ["RAILPUSH_GITHUB_ACTIONS_WORKFLOW"];
      if (workflowNames.length === 0) {
        deleteKeys.push("RAILPUSH_GITHUB_ACTIONS_WORKFLOWS");
      }

      const env_update = await client.mergeEnvVars(service_id, envVars, deleteKeys);
      return text({ service_id, workflows: workflowNames, env_update });
    }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  ENVIRONMENT VARIABLES
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_env_vars",
  "List environment variables for a service. Secret values are masked. Supports cursor pagination via limit/cursor.",
  {
    service_id: z.string().describe("Service ID"),
    limit: z.number().int().positive().max(100).optional().describe("Page size for cursor pagination (max 100)"),
    cursor: z.string().optional().describe("Opaque cursor returned by previous page"),
  },
  async ({ service_id, limit, cursor }) => {
    try { return text(await client.listEnvVars(service_id, { limit, cursor })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "set_env_vars",
  "Set environment variables for a service. This is a bulk replace operation — provide all env vars the service should have. Existing vars not in the list are removed. To perform a destructive replace, set confirm_destructive=true. Use upsert_env_vars for additive updates.",
  {
    service_id: z.string().describe("Service ID"),
    env_vars: z.array(z.object({
      key: z.string().describe("Variable name (e.g. DATABASE_URL)"),
      value: z.string().describe("Variable value"),
      is_secret: z.boolean().optional().describe("Mark as secret (masks value in dashboard, default: false)"),
    })).describe("Array of environment variables to set"),
    confirm_destructive: z.boolean().optional().describe("Required only when this replace removes existing keys"),
  },
  async ({ service_id, env_vars, confirm_destructive }) => {
    try { return text(await client.bulkUpdateEnvVars(service_id, env_vars, confirm_destructive)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "upsert_env_vars",
  "Add or update environment variables for a service WITHOUT removing existing vars. Unlike set_env_vars (which replaces everything), this is additive — keys not in the list are left untouched. Optionally specify keys to delete.",
  {
    service_id: z.string().describe("Service ID"),
    env_vars: z.array(z.object({
      key: z.string().describe("Variable name (e.g. DATABASE_URL)"),
      value: z.string().describe("Variable value"),
      is_secret: z.boolean().optional().describe("Mark as secret (default: false)"),
    })).optional().describe("Env vars to add or update"),
    delete: z.array(z.string()).optional().describe("Env var keys to remove"),
  },
  async ({ service_id, env_vars, delete: deleteKeys }) => {
    try { return text(await client.mergeEnvVars(service_id, env_vars ?? [], deleteKeys)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  PERSISTENT DISKS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_service_disks",
  "List persistent disks attached to a service. Services may have at most one attached disk.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.listServiceDisks(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "set_service_disk",
  "Create or replace a service disk attachment. Requires a single-instance service; redeploy is required after changes.",
  {
    service_id: z.string().describe("Service ID"),
    name: z.string().describe("Disk name"),
    mount_path: z.string().describe("Absolute mount path inside the container (e.g. /data)"),
    size_gb: z.number().optional().describe("Disk size in GiB (default: 1)"),
  },
  async ({ service_id, name, mount_path, size_gb }) => {
    try { return text(await client.upsertServiceDisk(service_id, { name, mount_path, size_gb })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_service_disk",
  "Delete the persistent disk attachment from a service. Redeploy is required after deletion.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.deleteServiceDisk(service_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  CUSTOM DOMAINS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_custom_domains",
  "List custom domains configured for a service. Supports cursor pagination via limit/cursor.",
  {
    service_id: z.string().describe("Service ID"),
    limit: z.number().int().positive().max(100).optional().describe("Page size for cursor pagination (max 100)"),
    cursor: z.string().optional().describe("Opaque cursor returned by previous page"),
  },
  async ({ service_id, limit, cursor }) => {
    try { return text(await client.listCustomDomains(service_id, { limit, cursor })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "add_custom_domain",
  "Add a custom domain to a service. You must point the domain's DNS (CNAME) to the service's public URL for verification and TLS provisioning. Optionally set redirect_target to make the domain 301-redirect to another URL (e.g. redirect apex to www).",
  {
    service_id: z.string().describe("Service ID"),
    domain: z.string().describe("Custom domain (e.g. app.example.com)"),
    redirect_target: z.string().optional().describe("If set, the domain will 301-redirect to this URL instead of proxying to the service (e.g. https://www.example.com)"),
  },
  async ({ service_id, domain, redirect_target }) => {
    try { return text(await client.addCustomDomain(service_id, domain, redirect_target)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_custom_domain",
  "Remove a custom domain from a service.",
  {
    service_id: z.string().describe("Service ID"),
    domain: z.string().describe("Domain to remove"),
  },
  async ({ service_id, domain }) => {
    try { return text(await client.deleteCustomDomain(service_id, domain)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  REWRITE & PROXY RULES
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_rewrite_rules",
  "List rewrite/proxy rules for a service. Rewrite rules let you route specific URL paths (e.g. /api/*) from one service to another service's backend.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.listRewriteRules(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "add_rewrite_rule",
  "Add a rewrite/proxy rule to a service. Routes requests matching source_path on this service to dest_path on the destination service. Supports wildcard paths (e.g. /api/*).",
  {
    service_id: z.string().describe("Source service ID (the service whose URL receives the request)"),
    source_path: z.string().describe("URL path pattern to match on the source service (e.g. /api/*)"),
    dest_service_id: z.string().describe("Destination service ID to proxy/rewrite to"),
    dest_path: z.string().optional().describe("Destination path (defaults to same as source_path, e.g. /api/*)"),
    rule_type: z.enum(["proxy", "redirect"]).optional().describe("Rule type: proxy (reverse proxy, default) or redirect (301)"),
  },
  async ({ service_id, source_path, dest_service_id, dest_path, rule_type }) => {
    try { return text(await client.addRewriteRule(service_id, source_path, dest_service_id, dest_path, rule_type)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_rewrite_rule",
  "Delete a rewrite/proxy rule from a service.",
  {
    service_id: z.string().describe("Service ID"),
    rule_id: z.string().describe("Rewrite rule ID"),
  },
  async ({ service_id, rule_id }) => {
    try { return text(await client.deleteRewriteRule(service_id, rule_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  DATABASES (Managed PostgreSQL)
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_databases",
  "List managed PostgreSQL databases in the workspace with optional server-side filters. Supports cursor pagination via limit/cursor.",
  {
    workspace_id: z.string().optional().describe("Workspace ID"),
    plan: z.enum(["free", "starter", "standard", "pro"]).optional().describe("Filter by plan"),
    status: z.string().optional().describe("Filter by status (e.g. available, creating, failed)"),
    pg_version: z.number().optional().describe("Filter by PostgreSQL major version (e.g. 16)"),
    name: z.string().optional().describe("Filter by database name (substring match)"),
    query: z.string().optional().describe("Generic search over database name/host/status/plan"),
    limit: z.number().int().positive().max(100).optional().describe("Page size for cursor pagination (max 100)"),
    cursor: z.string().optional().describe("Opaque cursor returned by previous page"),
  },
  async ({ workspace_id, plan, status, pg_version, name, query, limit, cursor }) => {
    try { return text(await client.listDatabases({ workspace_id, plan, status, pg_version, name, query, limit, cursor })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_database",
  "Create a new managed PostgreSQL database.",
  {
    name: z.string().describe("Database name (also used as db_name and username)"),
    plan: z.enum(["free", "starter", "standard", "pro"]).optional().describe("Plan tier (default: starter)"),
    pg_version: z.number().optional().describe("PostgreSQL major version (default: 16)"),
    workspace_id: z.string().optional().describe("Workspace ID"),
  },
  async (args) => {
    try { return text(await client.createDatabase(args as Record<string, unknown>)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_database",
  "Get database details and redacted connection strings (passwords are not returned by this endpoint).",
  { database_id: z.string().describe("Database ID") },
  async ({ database_id }) => {
    try { return text(await client.getDatabase(database_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "reveal_database_credentials",
  "Reveal plaintext credentials for a database. Use only when absolutely required; output may be stored in logs/conversation history.",
  {
    database_id: z.string().describe("Database ID"),
    acknowledge_sensitive_output: z.boolean().describe("Must be true to confirm you accept sensitive output exposure"),
  },
  async ({ database_id, acknowledge_sensitive_output }) => {
    if (!acknowledge_sensitive_output) {
      return text({
        status: "blocked",
        reason: "acknowledge_sensitive_output must be true",
      });
    }
    try { return text(await client.revealDatabaseCredentials(database_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "rotate_database_password",
  "Rotate a managed PostgreSQL database password. Returns the new plaintext password once and updates linked service connection URLs.",
  {
    database_id: z.string().describe("Database ID"),
    password: z.string().optional().describe("Optional custom password (min 16 chars). If omitted, RailPush generates one."),
    acknowledge_sensitive_output: z.boolean().describe("Must be true to confirm you accept sensitive output exposure"),
  },
  async ({ database_id, password, acknowledge_sensitive_output }) => {
    if (!acknowledge_sensitive_output) {
      return text({
        status: "blocked",
        reason: "acknowledge_sensitive_output must be true",
      });
    }
    try {
      return text(await client.rotateDatabasePassword(database_id, {
        password,
        acknowledge_sensitive_output: true,
      }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "query_database",
  "Run SQL against a managed PostgreSQL database. Default mode is read-only; set allow_write=true with acknowledge_risky_query=true for write-capable execution.",
  {
    database_id: z.string().describe("Database ID"),
    query: z.string().describe("SQL statement to execute"),
    allow_write: z.boolean().optional().describe("Enable write-capable transaction mode (default: false/read-only)"),
    acknowledge_risky_query: z.boolean().optional().describe("Required when allow_write=true"),
    max_rows: z.number().int().positive().max(1000).optional().describe("Max rows returned for SELECT/RETURNING queries (default: 100, max: 1000)"),
    timeout_ms: z.number().int().positive().max(120000).optional().describe("Statement timeout in milliseconds (default: 15000, max: 120000)"),
  },
  async ({ database_id, query, allow_write, acknowledge_risky_query, max_rows, timeout_ms }) => {
    try {
      return text(await client.queryDatabase(database_id, {
        query,
        allow_write,
        acknowledge_risky_query,
        max_rows,
        timeout_ms,
      }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_database_retention",
  "Get backup retention policy for a managed database.",
  {
    database_id: z.string().describe("Database ID"),
  },
  async ({ database_id }) => {
    try { return text(await client.getDatabaseRetention(database_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "set_database_retention",
  "Update backup retention windows for a managed database. Values accept day numbers or strings like 30d, 12w, 6m, 1y.",
  {
    database_id: z.string().describe("Database ID"),
    automated_backups: z.union([z.number().int().positive(), z.string()]).optional().describe("Automated backup retention (e.g. 30d)"),
    manual_backups: z.union([z.number().int().positive(), z.string()]).optional().describe("Manual backup retention (e.g. 365d)"),
    wal_archives: z.union([z.number().int().positive(), z.string()]).optional().describe("WAL archive retention policy (e.g. 7d)"),
  },
  async ({ database_id, automated_backups, manual_backups, wal_archives }) => {
    try {
      return text(await client.setDatabaseRetention(database_id, {
        automated_backups,
        manual_backups,
        wal_archives,
      }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_database",
  "Update a database configuration (plan and deletion protection).",
  {
    database_id: z.string().describe("Database ID"),
    plan: z.enum(["free", "starter", "standard", "pro"]).optional().describe("New plan tier"),
    deletion_protection: z.boolean().optional().describe("Enable/disable deletion protection for this database"),
  },
  async ({ database_id, plan, deletion_protection }) => {
    const payload = Object.fromEntries(Object.entries({ plan, deletion_protection }).filter(([, v]) => v !== undefined));
    try { return text(await client.updateDatabase(database_id, payload)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "bulk_update_databases",
  "Apply the same database updates to multiple databases in one API call. Returns per-database status and errors.",
  {
    database_ids: z.array(z.string()).min(1).max(200).describe("Array of database IDs to update"),
    changes: z.object({
      plan: z.enum(["free", "starter", "standard", "pro"]).optional(),
    }).passthrough().describe("Partial database update payload applied to each database"),
    dry_run: z.boolean().optional().describe("Validate only; do not apply changes"),
    transaction_mode: z.enum(["best_effort", "all_or_nothing"]).optional().describe("best_effort (default) or all_or_nothing preflight"),
    transactional: z.boolean().optional().describe("Alias for transaction_mode=all_or_nothing when true"),
  },
  async ({ database_ids, changes, dry_run, transaction_mode, transactional }) => {
    try {
      return text(await client.bulkUpdateDatabases({
        ids: database_ids,
        changes,
        dry_run,
        transaction_mode,
        transactional,
      }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_database",
  "Delete a managed database using a safeguarded flow. First confirmation soft-deletes it into a 72-hour recovery window. Optional hard_delete=true is only for permanent deletion after that window.",
  {
    database_id: z.string().describe("Database ID"),
    confirm_destructive: z.boolean().describe("Must be true to proceed with delete"),
    hard_delete: z.boolean().optional().describe("Set true only when you intend permanent deletion after recovery window"),
    confirm_linked_services: z.boolean().optional().describe("Set true to confirm deletion when services still reference this database"),
  },
  async ({ database_id, confirm_destructive, hard_delete, confirm_linked_services }) => {
    if (!confirm_destructive) {
      return text({
        status: "blocked",
        reason: "confirm_destructive must be true",
      });
    }
    try { return text(await client.deleteDatabase(database_id, hard_delete, confirm_linked_services)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "restore_database",
  "Restore a soft-deleted managed database from the recovery window.",
  { database_id: z.string().describe("Database ID") },
  async ({ database_id }) => {
    try { return text(await client.restoreDatabase(database_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "trigger_backup",
  "Trigger an immediate backup of a database.",
  { database_id: z.string().describe("Database ID") },
  async ({ database_id }) => {
    try { return text(await client.triggerBackup(database_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "list_backups",
  "List backups for a database. Supports cursor pagination via limit/cursor.",
  {
    database_id: z.string().describe("Database ID"),
    limit: z.number().int().positive().max(100).optional().describe("Page size for cursor pagination (max 100)"),
    cursor: z.string().optional().describe("Opaque cursor returned by previous page"),
  },
  async ({ database_id, limit, cursor }) => {
    try { return text(await client.listBackups(database_id, { limit, cursor })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "list_replicas",
  "List read replicas for a database.",
  { database_id: z.string().describe("Database ID") },
  async ({ database_id }) => {
    try { return text(await client.listReplicas(database_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_replica",
  "Create a read replica of a database.",
  {
    database_id: z.string().describe("Primary database ID"),
    name: z.string().optional().describe("Replica name (default: <primary>-replica)"),
    replication_mode: z.enum(["async", "sync"]).optional().describe("Replication mode (default: async)"),
  },
  async ({ database_id, ...opts }) => {
    try { return text(await client.createReplica(database_id, opts)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "promote_replica",
  "Promote a read replica to a standalone primary database.",
  {
    database_id: z.string().describe("Primary database ID"),
    replica_id: z.string().describe("Replica ID to promote"),
  },
  async ({ database_id, replica_id }) => {
    try { return text(await client.promoteReplica(database_id, replica_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "enable_ha",
  "Enable high availability for a database. Creates a hot standby replica that auto-promotes on failure.",
  { database_id: z.string().describe("Database ID") },
  async ({ database_id }) => {
    try { return text(await client.enableHA(database_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  KEY-VALUE (Managed Redis)
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_key_value_stores",
  "List managed Redis/key-value stores in the workspace with optional server-side filters. Supports cursor pagination via limit/cursor.",
  {
    workspace_id: z.string().optional().describe("Workspace ID"),
    plan: z.enum(["free", "starter", "standard", "pro"]).optional().describe("Filter by plan"),
    status: z.string().optional().describe("Filter by status (e.g. available, creating, failed)"),
    name: z.string().optional().describe("Filter by store name (substring match)"),
    query: z.string().optional().describe("Generic search over store name/host/status/plan/policy"),
    limit: z.number().int().positive().max(100).optional().describe("Page size for cursor pagination (max 100)"),
    cursor: z.string().optional().describe("Opaque cursor returned by previous page"),
  },
  async ({ workspace_id, plan, status, name, query, limit, cursor }) => {
    try { return text(await client.listKeyValues({ workspace_id, plan, status, name, query, limit, cursor })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_key_value_store",
  "Create a new managed Redis/key-value store.",
  {
    name: z.string().describe("Store name"),
    plan: z.enum(["free", "starter", "standard", "pro"]).optional().describe("Plan tier"),
    maxmemory_policy: z.string().optional().describe("Redis maxmemory policy (default: allkeys-lru)"),
    workspace_id: z.string().optional().describe("Workspace ID"),
  },
  async (args) => {
    try { return text(await client.createKeyValue(args as Record<string, unknown>)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_key_value_store",
  "Get details of a Redis/key-value store with redacted credentials.",
  { store_id: z.string().describe("Key-value store ID") },
  async ({ store_id }) => {
    try { return text(await client.getKeyValue(store_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "reveal_key_value_credentials",
  "Reveal plaintext credentials for a Redis/key-value store. Use only when absolutely required; output may be stored in logs/conversation history.",
  {
    store_id: z.string().describe("Key-value store ID"),
    acknowledge_sensitive_output: z.boolean().describe("Must be true to confirm you accept sensitive output exposure"),
  },
  async ({ store_id, acknowledge_sensitive_output }) => {
    if (!acknowledge_sensitive_output) {
      return text({
        status: "blocked",
        reason: "acknowledge_sensitive_output must be true",
      });
    }
    try { return text(await client.revealKeyValueCredentials(store_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_key_value_store",
  "Update a Redis/key-value store configuration (plan, maxmemory_policy, deletion protection).",
  {
    store_id: z.string().describe("Key-value store ID"),
    plan: z.enum(["free", "starter", "standard", "pro"]).optional().describe("New plan tier"),
    maxmemory_policy: z.string().optional().describe("Redis maxmemory policy"),
    deletion_protection: z.boolean().optional().describe("Enable/disable deletion protection for this key-value store"),
  },
  async ({ store_id, ...updates }) => {
    try {
      const data = Object.fromEntries(Object.entries(updates).filter(([, v]) => v !== undefined));
      return text(await client.updateKeyValue(store_id, data));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_key_value_store",
  "Delete a Redis/key-value store using a safeguarded flow. First confirmation soft-deletes it into a 72-hour recovery window. Optional hard_delete=true is only for permanent deletion after that window.",
  {
    store_id: z.string().describe("Key-value store ID"),
    confirm_destructive: z.boolean().describe("Must be true to proceed with delete"),
    hard_delete: z.boolean().optional().describe("Set true only when you intend permanent deletion after recovery window"),
  },
  async ({ store_id, confirm_destructive, hard_delete }) => {
    if (!confirm_destructive) {
      return text({
        status: "blocked",
        reason: "confirm_destructive must be true",
      });
    }
    try { return text(await client.deleteKeyValue(store_id, hard_delete)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "restore_key_value_store",
  "Restore a soft-deleted Redis/key-value store from the recovery window.",
  { store_id: z.string().describe("Key-value store ID") },
  async ({ store_id }) => {
    try { return text(await client.restoreKeyValue(store_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  LOGS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "get_logs",
  "Get runtime or deploy logs for a service. Supports optional text/regex search, time-window filtering, and level filtering.",
  {
    service_id: z.string().describe("Service ID"),
    log_type: z.enum(["runtime", "deploy"]).optional().describe("Log type (default: runtime)"),
    limit: z.number().optional().describe("Max log lines to return (default: 100)"),
    search: z.string().optional().describe("Filter logs containing this text"),
    regex: z.boolean().optional().describe("Interpret search as regex (case-insensitive)") ,
    since: z.string().optional().describe("Only include logs at/after this RFC3339 timestamp"),
    until: z.string().optional().describe("Only include logs at/before this RFC3339 timestamp"),
    level: z.enum(["debug", "info", "warn", "error", "warning"]).optional().describe("Filter by log level"),
  },
  async ({ service_id, log_type, limit, search, regex, since, until, level }) => {
    try {
      const normalizedLevel = level === "warning" ? "warn" : level;
      const logs = await client.queryLogs(service_id, {
        type: log_type,
        limit,
        search,
        regex,
        since,
        until,
        level: normalizedLevel,
      });
      return text(filterLogEntries(logs, { search, regex, since, until, level: normalizedLevel }));
    }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  AI FIX
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "start_ai_fix",
  "Start an automated AI fix session for a service that has a failed deploy. The AI analyzes build/runtime logs and attempts to fix the Dockerfile or configuration.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.startAIFix(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_ai_fix_status",
  "Check the status of an AI fix session for a service.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.getAIFixStatus(service_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  ONE-OFF JOBS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "exec_command",
  "Execute a single command directly inside a running service container and return stdout/stderr synchronously.",
  {
    service_id: z.string().describe("Service ID"),
    command: z.string().describe("Shell command to execute"),
    timeout_seconds: z.number().int().positive().max(120).optional().describe("Execution timeout in seconds (default: 30, max: 120)"),
    user: z.string().optional().describe("Optional Linux user for Docker-based services (not supported for Kubernetes services)"),
    acknowledge_risky_command: z.boolean().optional().describe("Acknowledge a risky command so it can run"),
    reason: z.string().optional().describe("Reason for executing a risky command"),
    max_output_bytes: z.number().int().positive().max(262144).optional().describe("Max captured bytes per stdout/stderr stream (default: 65536)"),
  },
  async ({ service_id, command, timeout_seconds, user, acknowledge_risky_command, reason, max_output_bytes }) => {
    try {
      return text(await client.execServiceCommand(service_id, {
        command,
        timeout_seconds,
        user,
        acknowledge_risky_command,
        reason,
        max_output_bytes,
      }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "run_job",
  "Run a one-off command against a service's container. Useful for migrations, data fixes, shell commands, etc.",
  {
    service_id: z.string().describe("Service ID"),
    command: z.string().describe("Shell command to execute"),
    name: z.string().optional().describe("Job name (default: 'One-off command')"),
    acknowledge_risky_command: z.boolean().optional().describe("Acknowledge a risky command so it can run"),
    reason: z.string().optional().describe("Reason for executing a risky command"),
  },
  async ({ service_id, command, name, acknowledge_risky_command, reason }) => {
    try {
      return text(await client.runJob(service_id, command, {
        name,
        acknowledge_risky_command,
        reason,
      }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "list_jobs",
  "List one-off jobs that have been run against a service. Supports cursor pagination via limit/cursor.",
  {
    service_id: z.string().describe("Service ID"),
    limit: z.number().int().positive().max(100).optional().describe("Page size for cursor pagination (max 100)"),
    cursor: z.string().optional().describe("Opaque cursor returned by previous page"),
  },
  async ({ service_id, limit, cursor }) => {
    try { return text(await client.listJobs(service_id, { limit, cursor })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_job",
  "Get details of a one-off job, including its output logs, status, and exit code.",
  { job_id: z.string().describe("Job ID") },
  async ({ job_id }) => {
    try { return text(await client.getJob(job_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  AUTOSCALING
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "get_autoscaling_policy",
  "Get the autoscaling policy for a service.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.getAutoscalingPolicy(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "set_autoscaling_policy",
  "Set or update the autoscaling policy for a service. Configure min/max instances and CPU/memory scaling targets.",
  {
    service_id: z.string().describe("Service ID"),
    enabled: z.boolean().describe("Enable or disable autoscaling"),
    min_instances: z.number().optional().describe("Minimum instances (default: 1)"),
    max_instances: z.number().optional().describe("Maximum instances"),
    cpu_target_percent: z.number().optional().describe("CPU usage target % to trigger scaling (default: 70, range: 10-95)"),
    memory_target_percent: z.number().optional().describe("Memory usage target % to trigger scaling (default: 75, range: 10-95)"),
    scale_out_cooldown_sec: z.number().optional().describe("Cooldown after scale-out in seconds (default: 120, min: 30)"),
    scale_in_cooldown_sec: z.number().optional().describe("Cooldown after scale-in in seconds (default: 180, min: 30)"),
  },
  async ({ service_id, ...policy }) => {
    try { return text(await client.upsertAutoscalingPolicy(service_id, policy)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  BLUEPRINTS (Infrastructure as Code)
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_blueprints",
  "List all blueprints (IaC definitions) in the workspace.",
  { workspace_id: z.string().optional().describe("Workspace ID") },
  async ({ workspace_id }) => {
    try { return text(await client.listBlueprints(workspace_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_blueprint",
  "Create a new blueprint from a Git repo. The repo should contain a railpush.yaml file that declares services and databases.",
  {
    name: z.string().describe("Blueprint name"),
    repo_url: z.string().describe("Git repository URL"),
    branch: z.string().optional().describe("Git branch (default: main)"),
    file_path: z.string().optional().describe("Path to railpush.yaml in the repo (default: railpush.yaml)"),
    ai_ignore_repo_yaml: z.boolean().optional().describe("If true, AI generates config instead of reading from repo"),
    workspace_id: z.string().optional().describe("Workspace ID"),
  },
  async (args) => {
    try { return text(await client.createBlueprint(args as Record<string, unknown>)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_blueprint",
  "Get blueprint details including its linked resources (services, databases) and their current statuses.",
  { blueprint_id: z.string().describe("Blueprint ID") },
  async ({ blueprint_id }) => {
    try { return text(await client.getBlueprint(blueprint_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "sync_blueprint",
  "Sync a blueprint — re-reads the railpush.yaml from the repo and creates/updates/deletes services to match.",
  { blueprint_id: z.string().describe("Blueprint ID") },
  async ({ blueprint_id }) => {
    try { return text(await client.syncBlueprint(blueprint_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_blueprint",
  "Update a blueprint. Currently supports moving it to a project folder. Set folder_id to null to move to root.",
  {
    blueprint_id: z.string().describe("Blueprint ID"),
    folder_id: z.string().nullable().optional().describe("Folder ID to move the blueprint into (null to move to root)"),
  },
  async ({ blueprint_id, ...updates }) => {
    try {
      const data = Object.fromEntries(Object.entries(updates).filter(([, v]) => v !== undefined));
      return text(await client.updateBlueprint(blueprint_id, data));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_blueprint",
  "Delete a blueprint and optionally its linked services.",
  { blueprint_id: z.string().describe("Blueprint ID") },
  async ({ blueprint_id }) => {
    try { return text(await client.deleteBlueprint(blueprint_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  ENV GROUPS (Shared Environment Variables)
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_env_groups",
  "List all environment variable groups in the workspace. Env groups allow sharing env vars across multiple services.",
  { workspace_id: z.string().optional().describe("Workspace ID") },
  async ({ workspace_id }) => {
    try { return text(await client.listEnvGroups(workspace_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_env_group",
  "Create a new environment variable group.",
  {
    name: z.string().describe("Group name"),
    workspace_id: z.string().optional().describe("Workspace ID"),
  },
  async (args) => {
    try { return text(await client.createEnvGroup(args)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_env_group",
  "Get env group details.",
  { group_id: z.string().describe("Env group ID") },
  async ({ group_id }) => {
    try { return text(await client.getEnvGroup(group_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_env_group",
  "Update an environment variable group (e.g. rename it).",
  {
    group_id: z.string().describe("Env group ID"),
    name: z.string().describe("New name for the env group"),
  },
  async ({ group_id, name }) => {
    try { return text(await client.updateEnvGroup(group_id, { name })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_env_group",
  "Delete an environment variable group.",
  { group_id: z.string().describe("Env group ID") },
  async ({ group_id }) => {
    try { return text(await client.deleteEnvGroup(group_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "list_env_group_vars",
  "List variables in an env group.",
  { group_id: z.string().describe("Env group ID") },
  async ({ group_id }) => {
    try { return text(await client.listEnvGroupVars(group_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "set_env_group_vars",
  "Set variables in an env group. This is a bulk replace — provide all vars the group should have.",
  {
    group_id: z.string().describe("Env group ID"),
    env_vars: z.array(z.object({
      key: z.string().describe("Variable name"),
      value: z.string().describe("Variable value"),
      is_secret: z.boolean().optional().describe("Mark as secret"),
    })).describe("Array of environment variables"),
  },
  async ({ group_id, env_vars }) => {
    try { return text(await client.bulkUpdateEnvGroupVars(group_id, env_vars)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "link_service_to_env_group",
  "Link a service to an env group so it inherits the group's variables.",
  {
    group_id: z.string().describe("Env group ID"),
    service_id: z.string().describe("Service ID to link"),
  },
  async ({ group_id, service_id }) => {
    try { return text(await client.linkServiceToEnvGroup(group_id, service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "unlink_service_from_env_group",
  "Unlink a service from an env group.",
  {
    group_id: z.string().describe("Env group ID"),
    service_id: z.string().describe("Service ID to unlink"),
  },
  async ({ group_id, service_id }) => {
    try { return text(await client.unlinkServiceFromEnvGroup(group_id, service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "list_env_group_linked_services",
  "List services linked to an env group. Set include_usage=true to return used/missing key details per linked service.",
  {
    group_id: z.string().describe("Env group ID"),
    include_usage: z.boolean().optional().describe("Include per-service used_keys/missing_keys details"),
  },
  async ({ group_id, include_usage }) => {
    try {
      if (include_usage) {
        return text(await client.listEnvGroupLinkedServicesDetailed(group_id));
      }
      return text(await client.listEnvGroupLinkedServices(group_id));
    }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  METRICS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "get_metrics",
  "Get current resource usage metrics (CPU, memory) for a service.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.getMetrics(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_metrics_history",
  "Get historical resource usage metrics (CPU, memory) for a service over time. Useful for trend analysis and capacity planning.",
  {
    service_id: z.string().describe("Service ID"),
    period: z.enum(["1h", "6h", "24h", "7d", "30d"]).optional().describe("Time period (default: 24h)"),
  },
  async ({ service_id, period }) => {
    try { return text(await client.getMetricsHistory(service_id, period ? { period } : undefined)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  PROJECTS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_projects",
  "List all projects in the workspace. Projects organize services into logical groups.",
  { workspace_id: z.string().optional().describe("Workspace ID") },
  async ({ workspace_id }) => {
    try { return text(await client.listProjects(workspace_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_project",
  "Create a new project to organize services into a logical group.",
  {
    name: z.string().describe("Project name"),
    workspace_id: z.string().optional().describe("Workspace ID"),
  },
  async (args) => {
    try { return text(await client.createProject(args as Record<string, unknown>)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_project",
  "Get details of a project, including its services and environments.",
  { project_id: z.string().describe("Project ID") },
  async ({ project_id }) => {
    try { return text(await client.getProject(project_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_project",
  "Update a project's name or move it to a folder. Set folder_id to null to move to root.",
  {
    project_id: z.string().describe("Project ID"),
    name: z.string().optional().describe("New project name"),
    folder_id: z.string().nullable().optional().describe("Folder ID to move the project into (null to move to root)"),
  },
  async ({ project_id, ...updates }) => {
    try {
      const data = Object.fromEntries(Object.entries(updates).filter(([, v]) => v !== undefined));
      return text(await client.updateProject(project_id, data));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_project",
  "Delete a project. Services within the project are not deleted.",
  { project_id: z.string().describe("Project ID") },
  async ({ project_id }) => {
    try { return text(await client.deleteProject(project_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  PROJECT FOLDERS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_project_folders",
  "List all project folders in the workspace. Folders organize projects into groups and can be nested (subfolders).",
  { workspace_id: z.string().optional().describe("Workspace ID") },
  async ({ workspace_id }) => {
    try { return text(await client.listProjectFolders(workspace_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_project_folder",
  "Create a new project folder. Optionally nest it inside another folder by providing parent_id. Max nesting depth is 3 levels.",
  {
    name: z.string().describe("Folder name"),
    parent_id: z.string().nullable().optional().describe("Parent folder ID to nest this folder inside (null for root)"),
    workspace_id: z.string().optional().describe("Workspace ID"),
  },
  async (args) => {
    try { return text(await client.createProjectFolder(args as Record<string, unknown>)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_project_folder",
  "Update a project folder's name or move it to a different parent folder.",
  {
    folder_id: z.string().describe("Folder ID"),
    name: z.string().optional().describe("New folder name"),
    parent_id: z.string().nullable().optional().describe("New parent folder ID (null to move to root)"),
  },
  async ({ folder_id, ...updates }) => {
    try {
      const data: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(updates)) {
        if (v !== undefined) data[k] = v;
      }
      return text(await client.updateProjectFolder(folder_id, data));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_project_folder",
  "Delete a project folder. Sub-folders are cascade deleted. Projects in the folder are moved to root (unassigned).",
  { folder_id: z.string().describe("Folder ID") },
  async ({ folder_id }) => {
    try { return text(await client.deleteProjectFolder(folder_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  ENVIRONMENTS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_environments",
  "List environments (e.g. staging, production) for a project.",
  { project_id: z.string().describe("Project ID") },
  async ({ project_id }) => {
    try { return text(await client.listEnvironments(project_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_environment",
  "Create a new environment within a project (e.g. staging, preview).",
  {
    project_id: z.string().describe("Project ID"),
    name: z.string().describe("Environment name (e.g. staging, production)"),
  },
  async ({ project_id, ...data }) => {
    try { return text(await client.createEnvironment(project_id, data as Record<string, unknown>)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_environment",
  "Update an environment's properties (e.g. rename it).",
  {
    environment_id: z.string().describe("Environment ID"),
    name: z.string().optional().describe("New name for the environment"),
  },
  async ({ environment_id, ...data }) => {
    try { return text(await client.updateEnvironment(environment_id, data as Record<string, unknown>)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_environment",
  "Delete an environment from a project.",
  { environment_id: z.string().describe("Environment ID") },
  async ({ environment_id }) => {
    try { return text(await client.deleteEnvironment(environment_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  GITHUB
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_github_repos",
  "List GitHub repositories accessible to the connected GitHub account.",
  {},
  async () => {
    try { return text(await client.listGitHubRepos()); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "list_github_branches",
  "List branches for a GitHub repository.",
  {
    owner: z.string().describe("Repository owner (user or org)"),
    repo: z.string().describe("Repository name"),
  },
  async ({ owner, repo }) => {
    try { return text(await client.listGitHubBranches(owner, repo)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "list_github_workflows",
  "List GitHub Actions workflows for a repository. Use workflow names from this list when setting deploy gate allowlists.",
  {
    owner: z.string().describe("Repository owner (user or org)"),
    repo: z.string().describe("Repository name"),
  },
  async ({ owner, repo }) => {
    try { return text(await client.listGitHubWorkflows(owner, repo)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "list_service_github_workflows",
  "List GitHub Actions workflows for a service's configured repository. Uses service-scoped API lookup.",
  {
    service_id: z.string().describe("Service ID"),
  },
  async ({ service_id }) => {
    try { return text(await client.listServiceGitHubWorkflows(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_service_github_webhook_status",
  "Get GitHub webhook installation status for a service (installed, missing, or permission_denied), including repair eligibility.",
  {
    service_id: z.string().describe("Service ID"),
  },
  async ({ service_id }) => {
    try { return text(await client.getServiceGitHubWebhookStatus(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "repair_service_github_webhook",
  "Repair (create or update) the GitHub webhook for a service's repository so RailPush receives push and workflow_run events.",
  {
    service_id: z.string().describe("Service ID"),
  },
  async ({ service_id }) => {
    try { return text(await client.repairServiceGitHubWebhook(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_service_event_webhook",
  "Get deploy-event webhook configuration for a service, including enabled status, selected events, and whether a secret is configured.",
  {
    service_id: z.string().describe("Service ID"),
  },
  async ({ service_id }) => {
    try { return text(await client.getServiceEventWebhook(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "set_service_event_webhook",
  "Enable/disable and configure deploy-event webhook delivery for a service.",
  {
    service_id: z.string().describe("Service ID"),
    enabled: z.boolean().describe("Enable or disable event webhook delivery"),
    url: z.string().optional().describe("Destination webhook URL (required when enabled=true)"),
    events: z.array(z.enum(["deploy.started", "deploy.success", "deploy.failed", "deploy.rollback"]))
      .optional()
      .describe("Events to deliver (defaults to all supported events)"),
    secret: z.string().optional().describe("Optional HMAC secret. Provide empty string to clear existing secret."),
  },
  async ({ service_id, enabled, url, events, secret }) => {
    try { return text(await client.setServiceEventWebhook(service_id, { enabled, url, events, secret })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "test_service_event_webhook",
  "Send a signed test payload (`deploy.test`) to the configured service event webhook endpoint.",
  {
    service_id: z.string().describe("Service ID"),
  },
  async ({ service_id }) => {
    try { return text(await client.testServiceEventWebhook(service_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  TEMPLATES
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_templates",
  "List available verified service templates with optional category/query filters.",
  {
    category: z.string().optional().describe("Filter by template category (for example: full-stack, agent)"),
    query: z.string().optional().describe("Search template name/description/tags"),
  },
  async ({ category, query }) => {
    try { return text(await client.listTemplates({ category, query })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_template",
  "Get full template details including resource topology.",
  {
    template_id: z.string().describe("Template ID (for example: django-postgres-redis)"),
  },
  async ({ template_id }) => {
    try { return text(await client.getTemplate(template_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "deploy_template",
  "Deploy a template stack (services and optional managed datastores) into a workspace.",
  {
    template_id: z.string().describe("Template ID"),
    workspace_id: z.string().optional().describe("Target workspace ID (defaults to caller's workspace)"),
    project_id: z.string().optional().describe("Optional project ID to associate created services"),
    environment_id: z.string().optional().describe("Optional environment ID for created services"),
    name_prefix: z.string().optional().describe("Resource naming prefix"),
    repo_url: z.string().optional().describe("Repository URL used for template services"),
    branch: z.string().optional().describe("Repository branch for template services (default: main)"),
    plan: z.enum(["free", "starter", "pro", "business", "enterprise"]).optional().describe("Plan for created resources (default: starter)"),
    customizations: z.record(z.any()).optional().describe("Optional customization bag reserved for future template overrides"),
  },
  async ({ template_id, workspace_id, project_id, environment_id, name_prefix, repo_url, branch, plan, customizations }) => {
    try {
      return text(await client.deployTemplate(template_id, {
        workspace_id,
        project_id,
        environment_id,
        name_prefix,
        repo_url,
        branch,
        plan,
        customizations,
      }));
    } catch (e) {
      return err(e);
    }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  SUPPORT TICKETS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_support_tickets",
  "List your support tickets with optional filters (status/category/component/tags/query).",
  {
    status: z.enum(["open", "pending", "solved", "closed"]).optional().describe("Filter by ticket status"),
    category: z.enum(["support", "feature_request", "bug", "bug_report", "security", "billing", "how_to", "incident", "feedback"]).optional().describe("Filter by ticket category"),
    component: z.enum(["services", "databases", "key-value", "deployments", "env-vars", "domains", "mcp-api", "billing", "auth", "builds", "dashboard"]).optional().describe("Filter by component"),
    tags: z.array(z.string()).optional().describe("Only tickets containing all of these tags"),
    query: z.string().optional().describe("Search subject text"),
    limit: z.number().optional().describe("Max results (default 50, max 200)"),
    offset: z.number().optional().describe("Pagination offset"),
  },
  async ({ status, category, component, tags, query, limit, offset }) => {
    try { return text(await client.listSupportTickets({ status, category, component, tags, query, limit, offset })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_support_ticket",
  "Create a new support ticket. Supports structured category, component, and tags for better triage.",
  {
    subject: z.string().describe("Ticket subject"),
    message: z.string().describe("Detailed description of the issue or question"),
    priority: z.enum(["low", "normal", "high", "urgent"]).optional().describe("Ticket priority (default: normal)"),
    category: z.enum(["support", "feature_request", "bug", "bug_report", "security", "billing", "how_to", "incident", "feedback"]).optional().describe("Ticket category (default: support)"),
    component: z.enum(["services", "databases", "key-value", "deployments", "env-vars", "domains", "mcp-api", "billing", "auth", "builds", "dashboard"]).optional().describe("Product component area"),
    tags: z.array(z.string()).optional().describe("User-defined tags"),
  },
  async ({ subject, message, priority, category, component, tags }) => {
    try { return text(await client.createSupportTicket({ subject, message, priority, category, component, tags })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_support_ticket",
  "Get details and message history of a support ticket.",
  { ticket_id: z.string().describe("Ticket ID") },
  async ({ ticket_id }) => {
    try { return text(await client.getSupportTicket(ticket_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "reply_to_support_ticket",
  "Add a reply message to an existing support ticket.",
  {
    ticket_id: z.string().describe("Ticket ID"),
    message: z.string().describe("Reply message"),
  },
  async ({ ticket_id, message }) => {
    try { return text(await client.addSupportTicketMessage(ticket_id, message)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_support_ticket_tags",
  "Replace tags on one of your support tickets.",
  {
    ticket_id: z.string().describe("Ticket ID"),
    tags: z.array(z.string()).describe("New full tag set (replaces existing tags)"),
  },
  async ({ ticket_id, tags }) => {
    try { return text(await client.updateSupportTicketTags(ticket_id, tags)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  OPS: SUPPORT TICKETS (admin/ops role required)
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_ops_tickets",
  "List support tickets across all users (ops/admin). Filter by status/category/priority and search by subject/email/workspace.",
  {
    status: z.enum(["open", "pending", "solved", "closed"]).optional().describe("Filter by ticket status"),
    category: z.enum(["support", "feature_request", "bug", "bug_report", "security", "billing", "how_to", "incident", "feedback"]).optional().describe("Filter by ticket category"),
    priority: z.enum(["low", "normal", "high", "urgent"]).optional().describe("Filter by ticket priority"),
    component: z.enum(["services", "databases", "key-value", "deployments", "env-vars", "domains", "mcp-api", "billing", "auth", "builds", "dashboard"]).optional().describe("Filter by component"),
    tags: z.array(z.string()).optional().describe("Only tickets containing all of these tags"),
    query: z.string().optional().describe("Search by subject, email, or workspace name"),
    limit: z.number().optional().describe("Max results (default 50, max 200)"),
    offset: z.number().optional().describe("Pagination offset"),
  },
  async ({ status, category, priority, component, tags, query, limit, offset }) => {
    try { return text(await client.listOpsTickets({ status, category, priority, component, tags, query, limit, offset })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "search_ops_tickets",
  "Advanced search for ops tickets with metadata (total + facets), including date filters and sorting.",
  {
    status: z.enum(["open", "pending", "solved", "closed"]).optional().describe("Filter by ticket status"),
    category: z.enum(["support", "feature_request", "bug", "bug_report", "security", "billing", "how_to", "incident", "feedback"]).optional().describe("Filter by ticket category"),
    priority: z.enum(["low", "normal", "high", "urgent"]).optional().describe("Filter by ticket priority"),
    component: z.enum(["services", "databases", "key-value", "deployments", "env-vars", "domains", "mcp-api", "billing", "auth", "builds", "dashboard"]).optional().describe("Filter by component"),
    tags: z.array(z.string()).optional().describe("Only tickets containing all of these tags"),
    query: z.string().optional().describe("Search by subject, email, or workspace name"),
    created_after: z.string().optional().describe("Only tickets created after this date/time (YYYY-MM-DD or RFC3339)"),
    created_before: z.string().optional().describe("Only tickets created before this date/time (YYYY-MM-DD or RFC3339)"),
    sort_by: z.enum(["updated_at", "created_at", "priority"]).optional().describe("Sort field (default: updated_at)"),
    sort_order: z.enum(["asc", "desc"]).optional().describe("Sort direction (default: desc)"),
    limit: z.number().optional().describe("Max results (default 50, max 200)"),
    offset: z.number().optional().describe("Pagination offset"),
  },
  async ({ status, category, priority, component, tags, query, created_after, created_before, sort_by, sort_order, limit, offset }) => {
    try {
      return text(await client.searchOpsTickets({ status, category, priority, component, tags, query, created_after, created_before, sort_by, sort_order, limit, offset }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_ops_ticket",
  "Get full ticket details including all messages and internal notes (ops/admin). Shows creator email, workspace name, and internal messages that customers cannot see.",
  { ticket_id: z.string().describe("Ticket ID") },
  async ({ ticket_id }) => {
    try { return text(await client.getOpsTicket(ticket_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_support_ticket_status",
  "Update a support ticket's status, priority, category, or assignment (ops/admin). Use this to close, solve, or triage tickets.",
  {
    ticket_id: z.string().describe("Ticket ID"),
    status: z.enum(["open", "pending", "solved", "closed"]).optional().describe("New status"),
    priority: z.enum(["low", "normal", "high", "urgent"]).optional().describe("New priority"),
    category: z.enum(["support", "feature_request", "bug", "bug_report", "security", "billing", "how_to", "incident", "feedback"]).optional().describe("New category"),
    component: z.enum(["services", "databases", "key-value", "deployments", "env-vars", "domains", "mcp-api", "billing", "auth", "builds", "dashboard"]).optional().describe("New component"),
    tags: z.array(z.string()).optional().describe("Replace ticket tags"),
    assigned_to: z.string().optional().describe("User ID to assign the ticket to"),
  },
  async ({ ticket_id, status, priority, category, component, tags, assigned_to }) => {
    const data: Record<string, string> = {};
    if (status) data.status = status;
    if (priority) data.priority = priority;
    if (category) data.category = category;
    if (component !== undefined) data.component = component;
    if (assigned_to) data.assigned_to = assigned_to;
    const payload = tags ? { ...data, tags } : data;
    try { return text(await client.updateOpsTicket(ticket_id, payload)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "bulk_update_ops_tickets",
  "Bulk update multiple ops tickets at once (status/priority/category), with an optional customer-visible reason message.",
  {
    ticket_ids: z.array(z.string()).min(1).describe("Ticket IDs to update (max 200)"),
    status: z.enum(["open", "pending", "solved", "closed"]).optional().describe("New status for all selected tickets"),
    priority: z.enum(["low", "normal", "high", "urgent"]).optional().describe("New priority for all selected tickets"),
    category: z.enum(["support", "feature_request", "bug", "bug_report", "security", "billing", "how_to", "incident", "feedback"]).optional().describe("New category for all selected tickets"),
    component: z.enum(["services", "databases", "key-value", "deployments", "env-vars", "domains", "mcp-api", "billing", "auth", "builds", "dashboard"]).optional().describe("New component for all selected tickets"),
    tags: z.array(z.string()).optional().describe("Replace tags for all selected tickets"),
    reason: z.string().optional().describe("Optional customer-visible message posted to each updated ticket"),
  },
  async ({ ticket_ids, status, priority, category, component, tags, reason }) => {
    try { return text(await client.bulkUpdateOpsTickets({ ticket_ids, status, priority, category, component, tags, reason })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "reply_to_ticket_as_ops",
  "Reply to a support ticket as ops/admin. Can post public replies visible to the customer, or internal notes only visible to ops staff.",
  {
    ticket_id: z.string().describe("Ticket ID"),
    message: z.string().describe("Reply message body"),
    is_internal: z.boolean().optional().describe("If true, the message is an internal note not visible to the customer (default: false)"),
  },
  async ({ ticket_id, message, is_internal }) => {
    try { return text(await client.addOpsTicketMessage(ticket_id, message, is_internal ?? false)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  BILLING
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "get_billing_overview",
  "Get billing overview including current plan, usage, credits, and payment method status.",
  {},
  async () => {
    try { return text(await client.getBillingOverview()); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  REGISTERED DOMAINS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_registered_domains",
  "List all domains registered through RailPush's domain registrar.",
  {},
  async () => {
    try { return text(await client.listRegisteredDomains()); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_registered_domain",
  "Get details of a registered domain, including expiry, auto-renew status, and nameservers.",
  { domain_id: z.string().describe("Domain ID") },
  async ({ domain_id }) => {
    try { return text(await client.getRegisteredDomain(domain_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "register_domain",
  "Register a new domain through RailPush's domain registrar.",
  {
    name: z.string().describe("Domain name to register (e.g. example.com)"),
  },
  async ({ name }) => {
    try { return text(await client.registerDomain({ name })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_registered_domain",
  "Remove a registered domain from your account.",
  { domain_id: z.string().describe("Domain ID") },
  async ({ domain_id }) => {
    try { return text(await client.deleteRegisteredDomain(domain_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "list_dns_records",
  "List DNS records for a registered domain.",
  { domain_id: z.string().describe("Domain ID") },
  async ({ domain_id }) => {
    try { return text(await client.listDnsRecords(domain_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_dns_record",
  "Create a DNS record for a registered domain.",
  {
    domain_id: z.string().describe("Domain ID"),
    type: z.enum(["A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "CAA"]).describe("Record type"),
    name: z.string().describe("Record name (e.g. @ or subdomain)"),
    content: z.string().describe("Record content (e.g. IP address, target hostname)"),
    ttl: z.number().optional().describe("TTL in seconds (default: 3600)"),
    priority: z.number().optional().describe("Priority (for MX and SRV records)"),
  },
  async ({ domain_id, ...record }) => {
    try { return text(await client.createDnsRecord(domain_id, record as Record<string, unknown>)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_dns_record",
  "Update an existing DNS record for a registered domain.",
  {
    domain_id: z.string().describe("Domain ID"),
    record_id: z.string().describe("DNS Record ID"),
    type: z.enum(["A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "CAA"]).optional().describe("Record type"),
    name: z.string().optional().describe("Record name"),
    content: z.string().optional().describe("Record content"),
    ttl: z.number().optional().describe("TTL in seconds"),
    priority: z.number().optional().describe("Priority"),
  },
  async ({ domain_id, record_id, ...updates }) => {
    try {
      const data = Object.fromEntries(Object.entries(updates).filter(([, v]) => v !== undefined));
      return text(await client.updateDnsRecord(domain_id, record_id, data));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_dns_record",
  "Delete a DNS record from a registered domain.",
  {
    domain_id: z.string().describe("Domain ID"),
    record_id: z.string().describe("DNS Record ID"),
  },
  async ({ domain_id, record_id }) => {
    try { return text(await client.deleteDnsRecord(domain_id, record_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  WORKSPACE MEMBERS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_workspace_members",
  "List all members of a workspace and their roles.",
  { workspace_id: z.string().describe("Workspace ID") },
  async ({ workspace_id }) => {
    try { return text(await client.listWorkspaceMembers(workspace_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "add_workspace_member",
  "Invite a new member to a workspace by email.",
  {
    workspace_id: z.string().describe("Workspace ID"),
    email: z.string().describe("Email address of the person to invite"),
    role: z.enum(["admin", "member", "viewer"]).optional().describe("Role (default: member)"),
  },
  async ({ workspace_id, email, role }) => {
    try { return text(await client.addWorkspaceMember(workspace_id, { email, role })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_workspace_member_role",
  "Change a workspace member's role.",
  {
    workspace_id: z.string().describe("Workspace ID"),
    user_id: z.string().describe("User ID of the member"),
    role: z.enum(["admin", "member", "viewer"]).describe("New role"),
  },
  async ({ workspace_id, user_id, role }) => {
    try { return text(await client.updateWorkspaceMemberRole(workspace_id, user_id, { role })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "remove_workspace_member",
  "Remove a member from a workspace.",
  {
    workspace_id: z.string().describe("Workspace ID"),
    user_id: z.string().describe("User ID of the member to remove"),
  },
  async ({ workspace_id, user_id }) => {
    try { return text(await client.removeWorkspaceMember(workspace_id, user_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "get_workspace_retention",
  "Get workspace-level retention policy for audit logs, deploy history, and metric history.",
  {
    workspace_id: z.string().describe("Workspace ID"),
  },
  async ({ workspace_id }) => {
    try { return text(await client.getWorkspaceRetention(workspace_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "set_workspace_retention",
  "Update workspace retention windows. Values accept day numbers or strings like 30d, 12w, 6m, 1y.",
  {
    workspace_id: z.string().describe("Workspace ID"),
    audit_logs: z.union([z.number().int().positive(), z.string()]).optional().describe("Audit log retention (e.g. 1y)"),
    deploy_history: z.union([z.number().int().positive(), z.string()]).optional().describe("Deploy history retention (e.g. 180d)"),
    metric_history: z.union([z.number().int().positive(), z.string()]).optional().describe("Metric history retention (e.g. 90d)"),
  },
  async ({ workspace_id, audit_logs, deploy_history, metric_history }) => {
    try {
      return text(await client.setWorkspaceRetention(workspace_id, {
        audit_logs,
        deploy_history,
        metric_history,
      }));
    }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  AUDIT LOGS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_audit_logs",
  "List audit logs for a workspace. Shows who did what and when — service creates, deploys, config changes, member adds, etc. Supports cursor pagination via limit/cursor.",
  {
    workspace_id: z.string().describe("Workspace ID"),
    limit: z.number().int().positive().max(100).optional().describe("Page size for cursor pagination (max 100)"),
    cursor: z.string().optional().describe("Opaque cursor returned by previous page"),
  },
  async ({ workspace_id, limit, cursor }) => {
    try { return text(await client.listAuditLogs(workspace_id, { limit, cursor })); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  PREVIEW ENVIRONMENTS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_preview_environments",
  "List all preview environments — ephemeral environments created from pull requests.",
  {},
  async () => {
    try { return text(await client.listPreviewEnvironments()); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_preview_environment",
  "Manually create or upsert a preview environment from a base service, with optional config/env var overrides and optional deploy trigger.",
  {
    workspace_id: z.string().optional().describe("Workspace ID (uses default if omitted)"),
    base_service_id: z.string().describe("Base service ID used as preview template"),
    pr_number: z.number().int().positive().describe("Pull request number"),
    pr_title: z.string().optional().describe("Pull request title"),
    pr_branch: z.string().describe("Pull request branch name"),
    base_branch: z.string().optional().describe("Base branch name"),
    commit_sha: z.string().optional().describe("Commit SHA for preview deploy metadata"),
    service_name: z.string().optional().describe("Optional explicit preview service name"),
    build_command: z.string().optional().describe("Override build command"),
    start_command: z.string().optional().describe("Override start command"),
    pre_deploy_command: z.string().optional().describe("Override pre-deploy command"),
    health_check_path: z.string().optional().describe("Override health check path"),
    dockerfile_path: z.string().optional().describe("Override Dockerfile path"),
    docker_context: z.string().optional().describe("Override Docker build context"),
    static_publish_path: z.string().optional().describe("Override static publish path"),
    port: z.number().int().positive().optional().describe("Override container port"),
    image_url: z.string().optional().describe("Override image URL"),
    trigger_deploy: z.boolean().optional().describe("Trigger deploy immediately (default: true)"),
    env_vars: z.array(z.object({
      key: z.string(),
      value: z.string(),
      is_secret: z.boolean().optional(),
    })).optional().describe("Optional env var overrides to upsert on preview service"),
  },
  async (args) => {
    try { return text(await client.createPreviewEnvironment(args)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_preview_environment",
  "Update preview environment metadata and preview service overrides. Optionally trigger a redeploy.",
  {
    preview_environment_id: z.string().describe("Preview environment ID"),
    pr_title: z.string().optional().describe("Pull request title"),
    pr_branch: z.string().optional().describe("Pull request branch"),
    base_branch: z.string().optional().describe("Base branch"),
    commit_sha: z.string().optional().describe("Commit SHA"),
    build_command: z.string().optional().describe("Override build command"),
    start_command: z.string().optional().describe("Override start command"),
    pre_deploy_command: z.string().optional().describe("Override pre-deploy command"),
    health_check_path: z.string().optional().describe("Override health check path"),
    dockerfile_path: z.string().optional().describe("Override Dockerfile path"),
    docker_context: z.string().optional().describe("Override Docker context"),
    static_publish_path: z.string().optional().describe("Override static publish path"),
    port: z.number().int().positive().optional().describe("Override container port"),
    image_url: z.string().optional().describe("Override image URL"),
    trigger_deploy: z.boolean().optional().describe("Trigger deploy immediately"),
    env_vars: z.array(z.object({
      key: z.string(),
      value: z.string(),
      is_secret: z.boolean().optional(),
    })).optional().describe("Optional env var overrides to upsert on preview service"),
  },
  async ({ preview_environment_id, ...updates }) => {
    try { return text(await client.updatePreviewEnvironment(preview_environment_id, updates)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_preview_environment",
  "Close a preview environment and remove its preview service resources.",
  { preview_environment_id: z.string().describe("Preview environment ID") },
  async ({ preview_environment_id }) => {
    try { return text(await client.deletePreviewEnvironment(preview_environment_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  START SERVER
// ════════════════════════════════════════════════════════════════════════

async function main() {
  const transport = new StdioServerTransport();
  await server.connect(transport);
  console.error("RailPush MCP server running on stdio");
}

main().catch((e) => {
  console.error("Fatal error:", e);
  process.exit(1);
});
