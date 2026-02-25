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
	defaultServiceRuntimeLogRetentionDays = 30
	defaultServiceBuildLogRetentionDays   = 90
	defaultServiceRequestLogRetentionDays = 14
)

type serviceRetention struct {
	RuntimeLogsDays int
	BuildLogsDays   int
	RequestLogsDays int
}

func (h *ServiceHandler) GetRetention(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	ret, err := getServiceRetention(serviceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load retention policy")
		return
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"service_id":          serviceID,
		"runtime_logs":        formatRetentionDays(ret.RuntimeLogsDays),
		"build_logs":          formatRetentionDays(ret.BuildLogsDays),
		"request_logs":        formatRetentionDays(ret.RequestLogsDays),
		"runtime_logs_days":   ret.RuntimeLogsDays,
		"build_logs_days":     ret.BuildLogsDays,
		"request_logs_days":   ret.RequestLogsDays,
		"enforced_cleanup":    []string{"build_logs"},
		"pending_enforcement": []string{"runtime_logs", "request_logs"},
	})
}

func (h *ServiceHandler) UpdateRetention(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	current, err := getServiceRetention(serviceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load retention policy")
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&payload); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	runtimeDays := current.RuntimeLogsDays
	buildDays := current.BuildLogsDays
	requestDays := current.RequestLogsDays
	updated := false

	if v, ok, err := parseRetentionDaysField(payload, "runtime_logs", 1, 3650); err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	} else if ok {
		runtimeDays = v
		updated = true
	}
	if v, ok, err := parseRetentionDaysField(payload, "build_logs", 1, 3650); err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	} else if ok {
		buildDays = v
		updated = true
	}
	if v, ok, err := parseRetentionDaysField(payload, "request_logs", 1, 3650); err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	} else if ok {
		requestDays = v
		updated = true
	}

	if !updated {
		utils.RespondError(w, http.StatusBadRequest, "at least one retention field must be provided")
		return
	}

	if _, err := database.DB.Exec(
		`UPDATE services
		    SET runtime_log_retention_days=$1,
		        build_log_retention_days=$2,
		        request_log_retention_days=$3,
		        updated_at=NOW()
		  WHERE id=$4`,
		runtimeDays, buildDays, requestDays, serviceID,
	); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update retention policy")
		return
	}

	services.Audit(svc.WorkspaceID, userID, "service.retention_updated", "service", serviceID, map[string]interface{}{
		"runtime_logs_days": runtimeDays,
		"build_logs_days":   buildDays,
		"request_logs_days": requestDays,
	})

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":            "updated",
		"service_id":        serviceID,
		"runtime_logs":      formatRetentionDays(runtimeDays),
		"build_logs":        formatRetentionDays(buildDays),
		"request_logs":      formatRetentionDays(requestDays),
		"runtime_logs_days": runtimeDays,
		"build_logs_days":   buildDays,
		"request_logs_days": requestDays,
	})
}

func getServiceRetention(serviceID string) (serviceRetention, error) {
	ret := serviceRetention{}
	err := database.DB.QueryRow(
		`SELECT COALESCE(runtime_log_retention_days, $2),
		        COALESCE(build_log_retention_days, $3),
		        COALESCE(request_log_retention_days, $4)
		   FROM services
		  WHERE id=$1`,
		serviceID,
		defaultServiceRuntimeLogRetentionDays,
		defaultServiceBuildLogRetentionDays,
		defaultServiceRequestLogRetentionDays,
	).Scan(&ret.RuntimeLogsDays, &ret.BuildLogsDays, &ret.RequestLogsDays)
	if err != nil {
		return serviceRetention{}, err
	}
	return ret, nil
}
