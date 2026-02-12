package registrar

import "fmt"

type NamecomAdapter struct {
	APIUser  string
	APIToken string
}

func NewNamecomAdapter(apiUser, apiToken string) *NamecomAdapter {
	return &NamecomAdapter{APIUser: apiUser, APIToken: apiToken}
}

func (n *NamecomAdapter) CheckAvailability(domain string) (*DomainAvailability, error) {
	return nil, fmt.Errorf("name.com adapter not configured")
}

func (n *NamecomAdapter) SearchAvailability(query string, tlds []string) ([]DomainAvailability, error) {
	return nil, fmt.Errorf("name.com adapter not configured")
}

func (n *NamecomAdapter) Register(domain string, years int) (*RegistrationResult, error) {
	return nil, fmt.Errorf("name.com adapter not configured")
}

func (n *NamecomAdapter) Renew(providerDomainID string, years int) (*RegistrationResult, error) {
	return nil, fmt.Errorf("name.com adapter not configured")
}

func (n *NamecomAdapter) CreateDnsRecord(domain string, rec DnsRecordInput) (*DnsRecordResult, error) {
	return nil, fmt.Errorf("name.com adapter not configured")
}

func (n *NamecomAdapter) UpdateDnsRecord(domain string, providerRecordID string, rec DnsRecordInput) error {
	return fmt.Errorf("name.com adapter not configured")
}

func (n *NamecomAdapter) DeleteDnsRecord(domain string, providerRecordID string) error {
	return fmt.Errorf("name.com adapter not configured")
}

func (n *NamecomAdapter) GetTldPricing() ([]TldPricing, error) {
	return nil, fmt.Errorf("name.com adapter not configured")
}
