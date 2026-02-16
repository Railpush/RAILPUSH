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
		"count": len(vars),
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
