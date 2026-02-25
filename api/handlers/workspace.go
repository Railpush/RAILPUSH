package handlers

import (
	"encoding/csv"
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

type WorkspaceHandler struct{}

func NewWorkspaceHandler() *WorkspaceHandler {
	return &WorkspaceHandler{}
}

func (h *WorkspaceHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	workspaceID := mux.Vars(r)["id"]
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	members, err := models.ListWorkspaceMembers(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list members")
		return
	}
	if members == nil {
		members = []models.WorkspaceMember{}
	}
	utils.RespondJSON(w, http.StatusOK, members)
}

func (h *WorkspaceHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	workspaceID := mux.Vars(r)["id"]
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" {
		utils.RespondError(w, http.StatusBadRequest, "email is required")
		return
	}
	user, err := models.GetUserByEmail(req.Email)
	if err != nil || user == nil {
		utils.RespondError(w, http.StatusNotFound, "user not found")
		return
	}
	role := models.NormalizeWorkspaceRole(req.Role)
	if role == models.RoleOwner {
		utils.RespondError(w, http.StatusBadRequest, "owner role cannot be assigned")
		return
	}
	if err := models.AddWorkspaceMember(workspaceID, user.ID, role); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to add member")
		return
	}
	services.Audit(workspaceID, userID, "workspace.member_added", "workspace_member", user.ID, map[string]interface{}{
		"role": role,
	})
	utils.RespondJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

func (h *WorkspaceHandler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	workspaceID := mux.Vars(r)["id"]
	targetUserID := mux.Vars(r)["userId"]
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	role, err := models.GetWorkspaceMemberRole(workspaceID, targetUserID)
	if err != nil || role == "" {
		utils.RespondError(w, http.StatusNotFound, "member not found")
		return
	}
	if role == models.RoleOwner {
		utils.RespondError(w, http.StatusBadRequest, "cannot change owner role")
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	newRole := models.NormalizeWorkspaceRole(req.Role)
	if newRole == models.RoleOwner {
		utils.RespondError(w, http.StatusBadRequest, "owner role cannot be assigned")
		return
	}
	if err := models.AddWorkspaceMember(workspaceID, targetUserID, newRole); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update member")
		return
	}
	services.Audit(workspaceID, userID, "workspace.member_role_updated", "workspace_member", targetUserID, map[string]interface{}{
		"role": newRole,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *WorkspaceHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	workspaceID := mux.Vars(r)["id"]
	targetUserID := mux.Vars(r)["userId"]
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	role, err := models.GetWorkspaceMemberRole(workspaceID, targetUserID)
	if err != nil || role == "" {
		utils.RespondError(w, http.StatusNotFound, "member not found")
		return
	}
	if role == models.RoleOwner {
		utils.RespondError(w, http.StatusBadRequest, "owner cannot be removed")
		return
	}
	if err := models.RemoveWorkspaceMember(workspaceID, targetUserID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}
	services.Audit(workspaceID, userID, "workspace.member_removed", "workspace_member", targetUserID, nil)
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (h *WorkspaceHandler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	workspaceID := mux.Vars(r)["id"]
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	pagination, err := parseCursorPagination(r)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if pagination.Enabled {
		total, err := models.CountAuditLogs(workspaceID)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to count audit logs")
			return
		}
		items, err := models.ListAuditLogsPage(workspaceID, pagination.Limit, pagination.Offset)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to list audit logs")
			return
		}
		if items == nil {
			items = []models.AuditLogEntry{}
		}
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"data":       items,
			"pagination": paginateWindowMeta(total, pagination, len(items)),
		})
		return
	}
	limit := 200
	if q := strings.TrimSpace(r.URL.Query().Get("limit")); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}
	items, err := models.ListAuditLogs(workspaceID, limit)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list audit logs")
		return
	}
	if items == nil {
		items = []models.AuditLogEntry{}
	}
	utils.RespondJSON(w, http.StatusOK, items)
}

func (h *WorkspaceHandler) ExportAuditLogsCSV(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	workspaceID := mux.Vars(r)["id"]
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	items, err := models.ListAuditLogs(workspaceID, 5000)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to export audit logs")
		return
	}

	filename := "audit-log-" + time.Now().UTC().Format("20060102-150405") + ".csv"
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "workspace_id", "user_id", "action", "resource_type", "resource_id", "details_json", "created_at"})
	for _, item := range items {
		_ = cw.Write([]string{
			item.ID,
			item.WorkspaceID,
			item.UserID,
			item.Action,
			item.ResourceType,
			item.ResourceID,
			string(item.DetailsJSON),
			item.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	cw.Flush()
}
