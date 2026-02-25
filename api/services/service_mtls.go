package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const ServiceMTLSConfigEnvKey = "RAILPUSH_MTLS_CONFIG"

type ServiceMTLSConfig struct {
	Enabled         *bool     `json:"enabled,omitempty"`
	Mode            string    `json:"mode,omitempty"`
	AllowedServices []string  `json:"allowed_services"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

func NormalizeServiceMTLSMode(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "", "strict":
		return "strict"
	case "permissive", "allow", "monitor":
		return "permissive"
	default:
		return ""
	}
}

func normalizeServiceMTLSAllowedServices(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, item := range raw {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
		if len(out) >= 200 {
			break
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func NormalizeServiceMTLSConfig(cfg *ServiceMTLSConfig, now time.Time) (*ServiceMTLSConfig, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	normalized := &ServiceMTLSConfig{AllowedServices: []string{}}
	if cfg == nil {
		enabled := false
		normalized.Enabled = &enabled
		normalized.Mode = "strict"
		return normalized, nil
	}

	enabled := false
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	normalized.Enabled = &enabled

	mode := NormalizeServiceMTLSMode(cfg.Mode)
	if mode == "" {
		return nil, fmt.Errorf("mode must be strict or permissive")
	}
	normalized.Mode = mode

	normalized.AllowedServices = normalizeServiceMTLSAllowedServices(cfg.AllowedServices)

	if cfg.UpdatedAt != nil {
		t := cfg.UpdatedAt.UTC()
		normalized.UpdatedAt = &t
	}

	return normalized, nil
}

func EncodeServiceMTLSConfig(cfg *ServiceMTLSConfig, now time.Time) (string, *ServiceMTLSConfig, error) {
	normalized, err := NormalizeServiceMTLSConfig(cfg, now)
	if err != nil {
		return "", nil, err
	}
	b, err := json.Marshal(normalized)
	if err != nil {
		return "", nil, err
	}
	return string(b), normalized, nil
}

func ParseServiceMTLSConfig(raw string, now time.Time) (*ServiceMTLSConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var cfg ServiceMTLSConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("invalid mtls config")
	}
	return NormalizeServiceMTLSConfig(&cfg, now)
}

func IsStrictServiceMTLS(cfg *ServiceMTLSConfig) bool {
	if cfg == nil {
		return false
	}
	enabled := false
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	if !enabled {
		return false
	}
	return NormalizeServiceMTLSMode(cfg.Mode) == "strict"
}
