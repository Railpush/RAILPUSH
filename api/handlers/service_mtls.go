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

func (h *ServiceHandler) serviceForMTLS(w http.ResponseWriter, r *http.Request, mutation bool) (*models.Service, bool) {
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

func resolveMTLSAllowedServiceIDs(workspaceID string, raw []string) ([]string, error) {
	resolved := []string{}
	if len(raw) == 0 {
		return resolved, nil
	}

	servicesInWorkspace, err := models.ListServices(workspaceID)
	if err != nil {
		return nil, err
	}

	lookup := map[string]string{}
	for _, svc := range servicesInWorkspace {
		id := strings.TrimSpace(svc.ID)
		if id == "" {
			continue
		}
		lookup[strings.ToLower(id)] = id
		name := strings.TrimSpace(svc.Name)
		if name != "" {
			lookup[strings.ToLower(name)] = id
			lookup[strings.ToLower(utils.ServiceDomainLabel(name))] = id
		}
		sub := strings.TrimSpace(svc.Subdomain)
		if sub != "" {
			lookup[strings.ToLower(sub)] = id
		}
	}

	seen := map[string]struct{}{}
	for _, item := range raw {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		candidate := strings.ToLower(value)
		if strings.HasPrefix(candidate, "srv_") {
			candidate = strings.TrimPrefix(candidate, "srv_")
		}
		serviceID, ok := lookup[candidate]
		if !ok || strings.TrimSpace(serviceID) == "" {
			return nil, fmt.Errorf("unknown allowed service %q", value)
		}
		if _, exists := seen[serviceID]; exists {
			continue
		}
		seen[serviceID] = struct{}{}
		resolved = append(resolved, serviceID)
	}

	return resolved, nil
}

func mtlsResponse(serviceID string, cfg *services.ServiceMTLSConfig) map[string]interface{} {
	if cfg == nil {
		disabled := false
		cfg = &services.ServiceMTLSConfig{
			Enabled:         &disabled,
			Mode:            "strict",
			AllowedServices: []string{},
		}
	}
	enabled := false
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	return map[string]interface{}{
		"service_id":        serviceID,
		"enabled":           enabled,
		"mode":              cfg.Mode,
		"allowed_services":  cfg.AllowedServices,
		"strict_enforcement": services.IsStrictServiceMTLS(cfg),
		"updated_at":        cfg.UpdatedAt,
	}
}

func (h *ServiceHandler) GetServiceMTLS(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForMTLS(w, r, false)
	if !ok {
		return
	}

	now := time.Now().UTC()
	env := h.decryptedServiceEnvMap(svc.ID)
	raw := strings.TrimSpace(env[services.ServiceMTLSConfigEnvKey])
	cfg, err := services.ParseServiceMTLSConfig(raw, now)
	if err != nil {
		res := mtlsResponse(svc.ID, nil)
		res["warning"] = "stored mtls config is invalid"
		utils.RespondJSON(w, http.StatusOK, res)
		return
	}
	utils.RespondJSON(w, http.StatusOK, mtlsResponse(svc.ID, cfg))
}

func (h *ServiceHandler) UpsertServiceMTLS(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.serviceForMTLS(w, r, true)
	if !ok {
		return
	}

	var req services.ServiceMTLSConfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	now := time.Now().UTC()
	req.UpdatedAt = &now
	_, normalized, err := services.EncodeServiceMTLSConfig(&req, now)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if normalized == nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid mtls config")
		return
	}

	resolvedAllowed, err := resolveMTLSAllowedServiceIDs(svc.WorkspaceID, normalized.AllowedServices)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalized.AllowedServices = resolvedAllowed
	normalized.UpdatedAt = &now

	encodedJSON, err := json.Marshal(normalized)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to encode mtls config")
		return
	}
	encrypted, err := utils.Encrypt(string(encodedJSON), h.Config.Crypto.EncryptionKey)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to persist mtls config")
		return
	}

	if err := models.MergeUpsertEnvVars("service", svc.ID, []models.EnvVar{{
		OwnerType:      "service",
		OwnerID:        svc.ID,
		Key:            services.ServiceMTLSConfigEnvKey,
		EncryptedValue: encrypted,
		IsSecret:       false,
	}}); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to save mtls config")
		return
	}

	userID := strings.TrimSpace(middleware.GetUserID(r))
	services.Audit(svc.WorkspaceID, userID, "service.mtls.updated", "service", svc.ID, map[string]interface{}{
		"enabled":               normalized.Enabled != nil && *normalized.Enabled,
		"mode":                  normalized.Mode,
		"allowed_services_count": len(normalized.AllowedServices),
		"strict_enforcement":    services.IsStrictServiceMTLS(normalized),
	})

	reconcileAttempted := false
	reconcileApplied := false
	reconcileErr := ""
	if h.Config != nil && h.Config.Kubernetes.Enabled {
		reconcileAttempted = true
		if kd, err := services.NewKubeDeployer(h.Config); err == nil {
			if err := kd.ReconcileServiceMTLSPolicy(svc, normalized); err == nil {
				reconcileApplied = true
			} else {
				reconcileErr = err.Error()
			}
		} else {
			reconcileErr = err.Error()
		}
	}

	response := mtlsResponse(svc.ID, normalized)
	response["policy_reconcile"] = map[string]interface{}{
		"attempted": reconcileAttempted,
		"applied":   reconcileApplied,
	}
	if strings.TrimSpace(reconcileErr) != "" {
		response["policy_reconcile_error"] = reconcileErr
	}

	utils.RespondJSON(w, http.StatusOK, response)
}
