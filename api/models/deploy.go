package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type Deploy struct {
	ID            string     `json:"id"`
	ServiceID     string     `json:"service_id"`
	Trigger       string     `json:"trigger"`
	Status        string     `json:"status"`
	CommitSHA     string     `json:"commit_sha"`
	CommitMessage string     `json:"commit_message"`
	Branch        string     `json:"branch"`
	ImageTag      string     `json:"image_tag"`
	BuildLog      string     `json:"build_log"`
	StartedAt     *time.Time `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	CreatedBy     *string    `json:"created_by"`
}

func CreateDeploy(d *Deploy) error {
	return database.DB.QueryRow("INSERT INTO deploys (service_id, trigger, commit_sha, commit_message, branch, image_tag, created_by) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, status",
		d.ServiceID, d.Trigger, d.CommitSHA, d.CommitMessage, d.Branch, d.ImageTag, d.CreatedBy).Scan(&d.ID, &d.Status)
}

func GetDeploy(id string) (*Deploy, error) {
	d := &Deploy{}
	err := database.DB.QueryRow("SELECT id, service_id, COALESCE(trigger,''), COALESCE(status,'pending'), COALESCE(commit_sha,''), COALESCE(commit_message,''), COALESCE(branch,''), COALESCE(image_tag,''), COALESCE(build_log,''), started_at, finished_at, created_by FROM deploys WHERE id=$1", id).Scan(
		&d.ID, &d.ServiceID, &d.Trigger, &d.Status, &d.CommitSHA, &d.CommitMessage, &d.Branch, &d.ImageTag, &d.BuildLog, &d.StartedAt, &d.FinishedAt, &d.CreatedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func ListDeploys(serviceID string) ([]Deploy, error) {
	rows, err := database.DB.Query("SELECT id, service_id, COALESCE(trigger,''), COALESCE(status,'pending'), COALESCE(commit_sha,''), COALESCE(commit_message,''), COALESCE(branch,''), COALESCE(image_tag,''), started_at, finished_at FROM deploys WHERE service_id=$1 ORDER BY started_at DESC NULLS LAST", serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deploys []Deploy
	for rows.Next() {
		var d Deploy
		if err := rows.Scan(&d.ID, &d.ServiceID, &d.Trigger, &d.Status, &d.CommitSHA, &d.CommitMessage, &d.Branch, &d.ImageTag, &d.StartedAt, &d.FinishedAt); err != nil {
			return nil, err
		}
		deploys = append(deploys, d)
	}
	return deploys, nil
}

func UpdateDeployStatus(id, status string) error {
	_, err := database.DB.Exec("UPDATE deploys SET status=$1, finished_at=NOW() WHERE id=$2", status, id)
	return err
}

func UpdateDeployBuildLog(id, logLine string) error {
	_, err := database.DB.Exec("UPDATE deploys SET build_log = COALESCE(build_log,'') || $1 || E'\\n' WHERE id=$2", logLine, id)
	return err
}

func UpdateDeployStarted(id, imageTag string) error {
	_, err := database.DB.Exec("UPDATE deploys SET status=$1, image_tag=$2, started_at=NOW() WHERE id=$3", "building", imageTag, id)
	return err
}
