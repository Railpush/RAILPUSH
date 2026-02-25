package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/railpush/api/models"
)

const (
	ServiceAccessControlConfigEnvKey    = "RAILPUSH_ACCESS_CONTROL_CONFIG"
	ServiceAccessControlAllowlistEnvKey = "RAILPUSH_IP_ALLOWLIST"
	ServiceAccessControlDenylistEnvKey  = "RAILPUSH_IP_BLOCKLIST"
)

type ServiceAccessControlRule struct {
	Name        string     `json:"name,omitempty"`
	CIDR        string     `json:"cidr"`
	Description string     `json:"description,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type ServiceAccessControlDenyResponse struct {
	Status int    `json:"status,omitempty"`
	Body   string `json:"body,omitempty"`
}

type ServiceIPAllowlistAccessControl struct {
	Enabled      *bool                             `json:"enabled,omitempty"`
	Mode         string                            `json:"mode,omitempty"`
	Rules        []ServiceAccessControlRule        `json:"rules"`
	DenyResponse *ServiceAccessControlDenyResponse `json:"deny_response,omitempty"`
}

type ServiceAccessControlConfig struct {
	IPAllowlist ServiceIPAllowlistAccessControl `json:"ip_allowlist"`
	UpdatedAt   *time.Time                      `json:"updated_at,omitempty"`
}

func NormalizeServiceAccessControlMode(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "", "allowlist", "whitelist", "allow":
		return "allowlist"
	case "blocklist", "denylist", "blacklist", "deny":
		return "blocklist"
	default:
		return ""
	}
}

func normalizeServiceAccessControlRuleName(raw, fallback string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		v = strings.TrimSpace(fallback)
	}
	if len(v) > 80 {
		v = strings.TrimSpace(v[:80])
	}
	return v
}

func normalizeServiceAccessControlRuleDescription(raw string) string {
	v := strings.TrimSpace(raw)
	if len(v) > 280 {
		v = strings.TrimSpace(v[:280])
	}
	return v
}

func normalizeServiceAccessControlDenyResponse(raw *ServiceAccessControlDenyResponse) (*ServiceAccessControlDenyResponse, error) {
	if raw == nil {
		return nil, nil
	}
	status := raw.Status
	if status == 0 {
		status = 403
	}
	if status < 400 || status > 599 {
		return nil, fmt.Errorf("deny_response.status must be between 400 and 599")
	}
	body := strings.TrimSpace(raw.Body)
	if len(body) > 2000 {
		body = strings.TrimSpace(body[:2000])
	}
	if body == "" && status == 403 {
		return nil, nil
	}
	return &ServiceAccessControlDenyResponse{Status: status, Body: body}, nil
}

func NormalizeServiceAccessControlConfig(cfg *ServiceAccessControlConfig, now time.Time) (*ServiceAccessControlConfig, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	normalized := &ServiceAccessControlConfig{
		IPAllowlist: ServiceIPAllowlistAccessControl{
			Rules: []ServiceAccessControlRule{},
		},
	}
	if cfg == nil {
		enabled := false
		normalized.IPAllowlist.Enabled = &enabled
		normalized.IPAllowlist.Mode = "allowlist"
		return normalized, nil
	}

	enabled := true
	if cfg.IPAllowlist.Enabled != nil {
		enabled = *cfg.IPAllowlist.Enabled
	}
	normalized.IPAllowlist.Enabled = &enabled

	mode := NormalizeServiceAccessControlMode(cfg.IPAllowlist.Mode)
	if mode == "" {
		return nil, fmt.Errorf("ip_allowlist.mode must be allowlist or blocklist")
	}
	normalized.IPAllowlist.Mode = mode

	if len(cfg.IPAllowlist.Rules) > 200 {
		return nil, fmt.Errorf("ip_allowlist.rules exceeds maximum of 200")
	}

	seenCIDRs := map[string]struct{}{}
	for _, rule := range cfg.IPAllowlist.Rules {
		rawCIDR := strings.TrimSpace(rule.CIDR)
		if rawCIDR == "" {
			continue
		}

		cidrs, err := models.NormalizeAndValidateCIDRAllowlist([]string{rawCIDR})
		if err != nil || len(cidrs) == 0 {
			return nil, fmt.Errorf("invalid cidr %q", rawCIDR)
		}
		cidr := cidrs[0]
		if _, ok := seenCIDRs[cidr]; ok {
			continue
		}
		seenCIDRs[cidr] = struct{}{}

		var expiresAt *time.Time
		if rule.ExpiresAt != nil {
			t := rule.ExpiresAt.UTC()
			expiresAt = &t
		}

		normalized.IPAllowlist.Rules = append(normalized.IPAllowlist.Rules, ServiceAccessControlRule{
			Name:        normalizeServiceAccessControlRuleName(rule.Name, cidr),
			CIDR:        cidr,
			Description: normalizeServiceAccessControlRuleDescription(rule.Description),
			ExpiresAt:   expiresAt,
		})
	}

	denyResponse, err := normalizeServiceAccessControlDenyResponse(cfg.IPAllowlist.DenyResponse)
	if err != nil {
		return nil, err
	}
	normalized.IPAllowlist.DenyResponse = denyResponse

	if cfg.UpdatedAt != nil {
		t := cfg.UpdatedAt.UTC()
		normalized.UpdatedAt = &t
	}

	return normalized, nil
}

func ParseServiceAccessControlConfig(raw string, now time.Time) (*ServiceAccessControlConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var cfg ServiceAccessControlConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("invalid access control config")
	}
	return NormalizeServiceAccessControlConfig(&cfg, now)
}

func EncodeServiceAccessControlConfig(cfg *ServiceAccessControlConfig, now time.Time) (string, *ServiceAccessControlConfig, error) {
	normalized, err := NormalizeServiceAccessControlConfig(cfg, now)
	if err != nil {
		return "", nil, err
	}
	b, err := json.Marshal(normalized)
	if err != nil {
		return "", nil, err
	}
	return string(b), normalized, nil
}

func ActiveCIDRsFromServiceAccessControl(cfg *ServiceAccessControlConfig, now time.Time) ([]string, []string) {
	if cfg == nil {
		return []string{}, []string{}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	enabled := true
	if cfg.IPAllowlist.Enabled != nil {
		enabled = *cfg.IPAllowlist.Enabled
	}
	if !enabled {
		return []string{}, []string{}
	}

	mode := NormalizeServiceAccessControlMode(cfg.IPAllowlist.Mode)
	if mode == "" {
		mode = "allowlist"
	}

	rawCIDRs := make([]string, 0, len(cfg.IPAllowlist.Rules))
	for _, rule := range cfg.IPAllowlist.Rules {
		cidr := strings.TrimSpace(rule.CIDR)
		if cidr == "" {
			continue
		}
		if rule.ExpiresAt != nil && rule.ExpiresAt.UTC().Before(now) {
			continue
		}
		rawCIDRs = append(rawCIDRs, cidr)
	}
	normalizedCIDRs, err := models.NormalizeAndValidateCIDRAllowlist(rawCIDRs)
	if err != nil || len(normalizedCIDRs) == 0 {
		return []string{}, []string{}
	}

	if mode == "blocklist" {
		return []string{}, normalizedCIDRs
	}
	return normalizedCIDRs, []string{}
}

func serviceAccessControlCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	norm, err := models.NormalizeAndValidateCIDRAllowlist(parts)
	if err != nil || len(norm) == 0 {
		return []string{}
	}
	return norm
}

func ResolveServiceAccessControlRangesFromEnv(env map[string]string, now time.Time) ([]string, []string) {
	if len(env) == 0 {
		return []string{}, []string{}
	}

	if raw := strings.TrimSpace(env[ServiceAccessControlConfigEnvKey]); raw != "" {
		cfg, err := ParseServiceAccessControlConfig(raw, now)
		if err == nil {
			allow, deny := ActiveCIDRsFromServiceAccessControl(cfg, now)
			if len(allow) > 0 || len(deny) > 0 {
				return allow, deny
			}
		}
	}

	allowKeys := []string{"INGRESS_WHITELIST_SOURCE_RANGE", ServiceAccessControlAllowlistEnvKey, "ALLOWED_IP_CIDRS"}
	denyKeys := []string{"INGRESS_DENYLIST_SOURCE_RANGE", ServiceAccessControlDenylistEnvKey, "RAILPUSH_IP_DENYLIST", "INGRESS_BLACKLIST_SOURCE_RANGE"}

	allow := []string{}
	for _, key := range allowKeys {
		if cidrs := serviceAccessControlCSV(env[key]); len(cidrs) > 0 {
			allow = cidrs
			break
		}
	}

	deny := []string{}
	for _, key := range denyKeys {
		if cidrs := serviceAccessControlCSV(env[key]); len(cidrs) > 0 {
			deny = cidrs
			break
		}
	}

	return allow, deny
}
