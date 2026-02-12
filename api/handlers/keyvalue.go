package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

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
		kv.Plan = "starter"
	}
	if kv.Port == 0 {
		kv.Port = 6379
	}
	if kv.MaxmemoryPolicy == "" {
		kv.MaxmemoryPolicy = "allkeys-lru"
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
		if bc.PaymentMethodLast4 == "" {
			utils.RespondError(w, http.StatusPaymentRequired, "payment method required. Please add a payment method in billing settings.")
			return
		}
	}

	pw, _ := utils.GenerateRandomString(16)
	encrypted, _ := utils.Encrypt(pw, h.Config.Crypto.EncryptionKey)
	kv.EncryptedPassword = encrypted

	if err := models.CreateManagedKeyValue(&kv); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create key-value store: "+err.Error())
		return
	}

	// Add to Stripe subscription for paid plans
	if kv.Plan != "free" && h.Stripe.Enabled() {
		bc, _ := models.GetBillingCustomerByUserID(userID)
		if bc != nil {
			if err := h.Stripe.AddSubscriptionItem(bc, "keyvalue", kv.ID, kv.Name, kv.Plan); err != nil {
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

	// Decrypt password for response
	if kv.EncryptedPassword != "" {
		pw, err := utils.Decrypt(kv.EncryptedPassword, h.Config.Crypto.EncryptionKey)
		if err == nil {
			type KVResponse struct {
				models.ManagedKeyValue
				Password string `json:"password"`
				RedisURL string `json:"redis_url"`
			}
			resp := KVResponse{
				ManagedKeyValue: *kv,
				Password:        pw,
				RedisURL:        "redis://:" + pw + "@" + kv.Host + ":" + intToStr(kv.Port),
			}
			utils.RespondJSON(w, http.StatusOK, resp)
			return
		}
	}

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
		h.Worker.Deployer.RemoveContainer(kv.ContainerID)
	}

	if err := models.DeleteManagedKeyValue(id); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete key-value store")
		return
	}
	services.Audit(kv.WorkspaceID, userID, "keyvalue.deleted", "keyvalue", id, map[string]interface{}{
		"name": kv.Name,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
