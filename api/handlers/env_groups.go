package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type EnvGroupHandler struct {
	Config *config.Config
}

func NewEnvGroupHandler(cfg *config.Config) *EnvGroupHandler {
	return &EnvGroupHandler{Config: cfg}
}

func (h *EnvGroupHandler) ListEnvGroups(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	workspaceID, err := resolveWorkspaceID(r, r.URL.Query().Get("workspace_id"))
	if err != nil || workspaceID == "" {
		utils.RespondError(w, http.StatusBadRequest, "workspace not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	groups, err := models.ListEnvGroups(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list env groups")
		return
	}
	if groups == nil {
		groups = []models.EnvGroup{}
	}
	utils.RespondJSON(w, http.StatusOK, groups)
}

func (h *EnvGroupHandler) CreateEnvGroup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	var req struct {
		WorkspaceID string `json:"workspace_id"`
		Name        string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	workspaceID, err := resolveWorkspaceID(r, req.WorkspaceID)
	if err != nil || workspaceID == "" {
		utils.RespondError(w, http.StatusBadRequest, "workspace not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		utils.RespondError(w, http.StatusBadRequest, "name is required")
		return
	}
	g := &models.EnvGroup{
		WorkspaceID: workspaceID,
		Name:        req.Name,
	}
	if err := models.CreateEnvGroup(g); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create env group")
		return
	}
	services.Audit(workspaceID, userID, "env_group.created", "env_group", g.ID, map[string]interface{}{
		"name": g.Name,
	})
	utils.RespondJSON(w, http.StatusCreated, g)
}

func (h *EnvGroupHandler) GetEnvGroup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	g, err := models.GetEnvGroup(id)
	if err != nil || g == nil {
		utils.RespondError(w, http.StatusNotFound, "environment group not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, g.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	utils.RespondJSON(w, http.StatusOK, g)
}

func (h *EnvGroupHandler) UpdateEnvGroup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	g, err := models.GetEnvGroup(id)
	if err != nil || g == nil {
		utils.RespondError(w, http.StatusNotFound, "environment group not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, g.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		utils.RespondError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := models.UpdateEnvGroup(id, req.Name); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update env group")
		return
	}
	g.Name = req.Name
	services.Audit(g.WorkspaceID, userID, "env_group.updated", "env_group", g.ID, map[string]interface{}{
		"name": g.Name,
	})
	utils.RespondJSON(w, http.StatusOK, g)
}

func (h *EnvGroupHandler) DeleteEnvGroup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	g, err := models.GetEnvGroup(id)
	if err != nil || g == nil {
		utils.RespondError(w, http.StatusNotFound, "environment group not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, g.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := models.DeleteEnvGroup(id); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete env group")
		return
	}
	services.Audit(g.WorkspaceID, userID, "env_group.deleted", "env_group", g.ID, map[string]interface{}{
		"name": g.Name,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *EnvGroupHandler) ListEnvGroupVars(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	g, err := models.GetEnvGroup(id)
	if err != nil || g == nil {
		utils.RespondError(w, http.StatusNotFound, "environment group not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, g.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	vars, err := models.ListEnvVars("env_group", id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list env group vars")
		return
	}
	result := make([]map[string]interface{}, 0, len(vars))
	for _, v := range vars {
		entry := map[string]interface{}{"id": v.ID, "key": v.Key, "is_secret": v.IsSecret, "created_at": v.CreatedAt}
		if !v.IsSecret {
			decrypted, err := utils.Decrypt(v.EncryptedValue, h.Config.Crypto.EncryptionKey)
			if err == nil {
				entry["value"] = decrypted
			} else {
				entry["value"] = ""
			}
		} else {
			entry["value"] = "********"
		}
		result = append(result, entry)
	}
	utils.RespondJSON(w, http.StatusOK, result)
}

func (h *EnvGroupHandler) BulkUpdateEnvGroupVars(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	g, err := models.GetEnvGroup(id)
	if err != nil || g == nil {
		utils.RespondError(w, http.StatusNotFound, "environment group not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, g.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	existing, _ := models.ListEnvVars("env_group", id)
	existingByKey := map[string]models.EnvVar{}
	for _, v := range existing {
		k := strings.TrimSpace(v.Key)
		if k != "" {
			existingByKey[k] = v
		}
	}

	var req []struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		IsSecret bool   `json:"is_secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	vars := make([]models.EnvVar, 0, len(req))
	for _, item := range req {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		if item.IsSecret && item.Value == "********" {
			if prev, ok := existingByKey[key]; ok && prev.IsSecret && strings.TrimSpace(prev.EncryptedValue) != "" {
				vars = append(vars, models.EnvVar{Key: key, EncryptedValue: prev.EncryptedValue, IsSecret: true})
				continue
			}
			utils.RespondError(w, http.StatusBadRequest, "secret value for "+key+" is masked; enter a new value")
			return
		}
		encrypted, err := utils.Encrypt(item.Value, h.Config.Crypto.EncryptionKey)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "encryption failed")
			return
		}
		vars = append(vars, models.EnvVar{Key: key, EncryptedValue: encrypted, IsSecret: item.IsSecret})
	}
	if err := models.BulkUpsertEnvVars("env_group", id, vars); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update env group vars: "+err.Error())
		return
	}
	services.Audit(g.WorkspaceID, userID, "env_group.vars_updated", "env_group", g.ID, map[string]interface{}{
		"count": len(vars),
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *EnvGroupHandler) LinkService(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	g, err := models.GetEnvGroup(id)
	if err != nil || g == nil {
		utils.RespondError(w, http.StatusNotFound, "environment group not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, g.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req struct {
		ServiceID string `json:"service_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ServiceID = strings.TrimSpace(req.ServiceID)
	if req.ServiceID == "" {
		utils.RespondError(w, http.StatusBadRequest, "service_id is required")
		return
	}
	svc, err := models.GetService(req.ServiceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if svc.WorkspaceID != g.WorkspaceID {
		utils.RespondError(w, http.StatusBadRequest, "service and env group must belong to the same workspace")
		return
	}
	if err := models.LinkServiceToEnvGroup(req.ServiceID, id); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to link service")
		return
	}
	services.Audit(g.WorkspaceID, userID, "env_group.service_linked", "env_group", g.ID, map[string]interface{}{
		"service_id": req.ServiceID,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "linked"})
}

func (h *EnvGroupHandler) UnlinkService(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	vars := mux.Vars(r)
	id := vars["id"]
	serviceID := vars["serviceId"]
	g, err := models.GetEnvGroup(id)
	if err != nil || g == nil {
		utils.RespondError(w, http.StatusNotFound, "environment group not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, g.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := models.UnlinkServiceFromEnvGroup(serviceID, id); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to unlink service")
		return
	}
	services.Audit(g.WorkspaceID, userID, "env_group.service_unlinked", "env_group", g.ID, map[string]interface{}{
		"service_id": serviceID,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "unlinked"})
}

func (h *EnvGroupHandler) ListLinkedServices(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	g, err := models.GetEnvGroup(id)
	if err != nil || g == nil {
		utils.RespondError(w, http.StatusNotFound, "environment group not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, g.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	serviceIDs, err := models.ListLinkedServices(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list linked services")
		return
	}
	if serviceIDs == nil {
		serviceIDs = []string{}
	}
	includeUsage := false
	includeUsageRaw := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("include_usage")))
	if includeUsageRaw == "1" || includeUsageRaw == "true" || includeUsageRaw == "yes" {
		includeUsage = true
	}
	if !includeUsage {
		utils.RespondJSON(w, http.StatusOK, serviceIDs)
		return
	}

	groupVars, err := models.ListEnvVars("env_group", id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to inspect env group vars")
		return
	}
	groupKeys := make([]string, 0, len(groupVars))
	groupKeySet := map[string]struct{}{}
	for _, v := range groupVars {
		k := strings.TrimSpace(v.Key)
		if k == "" {
			continue
		}
		if _, exists := groupKeySet[k]; exists {
			continue
		}
		groupKeySet[k] = struct{}{}
		groupKeys = append(groupKeys, k)
	}

	out := make([]map[string]interface{}, 0, len(serviceIDs))
	for _, serviceID := range serviceIDs {
		serviceName := ""
		if svc, err := models.GetService(serviceID); err == nil && svc != nil {
			serviceName = svc.Name
		}
		svcVars, err := models.ListEnvVars("service", serviceID)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to inspect linked service env vars")
			return
		}
		svcKeySet := map[string]struct{}{}
		for _, v := range svcVars {
			k := strings.TrimSpace(v.Key)
			if k != "" {
				svcKeySet[k] = struct{}{}
			}
		}
		usedKeys := make([]string, 0, len(groupKeys))
		missingKeys := make([]string, 0, len(groupKeys))
		for _, key := range groupKeys {
			if _, exists := svcKeySet[key]; exists {
				usedKeys = append(usedKeys, key)
			} else {
				missingKeys = append(missingKeys, key)
			}
		}
		out = append(out, map[string]interface{}{
			"service_id":    serviceID,
			"service_name":  serviceName,
			"used_keys":     usedKeys,
			"missing_keys":  missingKeys,
			"group_key_count": len(groupKeys),
		})
	}
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"service_ids":     serviceIDs,
		"group_keys":      groupKeys,
		"linked_services": out,
	})
	return
}
