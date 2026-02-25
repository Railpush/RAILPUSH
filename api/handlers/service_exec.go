package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

func (h *ServiceHandler) ExecServiceCommand(w http.ResponseWriter, r *http.Request) {
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
		Command                 string `json:"command"`
		TimeoutSeconds          int    `json:"timeout_seconds"`
		User                    string `json:"user"`
		AcknowledgeRiskyCommand bool   `json:"acknowledge_risky_command"`
		Reason                  string `json:"reason"`
		MaxOutputBytes          int    `json:"max_output_bytes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Command = strings.TrimSpace(req.Command)
	req.User = strings.TrimSpace(req.User)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Command == "" {
		utils.RespondError(w, http.StatusBadRequest, "command is required")
		return
	}

	timeout := 30 * time.Second
	if req.TimeoutSeconds > 0 {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}
	maxOutputBytes := req.MaxOutputBytes

	apiKeyID := middleware.GetAPIKeyID(r)
	apiKeyScopes := middleware.GetAPIKeyScopes(r)
	clientIP := middleware.ClientIPString(r)

	commandPolicy := services.EvaluateOneOffCommand(req.Command)
	auditDetails := map[string]interface{}{
		"service_id":        svc.ID,
		"command_preview":   services.OneOffCommandPreview(req.Command, 180),
		"command_sha256":    services.OneOffCommandSHA256(req.Command),
		"command_length":    len(req.Command),
		"timeout_seconds":   int(timeout / time.Second),
		"max_output_bytes":  maxOutputBytes,
		"run_as_user":       req.User,
		"risky":             commandPolicy.Risky,
		"risk_reasons":      commandPolicy.RiskReasons,
		"acknowledged_risk": req.AcknowledgeRiskyCommand,
		"reason":            req.Reason,
		"api_key_id":        apiKeyID,
		"api_key_scopes":    apiKeyScopes,
		"client_ip":         clientIP,
		"user_agent":        strings.TrimSpace(r.UserAgent()),
	}

	if commandPolicy.Blocked {
		auditDetails["blocked_reasons"] = commandPolicy.BlockReasons
		services.Audit(svc.WorkspaceID, userID, "service.exec_command_denied", "service", svc.ID, auditDetails)
		utils.RespondJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":           "command blocked by exec policy",
			"blocked_reasons": commandPolicy.BlockReasons,
		})
		return
	}

	if commandPolicy.Risky && apiKeyID != "" && !models.HasAnyAPIKeyScope(apiKeyScopes, models.APIKeyScopeAdmin) {
		auditDetails["blocked_reasons"] = []string{"risky commands via API key require admin scope"}
		services.Audit(svc.WorkspaceID, userID, "service.exec_command_denied", "service", svc.ID, auditDetails)
		utils.RespondJSON(w, http.StatusForbidden, map[string]interface{}{
			"error":        "risky exec command denied for API key",
			"risk_reasons": commandPolicy.RiskReasons,
		})
		return
	}

	if commandPolicy.Risky && !req.AcknowledgeRiskyCommand {
		services.Audit(svc.WorkspaceID, userID, "service.exec_command_denied", "service", svc.ID, auditDetails)
		utils.RespondJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":        "risky exec command requires acknowledge_risky_command=true",
			"risk_reasons": commandPolicy.RiskReasons,
		})
		return
	}

	if req.User != "" && h.Config != nil && h.Config.Kubernetes.Enabled && strings.HasPrefix(strings.TrimSpace(svc.ContainerID), "k8s:") {
		utils.RespondError(w, http.StatusBadRequest, "user override is not supported for kubernetes exec")
		return
	}

	if h.Worker == nil {
		utils.RespondError(w, http.StatusServiceUnavailable, "service exec worker unavailable")
		return
	}

	startedAt := time.Now()
	stdout, stderr, exitCode, timedOut, truncated, execErr := h.Worker.RunServiceExecCommand(svc, req.Command, req.User, timeout, maxOutputBytes)
	durationMS := time.Since(startedAt).Milliseconds()

	auditDetails["duration_ms"] = durationMS
	auditDetails["exit_code"] = exitCode
	auditDetails["timed_out"] = timedOut
	auditDetails["truncated"] = truncated

	if execErr != nil {
		auditDetails["error"] = execErr.Error()
		services.Audit(svc.WorkspaceID, userID, "service.exec_command_failed", "service", svc.ID, auditDetails)
		utils.RespondJSON(w, http.StatusBadGateway, map[string]interface{}{
			"error":   "failed to execute command",
			"details": execErr.Error(),
		})
		return
	}

	status := "ok"
	if timedOut {
		status = "timed_out"
	} else if exitCode != 0 {
		status = "failed"
	}
	auditDetails["status"] = status
	services.Audit(svc.WorkspaceID, userID, "service.exec_command_executed", "service", svc.ID, auditDetails)

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":      status,
		"service_id":  svc.ID,
		"exit_code":   exitCode,
		"stdout":      stdout,
		"stderr":      stderr,
		"duration_ms": durationMS,
		"timed_out":   timedOut,
		"truncated":   truncated,
	})
}
