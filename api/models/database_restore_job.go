package models

import (
	"database/sql"
	"strings"
	"time"

	"github.com/railpush/api/database"
)

type DatabaseRestoreJob struct {
	ID                    string     `json:"id"`
	SourceDatabaseID      string     `json:"source_database_id"`
	TargetDatabaseID      *string    `json:"target_database_id,omitempty"`
	WorkspaceID           string     `json:"workspace_id"`
	BackupID              *string    `json:"backup_id,omitempty"`
	TargetTime            time.Time  `json:"target_time"`
	EffectiveRestorePoint *time.Time `json:"effective_restore_point,omitempty"`
	RestoreTo             string     `json:"restore_to"`
	Status                string     `json:"status"`
	Error                 string     `json:"error,omitempty"`
	RequestedBy           *string    `json:"requested_by,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	StartedAt             *time.Time `json:"started_at,omitempty"`
	FinishedAt            *time.Time `json:"finished_at,omitempty"`
}

func dbRestoreStringPtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	cp := v
	return &cp
}

func CreateDatabaseRestoreJob(job *DatabaseRestoreJob) error {
	if job == nil {
		return sql.ErrNoRows
	}
	err := database.DB.QueryRow(
		`INSERT INTO database_restore_jobs (
			source_database_id,
			target_database_id,
			workspace_id,
			backup_id,
			target_time,
			effective_restore_point,
			restore_to,
			status,
			error,
			requested_by
		) VALUES (
			$1,
			NULLIF($2,'')::uuid,
			$3,
			NULLIF($4,'')::uuid,
			$5,
			$6,
			$7,
			$8,
			$9,
			NULLIF($10,'')::uuid
		)
		RETURNING id, created_at`,
		strings.TrimSpace(job.SourceDatabaseID),
		derefOrEmpty(job.TargetDatabaseID),
		strings.TrimSpace(job.WorkspaceID),
		derefOrEmpty(job.BackupID),
		job.TargetTime,
		job.EffectiveRestorePoint,
		strings.TrimSpace(job.RestoreTo),
		strings.TrimSpace(job.Status),
		strings.TrimSpace(job.Error),
		derefOrEmpty(job.RequestedBy),
	).Scan(&job.ID, &job.CreatedAt)
	return err
}

func MarkDatabaseRestoreJobRunning(id string, targetDatabaseID *string, effectiveRestorePoint *time.Time) error {
	_, err := database.DB.Exec(
		`UPDATE database_restore_jobs
		    SET status='running',
		        target_database_id=NULLIF($2,'')::uuid,
		        effective_restore_point=COALESCE($3, effective_restore_point),
		        error='',
		        started_at=NOW(),
		        finished_at=NULL
		  WHERE id=$1`,
		strings.TrimSpace(id),
		derefOrEmpty(targetDatabaseID),
		effectiveRestorePoint,
	)
	return err
}

func MarkDatabaseRestoreJobCompleted(id string) error {
	_, err := database.DB.Exec(
		`UPDATE database_restore_jobs
		    SET status='completed',
		        error='',
		        finished_at=NOW()
		  WHERE id=$1`,
		strings.TrimSpace(id),
	)
	return err
}

func MarkDatabaseRestoreJobFailed(id string, failure string) error {
	_, err := database.DB.Exec(
		`UPDATE database_restore_jobs
		    SET status='failed',
		        error=$2,
		        finished_at=NOW()
		  WHERE id=$1`,
		strings.TrimSpace(id),
		strings.TrimSpace(failure),
	)
	return err
}

func ListDatabaseRestoreJobs(databaseID string, limit int) ([]DatabaseRestoreJob, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	rows, err := database.DB.Query(
		`SELECT
			id,
			COALESCE(source_database_id::text, ''),
			COALESCE(target_database_id::text, ''),
			COALESCE(workspace_id::text, ''),
			COALESCE(backup_id::text, ''),
			target_time,
			effective_restore_point,
			COALESCE(restore_to, ''),
			COALESCE(status, ''),
			COALESCE(error, ''),
			COALESCE(requested_by::text, ''),
			created_at,
			started_at,
			finished_at
		 FROM database_restore_jobs
		 WHERE source_database_id=$1 OR target_database_id=$1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		strings.TrimSpace(databaseID),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]DatabaseRestoreJob, 0)
	for rows.Next() {
		var job DatabaseRestoreJob
		var targetID, backupID, requestedBy string
		var effectiveRestorePoint sql.NullTime
		var startedAt sql.NullTime
		var finishedAt sql.NullTime

		if err := rows.Scan(
			&job.ID,
			&job.SourceDatabaseID,
			&targetID,
			&job.WorkspaceID,
			&backupID,
			&job.TargetTime,
			&effectiveRestorePoint,
			&job.RestoreTo,
			&job.Status,
			&job.Error,
			&requestedBy,
			&job.CreatedAt,
			&startedAt,
			&finishedAt,
		); err != nil {
			return nil, err
		}

		job.TargetDatabaseID = dbRestoreStringPtr(targetID)
		job.BackupID = dbRestoreStringPtr(backupID)
		job.RequestedBy = dbRestoreStringPtr(requestedBy)
		if effectiveRestorePoint.Valid {
			t := effectiveRestorePoint.Time
			job.EffectiveRestorePoint = &t
		}
		if startedAt.Valid {
			t := startedAt.Time
			job.StartedAt = &t
		}
		if finishedAt.Valid {
			t := finishedAt.Time
			job.FinishedAt = &t
		}

		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []DatabaseRestoreJob{}
	}
	return out, nil
}

func GetLatestDatabaseCloneJob(targetDatabaseID string) (*DatabaseRestoreJob, error) {
	targetDatabaseID = strings.TrimSpace(targetDatabaseID)
	if targetDatabaseID == "" {
		return nil, nil
	}

	job := &DatabaseRestoreJob{}
	var targetID, backupID, requestedBy string
	var effectiveRestorePoint sql.NullTime
	var startedAt sql.NullTime
	var finishedAt sql.NullTime

	err := database.DB.QueryRow(
		`SELECT
			id,
			COALESCE(source_database_id::text, ''),
			COALESCE(target_database_id::text, ''),
			COALESCE(workspace_id::text, ''),
			COALESCE(backup_id::text, ''),
			target_time,
			effective_restore_point,
			COALESCE(restore_to, ''),
			COALESCE(status, ''),
			COALESCE(error, ''),
			COALESCE(requested_by::text, ''),
			created_at,
			started_at,
			finished_at
		 FROM database_restore_jobs
		 WHERE target_database_id=$1
		   AND restore_to='new_database'
		 ORDER BY created_at DESC
		 LIMIT 1`,
		targetDatabaseID,
	).Scan(
		&job.ID,
		&job.SourceDatabaseID,
		&targetID,
		&job.WorkspaceID,
		&backupID,
		&job.TargetTime,
		&effectiveRestorePoint,
		&job.RestoreTo,
		&job.Status,
		&job.Error,
		&requestedBy,
		&job.CreatedAt,
		&startedAt,
		&finishedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	job.TargetDatabaseID = dbRestoreStringPtr(targetID)
	job.BackupID = dbRestoreStringPtr(backupID)
	job.RequestedBy = dbRestoreStringPtr(requestedBy)
	if effectiveRestorePoint.Valid {
		t := effectiveRestorePoint.Time
		job.EffectiveRestorePoint = &t
	}
	if startedAt.Valid {
		t := startedAt.Time
		job.StartedAt = &t
	}
	if finishedAt.Valid {
		t := finishedAt.Time
		job.FinishedAt = &t
	}

	return job, nil
}
