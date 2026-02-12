package models

import (
	"database/sql"
	"time"

	"github.com/lib/pq"
	"github.com/railpush/api/database"
)

type SamlSSOConfig struct {
	WorkspaceID    string    `json:"workspace_id"`
	Enabled        bool      `json:"enabled"`
	EntityID       string    `json:"entity_id"`
	ACSURL         string    `json:"acs_url"`
	MetadataURL    string    `json:"metadata_url"`
	IDPSSOURL      string    `json:"idp_sso_url"`
	IDPCertPEM     string    `json:"idp_cert_pem"`
	AllowedDomains []string  `json:"allowed_domains"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func GetSamlSSOConfig(workspaceID string) (*SamlSSOConfig, error) {
	var c SamlSSOConfig
	err := database.DB.QueryRow(
		`SELECT workspace_id, COALESCE(enabled,false), COALESCE(entity_id,''), COALESCE(acs_url,''), COALESCE(metadata_url,''), COALESCE(idp_sso_url,''), COALESCE(idp_cert_pem,''), COALESCE(allowed_domains, '{}'), created_at, updated_at
		   FROM saml_sso_configs WHERE workspace_id=$1`,
		workspaceID,
	).Scan(
		&c.WorkspaceID, &c.Enabled, &c.EntityID, &c.ACSURL, &c.MetadataURL, &c.IDPSSOURL, &c.IDPCertPEM, pq.Array(&c.AllowedDomains), &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func UpsertSamlSSOConfig(c *SamlSSOConfig) error {
	return database.DB.QueryRow(
		`INSERT INTO saml_sso_configs (workspace_id, enabled, entity_id, acs_url, metadata_url, idp_sso_url, idp_cert_pem, allowed_domains, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW(),NOW())
		 ON CONFLICT (workspace_id)
		 DO UPDATE SET enabled=EXCLUDED.enabled, entity_id=EXCLUDED.entity_id, acs_url=EXCLUDED.acs_url, metadata_url=EXCLUDED.metadata_url, idp_sso_url=EXCLUDED.idp_sso_url, idp_cert_pem=EXCLUDED.idp_cert_pem, allowed_domains=EXCLUDED.allowed_domains, updated_at=NOW()
		 RETURNING created_at, updated_at`,
		c.WorkspaceID, c.Enabled, c.EntityID, c.ACSURL, c.MetadataURL, c.IDPSSOURL, c.IDPCertPEM, pq.Array(c.AllowedDomains),
	).Scan(&c.CreatedAt, &c.UpdatedAt)
}
