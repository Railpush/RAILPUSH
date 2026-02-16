package models

import (
	"encoding/json"
	"time"

	"github.com/railpush/api/database"
)

type AuditLogEntry struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	UserID       string          `json:"user_id"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	DetailsJSON  json.RawMessage `json:"details_json"`
	CreatedAt    time.Time       `json:"created_at"`
}

func CreateAuditLog(workspaceID, userID, action, resourceType, resourceID string, details interface{}) error {
	payload, _ := json.Marshal(details)
	_, err := database.DB.Exec(
		`INSERT INTO audit_log (workspace_id, user_id, action, resource_type, resource_id, details_json, created_at)
		 VALUES (NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, $3, $4, NULLIF($5, '')::uuid, $6::jsonb, NOW())`,
		workspaceID, userID, action, resourceType, resourceID, string(payload),
	)
	return err
}

func ListAuditLogs(workspaceID string, limit int) ([]AuditLogEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := database.DB.Query(
		`SELECT id, COALESCE(workspace_id::text,''), COALESCE(user_id::text,''), COALESCE(action,''), COALESCE(resource_type,''), COALESCE(resource_id::text,''), COALESCE(details_json::text,'{}'), created_at
		   FROM audit_log
		  WHERE workspace_id=$1
		  ORDER BY created_at DESC
		  LIMIT $2`,
		workspaceID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditLogEntry
	for rows.Next() {
		var e AuditLogEntry
		var details string
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.UserID, &e.Action, &e.ResourceType, &e.ResourceID, &details, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.DetailsJSON = json.RawMessage(details)
		out = append(out, e)
	}
	return out, nil
}
