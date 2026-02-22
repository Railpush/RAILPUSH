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

func isValidHostname(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return false
	}
	if strings.Contains(host, "://") {
		return false
	}
	if strings.ContainsAny(host, "/\\ \t\r\n") {
		return false
	}
	if strings.Contains(host, ":") { // no ports, no IPv6 literals
		return false
	}
	if !strings.Contains(host, ".") {
		return false
	}
	labels := strings.Split(host, ".")
	for _, l := range labels {
		if l == "" || len(l) > 63 {
			return false
		}
		if l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for i := 0; i < len(l); i++ {
			ch := l[i]
			ok := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-'
			if !ok {
				return false
			}
		}
	}
	return true
}

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
		Domain         string `json:"domain"`
		RedirectTarget string `json:"redirect_target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Domain == "" {
		utils.RespondError(w, http.StatusBadRequest, "domain is required")
		return
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	domain = strings.TrimSuffix(domain, ".")
	if domain == "" {
		utils.RespondError(w, http.StatusBadRequest, "domain is required")
		return
	}
	if !isValidHostname(domain) {
		utils.RespondError(w, http.StatusBadRequest, "invalid domain")
		return
	}

	redirectTarget := strings.ToLower(strings.TrimSpace(req.RedirectTarget))
	redirectTarget = strings.TrimSuffix(redirectTarget, ".")
	if redirectTarget != "" && !isValidHostname(redirectTarget) {
		utils.RespondError(w, http.StatusBadRequest, "invalid redirect_target domain")
		return
	}
	// Prevent self-redirect.
	if redirectTarget == domain {
		utils.RespondError(w, http.StatusBadRequest, "redirect_target cannot be the same as the domain")
		return
	}

	// Enforce global uniqueness (also enforced by DB index).
	if existing, err := models.GetCustomDomainByDomain(domain); err == nil && existing != nil {
		if existing.ServiceID == serviceID {
			utils.RespondError(w, http.StatusConflict, "domain already added")
			return
		}
		utils.RespondError(w, http.StatusConflict, "domain already in use")
		return
	}

	if h.Config.Kubernetes.Enabled {
		switch strings.ToLower(strings.TrimSpace(svc.Type)) {
		case "web", "static":
			// ok
		default:
			utils.RespondError(w, http.StatusBadRequest, "custom domains are only supported for web/static services")
			return
		}
	}
	d := &models.CustomDomain{ServiceID: serviceID, Domain: domain, RedirectTarget: redirectTarget}
	if err := models.CreateCustomDomain(d); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to add domain: "+err.Error())
		return
	}

	tlsProvisioned := false
	verified := false

	if h.Config.Kubernetes.Enabled {
		if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
			if redirectTarget != "" {
				// Redirect domain: create ingress with redirect annotation.
				if _, err := kd.UpsertCustomDomainRedirectIngress(svc, domain, redirectTarget); err != nil {
					_ = models.SetCustomDomainStatus(serviceID, domain, false, false)
					services.Audit(svc.WorkspaceID, userID, "domain.custom_added", "custom_domain", d.ID, map[string]interface{}{
						"domain":          domain,
						"redirect_target": redirectTarget,
						"tls_provisioned": false,
						"verified":        false,
						"ingress_error":   err.Error(),
					})
					utils.RespondJSON(w, http.StatusCreated, d)
					return
				}
			} else {
				// Normal domain: create standard proxy ingress.
				if _, err := kd.UpsertCustomDomainIngress(svc, domain); err != nil {
					_ = models.SetCustomDomainStatus(serviceID, domain, false, false)
					services.Audit(svc.WorkspaceID, userID, "domain.custom_added", "custom_domain", d.ID, map[string]interface{}{
						"domain":          domain,
						"tls_provisioned": false,
						"verified":        false,
						"ingress_error":   err.Error(),
					})
					utils.RespondJSON(w, http.StatusCreated, d)
					return
				}
			}
			if ready, err := kd.IsCustomDomainTLSReady(serviceID, domain); err == nil && ready {
				tlsProvisioned = true
				verified = true
				_ = models.SetCustomDomainStatus(serviceID, domain, verified, tlsProvisioned)
				d.Verified = verified
				d.TLSProvisioned = tlsProvisioned
			}
		}
	} else {
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
		if len(upstreamPorts) > 0 && !h.Config.Deploy.DisableRouter && h.Worker != nil && h.Worker.Router != nil {
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
			tlsProvisioned = true
			verified = true
			_ = models.SetCustomDomainStatus(serviceID, domain, verified, tlsProvisioned)
			d.Verified = verified
			d.TLSProvisioned = tlsProvisioned
		}
	}

	services.Audit(svc.WorkspaceID, userID, "domain.custom_added", "custom_domain", d.ID, map[string]interface{}{
		"domain":          domain,
		"redirect_target": redirectTarget,
		"tls_provisioned": tlsProvisioned,
		"verified":        verified,
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

	// In Kubernetes mode, update status based on whether the TLS secret exists.
	if h.Config.Kubernetes.Enabled && len(domains) > 0 {
		if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
			for i := range domains {
				ready, err := kd.IsCustomDomainTLSReady(serviceID, domains[i].Domain)
				if err != nil {
					continue
				}
				verified := ready
				if domains[i].TLSProvisioned != ready || domains[i].Verified != verified {
					_ = models.SetCustomDomainStatus(serviceID, domains[i].Domain, verified, ready)
					domains[i].TLSProvisioned = ready
					domains[i].Verified = verified
				}
			}
		}
	}
	utils.RespondJSON(w, http.StatusOK, domains)
}

func (h *DomainHandler) DeleteCustomDomain(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	domain := strings.ToLower(strings.TrimSpace(mux.Vars(r)["domain"]))
	domain = strings.TrimSuffix(domain, ".")
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

	if h.Config.Kubernetes.Enabled {
		if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
			_ = kd.DeleteCustomDomainIngress(serviceID, domain)
		}
	} else if !h.Config.Deploy.DisableRouter && h.Worker != nil && h.Worker.Router != nil {
		// Remove Caddy route
		_ = h.Worker.Router.RemoveRoute(domain)
	}

	if err := models.DeleteCustomDomain(serviceID, domain); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete domain")
		return
	}
	services.Audit(svc.WorkspaceID, userID, "domain.custom_deleted", "custom_domain", serviceID, map[string]interface{}{
		"domain": domain,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
