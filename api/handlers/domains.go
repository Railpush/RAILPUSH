package handlers

import (
	"context"
	"encoding/json"
	"net"
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

func (h *DomainHandler) requiredCNAMETarget(svc *models.Service) string {
	if h == nil || h.Config == nil || svc == nil {
		return ""
	}
	d := strings.ToLower(strings.TrimSpace(h.Config.Deploy.Domain))
	if d == "" || d == "localhost" {
		return ""
	}
	return utils.ServiceDefaultHost(svc.Type, svc.Name, svc.Subdomain, d)
}

func verifyDomainDNS(domain, expectedTarget string) (bool, string, string) {
	domain = strings.TrimSpace(strings.TrimSuffix(strings.ToLower(domain), "."))
	expectedTarget = strings.TrimSpace(strings.TrimSuffix(strings.ToLower(expectedTarget), "."))
	if domain == "" || expectedTarget == "" {
		return false, "", ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	r := net.DefaultResolver

	if cname, err := r.LookupCNAME(ctx, domain); err == nil {
		actual := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(cname)), ".")
		if actual == expectedTarget {
			return true, actual, ""
		}
		return false, actual, "cname does not match expected target"
	}

	// Fallback for apex/root records where users often use A/AAAA instead of CNAME.
	domainIPs, err := r.LookupIPAddr(ctx, domain)
	if err != nil || len(domainIPs) == 0 {
		if err != nil {
			return false, "", err.Error()
		}
		return false, "", "no DNS records found"
	}
	targetIPs, err := r.LookupIPAddr(ctx, expectedTarget)
	if err != nil || len(targetIPs) == 0 {
		if err != nil {
			return false, "", err.Error()
		}
		return false, "", "no DNS records found for expected target"
	}
	set := map[string]struct{}{}
	for _, ip := range targetIPs {
		set[ip.IP.String()] = struct{}{}
	}
	for _, ip := range domainIPs {
		if _, ok := set[ip.IP.String()]; ok {
			return true, ip.IP.String(), ""
		}
	}
	return false, "", "domain does not resolve to expected target"
}

func customDomainStatus(createdAt time.Time, dnsVerified, tlsProvisioned bool) string {
	if tlsProvisioned {
		return "cert_active"
	}
	if dnsVerified && time.Since(createdAt) > 45*time.Minute {
		return "cert_failed"
	}
	if dnsVerified {
		return "cert_provisioning"
	}
	return "pending_dns"
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

	// In Kubernetes mode, update status based on DNS + whether the TLS secret exists.
	if h.Config.Kubernetes.Enabled && len(domains) > 0 {
		if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
			target := h.requiredCNAMETarget(svc)
			for i := range domains {
				ready, _, err := kd.GetCustomDomainTLSInfo(serviceID, domains[i].Domain)
				if err != nil {
					continue
				}
				dnsVerified, _, _ := verifyDomainDNS(domains[i].Domain, target)
				if domains[i].TLSProvisioned != ready || domains[i].Verified != dnsVerified {
					_ = models.SetCustomDomainStatus(serviceID, domains[i].Domain, dnsVerified, ready)
					domains[i].TLSProvisioned = ready
					domains[i].Verified = dnsVerified
				}
			}
		}
	}

	target := h.requiredCNAMETarget(svc)
	resp := make([]map[string]interface{}, 0, len(domains))
	for _, d := range domains {
		dnsVerified := d.Verified
		actual := ""
		verifyErr := ""
		tlsProvisioned := d.TLSProvisioned
		var certExpiresAt interface{} = nil
		autoRenew := false
		if h.Config.Kubernetes.Enabled {
			if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
				if ready, exp, err := kd.GetCustomDomainTLSInfo(serviceID, d.Domain); err == nil {
					tlsProvisioned = ready
					if exp != nil {
						certExpiresAt = exp.Format(time.RFC3339)
					}
					autoRenew = true
				}
			}
		}
		if target != "" {
			ok, act, errMsg := verifyDomainDNS(d.Domain, target)
			dnsVerified = ok
			actual = act
			verifyErr = errMsg
		}
		resp = append(resp, map[string]interface{}{
			"id":                    d.ID,
			"service_id":            d.ServiceID,
			"domain":                d.Domain,
			"verified":              dnsVerified,
			"tls_provisioned":       tlsProvisioned,
			"redirect_target":       d.RedirectTarget,
			"created_at":            d.CreatedAt,
			"status":                customDomainStatus(d.CreatedAt, dnsVerified, tlsProvisioned),
			"required_cname_target": target,
			"dns_resolved_target":   actual,
			"verification_error":    verifyErr,
			"cert_expires_at":       certExpiresAt,
			"auto_renewal":          autoRenew,
		})
	}

	utils.RespondJSON(w, http.StatusOK, resp)
}

func (h *DomainHandler) VerifyCustomDomain(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	domain := strings.ToLower(strings.TrimSpace(mux.Vars(r)["domain"]))
	domain = strings.TrimSuffix(domain, ".")
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
	cd, err := models.GetCustomDomain(serviceID, domain)
	if err != nil || cd == nil {
		utils.RespondError(w, http.StatusNotFound, "custom domain not found")
		return
	}

	target := h.requiredCNAMETarget(svc)
	dnsVerified, actual, verifyErr := verifyDomainDNS(domain, target)
	tlsReady := cd.TLSProvisioned
	var certExpiresAt interface{} = nil
	autoRenew := false
	if h.Config.Kubernetes.Enabled {
		if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
			if ready, exp, err := kd.GetCustomDomainTLSInfo(serviceID, domain); err == nil {
				tlsReady = ready
				if exp != nil {
					certExpiresAt = exp.Format(time.RFC3339)
				}
				autoRenew = true
			}
		}
	}
	_ = models.SetCustomDomainStatus(serviceID, domain, dnsVerified, tlsReady)

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"domain":                domain,
		"required_cname_target": target,
		"dns_verified":          dnsVerified,
		"dns_resolved_target":   actual,
		"tls_provisioned":       tlsReady,
		"status":                customDomainStatus(cd.CreatedAt, dnsVerified, tlsReady),
		"verification_error":    verifyErr,
		"cert_expires_at":       certExpiresAt,
		"auto_renewal":          autoRenew,
	})
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
