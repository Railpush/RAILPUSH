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

// ── Bootstrap ──────────────────────────────────────────────────────────

const apiUrl = process.env.RAILPUSH_API_URL ?? "https://apps.railpush.com";
const apiKey = process.env.RAILPUSH_API_KEY ?? "";

if (!apiKey) {
  console.error("RAILPUSH_API_KEY is required. Set it as an environment variable.");
  process.exit(1);
}

const client = new RailPushClient({ apiUrl, apiKey });

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

// ════════════════════════════════════════════════════════════════════════
//  SERVICES
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_services",
  "List all services in the workspace. Returns name, type, runtime, status, and URL for each service.",
  { workspace_id: z.string().optional().describe("Workspace ID (uses default if omitted)") },
  async ({ workspace_id }) => {
    try { return text(await client.listServices(workspace_id)); }
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
  "delete_service",
  "Permanently delete a service. This removes all deployments, containers, and associated resources. Cannot be undone.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.deleteService(service_id)); }
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
  "Search and filter services. All filters are optional and combined with AND. Name filter uses substring match.",
  {
    type: z.enum(["web", "worker", "cron", "static", "pserv"]).optional().describe("Filter by service type"),
    status: z.string().optional().describe("Filter by status (e.g. live, created, suspended)"),
    runtime: z.string().optional().describe("Filter by runtime (e.g. node, python, go, docker)"),
    name: z.string().optional().describe("Filter by name (substring match, case-insensitive)"),
    suspended: z.boolean().optional().describe("Filter by suspension state"),
  },
  async ({ type, status, runtime, name, suspended }) => {
    try {
      return text(await client.searchServices({
        type, status, runtime, name,
        suspended: suspended !== undefined ? String(suspended) : undefined,
      }));
    }
    catch (e) { return err(e); }
  },
);

server.tool(
  "bulk_restart",
  "Restart multiple services at once. Returns results for each service.",
  {
    service_ids: z.array(z.string()).describe("Array of service IDs to restart"),
  },
  async ({ service_ids }) => {
    const results: Array<{ id: string; status: string; error?: string }> = [];
    for (const id of service_ids) {
      try { await client.restartService(id); results.push({ id, status: "restarted" }); }
      catch (e) { results.push({ id, status: "failed", error: e instanceof Error ? e.message : String(e) }); }
    }
    return text(results);
  },
);

server.tool(
  "bulk_deploy",
  "Trigger deploys for multiple services at once. Returns results for each service.",
  {
    service_ids: z.array(z.string()).describe("Array of service IDs to deploy"),
  },
  async ({ service_ids }) => {
    const results: Array<{ id: string; status: string; error?: string }> = [];
    for (const id of service_ids) {
      try { await client.triggerDeploy(id); results.push({ id, status: "deploy_triggered" }); }
      catch (e) { results.push({ id, status: "failed", error: e instanceof Error ? e.message : String(e) }); }
    }
    return text(results);
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
  "List deploy history for a service. Shows status, trigger, commit, timing, and any errors for each deploy.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.listDeploys(service_id)); }
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

// ════════════════════════════════════════════════════════════════════════
//  ENVIRONMENT VARIABLES
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_env_vars",
  "List all environment variables for a service. Secret values are masked. Use this to check what env vars are configured.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.listEnvVars(service_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "set_env_vars",
  "Set environment variables for a service. This is a bulk operation — provide all env vars the service should have. Existing vars not in the list will be removed. After updating, you typically need to trigger a new deploy.",
  {
    service_id: z.string().describe("Service ID"),
    env_vars: z.array(z.object({
      key: z.string().describe("Variable name (e.g. DATABASE_URL)"),
      value: z.string().describe("Variable value"),
      is_secret: z.boolean().optional().describe("Mark as secret (masks value in dashboard, default: false)"),
    })).describe("Array of environment variables to set"),
  },
  async ({ service_id, env_vars }) => {
    try { return text(await client.bulkUpdateEnvVars(service_id, env_vars)); }
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
//  CUSTOM DOMAINS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_custom_domains",
  "List custom domains configured for a service.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.listCustomDomains(service_id)); }
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
  "List all managed PostgreSQL databases in the workspace.",
  { workspace_id: z.string().optional().describe("Workspace ID") },
  async ({ workspace_id }) => {
    try { return text(await client.listDatabases(workspace_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_database",
  "Create a new managed PostgreSQL database. Returns the database credentials including password and connection URL.",
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
  "Get full details of a database, including connection credentials (password, internal/external URLs, psql commands).",
  { database_id: z.string().describe("Database ID") },
  async ({ database_id }) => {
    try { return text(await client.getDatabase(database_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_database",
  "Update a database configuration (currently supports plan changes).",
  {
    database_id: z.string().describe("Database ID"),
    plan: z.enum(["free", "starter", "standard", "pro"]).describe("New plan tier"),
  },
  async ({ database_id, plan }) => {
    try { return text(await client.updateDatabase(database_id, { plan })); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "delete_database",
  "Permanently delete a managed database and all its data. Cannot be undone.",
  { database_id: z.string().describe("Database ID") },
  async ({ database_id }) => {
    try { return text(await client.deleteDatabase(database_id)); }
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
  "List all backups for a database.",
  { database_id: z.string().describe("Database ID") },
  async ({ database_id }) => {
    try { return text(await client.listBackups(database_id)); }
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
  "List all managed Redis/key-value stores in the workspace.",
  { workspace_id: z.string().optional().describe("Workspace ID") },
  async ({ workspace_id }) => {
    try { return text(await client.listKeyValues(workspace_id)); }
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
  "Get details of a Redis/key-value store, including connection info.",
  { store_id: z.string().describe("Key-value store ID") },
  async ({ store_id }) => {
    try { return text(await client.getKeyValue(store_id)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "update_key_value_store",
  "Update a Redis/key-value store configuration (plan, maxmemory_policy).",
  {
    store_id: z.string().describe("Key-value store ID"),
    plan: z.enum(["free", "starter", "standard", "pro"]).optional().describe("New plan tier"),
    maxmemory_policy: z.string().optional().describe("Redis maxmemory policy"),
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
  "Delete a Redis/key-value store. Cannot be undone.",
  { store_id: z.string().describe("Key-value store ID") },
  async ({ store_id }) => {
    try { return text(await client.deleteKeyValue(store_id)); }
    catch (e) { return err(e); }
  },
);

// ════════════════════════════════════════════════════════════════════════
//  LOGS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "get_logs",
  "Get runtime or deploy logs for a service. Runtime logs show application stdout/stderr. Deploy logs show build + rollout output.",
  {
    service_id: z.string().describe("Service ID"),
    log_type: z.enum(["runtime", "deploy"]).optional().describe("Log type (default: runtime)"),
    limit: z.number().optional().describe("Max log lines to return (default: 100)"),
  },
  async ({ service_id, log_type, limit }) => {
    try { return text(await client.queryLogs(service_id, { type: log_type, limit })); }
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
  "run_job",
  "Run a one-off command against a service's container. Useful for migrations, data fixes, shell commands, etc.",
  {
    service_id: z.string().describe("Service ID"),
    command: z.string().describe("Shell command to execute"),
    name: z.string().optional().describe("Job name (default: 'One-off command')"),
  },
  async ({ service_id, command, name }) => {
    try { return text(await client.runJob(service_id, command, name)); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "list_jobs",
  "List one-off jobs that have been run against a service.",
  { service_id: z.string().describe("Service ID") },
  async ({ service_id }) => {
    try { return text(await client.listJobs(service_id)); }
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
  "List services linked to an env group.",
  { group_id: z.string().describe("Env group ID") },
  async ({ group_id }) => {
    try { return text(await client.listEnvGroupLinkedServices(group_id)); }
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

// ════════════════════════════════════════════════════════════════════════
//  SUPPORT TICKETS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_support_tickets",
  "List your support tickets.",
  {},
  async () => {
    try { return text(await client.listSupportTickets()); }
    catch (e) { return err(e); }
  },
);

server.tool(
  "create_support_ticket",
  "Create a new support ticket to get help from the RailPush team. Use category to classify the ticket as support, feature_request, or bug_report.",
  {
    subject: z.string().describe("Ticket subject"),
    message: z.string().describe("Detailed description of the issue or question"),
    priority: z.enum(["low", "normal", "high", "urgent"]).optional().describe("Ticket priority (default: normal)"),
    category: z.enum(["support", "feature_request", "bug_report"]).optional().describe("Ticket category (default: support). Use 'feature_request' for feature ideas, 'bug_report' for bugs."),
  },
  async ({ subject, message, priority, category }) => {
    try { return text(await client.createSupportTicket({ subject, message, priority, category })); }
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

// ════════════════════════════════════════════════════════════════════════
//  OPS: SUPPORT TICKETS (admin/ops role required)
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_ops_tickets",
  "List all support tickets across all users (ops/admin). Filter by status, category, or search by subject/email/workspace.",
  {
    status: z.enum(["open", "pending", "solved", "closed"]).optional().describe("Filter by ticket status"),
    category: z.enum(["support", "feature_request", "bug_report"]).optional().describe("Filter by ticket category"),
    query: z.string().optional().describe("Search by subject, email, or workspace name"),
    limit: z.number().optional().describe("Max results (default 50, max 200)"),
    offset: z.number().optional().describe("Pagination offset"),
  },
  async ({ status, category, query, limit, offset }) => {
    try { return text(await client.listOpsTickets({ status, category, query, limit, offset })); }
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
    category: z.enum(["support", "feature_request", "bug_report"]).optional().describe("New category"),
    assigned_to: z.string().optional().describe("User ID to assign the ticket to"),
  },
  async ({ ticket_id, status, priority, category, assigned_to }) => {
    const data: Record<string, string> = {};
    if (status) data.status = status;
    if (priority) data.priority = priority;
    if (category) data.category = category;
    if (assigned_to) data.assigned_to = assigned_to;
    try { return text(await client.updateOpsTicket(ticket_id, data)); }
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

// ════════════════════════════════════════════════════════════════════════
//  AUDIT LOGS
// ════════════════════════════════════════════════════════════════════════

server.tool(
  "list_audit_logs",
  "List audit logs for a workspace. Shows who did what and when — service creates, deploys, config changes, member adds, etc.",
  { workspace_id: z.string().describe("Workspace ID") },
  async ({ workspace_id }) => {
    try { return text(await client.listAuditLogs(workspace_id)); }
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
