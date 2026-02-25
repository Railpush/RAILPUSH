package models

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/railpush/api/database"
)

const (
	defaultDeployQueueEstimateSeconds = 180
	minDeployQueueEstimateSeconds     = 30
	maxDeployQueueEstimateSeconds     = 3600
	deployQueueEstimateSampleSize     = 80
)

// ClaimDeployLease attempts to claim a deploy for processing by setting a lease on the row.
// It returns true if the lease was acquired by this owner.
func ClaimDeployLease(deployID string, owner string, leaseSeconds int, maxAttempts int) (bool, error) {
	if leaseSeconds <= 0 {
		leaseSeconds = 600
	}
	res, err := database.DB.Exec(
		`UPDATE deploys
		 SET lease_owner=$1,
		     lease_acquired_at=NOW(),
		     lease_expires_at=NOW() + ($2 * INTERVAL '1 second'),
		     attempts=COALESCE(attempts,0)+1,
		     last_error=NULL
		 WHERE id=$3
		   AND status IN ('pending','building','deploying')
		   AND (lease_expires_at IS NULL OR lease_expires_at < NOW() OR lease_owner=$1)
		   AND ($4 <= 0 OR COALESCE(attempts,0) < $4)`,
		owner, leaseSeconds, deployID, maxAttempts,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// ExtendDeployLease extends the lease for an in-progress deploy owned by owner.
func ExtendDeployLease(deployID string, owner string, leaseSeconds int) error {
	if leaseSeconds <= 0 {
		leaseSeconds = 600
	}
	_, err := database.DB.Exec(
		`UPDATE deploys
		 SET lease_expires_at=NOW() + ($3 * INTERVAL '1 second')
		 WHERE id=$1 AND lease_owner=$2`,
		deployID, owner, leaseSeconds,
	)
	return err
}

// ReleaseDeployLease clears the lease on a deploy row.
func ReleaseDeployLease(deployID string, owner string) error {
	_, err := database.DB.Exec(
		`UPDATE deploys
		 SET lease_owner=NULL, lease_acquired_at=NULL, lease_expires_at=NULL
		 WHERE id=$1 AND lease_owner=$2`,
		deployID, owner,
	)
	return err
}

// ClaimExpiredDeploys leases a batch of deploys that are pending (or stuck building/deploying)
// and currently have no active lease. This supports crash recovery and horizontal scaling.
func ClaimExpiredDeploys(owner string, limit int, leaseSeconds int, maxAttempts int) ([]Deploy, error) {
	if limit <= 0 {
		limit = 1
	}
	if leaseSeconds <= 0 {
		leaseSeconds = 600
	}

	tx, err := database.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(
		`SELECT id, service_id, COALESCE(trigger,''), COALESCE(status,'pending'),
		        COALESCE(commit_sha,''), COALESCE(commit_message,''), COALESCE(branch,''),
		        COALESCE(image_tag,''), COALESCE(build_log,''), COALESCE(dockerfile_override,''), started_at, finished_at, created_by
		   FROM deploys
		  WHERE status IN ('pending','building','deploying')
		    AND (lease_expires_at IS NULL OR lease_expires_at < NOW())
		    AND ($1 <= 0 OR COALESCE(attempts,0) < $1)
		  ORDER BY created_at ASC
		  FOR UPDATE SKIP LOCKED
		  LIMIT $2`,
		maxAttempts, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	deploys := []Deploy{}
	ids := []string{}
	for rows.Next() {
		var d Deploy
		if err := rows.Scan(&d.ID, &d.ServiceID, &d.Trigger, &d.Status, &d.CommitSHA, &d.CommitMessage, &d.Branch, &d.ImageTag, &d.BuildLog, &d.DockerfileOverride, &d.StartedAt, &d.FinishedAt, &d.CreatedBy); err != nil {
			return nil, err
		}
		deploys = append(deploys, d)
		ids = append(ids, d.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}

	if _, err := tx.Exec(
		`UPDATE deploys
		 SET lease_owner=$1,
		     lease_acquired_at=NOW(),
		     lease_expires_at=NOW() + ($2 * INTERVAL '1 second'),
		     attempts=COALESCE(attempts,0)+1,
		     last_error=NULL
		 WHERE id = ANY($3::uuid[])`,
		owner, leaseSeconds, pq.Array(ids),
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return deploys, nil
}

// DeployQueueInfo contains queue position and stats for a pending deploy.
type DeployQueueInfo struct {
	Position             int    `json:"position"`
	TotalQueue           int    `json:"total_queued"`
	EstimatedWaitSeconds int    `json:"estimated_wait_seconds"`
	EstimatedWaitHuman   string `json:"estimated_wait_human,omitempty"`
	AverageDeploySeconds int    `json:"average_deploy_seconds,omitempty"`
	Concurrency          int    `json:"concurrency,omitempty"`
	Status               string `json:"status,omitempty"`
}

// GetDeployQueuePosition returns the queue position of a deploy (1-based) and total queue size.
// Returns position 0 if the deploy is not in the queue.
func GetDeployQueuePosition(deployID string, workerConcurrency int) (*DeployQueueInfo, error) {
	workerConcurrency = normalizeDeployWorkerConcurrency(workerConcurrency)
	info := &DeployQueueInfo{Concurrency: workerConcurrency}

	var status string
	var createdAt sql.NullTime
	err := database.DB.QueryRow(
		`SELECT COALESCE(status, ''), created_at
		   FROM deploys
		  WHERE id = $1`,
		deployID,
	).Scan(&status, &createdAt)
	if err == sql.ErrNoRows {
		return info, nil
	}
	if err != nil {
		return nil, err
	}

	status = strings.ToLower(strings.TrimSpace(status))
	info.Status = status

	err = database.DB.QueryRow(
		`SELECT COUNT(*) FROM deploys WHERE status IN ('pending','building','deploying')`,
	).Scan(&info.TotalQueue)
	if err != nil {
		return nil, err
	}

	if !isQueuedDeployStatus(status) || !createdAt.Valid || info.TotalQueue <= 0 {
		return info, nil
	}

	err = database.DB.QueryRow(
		`SELECT COUNT(*)
		   FROM deploys
		  WHERE status IN ('pending','building','deploying')
		    AND (created_at, id) <= ($1, $2::uuid)`,
		createdAt.Time.UTC(),
		deployID,
	).Scan(&info.Position)
	if err != nil {
		return nil, err
	}

	if info.Position < 0 {
		info.Position = 0
	}
	if info.Position > info.TotalQueue {
		info.Position = info.TotalQueue
	}

	avgDeploySeconds, err := recentAverageDeployDurationSeconds(deployQueueEstimateSampleSize)
	if err != nil {
		return nil, err
	}
	info.AverageDeploySeconds = avgDeploySeconds

	if status == "pending" && info.Position > 1 {
		ahead := info.Position - 1
		batches := int(math.Ceil(float64(ahead) / float64(workerConcurrency)))
		estimated := batches * avgDeploySeconds
		if estimated < 0 {
			estimated = 0
		}
		info.EstimatedWaitSeconds = estimated
		info.EstimatedWaitHuman = humanizeQueueWait(estimated)
	}

	return info, nil
}

func isQueuedDeployStatus(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return status == "pending" || status == "building" || status == "deploying"
}

func normalizeDeployWorkerConcurrency(workerConcurrency int) int {
	if workerConcurrency <= 0 {
		return 1
	}
	return workerConcurrency
}

func recentAverageDeployDurationSeconds(limit int) (int, error) {
	if limit <= 0 {
		limit = deployQueueEstimateSampleSize
	}

	var avg sql.NullFloat64
	err := database.DB.QueryRow(
		`SELECT AVG(duration_seconds)
		   FROM (
			 SELECT EXTRACT(EPOCH FROM (finished_at - started_at)) AS duration_seconds
			   FROM deploys
			  WHERE status IN ('live','failed','cancelled','canceled')
			    AND started_at IS NOT NULL
			    AND finished_at IS NOT NULL
			    AND finished_at >= started_at
			  ORDER BY finished_at DESC
			  LIMIT $1
		   ) recent`,
		limit,
	).Scan(&avg)
	if err != nil {
		return 0, err
	}

	if !avg.Valid || avg.Float64 <= 0 {
		return defaultDeployQueueEstimateSeconds, nil
	}

	seconds := int(math.Round(avg.Float64))
	if seconds < minDeployQueueEstimateSeconds {
		seconds = minDeployQueueEstimateSeconds
	}
	if seconds > maxDeployQueueEstimateSeconds {
		seconds = maxDeployQueueEstimateSeconds
	}
	return seconds, nil
}

func humanizeQueueWait(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	d := time.Duration(seconds) * time.Second
	if d < time.Minute {
		return "under 1m"
	}
	if d < time.Hour {
		minutes := int(math.Ceil(d.Minutes()))
		return fmt.Sprintf("~%dm", minutes)
	}
	hours := int(d.Hours())
	minutes := int(math.Ceil(d.Minutes())) % 60
	if minutes == 0 {
		return fmt.Sprintf("~%dh", hours)
	}
	return fmt.Sprintf("~%dh %dm", hours, minutes)
}

// GetQueueSummary returns the number of deploys currently in the queue.
func GetQueueSummary() (int, error) {
	var count int
	err := database.DB.QueryRow(
		`SELECT COUNT(*) FROM deploys WHERE status IN ('pending','building','deploying')`,
	).Scan(&count)
	return count, err
}

// MarkStaleDeploysFailed marks deploys as failed when they have exceeded maxAttempts and are not actively leased.
func MarkStaleDeploysFailed(maxAttempts int) (int64, error) {
	if maxAttempts <= 0 {
		return 0, nil
	}
	res, err := database.DB.Exec(
		`UPDATE deploys
		 SET status='failed', finished_at=NOW(), last_error='max attempts exceeded'
		 WHERE status IN ('pending','building','deploying')
		   AND COALESCE(attempts,0) >= $1
		   AND (lease_expires_at IS NULL OR lease_expires_at < NOW())`,
		maxAttempts,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}
