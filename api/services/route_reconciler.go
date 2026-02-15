package services

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"
)

type RouteReconciler struct {
	Config   *config.Config
	Router   *Router
	interval time.Duration
	stopCh   chan struct{}
}

func NewRouteReconciler(cfg *config.Config, router *Router) *RouteReconciler {
	return &RouteReconciler{
		Config:   cfg,
		Router:   router,
		interval: 45 * time.Second,
		stopCh:   make(chan struct{}),
	}
}

func (rr *RouteReconciler) Start() {
	if rr == nil || rr.Router == nil || !rr.enabled() {
		return
	}

	go func() {
		rr.reconcileOnce()
		ticker := time.NewTicker(rr.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				rr.reconcileOnce()
			case <-rr.stopCh:
				return
			}
		}
	}()

	log.Printf("Route reconciler started (interval=%s)", rr.interval)
}

func (rr *RouteReconciler) Stop() {
	if rr == nil {
		return
	}
	select {
	case <-rr.stopCh:
		return
	default:
		close(rr.stopCh)
	}
}

func (rr *RouteReconciler) enabled() bool {
	if rr.Config == nil {
		return false
	}
	if rr.Config.Deploy.DisableRouter {
		return false
	}
	domain := strings.ToLower(strings.TrimSpace(rr.Config.Deploy.Domain))
	return domain != "" && domain != "localhost"
}

func isRoutableServiceType(serviceType string) bool {
	switch serviceType {
	case "web", "static", "pserv":
		return true
	default:
		return false
	}
}

func collectUpstreamPorts(serviceID string, hostPort int) []int {
	set := map[int]struct{}{}
	if hostPort > 0 {
		set[hostPort] = struct{}{}
	}
	instances, err := models.ListServiceInstances(serviceID)
	if err == nil {
		for _, inst := range instances {
			if inst.HostPort > 0 {
				set[inst.HostPort] = struct{}{}
			}
		}
	}

	out := make([]int, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

func (rr *RouteReconciler) reconcileOnce() {
	if !rr.enabled() {
		return
	}

	if err := rr.Router.EnsureDynamicServer(); err != nil {
		log.Printf("route reconcile: ensure dynamic server failed: %v", err)
	}

	existingHosts, err := rr.Router.ListRouteHosts()
	if err != nil {
		log.Printf("route reconcile: failed to read current routes: %v", err)
		existingHosts = map[string]struct{}{}
	}

	services, err := models.ListServices("")
	if err != nil {
		log.Printf("route reconcile: failed to list services: %v", err)
		return
	}

	deployDomain := strings.ToLower(strings.TrimSpace(rr.Config.Deploy.Domain))
	for _, svc := range services {
		if !isRoutableServiceType(svc.Type) || svc.IsSuspended {
			continue
		}

		upstreams := collectUpstreamPorts(svc.ID, svc.HostPort)
		if len(upstreams) == 0 {
			continue
		}

		defaultHost := fmt.Sprintf("%s.%s", utils.ServiceHostLabel(svc.Name, svc.Subdomain), deployDomain)
		if _, exists := existingHosts[defaultHost]; !exists {
			if err := rr.Router.AddRouteUpstreams(defaultHost, upstreams); err != nil {
				log.Printf("route reconcile: failed to add service route for %s: %v", defaultHost, err)
			} else {
				existingHosts[defaultHost] = struct{}{}
			}
		}

		customDomains, err := models.ListCustomDomains(svc.ID)
		if err != nil {
			log.Printf("route reconcile: failed to list custom domains for service %s: %v", svc.ID, err)
			continue
		}
		for _, customDomain := range customDomains {
			host := strings.ToLower(strings.TrimSpace(customDomain.Domain))
			if host == "" {
				continue
			}

			if _, exists := existingHosts[host]; exists {
				_ = models.SetCustomDomainTLSProvisioned(svc.ID, host, true)
				continue
			}

			if err := rr.Router.AddRouteUpstreams(host, upstreams); err != nil {
				log.Printf("route reconcile: failed to add custom domain route for %s: %v", host, err)
				_ = models.SetCustomDomainTLSProvisioned(svc.ID, host, false)
				continue
			}

			existingHosts[host] = struct{}{}
			_ = models.SetCustomDomainTLSProvisioned(svc.ID, host, true)
		}
	}
}
