package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type ServiceHandler struct {
	Config *config.Config
	Worker *services.Worker
	Stripe *services.StripeService
}

func NewServiceHandler(cfg *config.Config, worker *services.Worker, stripe *services.StripeService) *ServiceHandler {
	return &ServiceHandler{Config: cfg, Worker: worker, Stripe: stripe}
}

func (h *ServiceHandler) ensureServiceDomainLabelAvailable(workspaceID, currentServiceID, desiredName string) error {
	desired := utils.ServiceDomainLabel(desiredName)
	if desired == "" {
		return fmt.Errorf("invalid service name")
	}
	svcs, err := models.ListServices(workspaceID)
	if err != nil {
		return err
	}
	for _, s := range svcs {
		if s.ID == currentServiceID {
			continue
		}
		if utils.ServiceDomainLabel(s.Name) == desired {
			if strings.TrimSpace(h.Config.Deploy.Domain) != "" && h.Config.Deploy.Domain != "localhost" {
				return fmt.Errorf("service name conflicts with existing subdomain: %s.%s", desired, h.Config.Deploy.Domain)
			}
			return fmt.Errorf("service name conflicts with an existing service")
		}
	}
	return nil
}

func (h *ServiceHandler) decorateServicePublicURL(svc *models.Service) {
	if svc == nil {
		return
	}
	svc.PublicURL = utils.ServicePublicURL(svc.Type, svc.Name, svc.Subdomain, h.Config.Deploy.Domain, svc.HostPort)
}

func (h *ServiceHandler) ensureAccess(w http.ResponseWriter, userID, workspaceID, minRole string) bool {
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, minRole); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func (h *ServiceHandler) isProtectedEnvironment(environmentID *string) bool {
	if environmentID == nil || *environmentID == "" {
		return false
	}
	env, err := models.GetEnvironment(*environmentID)
	if err != nil || env == nil {
		return false
	}
	return env.IsProtected
}

func (h *ServiceHandler) ListServices(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	wsID := r.URL.Query().Get("workspace_id")
	if wsID == "" {
		if ws, err := models.GetWorkspaceByOwner(userID); err == nil && ws != nil {
			wsID = ws.ID
		}
	}
	if wsID == "" {
		utils.RespondJSON(w, http.StatusOK, []models.Service{})
		return
	}
	if !h.ensureAccess(w, userID, wsID, models.RoleViewer) {
		return
	}
	svcs, err := models.ListServices(wsID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list services: "+err.Error())
		return
	}
	if svcs == nil {
		svcs = []models.Service{}
	}
	for i := range svcs {
		h.decorateServicePublicURL(&svcs[i])
	}
	utils.RespondJSON(w, http.StatusOK, svcs)
}

func (h *ServiceHandler) CreateService(w http.ResponseWriter, r *http.Request) {
	var svc models.Service
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := json.Unmarshal(body, &svc); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Default: auto-deploy should be ON unless explicitly disabled.
	// We use a pointer here to distinguish "missing" from "false".
	var req struct {
		AutoDeploy *bool `json:"auto_deploy"`
	}
	_ = json.Unmarshal(body, &req)
	if req.AutoDeploy == nil {
		svc.AutoDeploy = true
	}
	svc.Name = strings.TrimSpace(svc.Name)
	svc.Type = strings.TrimSpace(svc.Type)
	if svc.Name == "" || svc.Type == "" {
		utils.RespondError(w, http.StatusBadRequest, "name and type are required")
		return
	}
	userID := middleware.GetUserID(r)
	if svc.WorkspaceID == "" {
		ws, err := models.GetWorkspaceByOwner(userID)
		if err != nil || ws == nil {
			utils.RespondError(w, http.StatusBadRequest, "no workspace found for user")
			return
		}
		svc.WorkspaceID = ws.ID
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}

	if svc.ProjectID != nil && *svc.ProjectID != "" {
		project, err := models.GetProject(*svc.ProjectID)
		if err != nil || project == nil || project.WorkspaceID != svc.WorkspaceID {
			utils.RespondError(w, http.StatusBadRequest, "invalid project_id")
			return
		}
	}
	if svc.EnvironmentID != nil && *svc.EnvironmentID != "" {
		env, err := models.GetEnvironment(*svc.EnvironmentID)
		if err != nil || env == nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid environment_id")
			return
		}
		if svc.ProjectID != nil && *svc.ProjectID != "" && env.ProjectID != *svc.ProjectID {
			utils.RespondError(w, http.StatusBadRequest, "environment does not belong to project")
			return
		}
		if env.IsProtected && !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleAdmin) {
			return
		}
	}
	if svc.Port == 0 {
		svc.Port = 10000
	}
	if svc.Plan == "" {
		svc.Plan = services.PlanStarter
	}
	if p, ok := services.NormalizePlan(svc.Plan); ok {
		svc.Plan = p
	} else {
		utils.RespondError(w, http.StatusBadRequest, "invalid plan")
		return
	}
	if svc.Instances == 0 {
		svc.Instances = 1
	}
	if svc.Branch == "" {
		svc.Branch = "main"
	}

	// Free tier: limit 1 free service per workspace
	if svc.Plan == "free" {
		count, err := models.CountResourcesByWorkspaceAndPlan(svc.WorkspaceID, "service", "free")
		if err == nil && count >= 1 {
			utils.RespondError(w, http.StatusBadRequest, "free tier limit reached: 1 free service per workspace")
			return
		}
	}

	// Paid plan: ensure Stripe customer exists and has payment method
	if svc.Plan != "free" && h.Stripe.Enabled() {
		user, err := models.GetUserByID(userID)
		if err != nil || user == nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to get user")
			return
		}
		bc, err := h.Stripe.EnsureCustomer(userID, user.Email)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "billing error: "+err.Error())
			return
		}
		_ = bc // Payment method is optional when workspace credits cover the charge.
	}

	if err := models.CreateService(&svc); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create service: "+err.Error())
		return
	}

	// Add to Stripe subscription for paid plans
	if svc.Plan != "free" && h.Stripe.Enabled() {
		user, _ := models.GetUserByID(userID)
		if user != nil {
			bc, _ := models.GetBillingCustomerByUserID(userID)
			if bc != nil {
				if err := h.Stripe.AddSubscriptionItem(bc, svc.WorkspaceID, "service", svc.ID, svc.Name, svc.Plan); err != nil {
					log.Printf("Warning: failed to add billing for service %s: %v", svc.ID, err)
					// Rollback: delete the service
					models.DeleteService(svc.ID)
					if errors.Is(err, services.ErrNoDefaultPaymentMethod) {
						utils.RespondError(w, http.StatusPaymentRequired, "payment method required. Please add a default payment method in billing settings.")
						return
					}
					utils.RespondError(w, http.StatusInternalServerError, "billing error: "+err.Error())
					return
				}
			}
		}
	}

	// Auto-register GitHub webhook if repo is on GitHub and user has a token
	if strings.Contains(svc.RepoURL, "github.com") {
		owner, repo := ParseGitHubOwnerRepo(svc.RepoURL)
		if owner != "" && repo != "" {
			if encToken, err := models.GetUserGitHubToken(userID); err == nil && encToken != "" {
				if ghToken, err := utils.Decrypt(encToken, h.Config.Crypto.EncryptionKey); err == nil {
					gh := services.NewGitHub(h.Config)
					webhookURL := "https://" + h.Config.ControlPlane.Domain + "/api/v1/webhooks/github"
					if err := gh.CreateWebhook(ghToken, owner, repo, webhookURL, h.Config.GitHub.WebhookSecret); err != nil {
						log.Printf("Warning: failed to auto-register webhook for %s/%s: %v", owner, repo, err)
					}
				}
			}
		}
	}

	services.Audit(svc.WorkspaceID, userID, "service.created", "service", svc.ID, map[string]interface{}{
		"name":           svc.Name,
		"type":           svc.Type,
		"plan":           svc.Plan,
		"project_id":     svc.ProjectID,
		"environment_id": svc.EnvironmentID,
	})
	h.decorateServicePublicURL(&svc)
	utils.RespondJSON(w, http.StatusCreated, svc)
}

func (h *ServiceHandler) GetService(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleViewer) {
		return
	}
	h.decorateServicePublicURL(svc)
	utils.RespondJSON(w, http.StatusOK, svc)
}

func (h *ServiceHandler) UpdateService(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}
	if h.isProtectedEnvironment(svc.EnvironmentID) && !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleAdmin) {
		return
	}
	oldInstances := svc.Instances
	oldPlanRaw := svc.Plan
	oldPlanEffective := strings.ToLower(strings.TrimSpace(oldPlanRaw))
	if p, ok := services.NormalizePlan(oldPlanRaw); ok {
		oldPlanEffective = p
	}
	planProvided := false
	desiredPlan := oldPlanEffective
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if v, ok := updates["name"].(string); ok {
		v = strings.TrimSpace(v)
		if v == "" {
			utils.RespondError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		svc.Name = v
	}
	if v, ok := updates["branch"].(string); ok {
		svc.Branch = v
	}
	if v, ok := updates["build_command"].(string); ok {
		svc.BuildCommand = v
	}
	if v, ok := updates["start_command"].(string); ok {
		svc.StartCommand = v
	}
	if v, ok := updates["port"].(float64); ok {
		svc.Port = int(v)
	}
	if v, ok := updates["auto_deploy"].(bool); ok {
		svc.AutoDeploy = v
	}
	if v, ok := updates["docker_access"].(bool); ok {
		svc.DockerAccess = v
	}
	if v, ok := updates["plan"].(string); ok {
		planProvided = true
		if p, ok := services.NormalizePlan(v); ok {
			desiredPlan = p
		} else {
			utils.RespondError(w, http.StatusBadRequest, "invalid plan")
			return
		}
	}
	if v, ok := updates["instances"].(float64); ok {
		svc.Instances = int(v)
		if svc.Instances < 1 {
			svc.Instances = 1
		}
	}
	if v, ok := updates["dockerfile_path"].(string); ok {
		svc.DockerfilePath = v
	}
	if v, ok := updates["docker_context"].(string); ok {
		svc.DockerContext = v
	}
	if v, ok := updates["image_url"].(string); ok {
		svc.ImageURL = v
	}
	if v, ok := updates["health_check_path"].(string); ok {
		svc.HealthCheckPath = v
	}
	if v, ok := updates["pre_deploy_command"].(string); ok {
		svc.PreDeployCommand = v
	}
	if v, ok := updates["static_publish_path"].(string); ok {
		svc.StaticPublishPath = v
	}
	if v, ok := updates["schedule"].(string); ok {
		svc.Schedule = v
	}
	if v, ok := updates["max_shutdown_delay"].(float64); ok {
		svc.MaxShutdownDelay = int(v)
	}
	if raw, ok := updates["project_id"]; ok {
		if raw == nil {
			svc.ProjectID = nil
		} else if v, ok := raw.(string); ok {
			v = strings.TrimSpace(v)
			if v == "" {
				svc.ProjectID = nil
			} else {
				project, err := models.GetProject(v)
				if err != nil || project == nil || project.WorkspaceID != svc.WorkspaceID {
					utils.RespondError(w, http.StatusBadRequest, "invalid project_id")
					return
				}
				svc.ProjectID = &v
			}
		}
	}
	if raw, ok := updates["environment_id"]; ok {
		if raw == nil {
			svc.EnvironmentID = nil
		} else if v, ok := raw.(string); ok {
			v = strings.TrimSpace(v)
			if v == "" {
				svc.EnvironmentID = nil
			} else {
				env, err := models.GetEnvironment(v)
				if err != nil || env == nil {
					utils.RespondError(w, http.StatusBadRequest, "invalid environment_id")
					return
				}
				if svc.ProjectID != nil && *svc.ProjectID != "" && env.ProjectID != *svc.ProjectID {
					utils.RespondError(w, http.StatusBadRequest, "environment does not belong to project")
					return
				}
				if env.IsProtected && !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleAdmin) {
					return
				}
				svc.EnvironmentID = &v
			}
		}
	}
	if svc.Instances < 1 {
		svc.Instances = 1
	}

	newPlanEffective := oldPlanEffective
	if planProvided {
		newPlanEffective = desiredPlan
	}

	// Gate plan changes on Stripe success so users cannot upgrade resources without billing.
	if planProvided && newPlanEffective != oldPlanEffective && h.Stripe.Enabled() {
		if newPlanEffective == services.PlanFree {
			if err := h.Stripe.RemoveSubscriptionItem("service", svc.ID); err != nil {
				utils.RespondError(w, http.StatusBadGateway, "billing error: "+err.Error())
				return
			}
		} else {
			user, err := models.GetUserByID(userID)
			if err != nil || user == nil {
				utils.RespondError(w, http.StatusInternalServerError, "failed to get user")
				return
			}
			bc, err := h.Stripe.EnsureCustomer(userID, user.Email)
			if err != nil || bc == nil {
				if err == nil {
					err = fmt.Errorf("billing customer not found")
				}
				utils.RespondError(w, http.StatusBadGateway, "billing error: "+err.Error())
				return
			}
			if err := h.Stripe.AddSubscriptionItem(bc, svc.WorkspaceID, "service", svc.ID, svc.Name, newPlanEffective); err != nil {
				if errors.Is(err, services.ErrNoDefaultPaymentMethod) {
					utils.RespondError(w, http.StatusPaymentRequired, "payment method required. Please add a default payment method in billing settings.")
					return
				}
				utils.RespondError(w, http.StatusBadGateway, "billing error: "+err.Error())
				return
			}
		}
	}

	if planProvided {
		svc.Plan = newPlanEffective
	}
	if err := models.UpdateService(svc); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update service")
		return
	}

	// Best-effort: apply scaling/resource changes immediately for Kubernetes runtimes.
	// This improves UX for the Scaling page without requiring a full "deploy".
	if h.Config != nil && h.Config.Kubernetes.Enabled {
		svcType := strings.ToLower(strings.TrimSpace(svc.Type))
		isKubeDeployed := strings.HasPrefix(strings.TrimSpace(svc.ContainerID), "k8s:")
		if isKubeDeployed && svcType != "cron" && svcType != "cron_job" {
			if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
				if planProvided && newPlanEffective != oldPlanEffective {
					if err := kd.UpdateServiceDeploymentResources(svc); err != nil {
						log.Printf("WARNING: k8s update resources failed service=%s: %v", svc.ID, err)
					}
				}
				if oldInstances != svc.Instances && !svc.IsSuspended {
					desired := int32(1)
					if svc.Instances > 0 {
						desired = int32(svc.Instances)
					}
					if err := kd.ScaleService(svc, desired); err != nil {
						log.Printf("WARNING: k8s scale failed service=%s desired=%d: %v", svc.ID, desired, err)
					}
				}
			}
		}
	}

	services.Audit(svc.WorkspaceID, userID, "service.updated", "service", svc.ID, map[string]interface{}{
		"name":           svc.Name,
		"plan":           svc.Plan,
		"instances":      svc.Instances,
		"project_id":     svc.ProjectID,
		"environment_id": svc.EnvironmentID,
	})
	h.decorateServicePublicURL(svc)
	utils.RespondJSON(w, http.StatusOK, svc)
}

// DeleteService stops the Docker container and removes the Caddy route before deleting
func (h *ServiceHandler) DeleteService(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to look up service")
		return
	}
	if svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}
	if h.isProtectedEnvironment(svc.EnvironmentID) && !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleAdmin) {
		return
	}

	// Remove from Stripe subscription before deleting
	if svc.Plan != "free" && h.Stripe.Enabled() {
		if err := h.Stripe.RemoveSubscriptionItem("service", id); err != nil {
			log.Printf("Warning: failed to remove billing for service %s: %v", id, err)
		}
	}

	// Stop and remove Docker container
	if h.Config.Kubernetes.Enabled {
		if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
			_ = kd.DeleteServiceResources(svc)
		}
	} else {
		if svc.ContainerID != "" {
			h.Worker.Deployer.RemoveContainer(svc.ContainerID)
		}
		if instances, err := models.ListServiceInstances(id); err == nil {
			for _, inst := range instances {
				if inst.ContainerID != "" {
					_ = h.Worker.Deployer.RemoveContainer(inst.ContainerID)
				}
			}
		}
		_ = models.DeleteServiceInstancesByService(id)
		// Remove Caddy route
		if h.Config.Deploy.Domain != "" && h.Config.Deploy.Domain != "localhost" && !h.Config.Deploy.DisableRouter {
			domain := utils.ServiceHostLabel(svc.Name, svc.Subdomain) + "." + h.Config.Deploy.Domain
			h.Worker.Router.RemoveRoute(domain)
		}
	}
	// Remove any blueprint links to this service to avoid stale resources in blueprint UIs.
	_ = models.DeleteBlueprintResourcesByResource("service", id)
	if err := models.DeleteService(id); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete service")
		return
	}
	services.Audit(svc.WorkspaceID, userID, "service.deleted", "service", id, map[string]interface{}{
		"name": svc.Name,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// RestartService does docker restart on the container
func (h *ServiceHandler) RestartService(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}
	if h.isProtectedEnvironment(svc.EnvironmentID) && !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleAdmin) {
		return
	}
	models.UpdateServiceStatus(id, "restarting", svc.ContainerID, svc.HostPort)
	go func() {
		if h.Config.Kubernetes.Enabled {
			if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
				if err := kd.RestartService(svc); err != nil {
					models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
					return
				}
				models.UpdateServiceStatus(id, "live", svc.ContainerID, svc.HostPort)
				return
			}
			models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
			return
		}

		if svc.ContainerID == "" {
			models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
			return
		}
		if err := h.Worker.Deployer.RestartContainer(svc.ContainerID); err != nil {
			models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
			return
		}
		models.UpdateServiceStatus(id, "live", svc.ContainerID, svc.HostPort)
	}()
	services.Audit(svc.WorkspaceID, userID, "service.restarted", "service", id, nil)
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "restarting"})
}

// SuspendService does docker stop on the container
func (h *ServiceHandler) SuspendService(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}
	if h.isProtectedEnvironment(svc.EnvironmentID) && !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleAdmin) {
		return
	}
	if h.Config.Kubernetes.Enabled {
		if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
			_ = kd.ScaleService(svc, 0)
		}
	} else {
		if svc.ContainerID != "" {
			h.Worker.Deployer.StopContainer(svc.ContainerID)
		}
	}
	// Set is_suspended flag
	models.SetServiceSuspended(id, true)
	models.UpdateServiceStatus(id, "suspended", svc.ContainerID, svc.HostPort)
	// Record usage stop for metered billing (stops accruing per-minute charges).
	if models.IsBillingItemMetered("service", id) {
		_ = models.RecordUsageEvent("service", id, "stop")
	}
	services.Audit(svc.WorkspaceID, userID, "service.suspended", "service", id, nil)
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "suspended"})
}

// ResumeService does docker start on the stopped container
func (h *ServiceHandler) ResumeService(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}
	if h.isProtectedEnvironment(svc.EnvironmentID) && !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleAdmin) {
		return
	}
	models.SetServiceSuspended(id, false)
	models.UpdateServiceStatus(id, "deploying", svc.ContainerID, svc.HostPort)
	// Record usage start for metered billing (resumes per-minute charges).
	if models.IsBillingItemMetered("service", id) {
		_ = models.RecordUsageEvent("service", id, "start")
	}
	go func() {
		if h.Config.Kubernetes.Enabled {
			desired := int32(1)
			if svc.Instances > 0 {
				desired = int32(svc.Instances)
			}
			if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
				if err := kd.ScaleService(svc, desired); err != nil {
					models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
					return
				}
				models.UpdateServiceStatus(id, "live", svc.ContainerID, svc.HostPort)
				return
			}
			models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
			return
		}

		if svc.ContainerID == "" {
			models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
			return
		}
		if err := h.Worker.Deployer.StartContainer(svc.ContainerID); err != nil {
			models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
			return
		}
		models.UpdateServiceStatus(id, "live", svc.ContainerID, svc.HostPort)
	}()
	services.Audit(svc.WorkspaceID, userID, "service.resumed", "service", id, nil)
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deploying"})
}
