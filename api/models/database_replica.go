package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type DatabaseReplica struct {
	ID                string    `json:"id"`
	PrimaryDatabaseID string    `json:"primary_database_id"`
	WorkspaceID       string    `json:"workspace_id"`
	Name              string    `json:"name"`
	Region            string    `json:"region"`
	ContainerID       string    `json:"container_id"`
	Host              string    `json:"host"`
	Port              int       `json:"port"`
	Status            string    `json:"status"`
	ReplicationMode   string    `json:"replication_mode"`
	LagSeconds        int       `json:"lag_seconds"`
	Promoted          bool      `json:"promoted"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func CreateDatabaseReplica(r *DatabaseReplica) error {
	return database.DB.QueryRow(
		`INSERT INTO managed_database_replicas (primary_database_id, workspace_id, name, region, container_id, host, port, status, replication_mode, lag_seconds, promoted, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW(),NOW())
		 RETURNING id, created_at, updated_at`,
		r.PrimaryDatabaseID, r.WorkspaceID, r.Name, r.Region, r.ContainerID, r.Host, r.Port, r.Status, r.ReplicationMode, r.LagSeconds, r.Promoted,
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
}

func UpdateDatabaseReplicaStatus(id, status, containerID, host string, port int) error {
	_, err := database.DB.Exec(
		`UPDATE managed_database_replicas
		    SET status=$1, container_id=$2, host=$3, port=$4, updated_at=NOW()
		  WHERE id=$5`,
		status, containerID, host, port, id,
	)
	return err
}

func ListDatabaseReplicas(primaryDatabaseID string) ([]DatabaseReplica, error) {
	rows, err := database.DB.Query(
		`SELECT id, primary_database_id, workspace_id, name, COALESCE(region,'same-node'), COALESCE(container_id,''), COALESCE(host,''), COALESCE(port,0), COALESCE(status,'creating'), COALESCE(replication_mode,'async'), COALESCE(lag_seconds,0), COALESCE(promoted,false), created_at, updated_at
		   FROM managed_database_replicas
		  WHERE primary_database_id=$1
		  ORDER BY created_at ASC`,
		primaryDatabaseID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DatabaseReplica
	for rows.Next() {
		var r DatabaseReplica
		if err := rows.Scan(&r.ID, &r.PrimaryDatabaseID, &r.WorkspaceID, &r.Name, &r.Region, &r.ContainerID, &r.Host, &r.Port, &r.Status, &r.ReplicationMode, &r.LagSeconds, &r.Promoted, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func GetDatabaseReplica(id string) (*DatabaseReplica, error) {
	var r DatabaseReplica
	err := database.DB.QueryRow(
		`SELECT id, primary_database_id, workspace_id, name, COALESCE(region,'same-node'), COALESCE(container_id,''), COALESCE(host,''), COALESCE(port,0), COALESCE(status,'creating'), COALESCE(replication_mode,'async'), COALESCE(lag_seconds,0), COALESCE(promoted,false), created_at, updated_at
		   FROM managed_database_replicas
		  WHERE id=$1`,
		id,
	).Scan(&r.ID, &r.PrimaryDatabaseID, &r.WorkspaceID, &r.Name, &r.Region, &r.ContainerID, &r.Host, &r.Port, &r.Status, &r.ReplicationMode, &r.LagSeconds, &r.Promoted, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func PromoteDatabaseReplica(id string) error {
	_, err := database.DB.Exec(
		"UPDATE managed_database_replicas SET promoted=true, status='promoted', updated_at=NOW() WHERE id=$1",
		id,
	)
	return err
}

func DeleteDatabaseReplica(id string) error {
	_, err := database.DB.Exec("DELETE FROM managed_database_replicas WHERE id=$1", id)
	return err
}
