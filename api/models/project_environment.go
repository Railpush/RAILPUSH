package models

import (
	"database/sql"
	"strings"
	"time"

	"github.com/railpush/api/database"
)

type Project struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	FolderID    *string   `json:"folder_id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
}

type ProjectFolder struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	ParentID    *string   `json:"parent_id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
}

type Environment struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Name        string    `json:"name"`
	IsProtected bool      `json:"is_protected"`
	CreatedAt   time.Time `json:"created_at"`
}

func strPtrOrNil(v string) *string {
	clean := strings.TrimSpace(v)
	if clean == "" {
		return nil
	}
	cp := clean
	return &cp
}

func CreateProject(p *Project) error {
	var folderID interface{}
	if p.FolderID != nil && strings.TrimSpace(*p.FolderID) != "" {
		folderID = strings.TrimSpace(*p.FolderID)
	}

	return database.DB.QueryRow(
		"INSERT INTO projects (workspace_id, folder_id, name) VALUES ($1, $2, $3) RETURNING id, created_at",
		p.WorkspaceID, folderID, p.Name,
	).Scan(&p.ID, &p.CreatedAt)
}

func GetProject(id string) (*Project, error) {
	p := &Project{}
	var folderID string
	err := database.DB.QueryRow(
		"SELECT id, workspace_id, COALESCE(folder_id::text,''), name, created_at FROM projects WHERE id=$1",
		id,
	).Scan(&p.ID, &p.WorkspaceID, &folderID, &p.Name, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	p.FolderID = strPtrOrNil(folderID)
	return p, err
}

func ListProjects(workspaceID string) ([]Project, error) {
	rows, err := database.DB.Query(
		"SELECT id, workspace_id, COALESCE(folder_id::text,''), name, created_at FROM projects WHERE workspace_id=$1 ORDER BY created_at DESC",
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		var p Project
		var folderID string
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &folderID, &p.Name, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.FolderID = strPtrOrNil(folderID)
		out = append(out, p)
	}
	return out, nil
}

func UpdateProject(p *Project) error {
	var folderID interface{}
	if p.FolderID != nil && strings.TrimSpace(*p.FolderID) != "" {
		folderID = strings.TrimSpace(*p.FolderID)
	}
	_, err := database.DB.Exec(
		"UPDATE projects SET name=$1, folder_id=$2 WHERE id=$3",
		p.Name, folderID, p.ID,
	)
	return err
}

func DeleteProject(id string) error {
	tx, err := database.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Keep services intact while removing project linkage.
	if _, err := tx.Exec(
		`UPDATE services
		    SET project_id=NULL, environment_id=NULL, updated_at=NOW()
		  WHERE project_id=$1
		     OR environment_id IN (SELECT id FROM environments WHERE project_id=$1)`,
		id,
	); err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM projects WHERE id=$1", id); err != nil {
		return err
	}

	return tx.Commit()
}

func CreateProjectFolder(f *ProjectFolder) error {
	var parentID interface{}
	if f.ParentID != nil && strings.TrimSpace(*f.ParentID) != "" {
		parentID = strings.TrimSpace(*f.ParentID)
	}
	return database.DB.QueryRow(
		"INSERT INTO project_folders (workspace_id, parent_id, name) VALUES ($1, $2, $3) RETURNING id, created_at",
		f.WorkspaceID, parentID, f.Name,
	).Scan(&f.ID, &f.CreatedAt)
}

func GetProjectFolder(id string) (*ProjectFolder, error) {
	f := &ProjectFolder{}
	var parentID string
	err := database.DB.QueryRow(
		"SELECT id, workspace_id, COALESCE(parent_id::text,''), name, created_at FROM project_folders WHERE id=$1",
		id,
	).Scan(&f.ID, &f.WorkspaceID, &parentID, &f.Name, &f.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	f.ParentID = strPtrOrNil(parentID)
	return f, err
}

func ListProjectFolders(workspaceID string) ([]ProjectFolder, error) {
	rows, err := database.DB.Query(
		"SELECT id, workspace_id, COALESCE(parent_id::text,''), name, created_at FROM project_folders WHERE workspace_id=$1 ORDER BY created_at ASC",
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProjectFolder
	for rows.Next() {
		var f ProjectFolder
		var parentID string
		if err := rows.Scan(&f.ID, &f.WorkspaceID, &parentID, &f.Name, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.ParentID = strPtrOrNil(parentID)
		out = append(out, f)
	}
	return out, nil
}

func UpdateProjectFolder(f *ProjectFolder) error {
	var parentID interface{}
	if f.ParentID != nil && strings.TrimSpace(*f.ParentID) != "" {
		parentID = strings.TrimSpace(*f.ParentID)
	}
	_, err := database.DB.Exec(
		"UPDATE project_folders SET name=$1, parent_id=$2 WHERE id=$3",
		f.Name, parentID, f.ID,
	)
	return err
}

// FolderDepth returns how many ancestors a folder has (0 = root).
func FolderDepth(id string) (int, error) {
	var depth int
	err := database.DB.QueryRow(
		`WITH RECURSIVE ancestors AS (
			SELECT id, parent_id, 0 AS depth FROM project_folders WHERE id=$1
			UNION ALL
			SELECT pf.id, pf.parent_id, a.depth+1 FROM project_folders pf JOIN ancestors a ON pf.id=a.parent_id
		) SELECT COALESCE(MAX(depth),0) FROM ancestors`,
		id,
	).Scan(&depth)
	return depth, err
}

// FolderDescendantIDs returns all descendant folder IDs (for cycle detection).
func FolderDescendantIDs(id string) ([]string, error) {
	rows, err := database.DB.Query(
		`WITH RECURSIVE descendants AS (
			SELECT id FROM project_folders WHERE parent_id=$1
			UNION ALL
			SELECT pf.id FROM project_folders pf JOIN descendants d ON pf.parent_id=d.id
		) SELECT id FROM descendants`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		ids = append(ids, sid)
	}
	return ids, nil
}

func DeleteProjectFolder(id string) error {
	tx, err := database.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE projects SET folder_id=NULL WHERE folder_id=$1", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM project_folders WHERE id=$1", id); err != nil {
		return err
	}

	return tx.Commit()
}

func CreateEnvironment(e *Environment) error {
	return database.DB.QueryRow(
		"INSERT INTO environments (project_id, name, is_protected) VALUES ($1, $2, $3) RETURNING id, created_at",
		e.ProjectID, e.Name, e.IsProtected,
	).Scan(&e.ID, &e.CreatedAt)
}

func GetEnvironment(id string) (*Environment, error) {
	e := &Environment{}
	err := database.DB.QueryRow(
		"SELECT id, project_id, name, COALESCE(is_protected,false), created_at FROM environments WHERE id=$1",
		id,
	).Scan(&e.ID, &e.ProjectID, &e.Name, &e.IsProtected, &e.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return e, err
}

func ListEnvironments(projectID string) ([]Environment, error) {
	rows, err := database.DB.Query(
		"SELECT id, project_id, name, COALESCE(is_protected,false), created_at FROM environments WHERE project_id=$1 ORDER BY created_at ASC",
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Environment
	for rows.Next() {
		var e Environment
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Name, &e.IsProtected, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func UpdateEnvironment(e *Environment) error {
	_, err := database.DB.Exec(
		"UPDATE environments SET name=$1, is_protected=$2 WHERE id=$3",
		e.Name, e.IsProtected, e.ID,
	)
	return err
}

func DeleteEnvironment(id string) error {
	_, err := database.DB.Exec("DELETE FROM environments WHERE id=$1", id)
	return err
}

func ListProjectEnvironments(workspaceID string) ([]Environment, error) {
	rows, err := database.DB.Query(
		"SELECT e.id, e.project_id, e.name, COALESCE(e.is_protected,false), e.created_at FROM environments e JOIN projects p ON p.id=e.project_id WHERE p.workspace_id=$1 ORDER BY e.created_at ASC",
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Environment
	for rows.Next() {
		var e Environment
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Name, &e.IsProtected, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
