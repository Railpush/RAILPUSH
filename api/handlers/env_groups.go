package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type EnvGroupHandler struct{}

func NewEnvGroupHandler() *EnvGroupHandler {
	return &EnvGroupHandler{}
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
