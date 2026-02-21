package models

import (
	"database/sql"
	"fmt"
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
	ExternalPort      int       `json:"external_port"`
	DBName            string    `json:"db_name"`
	Username          string    `json:"username"`
	EncryptedPassword string    `json:"-"`
	Status            string    `json:"status"`
	HAEnabled         bool      `json:"ha_enabled"`
	HAStrategy        string    `json:"ha_strategy"`
	StandbyReplicaID  *string   `json:"standby_replica_id"`
	InitScript        string    `json:"init_script"`
	CreatedAt         time.Time `json:"created_at"`
}

func CreateManagedDatabase(d *ManagedDatabase) error {
	return database.DB.QueryRow("INSERT INTO managed_databases (workspace_id, name, plan, pg_version, host, port, db_name, username, encrypted_password, init_script) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id, status, created_at",
		d.WorkspaceID, d.Name, d.Plan, d.PGVersion, d.Host, d.Port, d.DBName, d.Username, d.EncryptedPassword, d.InitScript).Scan(&d.ID, &d.Status, &d.CreatedAt)
}

func GetManagedDatabase(id string) (*ManagedDatabase, error) {
	d := &ManagedDatabase{}
	var standbyReplicaID sql.NullString
	var externalPort sql.NullInt64
	err := database.DB.QueryRow("SELECT id, COALESCE(workspace_id::text,''), name, COALESCE(plan,'starter'), COALESCE(pg_version,16), COALESCE(container_id,''), COALESCE(host,'localhost'), COALESCE(port,5432), external_port, COALESCE(db_name,''), COALESCE(username,''), COALESCE(encrypted_password,''), COALESCE(status,'creating'), COALESCE(ha_enabled,false), COALESCE(ha_strategy,'none'), standby_replica_id::text, COALESCE(init_script,''), created_at FROM managed_databases WHERE id=$1", id).Scan(
		&d.ID, &d.WorkspaceID, &d.Name, &d.Plan, &d.PGVersion, &d.ContainerID, &d.Host, &d.Port, &externalPort, &d.DBName, &d.Username, &d.EncryptedPassword, &d.Status, &d.HAEnabled, &d.HAStrategy, &standbyReplicaID, &d.InitScript, &d.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if externalPort.Valid {
		d.ExternalPort = int(externalPort.Int64)
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
	query := "SELECT id, COALESCE(workspace_id::text,''), name, COALESCE(plan,'starter'), COALESCE(pg_version,16), COALESCE(container_id,''), COALESCE(host,'localhost'), COALESCE(port,5432), external_port, COALESCE(db_name,''), COALESCE(username,''), COALESCE(status,'creating'), COALESCE(ha_enabled,false), COALESCE(ha_strategy,'none'), standby_replica_id::text, COALESCE(init_script,''), created_at FROM managed_databases"
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
		var externalPort sql.NullInt64
		if err := rows.Scan(&d.ID, &d.WorkspaceID, &d.Name, &d.Plan, &d.PGVersion, &d.ContainerID, &d.Host, &d.Port, &externalPort, &d.DBName, &d.Username, &d.Status, &d.HAEnabled, &d.HAStrategy, &standbyReplicaID, &d.InitScript, &d.CreatedAt); err != nil {
			return nil, err
		}
		if externalPort.Valid {
			d.ExternalPort = int(externalPort.Int64)
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

func UpdateManagedDatabasePlan(id, plan string) error {
	_, err := database.DB.Exec("UPDATE managed_databases SET plan=$1 WHERE id=$2", plan, id)
	return err
}

func UpdateManagedDatabaseHA(id string, enabled bool, strategy string, standbyReplicaID *string) error {
	_, err := database.DB.Exec(
		"UPDATE managed_databases SET ha_enabled=$1, ha_strategy=$2, standby_replica_id=NULLIF($3,'')::uuid WHERE id=$4",
		enabled, strategy, derefOrEmpty(standbyReplicaID), id,
	)
	return err
}

// AllocateExternalPort assigns the next free external TCP port from the range
// [20000, 25000) and stores it atomically. Returns the allocated port.
func AllocateExternalPort(dbID string) (int, error) {
	const minPort, maxPort = 20000, 25000
	var port int
	err := database.DB.QueryRow(`
		UPDATE managed_databases
		SET external_port = (
			SELECT p FROM generate_series($1::int, $2::int) AS p
			WHERE p NOT IN (SELECT external_port FROM managed_databases WHERE external_port IS NOT NULL)
			ORDER BY p LIMIT 1
		)
		WHERE id = $3 AND external_port IS NULL
		RETURNING external_port`, minPort, maxPort-1, dbID).Scan(&port)
	if err != nil {
		return 0, fmt.Errorf("allocate external port: %w", err)
	}
	return port, nil
}

// SetExternalPort updates the external_port for a database (used during backfill).
func SetExternalPort(dbID string, port int) error {
	_, err := database.DB.Exec("UPDATE managed_databases SET external_port=$1 WHERE id=$2", port, dbID)
	return err
}

// ClearExternalPort removes the external_port assignment (used on deletion).
func ClearExternalPort(dbID string) error {
	_, err := database.DB.Exec("UPDATE managed_databases SET external_port=NULL WHERE id=$1", dbID)
	return err
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
