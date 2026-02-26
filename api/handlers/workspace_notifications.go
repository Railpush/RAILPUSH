package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

const maxWorkspaceNotificationRecipients = 50
const maxWorkspaceNotificationAuditFilters = 100

type workspaceNotificationChannelsResponse struct {
	WorkspaceID              string     `json:"workspace_id"`
	SlackWebhookConfigured   bool       `json:"slack_webhook_configured"`
	DiscordWebhookConfigured bool       `json:"discord_webhook_configured"`
	EmailRecipients          []string   `json:"email_recipients"`
	DeployEvents             []string   `json:"deploy_events"`
	AuditEvents              []string   `json:"audit_events"`
	CreatedAt                *time.Time `json:"created_at,omitempty"`
	UpdatedAt                *time.Time `json:"updated_at,omitempty"`
}

func (h *WorkspaceHandler) GetNotificationChannels(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	ws, err := models.GetWorkspace(workspaceID)
	if err != nil || ws == nil {
		utils.RespondError(w, http.StatusNotFound, "workspace not found")
		return
	}

	cfg, err := models.GetWorkspaceNotificationChannels(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load notification channels")
		return
	}
	if cfg == nil {
		cfg = &models.WorkspaceNotificationChannels{
			WorkspaceID:     workspaceID,
			EmailRecipients: []string{},
			DeployEvents:    models.DefaultWorkspaceNotificationDeployEvents(),
			AuditEvents:     models.DefaultWorkspaceNotificationAuditEvents(),
		}
	}

	utils.RespondJSON(w, http.StatusOK, workspaceNotificationChannelsResponseFromModel(cfg))
}

func (h *WorkspaceHandler) UpdateNotificationChannels(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	ws, err := models.GetWorkspace(workspaceID)
	if err != nil || ws == nil {
		utils.RespondError(w, http.StatusNotFound, "workspace not found")
		return
	}

	var req struct {
		SlackWebhookURL   *string   `json:"slack_webhook_url"`
		DiscordWebhookURL *string   `json:"discord_webhook_url"`
		EmailRecipients   *[]string `json:"email_recipients"`
		DeployEvents      *[]string `json:"deploy_events"`
		AuditEvents       *[]string `json:"audit_events"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SlackWebhookURL == nil && req.DiscordWebhookURL == nil && req.EmailRecipients == nil && req.DeployEvents == nil && req.AuditEvents == nil {
		utils.RespondError(w, http.StatusBadRequest, "at least one notification field must be provided")
		return
	}

	cfg, err := models.GetWorkspaceNotificationChannels(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load notification channels")
		return
	}
	if cfg == nil {
		cfg = &models.WorkspaceNotificationChannels{
			WorkspaceID:     workspaceID,
			EmailRecipients: []string{},
			DeployEvents:    models.DefaultWorkspaceNotificationDeployEvents(),
			AuditEvents:     models.DefaultWorkspaceNotificationAuditEvents(),
		}
	}

	if req.SlackWebhookURL != nil {
		slackURL := strings.TrimSpace(*req.SlackWebhookURL)
		if slackURL != "" {
			if err := validateWorkspaceNotificationWebhookURL(slackURL, "slack"); err != nil {
				utils.RespondError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		cfg.SlackWebhookURL = slackURL
	}
	if req.DiscordWebhookURL != nil {
		discordURL := strings.TrimSpace(*req.DiscordWebhookURL)
		if discordURL != "" {
			if err := validateWorkspaceNotificationWebhookURL(discordURL, "discord"); err != nil {
				utils.RespondError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		cfg.DiscordWebhookURL = discordURL
	}
	if req.EmailRecipients != nil {
		recipients, err := normalizeWorkspaceNotificationRecipients(*req.EmailRecipients)
		if err != nil {
			utils.RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		cfg.EmailRecipients = recipients
	}
	if req.DeployEvents != nil {
		events, err := parseWorkspaceNotificationDeployEvents(*req.DeployEvents)
		if err != nil {
			utils.RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		cfg.DeployEvents = events
	}
	if req.AuditEvents != nil {
		events, err := parseWorkspaceNotificationAuditEvents(*req.AuditEvents)
		if err != nil {
			utils.RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		cfg.AuditEvents = events
	}

	if err := models.UpsertWorkspaceNotificationChannels(cfg); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update notification channels")
		return
	}

	services.Audit(workspaceID, userID, "workspace.notification_channels.updated", "workspace", workspaceID, map[string]interface{}{
		"slack_webhook_configured":   strings.TrimSpace(cfg.SlackWebhookURL) != "",
		"discord_webhook_configured": strings.TrimSpace(cfg.DiscordWebhookURL) != "",
		"email_recipient_count":      len(cfg.EmailRecipients),
		"deploy_events":              cfg.DeployEvents,
		"audit_events":               cfg.AuditEvents,
	})

	utils.RespondJSON(w, http.StatusOK, workspaceNotificationChannelsResponseFromModel(cfg))
}

func workspaceNotificationChannelsResponseFromModel(cfg *models.WorkspaceNotificationChannels) workspaceNotificationChannelsResponse {
	res := workspaceNotificationChannelsResponse{
		WorkspaceID:              strings.TrimSpace(cfg.WorkspaceID),
		SlackWebhookConfigured:   strings.TrimSpace(cfg.SlackWebhookURL) != "",
		DiscordWebhookConfigured: strings.TrimSpace(cfg.DiscordWebhookURL) != "",
		EmailRecipients:          append([]string{}, cfg.EmailRecipients...),
		DeployEvents:             append([]string{}, cfg.DeployEvents...),
		AuditEvents:              append([]string{}, cfg.AuditEvents...),
	}
	if res.EmailRecipients == nil {
		res.EmailRecipients = []string{}
	}
	if res.DeployEvents == nil {
		res.DeployEvents = []string{}
	}
	if res.AuditEvents == nil {
		res.AuditEvents = []string{}
	}
	if !cfg.CreatedAt.IsZero() {
		v := cfg.CreatedAt
		res.CreatedAt = &v
	}
	if !cfg.UpdatedAt.IsZero() {
		v := cfg.UpdatedAt
		res.UpdatedAt = &v
	}
	return res
}

func normalizeWorkspaceNotificationRecipients(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		parts := strings.Split(item, ",")
		for _, p := range parts {
			v := strings.TrimSpace(p)
			if v == "" {
				continue
			}
			addr, err := mail.ParseAddress(v)
			if err != nil {
				return nil, fmt.Errorf("invalid email recipient: %q", v)
			}
			email := strings.ToLower(strings.TrimSpace(addr.Address))
			if email == "" {
				return nil, fmt.Errorf("invalid email recipient: %q", v)
			}
			if seen[email] {
				continue
			}
			seen[email] = true
			out = append(out, email)
			if len(out) > maxWorkspaceNotificationRecipients {
				return nil, fmt.Errorf("too many email recipients (max %d)", maxWorkspaceNotificationRecipients)
			}
		}
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

func parseWorkspaceNotificationDeployEvents(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		if strings.TrimSpace(item) == "" {
			continue
		}
		norm := models.NormalizeWorkspaceNotificationDeployEvent(item)
		if norm == "" {
			return nil, fmt.Errorf("unsupported deploy event: %q", strings.TrimSpace(item))
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

func parseWorkspaceNotificationAuditEvents(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	if len(raw) > maxWorkspaceNotificationAuditFilters {
		return nil, fmt.Errorf("too many audit event filters (max %d)", maxWorkspaceNotificationAuditFilters)
	}
	return models.ValidateWorkspaceNotificationAuditEvents(raw)
}

func validateWorkspaceNotificationWebhookURL(raw string, channel string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return fmt.Errorf("invalid %s webhook url", channel)
	}
	if !strings.EqualFold(strings.TrimSpace(u.Scheme), "https") || strings.TrimSpace(u.Host) == "" {
		return fmt.Errorf("invalid %s webhook url", channel)
	}
	host := strings.ToLower(strings.TrimSpace(u.Host))
	path := strings.TrimSpace(u.Path)

	switch channel {
	case "slack":
		if host != "hooks.slack.com" || !strings.HasPrefix(path, "/services/") {
			return fmt.Errorf("invalid slack webhook url")
		}
	case "discord":
		if host != "discord.com" && host != "discordapp.com" && host != "ptb.discord.com" && host != "canary.discord.com" {
			return fmt.Errorf("invalid discord webhook url")
		}
		if !strings.HasPrefix(path, "/api/webhooks/") {
			return fmt.Errorf("invalid discord webhook url")
		}
	default:
		return fmt.Errorf("unsupported webhook channel")
	}

	return nil
}
