package handlers

import (
	"net/http"

	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type PreviewEnvironmentHandler struct{}

func NewPreviewEnvironmentHandler() *PreviewEnvironmentHandler {
	return &PreviewEnvironmentHandler{}
}

func (h *PreviewEnvironmentHandler) ListPreviewEnvironments(w http.ResponseWriter, r *http.Request) {
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
	items, err := models.ListPreviewEnvironments(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list preview environments")
		return
	}
	if items == nil {
		items = []models.PreviewEnvironment{}
	}
	utils.RespondJSON(w, http.StatusOK, items)
}
