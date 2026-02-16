export type ServiceType = 'web' | 'pserv' | 'worker' | 'cron' | 'static' | 'keyvalue';
export type ServiceStatus = 'created' | 'building' | 'deploying' | 'live' | 'failed' | 'suspended' | 'deactivated';
export type DeployStatus = 'pending' | 'building' | 'deploying' | 'live' | 'failed' | 'cancelled';
export type DeployTrigger = 'git_push' | 'manual' | 'blueprint' | 'rollback' | 'preview' | 'autoscale' | 'github_push';
export type Runtime = 'docker' | 'image' | 'node' | 'python' | 'go' | 'ruby' | 'rust' | 'elixir';

export interface User {
  id: string;
  github_id: number;
  username: string;
  email: string;
  blueprint_ai_autogen_enabled?: boolean;
  avatar_url: string;
  role: string;
  created_at: string;
}

export interface BlueprintAISettings {
  enabled: boolean;
  available: boolean;
  model: string;
}

export interface Workspace {
  id: string;
  name: string;
  owner_id: string;
  deploy_policy: string;
  created_at: string;
}

export interface Project {
  id: string;
  workspace_id: string;
  folder_id: string | null;
  name: string;
  environments?: Environment[];
  created_at: string;
}

export interface ProjectFolder {
  id: string;
  workspace_id: string;
  name: string;
  created_at: string;
}

export interface Environment {
  id: string;
  project_id: string;
  name: string;
  is_protected: boolean;
  created_at: string;
}

export interface Service {
  id: string;
  workspace_id: string;
  project_id: string | null;
  environment_id: string | null;
  name: string;
  subdomain?: string;
  public_url?: string;
  type: ServiceType;
  runtime: Runtime;
  repo_url: string;
  branch: string;
  build_command: string;
  start_command: string;
  dockerfile_path: string;
  docker_context: string;
  image_url: string;
  health_check_path: string;
  port: number;
  auto_deploy: boolean;
  is_suspended: boolean;
  max_shutdown_delay: number;
  pre_deploy_command: string;
  static_publish_path: string;
  schedule: string;
  plan: string;
  instances: number;
  status: ServiceStatus;
  container_id: string;
  host_port: number;
  created_at: string;
  updated_at: string;
  latest_deploy?: Deploy;
}

export interface Deploy {
  id: string;
  service_id: string;
  trigger: DeployTrigger;
  status: DeployStatus;
  commit_sha: string;
  commit_message: string;
  branch: string;
  image_tag: string;
  build_log: string;
  started_at: string;
  finished_at: string;
  created_by: string;
}

export interface EnvVar {
  id: string;
  key: string;
  value: string;
  is_secret: boolean;
}

export interface EnvGroup {
  id: string;
  workspace_id: string;
  name: string;
  env_vars: EnvVar[];
  created_at: string;
}

export interface CustomDomain {
  id: string;
  service_id: string;
  domain: string;
  verified: boolean;
  tls_provisioned: boolean;
  created_at: string;
}

export interface ManagedDatabase {
  id: string;
  workspace_id: string;
  name: string;
  plan: string;
  pg_version: number;
  container_id: string;
  host: string;
  port: number;
  db_name: string;
  username: string;
  password?: string;
  internal_url: string;
  external_url: string;
  status: string;
  ha_enabled?: boolean;
  ha_strategy?: string;
  standby_replica_id?: string | null;
  created_at: string;
}

export interface ManagedKeyValue {
  id: string;
  workspace_id: string;
  name: string;
  plan: string;
  container_id: string;
  host: string;
  port: number;
  password?: string;
  internal_url: string;
  external_url: string;
  maxmemory_policy: string;
  status: string;
  created_at: string;
}

export interface BlueprintResource {
  blueprint_id: string;
  resource_type: 'service' | 'database' | 'keyvalue';
  resource_id: string;
  resource_name: string;
}

export interface Blueprint {
  id: string;
  workspace_id: string;
  name: string;
  repo_url: string;
  branch: string;
  file_path: string;
  last_synced_at: string;
  last_sync_status: string;
  created_at: string;
  resources?: BlueprintResource[];
}

export interface Disk {
  id: string;
  service_id: string;
  name: string;
  mount_path: string;
  size_gb: number;
  created_at: string;
}

export interface Backup {
  id: string;
  resource_type: string;
  resource_id: string;
  file_path: string;
  size_bytes: number;
  started_at: string;
  finished_at: string;
  status: string;
}

export interface LogEntry {
  timestamp: string;
  level: 'info' | 'warn' | 'error' | 'debug';
  message: string;
  instance_id: string;
}

export interface ApiKey {
  id: string;
  name: string;
  key?: string;
  scopes: string[];
  expires_at: string;
  created_at: string;
}

export interface Metrics {
  cpu: MetricPoint[];
  memory: MetricPoint[];
  network_in: MetricPoint[];
  network_out: MetricPoint[];
}

export interface MetricPoint {
  timestamp: string;
  value: number;
}

export interface BillingLineItem {
  resource_type: string;
  resource_id: string;
  resource_name: string;
  plan: string;
  monthly_cost: number;
}

export interface BillingOverview {
  has_payment_method: boolean;
  payment_method_last4: string;
  payment_method_brand: string;
  subscription_status: string;
  current_plan?: string;
  items: BillingLineItem[];
  monthly_total: number;
}

export interface GitHubRepo {
  id: number;
  full_name: string;
  name: string;
  private: boolean;
  clone_url: string;
  default_branch: string;
  updated_at: string;
}

export interface GitHubBranch {
  name: string;
  protected: boolean;
}

export interface DatabaseReplica {
  id: string;
  primary_database_id: string;
  workspace_id: string;
  name: string;
  region: string;
  container_id: string;
  host: string;
  port: number;
  status: string;
  replication_mode: string;
  lag_seconds: number;
  promoted: boolean;
  created_at: string;
  updated_at: string;
}

export interface WorkspaceMember {
  workspace_id: string;
  user_id: string;
  role: 'owner' | 'admin' | 'developer' | 'viewer';
  email?: string;
  username?: string;
  joined_at: string;
}

export interface AuditLogEntry {
  id: string;
  workspace_id: string;
  user_id: string;
  action: string;
  resource_type: string;
  resource_id: string;
  details_json: Record<string, unknown>;
  created_at: string;
}

export interface SamlSSOConfig {
  workspace_id: string;
  enabled: boolean;
  entity_id: string;
  acs_url: string;
  metadata_url: string;
  idp_sso_url: string;
  idp_cert_pem: string;
  allowed_domains: string[];
  created_at?: string;
  updated_at?: string;
}

export interface PreviewEnvironment {
  id: string;
  workspace_id: string;
  service_id: string | null;
  repository: string;
  pr_number: number;
  pr_title: string;
  pr_branch: string;
  base_branch: string;
  commit_sha: string;
  status: string;
  created_at: string;
  updated_at: string;
  closed_at: string | null;
}

export interface OneOffJob {
  id: string;
  workspace_id: string;
  service_id: string | null;
  name: string;
  command: string;
  status: string;
  exit_code: number | null;
  logs: string;
  created_by: string | null;
  started_at: string | null;
  finished_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface AutoscalingPolicy {
  service_id: string;
  enabled: boolean;
  min_instances: number;
  max_instances: number;
  cpu_target_percent: number;
  memory_target_percent: number;
  scale_out_cooldown_sec: number;
  scale_in_cooldown_sec: number;
  last_scaled_at?: string | null;
  created_at?: string;
  updated_at?: string;
}

export type DomainStatus = 'pending' | 'active' | 'expired' | 'cancelled' | 'transferring';
export type DnsRecordType = 'A' | 'AAAA' | 'CNAME' | 'MX' | 'TXT' | 'NS' | 'SRV' | 'CAA';

export interface RegisteredDomain {
  id: string;
  user_id: string;
  workspace_id: string;
  domain_name: string;
  tld: string;
  provider: string;
  provider_domain_id: string;
  status: DomainStatus;
  expires_at: string | null;
  auto_renew: boolean;
  whois_privacy: boolean;
  locked: boolean;
  cost_cents: number;
  sell_cents: number;
  created_at: string;
  updated_at: string;
}

export interface DnsRecord {
  id: string;
  domain_id: string;
  record_type: DnsRecordType;
  name: string;
  value: string;
  ttl: number;
  priority: number;
  managed: boolean;
  provider_record_id: string;
  created_at: string;
  updated_at: string;
}

export interface DomainSearchResult {
  domain: string;
  available: boolean;
  price_cents: number;
  currency: string;
}

// Ops / Incidents (Alertmanager)
export interface Incident {
  id: string;
  status: string;
  receiver: string;
  alertname: string;
  severity: string;
  namespace: string;
  summary: string;
  description: string;
  runbook_url: string;
  alerts_count: number;
  latest_event_id: string;
  latest_received_at: string;
  first_seen_at: string;
  last_seen_at: string;
  event_count: number;

  acknowledged_at?: string | null;
  acknowledged_by?: string | null;
  ack_note?: string | null;
  silence_id?: string | null;
  silenced_until?: string | null;
  silenced_by?: string | null;
}

export interface IncidentEvent {
  id: string;
  received_at: string;
  status: string;
  receiver: string;
  alerts_count: number;
}

export interface IncidentDetail extends Incident {
  latest_payload: unknown;
  events: IncidentEvent[];
}

// Ops / Dashboard
export interface OpsOverview {
  users_total: number;
  workspaces_total: number;
  services_total: number;

  deploys_pending: number;
  deploys_building: number;
  deploys_deploying: number;
  deploys_failed_24h: number;

  email_pending: number;
  email_retry: number;
  email_dead: number;
}

export interface OpsUserItem {
  id: string;
  username: string;
  email: string;
  role: string;
  created_at: string;
}

export interface OpsWorkspaceItem {
  id: string;
  name: string;
  owner_id: string;
  owner_email: string;
  created_at: string;
}

export interface OpsServiceItem {
  id: string;
  workspace_id: string;
  workspace_name: string;
  name: string;
  subdomain: string;
  type: string;
  runtime: string;
  status: string;
  repo_url: string;
  branch: string;
  created_at: string;
  updated_at: string;
}

export interface OpsDeployItem {
  id: string;
  service_id: string;
  service_name: string;
  status: string;
  trigger: string;
  branch: string;
  commit_sha: string;
  commit_message: string;
  created_at: string;
  started_at?: string | null;
  finished_at?: string | null;
  last_error?: string | null;
}

export interface OpsEmailOutboxItem {
  id: string;
  message_type: string;
  to_email: string;
  subject: string;
  status: string;
  attempts: number;
  last_error?: string | null;
  next_attempt_at: string;
  created_at: string;
  sent_at?: string | null;
}

// Support (customer-facing)
export interface SupportTicket {
  id: string;
  workspace_id: string;
  created_by: string;
  subject: string;
  status: string;
  priority: string;
  assigned_to: string;
  last_customer_reply_at?: string | null;
  last_ops_reply_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface SupportTicketMessage {
  id: string;
  ticket_id: string;
  author_id: string;
  body: string;
  is_internal: boolean;
  created_at: string;
}

// Ops: Billing
export interface OpsBillingCustomerItem {
  id: string;
  user_id: string;
  email: string;
  username: string;
  stripe_customer_id: string;
  stripe_subscription_id: string;
  subscription_status: string;
  payment_method_brand: string;
  payment_method_last4: string;
  items_count: number;
  created_at: string;
  updated_at: string;
}

export interface OpsBillingItem {
  id: string;
  resource_type: string;
  resource_id: string;
  resource_name: string;
  plan: string;
  stripe_price_id: string;
  stripe_subscription_item_id: string;
  created_at: string;
  updated_at: string;
}

export interface OpsBillingCustomerDetail {
  customer: OpsBillingCustomerItem;
  items: OpsBillingItem[];
}

// Ops: Tickets
export interface OpsTicketItem extends SupportTicket {
  workspace_name: string;
  created_by_email: string;
  created_by_username: string;
}

export interface OpsTicketDetail {
  ticket: OpsTicketItem;
  messages: SupportTicketMessage[];
}

// Ops: Credits
export interface OpsWorkspaceCreditItem {
  workspace_id: string;
  workspace_name: string;
  owner_email: string;
  balance_cents: number;
  created_at: string;
}

export interface WorkspaceCreditLedgerEntry {
  id: string;
  workspace_id: string;
  amount_cents: number;
  reason: string;
  created_by: string;
  created_at: string;
}

export interface OpsWorkspaceCreditDetail {
  workspace_id: string;
  workspace_name: string;
  balance_cents: number;
  ledger: WorkspaceCreditLedgerEntry[];
}

// Ops: Technical / Kube
export interface OpsKubeDeploymentSummary {
  name: string;
  desired_replicas: number;
  updated_replicas: number;
  ready_replicas: number;
  available_replicas: number;
  age_seconds: number;
}

export interface OpsKubePodSummary {
  name: string;
  phase: string;
  ready: boolean;
  restarts: number;
  node_name: string;
  age_seconds: number;
}

export interface OpsKubeSummary {
  enabled: boolean;
  namespace: string;
  deployments?: OpsKubeDeploymentSummary[];
  pods?: OpsKubePodSummary[];
  error?: string;
}

// Ops: Performance
export interface OpsPerformanceSummary {
  window_hours: number;
  deploys: {
    total: number;
    pending: number;
    building: number;
    deploying: number;
    live: number;
    failed: number;
  };
  queue_wait_seconds: {
    avg?: number;
    p50?: number;
    p95?: number;
  };
  deploy_duration_seconds: {
    avg?: number;
    p50?: number;
    p95?: number;
  };
  top_failures: Array<{ service_id: string; service_name: string; failures: number }>;
}
