package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
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
	count       int
	windowStart time.Time
	lastSeen    time.Time
}

type RateLimitInfo struct {
	Limit      int           `json:"limit"`
	Remaining  int           `json:"remaining"`
	ResetAt    time.Time     `json:"reset_at"`
	Window     time.Duration `json:"window"`
	RetryAfter int           `json:"retry_after,omitempty"`
}

type rateLimitInfoContextKey struct{}

func GetRateLimitInfo(r *http.Request) (RateLimitInfo, bool) {
	if r == nil {
		return RateLimitInfo{}, false
	}
	v := r.Context().Value(rateLimitInfoContextKey{})
	if v == nil {
		return RateLimitInfo{}, false
	}
	info, ok := v.(RateLimitInfo)
	return info, ok
}

var generalLimiter = &rateLimiter{
	visitors: make(map[string]*visitor),
	rate:     100,
	window:   time.Minute,
}

var authLimiter = &rateLimiter{
	visitors: make(map[string]*visitor),
	rate:     20,
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

// ClientIPString returns the normalized client IP, honoring trusted proxy
// headers in the same way as the rate limiter.
func ClientIPString(r *http.Request) string {
	return clientIPString(r)
}

func init() {
	reloadTrustedProxyCIDRsFromEnv()
	go generalLimiter.cleanup()
	go authLimiter.cleanup()
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

func (rl *rateLimiter) consume(ip string) (bool, RateLimitInfo) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now().UTC()
	v, exists := rl.visitors[ip]
	if !exists || now.Sub(v.windowStart) >= rl.window {
		v = &visitor{count: 0, windowStart: now, lastSeen: now}
		rl.visitors[ip] = v
	}
	v.count++
	v.lastSeen = now
	remaining := rl.rate - v.count
	if remaining < 0 {
		remaining = 0
	}
	resetAt := v.windowStart.Add(rl.window)
	info := RateLimitInfo{
		Limit:     rl.rate,
		Remaining: remaining,
		ResetAt:   resetAt,
		Window:    rl.window,
	}
	allowed := v.count <= rl.rate
	if !allowed {
		retry := int(time.Until(resetAt).Seconds())
		if retry <= 0 {
			retry = 1
		}
		info.RetryAfter = retry
	}
	return allowed, info
}

func writeRateLimitHeaders(w http.ResponseWriter, info RateLimitInfo) {
	if w == nil {
		return
	}
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(info.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(info.Remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(info.ResetAt.Unix(), 10))
	if info.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(info.RetryAfter))
	} else {
		w.Header().Del("Retry-After")
	}
}

func isPublicAuthEndpoint(path string) bool {
	switch path {
	case "/api/v1/auth/register",
		"/api/v1/auth/login",
		"/api/v1/auth/verify",
		"/api/v1/auth/verify/resend",
		"/api/v1/auth/github",
		"/api/v1/auth/github/callback":
		return true
	default:
		return false
	}
}

func limiterForPath(path string) *rateLimiter {
	if isPublicAuthEndpoint(path) {
		return authLimiter
	}
	return generalLimiter
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
			allowed, info := limiterForPath(r.URL.Path).consume(ip)
			writeRateLimitHeaders(w, info)
			r = r.WithContext(context.WithValue(r.Context(), rateLimitInfoContextKey{}, info))
			if !allowed {
				utils.RespondError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
