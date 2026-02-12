package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type AutoscalingHandler struct{}

func NewAutoscalingHandler() *AutoscalingHandler {
	return &AutoscalingHandler{}
}

func (h *AutoscalingHandler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	serviceID := mux.Vars(r)["id"]
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	p, err := models.GetAutoscalingPolicy(serviceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load autoscaling policy")
		return
	}
	if p == nil {
		p = &models.AutoscalingPolicy{
			ServiceID:           serviceID,
			Enabled:             false,
			MinInstances:        1,
			MaxInstances:        svc.Instances,
			CPUTargetPercent:    70,
			MemoryTargetPercent: 75,
			ScaleOutCooldownSec: 120,
			ScaleInCooldownSec:  180,
		}
	}
	utils.RespondJSON(w, http.StatusOK, p)
}

func (h *AutoscalingHandler) UpsertPolicy(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	serviceID := mux.Vars(r)["id"]
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req models.AutoscalingPolicy
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ServiceID = serviceID
	if req.MinInstances < 1 {
		req.MinInstances = 1
	}
	if req.MaxInstances < req.MinInstances {
		req.MaxInstances = req.MinInstances
	}
	if req.CPUTargetPercent < 10 || req.CPUTargetPercent > 95 {
		req.CPUTargetPercent = 70
	}
	if req.MemoryTargetPercent < 10 || req.MemoryTargetPercent > 95 {
		req.MemoryTargetPercent = 75
	}
	if req.ScaleOutCooldownSec < 30 {
		req.ScaleOutCooldownSec = 30
	}
	if req.ScaleInCooldownSec < 30 {
		req.ScaleInCooldownSec = 30
	}
	if err := models.UpsertAutoscalingPolicy(&req); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to save autoscaling policy")
		return
	}
	services.Audit(svc.WorkspaceID, userID, "autoscaling.policy_updated", "service", svc.ID, map[string]interface{}{
		"enabled":                req.Enabled,
		"min_instances":          req.MinInstances,
		"max_instances":          req.MaxInstances,
		"cpu_target_percent":     req.CPUTargetPercent,
		"memory_target_percent":  req.MemoryTargetPercent,
		"scale_out_cooldown_sec": req.ScaleOutCooldownSec,
		"scale_in_cooldown_sec":  req.ScaleInCooldownSec,
	})
	utils.RespondJSON(w, http.StatusOK, req)
}
