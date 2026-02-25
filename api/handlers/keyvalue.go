package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
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

type KeyValueHandler struct {
	Config *config.Config
	Worker *services.Worker
	Stripe *services.StripeService
}

func NewKeyValueHandler(cfg *config.Config, worker *services.Worker, stripe *services.StripeService) *KeyValueHandler {
	return &KeyValueHandler{Config: cfg, Worker: worker, Stripe: stripe}
}

func (h *KeyValueHandler) ListKeyValues(w http.ResponseWriter, r *http.Request) {
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
	pagination, err := parseCursorPagination(r)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	kvs, err := models.ListManagedKeyValuesByWorkspace(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list key-value stores")
		return
	}
	if kvs == nil {
		kvs = []models.ManagedKeyValue{}
	}
	active := kvs[:0]
	for _, item := range kvs {
		if strings.EqualFold(strings.TrimSpace(item.Status), "soft_deleted") {
			continue
		}
		active = append(active, item)
	}
	kvs = active

	filterPlan := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("plan")))
	filterStatus := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	filterName := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("name")))
	filterQuery := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))

	if filterPlan != "" || filterStatus != "" || filterName != "" || filterQuery != "" {
		filtered := kvs[:0]
		for _, item := range kvs {
			if filterPlan != "" && strings.ToLower(strings.TrimSpace(item.Plan)) != filterPlan {
				continue
			}
			if filterStatus != "" && strings.ToLower(strings.TrimSpace(item.Status)) != filterStatus {
				continue
			}
			if filterName != "" && !strings.Contains(strings.ToLower(item.Name), filterName) {
				continue
			}
			if filterQuery != "" {
				haystack := strings.ToLower(strings.Join([]string{
					item.Name,
					item.Host,
					item.Plan,
					item.Status,
					item.MaxmemoryPolicy,
				}, " "))
				if !strings.Contains(haystack, filterQuery) {
					continue
				}
			}
			filtered = append(filtered, item)
		}
		kvs = filtered
	}
	paged, pageMeta := paginateSlice(kvs, pagination)
	if pageMeta != nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"data":       paged,
			"pagination": pageMeta,
		})
		return
	}
	utils.RespondJSON(w, http.StatusOK, paged)
}

func (h *KeyValueHandler) CreateKeyValue(w http.ResponseWriter, r *http.Request) {
	var kv models.ManagedKeyValue
	if err := json.NewDecoder(r.Body).Decode(&kv); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	kv.Name = strings.TrimSpace(kv.Name)
	validationIssues := make([]utils.ValidationIssue, 0, 4)
	if kv.Name == "" {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "name", Message: "is required"})
	}
	if kv.Plan == "" {
		kv.Plan = services.PlanStarter
	}
	if p, ok := services.NormalizePlan(kv.Plan); ok {
		kv.Plan = p
	} else {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "plan", Message: "must be one of free, starter, standard, pro"})
	}
	if kv.Port == 0 {
		kv.Port = 6379
	} else if kv.Port < 0 || kv.Port > 65535 {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "port", Message: "must be between 1 and 65535"})
	}
	if p, ok := services.NormalizeRedisMaxmemoryPolicy(kv.MaxmemoryPolicy); ok {
		kv.MaxmemoryPolicy = p
	} else {
		validationIssues = append(validationIssues, utils.ValidationIssue{Field: "maxmemory_policy", Message: "is invalid"})
	}
	if len(validationIssues) > 0 {
		utils.RespondValidationErrors(w, http.StatusBadRequest, validationIssues)
		return
	}
	kv.Host = "localhost"

	userID := middleware.GetUserID(r)
	if kv.WorkspaceID == "" {
		ws, err := models.GetWorkspaceByOwner(userID)
		if err != nil || ws == nil {
			utils.RespondError(w, http.StatusBadRequest, "no workspace found for user")
			return
		}
		kv.WorkspaceID = ws.ID
	}
	if err := services.EnsureWorkspaceAccess(userID, kv.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Free tier: limit 1 free key-value per workspace
	if kv.Plan == "free" {
		count, err := models.CountResourcesByWorkspaceAndPlan(kv.WorkspaceID, "keyvalue", "free")
		if err == nil && count >= 1 {
			utils.RespondError(w, http.StatusBadRequest, "free tier limit reached: 1 free key-value store per workspace")
			return
		}
	}

	// Paid plan: ensure Stripe customer exists and has payment method
	var billingCustomer *models.BillingCustomer
	if kv.Plan != "free" && h.Stripe.Enabled() {
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

	pw, _ := utils.GenerateRandomString(16)
	encrypted, _ := utils.Encrypt(pw, h.Config.Crypto.EncryptionKey)
	kv.EncryptedPassword = encrypted

	if err := models.CreateManagedKeyValue(&kv); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create key-value store: "+err.Error())
		return
	}
	// In Kubernetes mode, the stable in-cluster endpoint is `sr-kv-<idPrefix>:6379`.
	if h.Config != nil && h.Config.Kubernetes.Enabled {
		internalHost := "sr-kv-" + kv.ID[:8]
		kv.Host = internalHost
		kv.Port = 6379
		_ = models.UpdateManagedKeyValueConnection(kv.ID, 6379, internalHost)
	}

	// Add to Stripe subscription for paid plans
	if kv.Plan != "free" && h.Stripe.Enabled() && billingCustomer != nil {
		if err := h.Stripe.AddSubscriptionItem(billingCustomer, kv.WorkspaceID, "keyvalue", kv.ID, kv.Name, kv.Plan); err != nil {
			log.Printf("Warning: failed to add billing for key-value %s: %v", kv.ID, err)
			models.DeleteManagedKeyValue(kv.ID)
			if errors.Is(err, services.ErrNoDefaultPaymentMethod) {
				utils.RespondError(w, http.StatusPaymentRequired, "payment method required. Please add a default payment method in billing settings.")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, "billing error: "+err.Error())
			return
		}
	}

	// Spin up real Redis container in background
	h.Worker.ProvisionKeyValue(&kv, pw)
	services.Audit(kv.WorkspaceID, userID, "keyvalue.created", "keyvalue", kv.ID, map[string]interface{}{
		"name": kv.Name,
		"plan": kv.Plan,
	})

	utils.RespondJSON(w, http.StatusCreated, kv)
}

func (h *KeyValueHandler) GetKeyValue(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	kv, err := models.GetManagedKeyValue(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if kv == nil {
		respondKeyValueNotFound(w, id)
		return
	}
	if strings.EqualFold(strings.TrimSpace(kv.Status), "soft_deleted") {
		respondKeyValueNotFound(w, id)
		return
	}
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, kv.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	pw := ""
	if kv.EncryptedPassword != "" {
		if decrypted, derr := utils.Decrypt(kv.EncryptedPassword, h.Config.Crypto.EncryptionKey); derr == nil {
			pw = decrypted
		}
	}
	resp := h.keyValueResponse(kv, pw, false)
	utils.RespondJSON(w, http.StatusOK, resp)
}

func (h *KeyValueHandler) RevealKeyValueCredentials(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	kv, err := models.GetManagedKeyValue(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if kv == nil {
		respondKeyValueNotFound(w, id)
		return
	}

	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, kv.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		AcknowledgeSensitiveOutput bool `json:"acknowledge_sensitive_output"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.AcknowledgeSensitiveOutput {
		utils.RespondError(w, http.StatusBadRequest, "acknowledge_sensitive_output must be true")
		return
	}

	if strings.TrimSpace(kv.EncryptedPassword) == "" {
		utils.RespondError(w, http.StatusNotFound, "key-value credentials unavailable")
		return
	}
	pw, err := utils.Decrypt(kv.EncryptedPassword, h.Config.Crypto.EncryptionKey)
	if err != nil || strings.TrimSpace(pw) == "" {
		utils.RespondError(w, http.StatusInternalServerError, "failed to decrypt key-value credentials")
		return
	}

	services.Audit(kv.WorkspaceID, userID, "keyvalue.credentials.revealed", "keyvalue", kv.ID, map[string]interface{}{
		"api_key_id": middleware.GetAPIKeyID(r),
	})

	resp := h.keyValueResponse(kv, pw, true)
	utils.RespondJSON(w, http.StatusOK, resp)
}

func (h *KeyValueHandler) keyValueResponse(kv *models.ManagedKeyValue, password string, revealCredentials bool) map[string]interface{} {
	passwordForURL := "<redacted>"
	if revealCredentials && strings.TrimSpace(password) != "" {
		passwordForURL = password
	}

	resp := map[string]interface{}{
		"id":                  kv.ID,
		"workspace_id":        kv.WorkspaceID,
		"name":                kv.Name,
		"plan":                kv.Plan,
		"container_id":        kv.ContainerID,
		"host":                kv.Host,
		"port":                kv.Port,
		"maxmemory_policy":    kv.MaxmemoryPolicy,
		"status":              kv.Status,
		"created_at":          kv.CreatedAt,
		"redis_url":           "redis://:" + passwordForURL + "@" + kv.Host + ":" + intToStr(kv.Port),
		"credentials_exposed": revealCredentials,
	}
	if revealCredentials {
		resp["password"] = password
	}
	return resp
}

func (h *KeyValueHandler) UpdateKeyValue(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	kv, err := models.GetManagedKeyValue(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if kv == nil {
		respondKeyValueNotFound(w, id)
		return
	}

	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, kv.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	oldPlan := kv.Plan
	if p, ok := services.NormalizePlan(oldPlan); ok {
		oldPlan = p
	} else {
		oldPlan = services.PlanStarter
	}

	planProvided := false
	desiredPlan := oldPlan
	policyProvided := false
	desiredPolicy := kv.MaxmemoryPolicy
	deletionProtectionProvided := false
	deletionProtection := false
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
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
	if v, ok := updates["maxmemory_policy"].(string); ok {
		policyProvided = true
		if p, ok := services.NormalizeRedisMaxmemoryPolicy(v); ok {
			desiredPolicy = p
		} else {
			utils.RespondError(w, http.StatusBadRequest, "invalid maxmemory_policy")
			return
		}
	}
	if v, ok := updates["deletion_protection"].(bool); ok {
		deletionProtectionProvided = true
		deletionProtection = v
	}

	// Apply maxmemory_policy change (independent of plan change).
	policyChanged := policyProvided && desiredPolicy != kv.MaxmemoryPolicy
	if policyChanged {
		if err := models.UpdateManagedKeyValueMaxmemoryPolicy(kv.ID, desiredPolicy); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to update maxmemory policy")
			return
		}
		kv.MaxmemoryPolicy = desiredPolicy
	}

	planChanged := planProvided && desiredPlan != oldPlan
	if !planChanged && !policyChanged && !deletionProtectionProvided {
		utils.RespondJSON(w, http.StatusOK, kv)
		return
	}
	if !planChanged {
		// Only policy changed — still need to re-apply K8s resources, skip billing section.
		if !policyChanged {
			goto applyProtection
		}
		goto applyKube
	}

	// Free tier: limit 1 free key-value per workspace
	if desiredPlan == services.PlanFree {
		count, err := models.CountResourcesByWorkspaceAndPlan(kv.WorkspaceID, "keyvalue", "free")
		if err == nil && count >= 1 {
			utils.RespondError(w, http.StatusBadRequest, "free tier limit reached: 1 free key-value store per workspace")
			return
		}
	}

	// Gate plan changes on Stripe success so users cannot upgrade resources without billing.
	if h.Stripe != nil && h.Stripe.Enabled() {
		if desiredPlan == services.PlanFree {
			if err := h.Stripe.RemoveSubscriptionItem("keyvalue", kv.ID); err != nil {
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
			if err := h.Stripe.AddSubscriptionItem(bc, kv.WorkspaceID, "keyvalue", kv.ID, kv.Name, desiredPlan); err != nil {
				if errors.Is(err, services.ErrNoDefaultPaymentMethod) {
					utils.RespondError(w, http.StatusPaymentRequired, "payment method required. Please add a default payment method in billing settings.")
					return
				}
				utils.RespondError(w, http.StatusBadGateway, "billing error: "+err.Error())
				return
			}
		}
	}

	kv.Plan = desiredPlan
	if err := models.UpdateManagedKeyValuePlan(kv.ID, desiredPlan); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update key-value plan")
		return
	}

applyKube:
	// Best-effort: apply Kubernetes resource updates immediately.
	if h.Config != nil && h.Config.Kubernetes.Enabled && strings.HasPrefix(strings.TrimSpace(kv.ContainerID), "k8s:") {
		if pw, err := utils.Decrypt(kv.EncryptedPassword, h.Config.Crypto.EncryptionKey); err == nil && strings.TrimSpace(pw) != "" {
			var kd *services.KubeDeployer
			if h.Worker != nil {
				if k, err := h.Worker.GetKubeDeployer(); err == nil {
					kd = k
				}
			}
			if kd == nil {
				if k, err := services.NewKubeDeployer(h.Config); err == nil {
					kd = k
				}
			}
			if kd != nil {
				if _, err := kd.EnsureManagedKeyValue(kv, pw); err != nil {
					log.Printf("WARNING: k8s managed keyvalue update failed kv=%s: %v", kv.ID, err)
				}
			}
		}
	}

	if deletionProtectionProvided {
		if err := models.SetResourceDeletionProtection("keyvalue", kv.ID, kv.WorkspaceID, kv.Name, deletionProtection); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to update deletion protection")
			return
		}
	}

	goto audit

applyProtection:
	if deletionProtectionProvided {
		if err := models.SetResourceDeletionProtection("keyvalue", kv.ID, kv.WorkspaceID, kv.Name, deletionProtection); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to update deletion protection")
			return
		}
	}

audit:
	var deletionProtectionAudit interface{}
	if deletionProtectionProvided {
		deletionProtectionAudit = deletionProtection
	}
	services.Audit(kv.WorkspaceID, userID, "keyvalue.updated", "keyvalue", kv.ID, map[string]interface{}{
		"plan":                kv.Plan,
		"maxmemory_policy":    kv.MaxmemoryPolicy,
		"deletion_protection": deletionProtectionAudit,
	})

	utils.RespondJSON(w, http.StatusOK, kv)
}

func (h *KeyValueHandler) DeleteKeyValue(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)

	kv, err := models.GetManagedKeyValue(id)
	if err != nil || kv == nil {
		respondKeyValueNotFound(w, id)
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, kv.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	state, err := models.GetResourceDeletionState("keyvalue", kv.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to read deletion state")
		return
	}
	if state != nil && state.DeletionProtection {
		utils.RespondError(w, http.StatusForbidden, "deletion protection is enabled for this key-value store")
		return
	}

	var req destructiveDeleteRequest
	if err := decodeOptionalJSONBody(w, r, &req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token := strings.TrimSpace(req.ConfirmationToken)
	if token == "" {
		confirmationToken, expiresAt, err := models.IssueResourceDeletionToken("keyvalue", kv.ID, kv.WorkspaceID, kv.Name, deleteConfirmationTTL)
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
	if err := models.VerifyResourceDeletionToken("keyvalue", kv.ID, token); err != nil {
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
			utils.RespondError(w, http.StatusConflict, "key-value store must be soft-deleted before hard delete")
			return
		}
		if state.PurgeAfter != nil && time.Now().Before(*state.PurgeAfter) {
			utils.RespondError(w, http.StatusConflict, "key-value store is in recovery window; hard delete available after "+state.PurgeAfter.Format(time.RFC3339))
			return
		}
		if err := h.hardDeleteKeyValue(kv); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to delete key-value store")
			return
		}
		_ = models.DeleteResourceDeletionState("keyvalue", kv.ID)
		services.Audit(kv.WorkspaceID, userID, "keyvalue.deleted", "keyvalue", id, map[string]interface{}{
			"name": kv.Name,
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

	purgeAfter, err := models.MarkResourceSoftDeleted("keyvalue", kv.ID, kv.WorkspaceID, kv.Name, softDeleteRecoveryWindow)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to soft-delete key-value store")
		return
	}
	_ = models.UpdateManagedKeyValueStatus(kv.ID, "soft_deleted", kv.ContainerID)
	services.Audit(kv.WorkspaceID, userID, "keyvalue.soft_deleted", "keyvalue", id, map[string]interface{}{
		"name":        kv.Name,
		"purge_after": purgeAfter,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":                "soft_deleted",
		"purge_after":           purgeAfter,
		"recovery_window_hours": int(softDeleteRecoveryWindow / time.Hour),
	})
}

func (h *KeyValueHandler) RestoreKeyValue(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	kv, err := models.GetManagedKeyValue(id)
	if err != nil || kv == nil {
		respondKeyValueNotFound(w, id)
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, kv.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	state, err := models.GetResourceDeletionState("keyvalue", kv.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to read deletion state")
		return
	}
	if state == nil || state.DeletedAt == nil {
		utils.RespondError(w, http.StatusBadRequest, "key-value store is not soft-deleted")
		return
	}
	if err := models.RestoreSoftDeletedResource("keyvalue", kv.ID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to restore key-value store")
		return
	}
	_ = models.UpdateManagedKeyValueStatus(kv.ID, "available", kv.ContainerID)
	services.Audit(kv.WorkspaceID, userID, "keyvalue.restored", "keyvalue", id, map[string]interface{}{
		"name": kv.Name,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"status": "restored"})
}

func (h *KeyValueHandler) hardDeleteKeyValue(kv *models.ManagedKeyValue) error {
	id := kv.ID

	// Remove from Stripe subscription before deleting
	if kv.Plan != "free" && h.Stripe.Enabled() {
		if err := h.Stripe.RemoveSubscriptionItem("keyvalue", id); err != nil {
			log.Printf("Warning: failed to remove billing for key-value %s: %v", id, err)
		}
	}
	if kv.ContainerID != "" {
		// Legacy docker mode only; in k8s mode we delete Kubernetes resources instead.
		if h.Config == nil || !h.Config.Kubernetes.Enabled {
			h.Worker.Deployer.RemoveContainer(kv.ContainerID)
		}
	}
	if h.Config != nil && h.Config.Kubernetes.Enabled && h.Worker != nil {
		if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
			_ = kd.DeleteManagedKeyValueResources(kv.ID)
		}
	}

	// Remove any blueprint links to this key-value store to avoid stale resources in blueprint UIs.
	_ = models.DeleteBlueprintResourcesByResource("keyvalue", id)
	if err := models.DeleteManagedKeyValue(id); err != nil {
		return err
	}
	return nil
}
