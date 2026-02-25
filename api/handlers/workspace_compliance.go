package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/database"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type workspaceCompliancePolicy struct {
	DataResidency               string
	AuditLogRetentionDays       int
	RequireEncryptionAtRest     bool
	RequireMFAForDestructive    bool
	SessionTimeoutMinutes       int
	IPAllowlistRequired         bool
}

type complianceControl struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	Evidence       string `json:"evidence,omitempty"`
	Recommendation string `json:"recommendation,omitempty"`
}

func normalizeComplianceDataResidency(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "global", "any", "any-region":
		return "global"
	case "eu", "eu-only", "eu_only":
		return "eu-only"
	case "us", "us-only", "us_only":
		return "us-only"
	default:
		return ""
	}
}

func getWorkspaceCompliancePolicy(workspaceID string) (workspaceCompliancePolicy, error) {
	policy := workspaceCompliancePolicy{}
	err := database.DB.QueryRow(
		`SELECT COALESCE(compliance_data_residency, 'global'),
		        COALESCE(audit_log_retention_days, 365),
		        COALESCE(compliance_require_encryption_at_rest, false),
		        COALESCE(compliance_require_mfa_for_destructive, false),
		        COALESCE(compliance_session_timeout_minutes, 30),
		        COALESCE(compliance_ip_allowlist_required, false)
		   FROM workspaces
		  WHERE id=$1`,
		workspaceID,
	).Scan(
		&policy.DataResidency,
		&policy.AuditLogRetentionDays,
		&policy.RequireEncryptionAtRest,
		&policy.RequireMFAForDestructive,
		&policy.SessionTimeoutMinutes,
		&policy.IPAllowlistRequired,
	)
	if err != nil {
		return workspaceCompliancePolicy{}, err
	}
	if normalizeComplianceDataResidency(policy.DataResidency) == "" {
		policy.DataResidency = "global"
	}
	if policy.AuditLogRetentionDays <= 0 {
		policy.AuditLogRetentionDays = defaultWorkspaceAuditLogRetentionDays
	}
	if policy.SessionTimeoutMinutes <= 0 {
		policy.SessionTimeoutMinutes = 30
	}
	return policy, nil
}

func (h *WorkspaceHandler) GetCompliancePolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	ws, err := models.GetWorkspace(workspaceID)
	if err != nil || ws == nil {
		utils.RespondError(w, http.StatusNotFound, "workspace not found")
		return
	}

	policy, err := getWorkspaceCompliancePolicy(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load compliance policy")
		return
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"workspace_id":                  workspaceID,
		"data_residency":               policy.DataResidency,
		"audit_log_retention":          formatRetentionDays(policy.AuditLogRetentionDays),
		"audit_log_retention_days":     policy.AuditLogRetentionDays,
		"require_encryption_at_rest":   policy.RequireEncryptionAtRest,
		"require_mfa_for_destructive":  policy.RequireMFAForDestructive,
		"session_timeout_minutes":      policy.SessionTimeoutMinutes,
		"ip_allowlist_required":        policy.IPAllowlistRequired,
		"phase":                        "phase_1",
		"enforced_controls":            []string{"audit_log_retention", "workspace_policy_guardrails"},
		"advisory_controls":            []string{"encryption_at_rest", "mfa_for_destructive", "ip_allowlist_required", "session_timeout_minutes", "data_residency"},
	})
}

func (h *WorkspaceHandler) UpdateCompliancePolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	ws, err := models.GetWorkspace(workspaceID)
	if err != nil || ws == nil {
		utils.RespondError(w, http.StatusNotFound, "workspace not found")
		return
	}

	current, err := getWorkspaceCompliancePolicy(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load compliance policy")
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&payload); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	policy := current
	updated := false

	if raw, ok := payload["data_residency"]; ok {
		strVal, ok := raw.(string)
		if !ok {
			utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "data_residency", Message: "must be one of global, eu-only, us-only"}})
			return
		}
		normalized := normalizeComplianceDataResidency(strVal)
		if normalized == "" {
			utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "data_residency", Message: "must be one of global, eu-only, us-only"}})
			return
		}
		policy.DataResidency = normalized
		updated = true
	}

	if v, ok, err := parseRetentionDaysField(payload, "audit_log_retention", 30, 3650); err != nil {
		utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "audit_log_retention", Message: err.Error()}})
		return
	} else if ok {
		policy.AuditLogRetentionDays = v
		updated = true
	}

	if raw, ok := payload["require_encryption_at_rest"]; ok {
		v, ok := raw.(bool)
		if !ok {
			utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "require_encryption_at_rest", Message: "must be a boolean"}})
			return
		}
		policy.RequireEncryptionAtRest = v
		updated = true
	}

	if raw, ok := payload["require_mfa_for_destructive"]; ok {
		v, ok := raw.(bool)
		if !ok {
			utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "require_mfa_for_destructive", Message: "must be a boolean"}})
			return
		}
		policy.RequireMFAForDestructive = v
		updated = true
	}

	if raw, ok := payload["session_timeout_minutes"]; ok {
		switch v := raw.(type) {
		case float64:
			if math.Trunc(v) != v {
				utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "session_timeout_minutes", Message: "must be a whole number between 5 and 1440"}})
				return
			}
			policy.SessionTimeoutMinutes = int(v)
		case int:
			policy.SessionTimeoutMinutes = v
		case int64:
			policy.SessionTimeoutMinutes = int(v)
		default:
			utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "session_timeout_minutes", Message: "must be a whole number between 5 and 1440"}})
			return
		}
		if policy.SessionTimeoutMinutes < 5 || policy.SessionTimeoutMinutes > 1440 {
			utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "session_timeout_minutes", Message: "must be between 5 and 1440"}})
			return
		}
		updated = true
	}

	if raw, ok := payload["ip_allowlist_required"]; ok {
		v, ok := raw.(bool)
		if !ok {
			utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "ip_allowlist_required", Message: "must be a boolean"}})
			return
		}
		policy.IPAllowlistRequired = v
		updated = true
	}

	if !updated {
		utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "request", Message: "at least one compliance field must be provided"}})
		return
	}

	if _, err := database.DB.Exec(
		`UPDATE workspaces
		    SET compliance_data_residency=$1,
		        audit_log_retention_days=$2,
		        compliance_require_encryption_at_rest=$3,
		        compliance_require_mfa_for_destructive=$4,
		        compliance_session_timeout_minutes=$5,
		        compliance_ip_allowlist_required=$6
		  WHERE id=$7`,
		policy.DataResidency,
		policy.AuditLogRetentionDays,
		policy.RequireEncryptionAtRest,
		policy.RequireMFAForDestructive,
		policy.SessionTimeoutMinutes,
		policy.IPAllowlistRequired,
		workspaceID,
	); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update compliance policy")
		return
	}

	services.Audit(workspaceID, userID, "workspace.compliance_updated", "workspace", workspaceID, map[string]interface{}{
		"data_residency":              policy.DataResidency,
		"audit_log_retention_days":    policy.AuditLogRetentionDays,
		"require_encryption_at_rest":  policy.RequireEncryptionAtRest,
		"require_mfa_for_destructive": policy.RequireMFAForDestructive,
		"session_timeout_minutes":     policy.SessionTimeoutMinutes,
		"ip_allowlist_required":       policy.IPAllowlistRequired,
	})

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":                      "updated",
		"workspace_id":                workspaceID,
		"data_residency":              policy.DataResidency,
		"audit_log_retention":         formatRetentionDays(policy.AuditLogRetentionDays),
		"audit_log_retention_days":    policy.AuditLogRetentionDays,
		"require_encryption_at_rest":  policy.RequireEncryptionAtRest,
		"require_mfa_for_destructive": policy.RequireMFAForDestructive,
		"session_timeout_minutes":     policy.SessionTimeoutMinutes,
		"ip_allowlist_required":       policy.IPAllowlistRequired,
	})
}

func (h *WorkspaceHandler) GetComplianceReport(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	ws, err := models.GetWorkspace(workspaceID)
	if err != nil || ws == nil {
		utils.RespondError(w, http.StatusNotFound, "workspace not found")
		return
	}

	framework := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("framework")))
	if framework == "" {
		framework = "soc2"
	}
	if framework != "soc2" && framework != "hipaa" && framework != "gdpr" {
		utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "framework", Message: "must be one of soc2, hipaa, gdpr"}})
		return
	}

	policy, err := getWorkspaceCompliancePolicy(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load compliance policy")
		return
	}

	controls := make([]complianceControl, 0, 8)
	addControl := func(id, name, status, evidence, recommendation string) {
		controls = append(controls, complianceControl{
			ID:             id,
			Name:           name,
			Status:         status,
			Evidence:       evidence,
			Recommendation: recommendation,
		})
	}

	switch framework {
	case "soc2":
		if policy.RequireMFAForDestructive {
			addControl("CC6.1", "Logical access safeguards", "met", "Destructive operations can require MFA policy at workspace level.", "")
		} else {
			addControl("CC6.1", "Logical access safeguards", "gap", "Workspace policy does not currently require MFA for destructive actions.", "Set require_mfa_for_destructive=true in workspace compliance policy.")
		}
		if policy.AuditLogRetentionDays >= 365 {
			addControl("CC7.2", "Audit trail retention", "met", fmt.Sprintf("Audit logs retained for %d days.", policy.AuditLogRetentionDays), "")
		} else {
			addControl("CC7.2", "Audit trail retention", "partial", fmt.Sprintf("Audit logs retained for %d days.", policy.AuditLogRetentionDays), "Increase audit_log_retention to at least 365d.")
		}
		if policy.RequireEncryptionAtRest {
			addControl("CC6.7", "Encryption baseline", "met", "Workspace policy requires encryption-at-rest posture for managed resources.", "")
		} else {
			addControl("CC6.7", "Encryption baseline", "partial", "Encryption requirement is not enforced at workspace policy level.", "Set require_encryption_at_rest=true.")
		}
	case "hipaa":
		if policy.SessionTimeoutMinutes <= 30 {
			addControl("164.312(a)(2)(iii)", "Automatic logoff", "met", fmt.Sprintf("Session timeout policy set to %d minutes.", policy.SessionTimeoutMinutes), "")
		} else {
			addControl("164.312(a)(2)(iii)", "Automatic logoff", "partial", fmt.Sprintf("Session timeout policy set to %d minutes.", policy.SessionTimeoutMinutes), "Lower session_timeout_minutes to 30 or less for stricter access control.")
		}
		if policy.RequireEncryptionAtRest {
			addControl("164.312(a)(2)(iv)", "Encryption and decryption", "met", "Workspace policy requires encryption-at-rest posture.", "")
		} else {
			addControl("164.312(a)(2)(iv)", "Encryption and decryption", "gap", "Encryption-at-rest requirement is not enforced by policy.", "Set require_encryption_at_rest=true.")
		}
		if policy.AuditLogRetentionDays >= 365 {
			addControl("164.312(b)", "Audit controls", "met", fmt.Sprintf("Audit logging retained for %d days.", policy.AuditLogRetentionDays), "")
		} else {
			addControl("164.312(b)", "Audit controls", "partial", fmt.Sprintf("Audit logging retained for %d days.", policy.AuditLogRetentionDays), "Increase audit_log_retention to at least 365d.")
		}
	case "gdpr":
		if policy.DataResidency == "eu-only" {
			addControl("GDPR-44", "Cross-border transfer controls", "met", "Workspace policy is restricted to EU-only data residency.", "")
		} else {
			addControl("GDPR-44", "Cross-border transfer controls", "gap", fmt.Sprintf("Workspace data residency is set to %s.", policy.DataResidency), "Set data_residency to eu-only for EU processing constraints.")
		}
		if policy.AuditLogRetentionDays >= 365 {
			addControl("GDPR-30", "Processing records retention", "met", fmt.Sprintf("Audit records retained for %d days.", policy.AuditLogRetentionDays), "")
		} else {
			addControl("GDPR-30", "Processing records retention", "partial", fmt.Sprintf("Audit records retained for %d days.", policy.AuditLogRetentionDays), "Increase audit_log_retention to at least 365d.")
		}
		if policy.IPAllowlistRequired {
			addControl("GDPR-32", "Access hardening", "met", "Workspace policy requires network allowlisting posture.", "")
		} else {
			addControl("GDPR-32", "Access hardening", "partial", "IP allowlist requirement is not enforced by workspace policy.", "Set ip_allowlist_required=true.")
		}
	}

	reportStatus := "met"
	for _, c := range controls {
		if c.Status == "gap" {
			reportStatus = "partial"
			break
		}
		if c.Status == "partial" {
			reportStatus = "partial"
		}
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"workspace_id": workspaceID,
		"framework":    framework,
		"status":       reportStatus,
		"phase":        "phase_1",
		"controls":     controls,
		"policy": map[string]interface{}{
			"data_residency":              policy.DataResidency,
			"audit_log_retention_days":    policy.AuditLogRetentionDays,
			"require_encryption_at_rest":  policy.RequireEncryptionAtRest,
			"require_mfa_for_destructive": policy.RequireMFAForDestructive,
			"session_timeout_minutes":     policy.SessionTimeoutMinutes,
			"ip_allowlist_required":       policy.IPAllowlistRequired,
		},
		"notes": []string{
			"This report covers workspace policy controls available in Phase 1.",
			"Data export/deletion workflows and legal artifacts (DPA/BAA/subprocessor registry) are tracked as follow-up compliance work.",
		},
	})
}
