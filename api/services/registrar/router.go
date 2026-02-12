package registrar

import "github.com/railpush/api/config"

type ProviderRouter struct {
	mock    *MockAdapter
	namecom *NamecomAdapter
	cfg     config.RegistrarConfig
}

func NewProviderRouter(cfg config.RegistrarConfig) *ProviderRouter {
	pr := &ProviderRouter{
		mock: NewMockAdapter(),
		cfg:  cfg,
	}
	if cfg.NamecomUser != "" && cfg.NamecomToken != "" {
		pr.namecom = NewNamecomAdapter(cfg.NamecomUser, cfg.NamecomToken)
	}
	return pr
}

func (pr *ProviderRouter) ForDomain(domain string) Adapter {
	if pr.cfg.Provider == "namecom" && pr.namecom != nil {
		return pr.namecom
	}
	return pr.mock
}

func (pr *ProviderRouter) Default() Adapter {
	if pr.cfg.Provider == "namecom" && pr.namecom != nil {
		return pr.namecom
	}
	return pr.mock
}
