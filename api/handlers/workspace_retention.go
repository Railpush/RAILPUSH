package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/railpush/api/database"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

const (
	defaultWorkspaceAuditLogRetentionDays  = 365
	defaultWorkspaceDeployHistoryRetention = 180
	defaultWorkspaceMetricHistoryRetention = 90
)

type workspaceRetention struct {
	AuditLogDays      int
	DeployHistoryDays int
	MetricHistoryDays int
}

func (h *WorkspaceHandler) GetRetention(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	ws, err := models.GetWorkspace(workspaceID)
	if err != nil || ws == nil {
		utils.RespondError(w, http.StatusNotFound, "workspace not found")
		return
	}

	ret, err := getWorkspaceRetention(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load retention policy")
		return
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"workspace_id":        workspaceID,
		"audit_logs":          formatRetentionDays(ret.AuditLogDays),
		"deploy_history":      formatRetentionDays(ret.DeployHistoryDays),
		"metric_history":      formatRetentionDays(ret.MetricHistoryDays),
		"audit_log_days":      ret.AuditLogDays,
		"deploy_history_days": ret.DeployHistoryDays,
		"metric_history_days": ret.MetricHistoryDays,
		"enforced_cleanup":    []string{"audit_logs", "deploy_history"},
		"pending_enforcement": []string{"metric_history"},
	})
}

func (h *WorkspaceHandler) UpdateRetention(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	ws, err := models.GetWorkspace(workspaceID)
	if err != nil || ws == nil {
		utils.RespondError(w, http.StatusNotFound, "workspace not found")
		return
	}

	current, err := getWorkspaceRetention(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load retention policy")
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&payload); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	auditDays := current.AuditLogDays
	deployDays := current.DeployHistoryDays
	metricDays := current.MetricHistoryDays
	updated := false

	if v, ok, err := parseRetentionDaysField(payload, "audit_logs", 1, 3650); err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	} else if ok {
		auditDays = v
		updated = true
	}
	if v, ok, err := parseRetentionDaysField(payload, "deploy_history", 1, 3650); err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	} else if ok {
		deployDays = v
		updated = true
	}
	if v, ok, err := parseRetentionDaysField(payload, "metric_history", 1, 3650); err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	} else if ok {
		metricDays = v
		updated = true
	}

	if !updated {
		utils.RespondError(w, http.StatusBadRequest, "at least one retention field must be provided")
		return
	}

	if _, err := database.DB.Exec(
		`UPDATE workspaces
		    SET audit_log_retention_days=$1,
		        deploy_history_retention_days=$2,
		        metric_history_retention_days=$3
		  WHERE id=$4`,
		auditDays, deployDays, metricDays, workspaceID,
	); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update retention policy")
		return
	}

	services.Audit(workspaceID, userID, "workspace.retention_updated", "workspace", workspaceID, map[string]interface{}{
		"audit_log_days":      auditDays,
		"deploy_history_days": deployDays,
		"metric_history_days": metricDays,
	})

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":              "updated",
		"workspace_id":        workspaceID,
		"audit_logs":          formatRetentionDays(auditDays),
		"deploy_history":      formatRetentionDays(deployDays),
		"metric_history":      formatRetentionDays(metricDays),
		"audit_log_days":      auditDays,
		"deploy_history_days": deployDays,
		"metric_history_days": metricDays,
	})
}

func getWorkspaceRetention(workspaceID string) (workspaceRetention, error) {
	ret := workspaceRetention{}
	err := database.DB.QueryRow(
		`SELECT COALESCE(audit_log_retention_days, $2),
		        COALESCE(deploy_history_retention_days, $3),
		        COALESCE(metric_history_retention_days, $4)
		   FROM workspaces
		  WHERE id=$1`,
		workspaceID,
		defaultWorkspaceAuditLogRetentionDays,
		defaultWorkspaceDeployHistoryRetention,
		defaultWorkspaceMetricHistoryRetention,
	).Scan(&ret.AuditLogDays, &ret.DeployHistoryDays, &ret.MetricHistoryDays)
	if err != nil {
		return workspaceRetention{}, err
	}
	return ret, nil
}
