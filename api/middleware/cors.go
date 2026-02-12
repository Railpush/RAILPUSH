package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/railpush/api/config"
)

func normalizeOrigin(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return "", false
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), true
}

func buildAllowedOrigins(cfg *config.Config) map[string]struct{} {
	out := map[string]struct{}{}
	add := func(origin string) {
		if normalized, ok := normalizeOrigin(origin); ok {
			out[normalized] = struct{}{}
		}
	}

	if cfg != nil {
		for _, origin := range cfg.CORS.AllowedOrigins {
			add(origin)
		}
		domain := strings.TrimSpace(cfg.Deploy.Domain)
		if domain != "" && !strings.EqualFold(domain, "localhost") {
			add("https://" + domain)
			add("https://www." + domain)
			add("http://" + domain)
			add("http://www." + domain)
		}
	}

	add("http://localhost:3000")
	add("http://localhost:5173")
	add("http://127.0.0.1:3000")
	add("http://127.0.0.1:5173")
	return out
}

func CORSMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	allowedOrigins := buildAllowedOrigins(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))

			if origin != "" {
				normalizedOrigin, ok := normalizeOrigin(origin)
				if ok {
					if _, allowed := allowedOrigins[normalizedOrigin]; allowed {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						w.Header().Set("Access-Control-Allow-Credentials", "true")
						w.Header().Set("Vary", "Origin")
					} else if r.Method == http.MethodOptions {
						http.Error(w, "origin not allowed", http.StatusForbidden)
						return
					}
				}
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			w.Header().Set("Access-Control-Max-Age", "86400")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
