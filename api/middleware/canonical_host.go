package middleware

import (
	"net"
	"net/http"
	"strings"

	"github.com/railpush/api/config"
)

func schemeForRequest(r *http.Request) string {
	if r != nil && r.TLS != nil {
		return "https"
	}
	if r == nil {
		return "http"
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		return "https"
	}
	return "http"
}

func hostAndPort(rawHost string) (string, string) {
	rawHost = strings.TrimSpace(rawHost)
	rawHost = strings.TrimSuffix(rawHost, ".")
	if rawHost == "" {
		return "", ""
	}
	if h, p, err := net.SplitHostPort(rawHost); err == nil {
		return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), ".")), strings.TrimSpace(p)
	}
	// Host header may not contain a port; also tolerate malformed values.
	// This is safe for our use because we're only comparing for known DNS hostnames.
	return strings.ToLower(rawHost), ""
}

// CanonicalHostMiddleware:
// - redirects www.<CONTROL_PLANE_DOMAIN> -> <CONTROL_PLANE_DOMAIN>
// - strips default ports (:443 for https, :80 for http) to avoid ugly canonical URLs
func CanonicalHostMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg == nil || cfg.ControlPlane.Domain == "" || strings.EqualFold(cfg.ControlPlane.Domain, "localhost") {
				next.ServeHTTP(w, r)
				return
			}

			canonical := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(cfg.ControlPlane.Domain), "."))
			scheme := schemeForRequest(r)

			h, port := hostAndPort(r.Host)
			if h == "" {
				next.ServeHTTP(w, r)
				return
			}

			// www -> apex
			if h == "www."+canonical {
				target := scheme + "://" + canonical + r.URL.RequestURI()
				http.Redirect(w, r, target, http.StatusPermanentRedirect)
				return
			}

			// Strip default ports from canonical host (e.g. railpush.com:443).
			if h == canonical {
				if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
					target := scheme + "://" + canonical + r.URL.RequestURI()
					http.Redirect(w, r, target, http.StatusPermanentRedirect)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

