package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type ManagedDatabase struct {
	ID                string    `json:"id"`
	WorkspaceID       string    `json:"workspace_id"`
	Name              string    `json:"name"`
	Plan              string    `json:"plan"`
	PGVersion         int       `json:"pg_version"`
	ContainerID       string    `json:"container_id"`
	Host              string    `json:"host"`
	Port              int       `json:"port"`
	DBName            string    `json:"db_name"`
	Username          string    `json:"username"`
	EncryptedPassword string    `json:"-"`
	Status            string    `json:"status"`
	HAEnabled         bool      `json:"ha_enabled"`
	HAStrategy        string    `json:"ha_strategy"`
	StandbyReplicaID  *string   `json:"standby_replica_id"`
	CreatedAt         time.Time `json:"created_at"`
}

func CreateManagedDatabase(d *ManagedDatabase) error {
	return database.DB.QueryRow("INSERT INTO managed_databases (workspace_id, name, plan, pg_version, host, port, db_name, username, encrypted_password) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id, status, created_at",
		d.WorkspaceID, d.Name, d.Plan, d.PGVersion, d.Host, d.Port, d.DBName, d.Username, d.EncryptedPassword).Scan(&d.ID, &d.Status, &d.CreatedAt)
}

func GetManagedDatabase(id string) (*ManagedDatabase, error) {
	d := &ManagedDatabase{}
	var standbyReplicaID sql.NullString
	err := database.DB.QueryRow("SELECT id, COALESCE(workspace_id::text,''), name, COALESCE(plan,'starter'), COALESCE(pg_version,16), COALESCE(container_id,''), COALESCE(host,'localhost'), COALESCE(port,5432), COALESCE(db_name,''), COALESCE(username,''), COALESCE(encrypted_password,''), COALESCE(status,'creating'), COALESCE(ha_enabled,false), COALESCE(ha_strategy,'none'), standby_replica_id::text, created_at FROM managed_databases WHERE id=$1", id).Scan(
		&d.ID, &d.WorkspaceID, &d.Name, &d.Plan, &d.PGVersion, &d.ContainerID, &d.Host, &d.Port, &d.DBName, &d.Username, &d.EncryptedPassword, &d.Status, &d.HAEnabled, &d.HAStrategy, &standbyReplicaID, &d.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if standbyReplicaID.Valid && standbyReplicaID.String != "" {
		d.StandbyReplicaID = &standbyReplicaID.String
	}
	return d, err
}

func ListManagedDatabases() ([]ManagedDatabase, error) {
	return ListManagedDatabasesByWorkspace("")
}

func ListManagedDatabasesByWorkspace(workspaceID string) ([]ManagedDatabase, error) {
	query := "SELECT id, workspace_id, name, plan, pg_version, container_id, host, port, db_name, username, status, COALESCE(ha_enabled,false), COALESCE(ha_strategy,'none'), standby_replica_id::text, created_at FROM managed_databases"
	var (
		rows *sql.Rows
		err  error
	)
	if workspaceID != "" {
		rows, err = database.DB.Query(query+" WHERE workspace_id=$1 ORDER BY created_at DESC", workspaceID)
	} else {
		rows, err = database.DB.Query(query + " ORDER BY created_at DESC")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dbs []ManagedDatabase
	for rows.Next() {
		var d ManagedDatabase
		var standbyReplicaID sql.NullString
		if err := rows.Scan(&d.ID, &d.WorkspaceID, &d.Name, &d.Plan, &d.PGVersion, &d.ContainerID, &d.Host, &d.Port, &d.DBName, &d.Username, &d.Status, &d.HAEnabled, &d.HAStrategy, &standbyReplicaID, &d.CreatedAt); err != nil {
			return nil, err
		}
		if standbyReplicaID.Valid && standbyReplicaID.String != "" {
			d.StandbyReplicaID = &standbyReplicaID.String
		}
		dbs = append(dbs, d)
	}
	return dbs, nil
}

func DeleteManagedDatabase(id string) error {
	_, err := database.DB.Exec("DELETE FROM managed_databases WHERE id=$1", id)
	return err
}

func UpdateManagedDatabaseStatus(id, status, containerID string) error {
	_, err := database.DB.Exec("UPDATE managed_databases SET status=$1, container_id=$2 WHERE id=$3", status, containerID, id)
	return err
}

func UpdateManagedDatabaseConnection(id string, port int, host string) error {
	_, err := database.DB.Exec("UPDATE managed_databases SET port=$1, host=$2 WHERE id=$3", port, host, id)
	return err
}

func UpdateManagedDatabaseHA(id string, enabled bool, strategy string, standbyReplicaID *string) error {
	_, err := database.DB.Exec(
		"UPDATE managed_databases SET ha_enabled=$1, ha_strategy=$2, standby_replica_id=NULLIF($3,'')::uuid WHERE id=$4",
		enabled, strategy, derefOrEmpty(standbyReplicaID), id,
	)
	return err
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
