package models

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/railpush/api/database"
)

var workspaceNotificationDeployEvents = []string{"success", "failed", "rollback"}

var workspaceNotificationAuditEventAliases = map[string]string{
	"all":             "*",
	"delete_*":        "*.deleted",
	"set_env_vars":    "envvars.set",
	"api_key_created": "auth.api_key.created",
	"api_key_deleted": "auth.api_key.deleted",
}

type WorkspaceNotificationChannels struct {
	WorkspaceID       string    `json:"workspace_id"`
	SlackWebhookURL   string    `json:"-"`
	DiscordWebhookURL string    `json:"-"`
	EmailRecipients   []string  `json:"email_recipients"`
	DeployEvents      []string  `json:"deploy_events"`
	AuditEvents       []string  `json:"audit_events"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func DefaultWorkspaceNotificationDeployEvents() []string {
	return append([]string{}, workspaceNotificationDeployEvents...)
}

func DefaultWorkspaceNotificationAuditEvents() []string {
	return []string{}
}

func NormalizeWorkspaceNotificationDeployEvent(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "success", "succeeded", "deploy.success", "deploy.succeeded":
		return "success"
	case "failed", "failure", "deploy.failed":
		return "failed"
	case "rollback", "rolled_back", "deploy.rollback", "deploy.rolled_back":
		return "rollback"
	default:
		return ""
	}
}

func NormalizeWorkspaceNotificationDeployEvents(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		norm := NormalizeWorkspaceNotificationDeployEvent(item)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func NormalizeWorkspaceNotificationAuditEvent(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	v = strings.ReplaceAll(v, " ", "")
	if v == "" {
		return ""
	}
	if alias, ok := workspaceNotificationAuditEventAliases[v]; ok {
		v = alias
	}
	for _, ch := range v {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= '0' && ch <= '9':
		case ch == '.', ch == '_', ch == '*':
		default:
			return ""
		}
	}
	if strings.Count(v, "*") > 1 {
		return ""
	}
	return v
}

func NormalizeWorkspaceNotificationAuditEvents(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		norm := NormalizeWorkspaceNotificationAuditEvent(item)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func IsWorkspaceNotificationAuditEventEnabled(filters []string, action string) bool {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		return false
	}
	for _, raw := range filters {
		filter := NormalizeWorkspaceNotificationAuditEvent(raw)
		if filter == "" {
			continue
		}
		if filter == "*" {
			return true
		}
		if strings.Contains(filter, "*") {
			parts := strings.SplitN(filter, "*", 2)
			prefix := parts[0]
			suffix := parts[1]
			if strings.HasPrefix(action, prefix) && strings.HasSuffix(action, suffix) {
				return true
			}
			continue
		}
		if action == filter {
			return true
		}
	}
	return false
}

func ValidateWorkspaceNotificationAuditEvents(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		norm := NormalizeWorkspaceNotificationAuditEvent(trimmed)
		if norm == "" {
			return nil, fmt.Errorf("unsupported audit event filter: %q", trimmed)
		}
		if seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

func IsWorkspaceNotificationDeployEventEnabled(events []string, event string) bool {
	event = NormalizeWorkspaceNotificationDeployEvent(event)
	if event == "" {
		return false
	}
	for _, item := range events {
		if NormalizeWorkspaceNotificationDeployEvent(item) == event {
			return true
		}
	}
	return false
}

func GetWorkspaceNotificationChannels(workspaceID string) (*WorkspaceNotificationChannels, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, nil
	}

	cfg := &WorkspaceNotificationChannels{}
	err := database.DB.QueryRow(
		`SELECT workspace_id::text,
		        COALESCE(slack_webhook_url, ''),
		        COALESCE(discord_webhook_url, ''),
		        COALESCE(email_recipients, '{}'::text[]),
		        COALESCE(deploy_events, '{}'::text[]),
		        COALESCE(audit_events, '{}'::text[]),
		        created_at,
		        updated_at
		   FROM workspace_notification_channels
		  WHERE workspace_id = $1`,
		workspaceID,
	).Scan(
		&cfg.WorkspaceID,
		&cfg.SlackWebhookURL,
		&cfg.DiscordWebhookURL,
		pq.Array(&cfg.EmailRecipients),
		pq.Array(&cfg.DeployEvents),
		pq.Array(&cfg.AuditEvents),
		&cfg.CreatedAt,
		&cfg.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cfg.DeployEvents = NormalizeWorkspaceNotificationDeployEvents(cfg.DeployEvents)
	cfg.AuditEvents = NormalizeWorkspaceNotificationAuditEvents(cfg.AuditEvents)
	if cfg.EmailRecipients == nil {
		cfg.EmailRecipients = []string{}
	}
	if cfg.DeployEvents == nil {
		cfg.DeployEvents = []string{}
	}
	if cfg.AuditEvents == nil {
		cfg.AuditEvents = []string{}
	}
	return cfg, nil
}

func UpsertWorkspaceNotificationChannels(cfg *WorkspaceNotificationChannels) error {
	if cfg == nil {
		return nil
	}
	cfg.WorkspaceID = strings.TrimSpace(cfg.WorkspaceID)
	cfg.SlackWebhookURL = strings.TrimSpace(cfg.SlackWebhookURL)
	cfg.DiscordWebhookURL = strings.TrimSpace(cfg.DiscordWebhookURL)
	cfg.DeployEvents = NormalizeWorkspaceNotificationDeployEvents(cfg.DeployEvents)
	cfg.AuditEvents = NormalizeWorkspaceNotificationAuditEvents(cfg.AuditEvents)
	if cfg.EmailRecipients == nil {
		cfg.EmailRecipients = []string{}
	}
	if cfg.DeployEvents == nil {
		cfg.DeployEvents = []string{}
	}
	if cfg.AuditEvents == nil {
		cfg.AuditEvents = []string{}
	}

	return database.DB.QueryRow(
		`INSERT INTO workspace_notification_channels (
			workspace_id,
			slack_webhook_url,
			discord_webhook_url,
			email_recipients,
			deploy_events,
			audit_events,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, COALESCE($4, '{}'::text[]), COALESCE($5, '{}'::text[]), COALESCE($6, '{}'::text[]), NOW(), NOW())
		ON CONFLICT (workspace_id)
		DO UPDATE SET
			slack_webhook_url = EXCLUDED.slack_webhook_url,
			discord_webhook_url = EXCLUDED.discord_webhook_url,
			email_recipients = EXCLUDED.email_recipients,
			deploy_events = EXCLUDED.deploy_events,
			audit_events = EXCLUDED.audit_events,
			updated_at = NOW()
		RETURNING created_at, updated_at`,
		cfg.WorkspaceID,
		cfg.SlackWebhookURL,
		cfg.DiscordWebhookURL,
		pq.Array(cfg.EmailRecipients),
		pq.Array(cfg.DeployEvents),
		pq.Array(cfg.AuditEvents),
	).Scan(&cfg.CreatedAt, &cfg.UpdatedAt)
}
