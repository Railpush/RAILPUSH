package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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
		Name    string `json:"name"`
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Command = strings.TrimSpace(req.Command)
	if req.Command == "" {
		utils.RespondError(w, http.StatusBadRequest, "command is required")
		return
	}
	if req.Name == "" {
		req.Name = "One-off command"
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

	services.Audit(svc.WorkspaceID, userID, "job.created", "one_off_job", job.ID, map[string]interface{}{
		"service_id": svc.ID,
		"name":       req.Name,
		"command":    req.Command,
	})

	go func(jobID string, service *models.Service, command string) {
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
	}(job.ID, svc, req.Command)

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
