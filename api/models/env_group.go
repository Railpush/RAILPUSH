package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type EnvGroup struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
}

func CreateEnvGroup(g *EnvGroup) error {
	return database.DB.QueryRow("INSERT INTO env_groups (workspace_id, name) VALUES ($1,$2) RETURNING id, created_at",
		g.WorkspaceID, g.Name).Scan(&g.ID, &g.CreatedAt)
}

func GetEnvGroupByName(workspaceID, name string) (*EnvGroup, error) {
	g := &EnvGroup{}
	err := database.DB.QueryRow("SELECT id, workspace_id, name, created_at FROM env_groups WHERE workspace_id=$1 AND name=$2",
		workspaceID, name).Scan(&g.ID, &g.WorkspaceID, &g.Name, &g.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return g, err
}

func GetEnvGroup(id string) (*EnvGroup, error) {
	g := &EnvGroup{}
	err := database.DB.QueryRow(
		"SELECT id, workspace_id, name, created_at FROM env_groups WHERE id=$1",
		id,
	).Scan(&g.ID, &g.WorkspaceID, &g.Name, &g.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return g, err
}

func ListEnvGroups(workspaceID string) ([]EnvGroup, error) {
	rows, err := database.DB.Query("SELECT id, workspace_id, name, created_at FROM env_groups WHERE workspace_id=$1 ORDER BY name", workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []EnvGroup
	for rows.Next() {
		var g EnvGroup
		if err := rows.Scan(&g.ID, &g.WorkspaceID, &g.Name, &g.CreatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func LinkServiceToEnvGroup(serviceID, envGroupID string) error {
	_, err := database.DB.Exec("INSERT INTO env_group_memberships (service_id, env_group_id) VALUES ($1,$2) ON CONFLICT DO NOTHING",
		serviceID, envGroupID)
	return err
}

func DeleteEnvGroup(id string) error {
	// env_vars.owner_id does not have a FK to env_groups; delete to avoid orphans.
	_ = DeleteEnvVars("env_group", id)
	_, err := database.DB.Exec("DELETE FROM env_groups WHERE id=$1", id)
	return err
}

func UpdateEnvGroup(id, name string) error {
	_, err := database.DB.Exec(
		"UPDATE env_groups SET name=$1 WHERE id=$2",
		name, id,
	)
	return err
}

// ListLinkedEnvGroupIDs returns env group IDs linked to a service, ordered by
// group creation time (earliest first) so that earlier groups win on conflict.
func ListLinkedEnvGroupIDs(serviceID string) ([]string, error) {
	rows, err := database.DB.Query(
		`SELECT m.env_group_id FROM env_group_memberships m
		 JOIN env_groups g ON g.id = m.env_group_id
		 WHERE m.service_id=$1
		 ORDER BY g.created_at ASC`,
		serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func UnlinkServiceFromEnvGroup(serviceID, envGroupID string) error {
	_, err := database.DB.Exec(
		"DELETE FROM env_group_memberships WHERE service_id=$1 AND env_group_id=$2",
		serviceID, envGroupID)
	return err
}

// ListLinkedServices returns service IDs linked to an env group.
func ListLinkedServices(envGroupID string) ([]string, error) {
	rows, err := database.DB.Query(
		"SELECT service_id FROM env_group_memberships WHERE env_group_id=$1",
		envGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
