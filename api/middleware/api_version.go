package middleware

import (
	"net/http"
	"strings"

	"github.com/railpush/api/utils"
	"github.com/railpush/api/versioning"
)

func APIVersionMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requested := strings.TrimSpace(r.Header.Get(versioning.PinHeader))
			source := "header"
			if requested == "" {
				requested = strings.TrimSpace(r.URL.Query().Get(versioning.PinQueryParam))
				source = "query"
			}
			if requested == "" {
				source = "default"
			}

			resolved, ok := versioning.ResolveVersionPin(requested)
			if !ok {
				utils.RespondJSON(w, http.StatusBadRequest, map[string]interface{}{
					"error":          "unsupported api version pin",
					"pin_header":     versioning.PinHeader,
					"pin_query":      versioning.PinQueryParam,
					"supported_pins": versioning.SupportedPins(),
				})
				return
			}

			resolved.Source = source
			w.Header().Set("X-RailPush-API-Major", versioning.APIMajorVersion)
			w.Header().Set("X-RailPush-API-Version", resolved.ID)
			w.Header().Set("X-RailPush-API-Version-Source", resolved.Source)
			if strings.TrimSpace(resolved.Requested) != "" {
				w.Header().Set("X-RailPush-API-Requested-Version", resolved.Requested)
			}

			vary := w.Header().Get("Vary")
			if vary == "" {
				w.Header().Set("Vary", versioning.PinHeader)
			} else if !strings.Contains(strings.ToLower(vary), strings.ToLower(versioning.PinHeader)) {
				w.Header().Set("Vary", vary+", "+versioning.PinHeader)
			}

			if resolved.Deprecated {
				w.Header().Set("Deprecation", "true")
				if strings.TrimSpace(resolved.Sunset) != "" {
					w.Header().Set("Sunset", resolved.Sunset)
				}
				if strings.TrimSpace(resolved.DeprecationURL) != "" {
					w.Header().Set("Link", "<"+resolved.DeprecationURL+">; rel=\"deprecation\"")
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
