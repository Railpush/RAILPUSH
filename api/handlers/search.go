package handlers

import (
	"net/http"
	"strings"

	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type SearchHandler struct{}

func NewSearchHandler() *SearchHandler {
	return &SearchHandler{}
}

func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
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

	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := utils.GetQueryInt(r, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	resp := map[string]interface{}{
		"query":     query,
		"services":  []models.Service{},
		"databases": []models.ManagedDatabase{},
		"key_value": []models.ManagedKeyValue{},
	}
	if query == "" {
		utils.RespondJSON(w, http.StatusOK, resp)
		return
	}

	svcs, err := models.ListServices(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to search services")
		return
	}
	dbs, err := models.ListManagedDatabasesByWorkspace(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to search databases")
		return
	}
	kvs, err := models.ListManagedKeyValuesByWorkspace(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to search key-value stores")
		return
	}

	servicesOut := make([]models.Service, 0, limit)
	for _, svc := range svcs {
		if strings.EqualFold(strings.TrimSpace(svc.Status), "soft_deleted") {
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{
			svc.Name,
			svc.RepoURL,
			svc.Branch,
			svc.Runtime,
			svc.Type,
		}, " "))
		if !strings.Contains(haystack, query) {
			continue
		}
		servicesOut = append(servicesOut, svc)
		if len(servicesOut) >= limit {
			break
		}
	}

	databasesOut := make([]models.ManagedDatabase, 0, limit)
	for _, db := range dbs {
		if strings.EqualFold(strings.TrimSpace(db.Status), "soft_deleted") {
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{
			db.Name,
			db.DBName,
			db.Host,
			db.Plan,
			db.Status,
		}, " "))
		if !strings.Contains(haystack, query) {
			continue
		}
		databasesOut = append(databasesOut, db)
		if len(databasesOut) >= limit {
			break
		}
	}

	keyValueOut := make([]models.ManagedKeyValue, 0, limit)
	for _, kv := range kvs {
		if strings.EqualFold(strings.TrimSpace(kv.Status), "soft_deleted") {
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{
			kv.Name,
			kv.Host,
			kv.Plan,
			kv.Status,
			kv.MaxmemoryPolicy,
		}, " "))
		if !strings.Contains(haystack, query) {
			continue
		}
		keyValueOut = append(keyValueOut, kv)
		if len(keyValueOut) >= limit {
			break
		}
	}

	resp["services"] = servicesOut
	resp["databases"] = databasesOut
	resp["key_value"] = keyValueOut
	utils.RespondJSON(w, http.StatusOK, resp)
}
