package registrar

import "time"

type DomainAvailability struct {
	Domain     string `json:"domain"`
	Available  bool   `json:"available"`
	PriceCents int    `json:"price_cents"`
	Currency   string `json:"currency"`
}

type TldPricing struct {
	TLD        string `json:"tld"`
	PriceCents int    `json:"price_cents"`
}

type RegistrationResult struct {
	ProviderDomainID string    `json:"provider_domain_id"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type DnsRecordInput struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority"`
}

type DnsRecordResult struct {
	ProviderRecordID string `json:"provider_record_id"`
}

type Adapter interface {
	CheckAvailability(domain string) (*DomainAvailability, error)
	SearchAvailability(query string, tlds []string) ([]DomainAvailability, error)
	Register(domain string, years int) (*RegistrationResult, error)
	Renew(providerDomainID string, years int) (*RegistrationResult, error)
	CreateDnsRecord(domain string, rec DnsRecordInput) (*DnsRecordResult, error)
	UpdateDnsRecord(domain string, providerRecordID string, rec DnsRecordInput) error
	DeleteDnsRecord(domain string, providerRecordID string) error
	GetTldPricing() ([]TldPricing, error)
}
