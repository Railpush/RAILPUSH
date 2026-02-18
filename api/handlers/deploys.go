package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type DeployHandler struct {
	Config *config.Config
	Worker *services.Worker
}

func NewDeployHandler(cfg *config.Config, worker *services.Worker) *DeployHandler {
	return &DeployHandler{Config: cfg, Worker: worker}
}

func (h *DeployHandler) TriggerDeploy(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		CommitSHA string `json:"commit_sha"`
		Branch    string `json:"branch"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Branch == "" {
		req.Branch = svc.Branch
	}
	deploy := &models.Deploy{
		ServiceID: serviceID,
		Trigger:   "manual",
		CommitSHA: req.CommitSHA,
		Branch:    req.Branch,
		CreatedBy: &userID,
	}
	if err := models.CreateDeploy(deploy); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create deploy: "+err.Error())
		return
	}

	// Look up user's GitHub token for private repo cloning
	var ghToken string
	if encToken, err := models.GetUserGitHubToken(userID); err == nil && encToken != "" {
		if t, err := utils.Decrypt(encToken, h.Config.Crypto.EncryptionKey); err == nil {
			ghToken = t
		} else {
			log.Printf("Warning: failed to decrypt GitHub token for user %s: %v", userID, err)
		}
	}

	// Enqueue the deploy job for the background worker
	h.Worker.Enqueue(services.DeployJob{
		Deploy:      deploy,
		Service:     svc,
		GitHubToken: ghToken,
	})
	services.Audit(svc.WorkspaceID, userID, "deploy.triggered", "deploy", deploy.ID, map[string]interface{}{
		"service_id": serviceID,
		"trigger":    "manual",
		"branch":     req.Branch,
		"commit_sha": req.CommitSHA,
	})

	utils.RespondJSON(w, http.StatusCreated, deploy)
}

func (h *DeployHandler) ListDeploys(w http.ResponseWriter, r *http.Request) {
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
	deploys, err := models.ListDeploys(serviceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list deploys: "+err.Error())
		return
	}
	if deploys == nil {
		deploys = []models.Deploy{}
	}
	utils.RespondJSON(w, http.StatusOK, deploys)
}

func (h *DeployHandler) GetDeploy(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	deployID := mux.Vars(r)["deployId"]
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
	deploy, err := models.GetDeploy(deployID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if deploy == nil {
		utils.RespondError(w, http.StatusNotFound, "deploy not found")
		return
	}
	if deploy.ServiceID != serviceID {
		utils.RespondError(w, http.StatusNotFound, "deploy not found")
		return
	}
	utils.RespondJSON(w, http.StatusOK, deploy)
}

func (h *DeployHandler) QueuePosition(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	deployID := mux.Vars(r)["deployId"]
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
	info, err := models.GetDeployQueuePosition(deployID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to get queue position")
		return
	}
	utils.RespondJSON(w, http.StatusOK, info)
}

func (h *DeployHandler) Rollback(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	deployID := mux.Vars(r)["deployId"]

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

	oldDeploy, err := models.GetDeploy(deployID)
	if err != nil || oldDeploy == nil {
		utils.RespondError(w, http.StatusNotFound, "deploy not found")
		return
	}
	if oldDeploy.ServiceID != serviceID {
		utils.RespondError(w, http.StatusNotFound, "deploy not found")
		return
	}

	newDeploy := &models.Deploy{
		ServiceID: serviceID,
		Trigger:   "rollback",
		CommitSHA: oldDeploy.CommitSHA,
		Branch:    oldDeploy.Branch,
		ImageTag:  oldDeploy.ImageTag,
		CreatedBy: &userID,
	}
	if err := models.CreateDeploy(newDeploy); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create rollback deploy")
		return
	}

	// Look up user's GitHub token for private repo cloning
	var ghTokenRollback string
	if encToken, err := models.GetUserGitHubToken(userID); err == nil && encToken != "" {
		if t, err := utils.Decrypt(encToken, h.Config.Crypto.EncryptionKey); err == nil {
			ghTokenRollback = t
		}
	}

	// Enqueue rollback deploy
	h.Worker.Enqueue(services.DeployJob{
		Deploy:      newDeploy,
		Service:     svc,
		GitHubToken: ghTokenRollback,
	})
	services.Audit(svc.WorkspaceID, userID, "deploy.rollback_triggered", "deploy", newDeploy.ID, map[string]interface{}{
		"service_id":  serviceID,
		"from_deploy": deployID,
		"commit_sha":  oldDeploy.CommitSHA,
	})

	utils.RespondJSON(w, http.StatusCreated, newDeploy)
}
