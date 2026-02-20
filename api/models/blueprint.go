package models

import (
	"database/sql"
	"strings"
	"time"

	"github.com/railpush/api/database"
)

type Blueprint struct {
	ID             string     `json:"id"`
	WorkspaceID    string     `json:"workspace_id"`
	Name           string     `json:"name"`
	RepoURL        string     `json:"repo_url"`
	Branch         string     `json:"branch"`
	FilePath       string     `json:"file_path"`
	// GeneratedYAML stores an AI-generated blueprint (e.g. when railpush.yaml is missing in the repo).
	// It is intentionally not returned in the public API by default to avoid large payloads.
	GeneratedYAML    string     `json:"-"`
	AIIgnoreRepoYAML bool       `json:"ai_ignore_repo_yaml"`
	LastSyncedAt     *time.Time `json:"last_synced_at"`
	LastSyncStatus   string     `json:"last_sync_status"`
	SyncLog          string     `json:"sync_log,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type BlueprintResource struct {
	BlueprintID  string `json:"blueprint_id"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	ResourceName string `json:"resource_name"`
}

func CreateBlueprint(b *Blueprint) error {
	return database.DB.QueryRow(
		"INSERT INTO blueprints (workspace_id, name, repo_url, branch, file_path, ai_ignore_repo_yaml) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at",
		b.WorkspaceID, b.Name, b.RepoURL, b.Branch, b.FilePath, b.AIIgnoreRepoYAML,
	).Scan(&b.ID, &b.CreatedAt)
}

const blueprintSelectCols = `id, workspace_id, name, COALESCE(repo_url,''), COALESCE(branch,'main'), COALESCE(file_path,'railpush.yaml'), COALESCE(ai_ignore_repo_yaml,false), last_synced_at, COALESCE(last_sync_status,''), COALESCE(sync_log,''), created_at`
const blueprintSelectColsWithGenerated = blueprintSelectCols + `, COALESCE(generated_yaml,'')`

func GetBlueprint(id string) (*Blueprint, error) {
	b := &Blueprint{}
	err := database.DB.QueryRow("SELECT "+blueprintSelectColsWithGenerated+" FROM blueprints WHERE id=$1", id).Scan(
		&b.ID, &b.WorkspaceID, &b.Name, &b.RepoURL, &b.Branch, &b.FilePath, &b.AIIgnoreRepoYAML, &b.LastSyncedAt, &b.LastSyncStatus, &b.SyncLog, &b.CreatedAt, &b.GeneratedYAML)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

func ListBlueprints() ([]Blueprint, error) {
	rows, err := database.DB.Query("SELECT " + blueprintSelectCols + " FROM blueprints ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bps []Blueprint
	for rows.Next() {
		var b Blueprint
		if err := rows.Scan(&b.ID, &b.WorkspaceID, &b.Name, &b.RepoURL, &b.Branch, &b.FilePath, &b.AIIgnoreRepoYAML, &b.LastSyncedAt, &b.LastSyncStatus, &b.SyncLog, &b.CreatedAt); err != nil {
			return nil, err
		}
		bps = append(bps, b)
	}
	return bps, nil
}

func ListBlueprintsByWorkspace(workspaceID string) ([]Blueprint, error) {
	rows, err := database.DB.Query("SELECT "+blueprintSelectCols+" FROM blueprints WHERE workspace_id=$1 ORDER BY created_at DESC", workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bps []Blueprint
	for rows.Next() {
		var b Blueprint
		if err := rows.Scan(&b.ID, &b.WorkspaceID, &b.Name, &b.RepoURL, &b.Branch, &b.FilePath, &b.AIIgnoreRepoYAML, &b.LastSyncedAt, &b.LastSyncStatus, &b.SyncLog, &b.CreatedAt); err != nil {
			return nil, err
		}
		bps = append(bps, b)
	}
	return bps, nil
}

func DeleteBlueprint(id string) error {
	_, err := database.DB.Exec("DELETE FROM blueprints WHERE id=$1", id)
	return err
}

func UpdateBlueprintSync(id, status string) error {
	_, err := database.DB.Exec("UPDATE blueprints SET last_synced_at=NOW(), last_sync_status=$1 WHERE id=$2", status, id)
	return err
}

func UpdateBlueprintSyncLog(id, syncLog string) error {
	_, err := database.DB.Exec("UPDATE blueprints SET sync_log=$1 WHERE id=$2", syncLog, id)
	return err
}

func UpdateBlueprintSyncWithLog(id, status, syncLog string) error {
	_, err := database.DB.Exec("UPDATE blueprints SET last_synced_at=NOW(), last_sync_status=$1, sync_log=$2 WHERE id=$3", status, syncLog, id)
	return err
}

func UpdateBlueprintGeneratedYAML(id string, generatedYAML string) error {
	_, err := database.DB.Exec("UPDATE blueprints SET generated_yaml=$1 WHERE id=$2", generatedYAML, id)
	return err
}

func DeleteBlueprintResourcesByResource(resourceType string, resourceID string) error {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if resourceType == "" || resourceID == "" {
		return nil
	}
	_, err := database.DB.Exec("DELETE FROM blueprint_resources WHERE resource_type=$1 AND resource_id=$2", resourceType, resourceID)
	return err
}

func CreateBlueprintResource(br *BlueprintResource) error {
	_, err := database.DB.Exec("INSERT INTO blueprint_resources (blueprint_id, resource_type, resource_id, resource_name) VALUES ($1,$2,$3,$4)",
		br.BlueprintID, br.ResourceType, br.ResourceID, br.ResourceName)
	return err
}

func DeleteBlueprintResource(br *BlueprintResource) error {
	if br == nil {
		return nil
	}
	_, err := database.DB.Exec(
		"DELETE FROM blueprint_resources WHERE blueprint_id=$1 AND resource_type=$2 AND resource_id=$3 AND resource_name=$4",
		br.BlueprintID, br.ResourceType, br.ResourceID, br.ResourceName,
	)
	return err
}

func ListBlueprintResources(blueprintID string) ([]BlueprintResource, error) {
	rows, err := database.DB.Query("SELECT blueprint_id, resource_type, resource_id, resource_name FROM blueprint_resources WHERE blueprint_id=$1", blueprintID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var resources []BlueprintResource
	for rows.Next() {
		var r BlueprintResource
		if err := rows.Scan(&r.BlueprintID, &r.ResourceType, &r.ResourceID, &r.ResourceName); err != nil {
			return nil, err
		}
		resources = append(resources, r)
	}
	return resources, nil
}

func GetBlueprintResourceByName(blueprintID, resourceType, name string) (*BlueprintResource, error) {
	r := &BlueprintResource{}
	err := database.DB.QueryRow("SELECT blueprint_id, resource_type, resource_id, resource_name FROM blueprint_resources WHERE blueprint_id=$1 AND resource_type=$2 AND resource_name=$3",
		blueprintID, resourceType, name).Scan(&r.BlueprintID, &r.ResourceType, &r.ResourceID, &r.ResourceName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func DeleteBlueprintResources(blueprintID string) error {
	_, err := database.DB.Exec("DELETE FROM blueprint_resources WHERE blueprint_id=$1", blueprintID)
	return err
}
