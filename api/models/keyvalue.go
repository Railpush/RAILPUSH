package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type ManagedKeyValue struct {
	ID                string    `json:"id"`
	WorkspaceID       string    `json:"workspace_id"`
	Name              string    `json:"name"`
	Plan              string    `json:"plan"`
	ContainerID       string    `json:"container_id"`
	Host              string    `json:"host"`
	Port              int       `json:"port"`
	EncryptedPassword string    `json:"-"`
	MaxmemoryPolicy   string    `json:"maxmemory_policy"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
}

func CreateManagedKeyValue(kv *ManagedKeyValue) error {
	return database.DB.QueryRow("INSERT INTO managed_keyvalue (workspace_id, name, plan, host, port, encrypted_password, maxmemory_policy) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, status, created_at",
		kv.WorkspaceID, kv.Name, kv.Plan, kv.Host, kv.Port, kv.EncryptedPassword, kv.MaxmemoryPolicy).Scan(&kv.ID, &kv.Status, &kv.CreatedAt)
}

func GetManagedKeyValue(id string) (*ManagedKeyValue, error) {
	kv := &ManagedKeyValue{}
	err := database.DB.QueryRow("SELECT id, COALESCE(workspace_id::text,''), name, COALESCE(plan,'starter'), COALESCE(container_id,''), COALESCE(host,'localhost'), COALESCE(port,6379), COALESCE(encrypted_password,''), COALESCE(maxmemory_policy,'allkeys-lru'), COALESCE(status,'creating'), created_at FROM managed_keyvalue WHERE id=$1", id).Scan(
		&kv.ID, &kv.WorkspaceID, &kv.Name, &kv.Plan, &kv.ContainerID, &kv.Host, &kv.Port, &kv.EncryptedPassword, &kv.MaxmemoryPolicy, &kv.Status, &kv.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return kv, err
}

func ListManagedKeyValues() ([]ManagedKeyValue, error) {
	return ListManagedKeyValuesByWorkspace("")
}

func ListManagedKeyValuesByWorkspace(workspaceID string) ([]ManagedKeyValue, error) {
	query := "SELECT id, workspace_id, name, plan, container_id, host, port, maxmemory_policy, status, created_at FROM managed_keyvalue"
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
	var kvs []ManagedKeyValue
	for rows.Next() {
		var kv ManagedKeyValue
		if err := rows.Scan(&kv.ID, &kv.WorkspaceID, &kv.Name, &kv.Plan, &kv.ContainerID, &kv.Host, &kv.Port, &kv.MaxmemoryPolicy, &kv.Status, &kv.CreatedAt); err != nil {
			return nil, err
		}
		kvs = append(kvs, kv)
	}
	return kvs, nil
}

func DeleteManagedKeyValue(id string) error {
	_, err := database.DB.Exec("DELETE FROM managed_keyvalue WHERE id=$1", id)
	return err
}

func UpdateManagedKeyValueStatus(id, status, containerID string) error {
	_, err := database.DB.Exec("UPDATE managed_keyvalue SET status=$1, container_id=$2 WHERE id=$3", status, containerID, id)
	return err
}

func UpdateManagedKeyValueConnection(id string, port int, host string) error {
	_, err := database.DB.Exec("UPDATE managed_keyvalue SET port=$1, host=$2 WHERE id=$3", port, host, id)
	return err
}

func UpdateManagedKeyValuePlan(id, plan string) error {
	_, err := database.DB.Exec("UPDATE managed_keyvalue SET plan=$1 WHERE id=$2", plan, id)
	return err
}
