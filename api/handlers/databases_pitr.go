package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/database"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

const (
	databaseRestoreTargetNewDatabase = "new_database"
	databaseRestoreTargetInPlace     = "in_place"
)

type pointInTimeRestoreRequest struct {
	BackupID           string `json:"backup_id"`
	TargetTime         string `json:"target_time"`
	RestoreTo          string `json:"restore_to"`
	NewDatabaseName    string `json:"new_database_name"`
	ConfirmDestructive bool   `json:"confirm_destructive"`
}

type databaseBackupSnapshot struct {
	ID           string
	FilePath     string
	RestorePoint time.Time
	TriggerType  string
	SizeBytes    int64
}

func (req pointInTimeRestoreRequest) hasPointInTimeFields() bool {
	return strings.TrimSpace(req.TargetTime) != "" || strings.TrimSpace(req.RestoreTo) != "" || strings.TrimSpace(req.NewDatabaseName) != "" || req.ConfirmDestructive
}

func (req pointInTimeRestoreRequest) hasBackupRestoreFields() bool {
	return strings.TrimSpace(req.BackupID) != ""
}

func normalizeDatabaseRestoreTarget(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "new_database", "new", "clone":
		return databaseRestoreTargetNewDatabase, true
	case "in_place", "inplace", "in-place":
		return databaseRestoreTargetInPlace, true
	default:
		return "", false
	}
}

func defaultRestoredDatabaseName(sourceName string, targetTime time.Time) string {
	base := utils.ServiceDomainLabel(sourceName)
	if base == "" {
		base = "database"
	}
	suffix := "restored-" + targetTime.UTC().Format("20060102-150405")
	maxBaseLen := 63 - (len(suffix) + 1)
	if maxBaseLen < 1 {
		maxBaseLen = 1
	}
	if len(base) > maxBaseLen {
		base = strings.Trim(base[:maxBaseLen], "-")
		if base == "" {
			base = "database"
		}
	}
	return base + "-" + suffix
}

func managedDatabaseRuntimeName(databaseID string) string {
	id := strings.ToLower(strings.TrimSpace(databaseID))
	if len(id) >= 8 {
		id = id[:8]
	}
	id = strings.NewReplacer("_", "-", ".", "-", " ", "-").Replace(id)
	id = strings.Trim(id, "-")
	if id == "" {
		id = "unknown"
	}
	return "sr-db-" + id
}

func databaseKubernetesStatefulSetName(db *models.ManagedDatabase) string {
	if db == nil {
		return ""
	}
	if strings.HasPrefix(strings.TrimSpace(db.ContainerID), "k8s:") {
		name := strings.TrimSpace(strings.TrimPrefix(db.ContainerID, "k8s:"))
		if name != "" {
			return name
		}
	}
	return managedDatabaseRuntimeName(db.ID)
}

func (h *DatabaseHandler) ListRestoreJobs(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	db, err := models.GetManagedDatabase(id)
	if err != nil || db == nil {
		respondDatabaseNotFound(w, id)
		return
	}
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed <= 0 {
			utils.RespondError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}

	jobs, err := models.ListDatabaseRestoreJobs(db.ID, limit)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list restore jobs")
		return
	}
	if jobs == nil {
		jobs = []models.DatabaseRestoreJob{}
	}
	utils.RespondJSON(w, http.StatusOK, jobs)
}

func computeDatabaseRecoveryWindow(backups []databaseBackupSnapshot, walRetentionDays int, now time.Time, databaseStatus string) (*time.Time, *time.Time) {
	if len(backups) == 0 {
		return nil, nil
	}
	now = now.UTC()
	status := strings.ToLower(strings.TrimSpace(databaseStatus))

	earliest := backups[0].RestorePoint.UTC()
	if walRetentionDays > 0 {
		horizon := now.Add(-(time.Duration(walRetentionDays) * 24 * time.Hour))
		if earliest.Before(horizon) {
			earliest = horizon
		}
	}

	latest := backups[len(backups)-1].RestorePoint.UTC()
	if status == "available" || status == "live" || status == "restoring" {
		latest = now
	}
	if latest.Before(earliest) {
		latest = earliest
	}

	earliestCopy := earliest
	latestCopy := latest
	return &earliestCopy, &latestCopy
}

func (h *DatabaseHandler) GetRecoveryWindow(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	db, err := models.GetManagedDatabase(id)
	if err != nil || db == nil {
		respondDatabaseNotFound(w, id)
		return
	}

	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	ret, err := getDatabaseRetention(db.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load retention policy")
		return
	}

	backups, err := listCompletedDatabaseBackups(db.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to inspect backups")
		return
	}

	earliest, latest := computeDatabaseRecoveryWindow(backups, ret.WALArchiveDays, time.Now().UTC(), db.Status)
	var latestBaseBackupAt interface{}
	if len(backups) > 0 {
		latestBaseBackupAt = backups[len(backups)-1].RestorePoint.UTC()
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"database_id":               db.ID,
		"earliest_recovery_point":   earliest,
		"latest_recovery_point":     latest,
		"retention_days":            ret.WALArchiveDays,
		"base_backup_count":         len(backups),
		"latest_base_backup_at":     latestBaseBackupAt,
		"restore_precision":         "backup_snapshot",
		"wal_archiving":             map[string]interface{}{"enabled": true, "retention_days": ret.WALArchiveDays, "enforcement": "best_effort"},
		"supports_in_place_restore": true,
		"supports_restore_to_new":   true,
	})
}

func listCompletedDatabaseBackups(databaseID string) ([]databaseBackupSnapshot, error) {
	rows, err := database.DB.Query(
		`SELECT
			id::text,
			COALESCE(file_path, ''),
			COALESCE(finished_at, started_at, NOW()),
			COALESCE(NULLIF(trigger_type, ''), 'manual'),
			COALESCE(size_bytes, 0)
		 FROM backups
		 WHERE resource_type='database'
		   AND resource_id=$1
		   AND status='completed'
		   AND COALESCE(file_path, '') <> ''
		 ORDER BY COALESCE(finished_at, started_at, NOW()) ASC`,
		strings.TrimSpace(databaseID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]databaseBackupSnapshot, 0)
	for rows.Next() {
		var b databaseBackupSnapshot
		if err := rows.Scan(&b.ID, &b.FilePath, &b.RestorePoint, &b.TriggerType, &b.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func latestBackupAtOrBefore(databaseID string, targetTime time.Time) (*databaseBackupSnapshot, error) {
	var b databaseBackupSnapshot
	err := database.DB.QueryRow(
		`SELECT
			id::text,
			COALESCE(file_path, ''),
			COALESCE(finished_at, started_at, NOW()),
			COALESCE(NULLIF(trigger_type, ''), 'manual'),
			COALESCE(size_bytes, 0)
		 FROM backups
		 WHERE resource_type='database'
		   AND resource_id=$1
		   AND status='completed'
		   AND COALESCE(file_path, '') <> ''
		   AND COALESCE(finished_at, started_at, NOW()) <= $2
		 ORDER BY COALESCE(finished_at, started_at, NOW()) DESC
		 LIMIT 1`,
		strings.TrimSpace(databaseID),
		targetTime.UTC(),
	).Scan(&b.ID, &b.FilePath, &b.RestorePoint, &b.TriggerType, &b.SizeBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func backupByIDForDatabase(databaseID string, backupID string) (*databaseBackupSnapshot, error) {
	var b databaseBackupSnapshot
	err := database.DB.QueryRow(
		`SELECT
			id::text,
			COALESCE(file_path, ''),
			COALESCE(finished_at, started_at, NOW()),
			COALESCE(NULLIF(trigger_type, ''), 'manual'),
			COALESCE(size_bytes, 0)
		 FROM backups
		 WHERE id = $1
		   AND resource_type='database'
		   AND resource_id=$2
		   AND status='completed'
		 LIMIT 1`,
		strings.TrimSpace(backupID),
		strings.TrimSpace(databaseID),
	).Scan(&b.ID, &b.FilePath, &b.RestorePoint, &b.TriggerType, &b.SizeBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (h *DatabaseHandler) queueBackupRestore(w http.ResponseWriter, r *http.Request, source *models.ManagedDatabase, userID string, req pointInTimeRestoreRequest) {
	if source == nil {
		utils.RespondError(w, http.StatusBadRequest, "missing database")
		return
	}

	if strings.TrimSpace(req.TargetTime) != "" {
		utils.RespondError(w, http.StatusBadRequest, "target_time cannot be combined with backup_id")
		return
	}

	backupID := strings.TrimSpace(req.BackupID)
	if backupID == "" {
		utils.RespondError(w, http.StatusBadRequest, "backup_id is required")
		return
	}

	restoreTo, ok := normalizeDatabaseRestoreTarget(req.RestoreTo)
	if !ok {
		utils.RespondError(w, http.StatusBadRequest, "restore_to must be new_database or in_place")
		return
	}
	if restoreTo == databaseRestoreTargetInPlace && !req.ConfirmDestructive {
		utils.RespondError(w, http.StatusBadRequest, "in_place restore requires confirm_destructive=true")
		return
	}
	if restoreTo == databaseRestoreTargetInPlace && strings.EqualFold(strings.TrimSpace(source.Status), "soft_deleted") {
		utils.RespondError(w, http.StatusConflict, "in_place restore is unavailable for soft-deleted databases")
		return
	}

	backup, err := backupByIDForDatabase(source.ID, backupID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load backup")
		return
	}
	if backup == nil {
		utils.RespondError(w, http.StatusNotFound, "backup not found")
		return
	}
	if strings.TrimSpace(backup.FilePath) == "" {
		utils.RespondJSON(w, http.StatusConflict, map[string]interface{}{
			"error":     "selected backup file is not available",
			"backup_id": backup.ID,
		})
		return
	}
	if _, err := os.Stat(backup.FilePath); err != nil {
		utils.RespondJSON(w, http.StatusConflict, map[string]interface{}{
			"error":       "selected backup file is not available on disk",
			"backup_id":   backup.ID,
			"backup_path": backup.FilePath,
		})
		return
	}

	target := source
	targetPassword := ""
	if restoreTo == databaseRestoreTargetNewDatabase {
		newName := strings.TrimSpace(req.NewDatabaseName)
		if newName == "" {
			newName = defaultRestoredDatabaseName(source.Name, backup.RestorePoint)
		}
		restoredDB, restoredPassword, err := h.createRestoredDatabase(source, userID, newName)
		if err != nil {
			utils.RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		target = restoredDB
		targetPassword = restoredPassword
	}

	targetID := target.ID
	reqBy := strings.TrimSpace(userID)
	targetTime := backup.RestorePoint.UTC()
	effectivePoint := backup.RestorePoint.UTC()
	job := &models.DatabaseRestoreJob{
		SourceDatabaseID:      source.ID,
		TargetDatabaseID:      &targetID,
		WorkspaceID:           source.WorkspaceID,
		BackupID:              &backupID,
		TargetTime:            targetTime,
		EffectiveRestorePoint: &effectivePoint,
		RestoreTo:             restoreTo,
		Status:                "queued",
		RequestedBy:           &reqBy,
	}
	if err := models.CreateDatabaseRestoreJob(job); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create restore job")
		return
	}

	go h.executePointInTimeRestoreJob(job.ID, source.ID, target.ID, targetPassword, backup, restoreTo, userID)

	services.Audit(source.WorkspaceID, userID, "database.backup_restore.queued", "database", source.ID, map[string]interface{}{
		"restore_job_id":          job.ID,
		"restore_to":              restoreTo,
		"target_database_id":      target.ID,
		"target_time":             targetTime,
		"effective_restore_point": backup.RestorePoint.UTC(),
		"backup_id":               backup.ID,
	})

	response := map[string]interface{}{
		"id":                      job.ID,
		"status":                  "queued",
		"restore_mode":            "backup_id",
		"restore_to":              restoreTo,
		"source_database_id":      source.ID,
		"target_database_id":      target.ID,
		"target_time":             targetTime.Format(time.RFC3339),
		"effective_restore_point": backup.RestorePoint.UTC().Format(time.RFC3339),
		"backup_id":               backup.ID,
		"restore_precision":       "backup_snapshot",
	}
	if restoreTo == databaseRestoreTargetNewDatabase {
		response["target_database_name"] = target.Name
	}
	utils.RespondJSON(w, http.StatusAccepted, response)
}

func (h *DatabaseHandler) queuePointInTimeRestore(w http.ResponseWriter, r *http.Request, source *models.ManagedDatabase, userID string, req pointInTimeRestoreRequest) {
	if source == nil {
		utils.RespondError(w, http.StatusBadRequest, "missing database")
		return
	}
	if strings.EqualFold(strings.TrimSpace(source.Status), "soft_deleted") {
		utils.RespondError(w, http.StatusConflict, "point-in-time restore is unavailable for soft-deleted databases")
		return
	}

	targetTimeRaw := strings.TrimSpace(req.TargetTime)
	if targetTimeRaw == "" {
		utils.RespondError(w, http.StatusBadRequest, "target_time is required")
		return
	}
	targetTime, err := time.Parse(time.RFC3339, targetTimeRaw)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, "target_time must be RFC3339 (e.g. 2026-02-24T14:46:00Z)")
		return
	}
	now := time.Now().UTC()
	if targetTime.After(now.Add(1 * time.Minute)) {
		utils.RespondError(w, http.StatusBadRequest, "target_time cannot be in the future")
		return
	}

	restoreTo, ok := normalizeDatabaseRestoreTarget(req.RestoreTo)
	if !ok {
		utils.RespondError(w, http.StatusBadRequest, "restore_to must be new_database or in_place")
		return
	}
	if restoreTo == databaseRestoreTargetInPlace && !req.ConfirmDestructive {
		utils.RespondError(w, http.StatusBadRequest, "in_place restore requires confirm_destructive=true")
		return
	}

	backup, err := latestBackupAtOrBefore(source.ID, targetTime)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to evaluate backups")
		return
	}
	if backup == nil {
		utils.RespondJSON(w, http.StatusConflict, map[string]interface{}{
			"error":       "no backup exists at or before target_time",
			"target_time": targetTime.UTC().Format(time.RFC3339),
			"hint":        "trigger a backup first or choose a later target_time",
		})
		return
	}
	if _, err := os.Stat(backup.FilePath); err != nil {
		utils.RespondJSON(w, http.StatusConflict, map[string]interface{}{
			"error":       "selected backup file is not available on disk",
			"backup_id":   backup.ID,
			"backup_path": backup.FilePath,
		})
		return
	}

	target := source
	targetPassword := ""
	if restoreTo == databaseRestoreTargetNewDatabase {
		newName := strings.TrimSpace(req.NewDatabaseName)
		if newName == "" {
			newName = defaultRestoredDatabaseName(source.Name, targetTime)
		}
		restoredDB, restoredPassword, err := h.createRestoredDatabase(source, userID, newName)
		if err != nil {
			utils.RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		target = restoredDB
		targetPassword = restoredPassword
	}

	backupID := backup.ID
	targetID := target.ID
	reqBy := strings.TrimSpace(userID)
	effectivePoint := backup.RestorePoint.UTC()
	job := &models.DatabaseRestoreJob{
		SourceDatabaseID:      source.ID,
		TargetDatabaseID:      &targetID,
		WorkspaceID:           source.WorkspaceID,
		BackupID:              &backupID,
		TargetTime:            targetTime.UTC(),
		EffectiveRestorePoint: &effectivePoint,
		RestoreTo:             restoreTo,
		Status:                "queued",
		RequestedBy:           &reqBy,
	}
	if err := models.CreateDatabaseRestoreJob(job); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create restore job")
		return
	}

	go h.executePointInTimeRestoreJob(job.ID, source.ID, target.ID, targetPassword, backup, restoreTo, userID)

	services.Audit(source.WorkspaceID, userID, "database.point_in_time_restore.queued", "database", source.ID, map[string]interface{}{
		"restore_job_id":          job.ID,
		"restore_to":              restoreTo,
		"target_database_id":      target.ID,
		"target_time":             targetTime.UTC(),
		"effective_restore_point": backup.RestorePoint.UTC(),
		"backup_id":               backup.ID,
	})

	response := map[string]interface{}{
		"id":                      job.ID,
		"status":                  "queued",
		"restore_to":              restoreTo,
		"source_database_id":      source.ID,
		"target_database_id":      target.ID,
		"target_time":             targetTime.UTC().Format(time.RFC3339),
		"effective_restore_point": backup.RestorePoint.UTC().Format(time.RFC3339),
		"backup_id":               backup.ID,
		"restore_precision":       "backup_snapshot",
	}
	if restoreTo == databaseRestoreTargetNewDatabase {
		response["target_database_name"] = target.Name
	}
	utils.RespondJSON(w, http.StatusAccepted, response)
}

func (h *DatabaseHandler) createRestoredDatabase(source *models.ManagedDatabase, userID, requestedName string) (*models.ManagedDatabase, string, error) {
	if h == nil || h.Config == nil {
		return nil, "", fmt.Errorf("database handler is not initialized")
	}
	if source == nil {
		return nil, "", fmt.Errorf("source database is required")
	}
	name := strings.TrimSpace(requestedName)
	if name == "" {
		return nil, "", fmt.Errorf("new_database_name is required for restore_to=new_database")
	}

	if source.Plan == services.PlanFree {
		count, err := models.CountResourcesByWorkspaceAndPlan(source.WorkspaceID, "database", services.PlanFree)
		if err == nil && count >= 1 {
			return nil, "", fmt.Errorf("free tier limit reached: 1 free database per workspace")
		}
	}

	password, err := utils.GenerateRandomString(24)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate password")
	}
	encrypted, err := utils.Encrypt(password, h.Config.Crypto.EncryptionKey)
	if err != nil {
		return nil, "", fmt.Errorf("failed to encrypt password")
	}

	restored := &models.ManagedDatabase{
		WorkspaceID:       source.WorkspaceID,
		Name:              name,
		Plan:              source.Plan,
		PGVersion:         source.PGVersion,
		Host:              "localhost",
		Port:              5432,
		DBName:            name,
		Username:          name,
		EncryptedPassword: encrypted,
	}

	if err := models.CreateManagedDatabase(restored); err != nil {
		return nil, "", fmt.Errorf("failed to create restored database: %w", err)
	}

	if h.Config != nil && h.Config.Kubernetes.Enabled {
		internalHost := managedDatabaseRuntimeName(restored.ID)
		restored.Host = internalHost
		restored.Port = 5432
		_ = models.UpdateManagedDatabaseConnection(restored.ID, 5432, internalHost)
	}

	if restored.Plan != services.PlanFree && h.Stripe != nil && h.Stripe.Enabled() {
		user, err := models.GetUserByID(userID)
		if err != nil || user == nil {
			_ = models.DeleteManagedDatabase(restored.ID)
			return nil, "", fmt.Errorf("failed to get user for billing")
		}
		bc, err := h.Stripe.EnsureCustomer(userID, user.Email)
		if err != nil || bc == nil {
			_ = models.DeleteManagedDatabase(restored.ID)
			if err == nil {
				err = fmt.Errorf("billing customer not found")
			}
			return nil, "", fmt.Errorf("billing error: %w", err)
		}
		if err := h.Stripe.AddSubscriptionItem(bc, restored.WorkspaceID, "database", restored.ID, restored.Name, restored.Plan); err != nil {
			_ = models.DeleteManagedDatabase(restored.ID)
			if errors.Is(err, services.ErrNoDefaultPaymentMethod) {
				return nil, "", fmt.Errorf("payment method required. Please add a default payment method in billing settings")
			}
			return nil, "", fmt.Errorf("billing error: %w", err)
		}
	}

	if h.Worker == nil {
		_ = models.DeleteManagedDatabase(restored.ID)
		return nil, "", fmt.Errorf("database worker is unavailable")
	}

	h.Worker.ProvisionDatabase(restored, password)
	return restored, password, nil
}

func (h *DatabaseHandler) waitForDatabaseReady(databaseID string, timeout time.Duration) (*models.ManagedDatabase, error) {
	if timeout <= 0 {
		timeout = 8 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for {
		db, err := models.GetManagedDatabase(databaseID)
		if err != nil {
			return nil, err
		}
		if db == nil {
			return nil, fmt.Errorf("database not found")
		}
		status := strings.ToLower(strings.TrimSpace(db.Status))
		if status == "available" || status == "live" {
			if strings.TrimSpace(db.ContainerID) != "" {
				return db, nil
			}
		}
		if status == "failed" {
			return nil, fmt.Errorf("database provisioning failed")
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for database to become available")
		}
		time.Sleep(3 * time.Second)
	}
}

func truncateRestoreOutput(raw string, max int) string {
	raw = strings.TrimSpace(raw)
	if max <= 0 || len(raw) <= max {
		return raw
	}
	if max < 4 {
		return raw[:max]
	}
	return raw[:max-3] + "..."
}

func (h *DatabaseHandler) applyBackupToManagedDatabase(db *models.ManagedDatabase, password string, backupPath string) error {
	if db == nil {
		return fmt.Errorf("missing target database")
	}
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("database password is unavailable")
	}
	if strings.TrimSpace(backupPath) == "" {
		return fmt.Errorf("backup file path is missing")
	}

	file, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("open backup file: %w", err)
	}
	defer file.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	var cmd *exec.Cmd
	if h.Config != nil && h.Config.Kubernetes.Enabled && strings.HasPrefix(strings.TrimSpace(db.ContainerID), "k8s:") {
		namespace := strings.TrimSpace(h.Config.Kubernetes.Namespace)
		if namespace == "" {
			namespace = "railpush"
		}
		podName := databaseKubernetesStatefulSetName(db) + "-0"
		cmd = exec.CommandContext(ctx,
			"kubectl", "exec", "-i", podName, "-n", namespace, "--",
			"env", "PGPASSWORD="+password,
			"psql", "-v", "ON_ERROR_STOP=1", "--no-psqlrc", "-h", "127.0.0.1", "-p", "5432", "-U", db.Username, "-d", db.DBName,
		)
	} else {
		containerName := managedDatabaseRuntimeName(db.ID)
		cmd = exec.CommandContext(ctx,
			"docker", "exec", "-i", "-e", "PGPASSWORD="+password,
			containerName,
			"psql", "-v", "ON_ERROR_STOP=1", "--no-psqlrc", "-U", db.Username, "-d", db.DBName,
		)
	}

	cmd.Stdin = file
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("restore timed out after 20m")
		}
		return fmt.Errorf("restore command failed: %s", truncateRestoreOutput(string(out), 1200))
	}

	return nil
}

func (h *DatabaseHandler) executePointInTimeRestoreJob(jobID, sourceDatabaseID, targetDatabaseID, targetPassword string, backup *databaseBackupSnapshot, restoreTo, requestedBy string) {
	if h == nil || h.Config == nil {
		_ = models.MarkDatabaseRestoreJobFailed(jobID, "database restore handler is unavailable")
		return
	}
	if strings.TrimSpace(jobID) == "" || backup == nil {
		return
	}

	targetID := strings.TrimSpace(targetDatabaseID)
	restorePoint := backup.RestorePoint.UTC()
	_ = models.MarkDatabaseRestoreJobRunning(jobID, &targetID, &restorePoint)

	targetDB, err := models.GetManagedDatabase(targetDatabaseID)
	if err != nil || targetDB == nil {
		msg := "failed to load target database"
		if err != nil {
			msg = msg + ": " + err.Error()
		}
		_ = models.MarkDatabaseRestoreJobFailed(jobID, truncateRestoreOutput(msg, 1024))
		return
	}

	if restoreTo == databaseRestoreTargetNewDatabase {
		targetDB, err = h.waitForDatabaseReady(targetDatabaseID, 10*time.Minute)
		if err != nil {
			_ = models.MarkDatabaseRestoreJobFailed(jobID, truncateRestoreOutput(err.Error(), 1024))
			_ = models.UpdateManagedDatabaseStatus(targetDatabaseID, "restore_failed", targetDB.ContainerID)
			return
		}
	}

	if strings.TrimSpace(targetPassword) == "" {
		if strings.TrimSpace(targetDB.EncryptedPassword) == "" {
			_ = models.MarkDatabaseRestoreJobFailed(jobID, "target database credentials are unavailable")
			return
		}
		decrypted, decErr := utils.Decrypt(targetDB.EncryptedPassword, h.Config.Crypto.EncryptionKey)
		if decErr != nil || strings.TrimSpace(decrypted) == "" {
			_ = models.MarkDatabaseRestoreJobFailed(jobID, "failed to decrypt target database credentials")
			return
		}
		targetPassword = decrypted
	}

	_ = models.UpdateManagedDatabaseStatus(targetDB.ID, "restoring", targetDB.ContainerID)
	if err := h.applyBackupToManagedDatabase(targetDB, targetPassword, backup.FilePath); err != nil {
		_ = models.MarkDatabaseRestoreJobFailed(jobID, truncateRestoreOutput(err.Error(), 1024))
		_ = models.UpdateManagedDatabaseStatus(targetDB.ID, "restore_failed", targetDB.ContainerID)
		services.Audit(targetDB.WorkspaceID, requestedBy, "database.point_in_time_restore.failed", "database", sourceDatabaseID, map[string]interface{}{
			"restore_job_id":     jobID,
			"target_database_id": targetDB.ID,
			"restore_to":         restoreTo,
			"error":              truncateRestoreOutput(err.Error(), 1024),
		})
		return
	}

	_ = models.UpdateManagedDatabaseStatus(targetDB.ID, "available", targetDB.ContainerID)
	_ = models.MarkDatabaseRestoreJobCompleted(jobID)
	h.syncDatabaseLinks(targetDB.ID)

	services.Audit(targetDB.WorkspaceID, requestedBy, "database.point_in_time_restore.completed", "database", sourceDatabaseID, map[string]interface{}{
		"restore_job_id":          jobID,
		"target_database_id":      targetDB.ID,
		"restore_to":              restoreTo,
		"effective_restore_point": backup.RestorePoint.UTC(),
	})

	log.Printf("database restore job completed source=%s target=%s job=%s", sourceDatabaseID, targetDatabaseID, jobID)
}
