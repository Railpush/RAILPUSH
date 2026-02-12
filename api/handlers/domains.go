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

type DomainHandler struct {
	Config *config.Config
	Worker *services.Worker
}

func NewDomainHandler(cfg *config.Config, worker *services.Worker) *DomainHandler {
	return &DomainHandler{Config: cfg, Worker: worker}
}

func (h *DomainHandler) AddCustomDomain(w http.ResponseWriter, r *http.Request) {
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
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Domain == "" {
		utils.RespondError(w, http.StatusBadRequest, "domain is required")
		return
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if domain == "" {
		utils.RespondError(w, http.StatusBadRequest, "domain is required")
		return
	}
	d := &models.CustomDomain{ServiceID: serviceID, Domain: domain}
	if err := models.CreateCustomDomain(d); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to add domain: "+err.Error())
		return
	}

	// Add Caddy route for the custom domain with all live upstream instances.
	upstreamPorts := []int{}
	if svc.HostPort > 0 {
		upstreamPorts = append(upstreamPorts, svc.HostPort)
	}
	if instances, err := models.ListServiceInstances(serviceID); err == nil {
		for _, inst := range instances {
			if inst.HostPort > 0 {
				upstreamPorts = append(upstreamPorts, inst.HostPort)
			}
		}
	}
	if len(upstreamPorts) > 0 {
		if err := h.Worker.Router.AddRouteUpstreams(domain, upstreamPorts); err != nil {
			_ = models.SetCustomDomainTLSProvisioned(serviceID, domain, false)
			services.Audit(svc.WorkspaceID, userID, "domain.custom_added", "custom_domain", d.ID, map[string]interface{}{
				"domain":          domain,
				"tls_provisioned": false,
				"route_error":     err.Error(),
			})
			utils.RespondJSON(w, http.StatusCreated, d)
			return
		}
		_ = models.SetCustomDomainTLSProvisioned(serviceID, domain, true)
	}
	services.Audit(svc.WorkspaceID, userID, "domain.custom_added", "custom_domain", d.ID, map[string]interface{}{
		"domain":          domain,
		"tls_provisioned": true,
	})

	utils.RespondJSON(w, http.StatusCreated, d)
}

func (h *DomainHandler) ListCustomDomains(w http.ResponseWriter, r *http.Request) {
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
	domains, err := models.ListCustomDomains(serviceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list domains")
		return
	}
	if domains == nil {
		domains = []models.CustomDomain{}
	}
	utils.RespondJSON(w, http.StatusOK, domains)
}

func (h *DomainHandler) DeleteCustomDomain(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	domain := strings.ToLower(strings.TrimSpace(mux.Vars(r)["domain"]))
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

	// Remove Caddy route
	_ = h.Worker.Router.RemoveRoute(domain)

	if err := models.DeleteCustomDomain(serviceID, domain); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete domain")
		return
	}
	services.Audit(svc.WorkspaceID, userID, "domain.custom_deleted", "custom_domain", serviceID, map[string]interface{}{
		"domain": domain,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
