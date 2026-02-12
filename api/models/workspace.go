package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type Workspace struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	OwnerID      string    `json:"owner_id"`
	DeployPolicy string    `json:"deploy_policy"`
	CreatedAt    time.Time `json:"created_at"`
}

func CreateWorkspace(w *Workspace) error {
	if err := database.DB.QueryRow(
		"INSERT INTO workspaces (name, owner_id, deploy_policy) VALUES ($1, $2, $3) RETURNING id, created_at",
		w.Name, w.OwnerID, w.DeployPolicy,
	).Scan(&w.ID, &w.CreatedAt); err != nil {
		return err
	}
	return EnsureWorkspaceOwnerMember(w.ID, w.OwnerID)
}

func GetWorkspaceByOwner(ownerID string) (*Workspace, error) {
	w := &Workspace{}
	err := database.DB.QueryRow(
		"SELECT id, name, owner_id, COALESCE(deploy_policy, 'cancel'), created_at FROM workspaces WHERE owner_id = $1 LIMIT 1", ownerID,
	).Scan(&w.ID, &w.Name, &w.OwnerID, &w.DeployPolicy, &w.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return w, err
}

func GetWorkspace(id string) (*Workspace, error) {
	w := &Workspace{}
	err := database.DB.QueryRow(
		"SELECT id, name, owner_id, COALESCE(deploy_policy, 'cancel'), created_at FROM workspaces WHERE id = $1", id,
	).Scan(&w.ID, &w.Name, &w.OwnerID, &w.DeployPolicy, &w.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return w, err
}
