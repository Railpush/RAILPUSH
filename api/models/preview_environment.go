package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type PreviewEnvironment struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	ServiceID   *string    `json:"service_id"`
	Repository  string     `json:"repository"`
	PRNumber    int        `json:"pr_number"`
	PRTitle     string     `json:"pr_title"`
	PRBranch    string     `json:"pr_branch"`
	BaseBranch  string     `json:"base_branch"`
	CommitSHA   string     `json:"commit_sha"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at"`
}

func CreateOrUpdatePreviewEnvironment(pe *PreviewEnvironment) error {
	return database.DB.QueryRow(
		`INSERT INTO preview_environments (workspace_id, service_id, repository, pr_number, pr_title, pr_branch, base_branch, commit_sha, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		 ON CONFLICT (workspace_id, repository, pr_number)
		 DO UPDATE SET
		   service_id=EXCLUDED.service_id,
		   pr_title=EXCLUDED.pr_title,
		   pr_branch=EXCLUDED.pr_branch,
		   base_branch=EXCLUDED.base_branch,
		   commit_sha=EXCLUDED.commit_sha,
		   status=EXCLUDED.status,
		   updated_at=NOW()
		 RETURNING id, created_at, updated_at`,
		pe.WorkspaceID, pe.ServiceID, pe.Repository, pe.PRNumber, pe.PRTitle, pe.PRBranch, pe.BaseBranch, pe.CommitSHA, pe.Status,
	).Scan(&pe.ID, &pe.CreatedAt, &pe.UpdatedAt)
}

func GetPreviewEnvironmentByRepoPR(workspaceID, repository string, prNumber int) (*PreviewEnvironment, error) {
	pe := &PreviewEnvironment{}
	var serviceID sql.NullString
	err := database.DB.QueryRow(
		`SELECT id, workspace_id, service_id::text, repository, pr_number, COALESCE(pr_title,''), COALESCE(pr_branch,''), COALESCE(base_branch,''), COALESCE(commit_sha,''), COALESCE(status,''), created_at, updated_at, closed_at
		   FROM preview_environments
		  WHERE workspace_id=$1 AND repository=$2 AND pr_number=$3`,
		workspaceID, repository, prNumber,
	).Scan(
		&pe.ID, &pe.WorkspaceID, &serviceID, &pe.Repository, &pe.PRNumber, &pe.PRTitle, &pe.PRBranch, &pe.BaseBranch, &pe.CommitSHA, &pe.Status, &pe.CreatedAt, &pe.UpdatedAt, &pe.ClosedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if serviceID.Valid && serviceID.String != "" {
		pe.ServiceID = &serviceID.String
	}
	return pe, nil
}

func ListPreviewEnvironments(workspaceID string) ([]PreviewEnvironment, error) {
	rows, err := database.DB.Query(
		`SELECT id, workspace_id, service_id::text, repository, pr_number, COALESCE(pr_title,''), COALESCE(pr_branch,''), COALESCE(base_branch,''), COALESCE(commit_sha,''), COALESCE(status,''), created_at, updated_at, closed_at
		   FROM preview_environments
		  WHERE workspace_id=$1
		  ORDER BY updated_at DESC`,
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PreviewEnvironment
	for rows.Next() {
		var pe PreviewEnvironment
		var serviceID sql.NullString
		if err := rows.Scan(
			&pe.ID, &pe.WorkspaceID, &serviceID, &pe.Repository, &pe.PRNumber, &pe.PRTitle, &pe.PRBranch, &pe.BaseBranch, &pe.CommitSHA, &pe.Status, &pe.CreatedAt, &pe.UpdatedAt, &pe.ClosedAt,
		); err != nil {
			return nil, err
		}
		if serviceID.Valid && serviceID.String != "" {
			pe.ServiceID = &serviceID.String
		}
		out = append(out, pe)
	}
	return out, nil
}

func MarkPreviewEnvironmentClosed(workspaceID, repository string, prNumber int) error {
	_, err := database.DB.Exec(
		`UPDATE preview_environments
		    SET status='closed', closed_at=NOW(), updated_at=NOW()
		  WHERE workspace_id=$1 AND repository=$2 AND pr_number=$3`,
		workspaceID, repository, prNumber,
	)
	return err
}
