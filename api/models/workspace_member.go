package models

import (
	"database/sql"
	"strings"
	"time"

	"github.com/railpush/api/database"
)

const (
	RoleOwner     = "owner"
	RoleAdmin     = "admin"
	RoleDeveloper = "developer"
	RoleViewer    = "viewer"
)

type WorkspaceMember struct {
	WorkspaceID string    `json:"workspace_id"`
	UserID      string    `json:"user_id"`
	Role        string    `json:"role"`
	Email       string    `json:"email,omitempty"`
	Username    string    `json:"username,omitempty"`
	JoinedAt    time.Time `json:"joined_at"`
}

func NormalizeWorkspaceRole(role string) string {
	clean := strings.ToLower(strings.TrimSpace(role))
	switch clean {
	case RoleOwner, RoleAdmin, RoleDeveloper, RoleViewer:
		return clean
	default:
		return RoleViewer
	}
}

func EnsureWorkspaceOwnerMember(workspaceID, ownerUserID string) error {
	_, err := database.DB.Exec(
		"INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, $3) ON CONFLICT (workspace_id, user_id) DO UPDATE SET role=EXCLUDED.role",
		workspaceID, ownerUserID, RoleOwner,
	)
	return err
}

func AddWorkspaceMember(workspaceID, userID, role string) error {
	role = NormalizeWorkspaceRole(role)
	_, err := database.DB.Exec(
		"INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, $3) ON CONFLICT (workspace_id, user_id) DO UPDATE SET role=EXCLUDED.role",
		workspaceID, userID, role,
	)
	return err
}

func RemoveWorkspaceMember(workspaceID, userID string) error {
	_, err := database.DB.Exec(
		"DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2",
		workspaceID, userID,
	)
	return err
}

func GetWorkspaceMemberRole(workspaceID, userID string) (string, error) {
	var role string
	err := database.DB.QueryRow(
		"SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2",
		workspaceID, userID,
	).Scan(&role)
	if err == sql.ErrNoRows {
		ws, wsErr := GetWorkspace(workspaceID)
		if wsErr != nil {
			return "", wsErr
		}
		if ws != nil && ws.OwnerID == userID {
			return RoleOwner, nil
		}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return role, nil
}

func IsWorkspaceMember(workspaceID, userID string) (bool, error) {
	role, err := GetWorkspaceMemberRole(workspaceID, userID)
	if err != nil {
		return false, err
	}
	return role != "", nil
}

func ListWorkspaceMembers(workspaceID string) ([]WorkspaceMember, error) {
	rows, err := database.DB.Query(
		`SELECT wm.workspace_id, wm.user_id, COALESCE(wm.role, 'viewer'),
		        COALESCE(u.email,''), COALESCE(u.username,''), COALESCE(u.created_at, NOW())
		   FROM workspace_members wm
		   JOIN users u ON u.id = wm.user_id
		  WHERE wm.workspace_id=$1
		  ORDER BY CASE wm.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 WHEN 'developer' THEN 2 ELSE 3 END, u.created_at ASC`,
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WorkspaceMember
	for rows.Next() {
		var m WorkspaceMember
		if err := rows.Scan(&m.WorkspaceID, &m.UserID, &m.Role, &m.Email, &m.Username, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
