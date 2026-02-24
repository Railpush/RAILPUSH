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

  constructor(config: RailPushClientConfig) {
    this.baseUrl = config.apiUrl.replace(/\/+$/, "");
    this.apiKey = config.apiKey;
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
      throw new RailPushAPIError(res.status, text);
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

  // ── Services ─────────────────────────────────────────────────────────

  async listServices(workspaceId?: string) {
    return this.request("GET", "/services", undefined, workspaceId ? { workspace_id: workspaceId } : undefined);
  }

  async searchServices(filters: { type?: string; status?: string; runtime?: string; name?: string; suspended?: string }) {
    const query: Record<string, string> = {};
    if (filters.type) query.type = filters.type;
    if (filters.status) query.status = filters.status;
    if (filters.runtime) query.runtime = filters.runtime;
    if (filters.name) query.name = filters.name;
    if (filters.suspended) query.suspended = filters.suspended;
    return this.request("GET", "/services", undefined, query);
  }

  async createService(data: Record<string, unknown>) {
    return this.request("POST", "/services", data);
  }

  async getService(id: string) {
    return this.request("GET", `/services/${id}`);
  }

  async updateService(id: string, data: Record<string, unknown>) {
    return this.request("PATCH", `/services/${id}`, data);
  }

  async deleteService(id: string) {
    return this.request("DELETE", `/services/${id}`);
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

  // ── Deploys ──────────────────────────────────────────────────────────

  async triggerDeploy(serviceId: string, data?: { commit_sha?: string; branch?: string }) {
    return this.request("POST", `/services/${serviceId}/deploys`, data ?? {});
  }

  async listDeploys(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/deploys`);
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

  // ── Environment Variables ────────────────────────────────────────────

  async listEnvVars(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/env-vars`);
  }

  async bulkUpdateEnvVars(serviceId: string, vars: Array<{ key: string; value: string; is_secret?: boolean }>) {
    return this.request("PUT", `/services/${serviceId}/env-vars`, vars);
  }

  async mergeEnvVars(serviceId: string, envVars: Array<{ key: string; value: string; is_secret?: boolean }>, deleteKeys?: string[]) {
    return this.request("PATCH", `/services/${serviceId}/env-vars`, {
      env_vars: envVars,
      delete: deleteKeys ?? [],
    });
  }

  // ── Custom Domains ───────────────────────────────────────────────────

  async listCustomDomains(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/custom-domains`);
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

  async listDatabases(workspaceId?: string) {
    return this.request("GET", "/databases", undefined, workspaceId ? { workspace_id: workspaceId } : undefined);
  }

  async createDatabase(data: Record<string, unknown>) {
    return this.request("POST", "/databases", data);
  }

  async getDatabase(id: string) {
    return this.request("GET", `/databases/${id}`);
  }

  async updateDatabase(id: string, data: Record<string, unknown>) {
    return this.request("PATCH", `/databases/${id}`, data);
  }

  async deleteDatabase(id: string) {
    return this.request("DELETE", `/databases/${id}`);
  }

  async triggerBackup(dbId: string) {
    return this.request("POST", `/databases/${dbId}/backups`);
  }

  async listBackups(dbId: string) {
    return this.request("GET", `/databases/${dbId}/backups`);
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

  async listKeyValues(workspaceId?: string) {
    return this.request("GET", "/keyvalue", undefined, workspaceId ? { workspace_id: workspaceId } : undefined);
  }

  async createKeyValue(data: Record<string, unknown>) {
    return this.request("POST", "/keyvalue", data);
  }

  async getKeyValue(id: string) {
    return this.request("GET", `/keyvalue/${id}`);
  }

  async updateKeyValue(id: string, data: Record<string, unknown>) {
    return this.request("PATCH", `/keyvalue/${id}`, data);
  }

  async deleteKeyValue(id: string) {
    return this.request("DELETE", `/keyvalue/${id}`);
  }

  // ── Logs ─────────────────────────────────────────────────────────────

  async queryLogs(serviceId: string, opts?: { limit?: number; type?: string }) {
    const query: Record<string, string> = {};
    if (opts?.limit) query.limit = String(opts.limit);
    if (opts?.type) query.type = opts.type;
    return this.request("GET", `/services/${serviceId}/logs`, undefined, query);
  }

  // ── AI Fix ───────────────────────────────────────────────────────────

  async startAIFix(serviceId: string) {
    return this.request("POST", `/services/${serviceId}/ai-fix`);
  }

  async getAIFixStatus(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/ai-fix/status`);
  }

  // ── One-Off Jobs ─────────────────────────────────────────────────────

  async runJob(serviceId: string, command: string, name?: string) {
    return this.request("POST", `/services/${serviceId}/jobs`, { command, name });
  }

  async listJobs(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/jobs`);
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

  async listSupportTickets() {
    return this.request("GET", "/support/tickets");
  }

  async createSupportTicket(data: { subject: string; message: string; priority?: string; category?: string }) {
    return this.request("POST", "/support/tickets", data);
  }

  async getSupportTicket(id: string) {
    return this.request("GET", `/support/tickets/${id}`);
  }

  async addSupportTicketMessage(ticketId: string, message: string) {
    return this.request("POST", `/support/tickets/${ticketId}/messages`, { message });
  }

  // ── Ops: Support Tickets (admin/ops role required) ─────────────────

  async listOpsTickets(params?: { status?: string; category?: string; query?: string; limit?: number; offset?: number }) {
    const qs = new URLSearchParams();
    if (params?.status) qs.set("status", params.status);
    if (params?.category) qs.set("category", params.category);
    if (params?.query) qs.set("query", params.query);
    if (params?.limit) qs.set("limit", String(params.limit));
    if (params?.offset) qs.set("offset", String(params.offset));
    const q = qs.toString();
    return this.request("GET", `/ops/tickets${q ? `?${q}` : ""}`);
  }

  async getOpsTicket(id: string) {
    return this.request("GET", `/ops/tickets/${id}`);
  }

  async updateOpsTicket(id: string, data: { status?: string; priority?: string; assigned_to?: string; category?: string }) {
    return this.request("PATCH", `/ops/tickets/${id}`, data);
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

  async listAuditLogs(workspaceId: string) {
    return this.request("GET", `/workspaces/${workspaceId}/audit-logs`);
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
}
