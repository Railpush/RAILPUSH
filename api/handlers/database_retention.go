package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/railpush/api/database"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

const (
	defaultDatabaseAutomatedBackupRetentionDays = 30
	defaultDatabaseManualBackupRetentionDays    = 365
	defaultDatabaseWALRetentionDays             = 7
)

type databaseRetention struct {
	AutomatedBackupDays int
	ManualBackupDays    int
	WALArchiveDays      int
}

func (h *DatabaseHandler) GetRetention(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	db, err := models.GetManagedDatabase(id)
	if err != nil || db == nil {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	ret, err := getDatabaseRetention(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load retention policy")
		return
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"database_id":           id,
		"automated_backups":     formatRetentionDays(ret.AutomatedBackupDays),
		"manual_backups":        formatRetentionDays(ret.ManualBackupDays),
		"wal_archives":          formatRetentionDays(ret.WALArchiveDays),
		"automated_backup_days": ret.AutomatedBackupDays,
		"manual_backup_days":    ret.ManualBackupDays,
		"wal_archive_days":      ret.WALArchiveDays,
		"enforced_cleanup":      []string{"automated_backups", "manual_backups"},
		"pending_enforcement":   []string{"wal_archives"},
	})
}

func (h *DatabaseHandler) UpdateRetention(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	db, err := models.GetManagedDatabase(id)
	if err != nil || db == nil {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	current, err := getDatabaseRetention(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load retention policy")
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&payload); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	automatedDays := current.AutomatedBackupDays
	manualDays := current.ManualBackupDays
	walDays := current.WALArchiveDays
	updated := false

	if v, ok, err := parseRetentionDaysField(payload, "automated_backups", 1, 3650); err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	} else if ok {
		automatedDays = v
		updated = true
	}
	if v, ok, err := parseRetentionDaysField(payload, "manual_backups", 1, 3650); err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	} else if ok {
		manualDays = v
		updated = true
	}
	if v, ok, err := parseRetentionDaysField(payload, "wal_archives", 1, 3650); err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	} else if ok {
		walDays = v
		updated = true
	}

	if !updated {
		utils.RespondError(w, http.StatusBadRequest, "at least one retention field must be provided")
		return
	}

	if _, err := database.DB.Exec(
		`UPDATE managed_databases
		    SET backup_retention_automated_days=$1,
		        backup_retention_manual_days=$2,
		        wal_retention_days=$3
		  WHERE id=$4`,
		automatedDays, manualDays, walDays, id,
	); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update retention policy")
		return
	}

	services.Audit(db.WorkspaceID, userID, "database.retention_updated", "database", id, map[string]interface{}{
		"automated_backup_days": automatedDays,
		"manual_backup_days":    manualDays,
		"wal_archive_days":      walDays,
	})

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":                "updated",
		"database_id":           id,
		"automated_backups":     formatRetentionDays(automatedDays),
		"manual_backups":        formatRetentionDays(manualDays),
		"wal_archives":          formatRetentionDays(walDays),
		"automated_backup_days": automatedDays,
		"manual_backup_days":    manualDays,
		"wal_archive_days":      walDays,
	})
}

func getDatabaseRetention(databaseID string) (databaseRetention, error) {
	ret := databaseRetention{}
	err := database.DB.QueryRow(
		`SELECT COALESCE(backup_retention_automated_days, $2),
		        COALESCE(backup_retention_manual_days, $3),
		        COALESCE(wal_retention_days, $4)
		   FROM managed_databases
		  WHERE id=$1`,
		databaseID,
		defaultDatabaseAutomatedBackupRetentionDays,
		defaultDatabaseManualBackupRetentionDays,
		defaultDatabaseWALRetentionDays,
	).Scan(&ret.AutomatedBackupDays, &ret.ManualBackupDays, &ret.WALArchiveDays)
	if err != nil {
		return databaseRetention{}, err
	}
	return ret, nil
}
