package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type OneOffJob struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	ServiceID   *string    `json:"service_id"`
	Name        string     `json:"name"`
	Command     string     `json:"command"`
	Status      string     `json:"status"`
	ExitCode    *int       `json:"exit_code"`
	Logs        string     `json:"logs"`
	CreatedBy   *string    `json:"created_by"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func CreateOneOffJob(j *OneOffJob) error {
	return database.DB.QueryRow(
		`INSERT INTO one_off_jobs (workspace_id, service_id, name, command, status, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, COALESCE(NULLIF($5, ''), 'pending'), $6, NOW(), NOW())
		 RETURNING id, created_at, updated_at`,
		j.WorkspaceID, j.ServiceID, j.Name, j.Command, j.Status, j.CreatedBy,
	).Scan(&j.ID, &j.CreatedAt, &j.UpdatedAt)
}

func GetOneOffJob(id string) (*OneOffJob, error) {
	var j OneOffJob
	var serviceID, createdBy sql.NullString
	var exitCode sql.NullInt64
	err := database.DB.QueryRow(
		`SELECT id, workspace_id, service_id::text, name, command, COALESCE(status,'pending'), exit_code, COALESCE(logs,''), created_by::text, started_at, finished_at, created_at, updated_at
		   FROM one_off_jobs
		  WHERE id=$1`,
		id,
	).Scan(
		&j.ID, &j.WorkspaceID, &serviceID, &j.Name, &j.Command, &j.Status, &exitCode, &j.Logs, &createdBy, &j.StartedAt, &j.FinishedAt, &j.CreatedAt, &j.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if serviceID.Valid && serviceID.String != "" {
		j.ServiceID = &serviceID.String
	}
	if createdBy.Valid && createdBy.String != "" {
		j.CreatedBy = &createdBy.String
	}
	if exitCode.Valid {
		v := int(exitCode.Int64)
		j.ExitCode = &v
	}
	return &j, nil
}

func ListOneOffJobsByService(serviceID string, limit int) ([]OneOffJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := database.DB.Query(
		`SELECT id, workspace_id, service_id::text, name, command, COALESCE(status,'pending'), exit_code, COALESCE(logs,''), created_by::text, started_at, finished_at, created_at, updated_at
		   FROM one_off_jobs
		  WHERE service_id=$1
		  ORDER BY created_at DESC
		  LIMIT $2`,
		serviceID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OneOffJob
	for rows.Next() {
		var j OneOffJob
		var serviceIDVal, createdBy sql.NullString
		var exitCode sql.NullInt64
		if err := rows.Scan(
			&j.ID, &j.WorkspaceID, &serviceIDVal, &j.Name, &j.Command, &j.Status, &exitCode, &j.Logs, &createdBy, &j.StartedAt, &j.FinishedAt, &j.CreatedAt, &j.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if serviceIDVal.Valid && serviceIDVal.String != "" {
			j.ServiceID = &serviceIDVal.String
		}
		if createdBy.Valid && createdBy.String != "" {
			j.CreatedBy = &createdBy.String
		}
		if exitCode.Valid {
			v := int(exitCode.Int64)
			j.ExitCode = &v
		}
		out = append(out, j)
	}
	return out, nil
}

func MarkOneOffJobRunning(id string) error {
	_, err := database.DB.Exec(
		"UPDATE one_off_jobs SET status='running', started_at=NOW(), updated_at=NOW() WHERE id=$1",
		id,
	)
	return err
}

func CompleteOneOffJob(id, status, logs string, exitCode int) error {
	_, err := database.DB.Exec(
		"UPDATE one_off_jobs SET status=$1, logs=$2, exit_code=$3, finished_at=NOW(), updated_at=NOW() WHERE id=$4",
		status, logs, exitCode, id,
	)
	return err
}
