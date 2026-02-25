package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

func (h *ServiceHandler) serviceForAccessControl(w http.ResponseWriter, r *http.Request, mutation bool) (*models.Service, bool) {
	userID := middleware.GetUserID(r)
	serviceID := strings.TrimSpace(mux.Vars(r)["id"])
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		respondServiceNotFound(w, serviceID)
		return nil, false
	}
	required := models.RoleViewer
	if mutation {
		required = models.RoleDeveloper
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, required) {
		return nil, false
	}
	return svc, true
}

func (h *ServiceHandler) decryptedServiceEnvMap(serviceID string) map[string]string {
	env := map[string]string{}
	if h == nil || h.Config == nil {
		return env
	}
	vars, err := models.ListEnvVars("service", serviceID)
	if err != nil {
		return env
	}
	for _, ev := range vars {
		key := strings.TrimSpace(ev.Key)
		if key == "" || strings.TrimSpace(ev.EncryptedValue) == "" {
			continue
		}
		decrypted, err := utils.Decrypt(ev.EncryptedValue, h.Config.Crypto.EncryptionKey)
		if err != nil {
			continue
		}
		env[key] = strings.TrimSpace(decrypted)
	}
	return env
}

func fallbackAccessControlConfigFromEnv(env map[string]string, now time.Time) *services.ServiceAccessControlConfig {
	allowCIDRs, denyCIDRs := services.ResolveServiceAccessControlRangesFromEnv(env, now)
	if len(allowCIDRs) == 0 && len(denyCIDRs) == 0 {
		return nil
	}

	enabled := true
	mode := "allowlist"
	rulesCIDRs := allowCIDRs
	if len(denyCIDRs) > 0 && len(allowCIDRs) == 0 {
		mode = "blocklist"
		rulesCIDRs = denyCIDRs
	}

	rules := make([]services.ServiceAccessControlRule, 0, len(rulesCIDRs))
	for _, cidr := range rulesCIDRs {
		rules = append(rules, services.ServiceAccessControlRule{
			Name: cidr,
			CIDR: cidr,
		})
	}

	cfg := &services.ServiceAccessControlConfig{
		IPAllowlist: services.ServiceIPAllowlistAccessControl{
			Enabled: &enabled,
			Mode:    mode,
			Rules:   rules,
		},
	}
	normalized, err := services.NormalizeServiceAccessControlConfig(cfg, now)
	if err != nil {
		return nil
	}
	return normalized
}

func accessControlResponse(serviceID string, cfg *services.ServiceAccessControlConfig, now time.Time) map[string]interface{} {
	if cfg == nil {
		disabled := false
		cfg = &services.ServiceAccessControlConfig{
			IPAllowlist: services.ServiceIPAllowlistAccessControl{
				Enabled: &disabled,
				Mode:    "allowlist",
				Rules:   []services.ServiceAccessControlRule{},
			},
		}
	}
	allowCIDRs, denyCIDRs := services.ActiveCIDRsFromServiceAccessControl(cfg, now)

	return map[string]interface{}{
		"service_id":   serviceID,
		"ip_allowlist": cfg.IPAllowlist,
		"active": map[string]interface{}{
			"allowlist_cidrs": allowCIDRs,
			"blocklist_cidrs": denyCIDRs,
		},
		"updated_at": cfg.UpdatedAt,
	}
}

func (h *ServiceHandler) GetAccessControl(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForAccessControl(w, r, false)
	if !ok {
		return
	}

	now := time.Now().UTC()
	env := h.decryptedServiceEnvMap(svc.ID)
	raw := strings.TrimSpace(env[services.ServiceAccessControlConfigEnvKey])
	cfg, err := services.ParseServiceAccessControlConfig(raw, now)
	if err != nil {
		fallback := fallbackAccessControlConfigFromEnv(env, now)
		res := accessControlResponse(svc.ID, fallback, now)
		res["warning"] = "stored access control config is invalid; showing fallback policy"
		utils.RespondJSON(w, http.StatusOK, res)
		return
	}
	if cfg == nil {
		cfg = fallbackAccessControlConfigFromEnv(env, now)
	}

	utils.RespondJSON(w, http.StatusOK, accessControlResponse(svc.ID, cfg, now))
}

func (h *ServiceHandler) UpsertAccessControl(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForAccessControl(w, r, true)
	if !ok {
		return
	}

	var req struct {
		IPAllowlist services.ServiceIPAllowlistAccessControl `json:"ip_allowlist"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	now := time.Now().UTC()
	cfg := &services.ServiceAccessControlConfig{IPAllowlist: req.IPAllowlist, UpdatedAt: &now}
	_, normalized, err := services.EncodeServiceAccessControlConfig(cfg, now)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if normalized == nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid access control config")
		return
	}
	encodedConfig, err := json.Marshal(normalized)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to encode access control config")
		return
	}

	configCipher, err := utils.Encrypt(string(encodedConfig), h.Config.Crypto.EncryptionKey)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to persist access control config")
		return
	}

	allowCIDRs, denyCIDRs := services.ActiveCIDRsFromServiceAccessControl(normalized, now)
	allowRaw := strings.Join(allowCIDRs, ",")
	denyRaw := strings.Join(denyCIDRs, ",")

	upserts := []models.EnvVar{{
		OwnerType:      "service",
		OwnerID:        svc.ID,
		Key:            services.ServiceAccessControlConfigEnvKey,
		EncryptedValue: configCipher,
		IsSecret:       false,
	}}

	if allowRaw != "" {
		allowCipher, err := utils.Encrypt(allowRaw, h.Config.Crypto.EncryptionKey)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to persist allowlist ranges")
			return
		}
		upserts = append(upserts, models.EnvVar{
			OwnerType:      "service",
			OwnerID:        svc.ID,
			Key:            services.ServiceAccessControlAllowlistEnvKey,
			EncryptedValue: allowCipher,
			IsSecret:       false,
		})
	}
	if denyRaw != "" {
		denyCipher, err := utils.Encrypt(denyRaw, h.Config.Crypto.EncryptionKey)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to persist blocklist ranges")
			return
		}
		upserts = append(upserts, models.EnvVar{
			OwnerType:      "service",
			OwnerID:        svc.ID,
			Key:            services.ServiceAccessControlDenylistEnvKey,
			EncryptedValue: denyCipher,
			IsSecret:       false,
		})
	}

	if err := models.MergeUpsertEnvVars("service", svc.ID, upserts); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to save access control settings")
		return
	}

	deleteKeys := []string{}
	if allowRaw == "" {
		deleteKeys = append(deleteKeys, services.ServiceAccessControlAllowlistEnvKey)
	}
	if denyRaw == "" {
		deleteKeys = append(deleteKeys, services.ServiceAccessControlDenylistEnvKey)
	}
	if len(deleteKeys) > 0 {
		_ = models.DeleteEnvVarsByKeys("service", svc.ID, deleteKeys)
	}

	userID := strings.TrimSpace(middleware.GetUserID(r))
	services.Audit(svc.WorkspaceID, userID, "service.access_control.updated", "service", svc.ID, map[string]interface{}{
		"enabled":          normalized.IPAllowlist.Enabled != nil && *normalized.IPAllowlist.Enabled,
		"mode":             normalized.IPAllowlist.Mode,
		"rules_count":      len(normalized.IPAllowlist.Rules),
		"allowlist_count":  len(allowCIDRs),
		"blocklist_count":  len(denyCIDRs),
		"deny_response_set": normalized.IPAllowlist.DenyResponse != nil,
	})

	reconcileAttempted := false
	reconcileApplied := false
	reconcileErr := ""
	if h.Config != nil && h.Config.Kubernetes.Enabled {
		reconcileAttempted = true
		if kd, err := services.NewKubeDeployer(h.Config); err == nil {
			if err := kd.ReconcileServiceIngressPolicies(svc); err == nil {
				reconcileApplied = true
			} else {
				reconcileErr = err.Error()
			}
		} else {
			reconcileErr = err.Error()
		}
	}

	response := accessControlResponse(svc.ID, normalized, now)
	response["ingress_policy_reconcile"] = map[string]interface{}{
		"attempted": reconcileAttempted,
		"applied":   reconcileApplied,
	}
	if strings.TrimSpace(reconcileErr) != "" {
		response["ingress_policy_reconcile_error"] = reconcileErr
	}

	utils.RespondJSON(w, http.StatusOK, response)
}

func (h *ServiceHandler) ListAccessControlLog(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForAccessControl(w, r, false)
	if !ok {
		return
	}

	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			if parsed < 1 {
				parsed = 1
			}
			if parsed > 200 {
				parsed = 200
			}
			limit = parsed
		}
	}

	entries, err := models.ListAuditLogsPage(svc.WorkspaceID, 500, 0)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load access control log")
		return
	}

	out := make([]map[string]interface{}, 0, limit)
	for _, entry := range entries {
		if len(out) >= limit {
			break
		}
		if strings.TrimSpace(entry.ResourceType) != "service" || strings.TrimSpace(entry.ResourceID) != svc.ID {
			continue
		}
		if strings.TrimSpace(entry.Action) != "service.access_control.updated" {
			continue
		}
		details := map[string]interface{}{}
		if len(entry.DetailsJSON) > 0 {
			_ = json.Unmarshal(entry.DetailsJSON, &details)
		}
		out = append(out, map[string]interface{}{
			"id":         entry.ID,
			"action":     entry.Action,
			"user_id":    entry.UserID,
			"created_at": entry.CreatedAt,
			"details":    details,
		})
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"service_id": svc.ID,
		"events":     out,
	})
}
