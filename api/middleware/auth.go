package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/railpush/api/config"
	"github.com/railpush/api/utils"
)

type contextKey string

const UserIDKey contextKey = "user_id"
const SessionCookieName = "rp_session"

var errMissingCredentials = errors.New("missing credentials")

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
	return ParseUserIDFromToken(cfg, extractTokenFromRequest(r))
}

func AuthMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := AuthenticateRequest(cfg, r)
			if err != nil {
				utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUserID(r *http.Request) string {
	if v, ok := r.Context().Value(UserIDKey).(string); ok {
		return v
	}
	return ""
}
