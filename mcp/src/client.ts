/**
 * RailPush API client — thin HTTP wrapper used by the MCP tool handlers.
 *
 * Configuration is read from environment variables:
 *   RAILPUSH_API_URL   — Base URL  (default: https://apps.railpush.com)
 *   RAILPUSH_API_KEY   — API key   (required)
 */

export interface RailPushClientConfig {
  apiUrl: string;
  apiKey: string;
  apiVersionPin?: string;
}

export class RailPushAPIError extends Error {
  constructor(
    public status: number,
    public body: string,
  ) {
    super(`RailPush API error ${status}: ${body}`);
    this.name = "RailPushAPIError";
  }
}

export class RailPushClient {
  private baseUrl: string;
  private apiKey: string;
  private apiVersionPin: string;

  constructor(config: RailPushClientConfig) {
    this.baseUrl = config.apiUrl.replace(/\/+$/, "");
    this.apiKey = config.apiKey;
    this.apiVersionPin = (config.apiVersionPin ?? "").trim() || "v1";
  }

  private async request(
    method: string,
    path: string,
    body?: unknown,
    query?: Record<string, string>,
  ): Promise<unknown> {
    let url = `${this.baseUrl}/api/v1${path}`;
    if (query) {
      const params = new URLSearchParams(
        Object.entries(query).filter(([, v]) => v !== undefined && v !== ""),
      );
      const qs = params.toString();
      if (qs) url += `?${qs}`;
    }

    const headers: Record<string, string> = {
      Authorization: `Bearer ${this.apiKey}`,
      Accept: "application/json",
    };
    if (this.apiVersionPin) {
      headers["X-RailPush-API-Version"] = this.apiVersionPin;
    }
    if (body !== undefined) {
      headers["Content-Type"] = "application/json";
    }

    const res = await fetch(url, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });

    const text = await res.text();
    if (!res.ok) {
      const retryAfter = res.headers.get("retry-after");
      const body = (res.status === 429 && retryAfter)
        ? `${text || "rate limit exceeded"} (Retry-After: ${retryAfter}s)`
        : text;
      throw new RailPushAPIError(res.status, body);
    }
    if (!text) return {};

    // Detect HTML responses (SPA fallback / routing miss) and surface them as errors
    const contentType = res.headers.get("content-type") ?? "";
    if (contentType.includes("text/html") || text.trimStart().startsWith("<!doctype") || text.trimStart().startsWith("<!DOCTYPE") || text.trimStart().startsWith("<html")) {
      throw new RailPushAPIError(res.status, `Expected JSON but received HTML — the API endpoint ${method} ${url} is likely not registered. Check the server route configuration.`);
    }

    try {
      return JSON.parse(text);
    } catch {
      throw new RailPushAPIError(res.status, `Expected JSON but could not parse response: ${text.slice(0, 200)}`);
    }
  }

  private async performConfirmedDelete(path: string, hardDelete?: boolean, extraPayload?: Record<string, unknown>): Promise<unknown> {
    const initiated = await this.request("DELETE", path, {});
    const token = (typeof initiated === "object" && initiated !== null && typeof (initiated as { confirmation_token?: unknown }).confirmation_token === "string")
      ? (initiated as { confirmation_token: string }).confirmation_token
      : "";
    if (!token) {
      return initiated;
    }
    return this.request("DELETE", path, {
      confirmation_token: token,
      hard_delete: Boolean(hardDelete),
      ...(extraPayload ?? {}),
    });
  }

  async getAPIVersionInfo() {
    return this.request("GET", "/version");
  }

  async getAPIVersionChangelog() {
    return this.request("GET", "/version/changelog");
  }

  async getRateLimitInfo() {
    return this.request("GET", "/rate-limit");
  }

  async searchWorkspaceResources(opts: { q: string; workspace_id?: string; limit?: number }) {
    const query: Record<string, string> = { q: opts.q };
    if (opts.workspace_id) query.workspace_id = opts.workspace_id;
    if (typeof opts.limit === "number" && opts.limit > 0) query.limit = String(opts.limit);
    return this.request("GET", "/search", undefined, query);
  }

  // ── Services ─────────────────────────────────────────────────────────

  async listServices(filters?: {
    workspace_id?: string;
    type?: string;
    status?: string;
    runtime?: string;
    plan?: string;
    name?: string;
    repo_url?: string;
    project_id?: string;
    query?: string;
    suspended?: string;
    limit?: number;
    cursor?: string;
  }) {
    return this.searchServices(filters ?? {});
  }

  async searchServices(filters: {
    workspace_id?: string;
    type?: string;
    status?: string;
    runtime?: string;
    plan?: string;
    name?: string;
    repo_url?: string;
    project_id?: string;
    query?: string;
    suspended?: string;
    limit?: number;
    cursor?: string;
  }) {
    const query: Record<string, string> = {};
    if (filters.workspace_id) query.workspace_id = filters.workspace_id;
    if (filters.type) query.type = filters.type;
    if (filters.status) query.status = filters.status;
    if (filters.runtime) query.runtime = filters.runtime;
    if (filters.plan) query.plan = filters.plan;
    if (filters.name) query.name = filters.name;
    if (filters.repo_url) query.repo_url = filters.repo_url;
    if (filters.project_id) query.project_id = filters.project_id;
    if (filters.query) query.query = filters.query;
    if (filters.suspended) query.suspended = filters.suspended;
    if (typeof filters.limit === "number" && filters.limit > 0) query.limit = String(filters.limit);
    if (filters.cursor) query.cursor = filters.cursor;
    return this.request("GET", "/services", undefined, query);
  }

  async createService(data: Record<string, unknown>) {
    return this.request("POST", "/services", data);
  }

  async cloneService(id: string, data: {
    name: string;
    include_env_vars?: boolean;
    overrides?: Record<string, unknown>;
  }) {
    return this.request("POST", `/services/${id}/clone`, data);
  }

  async getService(id: string) {
    return this.request("GET", `/services/${id}`);
  }

  async updateService(id: string, data: Record<string, unknown>) {
    return this.request("PATCH", `/services/${id}`, data);
  }

  async execServiceCommand(id: string, data: {
    command: string;
    timeout_seconds?: number;
    user?: string;
    acknowledge_risky_command?: boolean;
    reason?: string;
    max_output_bytes?: number;
  }) {
    return this.request("POST", `/services/${id}/exec`, data);
  }

  async getServiceRetention(id: string) {
    return this.request("GET", `/services/${id}/retention`);
  }

  async setServiceRetention(id: string, data: {
    runtime_logs?: string | number;
    build_logs?: string | number;
    request_logs?: string | number;
  }) {
    return this.request("PUT", `/services/${id}/retention`, data);
  }

  async getServiceMTLS(id: string) {
    return this.request("GET", `/services/${id}/mtls`);
  }

  async setServiceMTLS(id: string, data: Record<string, unknown>) {
    return this.request("PUT", `/services/${id}/mtls`, data);
  }

  async getServiceAccessControl(id: string) {
    return this.request("GET", `/services/${id}/access-control`);
  }

  async setServiceAccessControl(id: string, data: Record<string, unknown>) {
    return this.request("PUT", `/services/${id}/access-control`, data);
  }

  async listServiceAccessControlLog(id: string, limit?: number) {
    const query: Record<string, string> = {};
    if (typeof limit === "number" && limit > 0) query.limit = String(limit);
    return this.request("GET", `/services/${id}/access-control/log`, undefined, query);
  }

  async deleteService(id: string, hardDelete?: boolean) {
    return this.performConfirmedDelete(`/services/${id}`, hardDelete);
  }

  async restoreService(id: string) {
    return this.request("POST", `/services/${id}/restore`);
  }

  async restartService(id: string) {
    return this.request("POST", `/services/${id}/restart`);
  }

  async suspendService(id: string) {
    return this.request("POST", `/services/${id}/suspend`);
  }

  async resumeService(id: string) {
    return this.request("POST", `/services/${id}/resume`);
  }

  async bulkUpdateServices(data: {
    ids: string[];
    changes: Record<string, unknown>;
    dry_run?: boolean;
    transaction_mode?: "best_effort" | "all_or_nothing";
    transactional?: boolean;
  }) {
    return this.request("POST", "/services/bulk-update", data);
  }

  async bulkDeployServices(data: {
    ids: string[];
    commit_sha?: string;
    branch?: string;
    dry_run?: boolean;
    transaction_mode?: "best_effort" | "all_or_nothing";
    transactional?: boolean;
  }) {
    return this.request("POST", "/services/bulk-deploy", data);
  }

  async bulkRestartServices(data: {
    ids: string[];
    dry_run?: boolean;
    transaction_mode?: "best_effort" | "all_or_nothing";
    transactional?: boolean;
  }) {
    return this.request("POST", "/services/bulk-restart", data);
  }

  async bulkSetServiceEnvVars(data: {
    ids: string[];
    env_vars: Array<{ key: string; value: string; is_secret?: boolean }>;
    mode?: "merge" | "replace";
    delete?: string[];
    confirm_destructive?: boolean;
    dry_run?: boolean;
    transaction_mode?: "best_effort" | "all_or_nothing";
    transactional?: boolean;
  }) {
    return this.request("POST", "/services/bulk-set-env", data);
  }

  // ── Deploys ──────────────────────────────────────────────────────────

  async triggerDeploy(serviceId: string, data?: { commit_sha?: string; branch?: string }) {
    return this.request("POST", `/services/${serviceId}/deploys`, data ?? {});
  }

  async listDeploys(serviceId: string, opts?: { status?: string; branch?: string; since?: string; until?: string; limit?: number; cursor?: string }) {
    const query: Record<string, string> = {};
    if (opts?.status) query.status = opts.status;
    if (opts?.branch) query.branch = opts.branch;
    if (opts?.since) query.since = opts.since;
    if (opts?.until) query.until = opts.until;
    if (typeof opts?.limit === "number" && opts.limit > 0) query.limit = String(opts.limit);
    if (opts?.cursor) query.cursor = opts.cursor;
    return this.request("GET", `/services/${serviceId}/deploys`, undefined, query);
  }

  async getDeploy(serviceId: string, deployId: string) {
    return this.request("GET", `/services/${serviceId}/deploys/${deployId}`);
  }

  async rollbackDeploy(serviceId: string, deployId: string) {
    return this.request("POST", `/services/${serviceId}/deploys/${deployId}/rollback`);
  }

  async getDeployQueuePosition(serviceId: string, deployId: string) {
    return this.request("GET", `/services/${serviceId}/deploys/${deployId}/queue`);
  }

  async waitForDeploy(serviceId: string, deployId: string, timeoutSeconds?: number) {
    const query: Record<string, string> = {};
    if (typeof timeoutSeconds === "number" && timeoutSeconds > 0) {
      query.timeout = String(timeoutSeconds);
    }
    return this.request("POST", `/services/${serviceId}/deploys/${deployId}/wait`, {}, query);
  }

  // ── Environment Variables ────────────────────────────────────────────

  async listEnvVars(serviceId: string, opts?: { limit?: number; cursor?: string }) {
    const query: Record<string, string> = {};
    if (typeof opts?.limit === "number" && opts.limit > 0) query.limit = String(opts.limit);
    if (opts?.cursor) query.cursor = opts.cursor;
    return this.request("GET", `/services/${serviceId}/env-vars`, undefined, query);
  }

  async bulkUpdateEnvVars(
    serviceId: string,
    vars: Array<{ key: string; value: string; is_secret?: boolean }>,
    confirmDestructive?: boolean,
  ) {
    return this.request("PUT", `/services/${serviceId}/env-vars`, {
      mode: "replace",
      confirm_destructive: Boolean(confirmDestructive),
      env_vars: vars,
    });
  }

  async mergeEnvVars(serviceId: string, envVars: Array<{ key: string; value: string; is_secret?: boolean }>, deleteKeys?: string[]) {
    return this.request("PATCH", `/services/${serviceId}/env-vars`, {
      env_vars: envVars,
      delete: deleteKeys ?? [],
    });
  }

  // ── Persistent Disks ────────────────────────────────────────────────

  async listServiceDisks(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/disks`);
  }

  async upsertServiceDisk(serviceId: string, data: { name: string; mount_path: string; size_gb?: number }) {
    return this.request("PUT", `/services/${serviceId}/disks`, data);
  }

  async deleteServiceDisk(serviceId: string) {
    return this.request("DELETE", `/services/${serviceId}/disks`);
  }

  // ── Custom Domains ───────────────────────────────────────────────────

  async listCustomDomains(serviceId: string, opts?: { limit?: number; cursor?: string }) {
    const query: Record<string, string> = {};
    if (typeof opts?.limit === "number" && opts.limit > 0) query.limit = String(opts.limit);
    if (opts?.cursor) query.cursor = opts.cursor;
    return this.request("GET", `/services/${serviceId}/custom-domains`, undefined, query);
  }

  async addCustomDomain(serviceId: string, domain: string, redirectTarget?: string) {
    const body: Record<string, string> = { domain };
    if (redirectTarget) body.redirect_target = redirectTarget;
    return this.request("POST", `/services/${serviceId}/custom-domains`, body);
  }

  async deleteCustomDomain(serviceId: string, domain: string) {
    return this.request("DELETE", `/services/${serviceId}/custom-domains/${encodeURIComponent(domain)}`);
  }

  // ── Rewrite Rules ──────────────────────────────────────────────────

  async listRewriteRules(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/rewrite-rules`);
  }

  async addRewriteRule(serviceId: string, sourcePath: string, destServiceId: string, destPath?: string, ruleType?: string) {
    return this.request("POST", `/services/${serviceId}/rewrite-rules`, {
      source_path: sourcePath,
      dest_service_id: destServiceId,
      dest_path: destPath || sourcePath,
      rule_type: ruleType || "proxy",
    });
  }

  async deleteRewriteRule(serviceId: string, ruleId: string) {
    return this.request("DELETE", `/services/${serviceId}/rewrite-rules/${ruleId}`);
  }

  // ── Databases ────────────────────────────────────────────────────────

  async listDatabases(filters?: {
    workspace_id?: string;
    plan?: string;
    status?: string;
    pg_version?: number;
    name?: string;
    query?: string;
    limit?: number;
    cursor?: string;
  }) {
    const query: Record<string, string> = {};
    if (filters?.workspace_id) query.workspace_id = filters.workspace_id;
    if (filters?.plan) query.plan = filters.plan;
    if (filters?.status) query.status = filters.status;
    if (typeof filters?.pg_version === "number") query.pg_version = String(filters.pg_version);
    if (filters?.name) query.name = filters.name;
    if (filters?.query) query.query = filters.query;
    if (typeof filters?.limit === "number" && filters.limit > 0) query.limit = String(filters.limit);
    if (filters?.cursor) query.cursor = filters.cursor;
    return this.request("GET", "/databases", undefined, query);
  }

  async createDatabase(data: Record<string, unknown>) {
    return this.request("POST", "/databases", data);
  }

  async getDatabase(id: string) {
    return this.request("GET", `/databases/${id}`);
  }

  async revealDatabaseCredentials(id: string) {
    return this.request("POST", `/databases/${id}/credentials/reveal`, {
      acknowledge_sensitive_output: true,
    });
  }

  async rotateDatabasePassword(id: string, data?: { password?: string; acknowledge_sensitive_output?: boolean }) {
    return this.request("POST", `/databases/${id}/rotate-password`, {
      acknowledge_sensitive_output: true,
      ...(data ?? {}),
    });
  }

  async queryDatabase(id: string, data: {
    query: string;
    allow_write?: boolean;
    acknowledge_risky_query?: boolean;
    max_rows?: number;
    timeout_ms?: number;
  }) {
    return this.request("POST", `/databases/${id}/query`, data);
  }

  async getDatabaseRetention(id: string) {
    return this.request("GET", `/databases/${id}/retention`);
  }

  async getDatabaseRecoveryWindow(id: string) {
    return this.request("GET", `/databases/${id}/recovery-window`);
  }

  async setDatabaseRetention(id: string, data: {
    automated_backups?: string | number;
    manual_backups?: string | number;
    wal_archives?: string | number;
  }) {
    return this.request("PUT", `/databases/${id}/retention`, data);
  }

  async updateDatabase(id: string, data: Record<string, unknown>) {
    return this.request("PATCH", `/databases/${id}`, data);
  }

  async bulkUpdateDatabases(data: {
    ids: string[];
    changes: Record<string, unknown>;
    dry_run?: boolean;
    transaction_mode?: "best_effort" | "all_or_nothing";
    transactional?: boolean;
  }) {
    return this.request("POST", "/databases/bulk-update", data);
  }

  async deleteDatabase(id: string, hardDelete?: boolean, confirmLinkedServices?: boolean) {
    const extraPayload = confirmLinkedServices
      ? { confirm_linked_services: true }
      : undefined;
    return this.performConfirmedDelete(`/databases/${id}`, hardDelete, extraPayload);
  }

  async restoreDatabase(id: string, data?: {
    backup_id?: string;
    target_time?: string;
    restore_to?: "new_database" | "in_place";
    new_database_name?: string;
    confirm_destructive?: boolean;
  }) {
    return this.request("POST", `/databases/${id}/restore`, data);
  }

  async cloneDatabase(id: string, data: {
    name: string;
    plan?: "free" | "starter" | "standard" | "pro";
    source?: "live" | "backup" | "point_in_time";
    backup_id?: string;
    target_time?: string;
    sanitize?: boolean;
    sanitize_rules?: Record<string, string>;
  }) {
    return this.request("POST", `/databases/${id}/clone`, data);
  }

  async getDatabaseCloneStatus(id: string) {
    return this.request("GET", `/databases/${id}/clone-status`);
  }

  async listDatabaseRestores(dbId: string, opts?: { limit?: number }) {
    const query: Record<string, string> = {};
    if (typeof opts?.limit === "number" && opts.limit > 0) query.limit = String(opts.limit);
    return this.request("GET", `/databases/${dbId}/restores`, undefined, query);
  }

  async triggerBackup(dbId: string) {
    return this.request("POST", `/databases/${dbId}/backups`);
  }

  async listBackups(dbId: string, opts?: { limit?: number; cursor?: string }) {
    const query: Record<string, string> = {};
    if (typeof opts?.limit === "number" && opts.limit > 0) query.limit = String(opts.limit);
    if (opts?.cursor) query.cursor = opts.cursor;
    return this.request("GET", `/databases/${dbId}/backups`, undefined, query);
  }

  async getDatabaseConnectedServices(dbId: string) {
    return this.request("GET", `/databases/${dbId}/connected-services`);
  }

  async getDatabaseImpact(dbId: string) {
    return this.request("GET", `/databases/${dbId}/impact`);
  }

  async listReplicas(dbId: string) {
    return this.request("GET", `/databases/${dbId}/replicas`);
  }

  async createReplica(dbId: string, data?: { name?: string; region?: string; replication_mode?: string }) {
    return this.request("POST", `/databases/${dbId}/replicas`, data ?? {});
  }

  async promoteReplica(dbId: string, replicaId: string) {
    return this.request("POST", `/databases/${dbId}/replicas/${replicaId}/promote`);
  }

  async enableHA(dbId: string) {
    return this.request("POST", `/databases/${dbId}/ha/enable`);
  }

  // ── Key-Value (Redis) ────────────────────────────────────────────────

  async listKeyValues(filters?: {
    workspace_id?: string;
    plan?: string;
    status?: string;
    name?: string;
    query?: string;
    limit?: number;
    cursor?: string;
  }) {
    const query: Record<string, string> = {};
    if (filters?.workspace_id) query.workspace_id = filters.workspace_id;
    if (filters?.plan) query.plan = filters.plan;
    if (filters?.status) query.status = filters.status;
    if (filters?.name) query.name = filters.name;
    if (filters?.query) query.query = filters.query;
    if (typeof filters?.limit === "number" && filters.limit > 0) query.limit = String(filters.limit);
    if (filters?.cursor) query.cursor = filters.cursor;
    return this.request("GET", "/keyvalue", undefined, query);
  }

  async createKeyValue(data: Record<string, unknown>) {
    return this.request("POST", "/keyvalue", data);
  }

  async getKeyValue(id: string) {
    return this.request("GET", `/keyvalue/${id}`);
  }

  async revealKeyValueCredentials(id: string) {
    return this.request("POST", `/keyvalue/${id}/credentials/reveal`, {
      acknowledge_sensitive_output: true,
    });
  }

  async updateKeyValue(id: string, data: Record<string, unknown>) {
    return this.request("PATCH", `/keyvalue/${id}`, data);
  }

  async deleteKeyValue(id: string, hardDelete?: boolean) {
    return this.performConfirmedDelete(`/keyvalue/${id}`, hardDelete);
  }

  async restoreKeyValue(id: string) {
    return this.request("POST", `/keyvalue/${id}/restore`);
  }

  // ── Logs ─────────────────────────────────────────────────────────────

  async queryLogs(serviceId: string, opts?: {
    limit?: number;
    type?: string;
    since?: string;
    until?: string;
    search?: string;
    regex?: boolean;
    level?: string;
    filter?: string;
  }) {
    const query: Record<string, string> = {};
    if (opts?.limit) query.limit = String(opts.limit);
    if (opts?.type) query.type = opts.type;
    if (opts?.since) query.since = opts.since;
    if (opts?.until) query.until = opts.until;
    if (opts?.search) query.search = opts.search;
    if (opts?.regex) query.regex = "true";
    if (opts?.level) query.level = opts.level;
    if (opts?.filter) query.filter = opts.filter;
    return this.request("GET", `/services/${serviceId}/logs`, undefined, query);
  }

  async listLogDrains(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/log-drains`);
  }

  async createLogDrain(serviceId: string, data: Record<string, unknown>) {
    return this.request("POST", `/services/${serviceId}/log-drains`, data);
  }

  async deleteLogDrain(serviceId: string, drainId: string) {
    return this.request("DELETE", `/services/${serviceId}/log-drains/${drainId}`);
  }

  async getLogDrainStats(serviceId: string, drainId: string) {
    return this.request("GET", `/services/${serviceId}/log-drains/${drainId}/stats`);
  }

  async testLogDrain(serviceId: string, drainId: string) {
    return this.request("POST", `/services/${serviceId}/log-drains/${drainId}/test`, {});
  }

  async listLogAlerts(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/log-alerts`);
  }

  async createLogAlert(serviceId: string, data: Record<string, unknown>) {
    return this.request("POST", `/services/${serviceId}/log-alerts`, data);
  }

  async updateLogAlert(serviceId: string, alertId: string, data: Record<string, unknown>) {
    return this.request("PATCH", `/services/${serviceId}/log-alerts/${alertId}`, data);
  }

  async deleteLogAlert(serviceId: string, alertId: string) {
    return this.request("DELETE", `/services/${serviceId}/log-alerts/${alertId}`);
  }

  async createShellSession(serviceId: string, data?: {
    idle_timeout_minutes?: number;
    working_directory?: string;
  }) {
    return this.request("POST", `/services/${serviceId}/shell`, data ?? {});
  }

  async shellExec(sessionId: string, data: {
    command: string;
    timeout_seconds?: number;
    acknowledge_risky_command?: boolean;
    reason?: string;
  }) {
    return this.request("POST", `/shell/${sessionId}/exec`, data);
  }

  async closeShellSession(sessionId: string) {
    return this.request("DELETE", `/shell/${sessionId}`);
  }

  async listServiceFilesystem(serviceId: string, opts?: {
    path?: string;
    limit?: number;
  }) {
    const query: Record<string, string> = {};
    if (opts?.path) query.path = opts.path;
    if (typeof opts?.limit === "number" && opts.limit > 0) query.limit = String(opts.limit);
    return this.request("GET", `/services/${serviceId}/fs`, undefined, query);
  }

  async readServiceFilesystemFile(serviceId: string, opts: {
    path: string;
    max_bytes?: number;
  }) {
    const query: Record<string, string> = { path: opts.path };
    if (typeof opts.max_bytes === "number" && opts.max_bytes > 0) query.max_bytes = String(opts.max_bytes);
    return this.request("GET", `/services/${serviceId}/fs/read`, undefined, query);
  }

  async searchServiceFilesystem(serviceId: string, opts: {
    path?: string;
    pattern: string;
    recursive?: boolean;
    limit?: number;
  }) {
    const query: Record<string, string> = { pattern: opts.pattern };
    if (opts.path) query.path = opts.path;
    if (typeof opts.recursive === "boolean") query.recursive = opts.recursive ? "true" : "false";
    if (typeof opts.limit === "number" && opts.limit > 0) query.limit = String(opts.limit);
    return this.request("GET", `/services/${serviceId}/fs/search`, undefined, query);
  }

  // ── AI Fix ───────────────────────────────────────────────────────────

  async startAIFix(serviceId: string, data?: {
    hint?: string;
    preview_only?: boolean;
  }) {
    return this.request("POST", `/services/${serviceId}/ai-fix`, data ?? {});
  }

  async getAIFixStatus(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/ai-fix/status`);
  }

  async getAIFixDiagnosis(serviceId: string, deployId?: string) {
    const query: Record<string, string> = {};
    if (deployId) query.deploy_id = deployId;
    return this.request("GET", `/services/${serviceId}/ai-fix/diagnosis`, undefined, query);
  }

  // ── One-Off Jobs ─────────────────────────────────────────────────────

  async runJob(serviceId: string, command: string, opts?: {
    name?: string;
    acknowledge_risky_command?: boolean;
    reason?: string;
  }) {
    return this.request("POST", `/services/${serviceId}/jobs`, {
      command,
      name: opts?.name,
      acknowledge_risky_command: opts?.acknowledge_risky_command,
      reason: opts?.reason,
    });
  }

  async listJobs(serviceId: string, opts?: { limit?: number; cursor?: string }) {
    const query: Record<string, string> = {};
    if (typeof opts?.limit === "number" && opts.limit > 0) query.limit = String(opts.limit);
    if (opts?.cursor) query.cursor = opts.cursor;
    return this.request("GET", `/services/${serviceId}/jobs`, undefined, query);
  }

  async getJob(jobId: string) {
    return this.request("GET", `/jobs/${jobId}`);
  }

  // ── Autoscaling ──────────────────────────────────────────────────────

  async getAutoscalingPolicy(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/autoscaling`);
  }

  async upsertAutoscalingPolicy(serviceId: string, policy: Record<string, unknown>) {
    return this.request("PUT", `/services/${serviceId}/autoscaling`, policy);
  }

  // ── Blueprints ───────────────────────────────────────────────────────

  async listBlueprints(workspaceId?: string) {
    return this.request("GET", "/blueprints", undefined, workspaceId ? { workspace_id: workspaceId } : undefined);
  }

  async createBlueprint(data: Record<string, unknown>) {
    return this.request("POST", "/blueprints", data);
  }

  async getBlueprint(id: string) {
    return this.request("GET", `/blueprints/${id}`);
  }

  async syncBlueprint(id: string) {
    return this.request("POST", `/blueprints/${id}/sync`);
  }

  async updateBlueprint(id: string, data: Record<string, unknown>) {
    return this.request("PATCH", `/blueprints/${id}`, data);
  }

  async deleteBlueprint(id: string) {
    return this.request("DELETE", `/blueprints/${id}`);
  }

  // ── Env Groups ───────────────────────────────────────────────────────

  async listEnvGroups(workspaceId?: string) {
    return this.request("GET", "/env-groups", undefined, workspaceId ? { workspace_id: workspaceId } : undefined);
  }

  async createEnvGroup(data: { name: string; workspace_id?: string }) {
    return this.request("POST", "/env-groups", data);
  }

  async getEnvGroup(id: string) {
    return this.request("GET", `/env-groups/${id}`);
  }

  async updateEnvGroup(id: string, data: { name: string }) {
    return this.request("PATCH", `/env-groups/${id}`, data);
  }

  async deleteEnvGroup(id: string) {
    return this.request("DELETE", `/env-groups/${id}`);
  }

  async listEnvGroupVars(id: string) {
    return this.request("GET", `/env-groups/${id}/vars`);
  }

  async bulkUpdateEnvGroupVars(id: string, vars: Array<{ key: string; value: string; is_secret?: boolean }>) {
    return this.request("PUT", `/env-groups/${id}/vars`, vars);
  }

  async linkServiceToEnvGroup(groupId: string, serviceId: string) {
    return this.request("POST", `/env-groups/${groupId}/link`, { service_id: serviceId });
  }

  async unlinkServiceFromEnvGroup(groupId: string, serviceId: string) {
    return this.request("DELETE", `/env-groups/${groupId}/link/${serviceId}`);
  }

  async listEnvGroupLinkedServices(groupId: string) {
    return this.request("GET", `/env-groups/${groupId}/services`);
  }

  async listEnvGroupLinkedServicesDetailed(groupId: string) {
    return this.request("GET", `/env-groups/${groupId}/services`, undefined, { include_usage: "true" });
  }

  async getServiceDependencies(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/dependencies`);
  }

  async getWorkspaceTopology(workspaceId?: string) {
    if (workspaceId) {
      return this.request("GET", `/workspaces/${workspaceId}/topology`);
    }
    return this.request("GET", "/workspace/topology");
  }

  // ── Metrics ──────────────────────────────────────────────────────────

  async getMetrics(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/metrics`);
  }

  // ── Projects ─────────────────────────────────────────────────────────

  async listProjects(workspaceId?: string) {
    return this.request("GET", "/projects", undefined, workspaceId ? { workspace_id: workspaceId } : undefined);
  }

  // ── Projects ──────────────────────────────────────────────────────────

  async createProject(data: Record<string, unknown>) {
    return this.request("POST", "/projects", data);
  }

  async getProject(id: string) {
    return this.request("GET", `/projects/${id}`);
  }

  async updateProject(id: string, data: Record<string, unknown>) {
    return this.request("PATCH", `/projects/${id}`, data);
  }

  async deleteProject(id: string) {
    return this.request("DELETE", `/projects/${id}`);
  }

  // ── Project Folders ──────────────────────────────────────────────────

  async listProjectFolders(workspaceId?: string) {
    return this.request("GET", "/project-folders", undefined, workspaceId ? { workspace_id: workspaceId } : undefined);
  }

  async createProjectFolder(data: Record<string, unknown>) {
    return this.request("POST", "/project-folders", data);
  }

  async updateProjectFolder(id: string, data: Record<string, unknown>) {
    return this.request("PATCH", `/project-folders/${id}`, data);
  }

  async deleteProjectFolder(id: string) {
    return this.request("DELETE", `/project-folders/${id}`);
  }

  // ── Environments ────────────────────────────────────────────────────

  async listEnvironments(projectId: string) {
    return this.request("GET", `/projects/${projectId}/environments`);
  }

  async createEnvironment(projectId: string, data: Record<string, unknown>) {
    return this.request("POST", `/projects/${projectId}/environments`, data);
  }

  async updateEnvironment(envId: string, data: Record<string, unknown>) {
    return this.request("PATCH", `/environments/${envId}`, data);
  }

  async deleteEnvironment(envId: string) {
    return this.request("DELETE", `/environments/${envId}`);
  }

  // ── Metrics History ─────────────────────────────────────────────────

  async getMetricsHistory(serviceId: string, opts?: { period?: string }) {
    const query: Record<string, string> = {};
    if (opts?.period) query.period = opts.period;
    return this.request("GET", `/services/${serviceId}/metrics/history`, undefined, query);
  }

  // ── Support Tickets ─────────────────────────────────────────────────

  async listSupportTickets(params?: {
    status?: string;
    category?: string;
    component?: string;
    tags?: string[];
    query?: string;
    limit?: number;
    offset?: number;
  }) {
    const qs = new URLSearchParams();
    if (params?.status) qs.set("status", params.status);
    if (params?.category) qs.set("category", params.category);
    if (params?.component) qs.set("component", params.component);
    if (params?.tags?.length) qs.set("tags", params.tags.join(","));
    if (params?.query) qs.set("query", params.query);
    if (params?.limit) qs.set("limit", String(params.limit));
    if (params?.offset) qs.set("offset", String(params.offset));
    const q = qs.toString();
    return this.request("GET", `/support/tickets${q ? `?${q}` : ""}`);
  }

  async createSupportTicket(data: { subject: string; message: string; priority?: string; category?: string; component?: string; tags?: string[] }) {
    return this.request("POST", "/support/tickets", data);
  }

  async getSupportTicket(id: string) {
    return this.request("GET", `/support/tickets/${id}`);
  }

  async addSupportTicketMessage(ticketId: string, message: string) {
    return this.request("POST", `/support/tickets/${ticketId}/messages`, { message });
  }

  async updateSupportTicketTags(ticketId: string, tags: string[]) {
    return this.request("PATCH", `/support/tickets/${ticketId}/tags`, { tags });
  }

  // ── Ops: Support Tickets (admin/ops role required) ─────────────────

  async listOpsTickets(params?: { status?: string; category?: string; priority?: string; component?: string; tags?: string[]; query?: string; limit?: number; offset?: number }) {
    const qs = new URLSearchParams();
    if (params?.status) qs.set("status", params.status);
    if (params?.category) qs.set("category", params.category);
    if (params?.priority) qs.set("priority", params.priority);
    if (params?.component) qs.set("component", params.component);
    if (params?.tags?.length) qs.set("tags", params.tags.join(","));
    if (params?.query) qs.set("query", params.query);
    if (params?.limit) qs.set("limit", String(params.limit));
    if (params?.offset) qs.set("offset", String(params.offset));
    const q = qs.toString();
    return this.request("GET", `/ops/tickets${q ? `?${q}` : ""}`);
  }

  async searchOpsTickets(params?: {
    status?: string;
    category?: string;
    priority?: string;
    component?: string;
    tags?: string[];
    query?: string;
    created_after?: string;
    created_before?: string;
    sort_by?: "priority" | "created_at" | "updated_at";
    sort_order?: "asc" | "desc";
    limit?: number;
    offset?: number;
  }) {
    const qs = new URLSearchParams();
    qs.set("include_meta", "true");
    if (params?.status) qs.set("status", params.status);
    if (params?.category) qs.set("category", params.category);
    if (params?.priority) qs.set("priority", params.priority);
    if (params?.component) qs.set("component", params.component);
    if (params?.tags?.length) qs.set("tags", params.tags.join(","));
    if (params?.query) qs.set("query", params.query);
    if (params?.created_after) qs.set("created_after", params.created_after);
    if (params?.created_before) qs.set("created_before", params.created_before);
    if (params?.sort_by) qs.set("sort_by", params.sort_by);
    if (params?.sort_order) qs.set("sort_order", params.sort_order);
    if (params?.limit) qs.set("limit", String(params.limit));
    if (params?.offset) qs.set("offset", String(params.offset));
    return this.request("GET", `/ops/tickets?${qs.toString()}`);
  }

  async getOpsTicket(id: string) {
    return this.request("GET", `/ops/tickets/${id}`);
  }

  async updateOpsTicket(id: string, data: { status?: string; priority?: string; assigned_to?: string; category?: string; component?: string; tags?: string[] }) {
    return this.request("PATCH", `/ops/tickets/${id}`, data);
  }

  async bulkUpdateOpsTickets(data: { ticket_ids: string[]; status?: string; priority?: string; category?: string; component?: string; tags?: string[]; reason?: string }) {
    return this.request("POST", "/ops/tickets/bulk", data);
  }

  async addOpsTicketMessage(ticketId: string, message: string, isInternal = false) {
    return this.request("POST", `/ops/tickets/${ticketId}/messages`, { message, is_internal: isInternal });
  }

  // ── Billing ─────────────────────────────────────────────────────────

  async getBillingOverview() {
    return this.request("GET", "/billing");
  }

  // ── Registered Domains ──────────────────────────────────────────────

  async listRegisteredDomains() {
    return this.request("GET", "/domains");
  }

  async registerDomain(data: Record<string, unknown>) {
    return this.request("POST", "/domains", data);
  }

  async getRegisteredDomain(id: string) {
    return this.request("GET", `/domains/${id}`);
  }

  async deleteRegisteredDomain(id: string) {
    return this.request("DELETE", `/domains/${id}`);
  }

  async listDnsRecords(domainId: string) {
    return this.request("GET", `/domains/${domainId}/dns`);
  }

  async createDnsRecord(domainId: string, data: Record<string, unknown>) {
    return this.request("POST", `/domains/${domainId}/dns`, data);
  }

  async updateDnsRecord(domainId: string, recordId: string, data: Record<string, unknown>) {
    return this.request("PUT", `/domains/${domainId}/dns/${recordId}`, data);
  }

  async deleteDnsRecord(domainId: string, recordId: string) {
    return this.request("DELETE", `/domains/${domainId}/dns/${recordId}`);
  }

  // ── Auth / User ─────────────────────────────────────────────────────

  async getCurrentUser() {
    return this.request("GET", "/auth/user");
  }

  // ── Workspaces ──────────────────────────────────────────────────────

  async listWorkspaceMembers(workspaceId: string) {
    return this.request("GET", `/workspaces/${workspaceId}/members`);
  }

  async addWorkspaceMember(workspaceId: string, data: { email: string; role?: string }) {
    return this.request("POST", `/workspaces/${workspaceId}/members`, data);
  }

  async updateWorkspaceMemberRole(workspaceId: string, userId: string, data: { role: string }) {
    return this.request("PATCH", `/workspaces/${workspaceId}/members/${userId}`, data);
  }

  async removeWorkspaceMember(workspaceId: string, userId: string) {
    return this.request("DELETE", `/workspaces/${workspaceId}/members/${userId}`);
  }

  async listAuditLogs(workspaceId: string, opts?: { limit?: number; cursor?: string }) {
    const query: Record<string, string> = {};
    if (typeof opts?.limit === "number" && opts.limit > 0) query.limit = String(opts.limit);
    if (opts?.cursor) query.cursor = opts.cursor;
    return this.request("GET", `/workspaces/${workspaceId}/audit-logs`, undefined, query);
  }

  async getWorkspaceRetention(workspaceId: string) {
    return this.request("GET", `/workspaces/${workspaceId}/retention`);
  }

  async setWorkspaceRetention(workspaceId: string, data: {
    audit_logs?: string | number;
    deploy_history?: string | number;
    metric_history?: string | number;
  }) {
    return this.request("PUT", `/workspaces/${workspaceId}/retention`, data);
  }

  async getWorkspaceCompliance(workspaceId: string) {
    return this.request("GET", `/workspaces/${workspaceId}/compliance`);
  }

  async setWorkspaceCompliance(workspaceId: string, data: {
    data_residency?: string;
    audit_log_retention?: string | number;
    require_encryption_at_rest?: boolean;
    require_mfa_for_destructive?: boolean;
    session_timeout_minutes?: number;
    ip_allowlist_required?: boolean;
  }) {
    return this.request("PUT", `/workspaces/${workspaceId}/compliance`, data);
  }

  async getWorkspaceComplianceReport(workspaceId: string, opts?: { framework?: string }) {
    const query: Record<string, string> = {};
    if (opts?.framework) query.framework = opts.framework;
    return this.request("GET", `/workspaces/${workspaceId}/compliance/report`, undefined, query);
  }

  // ── Preview Environments ────────────────────────────────────────────

  async listPreviewEnvironments() {
    return this.request("GET", "/preview-environments");
  }

  async createPreviewEnvironment(data: Record<string, unknown>) {
    return this.request("POST", "/preview-environments", data);
  }

  async updatePreviewEnvironment(id: string, data: Record<string, unknown>) {
    return this.request("PATCH", `/preview-environments/${id}`, data);
  }

  async deletePreviewEnvironment(id: string) {
    return this.request("DELETE", `/preview-environments/${id}`);
  }

  // ── GitHub ───────────────────────────────────────────────────────────

  async listGitHubRepos() {
    return this.request("GET", "/github/repos");
  }

  async listGitHubBranches(owner: string, repo: string) {
    return this.request("GET", `/github/repos/${owner}/${repo}/branches`);
  }

  async listGitHubWorkflows(owner: string, repo: string) {
    return this.request("GET", `/github/repos/${owner}/${repo}/workflows`);
  }

  async listServiceGitHubWorkflows(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/github/workflows`);
  }

  async getServiceGitHubWebhookStatus(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/github/webhook/status`);
  }

  async repairServiceGitHubWebhook(serviceId: string) {
    return this.request("POST", `/services/${serviceId}/github/webhook/repair`);
  }

  async getServiceEventWebhook(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/event-webhook`);
  }

  async setServiceEventWebhook(serviceId: string, data: {
    enabled: boolean;
    url?: string;
    events?: string[];
    secret?: string;
  }) {
    return this.request("PUT", `/services/${serviceId}/event-webhook`, data);
  }

  async testServiceEventWebhook(serviceId: string) {
    return this.request("POST", `/services/${serviceId}/event-webhook/test`);
  }

  // ── Templates ────────────────────────────────────────────────────────

  async listTemplates(params?: { category?: string; query?: string }) {
    return this.request("GET", "/templates", undefined, params);
  }

  async getTemplate(templateId: string) {
    return this.request("GET", `/templates/${templateId}`);
  }

  async deployTemplate(templateId: string, data: {
    workspace_id?: string;
    project_id?: string;
    environment_id?: string;
    name_prefix?: string;
    repo_url?: string;
    branch?: string;
    plan?: string;
    customizations?: Record<string, unknown>;
  }) {
    return this.request("POST", `/templates/${templateId}/deploy`, data);
  }
}
