package services

import "strings"

var allowedRedisMaxmemoryPolicies = map[string]struct{}{
	"noeviction":      {},
	"allkeys-lru":     {},
	"allkeys-lfu":     {},
	"volatile-lru":    {},
	"volatile-lfu":    {},
	"allkeys-random":  {},
	"volatile-random": {},
	"volatile-ttl":    {},
}

// NormalizeRedisMaxmemoryPolicy returns a safe Redis maxmemory-policy value and whether the input was valid.
//
// Empty values are treated as the default policy and are considered valid.
func NormalizeRedisMaxmemoryPolicy(raw string) (policy string, ok bool) {
	p := strings.ToLower(strings.TrimSpace(raw))
	if p == "" {
		return "allkeys-lru", true
	}
	if _, ok := allowedRedisMaxmemoryPolicies[p]; ok {
		return p, true
	}
	return "allkeys-lru", false
}

