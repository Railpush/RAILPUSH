package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type EnvVarHandler struct {
	Config *config.Config
}

func NewEnvVarHandler(cfg *config.Config) *EnvVarHandler {
	return &EnvVarHandler{Config: cfg}
}

func (h *EnvVarHandler) ListEnvVars(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	vars, err := models.ListEnvVars("service", serviceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list env vars")
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

// MergeEnvVars handles PATCH /services/{id}/env-vars — additive upsert.
// Provided keys are created or updated; keys not in the payload are left untouched.
// Optionally, keys listed in "delete" are removed.
func (h *EnvVarHandler) MergeEnvVars(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		EnvVars []struct {
			Key      string `json:"key"`
			Value    string `json:"value"`
			IsSecret bool   `json:"is_secret"`
		} `json:"env_vars"`
		Delete []string `json:"delete"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Upsert provided vars.
	if len(req.EnvVars) > 0 {
		vars := make([]models.EnvVar, 0, len(req.EnvVars))
		for _, item := range req.EnvVars {
			key := strings.TrimSpace(item.Key)
			if key == "" {
				continue
			}
			encrypted, err := utils.Encrypt(item.Value, h.Config.Crypto.EncryptionKey)
			if err != nil {
				utils.RespondError(w, http.StatusInternalServerError, "encryption failed")
				return
			}
			vars = append(vars, models.EnvVar{Key: key, EncryptedValue: encrypted, IsSecret: item.IsSecret})
		}
		if err := models.MergeUpsertEnvVars("service", serviceID, vars); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to upsert env vars: "+err.Error())
			return
		}
	}

	// Delete specified keys.
	if len(req.Delete) > 0 {
		trimmed := make([]string, 0, len(req.Delete))
		for _, k := range req.Delete {
			k = strings.TrimSpace(k)
			if k != "" {
				trimmed = append(trimmed, k)
			}
		}
		if err := models.DeleteEnvVarsByKeys("service", serviceID, trimmed); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to delete env vars: "+err.Error())
			return
		}
	}

	services.Audit(svc.WorkspaceID, userID, "service.env_vars_merged", "service", svc.ID, map[string]interface{}{
		"upserted": len(req.EnvVars),
		"deleted":  len(req.Delete),
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *EnvVarHandler) BulkUpdateEnvVars(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Preserve existing secret values when the client sends a masked placeholder ("********").
	// The UI never sees secret plaintext, so "save" without this would overwrite secrets with the mask.
	existing, _ := models.ListEnvVars("service", serviceID)
	existingByKey := map[string]models.EnvVar{}
	for _, v := range existing {
		k := strings.TrimSpace(v.Key)
		if k == "" {
			continue
		}
		existingByKey[k] = v
	}

	type envVarInput struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		IsSecret bool   `json:"is_secret"`
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	confirmDestructive := false
	mode := ""
	var req []envVarInput
	if body[0] == '[' {
		if err := json.Unmarshal(body, &req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	} else {
		var payload struct {
			EnvVars            []envVarInput `json:"env_vars"`
			ConfirmDestructive bool          `json:"confirm_destructive"`
			Mode               string        `json:"mode"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		req = payload.EnvVars
		confirmDestructive = payload.ConfirmDestructive
		mode = strings.ToLower(strings.TrimSpace(payload.Mode))
		if mode != "" && mode != "replace" {
			utils.RespondError(w, http.StatusBadRequest, "invalid mode (use replace)")
			return
		}
	}

	if len(req) == 0 && len(existing) == 0 {
		utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		return
	}

	incomingKeys := map[string]struct{}{}
	missingExisting := make([]string, 0)

	for _, item := range req {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		if _, exists := incomingKeys[key]; exists {
			utils.RespondError(w, http.StatusBadRequest, "duplicate env var key: "+key)
			return
		}
		incomingKeys[key] = struct{}{}
	}
	for key := range existingByKey {
		if _, ok := incomingKeys[key]; !ok {
			missingExisting = append(missingExisting, key)
		}
	}
	if len(missingExisting) > 0 && !confirmDestructive {
		utils.RespondError(w, http.StatusBadRequest, "destructive replace detected; resend with {\"mode\":\"replace\",\"confirm_destructive\":true} or use PATCH /env-vars")
		return
	}

	vars := make([]models.EnvVar, 0, len(req))
	for _, item := range req {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}

		// Secret placeholder: keep the existing encrypted value for this key (if present).
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

	if err := models.BulkUpsertEnvVars("service", serviceID, vars); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update env vars: "+err.Error())
		return
	}
	services.Audit(svc.WorkspaceID, userID, "service.env_vars_updated", "service", svc.ID, map[string]interface{}{
		"count":                len(vars),
		"removed":              len(missingExisting),
		"confirm_destructive":  confirmDestructive,
		"mode":                 "replace",
	})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"status": "updated", "removed": len(missingExisting)})
}
