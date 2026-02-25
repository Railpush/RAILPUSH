package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

func (h *LogHandler) serviceForLogDrainAccess(w http.ResponseWriter, r *http.Request, mutation bool) (*models.Service, bool) {
	userID := middleware.GetUserID(r)
	serviceID := strings.TrimSpace(mux.Vars(r)["id"])
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		respondServiceNotFound(w, serviceID)
		return nil, false
	}
	requiredRole := models.RoleViewer
	if mutation {
		requiredRole = models.RoleDeveloper
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, requiredRole); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return nil, false
	}
	return svc, true
}

func normalizeLogDrainPatterns(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		k := strings.ToLower(v)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
		if len(out) >= 50 {
			break
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func validateRegexPatterns(values []string) error {
	for _, raw := range values {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if _, err := regexp.Compile("(?i)" + p); err != nil {
			return err
		}
	}
	return nil
}

func (h *LogHandler) logDrainResponse(drain models.ServiceLogDrain) map[string]interface{} {
	decoded := map[string]interface{}{}
	if h != nil && h.Config != nil {
		if cfg, err := services.DecodeServiceLogDrainConfig(h.Config, drain.ConfigEncrypted); err == nil {
			decoded = services.RedactServiceLogDrainConfig(cfg)
		}
	}

	var lagSeconds interface{}
	if drain.LastCursorAt != nil {
		lag := int(time.Since(drain.LastCursorAt.UTC()).Seconds())
		if lag < 0 {
			lag = 0
		}
		lagSeconds = lag
	}

	return map[string]interface{}{
		"id":          drain.ID,
		"service_id":  drain.ServiceID,
		"workspace_id": drain.WorkspaceID,
		"name":        drain.Name,
		"destination": drain.Destination,
		"enabled":     drain.Enabled,
		"config":      decoded,
		"filter": map[string]interface{}{
			"log_types":        drain.FilterLogTypes,
			"min_level":        drain.FilterMinLevel,
			"include_patterns": drain.IncludePatterns,
			"exclude_patterns": drain.ExcludePatterns,
		},
		"stats": map[string]interface{}{
			"sent_count":       drain.SentCount,
			"failed_count":     drain.FailedCount,
			"last_error":       drain.LastError,
			"last_delivery_at": drain.LastDeliveryAt,
			"last_cursor_at":   drain.LastCursorAt,
			"last_test_at":     drain.LastTestAt,
			"lag_seconds":      lagSeconds,
		},
		"created_at": drain.CreatedAt,
		"updated_at": drain.UpdatedAt,
	}
}

func (h *LogHandler) ListLogDrains(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForLogDrainAccess(w, r, false)
	if !ok {
		return
	}

	drains, err := models.ListServiceLogDrains(svc.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list log drains")
		return
	}
	out := make([]map[string]interface{}, 0, len(drains))
	for _, drain := range drains {
		out = append(out, h.logDrainResponse(drain))
	}
	utils.RespondJSON(w, http.StatusOK, out)
}

func (h *LogHandler) CreateLogDrain(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForLogDrainAccess(w, r, true)
	if !ok {
		return
	}

	var req struct {
		Name        string                 `json:"name"`
		Destination string                 `json:"destination"`
		Config      map[string]interface{} `json:"config"`
		Enabled     *bool                  `json:"enabled"`
		Filter      struct {
			LogTypes        []string `json:"log_types"`
			MinLevel        string   `json:"min_level"`
			IncludePatterns []string `json:"include_patterns"`
			ExcludePatterns []string `json:"exclude_patterns"`
		} `json:"filter"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		utils.RespondError(w, http.StatusBadRequest, "name is required")
		return
	}
	destination := models.NormalizeServiceLogDrainDestination(req.Destination)
	if destination == "" {
		utils.RespondError(w, http.StatusBadRequest, "invalid destination")
		return
	}

	logTypes := models.NormalizeServiceLogDrainLogTypes(req.Filter.LogTypes)
	minLevel := models.NormalizeServiceLogDrainLevel(req.Filter.MinLevel)
	if minLevel == "" {
		minLevel = "info"
	}
	includePatterns := normalizeLogDrainPatterns(req.Filter.IncludePatterns)
	excludePatterns := normalizeLogDrainPatterns(req.Filter.ExcludePatterns)
	if err := validateRegexPatterns(includePatterns); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid include_patterns regex")
		return
	}
	if err := validateRegexPatterns(excludePatterns); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid exclude_patterns regex")
		return
	}

	if req.Config == nil {
		req.Config = map[string]interface{}{}
	}
	if err := services.ValidateServiceLogDrainConfig(destination, req.Config); err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	encodedConfig, err := json.Marshal(req.Config)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid config")
		return
	}
	encryptedConfig, err := utils.Encrypt(string(encodedConfig), h.Config.Crypto.EncryptionKey)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to encrypt config")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	userID := strings.TrimSpace(middleware.GetUserID(r))
	var createdBy *string
	if userID != "" {
		createdBy = &userID
	}

	drain := &models.ServiceLogDrain{
		ServiceID:       svc.ID,
		WorkspaceID:     svc.WorkspaceID,
		Name:            name,
		Destination:     destination,
		ConfigEncrypted: encryptedConfig,
		FilterLogTypes:  logTypes,
		FilterMinLevel:  minLevel,
		IncludePatterns: includePatterns,
		ExcludePatterns: excludePatterns,
		Enabled:         enabled,
		CreatedBy:       createdBy,
	}

	if err := models.CreateServiceLogDrain(drain); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create log drain")
		return
	}

	services.Audit(svc.WorkspaceID, userID, "service.log_drain.created", "service", svc.ID, map[string]interface{}{
		"drain_id":    drain.ID,
		"destination": destination,
		"enabled":     enabled,
	})

	created, err := models.GetServiceLogDrainForService(svc.ID, drain.ID)
	if err != nil || created == nil {
		utils.RespondJSON(w, http.StatusCreated, map[string]string{"id": drain.ID})
		return
	}
	utils.RespondJSON(w, http.StatusCreated, h.logDrainResponse(*created))
}

func (h *LogHandler) DeleteLogDrain(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForLogDrainAccess(w, r, true)
	if !ok {
		return
	}
	drainID := strings.TrimSpace(mux.Vars(r)["drainId"])
	drain, err := models.GetServiceLogDrainForService(svc.ID, drainID)
	if err != nil || drain == nil {
		utils.RespondError(w, http.StatusNotFound, "log drain not found")
		return
	}
	if err := models.DeleteServiceLogDrain(svc.ID, drainID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete log drain")
		return
	}

	userID := strings.TrimSpace(middleware.GetUserID(r))
	services.Audit(svc.WorkspaceID, userID, "service.log_drain.deleted", "service", svc.ID, map[string]interface{}{
		"drain_id":    drain.ID,
		"destination": drain.Destination,
	})

	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *LogHandler) GetLogDrainStats(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForLogDrainAccess(w, r, false)
	if !ok {
		return
	}
	drainID := strings.TrimSpace(mux.Vars(r)["drainId"])
	drain, err := models.GetServiceLogDrainForService(svc.ID, drainID)
	if err != nil || drain == nil {
		utils.RespondError(w, http.StatusNotFound, "log drain not found")
		return
	}

	var lagSeconds interface{}
	if drain.LastCursorAt != nil {
		lag := int(time.Since(drain.LastCursorAt.UTC()).Seconds())
		if lag < 0 {
			lag = 0
		}
		lagSeconds = lag
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"drain_id":         drain.ID,
		"service_id":       drain.ServiceID,
		"destination":      drain.Destination,
		"enabled":          drain.Enabled,
		"sent_count":       drain.SentCount,
		"failed_count":     drain.FailedCount,
		"last_error":       drain.LastError,
		"last_delivery_at": drain.LastDeliveryAt,
		"last_cursor_at":   drain.LastCursorAt,
		"last_test_at":     drain.LastTestAt,
		"lag_seconds":      lagSeconds,
		"updated_at":       drain.UpdatedAt,
	})
}

func (h *LogHandler) TestLogDrain(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForLogDrainAccess(w, r, true)
	if !ok {
		return
	}
	drainID := strings.TrimSpace(mux.Vars(r)["drainId"])
	drain, err := models.GetServiceLogDrainForService(svc.ID, drainID)
	if err != nil || drain == nil {
		utils.RespondError(w, http.StatusNotFound, "log drain not found")
		return
	}

	now := time.Now().UTC()
	testEntry := services.LogDrainEntry{
		Timestamp:  now,
		Level:      "error",
		Message:    fmt.Sprintf("RailPush log drain test (%s)", drain.Name),
		InstanceID: "test",
		LogType:    "app",
		Fields: map[string]string{
			"event":      "log_drain_test",
			"destination": drain.Destination,
		},
	}

	err = services.DeliverServiceLogDrainBatch(h.Config, drain, svc.Name, []services.LogDrainEntry{testEntry})
	if err != nil {
		_ = models.RecordServiceLogDrainTestResult(drain.ID, false, err.Error())
		utils.RespondError(w, http.StatusBadGateway, "test delivery failed: "+err.Error())
		return
	}
	_ = models.RecordServiceLogDrainTestResult(drain.ID, true, "")

	userID := strings.TrimSpace(middleware.GetUserID(r))
	services.Audit(svc.WorkspaceID, userID, "service.log_drain.tested", "service", svc.ID, map[string]interface{}{
		"drain_id":    drain.ID,
		"destination": drain.Destination,
	})

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "sent",
		"drain_id":    drain.ID,
		"destination": drain.Destination,
		"tested_at":   now,
	})
}
