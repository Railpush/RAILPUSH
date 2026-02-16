import type {
  Service, Deploy, EnvVar, ManagedDatabase, ManagedKeyValue, Blueprint, BlueprintResource, EnvGroup, CustomDomain, User, Backup, LogEntry, BillingOverview,
  GitHubRepo, GitHubBranch, RegisteredDomain, DnsRecord, DomainSearchResult, Project, ProjectFolder, Environment, PreviewEnvironment, OneOffJob, AutoscalingPolicy,
  DatabaseReplica, WorkspaceMember, AuditLogEntry, SamlSSOConfig, Incident, IncidentDetail, OpsOverview, OpsUserItem, OpsWorkspaceItem, OpsServiceItem, OpsDeployItem,
  OpsEmailOutboxItem, OpsBillingCustomerItem, OpsBillingCustomerDetail, OpsTicketItem, OpsTicketDetail, OpsWorkspaceCreditItem, OpsWorkspaceCreditDetail,
  OpsKubeSummary, OpsPerformanceSummary, OpsDatastoreItem, OpsAuditLogEntry, SupportTicket, SupportTicketMessage, BlueprintAISettings,
} from '../types';

const BASE = '/api/v1';

export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...options,
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ message: res.statusText }));
    throw new ApiError(err.error || err.message || res.statusText, res.status);
  }
  if (res.status === 204) return {} as T;
  return res.json();
}

// Auth
export const auth = {
  register: (data: { email: string; password: string; name: string }) =>
    request<{ status: string }>('/auth/register', { method: 'POST', body: JSON.stringify(data) }),
  login: (data: { email: string; password: string }) =>
    request<{ user: User }>('/auth/login', { method: 'POST', body: JSON.stringify(data) }),
  verifyEmail: (token: string) =>
    request<{ status: string }>('/auth/verify', { method: 'POST', body: JSON.stringify({ token }) }),
  resendVerification: (email: string) =>
    request<{ status: string }>('/auth/verify/resend', { method: 'POST', body: JSON.stringify({ email }) }),
  getUser: () => request<{ user: User; workspace?: { id: string; name: string } }>('/auth/user'),
  loginGithub: () => { window.location.href = `${BASE}/auth/github`; },
  logout: async () => {
    try {
      await request<{ status: string }>('/auth/logout', { method: 'POST' });
    } catch {
      // best effort logout to clear server cookie
    } finally {
      window.location.href = '/login';
    }
  },
};

export const settings = {
  getBlueprintAI: () => request<BlueprintAISettings>('/settings/blueprint-ai'),
  updateBlueprintAI: (enabled: boolean) =>
    request<BlueprintAISettings>('/settings/blueprint-ai', { method: 'PUT', body: JSON.stringify({ enabled }) }),
};

// Services
export const services = {
  list: () => request<Service[]>('/services'),
  get: (id: string) => request<Service>(`/services/${id}`),
  create: (data: Partial<Service>) => request<Service>('/services', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: string, data: Partial<Service>) => request<Service>(`/services/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
  delete: (id: string) => request<void>(`/services/${id}`, { method: 'DELETE' }),
  restart: (id: string) => request<void>(`/services/${id}/restart`, { method: 'POST' }),
  suspend: (id: string) => request<void>(`/services/${id}/suspend`, { method: 'POST' }),
  resume: (id: string) => request<void>(`/services/${id}/resume`, { method: 'POST' }),
};

export const projects = {
  list: (workspaceId?: string) =>
    request<Project[]>(workspaceId ? `/projects?workspace_id=${encodeURIComponent(workspaceId)}` : '/projects'),
  get: (id: string) => request<Project>(`/projects/${id}`),
  create: (data: Partial<Project> & { name: string }) =>
    request<Project>('/projects', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: string, data: Partial<Project> & { name?: string }) =>
    request<Project>(`/projects/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
  delete: (id: string) =>
    request<void>(`/projects/${id}`, { method: 'DELETE' }),
  listEnvironments: (projectId: string) =>
    request<Environment[]>(`/projects/${projectId}/environments`),
  createEnvironment: (projectId: string, data: Partial<Environment> & { name: string }) =>
    request<Environment>(`/projects/${projectId}/environments`, { method: 'POST', body: JSON.stringify(data) }),
  updateEnvironment: (id: string, data: Partial<Environment>) =>
    request<Environment>(`/environments/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
  deleteEnvironment: (id: string) =>
    request<void>(`/environments/${id}`, { method: 'DELETE' }),
};

export const projectFolders = {
  list: (workspaceId?: string) =>
    request<ProjectFolder[]>(workspaceId ? `/project-folders?workspace_id=${encodeURIComponent(workspaceId)}` : '/project-folders'),
  create: (data: { workspace_id?: string; name: string }) =>
    request<ProjectFolder>('/project-folders', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: string, data: { name: string }) =>
    request<ProjectFolder>(`/project-folders/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
  delete: (id: string) =>
    request<void>(`/project-folders/${id}`, { method: 'DELETE' }),
};

// Deploys
export const deploys = {
  list: (serviceId: string) => request<Deploy[]>(`/services/${serviceId}/deploys`),
  get: (serviceId: string, deployId: string) => request<Deploy>(`/services/${serviceId}/deploys/${deployId}`),
  trigger: (serviceId: string, data?: { commit?: string; clearCache?: boolean }) =>
    request<Deploy>(`/services/${serviceId}/deploys`, { method: 'POST', body: JSON.stringify(data || {}) }),
  rollback: (serviceId: string, deployId: string) =>
    request<Deploy>(`/services/${serviceId}/deploys/${deployId}/rollback`, { method: 'POST' }),
};

// Env Vars
export const envVars = {
  list: (serviceId: string) => request<EnvVar[]>(`/services/${serviceId}/env-vars`),
  update: (serviceId: string, vars: EnvVar[]) =>
    request<EnvVar[]>(`/services/${serviceId}/env-vars`, { method: 'PUT', body: JSON.stringify(vars) }),
};

// Databases
export const databases = {
  list: () => request<ManagedDatabase[]>('/databases'),
  get: (id: string) => request<ManagedDatabase>(`/databases/${id}`),
  create: (data: Partial<ManagedDatabase>) => request<ManagedDatabase>('/databases', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: string, data: Partial<ManagedDatabase>) => request<ManagedDatabase>(`/databases/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
  delete: (id: string) => request<void>(`/databases/${id}`, { method: 'DELETE' }),
  listBackups: (id: string) => request<Backup[]>(`/databases/${id}/backups`),
  triggerBackup: (id: string) => request<Backup>(`/databases/${id}/backups`, { method: 'POST' }),
};

export const databaseReplicas = {
  list: (databaseId: string) => request<DatabaseReplica[]>(`/databases/${databaseId}/replicas`),
  create: (databaseId: string, data: { name?: string; region?: string; replication_mode?: string }) =>
    request<DatabaseReplica>(`/databases/${databaseId}/replicas`, { method: 'POST', body: JSON.stringify(data) }),
  promote: (databaseId: string, replicaId: string) =>
    request<{ status: string }>(`/databases/${databaseId}/replicas/${replicaId}/promote`, { method: 'POST' }),
  enableHA: (databaseId: string) =>
    request<{ status: string; standby_replica_id: string }>(`/databases/${databaseId}/ha/enable`, { method: 'POST' }),
};

// Key Value
export const keyvalue = {
  list: () => request<ManagedKeyValue[]>('/keyvalue'),
  get: (id: string) => request<ManagedKeyValue>(`/keyvalue/${id}`),
  create: (data: Partial<ManagedKeyValue>) => request<ManagedKeyValue>('/keyvalue', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: string, data: Partial<ManagedKeyValue>) => request<ManagedKeyValue>(`/keyvalue/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
  delete: (id: string) => request<void>(`/keyvalue/${id}`, { method: 'DELETE' }),
};

// Blueprints
export const blueprints = {
  list: () => request<Blueprint[]>('/blueprints'),
  get: (id: string) => request<Blueprint & { resources: BlueprintResource[] }>(`/blueprints/${id}`),
  create: (data: Partial<Blueprint>) => request<Blueprint>('/blueprints', { method: 'POST', body: JSON.stringify(data) }),
  sync: (id: string) => request<void>(`/blueprints/${id}/sync`, { method: 'POST' }),
  delete: (id: string) => request<void>(`/blueprints/${id}`, { method: 'DELETE' }),
};

// Env Groups
export const envGroups = {
  list: () => request<EnvGroup[]>('/env-groups'),
  get: (id: string) => request<EnvGroup>(`/env-groups/${id}`),
  create: (data: Partial<EnvGroup>) => request<EnvGroup>('/env-groups', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: string, data: Partial<EnvGroup>) => request<EnvGroup>(`/env-groups/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
  delete: (id: string) => request<void>(`/env-groups/${id}`, { method: 'DELETE' }),
};

export const previewEnvironments = {
  list: (workspaceId?: string) =>
    request<PreviewEnvironment[]>(workspaceId ? `/preview-environments?workspace_id=${encodeURIComponent(workspaceId)}` : '/preview-environments'),
};

export const oneOffJobs = {
  list: (serviceId: string) => request<OneOffJob[]>(`/services/${serviceId}/jobs`),
  run: (serviceId: string, data: { name?: string; command: string }) =>
    request<OneOffJob>(`/services/${serviceId}/jobs`, { method: 'POST', body: JSON.stringify(data) }),
  get: (jobId: string) => request<OneOffJob>(`/jobs/${jobId}`),
};

export const autoscaling = {
  get: (serviceId: string) => request<AutoscalingPolicy>(`/services/${serviceId}/autoscaling`),
  update: (serviceId: string, data: Partial<AutoscalingPolicy>) =>
    request<AutoscalingPolicy>(`/services/${serviceId}/autoscaling`, { method: 'PUT', body: JSON.stringify(data) }),
};

export const workspaceAdmin = {
  listMembers: (workspaceId: string) => request<WorkspaceMember[]>(`/workspaces/${workspaceId}/members`),
  addMember: (workspaceId: string, data: { email: string; role: string }) =>
    request<{ status: string }>(`/workspaces/${workspaceId}/members`, { method: 'POST', body: JSON.stringify(data) }),
  updateMemberRole: (workspaceId: string, userId: string, role: string) =>
    request<{ status: string }>(`/workspaces/${workspaceId}/members/${userId}`, { method: 'PATCH', body: JSON.stringify({ role }) }),
  removeMember: (workspaceId: string, userId: string) =>
    request<{ status: string }>(`/workspaces/${workspaceId}/members/${userId}`, { method: 'DELETE' }),
  listAuditLogs: (workspaceId: string, limit?: number) =>
    request<AuditLogEntry[]>(`/workspaces/${workspaceId}/audit-logs${limit ? `?limit=${limit}` : ''}`),
  auditCsvUrl: (workspaceId: string) => `${BASE}/workspaces/${workspaceId}/audit-logs.csv`,
  getSamlConfig: (workspaceId: string) => request<SamlSSOConfig>(`/workspaces/${workspaceId}/sso/saml/config`),
  upsertSamlConfig: (workspaceId: string, data: SamlSSOConfig) =>
    request<SamlSSOConfig>(`/workspaces/${workspaceId}/sso/saml/config`, { method: 'PUT', body: JSON.stringify(data) }),
};

// Billing
export const billing = {
  getOverview: () => request<BillingOverview>('/billing'),
  createCheckoutSession: (returnUrl?: string) =>
    request<{ url: string }>('/billing/checkout-session', { method: 'POST', body: JSON.stringify({ return_url: returnUrl }) }),
  createPortalSession: (returnUrl?: string) =>
    request<{ url: string }>('/billing/portal-session', { method: 'POST', body: JSON.stringify({ return_url: returnUrl }) }),
  getPaymentMethod: () =>
    request<{ has_payment_method: boolean; last4?: string; brand?: string }>('/billing/payment-method'),
};

// GitHub
export const github = {
  listRepos: () => request<GitHubRepo[]>('/github/repos'),
  listBranches: (owner: string, repo: string) =>
    request<GitHubBranch[]>(`/github/repos/${owner}/${repo}/branches`),
};

// Custom Domains
export const domains = {
  list: (serviceId: string) => request<CustomDomain[]>(`/services/${serviceId}/custom-domains`),
  add: (serviceId: string, domain: string) =>
    request<CustomDomain>(`/services/${serviceId}/custom-domains`, { method: 'POST', body: JSON.stringify({ domain }) }),
  remove: (serviceId: string, domain: string) =>
    request<void>(`/services/${serviceId}/custom-domains/${domain}`, { method: 'DELETE' }),
};

// Registered Domains
export const registeredDomains = {
  search: (query: string, tlds?: string[]) =>
    request<DomainSearchResult[]>('/domains/search', { method: 'POST', body: JSON.stringify({ query, tlds }) }),
  register: (domain: string) =>
    request<RegisteredDomain>('/domains', { method: 'POST', body: JSON.stringify({ domain }) }),
  list: () => request<RegisteredDomain[]>('/domains'),
  get: (id: string) => request<RegisteredDomain>(`/domains/${id}`),
  update: (id: string, data: Partial<RegisteredDomain>) =>
    request<RegisteredDomain>(`/domains/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
  delete: (id: string) => request<void>(`/domains/${id}`, { method: 'DELETE' }),
  renew: (id: string) => request<RegisteredDomain>(`/domains/${id}/renew`, { method: 'POST' }),
};

// DNS Records
export const dnsRecords = {
  list: (domainId: string) => request<DnsRecord[]>(`/domains/${domainId}/dns`),
  create: (domainId: string, data: Partial<DnsRecord>) =>
    request<DnsRecord>(`/domains/${domainId}/dns`, { method: 'POST', body: JSON.stringify(data) }),
  update: (domainId: string, recordId: string, data: Partial<DnsRecord>) =>
    request<DnsRecord>(`/domains/${domainId}/dns/${recordId}`, { method: 'PUT', body: JSON.stringify(data) }),
  delete: (domainId: string, recordId: string) =>
    request<void>(`/domains/${domainId}/dns/${recordId}`, { method: 'DELETE' }),
};

// Ops / Incidents
export const ops = {
  overview: () => request<OpsOverview>('/ops/overview'),
  getSettings: () => request<Record<string, unknown>>('/ops/settings'),
  enableAutoDeployAll: (confirm: string) =>
    request<{ status: string; updated: number }>('/ops/actions/auto-deploy/enable-all', { method: 'POST', body: JSON.stringify({ confirm }) }),
  sendTestEmail: (to: string) =>
    request<{ status: string; id?: string }>('/ops/actions/email/test', { method: 'POST', body: JSON.stringify({ to }) }),
  // Billing
  listBillingCustomers: (params?: { query?: string; status?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.query) qs.set('query', params.query);
    if (params?.status) qs.set('status', params.status);
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.offset) qs.set('offset', String(params.offset));
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<OpsBillingCustomerItem[]>(`/ops/billing/customers${suffix}`);
  },
  getBillingCustomer: (id: string) => request<OpsBillingCustomerDetail>(`/ops/billing/customers/${encodeURIComponent(id)}`),

  // Tickets
  listTickets: (params?: { query?: string; status?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.query) qs.set('query', params.query);
    if (params?.status) qs.set('status', params.status);
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.offset) qs.set('offset', String(params.offset));
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<OpsTicketItem[]>(`/ops/tickets${suffix}`);
  },
  getTicket: (id: string) => request<OpsTicketDetail>(`/ops/tickets/${encodeURIComponent(id)}`),
  updateTicket: (id: string, data: { status?: string; priority?: string; assigned_to?: string }) =>
    request<{ status: string }>(`/ops/tickets/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(data) }),
  createTicketMessage: (id: string, data: { message: string; is_internal?: boolean }) =>
    request<SupportTicketMessage>(`/ops/tickets/${encodeURIComponent(id)}/messages`, { method: 'POST', body: JSON.stringify(data) }),

  // Credits
  listCreditsWorkspaces: (params?: { query?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.query) qs.set('query', params.query);
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.offset) qs.set('offset', String(params.offset));
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<OpsWorkspaceCreditItem[]>(`/ops/credits/workspaces${suffix}`);
  },
  getCreditsWorkspace: (workspaceId: string) =>
    request<OpsWorkspaceCreditDetail>(`/ops/credits/workspaces/${encodeURIComponent(workspaceId)}`),
  grantCredits: (workspaceId: string, data: { amount_cents: number; reason?: string }) =>
    request<{ status: string; balance_cents: number }>(`/ops/credits/workspaces/${encodeURIComponent(workspaceId)}/grant`, { method: 'POST', body: JSON.stringify(data) }),

  // Technical / Performance
  getKubeSummary: () => request<OpsKubeSummary>('/ops/kube/summary'),
  getPerformanceSummary: (params?: { window_hours?: number }) => {
    const qs = new URLSearchParams();
    if (params?.window_hours) qs.set('window_hours', String(params.window_hours));
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<OpsPerformanceSummary>(`/ops/performance${suffix}`);
  },
  listUsers: (params?: { query?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.query) qs.set('query', params.query);
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.offset) qs.set('offset', String(params.offset));
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<OpsUserItem[]>(`/ops/users${suffix}`);
  },
  listWorkspaces: (params?: { query?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.query) qs.set('query', params.query);
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.offset) qs.set('offset', String(params.offset));
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<OpsWorkspaceItem[]>(`/ops/workspaces${suffix}`);
  },
  listServices: (params?: { query?: string; status?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.query) qs.set('query', params.query);
    if (params?.status) qs.set('status', params.status);
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.offset) qs.set('offset', String(params.offset));
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<OpsServiceItem[]>(`/ops/services${suffix}`);
  },
  listDeploys: (params?: { query?: string; status?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.query) qs.set('query', params.query);
    if (params?.status) qs.set('status', params.status);
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.offset) qs.set('offset', String(params.offset));
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<OpsDeployItem[]>(`/ops/deploys${suffix}`);
  },
  listEmailOutbox: (params?: { status?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.status) qs.set('status', params.status);
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.offset) qs.set('offset', String(params.offset));
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<OpsEmailOutboxItem[]>(`/ops/email/outbox${suffix}`);
  },
  queryServiceLogs: (serviceId: string, params?: { limit?: number; type?: 'runtime' | 'deploy' }) => {
    const qs = new URLSearchParams();
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.type) qs.set('type', params.type);
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<unknown>(`/ops/services/${encodeURIComponent(serviceId)}/logs${suffix}`);
  },
  listIncidents: (params?: { state?: 'active' | 'resolved' | 'all'; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    qs.set('state', params?.state || 'active');
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.offset) qs.set('offset', String(params.offset));
    return request<Incident[]>(`/ops/incidents?${qs}`);
  },
  getIncident: (id: string, params?: { events_limit?: number }) => {
    const qs = new URLSearchParams();
    if (params?.events_limit) qs.set('events_limit', String(params.events_limit));
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<IncidentDetail>(`/ops/incidents/${encodeURIComponent(id)}${suffix}`);
  },
  acknowledgeIncident: (id: string, data?: { note?: string }) =>
    request<{ status: string; ack?: unknown }>(`/ops/incidents/${encodeURIComponent(id)}/ack`, { method: 'POST', body: JSON.stringify(data || {}) }),
  silenceIncident: (id: string, data?: { scope?: 'group' | 'alertname'; duration_minutes?: number; comment?: string }) =>
    request<{ status: string; silence?: unknown }>(`/ops/incidents/${encodeURIComponent(id)}/silence`, { method: 'POST', body: JSON.stringify(data || {}) }),

  // Ops: Admin actions (must-have)
  setUserRole: (userId: string, role: string) =>
    request<{ status: string }>(`/ops/users/${encodeURIComponent(userId)}`, { method: 'PATCH', body: JSON.stringify({ role }) }),
  suspendUser: (userId: string) =>
    request<{ status: string }>(`/ops/users/${encodeURIComponent(userId)}/suspend`, { method: 'POST' }),
  resumeUser: (userId: string) =>
    request<{ status: string }>(`/ops/users/${encodeURIComponent(userId)}/resume`, { method: 'POST' }),

  suspendWorkspace: (workspaceId: string) =>
    request<{ status: string }>(`/ops/workspaces/${encodeURIComponent(workspaceId)}/suspend`, { method: 'POST' }),
  resumeWorkspace: (workspaceId: string) =>
    request<{ status: string }>(`/ops/workspaces/${encodeURIComponent(workspaceId)}/resume`, { method: 'POST' }),

  restartService: (serviceId: string) =>
    request<{ status: string }>(`/ops/services/${encodeURIComponent(serviceId)}/restart`, { method: 'POST' }),
  suspendService: (serviceId: string) =>
    request<{ status: string }>(`/ops/services/${encodeURIComponent(serviceId)}/suspend`, { method: 'POST' }),
  resumeService: (serviceId: string) =>
    request<{ status: string }>(`/ops/services/${encodeURIComponent(serviceId)}/resume`, { method: 'POST' }),

  // Ops: Datastores
  listDatastores: (params?: { query?: string; kind?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.query) qs.set('query', params.query);
    if (params?.kind) qs.set('kind', params.kind);
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.offset) qs.set('offset', String(params.offset));
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<OpsDatastoreItem[]>(`/ops/datastores${suffix}`);
  },

  // Ops: Audit log (global)
  listAuditLogs: (params?: { query?: string; limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.query) qs.set('query', params.query);
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.offset) qs.set('offset', String(params.offset));
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<OpsAuditLogEntry[]>(`/ops/audit-logs${suffix}`);
  },
};

// Support (customer-facing)
export const support = {
  listTickets: (params?: { limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.offset) qs.set('offset', String(params.offset));
    const suffix = qs.toString() ? `?${qs}` : '';
    return request<SupportTicket[]>(`/support/tickets${suffix}`);
  },
  createTicket: (data: { subject: string; message: string; priority?: string }) =>
    request<{ ticket: SupportTicket; messages: SupportTicketMessage[] }>('/support/tickets', { method: 'POST', body: JSON.stringify(data) }),
  getTicket: (id: string) => request<{ ticket: SupportTicket; messages: SupportTicketMessage[] }>(`/support/tickets/${encodeURIComponent(id)}`),
  createMessage: (id: string, data: { message: string }) =>
    request<SupportTicketMessage>(`/support/tickets/${encodeURIComponent(id)}/messages`, { method: 'POST', body: JSON.stringify(data) }),
};

// Logs
export const logs = {
  query: (serviceId: string, params?: { since?: string; until?: string; search?: string; limit?: number; type?: string }) => {
    const qs = new URLSearchParams();
    if (params?.since) qs.set('since', params.since);
    if (params?.until) qs.set('until', params.until);
    if (params?.search) qs.set('search', params.search);
    if (params?.limit) qs.set('limit', String(params.limit));
    if (params?.type) qs.set('type', params.type);
    return request<LogEntry[]>(`/services/${serviceId}/logs?${qs}`);
  },
};

// WebSocket helpers
export function connectLogStream(serviceId: string, onMessage: (entry: LogEntry) => void): WebSocket {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const ws = new WebSocket(`${proto}//${window.location.host}/ws/logs/${serviceId}`);
  ws.onmessage = (e) => { try { onMessage(JSON.parse(e.data)); } catch { /* ignore malformed websocket payload */ } };
  return ws;
}

export function connectBuildStream(deployId: string, onMessage: (line: string) => void): WebSocket {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const ws = new WebSocket(`${proto}//${window.location.host}/ws/builds/${deployId}`);
  ws.onmessage = (e) => onMessage(e.data);
  return ws;
}

export function connectEventStream(onMessage: (event: unknown) => void): WebSocket {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const ws = new WebSocket(`${proto}//${window.location.host}/ws/events`);
  ws.onmessage = (e) => { try { onMessage(JSON.parse(e.data)); } catch { /* ignore malformed websocket payload */ } };
  return ws;
}
