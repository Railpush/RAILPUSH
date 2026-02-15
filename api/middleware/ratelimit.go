package middleware

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/utils"
)

type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int
	window   time.Duration
}

type visitor struct {
	count    int
	windowStart time.Time
	lastSeen time.Time
}

var limiter = &rateLimiter{
	visitors: make(map[string]*visitor),
	rate:     100,
	window:   time.Minute,
}

var trustedProxyNetsMu sync.RWMutex
var trustedProxyNets []*net.IPNet

func parseIP(raw string) net.IP {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// Strip optional brackets from IPv6 literals.
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	return net.ParseIP(raw)
}

func parseCIDRs(raw string) ([]*net.IPNet, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var nets []*net.IPNet
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "/") {
			_, n, err := net.ParseCIDR(part)
			if err != nil {
				return nil, err
			}
			nets = append(nets, n)
			continue
		}
		ip := parseIP(part)
		if ip == nil {
			return nil, fmt.Errorf("invalid ip: %q", part)
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		_, n, err := net.ParseCIDR(fmt.Sprintf("%s/%d", ip.String(), bits))
		if err != nil {
			return nil, err
		}
		nets = append(nets, n)
	}
	return nets, nil
}

func reloadTrustedProxyCIDRsFromEnv() {
	nets, err := parseCIDRs(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if err != nil {
		// Fail-closed: if config is invalid, do not trust any forwarded headers from public peers.
		nets = nil
	}
	trustedProxyNetsMu.Lock()
	trustedProxyNets = nets
	trustedProxyNetsMu.Unlock()
}

func isTrustedProxy(ip net.IP) bool {
	if ip == nil {
		return false
	}
	trustedProxyNetsMu.RLock()
	nets := trustedProxyNets
	trustedProxyNetsMu.RUnlock()
	if len(nets) > 0 {
		for _, n := range nets {
			if n != nil && n.Contains(ip) {
				return true
			}
		}
		return false
	}
	// We only trust forwarded headers when the direct peer is a loopback/private address,
	// which is the common case when running behind a reverse proxy/ingress.
	//
	// NOTE: If you run behind a public proxy (Cloudflare, external LB), set
	// TRUSTED_PROXY_CIDRS to those proxy CIDRs so forwarded headers are only trusted
	// from known peers.
	return ip.IsLoopback() || ip.IsPrivate()
}

func remoteIP(r *http.Request) net.IP {
	if r == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		// r.RemoteAddr is sometimes already just the IP.
		host = strings.TrimSpace(r.RemoteAddr)
	}
	return parseIP(host)
}

func forwardedClientIP(r *http.Request) net.IP {
	if r == nil {
		return nil
	}
	// Prefer the single-IP header set by our ingress (sanitized).
	//
	// NOTE: CF-Connecting-IP / True-Client-IP can be client-spoofed if a request
	// reaches the origin directly. We keep those as fallbacks for deployments
	// where the edge proxy strips/overwrites headers, but prefer X-Real-IP first.
	for _, hdr := range []string{"X-Real-IP", "CF-Connecting-IP", "True-Client-IP"} {
		if ip := parseIP(r.Header.Get(hdr)); ip != nil {
			return ip
		}
	}

	xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if xff == "" {
		return nil
	}
	// X-Forwarded-For is a comma-separated list where the left-most is the original client.
	parts := strings.Split(xff, ",")
	if len(parts) == 0 {
		return nil
	}
	return parseIP(parts[0])
}

func clientIPString(r *http.Request) string {
	rip := remoteIP(r)
	if isTrustedProxy(rip) {
		if fip := forwardedClientIP(r); fip != nil {
			return fip.String()
		}
	}
	if rip != nil {
		return rip.String()
	}
	// Last resort (should be rare, but avoids empty map keys).
	return strings.TrimSpace(r.RemoteAddr)
}

func init() {
	reloadTrustedProxyCIDRsFromEnv()
	go limiter.cleanup()
}

func (rl *rateLimiter) cleanup() {
	for {
		time.Sleep(time.Minute)
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			if time.Since(v.lastSeen) > rl.window {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	v, exists := rl.visitors[ip]
	if !exists || now.Sub(v.windowStart) > rl.window {
		rl.visitors[ip] = &visitor{count: 1, windowStart: now, lastSeen: now}
		return true
	}
	v.count++
	v.lastSeen = now
	return v.count <= rl.rate
}

func RateLimitMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never rate-limit Kubernetes probes / uptime checks or signed webhooks.
		switch r.URL.Path {
		case "/healthz", "/readyz":
			next.ServeHTTP(w, r)
			return
		case "/api/v1/webhooks/stripe", "/api/v1/webhooks/alertmanager":
			next.ServeHTTP(w, r)
			return
		case "/api/v1/webhooks/github":
			// If signature verification is enabled, don't rate-limit GitHub deliveries.
			if cfg != nil && strings.TrimSpace(cfg.GitHub.WebhookSecret) != "" {
				next.ServeHTTP(w, r)
				return
			}
		}
		ip := clientIPString(r)
		if !limiter.allow(ip) {
			utils.RespondError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
		})
	}
}
