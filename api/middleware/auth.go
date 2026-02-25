package middleware

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"
)

type contextKey string

const UserIDKey contextKey = "user_id"
const APIKeyIDKey contextKey = "api_key_id"
const APIKeyScopesKey contextKey = "api_key_scopes"
const SessionCookieName = "rp_session"

var errMissingCredentials = errors.New("missing credentials")

type AuthResult struct {
	UserID      string
	UsedAPIKey  bool
	APIKeyID    string
	APIKeyScope []string
}

func extractTokenFromRequest(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") && strings.TrimSpace(parts[1]) != "" {
			return strings.TrimSpace(parts[1])
		}
	}
	if cookie, err := r.Cookie(SessionCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

func ParseUserIDFromToken(cfg *config.Config, tokenStr string) (string, error) {
	tokenStr = strings.TrimSpace(tokenStr)
	if tokenStr == "" {
		return "", errMissingCredentials
	}
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(cfg.JWT.Secret), nil
	})
	if err != nil || !token.Valid {
		return "", errors.New("invalid token")
	}
	if claims.ExpiresAt != nil && claims.ExpiresAt.Time.Before(time.Now()) {
		return "", errors.New("token expired")
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return "", errors.New("invalid token subject")
	}
	return claims.Subject, nil
}

func AuthenticateRequest(cfg *config.Config, r *http.Request) (string, error) {
	result, err := AuthenticateRequestWithResult(cfg, r)
	if err != nil {
		return "", err
	}
	return result.UserID, nil
}

func AuthenticateRequestWithResult(cfg *config.Config, r *http.Request) (*AuthResult, error) {
	tokenStr := extractTokenFromRequest(r)
	// First try JWT.
	userID, err := ParseUserIDFromToken(cfg, tokenStr)
	if err == nil {
		return &AuthResult{UserID: userID, UsedAPIKey: false}, nil
	}
	// Fall back to API key lookup.
	if tokenStr != "" {
		if keyIdentity, kerr := models.ResolveAPIKey(tokenStr); kerr == nil && keyIdentity != nil && keyIdentity.UserID != "" {
			return &AuthResult{
				UserID:      keyIdentity.UserID,
				UsedAPIKey:  true,
				APIKeyID:    keyIdentity.ID,
				APIKeyScope: append([]string{}, keyIdentity.Scopes...),
			}, nil
		}
	}
	return nil, err
}

func AuthMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authResult, err := AuthenticateRequestWithResult(cfg, r)
			if err != nil {
				utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			userID := authResult.UserID
			if u, err := models.GetUserByID(userID); err != nil {
				utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
				return
			} else if u == nil {
				utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
				return
			} else if u.IsSuspended {
				utils.RespondError(w, http.StatusForbidden, "account suspended")
				return
			}

			if authResult.UsedAPIKey {
				required := requiredAPIKeyScopesForRequest(r)
				if len(required) > 0 && !models.HasAnyAPIKeyScope(authResult.APIKeyScope, required...) {
					utils.RespondError(w, http.StatusForbidden, "api key scope denied")
					return
				}
			}

			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			if authResult.UsedAPIKey {
				ctx = context.WithValue(ctx, APIKeyIDKey, authResult.APIKeyID)
				ctx = context.WithValue(ctx, APIKeyScopesKey, append([]string{}, authResult.APIKeyScope...))
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requiredAPIKeyScopesForRequest(r *http.Request) []string {
	cleanPath := path.Clean("/" + strings.TrimSpace(r.URL.Path))
	method := strings.ToUpper(strings.TrimSpace(r.Method))

	if strings.HasPrefix(cleanPath, "/api/v1/ops/") {
		return []string{models.APIKeyScopeOps}
	}
	if strings.HasPrefix(cleanPath, "/api/v1/billing") {
		if method == http.MethodGet || method == http.MethodHead {
			return []string{models.APIKeyScopeBilling, models.APIKeyScopeRead}
		}
		return []string{models.APIKeyScopeBilling}
	}
	if strings.HasPrefix(cleanPath, "/api/v1/support/tickets") {
		if method == http.MethodGet || method == http.MethodHead {
			return []string{models.APIKeyScopeSupport, models.APIKeyScopeRead}
		}
		return []string{models.APIKeyScopeSupport, models.APIKeyScopeWrite}
	}
	if strings.HasPrefix(cleanPath, "/api/v1/auth/api-keys") {
		if method == http.MethodGet || method == http.MethodHead {
			return []string{models.APIKeyScopeAdmin, models.APIKeyScopeRead}
		}
		return []string{models.APIKeyScopeAdmin}
	}

	if strings.Contains(cleanPath, "/deploys") ||
		strings.HasSuffix(cleanPath, "/restart") ||
		strings.HasSuffix(cleanPath, "/suspend") ||
		strings.HasSuffix(cleanPath, "/resume") ||
		strings.Contains(cleanPath, "/jobs") ||
		strings.HasSuffix(cleanPath, "/ai-fix") ||
		strings.HasSuffix(cleanPath, "/ai-fix/status") ||
		strings.Contains(cleanPath, "/templates/") && strings.HasSuffix(cleanPath, "/deploy") {
		if method == http.MethodGet || method == http.MethodHead {
			return []string{models.APIKeyScopeDeploy, models.APIKeyScopeRead}
		}
		return []string{models.APIKeyScopeDeploy}
	}

	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return []string{models.APIKeyScopeRead}
	}
	return []string{models.APIKeyScopeWrite}
}

func GetUserID(r *http.Request) string {
	if v, ok := r.Context().Value(UserIDKey).(string); ok {
		return v
	}
	return ""
}

func GetAPIKeyID(r *http.Request) string {
	if v, ok := r.Context().Value(APIKeyIDKey).(string); ok {
		return v
	}
	return ""
}

func GetAPIKeyScopes(r *http.Request) []string {
	if v, ok := r.Context().Value(APIKeyScopesKey).([]string); ok {
		return append([]string{}, v...)
	}
	return nil
}
