package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/database"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type DatabaseHandler struct {
	Config *config.Config
	Worker *services.Worker
	Stripe *services.StripeService
}

func NewDatabaseHandler(cfg *config.Config, worker *services.Worker, stripe *services.StripeService) *DatabaseHandler {
	return &DatabaseHandler{Config: cfg, Worker: worker, Stripe: stripe}
}

func (h *DatabaseHandler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	workspaceID, err := resolveWorkspaceID(r, r.URL.Query().Get("workspace_id"))
	if err != nil || workspaceID == "" {
		utils.RespondError(w, http.StatusBadRequest, "workspace not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	dbs, err := models.ListManagedDatabasesByWorkspace(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list databases")
		return
	}
	if dbs == nil {
		dbs = []models.ManagedDatabase{}
	}
	utils.RespondJSON(w, http.StatusOK, dbs)
}

func (h *DatabaseHandler) CreateDatabase(w http.ResponseWriter, r *http.Request) {
	var db models.ManagedDatabase
	if err := json.NewDecoder(r.Body).Decode(&db); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if db.Name == "" {
		utils.RespondError(w, http.StatusBadRequest, "name is required")
		return
	}
	if db.PGVersion == 0 {
		db.PGVersion = 16
	}
	if db.Plan == "" {
		db.Plan = services.PlanStarter
	}
	if p, ok := services.NormalizePlan(db.Plan); ok {
		db.Plan = p
	} else {
		utils.RespondError(w, http.StatusBadRequest, "invalid plan")
		return
	}
	if db.Port == 0 {
		db.Port = 5432
	}
	db.Host = "localhost"
	db.DBName = db.Name
	db.Username = db.Name

	userID := middleware.GetUserID(r)
	if db.WorkspaceID == "" {
		ws, err := models.GetWorkspaceByOwner(userID)
		if err != nil || ws == nil {
			utils.RespondError(w, http.StatusBadRequest, "no workspace found for user")
			return
		}
		db.WorkspaceID = ws.ID
	}
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Free tier: limit 1 free database per workspace
	if db.Plan == "free" {
		count, err := models.CountResourcesByWorkspaceAndPlan(db.WorkspaceID, "database", "free")
		if err == nil && count >= 1 {
			utils.RespondError(w, http.StatusBadRequest, "free tier limit reached: 1 free database per workspace")
			return
		}
	}

	// Paid plan: ensure Stripe customer exists and has payment method
	if db.Plan != "free" && h.Stripe.Enabled() {
		user, err := models.GetUserByID(userID)
		if err != nil || user == nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to get user")
			return
		}
		bc, err := h.Stripe.EnsureCustomer(userID, user.Email)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "billing error: "+err.Error())
			return
		}
		if bc.PaymentMethodLast4 == "" {
			utils.RespondError(w, http.StatusPaymentRequired, "payment method required. Please add a payment method in billing settings.")
			return
		}
	}

	// Generate and encrypt the password
	pw, _ := utils.GenerateRandomString(16)
	encrypted, _ := utils.Encrypt(pw, h.Config.Crypto.EncryptionKey)
	db.EncryptedPassword = encrypted

	if err := models.CreateManagedDatabase(&db); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create database: "+err.Error())
		return
	}
	// In Kubernetes mode, the stable in-cluster endpoint is `sr-db-<idPrefix>:5432`.
	if h.Config != nil && h.Config.Kubernetes.Enabled {
		internalHost := fmt.Sprintf("sr-db-%s", db.ID[:8])
		db.Host = internalHost
		db.Port = 5432
		_ = models.UpdateManagedDatabaseConnection(db.ID, 5432, internalHost)
	}

	// Add to Stripe subscription for paid plans
	if db.Plan != "free" && h.Stripe.Enabled() {
		bc, _ := models.GetBillingCustomerByUserID(userID)
		if bc != nil {
			if err := h.Stripe.AddSubscriptionItem(bc, "database", db.ID, db.Name, db.Plan); err != nil {
				log.Printf("Warning: failed to add billing for database %s: %v", db.ID, err)
				models.DeleteManagedDatabase(db.ID)
				if errors.Is(err, services.ErrNoDefaultPaymentMethod) {
					utils.RespondError(w, http.StatusPaymentRequired, "payment method required. Please add a default payment method in billing settings.")
					return
				}
				utils.RespondError(w, http.StatusInternalServerError, "billing error: "+err.Error())
				return
			}
		}
	}

	// Spin up real PostgreSQL container in background
	h.Worker.ProvisionDatabase(&db, pw)
	services.Audit(db.WorkspaceID, userID, "database.created", "database", db.ID, map[string]interface{}{
		"name": db.Name,
		"plan": db.Plan,
	})

	utils.RespondJSON(w, http.StatusCreated, db)
}

func (h *DatabaseHandler) GetDatabase(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	db, err := models.GetManagedDatabase(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if db == nil {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Decrypt password for response
	if db.EncryptedPassword != "" {
		pw, err := utils.Decrypt(db.EncryptedPassword, h.Config.Crypto.EncryptionKey)
		if err == nil {
			externalHost := db.Host
			if strings.TrimSpace(h.Config.ControlPlane.Domain) != "" {
				externalHost = h.Config.ControlPlane.Domain
			}
			type DatabaseResponse struct {
				models.ManagedDatabase
				Password    string `json:"password"`
				InternalURL string `json:"internal_url"`
				ExternalURL string `json:"external_url"`
				PSQLCommand string `json:"psql_command"`
			}
			resp := DatabaseResponse{
				ManagedDatabase: *db,
				Password:        pw,
				InternalURL:     "postgresql://" + db.Username + ":" + pw + "@" + db.Host + ":" + intToStr(db.Port) + "/" + db.DBName,
				ExternalURL:     "postgresql://" + db.Username + ":" + pw + "@" + externalHost + ":" + intToStr(db.Port) + "/" + db.DBName,
				PSQLCommand:     "PGPASSWORD=" + pw + " psql -h " + db.Host + " -p " + intToStr(db.Port) + " -U " + db.Username + " " + db.DBName,
			}
			utils.RespondJSON(w, http.StatusOK, resp)
			return
		}
	}

	utils.RespondJSON(w, http.StatusOK, db)
}

func (h *DatabaseHandler) DeleteDatabase(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)

	// Get database to find container and plan
	db, err := models.GetManagedDatabase(id)
	if err != nil || db == nil {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Remove from Stripe subscription before deleting
	if db.Plan != "free" && h.Stripe.Enabled() {
		if err := h.Stripe.RemoveSubscriptionItem("database", id); err != nil {
			log.Printf("Warning: failed to remove billing for database %s: %v", id, err)
		}
	}
	if db.ContainerID != "" {
		// Legacy docker mode only; in k8s mode we delete Kubernetes resources instead.
		if h.Config == nil || !h.Config.Kubernetes.Enabled {
			h.Worker.Deployer.RemoveContainer(db.ContainerID)
		}
	}
	if h.Config != nil && h.Config.Kubernetes.Enabled && h.Worker != nil {
		if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
			_ = kd.DeleteManagedDatabaseResources(db.ID)
		}
	}

	if err := models.DeleteManagedDatabase(id); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete database")
		return
	}
	services.Audit(db.WorkspaceID, userID, "database.deleted", "database", id, map[string]interface{}{
		"name": db.Name,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *DatabaseHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
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
	rows, err := database.DB.Query("SELECT id, resource_type, resource_id, COALESCE(file_path,''), COALESCE(size_bytes,0), started_at, finished_at, COALESCE(status,'') FROM backups WHERE resource_type=$1 AND resource_id=$2 ORDER BY started_at DESC", "database", id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list backups")
		return
	}
	defer rows.Close()
	type Backup struct {
		ID           string     `json:"id"`
		ResourceType string     `json:"resource_type"`
		ResourceID   string     `json:"resource_id"`
		FilePath     string     `json:"file_path"`
		SizeBytes    int64      `json:"size_bytes"`
		StartedAt    *time.Time `json:"started_at"`
		FinishedAt   *time.Time `json:"finished_at"`
		Status       string     `json:"status"`
	}
	var backups []Backup
	for rows.Next() {
		var b Backup
		if err := rows.Scan(&b.ID, &b.ResourceType, &b.ResourceID, &b.FilePath, &b.SizeBytes, &b.StartedAt, &b.FinishedAt, &b.Status); err != nil {
			continue
		}
		backups = append(backups, b)
	}
	if backups == nil {
		backups = []Backup{}
	}
	utils.RespondJSON(w, http.StatusOK, backups)
}

// TriggerBackup runs an actual pg_dump against the database container
func (h *DatabaseHandler) TriggerBackup(w http.ResponseWriter, r *http.Request) {
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

	var backupID string
	err = database.DB.QueryRow("INSERT INTO backups (resource_type, resource_id, status, started_at) VALUES ($1, $2, $3, NOW()) RETURNING id", "database", id, "running").Scan(&backupID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create backup record")
		return
	}

	// Run pg_dump in background
	go func() {
		backupDir := h.Config.Deploy.BackupDir
		os.MkdirAll(backupDir, 0755)

		filename := fmt.Sprintf("%s_%s.sql", db.DBName, time.Now().Format("20060102_150405"))
		filePath := filepath.Join(backupDir, filename)
		containerName := fmt.Sprintf("sr-db-%s", db.ID[:8])

		// Decrypt password
		pw := ""
		if db.EncryptedPassword != "" {
			decrypted, err := utils.Decrypt(db.EncryptedPassword, h.Config.Crypto.EncryptionKey)
			if err == nil {
				pw = decrypted
			}
		}

		// Run pg_dump inside the container
		out, err := h.Worker.Deployer.ExecCommand("docker", "exec",
			"-e", fmt.Sprintf("PGPASSWORD=%s", pw),
			containerName,
			"pg_dump", "-U", db.Username, "-d", db.DBName, "--clean", "--if-exists")
		if err != nil {
			log.Printf("Backup failed for database %s: %v", db.Name, err)
			database.DB.Exec("UPDATE backups SET status=$1, finished_at=NOW() WHERE id=$2", "failed", backupID)
			return
		}

		// Write to file
		if err := os.WriteFile(filePath, []byte(out), 0644); err != nil {
			log.Printf("Failed to write backup file: %v", err)
			database.DB.Exec("UPDATE backups SET status=$1, finished_at=NOW() WHERE id=$2", "failed", backupID)
			return
		}

		// Get file size
		info, _ := os.Stat(filePath)
		size := int64(0)
		if info != nil {
			size = info.Size()
		}

		database.DB.Exec("UPDATE backups SET status=$1, file_path=$2, size_bytes=$3, finished_at=NOW() WHERE id=$4",
			"completed", filePath, size, backupID)
		log.Printf("Backup completed for database %s: %s (%d bytes)", db.Name, filePath, size)
	}()

	services.Audit(db.WorkspaceID, userID, "database.backup_triggered", "database", db.ID, map[string]interface{}{
		"backup_id": backupID,
	})
	utils.RespondJSON(w, http.StatusCreated, map[string]string{"id": backupID, "status": "running"})
}

func intToStr(i int) string {
	return fmt.Sprintf("%d", i)
}

func (h *DatabaseHandler) ListReplicas(w http.ResponseWriter, r *http.Request) {
	primaryID := mux.Vars(r)["id"]
	primary, err := models.GetManagedDatabase(primaryID)
	if err != nil || primary == nil {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, primary.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	replicas, err := models.ListDatabaseReplicas(primaryID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list replicas")
		return
	}
	if replicas == nil {
		replicas = []models.DatabaseReplica{}
	}
	utils.RespondJSON(w, http.StatusOK, replicas)
}

func (h *DatabaseHandler) CreateReplica(w http.ResponseWriter, r *http.Request) {
	primaryID := mux.Vars(r)["id"]
	primary, err := models.GetManagedDatabase(primaryID)
	if err != nil || primary == nil {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, primary.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req struct {
		Name            string `json:"name"`
		Region          string `json:"region"`
		ReplicationMode string `json:"replication_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = primary.Name + "-replica"
	}
	if req.Region == "" {
		req.Region = "same-node"
	}
	if req.ReplicationMode == "" {
		req.ReplicationMode = "async"
	}

	replica := &models.DatabaseReplica{
		PrimaryDatabaseID: primary.ID,
		WorkspaceID:       primary.WorkspaceID,
		Name:              req.Name,
		Region:            req.Region,
		Status:            "creating",
		ReplicationMode:   req.ReplicationMode,
		LagSeconds:        0,
	}
	if err := models.CreateDatabaseReplica(replica); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create replica")
		return
	}

	pw := ""
	if primary.EncryptedPassword != "" {
		if decrypted, err := utils.Decrypt(primary.EncryptedPassword, h.Config.Crypto.EncryptionKey); err == nil {
			pw = decrypted
		}
	}
	h.Worker.ProvisionDatabaseReplica(primary, replica, pw)
	services.Audit(primary.WorkspaceID, userID, "database.replica_created", "database_replica", replica.ID, map[string]interface{}{
		"primary_database_id": primary.ID,
		"name":                replica.Name,
		"region":              replica.Region,
		"mode":                replica.ReplicationMode,
	})
	utils.RespondJSON(w, http.StatusCreated, replica)
}

func (h *DatabaseHandler) PromoteReplica(w http.ResponseWriter, r *http.Request) {
	primaryID := mux.Vars(r)["id"]
	replicaID := mux.Vars(r)["replicaId"]
	primary, err := models.GetManagedDatabase(primaryID)
	if err != nil || primary == nil {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, primary.WorkspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	replica, err := models.GetDatabaseReplica(replicaID)
	if err != nil || replica == nil || replica.PrimaryDatabaseID != primary.ID {
		utils.RespondError(w, http.StatusNotFound, "replica not found")
		return
	}
	if replica.ContainerID == "" || replica.Port == 0 {
		utils.RespondError(w, http.StatusBadRequest, "replica is not ready")
		return
	}

	// Promote by switching primary DB connection to replica container.
	if primary.ContainerID != "" {
		_ = h.Worker.Deployer.RemoveContainer(primary.ContainerID)
	}
	_ = models.UpdateManagedDatabaseStatus(primary.ID, "available", replica.ContainerID)
	_ = models.UpdateManagedDatabaseConnection(primary.ID, replica.Port, replica.Host)
	_ = models.PromoteDatabaseReplica(replica.ID)
	_ = models.UpdateManagedDatabaseHA(primary.ID, true, "manual-failover", &replica.ID)

	services.Audit(primary.WorkspaceID, userID, "database.failover_promoted", "database_replica", replica.ID, map[string]interface{}{
		"primary_database_id": primary.ID,
		"replica_id":          replica.ID,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "promoted"})
}

func (h *DatabaseHandler) EnableHA(w http.ResponseWriter, r *http.Request) {
	primaryID := mux.Vars(r)["id"]
	primary, err := models.GetManagedDatabase(primaryID)
	if err != nil || primary == nil {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, primary.WorkspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Create a standby replica if one does not already exist.
	replicas, _ := models.ListDatabaseReplicas(primary.ID)
	var standby *models.DatabaseReplica
	if len(replicas) > 0 {
		standby = &replicas[0]
	} else {
		name := primary.Name + "-standby"
		standby = &models.DatabaseReplica{
			PrimaryDatabaseID: primary.ID,
			WorkspaceID:       primary.WorkspaceID,
			Name:              name,
			Region:            "same-node",
			Status:            "creating",
			ReplicationMode:   "sync",
		}
		if err := models.CreateDatabaseReplica(standby); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to create standby replica")
			return
		}
		pw := ""
		if primary.EncryptedPassword != "" {
			if decrypted, err := utils.Decrypt(primary.EncryptedPassword, h.Config.Crypto.EncryptionKey); err == nil {
				pw = decrypted
			}
		}
		h.Worker.ProvisionDatabaseReplica(primary, standby, pw)
	}
	_ = models.UpdateManagedDatabaseHA(primary.ID, true, "single-standby", &standby.ID)
	services.Audit(primary.WorkspaceID, userID, "database.ha_enabled", "database", primary.ID, map[string]interface{}{
		"standby_replica_id": standby.ID,
		"strategy":           "single-standby",
	})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":             "ha_enabled",
		"standby_replica_id": standby.ID,
	})
}
