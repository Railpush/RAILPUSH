package models

import (
	"time"

	"github.com/railpush/api/database"
)

type EnvVar struct {
	ID             string    `json:"id"`
	OwnerType      string    `json:"owner_type"`
	OwnerID        string    `json:"owner_id"`
	Key            string    `json:"key"`
	EncryptedValue string    `json:"-"`
	Value          string    `json:"value,omitempty"`
	IsSecret       bool      `json:"is_secret"`
	CreatedAt      time.Time `json:"created_at"`
}

func ListEnvVars(ownerType, ownerID string) ([]EnvVar, error) {
	rows, err := database.DB.Query("SELECT id, owner_type, owner_id, key, encrypted_value, is_secret, created_at FROM env_vars WHERE owner_type=$1 AND owner_id=$2 ORDER BY key", ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var vars []EnvVar
	for rows.Next() {
		var v EnvVar
		if err := rows.Scan(&v.ID, &v.OwnerType, &v.OwnerID, &v.Key, &v.EncryptedValue, &v.IsSecret, &v.CreatedAt); err != nil {
			return nil, err
		}
		vars = append(vars, v)
	}
	return vars, nil
}

func BulkUpsertEnvVars(ownerType, ownerID string, vars []EnvVar) error {
	tx, err := database.DB.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM env_vars WHERE owner_type=$1 AND owner_id=$2", ownerType, ownerID); err != nil {
		tx.Rollback()
		return err
	}
	for _, v := range vars {
		if _, err := tx.Exec("INSERT INTO env_vars (owner_type, owner_id, key, encrypted_value, is_secret) VALUES ($1,$2,$3,$4,$5)",
			ownerType, ownerID, v.Key, v.EncryptedValue, v.IsSecret); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
