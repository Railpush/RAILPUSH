package handlers

import (
	"log"
	"net/http"

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

	// Check for existing active session
	existing, _ := models.GetActiveAIFixSessionForService(svc.ID)
	if existing != nil {
		utils.RespondError(w, http.StatusConflict, "AI fix already in progress")
		return
	}

	// Check that there's a failed deploy to fix
	failedDeploy, _ := models.GetLastFailedDeploy(svc.ID)
	if failedDeploy == nil {
		utils.RespondError(w, http.StatusBadRequest, "no failed deploy to fix")
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
		if err := aiFixer.AttemptFix(session, h.Worker); err != nil {
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

func getMostRecentAIFixSession(serviceID string) (*models.AIFixSession, error) {
	// Get the most recent non-running session (success, exhausted, error)
	return models.GetMostRecentAIFixSession(serviceID)
}
