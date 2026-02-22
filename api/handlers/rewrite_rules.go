package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type RewriteRuleHandler struct {
	Config *config.Config
	Worker *services.Worker
}

func NewRewriteRuleHandler(cfg *config.Config, worker *services.Worker) *RewriteRuleHandler {
	return &RewriteRuleHandler{Config: cfg, Worker: worker}
}

func (h *RewriteRuleHandler) AddRewriteRule(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
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
		SourcePath    string `json:"source_path"`
		DestServiceID string `json:"dest_service_id"`
		DestPath      string `json:"dest_path"`
		RuleType      string `json:"rule_type"`
		Priority      int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SourcePath == "" {
		utils.RespondError(w, http.StatusBadRequest, "source_path is required")
		return
	}
	if req.DestServiceID == "" {
		utils.RespondError(w, http.StatusBadRequest, "dest_service_id is required")
		return
	}

	// Normalize paths.
	req.SourcePath = "/" + strings.TrimLeft(req.SourcePath, "/")
	if req.DestPath == "" {
		req.DestPath = req.SourcePath
	} else {
		req.DestPath = "/" + strings.TrimLeft(req.DestPath, "/")
	}
	if req.RuleType == "" {
		req.RuleType = "proxy"
	}
	if req.RuleType != "proxy" && req.RuleType != "redirect" {
		utils.RespondError(w, http.StatusBadRequest, "rule_type must be 'proxy' or 'redirect'")
		return
	}

	// Verify the destination service exists and is in the same workspace.
	destSvc, err := models.GetService(req.DestServiceID)
	if err != nil || destSvc == nil {
		utils.RespondError(w, http.StatusBadRequest, "destination service not found")
		return
	}
	if destSvc.WorkspaceID != svc.WorkspaceID {
		utils.RespondError(w, http.StatusBadRequest, "destination service must be in the same workspace")
		return
	}

	rule := &models.RewriteRule{
		ServiceID:     serviceID,
		SourcePath:    req.SourcePath,
		DestServiceID: req.DestServiceID,
		DestPath:      req.DestPath,
		RuleType:      req.RuleType,
		Priority:      req.Priority,
	}
	if err := models.CreateRewriteRule(rule); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create rewrite rule: "+err.Error())
		return
	}

	// Reconcile the K8s ingress to include the new rewrite path.
	if h.Config.Kubernetes.Enabled {
		if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
			if err := kd.ReconcileRewriteRuleIngresses(svc); err != nil {
				// Log but don't fail — the rule is saved, next deploy will pick it up.
				services.Audit(svc.WorkspaceID, userID, "rewrite_rule.create_ingress_error", "rewrite_rule", rule.ID, map[string]interface{}{
					"error": err.Error(),
				})
			}
		}
	}

	// Re-query to get joined dest_service_name.
	if created, err := models.GetRewriteRule(rule.ID); err == nil && created != nil {
		rule = created
	}

	services.Audit(svc.WorkspaceID, userID, "rewrite_rule.created", "rewrite_rule", rule.ID, map[string]interface{}{
		"source_path":    rule.SourcePath,
		"dest_service":   rule.DestServiceID,
		"dest_path":      rule.DestPath,
		"rule_type":      rule.RuleType,
	})
	utils.RespondJSON(w, http.StatusCreated, rule)
}

func (h *RewriteRuleHandler) ListRewriteRules(w http.ResponseWriter, r *http.Request) {
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
	rules, err := models.ListRewriteRules(serviceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list rewrite rules")
		return
	}
	if rules == nil {
		rules = []models.RewriteRule{}
	}
	utils.RespondJSON(w, http.StatusOK, rules)
}

func (h *RewriteRuleHandler) DeleteRewriteRule(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	ruleID := mux.Vars(r)["ruleId"]
	userID := middleware.GetUserID(r)
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Verify the rule belongs to this service.
	rule, err := models.GetRewriteRule(ruleID)
	if err != nil || rule == nil {
		utils.RespondError(w, http.StatusNotFound, "rule not found")
		return
	}
	if rule.ServiceID != serviceID {
		utils.RespondError(w, http.StatusNotFound, "rule not found")
		return
	}

	if err := models.DeleteRewriteRule(ruleID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete rule")
		return
	}

	// Reconcile K8s ingresses (will remove the old path-based ingress).
	if h.Config.Kubernetes.Enabled {
		if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
			_ = kd.ReconcileRewriteRuleIngresses(svc)
		}
	}

	services.Audit(svc.WorkspaceID, userID, "rewrite_rule.deleted", "rewrite_rule", ruleID, map[string]interface{}{
		"source_path": rule.SourcePath,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
