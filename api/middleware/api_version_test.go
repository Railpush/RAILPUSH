package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/railpush/api/versioning"
)

func TestAPIVersionMiddlewareDefaultVersion(t *testing.T) {
	called := false
	h := APIVersionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/services", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !called {
		t.Fatalf("expected next handler to be called")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if got := rr.Header().Get("X-RailPush-API-Version"); got != versioning.CurrentAPIVersion {
		t.Fatalf("expected current API version %q, got %q", versioning.CurrentAPIVersion, got)
	}
	if got := rr.Header().Get("X-RailPush-API-Version-Source"); got != "default" {
		t.Fatalf("expected source default, got %q", got)
	}
}

func TestAPIVersionMiddlewareHeaderPin(t *testing.T) {
	called := false
	h := APIVersionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/services", nil)
	req.Header.Set(versioning.PinHeader, "v1")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !called {
		t.Fatalf("expected next handler to be called")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if got := rr.Header().Get("X-RailPush-API-Requested-Version"); got != "v1" {
		t.Fatalf("expected requested pin v1, got %q", got)
	}
	if got := rr.Header().Get("X-RailPush-API-Version-Source"); got != "header" {
		t.Fatalf("expected source header, got %q", got)
	}
}

func TestAPIVersionMiddlewareRejectsUnknownPin(t *testing.T) {
	called := false
	h := APIVersionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/services?api_version=does-not-exist", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if called {
		t.Fatalf("did not expect next handler to be called")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAPIVersionMiddlewareDeprecationHeaders(t *testing.T) {
	called := false
	h := APIVersionMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/services", nil)
	req.Header.Set(versioning.PinHeader, "2025-12-01")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !called {
		t.Fatalf("expected next handler to be called")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if got := rr.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("expected deprecation header, got %q", got)
	}
	if rr.Header().Get("Sunset") == "" {
		t.Fatalf("expected sunset header to be set")
	}
}
