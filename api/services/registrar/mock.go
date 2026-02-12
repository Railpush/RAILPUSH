package registrar

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type MockAdapter struct {
	mu      sync.RWMutex
	domains map[string]time.Time
	records map[string][]mockDnsRecord
	nextID  int
}

type mockDnsRecord struct {
	ID       string
	Type     string
	Name     string
	Value    string
	TTL      int
	Priority int
}

var tldPrices = map[string]int{
	"com": 1199,
	"net": 1399,
	"org": 1299,
	"io":  3999,
	"dev": 1499,
	"app": 1499,
	"xyz": 199,
	"co":  2999,
}

var takenDomains = map[string]bool{
	"taken.com":  true,
	"google.com": true,
}

func NewMockAdapter() *MockAdapter {
	return &MockAdapter{
		domains: make(map[string]time.Time),
		records: make(map[string][]mockDnsRecord),
	}
}

func (m *MockAdapter) CheckAvailability(domain string) (*DomainAvailability, error) {
	domain = strings.ToLower(domain)
	parts := strings.SplitN(domain, ".", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid domain: %s", domain)
	}
	tld := parts[1]
	price, ok := tldPrices[tld]
	if !ok {
		return &DomainAvailability{Domain: domain, Available: false, PriceCents: 0, Currency: "USD"}, nil
	}

	m.mu.RLock()
	_, registered := m.domains[domain]
	m.mu.RUnlock()

	available := !registered && !takenDomains[domain]
	return &DomainAvailability{Domain: domain, Available: available, PriceCents: price, Currency: "USD"}, nil
}

func (m *MockAdapter) SearchAvailability(query string, tlds []string) ([]DomainAvailability, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	// Strip any TLD from the query itself
	for tld := range tldPrices {
		query = strings.TrimSuffix(query, "."+tld)
	}

	if len(tlds) == 0 {
		for tld := range tldPrices {
			tlds = append(tlds, tld)
		}
	}

	var results []DomainAvailability
	for _, tld := range tlds {
		domain := query + "." + tld
		avail, err := m.CheckAvailability(domain)
		if err != nil {
			continue
		}
		results = append(results, *avail)
	}
	return results, nil
}

func (m *MockAdapter) Register(domain string, years int) (*RegistrationResult, error) {
	domain = strings.ToLower(domain)
	avail, err := m.CheckAvailability(domain)
	if err != nil {
		return nil, err
	}
	if !avail.Available {
		return nil, fmt.Errorf("domain %s is not available", domain)
	}

	expires := time.Now().AddDate(years, 0, 0)

	m.mu.Lock()
	m.domains[domain] = expires
	m.nextID++
	providerID := fmt.Sprintf("mock-%d", m.nextID)
	m.mu.Unlock()

	return &RegistrationResult{
		ProviderDomainID: providerID,
		ExpiresAt:        expires,
	}, nil
}

func (m *MockAdapter) Renew(providerDomainID string, years int) (*RegistrationResult, error) {
	newExpiry := time.Now().AddDate(years, 0, 0)
	return &RegistrationResult{
		ProviderDomainID: providerDomainID,
		ExpiresAt:        newExpiry,
	}, nil
}

func (m *MockAdapter) CreateDnsRecord(domain string, rec DnsRecordInput) (*DnsRecordResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := fmt.Sprintf("mock-dns-%d", m.nextID)
	m.records[domain] = append(m.records[domain], mockDnsRecord{
		ID: id, Type: rec.Type, Name: rec.Name, Value: rec.Value, TTL: rec.TTL, Priority: rec.Priority,
	})
	return &DnsRecordResult{ProviderRecordID: id}, nil
}

func (m *MockAdapter) UpdateDnsRecord(domain string, providerRecordID string, rec DnsRecordInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	recs := m.records[domain]
	for i, r := range recs {
		if r.ID == providerRecordID {
			recs[i] = mockDnsRecord{ID: r.ID, Type: rec.Type, Name: rec.Name, Value: rec.Value, TTL: rec.TTL, Priority: rec.Priority}
			m.records[domain] = recs
			return nil
		}
	}
	return fmt.Errorf("record not found")
}

func (m *MockAdapter) DeleteDnsRecord(domain string, providerRecordID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	recs := m.records[domain]
	for i, r := range recs {
		if r.ID == providerRecordID {
			m.records[domain] = append(recs[:i], recs[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("record not found")
}

func (m *MockAdapter) GetTldPricing() ([]TldPricing, error) {
	var pricing []TldPricing
	for tld, price := range tldPrices {
		pricing = append(pricing, TldPricing{TLD: tld, PriceCents: price})
	}
	return pricing, nil
}
