package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type RegisteredDomain struct {
	ID               string     `json:"id"`
	UserID           string     `json:"user_id"`
	WorkspaceID      string     `json:"workspace_id"`
	DomainName       string     `json:"domain_name"`
	TLD              string     `json:"tld"`
	Provider         string     `json:"provider"`
	ProviderDomainID string     `json:"provider_domain_id"`
	Status           string     `json:"status"`
	ExpiresAt        *time.Time `json:"expires_at"`
	AutoRenew        bool       `json:"auto_renew"`
	WhoisPrivacy     bool       `json:"whois_privacy"`
	Locked           bool       `json:"locked"`
	CostCents        int        `json:"cost_cents"`
	SellCents        int        `json:"sell_cents"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func CreateRegisteredDomain(d *RegisteredDomain) error {
	return database.DB.QueryRow(
		`INSERT INTO registered_domains (user_id, workspace_id, domain_name, tld, provider, provider_domain_id, status, expires_at, auto_renew, whois_privacy, locked, cost_cents, sell_cents)
		 VALUES ($1, NULLIF($2,'')::UUID, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 RETURNING id, status, created_at, updated_at`,
		d.UserID, d.WorkspaceID, d.DomainName, d.TLD, d.Provider, d.ProviderDomainID,
		d.Status, d.ExpiresAt, d.AutoRenew, d.WhoisPrivacy, d.Locked, d.CostCents, d.SellCents,
	).Scan(&d.ID, &d.Status, &d.CreatedAt, &d.UpdatedAt)
}

func GetRegisteredDomain(id string) (*RegisteredDomain, error) {
	d := &RegisteredDomain{}
	var expiresAt sql.NullTime
	err := database.DB.QueryRow(
		`SELECT id, user_id, COALESCE(workspace_id::text,''), domain_name, tld,
		        COALESCE(provider,'mock'), COALESCE(provider_domain_id,''), COALESCE(status,'pending'),
		        expires_at, auto_renew, whois_privacy, locked,
		        COALESCE(cost_cents,0), COALESCE(sell_cents,0), created_at, updated_at
		 FROM registered_domains WHERE id=$1`, id,
	).Scan(&d.ID, &d.UserID, &d.WorkspaceID, &d.DomainName, &d.TLD,
		&d.Provider, &d.ProviderDomainID, &d.Status,
		&expiresAt, &d.AutoRenew, &d.WhoisPrivacy, &d.Locked,
		&d.CostCents, &d.SellCents, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if expiresAt.Valid {
		d.ExpiresAt = &expiresAt.Time
	}
	return d, err
}

func GetRegisteredDomainByName(name string) (*RegisteredDomain, error) {
	d := &RegisteredDomain{}
	var expiresAt sql.NullTime
	err := database.DB.QueryRow(
		`SELECT id, user_id, COALESCE(workspace_id::text,''), domain_name, tld,
		        COALESCE(provider,'mock'), COALESCE(provider_domain_id,''), COALESCE(status,'pending'),
		        expires_at, auto_renew, whois_privacy, locked,
		        COALESCE(cost_cents,0), COALESCE(sell_cents,0), created_at, updated_at
		 FROM registered_domains WHERE domain_name=$1`, name,
	).Scan(&d.ID, &d.UserID, &d.WorkspaceID, &d.DomainName, &d.TLD,
		&d.Provider, &d.ProviderDomainID, &d.Status,
		&expiresAt, &d.AutoRenew, &d.WhoisPrivacy, &d.Locked,
		&d.CostCents, &d.SellCents, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if expiresAt.Valid {
		d.ExpiresAt = &expiresAt.Time
	}
	return d, err
}

func ListRegisteredDomainsByUser(userID string) ([]RegisteredDomain, error) {
	rows, err := database.DB.Query(
		`SELECT id, user_id, COALESCE(workspace_id::text,''), domain_name, tld,
		        COALESCE(provider,'mock'), COALESCE(provider_domain_id,''), COALESCE(status,'pending'),
		        expires_at, auto_renew, whois_privacy, locked,
		        COALESCE(cost_cents,0), COALESCE(sell_cents,0), created_at, updated_at
		 FROM registered_domains WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var domains []RegisteredDomain
	for rows.Next() {
		var d RegisteredDomain
		var expiresAt sql.NullTime
		if err := rows.Scan(&d.ID, &d.UserID, &d.WorkspaceID, &d.DomainName, &d.TLD,
			&d.Provider, &d.ProviderDomainID, &d.Status,
			&expiresAt, &d.AutoRenew, &d.WhoisPrivacy, &d.Locked,
			&d.CostCents, &d.SellCents, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			d.ExpiresAt = &expiresAt.Time
		}
		domains = append(domains, d)
	}
	return domains, nil
}

func UpdateRegisteredDomain(d *RegisteredDomain) error {
	_, err := database.DB.Exec(
		`UPDATE registered_domains SET auto_renew=$1, whois_privacy=$2, locked=$3, updated_at=NOW() WHERE id=$4`,
		d.AutoRenew, d.WhoisPrivacy, d.Locked, d.ID)
	return err
}

func UpdateRegisteredDomainStatus(id, status string) error {
	_, err := database.DB.Exec(
		`UPDATE registered_domains SET status=$1, updated_at=NOW() WHERE id=$2`, status, id)
	return err
}

func UpdateRegisteredDomainExpiry(id string, expiresAt time.Time) error {
	_, err := database.DB.Exec(
		`UPDATE registered_domains SET expires_at=$1, updated_at=NOW() WHERE id=$2`, expiresAt, id)
	return err
}

func DeleteRegisteredDomain(id string) error {
	_, err := database.DB.Exec(`DELETE FROM registered_domains WHERE id=$1`, id)
	return err
}

func CreateDomainTransaction(domainID, userID, txType string, amountCents int, status string) error {
	_, err := database.DB.Exec(
		`INSERT INTO domain_transactions (domain_id, user_id, type, amount_cents, status) VALUES ($1,$2,$3,$4,$5)`,
		domainID, userID, txType, amountCents, status)
	return err
}
