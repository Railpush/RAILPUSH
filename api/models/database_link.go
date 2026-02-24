package models

import (
	"time"

	"github.com/railpush/api/database"
)

type ServiceDatabaseLink struct {
	ID             string    `json:"id"`
	ServiceID      string    `json:"service_id"`
	DatabaseID     string    `json:"database_id"`
	EnvVarName     string    `json:"env_var_name"`
	UseInternalURL bool      `json:"use_internal_url"`
	CreatedAt      time.Time `json:"created_at"`
}

func UpsertServiceDatabaseLink(link *ServiceDatabaseLink) error {
	return database.DB.QueryRow(
		`INSERT INTO service_database_links (service_id, database_id, env_var_name, use_internal_url)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (service_id, database_id, env_var_name)
		 DO UPDATE SET use_internal_url=EXCLUDED.use_internal_url
		 RETURNING id, created_at`,
		link.ServiceID, link.DatabaseID, link.EnvVarName, link.UseInternalURL,
	).Scan(&link.ID, &link.CreatedAt)
}

func ListServiceDatabaseLinks(serviceID string) ([]ServiceDatabaseLink, error) {
	rows, err := database.DB.Query(
		`SELECT id, service_id, database_id, env_var_name, use_internal_url, created_at
		   FROM service_database_links
		  WHERE service_id=$1
		  ORDER BY created_at ASC`,
		serviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServiceDatabaseLink{}
	for rows.Next() {
		var l ServiceDatabaseLink
		if err := rows.Scan(&l.ID, &l.ServiceID, &l.DatabaseID, &l.EnvVarName, &l.UseInternalURL, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func ListServiceDatabaseLinksByDatabase(databaseID string) ([]ServiceDatabaseLink, error) {
	rows, err := database.DB.Query(
		`SELECT id, service_id, database_id, env_var_name, use_internal_url, created_at
		   FROM service_database_links
		  WHERE database_id=$1
		  ORDER BY created_at ASC`,
		databaseID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServiceDatabaseLink{}
	for rows.Next() {
		var l ServiceDatabaseLink
		if err := rows.Scan(&l.ID, &l.ServiceID, &l.DatabaseID, &l.EnvVarName, &l.UseInternalURL, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func DeleteServiceDatabaseLink(serviceID, databaseID, envVarName string) error {
	_, err := database.DB.Exec(
		`DELETE FROM service_database_links WHERE service_id=$1 AND database_id=$2 AND env_var_name=$3`,
		serviceID, databaseID, envVarName,
	)
	return err
}
