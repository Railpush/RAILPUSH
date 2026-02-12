package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/railpush/api/config"
)

type Router struct {
	Config *config.Config
}

func NewRouter(cfg *config.Config) *Router {
	return &Router{Config: cfg}
}

func (rt *Router) putJSON(url string, payload interface{}) error {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (rt *Router) ensureRouteList(serverName string) error {
	url := fmt.Sprintf("%s/config/apps/http/servers/%s/routes", rt.Config.Deploy.CaddyAPIURL, serverName)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	bodyText := strings.TrimSpace(string(respBody))

	if resp.StatusCode == 200 {
		if bodyText == "" || bodyText == "null" {
			delReq, _ := http.NewRequest("DELETE", url, nil)
			delResp, delErr := http.DefaultClient.Do(delReq)
			if delErr == nil && delResp != nil {
				delResp.Body.Close()
			}
			if err := rt.putJSON(url, []interface{}{}); err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "invalid traversal path") {
					return nil
				}
				return err
			}
			return nil
		}
		return nil
	}

	// Some Caddy builds reject traversal to nested route-list paths.
	// In this case route operations can still succeed via POST/DELETE calls.
	if resp.StatusCode == 400 && strings.Contains(strings.ToLower(bodyText), "invalid traversal path") {
		return nil
	}

	if resp.StatusCode == 404 {
		if err := rt.putJSON(url, []interface{}{}); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "invalid traversal path") {
				return nil
			}
			return err
		}
		return nil
	}

	return fmt.Errorf("caddy returned %d: %s", resp.StatusCode, bodyText)
}

func extractRouteHosts(raw json.RawMessage) []string {
	var route map[string]interface{}
	if err := json.Unmarshal(raw, &route); err != nil {
		return nil
	}

	matches, ok := route["match"].([]interface{})
	if !ok || len(matches) == 0 {
		return nil
	}

	var hosts []string
	for _, matchItem := range matches {
		matchMap, ok := matchItem.(map[string]interface{})
		if !ok {
			continue
		}
		hostList, ok := matchMap["host"].([]interface{})
		if !ok {
			continue
		}
		for _, h := range hostList {
			if host, ok := h.(string); ok && host != "" {
				hosts = append(hosts, strings.ToLower(strings.TrimSpace(host)))
			}
		}
	}
	return hosts
}

func (rt *Router) listRouteHosts(serverName string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	url := fmt.Sprintf("%s/config/apps/http/servers/%s/routes", rt.Config.Deploy.CaddyAPIURL, serverName)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	bodyText := strings.TrimSpace(string(respBody))
	lowerBody := strings.ToLower(bodyText)

	if resp.StatusCode >= 400 {
		if resp.StatusCode == 404 {
			return out, nil
		}
		if strings.Contains(lowerBody, "invalid traversal path") {
			return out, nil
		}
		return nil, fmt.Errorf("caddy returned %d: %s", resp.StatusCode, bodyText)
	}

	if bodyText == "" || bodyText == "null" {
		return out, nil
	}

	var routes []json.RawMessage
	if err := json.Unmarshal(respBody, &routes); err != nil {
		return nil, err
	}
	for _, raw := range routes {
		for _, host := range extractRouteHosts(raw) {
			out[host] = struct{}{}
		}
	}
	return out, nil
}

// ListRouteHosts returns hostnames currently present in Caddy route tables.
func (rt *Router) ListRouteHosts() (map[string]struct{}, error) {
	out := map[string]struct{}{}
	for _, serverName := range []string{"services", "srv0"} {
		hosts, err := rt.listRouteHosts(serverName)
		if err != nil {
			return nil, err
		}
		for host := range hosts {
			out[host] = struct{}{}
		}
	}
	return out, nil
}

// EnsureDynamicServer creates the "services" server in Caddy's config
// if it doesn't already exist. This server handles default and custom domains.
func (rt *Router) EnsureDynamicServer() error {
	// Check if the server already exists
	url := fmt.Sprintf("%s/config/apps/http/servers/services", rt.Config.Deploy.CaddyAPIURL)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("caddy admin unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		// Server already exists; route list is managed by AddRoute/RemoveRoute calls.
		return nil
	}

	// Create the dynamic server for subdomain routing
	server := map[string]interface{}{
		"listen":                  []string{":443"},
		"routes":                  []interface{}{},
		"tls_connection_policies": []map[string]interface{}{{}},
		"automatic_https": map[string]interface{}{
			"disable": false,
		},
	}

	if err := rt.putJSON(url, server); err != nil {
		return fmt.Errorf("failed to create dynamic server: %w", err)
	}

	log.Println("Created Caddy dynamic server for subdomain routing")
	return nil
}

func (rt *Router) AddRoute(domain string, port int) error {
	return rt.AddRouteUpstreams(domain, []int{port})
}

func (rt *Router) AddRouteUpstreams(domain string, ports []int) error {
	if len(ports) == 0 {
		return fmt.Errorf("at least one upstream port is required")
	}
	upstreams := make([]map[string]string, 0, len(ports))
	for _, p := range ports {
		if p <= 0 {
			continue
		}
		upstreams = append(upstreams, map[string]string{"dial": fmt.Sprintf("localhost:%d", p)})
	}
	if len(upstreams) == 0 {
		return fmt.Errorf("no valid upstream ports")
	}

	route := map[string]interface{}{
		"match": []map[string]interface{}{
			{"host": []string{domain}},
		},
		"handle": []map[string]interface{}{
			{
				"handler":   "reverse_proxy",
				"upstreams": upstreams,
			},
		},
	}
	body, _ := json.Marshal(route)

	// Upsert semantics: remove old host route if it exists before adding fresh.
	_ = rt.RemoveRoute(domain)

	// Try adding to the dynamic "services" server first
	_ = rt.ensureRouteList("services")
	url := fmt.Sprintf("%s/config/apps/http/servers/services/routes", rt.Config.Deploy.CaddyAPIURL)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		// Fallback to srv0 if dynamic server doesn't exist
		_ = rt.ensureRouteList("srv0")
		url = fmt.Sprintf("%s/config/apps/http/servers/srv0/routes", rt.Config.Deploy.CaddyAPIURL)
		resp, err = http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			return err
		}
	}
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lowerBody := strings.ToLower(string(respBody))

		// Some Caddy builds disallow direct traversal to `services/routes`.
		// Fallback to srv0 route table.
		if strings.Contains(url, "/services/") &&
			(strings.Contains(lowerBody, "invalid traversal path") || resp.StatusCode == 404) {
			_ = rt.ensureRouteList("srv0")
			url = fmt.Sprintf("%s/config/apps/http/servers/srv0/routes", rt.Config.Deploy.CaddyAPIURL)
			resp, err = http.Post(url, "application/json", bytes.NewReader(body))
			if err != nil {
				return err
			}
			if resp.StatusCode < 400 {
				defer resp.Body.Close()
				log.Printf("Added route: %s -> ports=%v", domain, ports)
				return nil
			}
			respBody, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
			lowerBody = strings.ToLower(string(respBody))
		}

		// Retry once after forcing route-list initialization.
		if strings.Contains(lowerBody, "routes") || strings.Contains(lowerBody, "routelist") {
			if strings.Contains(url, "/services/") {
				_ = rt.ensureRouteList("services")
			} else if strings.Contains(url, "/srv0/") {
				_ = rt.ensureRouteList("srv0")
			}
			resp, err = http.Post(url, "application/json", bytes.NewReader(body))
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("caddy returned %d: %s", resp.StatusCode, string(respBody))
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy returned %d: %s", resp.StatusCode, string(respBody))
	}
	log.Printf("Added route: %s -> ports=%v", domain, ports)
	return nil
}

func (rt *Router) RemoveRoute(domain string) error {
	removeFromServer := func(serverName string) (bool, error) {
		url := fmt.Sprintf("%s/config/apps/http/servers/%s/routes", rt.Config.Deploy.CaddyAPIURL, serverName)
		resp, err := http.Get(url)
		if err != nil {
			return false, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return false, nil
		}

		var routes []json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&routes); err != nil {
			return false, err
		}

		for i, raw := range routes {
			var route map[string]interface{}
			_ = json.Unmarshal(raw, &route)
			matches, ok := route["match"].([]interface{})
			if !ok || len(matches) == 0 {
				continue
			}
			m, ok := matches[0].(map[string]interface{})
			if !ok {
				continue
			}
			hosts, ok := m["host"].([]interface{})
			if !ok || len(hosts) == 0 {
				continue
			}
			host, ok := hosts[0].(string)
			if !ok || host != domain {
				continue
			}

			delURL := fmt.Sprintf("%s/config/apps/http/servers/%s/routes/%d", rt.Config.Deploy.CaddyAPIURL, serverName, i)
			req, _ := http.NewRequest("DELETE", delURL, nil)
			resp2, err := http.DefaultClient.Do(req)
			if err != nil {
				return false, err
			}
			resp2.Body.Close()
			log.Printf("Removed route for: %s (server=%s)", domain, serverName)
			return true, nil
		}
		return false, nil
	}

	if removed, err := removeFromServer("services"); err != nil {
		return err
	} else if removed {
		return nil
	}
	if removed, err := removeFromServer("srv0"); err != nil {
		return err
	} else if removed {
		return nil
	}
	return fmt.Errorf("route not found for domain: %s", domain)
}

func (rt *Router) ReloadConfig() error {
	resp, err := http.Post(rt.Config.Deploy.CaddyAPIURL+"/load", "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
