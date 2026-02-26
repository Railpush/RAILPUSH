package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

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

var requiredGitHubWebhookEvents = []string{"push", "workflow_run"}

var supportedServiceEventWebhookEvents = []string{
	"deploy.started",
	"deploy.success",
	"deploy.failed",
	"deploy.rollback",
}

const (
	serviceEventWebhookURLKey    = "RAILPUSH_EVENT_WEBHOOK_URL"
	serviceEventWebhookEventsKey = "RAILPUSH_EVENT_WEBHOOK_EVENTS"
	serviceEventWebhookSecretKey = "RAILPUSH_EVENT_WEBHOOK_SECRET"

	legacyDeployWebhookURLKey       = "DEPLOY_WEBHOOK_URL"
	legacyAltDeployWebhookURLKey    = "RAILPUSH_DEPLOY_WEBHOOK_URL"
	legacyDeployWebhookEventsKey    = "DEPLOY_WEBHOOK_EVENTS"
	legacyDeployWebhookSecretKey    = "DEPLOY_WEBHOOK_SECRET"
	legacyAltDeployWebhookSecretKey = "RAILPUSH_DEPLOY_WEBHOOK_SECRET"
)

type ServiceEventWebhookConfig struct {
	Enabled         bool     `json:"enabled"`
	URL             string   `json:"url,omitempty"`
	Events          []string `json:"events"`
	SecretSet       bool     `json:"secret_set"`
	SupportedEvents []string `json:"supported_events"`
}

type serviceEventWebhookResolved struct {
	Config ServiceEventWebhookConfig
	Secret string
}

type ServiceGitHubWebhookStatus struct {
	Supported     bool     `json:"supported"`
	Status        string   `json:"status"`
	Message       string   `json:"message,omitempty"`
	Owner         string   `json:"owner,omitempty"`
	Repo          string   `json:"repo,omitempty"`
	WebhookURL    string   `json:"webhook_url,omitempty"`
	Active        bool     `json:"active"`
	Events        []string `json:"events,omitempty"`
	MissingEvents []string `json:"missing_events,omitempty"`
	CanRepair     bool     `json:"can_repair"`
}

type cloneServiceRequest struct {
	Name           string                `json:"name"`
	IncludeEnvVars *bool                 `json:"include_env_vars"`
	Overrides      cloneServiceOverrides `json:"overrides"`
}

type cloneServiceOverrides struct {
	Type              *string `json:"type"`
	Runtime           *string `json:"runtime"`
	RepoURL           *string `json:"repo_url"`
	Branch            *string `json:"branch"`
	BuildCommand      *string `json:"build_command"`
	StartCommand      *string `json:"start_command"`
	DockerfilePath    *string `json:"dockerfile_path"`
	DockerContext     *string `json:"docker_context"`
	BuildContext      *string `json:"build_context"`
	ImageURL          *string `json:"image_url"`
	HealthCheckPath   *string `json:"health_check_path"`
	Port              *int    `json:"port"`
	AutoDeploy        *bool   `json:"auto_deploy"`
	MaxShutdownDelay  *int    `json:"max_shutdown_delay"`
	PreDeployCommand  *string `json:"pre_deploy_command"`
	StaticPublishPath *string `json:"static_publish_path"`
	Schedule          *string `json:"schedule"`
	Plan              *string `json:"plan"`
	Instances         *int    `json:"instances"`
	DockerAccess      *bool   `json:"docker_access"`
	BaseImage         *string `json:"base_image"`
	BuildInclude      *string `json:"build_include"`
	BuildExclude      *string `json:"build_exclude"`
	ProjectID         *string `json:"project_id"`
	EnvironmentID     *string `json:"environment_id"`
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
	svc.RepoURL = services.RedactRepoURLCredentials(svc.RepoURL)
	svc.PublicURL = utils.ServicePublicURL(svc.Type, svc.Name, svc.Subdomain, h.Config.Deploy.Domain, svc.HostPort)
}

func (h *ServiceHandler) ensureAccess(w http.ResponseWriter, userID, workspaceID, minRole string) bool {
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, minRole); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func cloneServiceStringPtr(raw *string) *string {
	if raw == nil {
		return nil
	}
	v := strings.TrimSpace(*raw)
	if v == "" {
		return nil
	}
	cp := v
	return &cp
}

func (h *ServiceHandler) webhookEndpointURL() string {
	domain := strings.TrimSpace(h.Config.ControlPlane.Domain)
	if domain == "" {
		return ""
	}
	return "https://" + domain + "/api/v1/webhooks/github"
}

func (h *ServiceHandler) resolveServiceGitHubToken(userID, workspaceID string) string {
	if ws, err := models.GetWorkspace(workspaceID); err == nil && ws != nil {
		if encToken, err := models.GetUserGitHubToken(ws.OwnerID); err == nil && encToken != "" {
			if t, err := utils.Decrypt(encToken, h.Config.Crypto.EncryptionKey); err == nil {
				return t
			}
		}
	}

	if encToken, err := models.GetUserGitHubToken(userID); err == nil && encToken != "" {
		if t, err := utils.Decrypt(encToken, h.Config.Crypto.EncryptionKey); err == nil {
			return t
		}
	}

	return ""
}

func normalizeWebhookEvents(events []string) []string {
	normalized := make([]string, 0, len(events))
	seen := map[string]bool{}
	for _, raw := range events {
		e := strings.TrimSpace(raw)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		normalized = append(normalized, e)
	}
	return normalized
}

func missingWebhookEvents(events []string, required []string) []string {
	have := map[string]bool{}
	for _, e := range normalizeWebhookEvents(events) {
		have[e] = true
	}
	missing := make([]string, 0, len(required))
	for _, req := range required {
		r := strings.TrimSpace(req)
		if r == "" || have[r] {
			continue
		}
		missing = append(missing, r)
	}
	return missing
}

func normalizeServiceEventWebhookEventName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "deploy.started", "started":
		return "deploy.started"
	case "deploy.success", "deploy.succeeded", "success", "succeeded":
		return "deploy.success"
	case "deploy.failed", "failed", "failure":
		return "deploy.failed"
	case "deploy.rollback", "deploy.rolled_back", "rollback", "rolled_back":
		return "deploy.rollback"
	default:
		return ""
	}
}

func parseServiceEventWebhookEvents(raw string) []string {
	out := make([]string, 0, len(supportedServiceEventWebhookEvents))
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		norm := normalizeServiceEventWebhookEventName(part)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	if len(out) == 0 {
		out = append([]string{}, supportedServiceEventWebhookEvents...)
	}
	return out
}

func firstNonEmptyEnvValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(values[key]); v != "" {
			return v
		}
	}
	return ""
}

func firstSecretSet(secretSet map[string]bool, keys ...string) bool {
	for _, key := range keys {
		if secretSet[key] {
			return true
		}
	}
	return false
}

func (h *ServiceHandler) loadServiceEventWebhookResolved(serviceID string) (serviceEventWebhookResolved, error) {
	resolved := serviceEventWebhookResolved{
		Config: ServiceEventWebhookConfig{
			Enabled:         false,
			Events:          append([]string{}, supportedServiceEventWebhookEvents...),
			SupportedEvents: append([]string{}, supportedServiceEventWebhookEvents...),
		},
	}
	vars, err := models.ListEnvVars("service", serviceID)
	if err != nil {
		return resolved, err
	}
	values := map[string]string{}
	secretSet := map[string]bool{}
	for _, ev := range vars {
		key := strings.TrimSpace(ev.Key)
		if key == "" || strings.TrimSpace(ev.EncryptedValue) == "" {
			continue
		}
		if ev.IsSecret {
			secretSet[key] = true
		}
		decrypted, decErr := utils.Decrypt(ev.EncryptedValue, h.Config.Crypto.EncryptionKey)
		if decErr != nil {
			continue
		}
		values[key] = strings.TrimSpace(decrypted)
	}

	url := firstNonEmptyEnvValue(values,
		serviceEventWebhookURLKey,
		legacyDeployWebhookURLKey,
		legacyAltDeployWebhookURLKey,
	)
	eventsRaw := firstNonEmptyEnvValue(values,
		serviceEventWebhookEventsKey,
		legacyDeployWebhookEventsKey,
	)
	secret := firstNonEmptyEnvValue(values,
		serviceEventWebhookSecretKey,
		legacyDeployWebhookSecretKey,
		legacyAltDeployWebhookSecretKey,
	)

	resolved.Secret = secret
	resolved.Config.URL = url
	resolved.Config.Enabled = url != ""
	resolved.Config.Events = parseServiceEventWebhookEvents(eventsRaw)
	resolved.Config.SecretSet = firstSecretSet(secretSet,
		serviceEventWebhookSecretKey,
		legacyDeployWebhookSecretKey,
		legacyAltDeployWebhookSecretKey,
	) || strings.TrimSpace(secret) != ""

	return resolved, nil
}

func (h *ServiceHandler) getServiceGitHubWebhookStatus(userID string, svc *models.Service) ServiceGitHubWebhookStatus {
	status := ServiceGitHubWebhookStatus{
		Supported: false,
		Status:    "missing",
		CanRepair: false,
	}
	if svc == nil {
		status.Message = "service not found"
		return status
	}

	owner, repo := ParseGitHubOwnerRepo(svc.RepoURL)
	if owner == "" || repo == "" {
		status.Message = "service repository is not a GitHub repository URL"
		return status
	}
	status.Supported = true
	status.Owner = owner
	status.Repo = repo
	status.WebhookURL = h.webhookEndpointURL()

	ghToken := h.resolveServiceGitHubToken(userID, svc.WorkspaceID)
	if ghToken == "" {
		status.Status = "permission_denied"
		status.Message = "no GitHub account connected for this workspace"
		return status
	}

	if status.WebhookURL == "" {
		status.Status = "missing"
		status.Message = "control plane domain is not configured"
		return status
	}

	gh := services.NewGitHub(h.Config)
	hooks, err := gh.ListWebhooks(ghToken, owner, repo)
	if err != nil {
		var ghErr *services.GitHubAPIError
		if errors.As(err, &ghErr) {
			switch ghErr.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
				status.Status = "permission_denied"
				status.Message = "GitHub token cannot read repository webhooks (requires repo admin access)"
				return status
			}
		}
		status.Status = "permission_denied"
		status.Message = "failed to query GitHub webhooks"
		return status
	}

	targetURL := strings.TrimRight(strings.TrimSpace(status.WebhookURL), "/")
	for _, hook := range hooks {
		if strings.TrimRight(strings.TrimSpace(hook.Config.URL), "/") != targetURL {
			continue
		}

		status.Active = hook.Active
		status.Events = normalizeWebhookEvents(hook.Events)
		status.MissingEvents = missingWebhookEvents(status.Events, requiredGitHubWebhookEvents)
		status.CanRepair = true

		if hook.Active && len(status.MissingEvents) == 0 {
			status.Status = "installed"
			status.Message = "GitHub webhook installed and healthy"
			return status
		}

		status.Status = "missing"
		if !hook.Active {
			status.Message = "GitHub webhook exists but is inactive"
			return status
		}
		status.Message = "GitHub webhook exists but is missing required events"
		return status
	}

	status.Status = "missing"
	status.Message = "GitHub webhook not installed"
	status.CanRepair = true
	return status
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
	pagination, err := parseCursorPagination(r)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
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
	active := svcs[:0]
	for _, s := range svcs {
		if strings.EqualFold(strings.TrimSpace(s.Status), "soft_deleted") {
			continue
		}
		active = append(active, s)
	}
	svcs = active

	// Optional query-param filters (all optional, combine with AND).
	filterType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
	filterStatus := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	filterRuntime := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("runtime")))
	filterPlan := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("plan")))
	filterName := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("name")))
	filterRepoURL := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("repo_url")))
	filterProjectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	filterQuery := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
	filterSuspended := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("suspended")))

	if filterType != "" || filterStatus != "" || filterRuntime != "" || filterPlan != "" || filterName != "" || filterRepoURL != "" || filterProjectID != "" || filterQuery != "" || filterSuspended != "" {
		filtered := svcs[:0]
		for _, s := range svcs {
			if filterType != "" && strings.ToLower(s.Type) != filterType {
				continue
			}
			if filterStatus != "" && strings.ToLower(s.Status) != filterStatus {
				continue
			}
			if filterRuntime != "" && strings.ToLower(s.Runtime) != filterRuntime {
				continue
			}
			if filterPlan != "" && strings.ToLower(strings.TrimSpace(s.Plan)) != filterPlan {
				continue
			}
			if filterName != "" && !strings.Contains(strings.ToLower(s.Name), filterName) {
				continue
			}
			if filterRepoURL != "" && !strings.Contains(strings.ToLower(strings.TrimSpace(s.RepoURL)), filterRepoURL) {
				continue
			}
			if filterProjectID != "" {
				if s.ProjectID == nil || strings.TrimSpace(*s.ProjectID) != filterProjectID {
					continue
				}
			}
			if filterQuery != "" {
				haystack := strings.ToLower(strings.Join([]string{
					s.Name,
					s.RepoURL,
					s.Runtime,
					s.Type,
					s.Branch,
				}, " "))
				if !strings.Contains(haystack, filterQuery) {
					continue
				}
			}
			if filterSuspended == "true" && !s.IsSuspended {
				continue
			}
			if filterSuspended == "false" && s.IsSuspended {
				continue
			}
			filtered = append(filtered, s)
		}
		svcs = filtered
	}

	paged, pageMeta := paginateSlice(svcs, pagination)
	for i := range paged {
		h.decorateServicePublicURL(&paged[i])
	}
	if pageMeta != nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"data":       paged,
			"pagination": pageMeta,
		})
		return
	}
	utils.RespondJSON(w, http.StatusOK, paged)
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
	svc.RepoURL = strings.TrimSpace(svc.RepoURL)
	validationIssues := make([]utils.ValidationIssue, 0, 8)
	if svc.Name == "" {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "name", Message: "is required"})
	}
	if svc.Type == "" {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "type", Message: "is required"})
	}
	if svc.Port < 0 || svc.Port > 65535 {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "port", Message: "must be between 0 and 65535"})
	}
	if svc.Instances < 0 {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "instances", Message: "must be >= 0"})
	}
	if services.RepoURLHasEmbeddedCredentials(svc.RepoURL) {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "repo_url", Message: "must not include embedded credentials; connect your GitHub account instead"})
	}
	if strings.TrimSpace(svc.Plan) != "" {
		if _, ok := services.NormalizePlan(svc.Plan); !ok {
			validationIssues = append(validationIssues, utils.ValidationIssue{Field: "plan", Message: "must be one of free, starter, standard, pro"})
		}
	}
	if len(validationIssues) > 0 {
		utils.RespondValidationErrors(w, http.StatusBadRequest, validationIssues)
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

	relationIssues := make([]utils.ValidationIssue, 0, 3)
	if svc.ProjectID != nil && *svc.ProjectID != "" {
		project, err := models.GetProject(*svc.ProjectID)
		if err != nil || project == nil || project.WorkspaceID != svc.WorkspaceID {
			relationIssues = append(relationIssues, utils.ValidationIssue{Field: "project_id", Message: "is invalid"})
		}
	}
	if svc.EnvironmentID != nil && *svc.EnvironmentID != "" {
		env, err := models.GetEnvironment(*svc.EnvironmentID)
		if err != nil || env == nil {
			relationIssues = append(relationIssues, utils.ValidationIssue{Field: "environment_id", Message: "is invalid"})
		} else {
			if svc.ProjectID != nil && *svc.ProjectID != "" && env.ProjectID != *svc.ProjectID {
				relationIssues = append(relationIssues, utils.ValidationIssue{Field: "environment_id", Message: "does not belong to project_id"})
			}
			if len(relationIssues) == 0 && env.IsProtected && !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleAdmin) {
				return
			}
		}
	}
	if len(relationIssues) > 0 {
		utils.RespondValidationErrors(w, http.StatusBadRequest, relationIssues)
		return
	}
	if svc.Port == 0 {
		svc.Port = 10000
	}
	if svc.Plan == "" {
		svc.Plan = services.PlanStarter
	}
	if p, ok := services.NormalizePlan(svc.Plan); ok {
		svc.Plan = p
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
	var billingCustomer *models.BillingCustomer
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
		if bc == nil {
			utils.RespondError(w, http.StatusInternalServerError, "billing error: failed to initialize billing customer")
			return
		}
		billingCustomer = bc
	}

	if err := models.CreateService(&svc); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create service: "+err.Error())
		return
	}

	// Add to Stripe subscription for paid plans
	if svc.Plan != "free" && h.Stripe.Enabled() && billingCustomer != nil {
		if err := h.Stripe.AddSubscriptionItem(billingCustomer, svc.WorkspaceID, "service", svc.ID, svc.Name, svc.Plan); err != nil {
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

func (h *ServiceHandler) CloneService(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	source, err := models.GetService(id)
	if err != nil || source == nil {
		respondServiceNotFound(w, id)
		return
	}
	if strings.EqualFold(strings.TrimSpace(source.Status), "soft_deleted") {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, source.WorkspaceID, models.RoleDeveloper) {
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var req cloneServiceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "name", Message: "is required"}})
		return
	}

	clone := models.Service{
		WorkspaceID:       source.WorkspaceID,
		ProjectID:         cloneServiceStringPtr(source.ProjectID),
		EnvironmentID:     cloneServiceStringPtr(source.EnvironmentID),
		Name:              req.Name,
		Type:              strings.TrimSpace(source.Type),
		Runtime:           strings.TrimSpace(source.Runtime),
		RepoURL:           services.RedactRepoURLCredentials(source.RepoURL),
		Branch:            strings.TrimSpace(source.Branch),
		BuildCommand:      source.BuildCommand,
		StartCommand:      source.StartCommand,
		DockerfilePath:    source.DockerfilePath,
		DockerContext:     source.DockerContext,
		ImageURL:          source.ImageURL,
		HealthCheckPath:   source.HealthCheckPath,
		Port:              source.Port,
		AutoDeploy:        source.AutoDeploy,
		MaxShutdownDelay:  source.MaxShutdownDelay,
		PreDeployCommand:  source.PreDeployCommand,
		StaticPublishPath: source.StaticPublishPath,
		Schedule:          source.Schedule,
		Plan:              strings.TrimSpace(source.Plan),
		Instances:         source.Instances,
		DockerAccess:      source.DockerAccess,
		BaseImage:         source.BaseImage,
		BuildInclude:      source.BuildInclude,
		BuildExclude:      source.BuildExclude,
	}

	o := req.Overrides
	if o.Type != nil {
		clone.Type = strings.TrimSpace(*o.Type)
	}
	if o.Runtime != nil {
		clone.Runtime = strings.TrimSpace(*o.Runtime)
	}
	if o.RepoURL != nil {
		clone.RepoURL = strings.TrimSpace(*o.RepoURL)
	}
	if o.Branch != nil {
		clone.Branch = strings.TrimSpace(*o.Branch)
	}
	if o.BuildCommand != nil {
		clone.BuildCommand = strings.TrimSpace(*o.BuildCommand)
	}
	if o.StartCommand != nil {
		clone.StartCommand = strings.TrimSpace(*o.StartCommand)
	}
	if o.DockerfilePath != nil {
		clone.DockerfilePath = strings.TrimSpace(*o.DockerfilePath)
	}
	if o.DockerContext != nil {
		clone.DockerContext = strings.TrimSpace(*o.DockerContext)
	}
	if o.BuildContext != nil && o.DockerContext == nil {
		clone.DockerContext = strings.TrimSpace(*o.BuildContext)
	}
	if o.ImageURL != nil {
		clone.ImageURL = strings.TrimSpace(*o.ImageURL)
	}
	if o.HealthCheckPath != nil {
		clone.HealthCheckPath = strings.TrimSpace(*o.HealthCheckPath)
	}
	if o.Port != nil {
		clone.Port = *o.Port
	}
	if o.AutoDeploy != nil {
		clone.AutoDeploy = *o.AutoDeploy
	}
	if o.MaxShutdownDelay != nil {
		clone.MaxShutdownDelay = *o.MaxShutdownDelay
	}
	if o.PreDeployCommand != nil {
		clone.PreDeployCommand = strings.TrimSpace(*o.PreDeployCommand)
	}
	if o.StaticPublishPath != nil {
		clone.StaticPublishPath = strings.TrimSpace(*o.StaticPublishPath)
	}
	if o.Schedule != nil {
		clone.Schedule = strings.TrimSpace(*o.Schedule)
	}
	if o.Plan != nil {
		clone.Plan = strings.TrimSpace(*o.Plan)
	}
	if o.Instances != nil {
		clone.Instances = *o.Instances
	}
	if o.DockerAccess != nil {
		clone.DockerAccess = *o.DockerAccess
	}
	if o.BaseImage != nil {
		clone.BaseImage = strings.TrimSpace(*o.BaseImage)
	}
	if o.BuildInclude != nil {
		clone.BuildInclude = strings.TrimSpace(*o.BuildInclude)
	}
	if o.BuildExclude != nil {
		clone.BuildExclude = strings.TrimSpace(*o.BuildExclude)
	}
	if o.ProjectID != nil {
		clone.ProjectID = cloneServiceStringPtr(o.ProjectID)
	}
	if o.EnvironmentID != nil {
		clone.EnvironmentID = cloneServiceStringPtr(o.EnvironmentID)
	}

	validationIssues := make([]utils.ValidationIssue, 0, 8)
	if clone.Name == "" {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "name", Message: "is required"})
	}
	if clone.Type == "" {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "type", Message: "is required"})
	}
	if clone.Port < 0 || clone.Port > 65535 {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "port", Message: "must be between 0 and 65535"})
	}
	if clone.Instances < 0 {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "instances", Message: "must be >= 0"})
	}
	if services.RepoURLHasEmbeddedCredentials(clone.RepoURL) {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "overrides.repo_url", Message: "must not include embedded credentials; connect your GitHub account instead"})
	}
	if strings.TrimSpace(clone.Plan) != "" {
		if _, ok := services.NormalizePlan(clone.Plan); !ok {
			validationIssues = append(validationIssues, utils.ValidationIssue{Field: "plan", Message: "must be one of free, starter, standard, pro"})
		}
	}
	if len(validationIssues) > 0 {
		utils.RespondValidationErrors(w, http.StatusBadRequest, validationIssues)
		return
	}

	relationIssues := make([]utils.ValidationIssue, 0, 3)
	if clone.ProjectID != nil && *clone.ProjectID != "" {
		project, err := models.GetProject(*clone.ProjectID)
		if err != nil || project == nil || project.WorkspaceID != clone.WorkspaceID {
			relationIssues = append(relationIssues, utils.ValidationIssue{Field: "project_id", Message: "is invalid"})
		}
	}
	if clone.EnvironmentID != nil && *clone.EnvironmentID != "" {
		env, err := models.GetEnvironment(*clone.EnvironmentID)
		if err != nil || env == nil {
			relationIssues = append(relationIssues, utils.ValidationIssue{Field: "environment_id", Message: "is invalid"})
		} else {
			if clone.ProjectID != nil && *clone.ProjectID != "" && env.ProjectID != *clone.ProjectID {
				relationIssues = append(relationIssues, utils.ValidationIssue{Field: "environment_id", Message: "does not belong to project_id"})
			}
			if len(relationIssues) == 0 && env.IsProtected && !h.ensureAccess(w, userID, clone.WorkspaceID, models.RoleAdmin) {
				return
			}
		}
	}
	if len(relationIssues) > 0 {
		utils.RespondValidationErrors(w, http.StatusBadRequest, relationIssues)
		return
	}

	if clone.Port == 0 {
		clone.Port = 10000
	}
	if clone.Plan == "" {
		clone.Plan = services.PlanStarter
	}
	if p, ok := services.NormalizePlan(clone.Plan); ok {
		clone.Plan = p
	}
	if clone.Instances == 0 {
		clone.Instances = 1
	}
	if clone.Branch == "" {
		clone.Branch = "main"
	}

	if clone.Plan == services.PlanFree {
		count, err := models.CountResourcesByWorkspaceAndPlan(clone.WorkspaceID, "service", services.PlanFree)
		if err == nil && count >= 1 {
			utils.RespondError(w, http.StatusBadRequest, "free tier limit reached: 1 free service per workspace")
			return
		}
	}

	stripeEnabled := h.Stripe != nil && h.Stripe.Enabled()
	var billingCustomer *models.BillingCustomer
	if clone.Plan != services.PlanFree && stripeEnabled {
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
		if bc == nil {
			utils.RespondError(w, http.StatusInternalServerError, "billing error: failed to initialize billing customer")
			return
		}
		billingCustomer = bc
	}

	if err := models.CreateService(&clone); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to clone service: "+err.Error())
		return
	}

	includeEnvVars := true
	if req.IncludeEnvVars != nil {
		includeEnvVars = *req.IncludeEnvVars
	}
	envVarsCopied := 0
	if includeEnvVars {
		sourceVars, err := models.ListEnvVars("service", source.ID)
		if err != nil {
			_ = models.DeleteService(clone.ID)
			utils.RespondError(w, http.StatusInternalServerError, "failed to clone env vars")
			return
		}
		if len(sourceVars) > 0 {
			clonedVars := make([]models.EnvVar, 0, len(sourceVars))
			for _, ev := range sourceVars {
				clonedVars = append(clonedVars, models.EnvVar{
					Key:            ev.Key,
					EncryptedValue: ev.EncryptedValue,
					IsSecret:       ev.IsSecret,
				})
			}
			if err := models.BulkUpsertEnvVars("service", clone.ID, clonedVars); err != nil {
				_ = models.DeleteService(clone.ID)
				utils.RespondError(w, http.StatusInternalServerError, "failed to clone env vars")
				return
			}
			envVarsCopied = len(clonedVars)
		}
	}

	if clone.Plan != services.PlanFree && stripeEnabled && billingCustomer != nil {
		if err := h.Stripe.AddSubscriptionItem(billingCustomer, clone.WorkspaceID, "service", clone.ID, clone.Name, clone.Plan); err != nil {
			log.Printf("Warning: failed to add billing for cloned service %s: %v", clone.ID, err)
			_ = models.DeleteService(clone.ID)
			if errors.Is(err, services.ErrNoDefaultPaymentMethod) {
				utils.RespondError(w, http.StatusPaymentRequired, "payment method required. Please add a default payment method in billing settings.")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, "billing error: "+err.Error())
			return
		}
	}

	if strings.Contains(clone.RepoURL, "github.com") {
		owner, repo := ParseGitHubOwnerRepo(clone.RepoURL)
		if owner != "" && repo != "" {
			if encToken, err := models.GetUserGitHubToken(userID); err == nil && encToken != "" {
				if ghToken, err := utils.Decrypt(encToken, h.Config.Crypto.EncryptionKey); err == nil {
					gh := services.NewGitHub(h.Config)
					webhookURL := "https://" + h.Config.ControlPlane.Domain + "/api/v1/webhooks/github"
					if err := gh.CreateWebhook(ghToken, owner, repo, webhookURL, h.Config.GitHub.WebhookSecret); err != nil {
						log.Printf("Warning: failed to auto-register webhook for cloned service %s (%s/%s): %v", clone.ID, owner, repo, err)
					}
				}
			}
		}
	}

	projectID := ""
	if clone.ProjectID != nil {
		projectID = *clone.ProjectID
	}
	environmentID := ""
	if clone.EnvironmentID != nil {
		environmentID = *clone.EnvironmentID
	}

	services.Audit(clone.WorkspaceID, userID, "service.cloned", "service", clone.ID, map[string]interface{}{
		"source_service_id": source.ID,
		"name":              clone.Name,
		"type":              clone.Type,
		"plan":              clone.Plan,
		"project_id":        projectID,
		"environment_id":    environmentID,
		"include_env_vars":  includeEnvVars,
		"env_vars_copied":   envVarsCopied,
	})

	clone.BuildContext = clone.DockerContext
	h.decorateServicePublicURL(&clone)
	utils.RespondJSON(w, http.StatusCreated, map[string]interface{}{
		"service":                clone,
		"cloned_from_service_id": source.ID,
		"env_vars_copied":        envVarsCopied,
	})
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
		respondServiceNotFound(w, id)
		return
	}
	if strings.EqualFold(strings.TrimSpace(svc.Status), "soft_deleted") {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleViewer) {
		return
	}
	h.decorateServicePublicURL(svc)
	utils.RespondJSON(w, http.StatusOK, svc)
}

func (h *ServiceHandler) ListServiceGitHubWorkflows(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]

	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleViewer) {
		return
	}

	owner, repo := ParseGitHubOwnerRepo(svc.RepoURL)
	if owner == "" || repo == "" {
		utils.RespondJSON(w, http.StatusOK, []services.GitHubWorkflow{})
		return
	}

	ghToken := h.resolveServiceGitHubToken(userID, svc.WorkspaceID)
	if ghToken == "" {
		utils.RespondError(w, http.StatusBadRequest, "no GitHub account connected")
		return
	}

	gh := services.NewGitHub(h.Config)
	workflows, err := gh.ListWorkflows(ghToken, owner, repo)
	if err != nil {
		utils.RespondError(w, http.StatusBadGateway, "failed to list workflows: "+err.Error())
		return
	}
	utils.RespondJSON(w, http.StatusOK, workflows)
}

func (h *ServiceHandler) GetServiceGitHubWebhookStatus(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]

	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleViewer) {
		return
	}

	status := h.getServiceGitHubWebhookStatus(userID, svc)
	utils.RespondJSON(w, http.StatusOK, status)
}

func (h *ServiceHandler) RepairServiceGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]

	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}

	owner, repo := ParseGitHubOwnerRepo(svc.RepoURL)
	if owner == "" || repo == "" {
		utils.RespondError(w, http.StatusBadRequest, "service repository is not a GitHub repository URL")
		return
	}

	ghToken := h.resolveServiceGitHubToken(userID, svc.WorkspaceID)
	if ghToken == "" {
		utils.RespondError(w, http.StatusBadRequest, "no GitHub account connected")
		return
	}

	webhookURL := h.webhookEndpointURL()
	if webhookURL == "" {
		utils.RespondError(w, http.StatusBadRequest, "control plane domain is not configured")
		return
	}

	gh := services.NewGitHub(h.Config)
	if err := gh.CreateWebhook(ghToken, owner, repo, webhookURL, h.Config.GitHub.WebhookSecret); err != nil {
		var ghErr *services.GitHubAPIError
		if errors.As(err, &ghErr) {
			switch ghErr.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
				utils.RespondError(w, http.StatusBadRequest, "GitHub token lacks permission to manage repository webhooks")
				return
			}
		}
		utils.RespondError(w, http.StatusBadGateway, "failed to repair webhook: "+err.Error())
		return
	}

	services.Audit(svc.WorkspaceID, userID, "service.github_webhook.repaired", "service", svc.ID, map[string]interface{}{
		"repo_url": services.RedactRepoURLCredentials(svc.RepoURL),
		"owner":    owner,
		"repo":     repo,
	})

	status := h.getServiceGitHubWebhookStatus(userID, svc)
	utils.RespondJSON(w, http.StatusOK, status)
}

func (h *ServiceHandler) GetServiceEventWebhookConfig(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]

	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleViewer) {
		return
	}

	resolved, err := h.loadServiceEventWebhookResolved(svc.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load event webhook config")
		return
	}
	utils.RespondJSON(w, http.StatusOK, resolved.Config)
}

func (h *ServiceHandler) UpdateServiceEventWebhookConfig(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]

	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}

	var req struct {
		Enabled bool     `json:"enabled"`
		URL     string   `json:"url"`
		Events  []string `json:"events"`
		Secret  *string  `json:"secret"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	envVars := make([]models.EnvVar, 0, 3)
	deleteKeys := make([]string, 0, 8)
	deleteSeen := map[string]bool{}
	addDeleteKey := func(key string) {
		key = strings.TrimSpace(key)
		if key == "" || deleteSeen[key] {
			return
		}
		deleteSeen[key] = true
		deleteKeys = append(deleteKeys, key)
	}

	if req.Enabled {
		url := strings.TrimSpace(req.URL)
		if url == "" {
			utils.RespondError(w, http.StatusBadRequest, "url is required when enabled=true")
			return
		}
		events := append([]string{}, supportedServiceEventWebhookEvents...)
		if len(req.Events) > 0 {
			normalized := make([]string, 0, len(req.Events))
			seen := map[string]bool{}
			for _, raw := range req.Events {
				norm := normalizeServiceEventWebhookEventName(raw)
				if norm == "" || seen[norm] {
					continue
				}
				seen[norm] = true
				normalized = append(normalized, norm)
			}
			if len(normalized) == 0 {
				utils.RespondError(w, http.StatusBadRequest, "events contains no supported event names")
				return
			}
			events = normalized
		}

		encURL, err := utils.Encrypt(url, h.Config.Crypto.EncryptionKey)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to encrypt webhook url")
			return
		}
		envVars = append(envVars,
			models.EnvVar{Key: serviceEventWebhookURLKey, EncryptedValue: encURL, IsSecret: false},
		)

		encEvents, err := utils.Encrypt(strings.Join(events, ","), h.Config.Crypto.EncryptionKey)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to encrypt webhook events")
			return
		}
		envVars = append(envVars,
			models.EnvVar{Key: serviceEventWebhookEventsKey, EncryptedValue: encEvents, IsSecret: false},
		)

		addDeleteKey(legacyDeployWebhookURLKey)
		addDeleteKey(legacyAltDeployWebhookURLKey)
		addDeleteKey(legacyDeployWebhookEventsKey)

		if req.Secret != nil {
			secret := strings.TrimSpace(*req.Secret)
			if secret == "" {
				addDeleteKey(serviceEventWebhookSecretKey)
				addDeleteKey(legacyDeployWebhookSecretKey)
				addDeleteKey(legacyAltDeployWebhookSecretKey)
			} else {
				encSecret, err := utils.Encrypt(secret, h.Config.Crypto.EncryptionKey)
				if err != nil {
					utils.RespondError(w, http.StatusInternalServerError, "failed to encrypt webhook secret")
					return
				}
				envVars = append(envVars,
					models.EnvVar{Key: serviceEventWebhookSecretKey, EncryptedValue: encSecret, IsSecret: true},
				)
				addDeleteKey(legacyDeployWebhookSecretKey)
				addDeleteKey(legacyAltDeployWebhookSecretKey)
			}
		}
	} else {
		addDeleteKey(serviceEventWebhookURLKey)
		addDeleteKey(serviceEventWebhookEventsKey)
		addDeleteKey(serviceEventWebhookSecretKey)
		addDeleteKey(legacyDeployWebhookURLKey)
		addDeleteKey(legacyAltDeployWebhookURLKey)
		addDeleteKey(legacyDeployWebhookEventsKey)
		addDeleteKey(legacyDeployWebhookSecretKey)
		addDeleteKey(legacyAltDeployWebhookSecretKey)
	}

	if len(envVars) > 0 {
		if err := models.MergeUpsertEnvVars("service", svc.ID, envVars); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to save event webhook config")
			return
		}
	}
	if len(deleteKeys) > 0 {
		if err := models.DeleteEnvVarsByKeys("service", svc.ID, deleteKeys); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to clean up legacy event webhook config")
			return
		}
	}

	services.Audit(svc.WorkspaceID, userID, "service.event_webhook.updated", "service", svc.ID, map[string]interface{}{
		"enabled": req.Enabled,
		"events":  req.Events,
	})

	resolved, err := h.loadServiceEventWebhookResolved(svc.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "event webhook config updated but failed to reload")
		return
	}
	utils.RespondJSON(w, http.StatusOK, resolved.Config)
}

func (h *ServiceHandler) TestServiceEventWebhook(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]

	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}

	resolved, err := h.loadServiceEventWebhookResolved(svc.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load event webhook config")
		return
	}
	if !resolved.Config.Enabled || strings.TrimSpace(resolved.Config.URL) == "" {
		utils.RespondError(w, http.StatusBadRequest, "event webhook is not enabled")
		return
	}

	payload := map[string]interface{}{
		"event":        "deploy.test",
		"service_id":   svc.ID,
		"service_name": svc.Name,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
		"test":         true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to encode test payload")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolved.Config.URL, bytes.NewReader(body))
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid webhook url")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "railpush-event-webhook/1.0")
	if strings.TrimSpace(resolved.Secret) != "" {
		mac := hmac.New(sha256.New, []byte(resolved.Secret))
		mac.Write(body)
		req.Header.Set("X-RailPush-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		utils.RespondError(w, http.StatusBadGateway, "test delivery failed: "+err.Error())
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	preview := strings.TrimSpace(string(respBody))

	services.Audit(svc.WorkspaceID, userID, "service.event_webhook.tested", "service", svc.ID, map[string]interface{}{
		"http_status": resp.StatusCode,
	})

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		utils.RespondError(w, http.StatusBadGateway, fmt.Sprintf("test delivery failed with HTTP %d", resp.StatusCode))
		return
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":            "sent",
		"webhook_http_code": resp.StatusCode,
		"response_preview":  preview,
	})
}

func (h *ServiceHandler) UpdateService(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		respondServiceNotFound(w, id)
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
	var deletionProtection *bool
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if v, ok := updates["deletion_protection"].(bool); ok {
		deletionProtection = &v
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
	if v, ok := updates["build_context"].(string); ok {
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
		svc.Plan = newPlanEffective
	}

	if isDryRunRequest(r) {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"status":                "dry_run",
			"service_id":            svc.ID,
			"workspace_id":          svc.WorkspaceID,
			"name":                  svc.Name,
			"plan":                  svc.Plan,
			"instances":             svc.Instances,
			"project_id":            svc.ProjectID,
			"environment_id":        svc.EnvironmentID,
			"deletion_protection":   deletionProtection,
			"would_change_plan":     planProvided && newPlanEffective != oldPlanEffective,
			"would_scale_instances": oldInstances != svc.Instances,
		})
		return
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

	if err := models.UpdateService(svc); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update service")
		return
	}
	if deletionProtection != nil {
		if err := models.SetResourceDeletionProtection("service", svc.ID, svc.WorkspaceID, svc.Name, *deletionProtection); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to update deletion protection")
			return
		}
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

	var deletionProtectionAudit interface{}
	if deletionProtection != nil {
		deletionProtectionAudit = *deletionProtection
	}
	services.Audit(svc.WorkspaceID, userID, "service.updated", "service", svc.ID, map[string]interface{}{
		"name":                svc.Name,
		"plan":                svc.Plan,
		"instances":           svc.Instances,
		"project_id":          svc.ProjectID,
		"environment_id":      svc.EnvironmentID,
		"deletion_protection": deletionProtectionAudit,
	})
	h.decorateServicePublicURL(svc)
	utils.RespondJSON(w, http.StatusOK, svc)
}

// DeleteService enforces a two-step destructive confirmation flow.
// First call (without confirmation_token) returns a short-lived token.
// Second call with confirmation_token soft-deletes the service into a recoverable state.
// After the recovery window, callers may pass hard_delete=true with a fresh token to permanently purge.
func (h *ServiceHandler) DeleteService(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to look up service")
		return
	}
	if svc == nil {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}
	if h.isProtectedEnvironment(svc.EnvironmentID) && !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleAdmin) {
		return
	}
	state, err := models.GetResourceDeletionState("service", svc.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to read deletion state")
		return
	}
	if state != nil && state.DeletionProtection {
		utils.RespondError(w, http.StatusForbidden, "deletion protection is enabled for this service")
		return
	}

	var req destructiveDeleteRequest
	if err := decodeOptionalJSONBody(w, r, &req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token := strings.TrimSpace(req.ConfirmationToken)
	if token == "" {
		confirmationToken, expiresAt, err := models.IssueResourceDeletionToken("service", svc.ID, svc.WorkspaceID, svc.Name, deleteConfirmationTTL)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to issue confirmation token")
			return
		}
		utils.RespondJSON(w, http.StatusAccepted, map[string]interface{}{
			"status":                     "confirmation_required",
			"confirmation_token":         confirmationToken,
			"confirmation_token_expires": expiresAt,
			"hard_delete":                false,
			"recovery_window_hours":      int(softDeleteRecoveryWindow / time.Hour),
		})
		return
	}
	if err := models.VerifyResourceDeletionToken("service", svc.ID, token); err != nil {
		switch {
		case errors.Is(err, models.ErrDeleteConfirmationExpired):
			utils.RespondError(w, http.StatusBadRequest, "confirmation token expired; request a new token")
		case errors.Is(err, models.ErrDeleteConfirmationInvalid):
			utils.RespondError(w, http.StatusBadRequest, "invalid confirmation token")
		default:
			utils.RespondError(w, http.StatusBadRequest, "confirmation token required")
		}
		return
	}

	if req.HardDelete {
		if state == nil || state.DeletedAt == nil {
			utils.RespondError(w, http.StatusConflict, "service must be soft-deleted before hard delete")
			return
		}
		if state.PurgeAfter != nil && time.Now().Before(*state.PurgeAfter) {
			utils.RespondError(w, http.StatusConflict, "service is in recovery window; hard delete available after "+state.PurgeAfter.Format(time.RFC3339))
			return
		}
		if err := h.hardDeleteService(svc); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to delete service")
			return
		}
		_ = models.DeleteResourceDeletionState("service", svc.ID)
		services.Audit(svc.WorkspaceID, userID, "service.deleted", "service", id, map[string]interface{}{
			"name": svc.Name,
		})
		utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return
	}
	if state != nil && state.DeletedAt != nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"status":      "soft_deleted",
			"purge_after": state.PurgeAfter,
		})
		return
	}

	purgeAfter, err := models.MarkResourceSoftDeleted("service", svc.ID, svc.WorkspaceID, svc.Name, softDeleteRecoveryWindow)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to soft-delete service")
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
		if instances, err := models.ListServiceInstances(id); err == nil {
			for _, inst := range instances {
				if inst.ContainerID != "" {
					_ = h.Worker.Deployer.StopContainer(inst.ContainerID)
				}
			}
		}
	}
	_ = models.SetServiceSuspended(id, true)
	_ = models.UpdateServiceStatus(id, "soft_deleted", svc.ContainerID, svc.HostPort)
	if models.IsBillingItemMetered("service", id) {
		_ = models.RecordUsageEvent("service", id, "stop")
	}

	services.Audit(svc.WorkspaceID, userID, "service.soft_deleted", "service", id, map[string]interface{}{
		"name":        svc.Name,
		"purge_after": purgeAfter,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":                "soft_deleted",
		"purge_after":           purgeAfter,
		"recovery_window_hours": int(softDeleteRecoveryWindow / time.Hour),
	})
}

func (h *ServiceHandler) RestoreService(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}
	if h.isProtectedEnvironment(svc.EnvironmentID) && !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleAdmin) {
		return
	}
	state, err := models.GetResourceDeletionState("service", svc.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to read deletion state")
		return
	}
	if state == nil || state.DeletedAt == nil {
		utils.RespondError(w, http.StatusBadRequest, "service is not soft-deleted")
		return
	}
	if err := models.RestoreSoftDeletedResource("service", svc.ID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to restore service")
		return
	}
	_ = models.UpdateServiceStatus(id, "suspended", svc.ContainerID, svc.HostPort)
	_ = models.SetServiceSuspended(id, true)
	services.Audit(svc.WorkspaceID, userID, "service.restored", "service", id, map[string]interface{}{
		"name": svc.Name,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "restored",
		"message": "service restored in suspended state; call resume to run it again",
	})
}

func (h *ServiceHandler) hardDeleteService(svc *models.Service) error {
	id := svc.ID

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
		return err
	}
	return nil
}

// RestartService does docker restart on the container
func (h *ServiceHandler) RestartService(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}
	if h.isProtectedEnvironment(svc.EnvironmentID) && !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleAdmin) {
		return
	}
	if isDryRunRequest(r) {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"status":        "dry_run",
			"service_id":    id,
			"workspace_id":  svc.WorkspaceID,
			"would_restart": true,
		})
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
		respondServiceNotFound(w, id)
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
		respondServiceNotFound(w, id)
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
