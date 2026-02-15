package middleware

import (
	"net/http"
	"testing"
)

func TestClientIPString_UsesRemoteAddrHost(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	reloadTrustedProxyCIDRsFromEnv()
	r := &http.Request{RemoteAddr: "203.0.113.10:54321", Header: http.Header{}}
	if got := clientIPString(r); got != "203.0.113.10" {
		t.Fatalf("expected remote host ip, got %q", got)
	}
}

func TestClientIPString_TrustedProxyUsesForwardedHeaders(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	reloadTrustedProxyCIDRsFromEnv()
	r := &http.Request{RemoteAddr: "10.1.2.3:12345", Header: http.Header{}}
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 10.1.2.3")
	if got := clientIPString(r); got != "198.51.100.7" {
		t.Fatalf("expected first forwarded ip, got %q", got)
	}
}

func TestClientIPString_TrustedProxyPrefersXRealIP(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	reloadTrustedProxyCIDRsFromEnv()
	r := &http.Request{RemoteAddr: "127.0.0.1:1111", Header: http.Header{}}
	r.Header.Set("X-Forwarded-For", "198.51.100.7")
	r.Header.Set("X-Real-IP", "192.0.2.9")
	if got := clientIPString(r); got != "192.0.2.9" {
		t.Fatalf("expected X-Real-IP, got %q", got)
	}
}

func TestClientIPString_TrustedProxyCIDRsAllowsPublicProxies(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "203.0.113.0/24")
	reloadTrustedProxyCIDRsFromEnv()
	r := &http.Request{RemoteAddr: "203.0.113.55:1111", Header: http.Header{}}
	r.Header.Set("CF-Connecting-IP", "198.51.100.9")
	if got := clientIPString(r); got != "198.51.100.9" {
		t.Fatalf("expected CF-Connecting-IP when peer is in TRUSTED_PROXY_CIDRS, got %q", got)
	}
}
