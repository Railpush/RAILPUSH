package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/railpush/api/models"
)

func Audit(workspaceID, userID, action, resourceType, resourceID string, details interface{}) {
	if workspaceID == "" || userID == "" || action == "" {
		return
	}
	if err := models.CreateAuditLog(workspaceID, userID, action, resourceType, resourceID, details); err != nil {
		log.Printf("audit write failed: action=%s workspace=%s user=%s err=%v", action, workspaceID, userID, err)
		return
	}
	go notifyWorkspaceAuditEvent(workspaceID, userID, action, resourceType, resourceID, details)
}

func notifyWorkspaceAuditEvent(workspaceID, userID, action, resourceType, resourceID string, details interface{}) {
	workspaceID = strings.TrimSpace(workspaceID)
	action = strings.TrimSpace(action)
	if workspaceID == "" || action == "" {
		return
	}

	cfg, err := models.GetWorkspaceNotificationChannels(workspaceID)
	if err != nil || cfg == nil {
		return
	}
	if !models.IsWorkspaceNotificationAuditEventEnabled(cfg.AuditEvents, action) {
		return
	}

	slackURL := strings.TrimSpace(cfg.SlackWebhookURL)
	discordURL := strings.TrimSpace(cfg.DiscordWebhookURL)
	if slackURL == "" && discordURL == "" && len(cfg.EmailRecipients) == 0 {
		return
	}

	now := time.Now().UTC()
	payload := map[string]interface{}{
		"workspace_id":  workspaceID,
		"user_id":       strings.TrimSpace(userID),
		"action":        action,
		"resource_type": strings.TrimSpace(resourceType),
		"resource_id":   strings.TrimSpace(resourceID),
		"occurred_at":   now.Format(time.RFC3339),
		"details":       details,
	}
	body, _ := json.Marshal(payload)
	summary := buildAuditAlertSummary(payload)

	if slackURL != "" {
		if msg, err := json.Marshal(map[string]string{"text": summary}); err == nil {
			go postWorkspaceNotificationWebhook("slack", slackURL, msg)
		}
	}
	if discordURL != "" {
		if msg, err := json.Marshal(map[string]string{"content": summary}); err == nil {
			go postWorkspaceNotificationWebhook("discord", discordURL, msg)
		}
	}

	if len(cfg.EmailRecipients) == 0 {
		return
	}
	subject := "Audit alert: " + action
	text := summary + "\n\nPayload:\n" + string(body)
	for _, recipient := range cfg.EmailRecipients {
		email := strings.ToLower(strings.TrimSpace(recipient))
		if email == "" {
			continue
		}
		dedupe := fmt.Sprintf("audit-alert:%s:%s:%s:%s:%s", workspaceID, action, strings.TrimSpace(resourceID), email, now.Format(time.RFC3339Nano))
		if _, err := models.EnqueueEmail(dedupe, "audit_event_alert", email, subject, text, ""); err != nil {
			log.Printf("audit alert email enqueue failed workspace=%s action=%s err=%v", workspaceID, action, err)
		}
	}
}

func buildAuditAlertSummary(payload map[string]interface{}) string {
	action, _ := payload["action"].(string)
	workspaceID, _ := payload["workspace_id"].(string)
	userID, _ := payload["user_id"].(string)
	resourceType, _ := payload["resource_type"].(string)
	resourceID, _ := payload["resource_id"].(string)
	occurredAt, _ := payload["occurred_at"].(string)

	lines := []string{
		"RailPush audit alert",
		"Action: " + action,
		"Workspace: " + workspaceID,
		"User: " + userID,
	}
	if strings.TrimSpace(resourceType) != "" || strings.TrimSpace(resourceID) != "" {
		lines = append(lines, "Resource: "+strings.TrimSpace(resourceType)+" "+strings.TrimSpace(resourceID))
	}
	if strings.TrimSpace(occurredAt) != "" {
		lines = append(lines, "Time: "+occurredAt)
	}
	if details, ok := payload["details"]; ok && details != nil {
		if b, err := json.Marshal(details); err == nil && len(b) > 0 {
			lines = append(lines, "Details: "+string(b))
		}
	}
	return strings.Join(lines, "\n")
}

func postWorkspaceNotificationWebhook(channel string, webhookURL string, body []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("audit alert webhook: %s request build failed err=%v", channel, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "railpush-audit-notification/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("audit alert webhook: %s request failed err=%v", channel, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("audit alert webhook: %s non-2xx status=%d", channel, resp.StatusCode)
	}
}
