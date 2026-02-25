package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
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

	kvs, err := models.ListManagedKeyValuesByWorkspace(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list key-value stores")
		return
	}
	if kvs == nil {
		kvs = []models.ManagedKeyValue{}
	}
	utils.RespondJSON(w, http.StatusOK, kvs)
}

func (h *KeyValueHandler) CreateKeyValue(w http.ResponseWriter, r *http.Request) {
	var kv models.ManagedKeyValue
	if err := json.NewDecoder(r.Body).Decode(&kv); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if kv.Name == "" {
		utils.RespondError(w, http.StatusBadRequest, "name is required")
		return
	}
	if kv.Plan == "" {
		kv.Plan = services.PlanStarter
	}
	if p, ok := services.NormalizePlan(kv.Plan); ok {
		kv.Plan = p
	} else {
		utils.RespondError(w, http.StatusBadRequest, "invalid plan")
		return
	}
	if kv.Port == 0 {
		kv.Port = 6379
	}
	if p, ok := services.NormalizeRedisMaxmemoryPolicy(kv.MaxmemoryPolicy); ok {
		kv.MaxmemoryPolicy = p
	} else {
		utils.RespondError(w, http.StatusBadRequest, "invalid maxmemory_policy")
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
		utils.RespondError(w, http.StatusNotFound, "key-value store not found")
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
		utils.RespondError(w, http.StatusNotFound, "key-value store not found")
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
		utils.RespondError(w, http.StatusNotFound, "key-value store not found")
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
	if !planChanged && !policyChanged {
		utils.RespondJSON(w, http.StatusOK, kv)
		return
	}
	if !planChanged {
		// Only policy changed — still need to re-apply K8s resources, skip billing section.
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

	services.Audit(kv.WorkspaceID, userID, "keyvalue.updated", "keyvalue", kv.ID, map[string]interface{}{
		"plan":              kv.Plan,
		"maxmemory_policy":  kv.MaxmemoryPolicy,
	})

	utils.RespondJSON(w, http.StatusOK, kv)
}

func (h *KeyValueHandler) DeleteKeyValue(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)

	// Get KV to find container and plan
	kv, err := models.GetManagedKeyValue(id)
	if err != nil || kv == nil {
		utils.RespondError(w, http.StatusNotFound, "key-value store not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, kv.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

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
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete key-value store")
		return
	}
	services.Audit(kv.WorkspaceID, userID, "keyvalue.deleted", "keyvalue", id, map[string]interface{}{
		"name": kv.Name,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
