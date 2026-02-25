package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

func parseLogAlertDurationSeconds(raw interface{}, fallback int) (int, error) {
	if raw == nil {
		return fallback, nil
	}
	switch v := raw.(type) {
	case float64:
		if v <= 0 {
			return 0, fmt.Errorf("duration must be positive")
		}
		return int(v), nil
	case int:
		if v <= 0 {
			return 0, fmt.Errorf("duration must be positive")
		}
		return v, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return fallback, nil
		}
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			return 0, fmt.Errorf("invalid duration")
		}
		return int(d.Seconds()), nil
	default:
		return 0, fmt.Errorf("invalid duration")
	}
}

func parseLogAlertComparison(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "", "greater_than", ">", "gt":
		return "greater_than"
	case "greater_than_or_equal", "gte", ">=":
		return "greater_than_or_equal"
	case "equal", "==", "eq":
		return "equal"
	default:
		return ""
	}
}

func (h *LogHandler) serviceForLogAlertAccess(w http.ResponseWriter, r *http.Request, mutation bool) (*models.Service, bool) {
	userID := middleware.GetUserID(r)
	serviceID := strings.TrimSpace(mux.Vars(r)["id"])
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return nil, false
	}
	required := models.RoleViewer
	if mutation {
		required = models.RoleDeveloper
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, required); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return nil, false
	}
	return svc, true
}

func buildStructuredFilterQuery(raw interface{}) (string, error) {
	if raw == nil {
		return "", nil
	}
	switch v := raw.(type) {
	case string:
		_, err := services.ParseStructuredFilter(v)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(v), nil
	case map[string]interface{}:
		parts := make([]string, 0, len(v))
		for key, val := range v {
			k := strings.TrimSpace(key)
			if k == "" {
				continue
			}
			value := strings.TrimSpace(fmt.Sprintf("%v", val))
			if value == "" {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s:%s", k, value))
		}
		out := strings.Join(parts, " AND ")
		_, err := services.ParseStructuredFilter(out)
		if err != nil {
			return "", err
		}
		return out, nil
	default:
		return "", fmt.Errorf("invalid filter")
	}
}

func formatLogAlertResponse(alert models.ServiceLogAlert) map[string]interface{} {
	window := time.Duration(alert.WindowSeconds) * time.Second
	cooldown := time.Duration(alert.CooldownSeconds) * time.Second

	return map[string]interface{}{
		"id":           alert.ID,
		"service_id":   alert.ServiceID,
		"workspace_id": alert.WorkspaceID,
		"name":         alert.Name,
		"enabled":      alert.Enabled,
		"condition": map[string]interface{}{
			"filter":     alert.FilterQuery,
			"pattern":    alert.Pattern,
			"threshold":  alert.Threshold,
			"window":     window.String(),
			"comparison": alert.Comparison,
		},
		"notification": map[string]interface{}{
			"channels":    alert.Channels,
			"webhook_url": alert.WebhookURL,
			"cooldown":    cooldown.String(),
			"priority":    alert.Priority,
		},
		"status":            alert.Status,
		"last_match_count":  alert.LastMatchCount,
		"last_evaluated_at": alert.LastEvaluatedAt,
		"last_triggered_at": alert.LastTriggeredAt,
		"last_resolved_at":  alert.LastResolvedAt,
		"created_at":        alert.CreatedAt,
		"updated_at":        alert.UpdatedAt,
	}
}

func (h *LogHandler) ListLogAlerts(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForLogAlertAccess(w, r, false)
	if !ok {
		return
	}
	alerts, err := models.ListServiceLogAlerts(svc.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list log alerts")
		return
	}
	out := make([]map[string]interface{}, 0, len(alerts))
	for _, alert := range alerts {
		out = append(out, formatLogAlertResponse(alert))
	}
	utils.RespondJSON(w, http.StatusOK, out)
}

func (h *LogHandler) CreateLogAlert(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForLogAlertAccess(w, r, true)
	if !ok {
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024)).Decode(&payload); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	name := strings.TrimSpace(fmt.Sprintf("%v", payload["name"]))
	if name == "" {
		utils.RespondError(w, http.StatusBadRequest, "name is required")
		return
	}

	enabled := true
	if raw, ok := payload["enabled"].(bool); ok {
		enabled = raw
	}

	condition, _ := payload["condition"].(map[string]interface{})
	notification, _ := payload["notification"].(map[string]interface{})

	filterQuery, err := buildStructuredFilterQuery(condition["filter"])
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid condition.filter")
		return
	}

	pattern := strings.TrimSpace(fmt.Sprintf("%v", condition["pattern"]))
	if strings.EqualFold(pattern, "<nil>") {
		pattern = ""
	}

	threshold := 1
	if raw, ok := condition["threshold"].(float64); ok {
		threshold = int(raw)
	}
	if threshold <= 0 {
		utils.RespondError(w, http.StatusBadRequest, "condition.threshold must be >= 1")
		return
	}

	windowSeconds, err := parseLogAlertDurationSeconds(condition["window"], 300)
	if err != nil || windowSeconds < 10 || windowSeconds > 86400 {
		utils.RespondError(w, http.StatusBadRequest, "condition.window must be between 10s and 24h")
		return
	}

	comparison := "greater_than"
	if raw, exists := condition["comparison"]; exists && raw != nil {
		comparison = parseLogAlertComparison(strings.TrimSpace(fmt.Sprintf("%v", raw)))
	}
	if comparison == "" {
		utils.RespondError(w, http.StatusBadRequest, "invalid condition.comparison")
		return
	}

	channels := []string{"incident"}
	if raw, ok := notification["channels"].([]interface{}); ok && len(raw) > 0 {
		channels = channels[:0]
		for _, v := range raw {
			channels = append(channels, fmt.Sprintf("%v", v))
		}
	}

	webhookURL := strings.TrimSpace(fmt.Sprintf("%v", notification["webhook_url"]))
	if strings.EqualFold(webhookURL, "<nil>") {
		webhookURL = ""
	}

	priority := "normal"
	if raw, exists := notification["priority"]; exists && raw != nil {
		priority = strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", raw)))
	}
	if strings.EqualFold(priority, "<nil>") || priority == "" {
		priority = "normal"
	}

	cooldownSeconds, err := parseLogAlertDurationSeconds(notification["cooldown"], 900)
	if err != nil || cooldownSeconds < 0 || cooldownSeconds > 86400 {
		utils.RespondError(w, http.StatusBadRequest, "notification.cooldown must be between 0s and 24h")
		return
	}

	if filterQuery == "" && pattern == "" {
		utils.RespondError(w, http.StatusBadRequest, "condition.filter or condition.pattern is required")
		return
	}

	userID := strings.TrimSpace(middleware.GetUserID(r))
	var createdBy *string
	if userID != "" {
		createdBy = &userID
	}
	alert := &models.ServiceLogAlert{
		ServiceID:       svc.ID,
		WorkspaceID:     svc.WorkspaceID,
		Name:            name,
		Enabled:         enabled,
		FilterQuery:     filterQuery,
		Pattern:         pattern,
		Threshold:       threshold,
		WindowSeconds:   windowSeconds,
		Comparison:      comparison,
		CooldownSeconds: cooldownSeconds,
		Channels:        channels,
		WebhookURL:      webhookURL,
		Priority:        priority,
		CreatedBy:       createdBy,
	}
	if err := models.CreateServiceLogAlert(alert); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create log alert")
		return
	}
	created, _ := models.GetServiceLogAlert(alert.ID)
	if created == nil {
		utils.RespondJSON(w, http.StatusCreated, map[string]string{"id": alert.ID})
		return
	}
	utils.RespondJSON(w, http.StatusCreated, formatLogAlertResponse(*created))
}

func (h *LogHandler) UpdateLogAlert(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForLogAlertAccess(w, r, true)
	if !ok {
		return
	}
	alertID := strings.TrimSpace(mux.Vars(r)["alertId"])
	alert, err := models.GetServiceLogAlert(alertID)
	if err != nil || alert == nil || alert.ServiceID != svc.ID {
		utils.RespondError(w, http.StatusNotFound, "log alert not found")
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024)).Decode(&payload); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	if raw, ok := payload["name"]; ok {
		name := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if name == "" {
			utils.RespondError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		alert.Name = name
	}
	if raw, ok := payload["enabled"].(bool); ok {
		alert.Enabled = raw
	}

	if condition, ok := payload["condition"].(map[string]interface{}); ok {
		if raw, exists := condition["filter"]; exists {
			filterQuery, err := buildStructuredFilterQuery(raw)
			if err != nil {
				utils.RespondError(w, http.StatusBadRequest, "invalid condition.filter")
				return
			}
			alert.FilterQuery = filterQuery
		}
		if raw, exists := condition["pattern"]; exists {
			alert.Pattern = strings.TrimSpace(fmt.Sprintf("%v", raw))
			if strings.EqualFold(alert.Pattern, "<nil>") {
				alert.Pattern = ""
			}
		}
		if raw, exists := condition["threshold"]; exists {
			if v, ok := raw.(float64); ok {
				alert.Threshold = int(v)
			}
			if alert.Threshold <= 0 {
				utils.RespondError(w, http.StatusBadRequest, "condition.threshold must be >= 1")
				return
			}
		}
		if raw, exists := condition["comparison"]; exists {
			if raw == nil {
				alert.Comparison = "greater_than"
			} else {
				comparison := parseLogAlertComparison(fmt.Sprintf("%v", raw))
				if comparison == "" {
					utils.RespondError(w, http.StatusBadRequest, "invalid condition.comparison")
					return
				}
				alert.Comparison = comparison
			}
		}
		if raw, exists := condition["window"]; exists {
			windowSeconds, err := parseLogAlertDurationSeconds(raw, alert.WindowSeconds)
			if err != nil || windowSeconds < 10 || windowSeconds > 86400 {
				utils.RespondError(w, http.StatusBadRequest, "condition.window must be between 10s and 24h")
				return
			}
			alert.WindowSeconds = windowSeconds
		}
	}

	if notification, ok := payload["notification"].(map[string]interface{}); ok {
		if raw, exists := notification["channels"]; exists {
			vals, ok := raw.([]interface{})
			if !ok {
				utils.RespondError(w, http.StatusBadRequest, "notification.channels must be an array")
				return
			}
			channels := make([]string, 0, len(vals))
			for _, v := range vals {
				channels = append(channels, fmt.Sprintf("%v", v))
			}
			alert.Channels = channels
		}
		if raw, exists := notification["webhook_url"]; exists {
			alert.WebhookURL = strings.TrimSpace(fmt.Sprintf("%v", raw))
			if strings.EqualFold(alert.WebhookURL, "<nil>") {
				alert.WebhookURL = ""
			}
		}
		if raw, exists := notification["priority"]; exists {
			alert.Priority = strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", raw)))
		}
		if raw, exists := notification["cooldown"]; exists {
			cooldownSeconds, err := parseLogAlertDurationSeconds(raw, alert.CooldownSeconds)
			if err != nil || cooldownSeconds < 0 || cooldownSeconds > 86400 {
				utils.RespondError(w, http.StatusBadRequest, "notification.cooldown must be between 0s and 24h")
				return
			}
			alert.CooldownSeconds = cooldownSeconds
		}
	}

	if strings.TrimSpace(alert.FilterQuery) == "" && strings.TrimSpace(alert.Pattern) == "" {
		utils.RespondError(w, http.StatusBadRequest, "condition.filter or condition.pattern is required")
		return
	}

	if err := models.UpdateServiceLogAlert(alert); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update log alert")
		return
	}
	updated, _ := models.GetServiceLogAlert(alert.ID)
	if updated == nil {
		utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	utils.RespondJSON(w, http.StatusOK, formatLogAlertResponse(*updated))
}

func (h *LogHandler) DeleteLogAlert(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForLogAlertAccess(w, r, true)
	if !ok {
		return
	}
	alertID := strings.TrimSpace(mux.Vars(r)["alertId"])
	alert, err := models.GetServiceLogAlert(alertID)
	if err != nil || alert == nil || alert.ServiceID != svc.ID {
		utils.RespondError(w, http.StatusNotFound, "log alert not found")
		return
	}
	if err := models.DeleteServiceLogAlert(alertID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete log alert")
		return
	}
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
