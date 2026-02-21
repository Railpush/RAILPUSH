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
    try {
      return JSON.parse(text);
    } catch {
      return { raw: text };
    }
  }

  // ── Services ─────────────────────────────────────────────────────────

  async listServices(workspaceId?: string) {
    return this.request("GET", "/services", undefined, workspaceId ? { workspace_id: workspaceId } : undefined);
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

  // ── Custom Domains ───────────────────────────────────────────────────

  async listCustomDomains(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/custom-domains`);
  }

  async addCustomDomain(serviceId: string, domain: string) {
    return this.request("POST", `/services/${serviceId}/custom-domains`, { domain });
  }

  async deleteCustomDomain(serviceId: string, domain: string) {
    return this.request("DELETE", `/services/${serviceId}/custom-domains/${encodeURIComponent(domain)}`);
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
    return this.request("POST", `/databases/${dbId}/ha`);
  }

  // ── Key-Value (Redis) ────────────────────────────────────────────────

  async listKeyValues(workspaceId?: string) {
    return this.request("GET", "/key-value", undefined, workspaceId ? { workspace_id: workspaceId } : undefined);
  }

  async createKeyValue(data: Record<string, unknown>) {
    return this.request("POST", "/key-value", data);
  }

  async getKeyValue(id: string) {
    return this.request("GET", `/key-value/${id}`);
  }

  async deleteKeyValue(id: string) {
    return this.request("DELETE", `/key-value/${id}`);
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
    return this.request("GET", `/services/${serviceId}/ai-fix`);
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
    return this.request("PUT", `/env-groups/${id}`, data);
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
    return this.request("POST", `/env-groups/${groupId}/links`, { service_id: serviceId });
  }

  async unlinkServiceFromEnvGroup(groupId: string, serviceId: string) {
    return this.request("DELETE", `/env-groups/${groupId}/links/${serviceId}`);
  }

  async listEnvGroupLinkedServices(groupId: string) {
    return this.request("GET", `/env-groups/${groupId}/links`);
  }

  // ── Metrics ──────────────────────────────────────────────────────────

  async getMetrics(serviceId: string) {
    return this.request("GET", `/services/${serviceId}/metrics`);
  }

  // ── Projects ─────────────────────────────────────────────────────────

  async listProjects(workspaceId?: string) {
    return this.request("GET", "/projects", undefined, workspaceId ? { workspace_id: workspaceId } : undefined);
  }

  // ── GitHub ───────────────────────────────────────────────────────────

  async listGitHubRepos() {
    return this.request("GET", "/github/repos");
  }

  async listGitHubBranches(owner: string, repo: string) {
    return this.request("GET", `/github/repos/${owner}/${repo}/branches`);
  }
}
