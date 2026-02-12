package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type CustomDomain struct {
	ID             string    `json:"id"`
	ServiceID      string    `json:"service_id"`
	Domain         string    `json:"domain"`
	Verified       bool      `json:"verified"`
	TLSProvisioned bool      `json:"tls_provisioned"`
	CreatedAt      time.Time `json:"created_at"`
}

func CreateCustomDomain(d *CustomDomain) error {
	return database.DB.QueryRow("INSERT INTO custom_domains (service_id, domain) VALUES ($1,$2) RETURNING id, created_at",
		d.ServiceID, d.Domain).Scan(&d.ID, &d.CreatedAt)
}

func ListCustomDomains(serviceID string) ([]CustomDomain, error) {
	rows, err := database.DB.Query("SELECT id, service_id, domain, verified, tls_provisioned, created_at FROM custom_domains WHERE service_id=$1", serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var domains []CustomDomain
	for rows.Next() {
		var d CustomDomain
		if err := rows.Scan(&d.ID, &d.ServiceID, &d.Domain, &d.Verified, &d.TLSProvisioned, &d.CreatedAt); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	return domains, nil
}

func DeleteCustomDomain(serviceID, domain string) error {
	_, err := database.DB.Exec("DELETE FROM custom_domains WHERE service_id=$1 AND domain=$2", serviceID, domain)
	return err
}

func SetCustomDomainTLSProvisioned(serviceID, domain string, provisioned bool) error {
	_, err := database.DB.Exec(
		"UPDATE custom_domains SET tls_provisioned=$1 WHERE service_id=$2 AND domain=$3",
		provisioned, serviceID, domain,
	)
	return err
}

func GetCustomDomain(serviceID, domain string) (*CustomDomain, error) {
	d := &CustomDomain{}
	err := database.DB.QueryRow("SELECT id, service_id, domain, verified, tls_provisioned, created_at FROM custom_domains WHERE service_id=$1 AND domain=$2", serviceID, domain).Scan(
		&d.ID, &d.ServiceID, &d.Domain, &d.Verified, &d.TLSProvisioned, &d.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}
