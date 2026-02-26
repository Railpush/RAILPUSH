package handlers

import (
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

type AIFixHandler struct {
	Config *config.Config
	Worker *services.Worker
}

func NewAIFixHandler(cfg *config.Config, worker *services.Worker) *AIFixHandler {
	return &AIFixHandler{Config: cfg, Worker: worker}
}

// StartFix creates a new AI fix session and starts the first attempt.
func (h *AIFixHandler) StartFix(w http.ResponseWriter, r *http.Request) {
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

	// Check AI availability
	aiFixer := services.NewAIFixService(h.Config)
	if !aiFixer.Available() {
		utils.RespondError(w, http.StatusBadRequest, "AI not available")
		return
	}

	var req struct {
		Hint        string `json:"hint"`
		PreviewOnly bool   `json:"preview_only"`
	}
	if err := decodeOptionalJSONBody(w, r, &req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Hint = strings.TrimSpace(req.Hint)

	// Check that there's a failed deploy to fix
	failedDeploy, _ := models.GetLastFailedDeploy(svc.ID)
	if failedDeploy == nil {
		utils.RespondError(w, http.StatusBadRequest, "no failed deploy to fix")
		return
	}

	if req.PreviewOnly {
		patch, err := aiFixer.PreviewFix(svc, failedDeploy, req.Hint)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to generate AI fix preview")
			return
		}
		dockerfileChanged := strings.TrimSpace(patch.Dockerfile) != "" && strings.TrimSpace(patch.Dockerfile) != strings.TrimSpace(failedDeploy.DockerfileOverride)
		buildCommandChanged := strings.TrimSpace(patch.BuildCommand) != "" && strings.TrimSpace(patch.BuildCommand) != strings.TrimSpace(svc.BuildCommand)
		startCommandChanged := strings.TrimSpace(patch.StartCommand) != "" && strings.TrimSpace(patch.StartCommand) != strings.TrimSpace(svc.StartCommand)
		canApply := dockerfileChanged || buildCommandChanged || startCommandChanged || len(patch.EnvVars) > 0
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"status":           "preview",
			"service_id":       svc.ID,
			"failed_deploy_id": failedDeploy.ID,
			"source":           patch.Source,
			"summary":          patch.Summary,
			"dockerfile_diff":  patch.DockerfileDiff,
			"changes": map[string]interface{}{
				"dockerfile_changed": dockerfileChanged,
				"build_command":     patch.BuildCommand,
				"start_command":     patch.StartCommand,
				"env_vars":          patch.EnvVars,
			},
			"can_apply": canApply,
		})
		return
	}

	// Check for existing active session
	existing, _ := models.GetActiveAIFixSessionForService(svc.ID)
	if existing != nil {
		utils.RespondError(w, http.StatusConflict, "AI fix already in progress")
		return
	}

	// Create session
	session, err := models.CreateAIFixSession(svc.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create AI fix session")
		return
	}

	// Start first attempt in background
	go func() {
		if err := aiFixer.AttemptFixWithOptions(session, h.Worker, services.AIFixOptions{Hint: req.Hint}); err != nil {
			log.Printf("ai_fix: first attempt failed for service %s: %v", svc.ID, err)
			_ = models.UpdateAIFixSessionStatus(session.ID, "error")
		}
	}()

	utils.RespondJSON(w, http.StatusOK, session)
}

// GetStatus returns the current status of an AI fix session for a service.
func (h *AIFixHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
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

	session, err := models.GetActiveAIFixSessionForService(svc.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to query session")
		return
	}
	if session == nil {
		// No active session — check for the most recent completed one
		session, err = getMostRecentAIFixSession(svc.ID)
		if err != nil || session == nil {
			utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
				"active": false,
			})
			return
		}
	}

	resp := map[string]interface{}{
		"active":          session.Status == "running",
		"session":         session,
		"current_attempt": session.CurrentAttempt,
		"max_attempts":    session.MaxAttempts,
		"status":          session.Status,
		"last_ai_summary": session.LastAISummary,
	}

	// Include last deploy status if available
	if session.LastDeployID != "" {
		if deploy, err := models.GetDeploy(session.LastDeployID); err == nil && deploy != nil {
			resp["last_deploy_status"] = deploy.Status
		}
	}

	utils.RespondJSON(w, http.StatusOK, resp)
}

// GetDiagnosis returns a plain-English diagnosis for a failed deploy.
// It does not mutate state or trigger a new deploy.
func (h *AIFixHandler) GetDiagnosis(w http.ResponseWriter, r *http.Request) {
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

	deployID := strings.TrimSpace(r.URL.Query().Get("deploy_id"))
	var deploy *models.Deploy
	if deployID != "" {
		deploy, err = models.GetDeploy(deployID)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to load deploy")
			return
		}
		if deploy == nil || deploy.ServiceID != svc.ID {
			utils.RespondError(w, http.StatusNotFound, "deploy not found")
			return
		}
		if strings.ToLower(strings.TrimSpace(deploy.Status)) != "failed" {
			utils.RespondError(w, http.StatusBadRequest, "deploy is not failed")
			return
		}
	} else {
		deploy, err = models.GetLastFailedDeploy(svc.ID)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to load failed deploy")
			return
		}
		if deploy == nil {
			utils.RespondError(w, http.StatusNotFound, "no failed deploy to diagnose")
			return
		}
	}

	aiFixer := services.NewAIFixService(h.Config)
	diagnosis, err := aiFixer.DiagnoseDeployFailure(deploy)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to generate diagnosis")
		return
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"deploy_id":      deploy.ID,
		"status":         deploy.Status,
		"source":         diagnosis.Source,
		"summary":        diagnosis.Summary,
		"probable_cause": diagnosis.ProbableCause,
		"suggested_fix":  diagnosis.SuggestedFix,
		"confidence":     diagnosis.Confidence,
		"can_auto_fix":   aiFixer.Available(),
	})
}

func getMostRecentAIFixSession(serviceID string) (*models.AIFixSession, error) {
	// Get the most recent non-running session (success, exhausted, error)
	return models.GetMostRecentAIFixSession(serviceID)
}
