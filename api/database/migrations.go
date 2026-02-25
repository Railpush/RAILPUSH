package database

import (
	"context"
	"fmt"
	"log"
	"time"
)

func RunMigrations() error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}

	// Serialize migrations across all control-plane/worker replicas.
	// A dedicated connection is required so the advisory lock is held for the full run.
	const migrationAdvisoryLockKey int64 = 724011201
	conn, err := DB.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("migration conn: %w", err)
	}
	defer conn.Close()

	log.Printf("Acquiring migration advisory lock (%d)...", migrationAdvisoryLockKey)
	if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_lock($1)", migrationAdvisoryLockKey); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrationAdvisoryLockKey); err != nil {
			log.Printf("WARNING: release migration advisory lock failed: %v", err)
		}
	}()

	for i, m := range migrationSQL {
		if _, err := conn.ExecContext(context.Background(), m); err != nil {
			log.Printf("Migration %d failed: %v", i, err)
			return err
		}
	}
	log.Println("Database migrations completed")
	return nil
}

var migrationSQL = []string{
	`CREATE EXTENSION IF NOT EXISTS "pgcrypto"`,
	`CREATE TABLE IF NOT EXISTS users (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), github_id BIGINT UNIQUE, username VARCHAR(255), email VARCHAR(255) UNIQUE, password_hash VARCHAR(255), avatar_url TEXT, role VARCHAR(50) DEFAULT 'member', created_at TIMESTAMPTZ DEFAULT NOW())`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash VARCHAR(255)`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS is_suspended BOOLEAN DEFAULT FALSE`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ`,
	// Backfill existing accounts once (guarded by a cutoff so new signups remain unverified).
	`UPDATE users
	    SET email_verified_at = COALESCE(email_verified_at, created_at)
	  WHERE email_verified_at IS NULL
	    AND created_at < '2026-02-15T05:00:00Z'::timestamptz`,
	`CREATE TABLE IF NOT EXISTS email_verification_tokens (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash TEXT NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		used_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_email_verification_tokens_hash_unique ON email_verification_tokens(token_hash)`,
	`CREATE INDEX IF NOT EXISTS idx_email_verification_tokens_user_created_at ON email_verification_tokens(user_id, created_at DESC)`,
	`CREATE TABLE IF NOT EXISTS api_keys (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), user_id UUID REFERENCES users(id) ON DELETE CASCADE, name VARCHAR(255), key_hash VARCHAR(255), scopes TEXT[], expires_at TIMESTAMPTZ, created_at TIMESTAMPTZ DEFAULT NOW())`,
	`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS scopes TEXT[]`,
	`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS allowed_cidrs TEXT[]`,
	`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ`,
	`UPDATE api_keys SET scopes='{"*"}'::text[] WHERE scopes IS NULL`,
	`CREATE TABLE IF NOT EXISTS workspaces (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), name VARCHAR(255), owner_id UUID REFERENCES users(id) ON DELETE CASCADE, deploy_policy VARCHAR(50) DEFAULT 'cancel', created_at TIMESTAMPTZ DEFAULT NOW())`,
	`ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS is_suspended BOOLEAN DEFAULT FALSE`,
	`ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ`,
	`CREATE TABLE IF NOT EXISTS workspace_members (workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE, user_id UUID REFERENCES users(id) ON DELETE CASCADE, role VARCHAR(50), PRIMARY KEY (workspace_id, user_id))`,
	`CREATE TABLE IF NOT EXISTS project_folders (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE, name VARCHAR(255), created_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE TABLE IF NOT EXISTS projects (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE, name VARCHAR(255), created_at TIMESTAMPTZ DEFAULT NOW())`,
	`ALTER TABLE projects ADD COLUMN IF NOT EXISTS folder_id UUID`,
	`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='projects_folder_id_fkey') THEN ALTER TABLE projects ADD CONSTRAINT projects_folder_id_fkey FOREIGN KEY (folder_id) REFERENCES project_folders(id) ON DELETE SET NULL; END IF; END $$`,
	`CREATE INDEX IF NOT EXISTS idx_project_folders_workspace_id ON project_folders(workspace_id)`,
	`CREATE INDEX IF NOT EXISTS idx_projects_folder_id ON projects(folder_id)`,
	`CREATE TABLE IF NOT EXISTS environments (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), project_id UUID REFERENCES projects(id) ON DELETE CASCADE, name VARCHAR(255), is_protected BOOLEAN DEFAULT FALSE, created_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE TABLE IF NOT EXISTS services (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE, project_id UUID, environment_id UUID, name VARCHAR(255), type VARCHAR(50), runtime VARCHAR(50), repo_url TEXT, branch VARCHAR(255) DEFAULT 'main', build_command TEXT, start_command TEXT, dockerfile_path VARCHAR(255), docker_context VARCHAR(255), image_url TEXT, health_check_path VARCHAR(255), port INT DEFAULT 10000, auto_deploy BOOLEAN DEFAULT TRUE, is_suspended BOOLEAN DEFAULT FALSE, max_shutdown_delay INT DEFAULT 30, pre_deploy_command TEXT, static_publish_path VARCHAR(255), schedule VARCHAR(255), plan VARCHAR(50) DEFAULT 'starter', instances INT DEFAULT 1, status VARCHAR(50) DEFAULT 'created', container_id VARCHAR(255), host_port INT, created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE TABLE IF NOT EXISTS env_vars (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), owner_type VARCHAR(50), owner_id UUID, key VARCHAR(255), encrypted_value TEXT, is_secret BOOLEAN DEFAULT FALSE, created_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE TABLE IF NOT EXISTS env_groups (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE, name VARCHAR(255), created_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE TABLE IF NOT EXISTS env_group_memberships (service_id UUID REFERENCES services(id) ON DELETE CASCADE, env_group_id UUID REFERENCES env_groups(id) ON DELETE CASCADE, PRIMARY KEY (service_id, env_group_id))`,
	`CREATE TABLE IF NOT EXISTS disks (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), service_id UUID REFERENCES services(id) ON DELETE CASCADE, name VARCHAR(255), mount_path VARCHAR(255), size_gb INT, created_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE TABLE IF NOT EXISTS custom_domains (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), service_id UUID REFERENCES services(id) ON DELETE CASCADE, domain VARCHAR(255), verified BOOLEAN DEFAULT FALSE, tls_provisioned BOOLEAN DEFAULT FALSE, created_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE TABLE IF NOT EXISTS deploys (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), service_id UUID REFERENCES services(id) ON DELETE CASCADE, trigger VARCHAR(50), status VARCHAR(50) DEFAULT 'pending', commit_sha VARCHAR(255), commit_message TEXT, branch VARCHAR(255), image_tag VARCHAR(255), build_log TEXT, started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ, created_by UUID)`,
	`CREATE TABLE IF NOT EXISTS managed_databases (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE, name VARCHAR(255), plan VARCHAR(50), pg_version INT DEFAULT 16, container_id VARCHAR(255), host VARCHAR(255), port INT DEFAULT 5432, db_name VARCHAR(255), username VARCHAR(255), encrypted_password TEXT, status VARCHAR(50) DEFAULT 'creating', created_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE TABLE IF NOT EXISTS managed_keyvalue (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE, name VARCHAR(255), plan VARCHAR(50), container_id VARCHAR(255), host VARCHAR(255), port INT DEFAULT 6379, encrypted_password TEXT, maxmemory_policy VARCHAR(50) DEFAULT 'allkeys-lru', status VARCHAR(50) DEFAULT 'creating', created_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE TABLE IF NOT EXISTS blueprints (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE, name VARCHAR(255), repo_url TEXT, branch VARCHAR(255), file_path VARCHAR(255) DEFAULT 'railpush.yaml', last_synced_at TIMESTAMPTZ, last_sync_status VARCHAR(50), created_at TIMESTAMPTZ DEFAULT NOW())`,
	`ALTER TABLE blueprints ADD COLUMN IF NOT EXISTS ai_ignore_repo_yaml BOOLEAN DEFAULT FALSE`,
	`CREATE TABLE IF NOT EXISTS blueprint_resources (blueprint_id UUID REFERENCES blueprints(id) ON DELETE CASCADE, resource_type VARCHAR(50), resource_id UUID, resource_name VARCHAR(255))`,
	`CREATE TABLE IF NOT EXISTS backups (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), resource_type VARCHAR(50), resource_id UUID, file_path TEXT, size_bytes BIGINT, started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ, status VARCHAR(50))`,
	`CREATE TABLE IF NOT EXISTS audit_log (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id UUID, user_id UUID, action VARCHAR(255), resource_type VARCHAR(50), resource_id UUID, details_json JSONB, created_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE TABLE IF NOT EXISTS billing_customers (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), user_id UUID UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE, stripe_customer_id VARCHAR(255) UNIQUE NOT NULL, stripe_subscription_id VARCHAR(255), payment_method_last4 VARCHAR(4), payment_method_brand VARCHAR(50), subscription_status VARCHAR(50) DEFAULT 'incomplete', created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW())`,
	`ALTER TABLE billing_customers ADD COLUMN IF NOT EXISTS credits_migrated BOOLEAN NOT NULL DEFAULT FALSE`,
	`ALTER TABLE billing_customers ADD COLUMN IF NOT EXISTS last_billing_sync_at TIMESTAMPTZ`,
	`CREATE TABLE IF NOT EXISTS billing_items (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), billing_customer_id UUID NOT NULL REFERENCES billing_customers(id) ON DELETE CASCADE, stripe_subscription_item_id VARCHAR(255) NOT NULL, stripe_price_id VARCHAR(255) NOT NULL, resource_type VARCHAR(50) NOT NULL, resource_id UUID NOT NULL, resource_name VARCHAR(255), plan VARCHAR(50) NOT NULL, created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW())`,
	// One Stripe subscription item represents a plan price, and quantity represents how many resources are on that plan.
	// Older schema incorrectly enforced uniqueness per stripe_subscription_item_id, which breaks multi-resource billing.
	`ALTER TABLE billing_items DROP CONSTRAINT IF EXISTS billing_items_stripe_subscription_item_id_key`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_billing_items_resource_unique ON billing_items(resource_type, resource_id)`,
	`CREATE INDEX IF NOT EXISTS idx_billing_items_sub_item ON billing_items(stripe_subscription_item_id)`,
	`CREATE INDEX IF NOT EXISTS idx_billing_items_customer_price ON billing_items(billing_customer_id, stripe_price_id)`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS github_access_token TEXT`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS blueprint_ai_autogen_enabled BOOLEAN DEFAULT FALSE`,
	`ALTER TABLE blueprints ADD COLUMN IF NOT EXISTS generated_yaml TEXT`,
	`CREATE TABLE IF NOT EXISTS registered_domains (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE, workspace_id UUID REFERENCES workspaces(id) ON DELETE SET NULL, domain_name VARCHAR(255) UNIQUE NOT NULL, tld VARCHAR(63) NOT NULL, provider VARCHAR(50) DEFAULT 'mock', provider_domain_id VARCHAR(255), status VARCHAR(50) DEFAULT 'pending', expires_at TIMESTAMPTZ, auto_renew BOOLEAN DEFAULT TRUE, whois_privacy BOOLEAN DEFAULT TRUE, locked BOOLEAN DEFAULT FALSE, cost_cents INT DEFAULT 0, sell_cents INT DEFAULT 0, created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE TABLE IF NOT EXISTS dns_records (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), domain_id UUID NOT NULL REFERENCES registered_domains(id) ON DELETE CASCADE, record_type VARCHAR(10) NOT NULL, name VARCHAR(255) NOT NULL, value TEXT NOT NULL, ttl INT DEFAULT 3600, priority INT DEFAULT 0, managed BOOLEAN DEFAULT FALSE, provider_record_id VARCHAR(255), created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE TABLE IF NOT EXISTS domain_transactions (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), domain_id UUID NOT NULL REFERENCES registered_domains(id) ON DELETE CASCADE, user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE, type VARCHAR(50) NOT NULL, amount_cents INT DEFAULT 0, status VARCHAR(50) DEFAULT 'completed', details JSONB, created_at TIMESTAMPTZ DEFAULT NOW())`,
	`ALTER TABLE blueprints ALTER COLUMN last_sync_status TYPE TEXT`,
	`CREATE TABLE IF NOT EXISTS preview_environments (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE, service_id UUID REFERENCES services(id) ON DELETE SET NULL, repository VARCHAR(512) NOT NULL, pr_number INT NOT NULL, pr_title TEXT, pr_branch VARCHAR(255), base_branch VARCHAR(255), commit_sha VARCHAR(255), status VARCHAR(50) DEFAULT 'provisioning', created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW(), closed_at TIMESTAMPTZ)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_preview_env_workspace_repo_pr ON preview_environments(workspace_id, repository, pr_number)`,
	`CREATE TABLE IF NOT EXISTS one_off_jobs (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE, service_id UUID REFERENCES services(id) ON DELETE SET NULL, name VARCHAR(255) NOT NULL, command TEXT NOT NULL, status VARCHAR(50) DEFAULT 'pending', exit_code INT, logs TEXT DEFAULT '', created_by UUID REFERENCES users(id) ON DELETE SET NULL, started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ, created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE INDEX IF NOT EXISTS idx_one_off_jobs_service_created ON one_off_jobs(service_id, created_at DESC)`,
	`CREATE TABLE IF NOT EXISTS service_autoscaling_policies (service_id UUID PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE, enabled BOOLEAN DEFAULT FALSE, min_instances INT DEFAULT 1, max_instances INT DEFAULT 1, cpu_target_percent INT DEFAULT 70, memory_target_percent INT DEFAULT 75, scale_out_cooldown_sec INT DEFAULT 120, scale_in_cooldown_sec INT DEFAULT 180, last_scaled_at TIMESTAMPTZ, created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE TABLE IF NOT EXISTS service_instances (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE, container_id VARCHAR(255) NOT NULL, host_port INT NOT NULL, role VARCHAR(32) DEFAULT 'replica', status VARCHAR(50) DEFAULT 'live', created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE INDEX IF NOT EXISTS idx_service_instances_service_id ON service_instances(service_id)`,
	`CREATE TABLE IF NOT EXISTS managed_database_replicas (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), primary_database_id UUID NOT NULL REFERENCES managed_databases(id) ON DELETE CASCADE, workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE, name VARCHAR(255) NOT NULL, region VARCHAR(128) DEFAULT 'same-node', container_id VARCHAR(255), host VARCHAR(255), port INT, status VARCHAR(50) DEFAULT 'creating', replication_mode VARCHAR(32) DEFAULT 'async', lag_seconds INT DEFAULT 0, promoted BOOLEAN DEFAULT FALSE, created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW())`,
	`CREATE INDEX IF NOT EXISTS idx_db_replicas_primary ON managed_database_replicas(primary_database_id)`,
	`ALTER TABLE managed_databases ADD COLUMN IF NOT EXISTS ha_enabled BOOLEAN DEFAULT FALSE`,
	`ALTER TABLE managed_databases ADD COLUMN IF NOT EXISTS ha_strategy VARCHAR(32) DEFAULT 'none'`,
	`ALTER TABLE managed_databases ADD COLUMN IF NOT EXISTS standby_replica_id UUID`,
	`ALTER TABLE services ADD COLUMN IF NOT EXISTS project_id UUID`,
	`ALTER TABLE services ADD COLUMN IF NOT EXISTS environment_id UUID`,
	`CREATE TABLE IF NOT EXISTS saml_sso_configs (workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE, enabled BOOLEAN DEFAULT FALSE, entity_id VARCHAR(512) NOT NULL, acs_url VARCHAR(1024) NOT NULL, metadata_url VARCHAR(1024), idp_sso_url VARCHAR(1024), idp_cert_pem TEXT, allowed_domains TEXT[] DEFAULT '{}', created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW())`,

	// Durable deploy queue leasing (enables horizontal worker scaling + crash recovery).
	`ALTER TABLE deploys ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT NOW()`,
	`ALTER TABLE deploys ADD COLUMN IF NOT EXISTS lease_owner TEXT`,
	`ALTER TABLE deploys ADD COLUMN IF NOT EXISTS lease_acquired_at TIMESTAMPTZ`,
	`ALTER TABLE deploys ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ`,
	`ALTER TABLE deploys ADD COLUMN IF NOT EXISTS attempts INT DEFAULT 0`,
	`ALTER TABLE deploys ADD COLUMN IF NOT EXISTS last_error TEXT`,
	`CREATE INDEX IF NOT EXISTS idx_deploys_queue ON deploys(status, lease_expires_at, created_at)`,

	// Custom domains must be globally unique to prevent host routing conflicts.
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_custom_domains_domain_unique ON custom_domains ((lower(domain)))`,

	// GitHub OAuth may not provide a public email; store empty emails as NULL to avoid uniqueness conflicts.
	`UPDATE users SET email=NULL WHERE email=''`,

	// Webhook lookup performance: repo+branch targeting (avoid full-table scans).
	`CREATE INDEX IF NOT EXISTS idx_services_repo_branch ON services(repo_url, branch)`,
	`CREATE INDEX IF NOT EXISTS idx_services_repo_branch_autodeploy ON services(repo_url, branch) WHERE auto_deploy = true`,

	// Alertmanager webhook sink (internal alert delivery).
	`CREATE TABLE IF NOT EXISTS alert_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		received_at TIMESTAMPTZ DEFAULT NOW(),
		status TEXT,
		receiver TEXT,
		group_key TEXT,
		alertname TEXT,
		severity TEXT,
		namespace TEXT,
		payload JSONB NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_alert_events_received_at ON alert_events(received_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_alert_events_alertname_received_at ON alert_events(alertname, received_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_alert_events_group_key_received_at ON alert_events(group_key, received_at DESC)`,

	// Ops incidents: acknowledgements + silences (created via the RailPush UI).
	`CREATE TABLE IF NOT EXISTS incident_acknowledgements (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		group_key TEXT NOT NULL,
		acknowledged_by UUID REFERENCES users(id) ON DELETE SET NULL,
		note TEXT,
		created_at TIMESTAMPTZ DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_incident_acks_group_key_created_at ON incident_acknowledgements(group_key, created_at DESC)`,

	`CREATE TABLE IF NOT EXISTS incident_silences (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		group_key TEXT NOT NULL,
		silence_id TEXT NOT NULL,
		scope TEXT NOT NULL DEFAULT 'group',
		created_by UUID REFERENCES users(id) ON DELETE SET NULL,
		comment TEXT,
		matchers JSONB NOT NULL,
		starts_at TIMESTAMPTZ NOT NULL,
		ends_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_incident_silences_group_key_ends_at ON incident_silences(group_key, ends_at DESC)`,

	// Deterministic "subdomain label" function for uniqueness + routing. Must match utils.ServiceDomainLabel.
	`CREATE OR REPLACE FUNCTION railpush_service_domain_label(input text) RETURNS text AS $$
DECLARE
	label text;
BEGIN
	IF input IS NULL THEN
		input := '';
	END IF;
	label := lower(btrim(input));
	label := replace(label, '_', '-');
	label := replace(label, '.', '-');
	label := regexp_replace(label, '[^a-z0-9-]+', '-', 'g');
	label := regexp_replace(label, '-+', '-', 'g');
	label := regexp_replace(label, '(^-+|-+$)', '', 'g');
	IF label = '' THEN
		label := 'service';
	END IF;
	IF length(label) > 63 THEN
		label := left(label, 63);
		label := regexp_replace(label, '(^-+|-+$)', '', 'g');
		IF label = '' THEN
			label := 'service';
		END IF;
	END IF;
	RETURN label;
END;
$$ LANGUAGE plpgsql IMMUTABLE`,

	// Stable service subdomain label, separate from display name.
	`ALTER TABLE services ADD COLUMN IF NOT EXISTS subdomain VARCHAR(63)`,
	`UPDATE services SET subdomain = railpush_service_domain_label(name) WHERE (subdomain IS NULL OR subdomain='') AND COALESCE(name,'') <> ''`,
	// If duplicates exist, deterministically disambiguate by suffixing a short ID fragment.
	`WITH ranked AS (
		SELECT id, subdomain,
		       row_number() OVER (PARTITION BY lower(subdomain) ORDER BY created_at ASC, id ASC) AS rn
		FROM services
		WHERE COALESCE(subdomain,'') <> ''
	)
	UPDATE services s
	SET subdomain = railpush_service_domain_label(r.subdomain || '-' || left(replace(s.id::text, '-', ''), 8))
	FROM ranked r
	WHERE s.id = r.id AND r.rn > 1`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_services_subdomain_unique ON services ((lower(subdomain))) WHERE COALESCE(subdomain,'') <> ''`,

	// Stripe webhook idempotency + debugging (avoid duplicate processing on retries).
	`CREATE TABLE IF NOT EXISTS stripe_webhook_events (
		event_id TEXT PRIMARY KEY,
		event_type TEXT,
		livemode BOOLEAN,
		api_version TEXT,
		received_at TIMESTAMPTZ DEFAULT NOW(),
		processed_at TIMESTAMPTZ DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_stripe_webhook_events_received_at ON stripe_webhook_events(received_at DESC)`,

	// Billing safety: prevent double-charging by enforcing 1 billing item per resource.
	`DELETE FROM billing_items b USING (
		SELECT id,
		       row_number() OVER (PARTITION BY resource_type, resource_id ORDER BY created_at DESC, id DESC) AS rn
		FROM billing_items
	) d
	WHERE b.id = d.id AND d.rn > 1`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_billing_items_resource_unique ON billing_items(resource_type, resource_id)`,

	// Transactional email outbox (reliable, async delivery).
	`CREATE TABLE IF NOT EXISTS email_outbox (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		dedupe_key TEXT,
		message_type TEXT NOT NULL,
		to_email TEXT NOT NULL,
		subject TEXT NOT NULL,
		body_text TEXT NOT NULL,
		body_html TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		attempts INT NOT NULL DEFAULT 0,
		last_error TEXT,
		next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		lease_owner TEXT,
		lease_acquired_at TIMESTAMPTZ,
		lease_expires_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		sent_at TIMESTAMPTZ
	)`,
	`CREATE INDEX IF NOT EXISTS idx_email_outbox_queue ON email_outbox(status, next_attempt_at, lease_expires_at)`,
	`CREATE INDEX IF NOT EXISTS idx_email_outbox_created_at ON email_outbox(created_at DESC)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_email_outbox_dedupe ON email_outbox(dedupe_key)
		WHERE dedupe_key IS NOT NULL`,

	// Backfill: some services created before the auto-deploy default was enforced in handlers
	// ended up with auto_deploy=false. Flip them once, without overriding future user changes.
	`UPDATE services
	    SET auto_deploy=true
	  WHERE auto_deploy IS DISTINCT FROM true
	    AND updated_at < '2026-02-15T05:00:00Z'::timestamptz`,

	// Support tickets (customer support surface + ops triage).
	`CREATE TABLE IF NOT EXISTS support_tickets (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		workspace_id UUID REFERENCES workspaces(id) ON DELETE SET NULL,
		created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		subject TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'open',
		priority TEXT NOT NULL DEFAULT 'normal',
		assigned_to UUID REFERENCES users(id) ON DELETE SET NULL,
		last_customer_reply_at TIMESTAMPTZ,
		last_ops_reply_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_support_tickets_created_at ON support_tickets(created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_support_tickets_status_updated_at ON support_tickets(status, updated_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_support_tickets_workspace ON support_tickets(workspace_id, created_at DESC)`,

	`CREATE TABLE IF NOT EXISTS support_ticket_messages (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		ticket_id UUID NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
		author_id UUID REFERENCES users(id) ON DELETE SET NULL,
		body TEXT NOT NULL,
		is_internal BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_support_ticket_messages_ticket_created_at ON support_ticket_messages(ticket_id, created_at ASC)`,

	// Workspace credits (ops-granted credits/adjustments; balance is SUM(amount_cents)).
	`CREATE TABLE IF NOT EXISTS workspace_credit_ledger (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
		amount_cents INT NOT NULL,
		reason TEXT,
		created_by UUID REFERENCES users(id) ON DELETE SET NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_workspace_credit_ledger_workspace_created_at ON workspace_credit_ledger(workspace_id, created_at DESC)`,

	// AI fix sessions — track AI-assisted Dockerfile repair attempts.
	`CREATE TABLE IF NOT EXISTS ai_fix_sessions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		service_id UUID NOT NULL,
		status TEXT NOT NULL DEFAULT 'running',
		max_attempts INT NOT NULL DEFAULT 3,
		current_attempt INT NOT NULL DEFAULT 0,
		last_deploy_id TEXT,
		last_ai_summary TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_ai_fix_sessions_service_status ON ai_fix_sessions(service_id, status)`,
	`ALTER TABLE deploys ADD COLUMN IF NOT EXISTS dockerfile_override TEXT NOT NULL DEFAULT ''`,

	// Invoice history (populated from Stripe webhooks for local reconciliation).
	`CREATE TABLE IF NOT EXISTS billing_invoices (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		billing_customer_id UUID NOT NULL REFERENCES billing_customers(id) ON DELETE CASCADE,
		stripe_invoice_id TEXT UNIQUE NOT NULL,
		status TEXT NOT NULL DEFAULT 'paid',
		amount_due_cents INT NOT NULL DEFAULT 0,
		amount_paid_cents INT NOT NULL DEFAULT 0,
		currency TEXT NOT NULL DEFAULT 'usd',
		hosted_invoice_url TEXT,
		invoice_pdf_url TEXT,
		period_start TIMESTAMPTZ,
		period_end TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_billing_invoices_customer_created ON billing_invoices(billing_customer_id, created_at DESC)`,

	// Usage tracking for per-minute billing. Records start/stop events for each resource.
	// A resource is "active" when its latest event is "start". The hourly reporter
	// calculates active minutes between last_reported_at and now, reports them to Stripe,
	// and updates last_reported_at.
	`CREATE TABLE IF NOT EXISTS resource_usage_events (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		resource_type VARCHAR(50) NOT NULL,
		resource_id UUID NOT NULL,
		event VARCHAR(20) NOT NULL,
		occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_resource_usage_events_resource ON resource_usage_events(resource_type, resource_id, occurred_at DESC)`,

	// Tracks per-resource metered billing state. Each resource with a metered subscription
	// item gets a row here so the hourly reporter knows where to send usage records.
	`ALTER TABLE billing_items ADD COLUMN IF NOT EXISTS is_metered BOOLEAN NOT NULL DEFAULT FALSE`,
	`ALTER TABLE billing_items ADD COLUMN IF NOT EXISTS last_usage_reported_at TIMESTAMPTZ`,

	// Sync log: stores detailed step-by-step sync output so users can see why sync failed.
	`ALTER TABLE blueprints ADD COLUMN IF NOT EXISTS sync_log TEXT`,

	// Docker-in-Docker sidecar support: services that need Docker daemon access (e.g., pentagi).
	`ALTER TABLE services ADD COLUMN IF NOT EXISTS docker_access BOOLEAN DEFAULT FALSE`,

	// Per-service build context control: include/exclude file lists for generating .dockerignore.
	`ALTER TABLE services ADD COLUMN IF NOT EXISTS base_image TEXT DEFAULT ''`,
	`ALTER TABLE services ADD COLUMN IF NOT EXISTS build_include TEXT DEFAULT ''`,
	`ALTER TABLE services ADD COLUMN IF NOT EXISTS build_exclude TEXT DEFAULT ''`,

	// Database init scripts: run SQL once when a managed database is first provisioned.
	`ALTER TABLE managed_databases ADD COLUMN IF NOT EXISTS init_script TEXT DEFAULT ''`,

	// External access: unique TCP port per database proxied by nginx ingress controller.
	`ALTER TABLE managed_databases ADD COLUMN IF NOT EXISTS external_port INT`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_databases_external_port ON managed_databases(external_port) WHERE external_port IS NOT NULL`,
	`ALTER TABLE managed_databases ADD COLUMN IF NOT EXISTS password_rotated_at TIMESTAMPTZ`,
	`UPDATE managed_databases SET password_rotated_at = COALESCE(password_rotated_at, created_at, NOW()) WHERE password_rotated_at IS NULL`,

	// Support ticket category (support, feature_request, bug_report).
	`ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'support'`,
	`CREATE INDEX IF NOT EXISTS idx_support_tickets_category ON support_tickets(category)`,
	// Expanded support ticket triage metadata: canonical categories, component, and tags.
	`UPDATE support_tickets SET category='bug' WHERE category IN ('bug_report', 'bug-report')`,
	`ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS component TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}'`,
	`CREATE INDEX IF NOT EXISTS idx_support_tickets_component ON support_tickets(component)`,
	`CREATE INDEX IF NOT EXISTS idx_support_tickets_tags_gin ON support_tickets USING GIN(tags)`,

	// Nested project folders: a folder can contain sub-folders (self-referencing FK).
	`ALTER TABLE project_folders ADD COLUMN IF NOT EXISTS parent_id UUID`,
	`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='project_folders_parent_id_fkey') THEN ALTER TABLE project_folders ADD CONSTRAINT project_folders_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES project_folders(id) ON DELETE CASCADE; END IF; END $$`,
	`CREATE INDEX IF NOT EXISTS idx_project_folders_parent_id ON project_folders(parent_id)`,

	// Blueprints can be organized into project folders too.
	`ALTER TABLE blueprints ADD COLUMN IF NOT EXISTS folder_id UUID`,
	`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='blueprints_folder_id_fkey') THEN ALTER TABLE blueprints ADD CONSTRAINT blueprints_folder_id_fkey FOREIGN KEY (folder_id) REFERENCES project_folders(id) ON DELETE SET NULL; END IF; END $$`,
	`CREATE INDEX IF NOT EXISTS idx_blueprints_folder_id ON blueprints(folder_id)`,

	// Env vars: unique per owner+key so we can use ON CONFLICT for additive upsert.
	// Deduplicate any pre-existing duplicates first (keep the newest row).
	`DELETE FROM env_vars WHERE id NOT IN (SELECT DISTINCT ON (owner_type, owner_id, key) id FROM env_vars ORDER BY owner_type, owner_id, key, created_at DESC)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_env_vars_owner_key ON env_vars(owner_type, owner_id, key)`,

	// Custom domain redirects: e.g. apex domain redirects to www subdomain.
	`ALTER TABLE custom_domains ADD COLUMN IF NOT EXISTS redirect_target VARCHAR(255) DEFAULT ''`,

	// Rewrite/proxy rules: route path prefixes from one service to another.
	// e.g. /api/* on a static frontend proxied to a backend service.
	`CREATE TABLE IF NOT EXISTS rewrite_rules (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
		source_path VARCHAR(255) NOT NULL,
		dest_service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
		dest_path VARCHAR(255) NOT NULL DEFAULT '/',
		rule_type VARCHAR(20) NOT NULL DEFAULT 'proxy',
		priority INT NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_rewrite_rules_service_id ON rewrite_rules(service_id)`,

	// Service <-> managed database links (auto-injected connection URLs).
	`CREATE TABLE IF NOT EXISTS service_database_links (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
		database_id UUID NOT NULL REFERENCES managed_databases(id) ON DELETE CASCADE,
		env_var_name VARCHAR(255) NOT NULL DEFAULT 'DATABASE_URL',
		use_internal_url BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_service_database_links_unique ON service_database_links(service_id, database_id, env_var_name)`,
	`CREATE INDEX IF NOT EXISTS idx_service_database_links_service ON service_database_links(service_id)`,
	`CREATE INDEX IF NOT EXISTS idx_service_database_links_database ON service_database_links(database_id)`,

	// Destructive-operation safety state: confirmation tokens, deletion protection,
	// and soft-delete metadata for high-risk resources.
	`CREATE TABLE IF NOT EXISTS resource_deletion_states (
		resource_type VARCHAR(50) NOT NULL,
		resource_id UUID NOT NULL,
		workspace_id UUID,
		resource_name TEXT NOT NULL DEFAULT '',
		deletion_protection BOOLEAN NOT NULL DEFAULT FALSE,
		deleted_at TIMESTAMPTZ,
		purge_after TIMESTAMPTZ,
		token_hash TEXT NOT NULL DEFAULT '',
		token_expires_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY(resource_type, resource_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_resource_deletion_states_workspace ON resource_deletion_states(workspace_id)`,
	`CREATE INDEX IF NOT EXISTS idx_resource_deletion_states_deleted ON resource_deletion_states(resource_type, deleted_at)`,

	// Data retention policies (workspace-, service-, and database-level).
	`ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS audit_log_retention_days INT NOT NULL DEFAULT 365`,
	`ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS deploy_history_retention_days INT NOT NULL DEFAULT 180`,
	`ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS metric_history_retention_days INT NOT NULL DEFAULT 90`,
	`ALTER TABLE services ADD COLUMN IF NOT EXISTS runtime_log_retention_days INT NOT NULL DEFAULT 30`,
	`ALTER TABLE services ADD COLUMN IF NOT EXISTS build_log_retention_days INT NOT NULL DEFAULT 90`,
	`ALTER TABLE services ADD COLUMN IF NOT EXISTS request_log_retention_days INT NOT NULL DEFAULT 14`,
	`ALTER TABLE managed_databases ADD COLUMN IF NOT EXISTS backup_retention_automated_days INT NOT NULL DEFAULT 30`,
	`ALTER TABLE managed_databases ADD COLUMN IF NOT EXISTS backup_retention_manual_days INT NOT NULL DEFAULT 365`,
	`ALTER TABLE managed_databases ADD COLUMN IF NOT EXISTS wal_retention_days INT NOT NULL DEFAULT 7`,
	`ALTER TABLE backups ADD COLUMN IF NOT EXISTS trigger_type TEXT NOT NULL DEFAULT 'manual'`,
	`UPDATE backups SET trigger_type='manual' WHERE COALESCE(trigger_type,'')=''`,
}
