package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type OneOffJobHandler struct {
	Worker   *services.Worker
	Executor *services.OneOffExecutor
}

func NewOneOffJobHandler(worker *services.Worker) *OneOffJobHandler {
	return &OneOffJobHandler{
		Worker:   worker,
		Executor: services.NewOneOffExecutor(worker.Deployer),
	}
}

func (h *OneOffJobHandler) RunServiceJob(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Name                   string `json:"name"`
		Command                string `json:"command"`
		AcknowledgeRiskyCommand bool   `json:"acknowledge_risky_command"`
		Reason                 string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Command = strings.TrimSpace(req.Command)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Command == "" {
		utils.RespondError(w, http.StatusBadRequest, "command is required")
		return
	}
	if req.Name == "" {
		req.Name = "One-off command"
	}

	commandPolicy := services.EvaluateOneOffCommand(req.Command)
	apiKeyID := middleware.GetAPIKeyID(r)
	apiKeyScopes := middleware.GetAPIKeyScopes(r)
	clientIP := middleware.ClientIPString(r)
	commandPreview := services.OneOffCommandPreview(req.Command, 180)
	commandSHA := services.OneOffCommandSHA256(req.Command)

	auditDetails := map[string]interface{}{
		"service_id":       svc.ID,
		"name":             req.Name,
		"command_preview":  commandPreview,
		"command_sha256":   commandSHA,
		"command_length":   len(req.Command),
		"risky":            commandPolicy.Risky,
		"risk_reasons":     commandPolicy.RiskReasons,
		"acknowledged_risk": req.AcknowledgeRiskyCommand,
		"reason":           req.Reason,
		"api_key_id":       apiKeyID,
		"api_key_scopes":   apiKeyScopes,
		"client_ip":        clientIP,
		"user_agent":       strings.TrimSpace(r.UserAgent()),
	}

	if commandPolicy.Blocked {
		auditDetails["blocked_reasons"] = commandPolicy.BlockReasons
		services.Audit(svc.WorkspaceID, userID, "job.denied", "one_off_job", "", auditDetails)
		utils.RespondJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":           "command blocked by run_job policy",
			"blocked_reasons": commandPolicy.BlockReasons,
		})
		return
	}

	if commandPolicy.Risky && apiKeyID != "" && !models.HasAnyAPIKeyScope(apiKeyScopes, models.APIKeyScopeAdmin) {
		auditDetails["blocked_reasons"] = []string{"risky commands via API key require admin scope"}
		services.Audit(svc.WorkspaceID, userID, "job.denied", "one_off_job", "", auditDetails)
		utils.RespondJSON(w, http.StatusForbidden, map[string]interface{}{
			"error":        "risky run_job command denied for API key",
			"risk_reasons": commandPolicy.RiskReasons,
		})
		return
	}

	if commandPolicy.Risky && !req.AcknowledgeRiskyCommand {
		services.Audit(svc.WorkspaceID, userID, "job.denied", "one_off_job", "", auditDetails)
		utils.RespondJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":        "risky run_job command requires acknowledge_risky_command=true",
			"risk_reasons": commandPolicy.RiskReasons,
		})
		return
	}

	job := &models.OneOffJob{
		WorkspaceID: svc.WorkspaceID,
		ServiceID:   &svc.ID,
		Name:        req.Name,
		Command:     req.Command,
		Status:      "pending",
		CreatedBy:   &userID,
	}
	if err := models.CreateOneOffJob(job); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create job")
		return
	}

	auditDetails["job_id"] = job.ID
	services.Audit(svc.WorkspaceID, userID, "job.created", "one_off_job", job.ID, auditDetails)

	go func(jobID string, service *models.Service, command string, risky bool, riskReasons []string) {
		started := time.Now()
		_ = models.MarkOneOffJobRunning(jobID)
		out := ""
		exitCode := 0
		var err error
		if h.Worker != nil && h.Worker.Config != nil && h.Worker.Config.Kubernetes.Enabled {
			kd, kerr := h.Worker.GetKubeDeployer()
			if kerr != nil || kd == nil {
				out = "failed to initialize kubernetes client"
				exitCode = 1
				err = fmt.Errorf("kubernetes client: %v", kerr)
			} else {
				out, exitCode, err = kd.RunOneOffJob(jobID, service, command)
			}
		} else {
			out, exitCode, err = h.Executor.RunForService(service, command)
		}
		status := "succeeded"
		if err != nil || exitCode != 0 {
			status = "failed"
		}
		_ = models.CompleteOneOffJob(jobID, status, out, exitCode)
		services.Audit(service.WorkspaceID, userID, "job.completed", "one_off_job", jobID, map[string]interface{}{
			"service_id":      service.ID,
			"status":          status,
			"exit_code":       exitCode,
			"duration_ms":     time.Since(started).Milliseconds(),
			"command_sha256":  services.OneOffCommandSHA256(command),
			"command_preview": services.OneOffCommandPreview(command, 180),
			"risky":           risky,
			"risk_reasons":    riskReasons,
			"had_error":       err != nil,
		})
	}(job.ID, svc, req.Command, commandPolicy.Risky, commandPolicy.RiskReasons)

	utils.RespondJSON(w, http.StatusCreated, job)
}

func (h *OneOffJobHandler) ListServiceJobs(w http.ResponseWriter, r *http.Request) {
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
	pagination, err := parseCursorPagination(r)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if pagination.Enabled {
		total, err := models.CountOneOffJobsByService(serviceID)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to count jobs")
			return
		}
		items, err := models.ListOneOffJobsByServicePage(serviceID, pagination.Limit, pagination.Offset)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to list jobs")
			return
		}
		if items == nil {
			items = []models.OneOffJob{}
		}
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"data":       items,
			"pagination": paginateWindowMeta(total, pagination, len(items)),
		})
		return
	}

	items, err := models.ListOneOffJobsByService(serviceID, 100)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}
	if items == nil {
		items = []models.OneOffJob{}
	}
	utils.RespondJSON(w, http.StatusOK, items)
}

func (h *OneOffJobHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	jobID := mux.Vars(r)["jobId"]
	job, err := models.GetOneOffJob(jobID)
	if err != nil || job == nil {
		utils.RespondError(w, http.StatusNotFound, "job not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, job.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	utils.RespondJSON(w, http.StatusOK, job)
}
