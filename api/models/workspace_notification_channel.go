package models

import (
	"database/sql"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/railpush/api/database"
)

var workspaceNotificationDeployEvents = []string{"success", "failed", "rollback"}

type WorkspaceNotificationChannels struct {
	WorkspaceID       string    `json:"workspace_id"`
	SlackWebhookURL   string    `json:"-"`
	DiscordWebhookURL string    `json:"-"`
	EmailRecipients   []string  `json:"email_recipients"`
	DeployEvents      []string  `json:"deploy_events"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func DefaultWorkspaceNotificationDeployEvents() []string {
	return append([]string{}, workspaceNotificationDeployEvents...)
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
	if cfg.EmailRecipients == nil {
		cfg.EmailRecipients = []string{}
	}
	if cfg.DeployEvents == nil {
		cfg.DeployEvents = []string{}
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
	if cfg.EmailRecipients == nil {
		cfg.EmailRecipients = []string{}
	}
	if cfg.DeployEvents == nil {
		cfg.DeployEvents = []string{}
	}

	return database.DB.QueryRow(
		`INSERT INTO workspace_notification_channels (
			workspace_id,
			slack_webhook_url,
			discord_webhook_url,
			email_recipients,
			deploy_events,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, COALESCE($4, '{}'::text[]), COALESCE($5, '{}'::text[]), NOW(), NOW())
		ON CONFLICT (workspace_id)
		DO UPDATE SET
			slack_webhook_url = EXCLUDED.slack_webhook_url,
			discord_webhook_url = EXCLUDED.discord_webhook_url,
			email_recipients = EXCLUDED.email_recipients,
			deploy_events = EXCLUDED.deploy_events,
			updated_at = NOW()
		RETURNING created_at, updated_at`,
		cfg.WorkspaceID,
		cfg.SlackWebhookURL,
		cfg.DiscordWebhookURL,
		pq.Array(cfg.EmailRecipients),
		pq.Array(cfg.DeployEvents),
	).Scan(&cfg.CreatedAt, &cfg.UpdatedAt)
}
