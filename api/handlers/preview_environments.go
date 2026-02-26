package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type PreviewEnvironmentHandler struct {
	Config *config.Config
	Worker *services.Worker
}

func NewPreviewEnvironmentHandler(cfg *config.Config, worker *services.Worker) *PreviewEnvironmentHandler {
	return &PreviewEnvironmentHandler{Config: cfg, Worker: worker}
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
	for i := range items {
		items[i].Repository = services.RedactRepoURLCredentials(items[i].Repository)
	}
	utils.RespondJSON(w, http.StatusOK, items)
}

func (h *PreviewEnvironmentHandler) CreatePreviewEnvironment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	var req struct {
		WorkspaceID      string `json:"workspace_id"`
		BaseServiceID    string `json:"base_service_id"`
		PRNumber         int    `json:"pr_number"`
		PRTitle          string `json:"pr_title"`
		PRBranch         string `json:"pr_branch"`
		BaseBranch       string `json:"base_branch"`
		CommitSHA        string `json:"commit_sha"`
		ServiceName      string `json:"service_name"`
		BuildCommand     string `json:"build_command"`
		StartCommand     string `json:"start_command"`
		PreDeployCommand string `json:"pre_deploy_command"`
		HealthCheckPath  string `json:"health_check_path"`
		DockerfilePath   string `json:"dockerfile_path"`
		DockerContext    string `json:"docker_context"`
		StaticPublish    string `json:"static_publish_path"`
		Port             int    `json:"port"`
		ImageURL         string `json:"image_url"`
		TriggerDeploy    *bool  `json:"trigger_deploy"`
		EnvVars          []struct {
			Key      string `json:"key"`
			Value    string `json:"value"`
			IsSecret bool   `json:"is_secret"`
		} `json:"env_vars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.BaseServiceID = strings.TrimSpace(req.BaseServiceID)
	req.PRBranch = strings.TrimSpace(req.PRBranch)
	req.BaseBranch = strings.TrimSpace(req.BaseBranch)
	req.CommitSHA = strings.TrimSpace(req.CommitSHA)
	req.PRTitle = strings.TrimSpace(req.PRTitle)
	req.ServiceName = strings.TrimSpace(req.ServiceName)

	if req.BaseServiceID == "" || req.PRNumber <= 0 || req.PRBranch == "" {
		utils.RespondError(w, http.StatusBadRequest, "base_service_id, pr_number and pr_branch are required")
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

	baseSvc, err := models.GetService(req.BaseServiceID)
	if err != nil || baseSvc == nil || baseSvc.WorkspaceID != workspaceID {
		utils.RespondError(w, http.StatusNotFound, "base service not found")
		return
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(baseSvc.Name)), "preview-") {
		utils.RespondError(w, http.StatusBadRequest, "base_service_id must reference a non-preview service")
		return
	}

	existing, _ := models.GetPreviewEnvironmentByRepoPR(workspaceID, baseSvc.RepoURL, req.PRNumber)
	var previewSvc *models.Service
	if existing != nil && existing.ServiceID != nil {
		previewSvc, _ = models.GetService(*existing.ServiceID)
	}

	if previewSvc == nil {
		previewName := req.ServiceName
		if previewName == "" {
			previewName = fmt.Sprintf("preview-%s-pr-%d", utils.ServiceHostLabel(baseSvc.Name, baseSvc.Subdomain), req.PRNumber)
		}
		candidate := &models.Service{
			WorkspaceID:       baseSvc.WorkspaceID,
			ProjectID:         baseSvc.ProjectID,
			EnvironmentID:     baseSvc.EnvironmentID,
			Name:              previewName,
			Type:              baseSvc.Type,
			Runtime:           baseSvc.Runtime,
			RepoURL:           baseSvc.RepoURL,
			Branch:            req.PRBranch,
			BuildCommand:      baseSvc.BuildCommand,
			StartCommand:      baseSvc.StartCommand,
			DockerfilePath:    baseSvc.DockerfilePath,
			DockerContext:     baseSvc.DockerContext,
			ImageURL:          baseSvc.ImageURL,
			HealthCheckPath:   baseSvc.HealthCheckPath,
			Port:              baseSvc.Port,
			AutoDeploy:        false,
			MaxShutdownDelay:  baseSvc.MaxShutdownDelay,
			PreDeployCommand:  baseSvc.PreDeployCommand,
			StaticPublishPath: baseSvc.StaticPublishPath,
			Schedule:          baseSvc.Schedule,
			Plan:              baseSvc.Plan,
			Instances:         1,
			DockerAccess:      baseSvc.DockerAccess,
		}
		if err := models.CreateService(candidate); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to create preview service")
			return
		}
		previewSvc = candidate

		if baseVars, err := models.ListEnvVars("service", baseSvc.ID); err == nil && len(baseVars) > 0 {
			vars := make([]models.EnvVar, 0, len(baseVars))
			for _, v := range baseVars {
				vars = append(vars, models.EnvVar{Key: v.Key, EncryptedValue: v.EncryptedValue, IsSecret: v.IsSecret})
			}
			_ = models.BulkUpsertEnvVars("service", candidate.ID, vars)
		}
	}

	if req.BuildCommand != "" {
		previewSvc.BuildCommand = req.BuildCommand
	}
	if req.StartCommand != "" {
		previewSvc.StartCommand = req.StartCommand
	}
	if req.PreDeployCommand != "" {
		previewSvc.PreDeployCommand = req.PreDeployCommand
	}
	if req.HealthCheckPath != "" {
		previewSvc.HealthCheckPath = req.HealthCheckPath
	}
	if req.DockerfilePath != "" {
		previewSvc.DockerfilePath = req.DockerfilePath
	}
	if req.DockerContext != "" {
		previewSvc.DockerContext = req.DockerContext
	}
	if req.StaticPublish != "" {
		previewSvc.StaticPublishPath = req.StaticPublish
	}
	if req.ImageURL != "" {
		previewSvc.ImageURL = req.ImageURL
	}
	if req.Port > 0 {
		previewSvc.Port = req.Port
	}
	previewSvc.Branch = req.PRBranch
	_ = models.UpdateService(previewSvc)

	if len(req.EnvVars) > 0 {
		vars := make([]models.EnvVar, 0, len(req.EnvVars))
		for _, v := range req.EnvVars {
			key := strings.TrimSpace(v.Key)
			if key == "" {
				continue
			}
			encrypted, err := utils.Encrypt(v.Value, h.Config.Crypto.EncryptionKey)
			if err != nil {
				utils.RespondError(w, http.StatusInternalServerError, "failed to encrypt env var value")
				return
			}
			vars = append(vars, models.EnvVar{Key: key, EncryptedValue: encrypted, IsSecret: v.IsSecret})
		}
		if len(vars) > 0 {
			if err := models.BulkUpsertEnvVars("service", previewSvc.ID, vars); err != nil {
				utils.RespondError(w, http.StatusInternalServerError, "failed to apply env var overrides")
				return
			}
		}
	}

	baseBranch := req.BaseBranch
	if baseBranch == "" {
		baseBranch = baseSvc.Branch
	}

	status := "ready"
	triggerDeploy := true
	if req.TriggerDeploy != nil {
		triggerDeploy = *req.TriggerDeploy
	}
	if triggerDeploy {
		status = "deploying"
	}

	pe := &models.PreviewEnvironment{
		WorkspaceID: workspaceID,
		ServiceID:   &previewSvc.ID,
		Repository:  services.RedactRepoURLCredentials(baseSvc.RepoURL),
		PRNumber:    req.PRNumber,
		PRTitle:     req.PRTitle,
		PRBranch:    req.PRBranch,
		BaseBranch:  baseBranch,
		CommitSHA:   req.CommitSHA,
		Status:      status,
	}
	if err := models.CreateOrUpdatePreviewEnvironment(pe); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create preview environment")
		return
	}

	if triggerDeploy {
		if err := h.enqueuePreviewDeploy(baseSvc.WorkspaceID, previewSvc, pe); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to trigger preview deploy")
			return
		}
	}

	services.Audit(workspaceID, userID, "preview_environment.created", "preview_environment", pe.ID, map[string]interface{}{
		"service_id": previewSvc.ID,
		"repository": services.RedactRepoURLCredentials(pe.Repository),
		"pr_number":  pe.PRNumber,
	})

	pe.Repository = services.RedactRepoURLCredentials(pe.Repository)
	utils.RespondJSON(w, http.StatusCreated, pe)
}

func (h *PreviewEnvironmentHandler) UpdatePreviewEnvironment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	previewID := strings.TrimSpace(mux.Vars(r)["id"])
	if previewID == "" {
		utils.RespondError(w, http.StatusBadRequest, "preview environment id is required")
		return
	}
	pe, err := models.GetPreviewEnvironment(previewID)
	if err != nil || pe == nil {
		utils.RespondError(w, http.StatusNotFound, "preview environment not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, pe.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		PRTitle          *string `json:"pr_title"`
		PRBranch         *string `json:"pr_branch"`
		BaseBranch       *string `json:"base_branch"`
		CommitSHA        *string `json:"commit_sha"`
		BuildCommand     *string `json:"build_command"`
		StartCommand     *string `json:"start_command"`
		PreDeployCommand *string `json:"pre_deploy_command"`
		HealthCheckPath  *string `json:"health_check_path"`
		DockerfilePath   *string `json:"dockerfile_path"`
		DockerContext    *string `json:"docker_context"`
		StaticPublish    *string `json:"static_publish_path"`
		Port             *int    `json:"port"`
		ImageURL         *string `json:"image_url"`
		TriggerDeploy    bool    `json:"trigger_deploy"`
		EnvVars          []struct {
			Key      string `json:"key"`
			Value    string `json:"value"`
			IsSecret bool   `json:"is_secret"`
		} `json:"env_vars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var previewSvc *models.Service
	if pe.ServiceID != nil {
		previewSvc, _ = models.GetService(*pe.ServiceID)
	}
	if previewSvc == nil {
		utils.RespondError(w, http.StatusNotFound, "preview service not found")
		return
	}

	if req.PRTitle != nil {
		pe.PRTitle = strings.TrimSpace(*req.PRTitle)
	}
	if req.PRBranch != nil {
		b := strings.TrimSpace(*req.PRBranch)
		if b != "" {
			pe.PRBranch = b
			previewSvc.Branch = b
		}
	}
	if req.BaseBranch != nil {
		pe.BaseBranch = strings.TrimSpace(*req.BaseBranch)
	}
	if req.CommitSHA != nil {
		pe.CommitSHA = strings.TrimSpace(*req.CommitSHA)
	}
	if req.BuildCommand != nil {
		previewSvc.BuildCommand = strings.TrimSpace(*req.BuildCommand)
	}
	if req.StartCommand != nil {
		previewSvc.StartCommand = strings.TrimSpace(*req.StartCommand)
	}
	if req.PreDeployCommand != nil {
		previewSvc.PreDeployCommand = strings.TrimSpace(*req.PreDeployCommand)
	}
	if req.HealthCheckPath != nil {
		previewSvc.HealthCheckPath = strings.TrimSpace(*req.HealthCheckPath)
	}
	if req.DockerfilePath != nil {
		previewSvc.DockerfilePath = strings.TrimSpace(*req.DockerfilePath)
	}
	if req.DockerContext != nil {
		previewSvc.DockerContext = strings.TrimSpace(*req.DockerContext)
	}
	if req.StaticPublish != nil {
		previewSvc.StaticPublishPath = strings.TrimSpace(*req.StaticPublish)
	}
	if req.ImageURL != nil {
		previewSvc.ImageURL = strings.TrimSpace(*req.ImageURL)
	}
	if req.Port != nil && *req.Port > 0 {
		previewSvc.Port = *req.Port
	}

	if err := models.UpdateService(previewSvc); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update preview service")
		return
	}

	if len(req.EnvVars) > 0 {
		vars := make([]models.EnvVar, 0, len(req.EnvVars))
		for _, v := range req.EnvVars {
			key := strings.TrimSpace(v.Key)
			if key == "" {
				continue
			}
			encrypted, err := utils.Encrypt(v.Value, h.Config.Crypto.EncryptionKey)
			if err != nil {
				utils.RespondError(w, http.StatusInternalServerError, "failed to encrypt env var value")
				return
			}
			vars = append(vars, models.EnvVar{Key: key, EncryptedValue: encrypted, IsSecret: v.IsSecret})
		}
		if len(vars) > 0 {
			if err := models.BulkUpsertEnvVars("service", previewSvc.ID, vars); err != nil {
				utils.RespondError(w, http.StatusInternalServerError, "failed to apply env var overrides")
				return
			}
		}
	}

	if req.TriggerDeploy {
		pe.Status = "deploying"
		if err := h.enqueuePreviewDeploy(pe.WorkspaceID, previewSvc, pe); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to trigger preview deploy")
			return
		}
	}

	if err := models.CreateOrUpdatePreviewEnvironment(pe); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update preview environment")
		return
	}

	services.Audit(pe.WorkspaceID, userID, "preview_environment.updated", "preview_environment", pe.ID, map[string]interface{}{
		"service_id": previewSvc.ID,
	})

	pe.Repository = services.RedactRepoURLCredentials(pe.Repository)
	utils.RespondJSON(w, http.StatusOK, pe)
}

func (h *PreviewEnvironmentHandler) DeletePreviewEnvironment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	previewID := strings.TrimSpace(mux.Vars(r)["id"])
	if previewID == "" {
		utils.RespondError(w, http.StatusBadRequest, "preview environment id is required")
		return
	}
	pe, err := models.GetPreviewEnvironment(previewID)
	if err != nil || pe == nil {
		utils.RespondError(w, http.StatusNotFound, "preview environment not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, pe.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	if pe.ServiceID != nil {
		if previewSvc, _ := models.GetService(*pe.ServiceID); previewSvc != nil {
			_ = h.cleanupPreviewService(previewSvc)
			_ = models.DeleteService(previewSvc.ID)
		}
	}

	if err := models.MarkPreviewEnvironmentClosedByID(pe.ID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to close preview environment")
		return
	}

	services.Audit(pe.WorkspaceID, userID, "preview_environment.deleted", "preview_environment", pe.ID, map[string]interface{}{
		"service_id": pe.ServiceID,
	})

	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *PreviewEnvironmentHandler) enqueuePreviewDeploy(workspaceID string, svc *models.Service, pe *models.PreviewEnvironment) error {
	if h == nil || h.Worker == nil || svc == nil || pe == nil {
		return fmt.Errorf("missing preview deploy dependencies")
	}
	if strings.TrimSpace(pe.CommitSHA) == "" {
		pe.CommitSHA = "manual"
	}
	deploy := &models.Deploy{
		ServiceID:     svc.ID,
		Trigger:       "preview",
		CommitSHA:     pe.CommitSHA,
		CommitMessage: fmt.Sprintf("Preview deploy for PR #%d", pe.PRNumber),
		Branch:        strings.TrimSpace(pe.PRBranch),
	}
	if deploy.Branch == "" {
		deploy.Branch = svc.Branch
	}
	if err := models.CreateDeploy(deploy); err != nil {
		return err
	}

	var ghToken string
	if ws, err := models.GetWorkspace(workspaceID); err == nil && ws != nil {
		if encToken, err := models.GetUserGitHubToken(ws.OwnerID); err == nil && encToken != "" {
			if t, err := utils.Decrypt(encToken, h.Config.Crypto.EncryptionKey); err == nil {
				ghToken = t
			}
		}
	}
	svcCopy := *svc
	h.Worker.Enqueue(services.DeployJob{
		Deploy:      deploy,
		Service:     &svcCopy,
		GitHubToken: ghToken,
	})
	return nil
}

func (h *PreviewEnvironmentHandler) cleanupPreviewService(svc *models.Service) error {
	if svc == nil {
		return nil
	}
	if h != nil && h.Config != nil && h.Config.Kubernetes.Enabled {
		if h.Worker != nil {
			if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
				_ = kd.DeleteServiceResources(svc)
			}
		}
	} else if h != nil && h.Worker != nil {
		if svc.ContainerID != "" {
			h.Worker.Deployer.RemoveContainer(svc.ContainerID)
		}
		if instances, err := models.ListServiceInstances(svc.ID); err == nil {
			for _, inst := range instances {
				if inst.ContainerID != "" {
					_ = h.Worker.Deployer.RemoveContainer(inst.ContainerID)
				}
			}
		}
		_ = models.DeleteServiceInstancesByService(svc.ID)
		if h.Config != nil && h.Worker.Router != nil && h.Config.Deploy.Domain != "" && h.Config.Deploy.Domain != "localhost" && !h.Config.Deploy.DisableRouter {
			domain := utils.ServiceHostLabel(svc.Name, svc.Subdomain) + "." + h.Config.Deploy.Domain
			h.Worker.Router.RemoveRoute(domain)
		}
	}
	_ = models.DeleteServiceInstancesByService(svc.ID)
	return nil
}
