package middleware

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
)

func signedToken(t *testing.T, secret, subject string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   subject,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	})
	tok, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tok
}

func TestAuthenticateRequestFromCookie(t *testing.T) {
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-secret"}}
	tok := signedToken(t, cfg.JWT.Secret, "user-123")

	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: tok})

	userID, err := AuthenticateRequest(cfg, req)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if userID != "user-123" {
		t.Fatalf("expected user-123, got %q", userID)
	}
}

func TestAuthenticateRequestPrefersAuthorizationHeader(t *testing.T) {
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-secret"}}
	tok := signedToken(t, cfg.JWT.Secret, "user-abc")

	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	userID, err := AuthenticateRequest(cfg, req)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if userID != "user-abc" {
		t.Fatalf("expected user-abc, got %q", userID)
	}
}

func TestAuthenticateRequestMissingToken(t *testing.T) {
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-secret"}}
	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	if _, err := AuthenticateRequest(cfg, req); err == nil {
		t.Fatalf("expected error for missing credentials")
	}
}

func TestAuthenticateRequestRejectsQueryToken(t *testing.T) {
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-secret"}}
	tok := signedToken(t, cfg.JWT.Secret, "user-query")

	req := httptest.NewRequest(http.MethodGet, "http://example.com?token="+tok, nil)
	if _, err := AuthenticateRequest(cfg, req); err == nil {
		t.Fatalf("expected query token to be rejected")
	}
}

func TestRequiredAPIKeyScopesForRequest(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   []string
	}{
		{method: http.MethodGet, path: "/api/v1/services", want: []string{models.APIKeyScopeRead}},
		{method: http.MethodPost, path: "/api/v1/services", want: []string{models.APIKeyScopeWrite}},
		{method: http.MethodPost, path: "/api/v1/services/abc/deploys", want: []string{models.APIKeyScopeDeploy}},
		{method: http.MethodPost, path: "/api/v1/services/bulk-deploy", want: []string{models.APIKeyScopeDeploy}},
		{method: http.MethodPost, path: "/api/v1/services/bulk-restart", want: []string{models.APIKeyScopeDeploy}},
		{method: http.MethodPost, path: "/api/v1/services/bulk-update", want: []string{models.APIKeyScopeWrite}},
		{method: http.MethodPost, path: "/api/v1/services/bulk-set-env", want: []string{models.APIKeyScopeWrite}},
		{method: http.MethodPost, path: "/api/v1/databases/bulk-update", want: []string{models.APIKeyScopeWrite}},
		{method: http.MethodGet, path: "/api/v1/ops/tickets", want: []string{models.APIKeyScopeOps}},
		{method: http.MethodPost, path: "/api/v1/auth/api-keys", want: []string{models.APIKeyScopeAdmin}},
		{method: http.MethodGet, path: "/api/v1/billing", want: []string{models.APIKeyScopeBilling, models.APIKeyScopeRead}},
	}

	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, "http://example.com"+tc.path, nil)
		got := requiredAPIKeyScopesForRequest(req)
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("requiredAPIKeyScopesForRequest(%s %s): got %v want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestIsClientIPAllowedByCIDRs(t *testing.T) {
	tests := []struct {
		name    string
		client  string
		allowed []string
		want    bool
	}{
		{name: "single ipv4 cidr", client: "203.0.113.10", allowed: []string{"203.0.113.10/32"}, want: true},
		{name: "in subnet", client: "10.0.1.23", allowed: []string{"10.0.0.0/8"}, want: true},
		{name: "not in subnet", client: "192.168.1.10", allowed: []string{"10.0.0.0/8"}, want: false},
		{name: "single ipv6", client: "2001:db8::1", allowed: []string{"2001:db8::/64"}, want: true},
		{name: "ignores invalid entry", client: "10.0.0.1", allowed: []string{"not-a-cidr"}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isClientIPAllowedByCIDRs(tc.client, tc.allowed); got != tc.want {
				t.Fatalf("isClientIPAllowedByCIDRs(%q, %v)=%v want %v", tc.client, tc.allowed, got, tc.want)
			}
		})
	}
}
