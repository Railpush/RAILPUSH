package handlers

import (
	"context"
	stdsql "database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	pagination, err := parseCursorPagination(r)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
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
	active := dbs[:0]
	for _, item := range dbs {
		if strings.EqualFold(strings.TrimSpace(item.Status), "soft_deleted") {
			continue
		}
		active = append(active, item)
	}
	dbs = active

	filterPlan := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("plan")))
	filterStatus := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	filterName := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("name")))
	filterQuery := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
	filterPGVersionRaw := strings.TrimSpace(r.URL.Query().Get("pg_version"))
	filterPGVersion := 0
	if filterPGVersionRaw != "" {
		parsed, err := strconv.Atoi(filterPGVersionRaw)
		if err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid pg_version")
			return
		}
		filterPGVersion = parsed
	}

	if filterPlan != "" || filterStatus != "" || filterName != "" || filterQuery != "" || filterPGVersionRaw != "" {
		filtered := dbs[:0]
		for _, item := range dbs {
			if filterPlan != "" && strings.ToLower(strings.TrimSpace(item.Plan)) != filterPlan {
				continue
			}
			if filterStatus != "" && strings.ToLower(strings.TrimSpace(item.Status)) != filterStatus {
				continue
			}
			if filterPGVersionRaw != "" && item.PGVersion != filterPGVersion {
				continue
			}
			if filterName != "" && !strings.Contains(strings.ToLower(item.Name), filterName) {
				continue
			}
			if filterQuery != "" {
				haystack := strings.ToLower(strings.Join([]string{
					item.Name,
					item.DBName,
					item.Host,
					item.Plan,
					item.Status,
				}, " "))
				if !strings.Contains(haystack, filterQuery) {
					continue
				}
			}
			filtered = append(filtered, item)
		}
		dbs = filtered
	}
	paged, pageMeta := paginateSlice(dbs, pagination)
	if pageMeta != nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"data":       paged,
			"pagination": pageMeta,
		})
		return
	}
	utils.RespondJSON(w, http.StatusOK, paged)
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
	var billingCustomer *models.BillingCustomer
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
		if bc == nil {
			utils.RespondError(w, http.StatusInternalServerError, "billing error: failed to initialize billing customer")
			return
		}
		billingCustomer = bc
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
		h.syncDatabaseLinks(db.ID)
	}

	// Add to Stripe subscription for paid plans
	if db.Plan != "free" && h.Stripe.Enabled() && billingCustomer != nil {
		if err := h.Stripe.AddSubscriptionItem(billingCustomer, db.WorkspaceID, "database", db.ID, db.Name, db.Plan); err != nil {
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
	if strings.EqualFold(strings.TrimSpace(db.Status), "soft_deleted") {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	pw := ""
	if db.EncryptedPassword != "" {
		if decrypted, derr := utils.Decrypt(db.EncryptedPassword, h.Config.Crypto.EncryptionKey); derr == nil {
			pw = decrypted
		}
	}
	resp := h.databaseResponse(db, pw, false)
	utils.RespondJSON(w, http.StatusOK, resp)
}

func (h *DatabaseHandler) RevealDatabaseCredentials(w http.ResponseWriter, r *http.Request) {
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
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		AcknowledgeSensitiveOutput bool `json:"acknowledge_sensitive_output"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.AcknowledgeSensitiveOutput {
		utils.RespondError(w, http.StatusBadRequest, "acknowledge_sensitive_output must be true")
		return
	}

	if strings.TrimSpace(db.EncryptedPassword) == "" {
		utils.RespondError(w, http.StatusNotFound, "database credentials unavailable")
		return
	}
	pw, err := utils.Decrypt(db.EncryptedPassword, h.Config.Crypto.EncryptionKey)
	if err != nil || strings.TrimSpace(pw) == "" {
		utils.RespondError(w, http.StatusInternalServerError, "failed to decrypt database credentials")
		return
	}

	services.Audit(db.WorkspaceID, userID, "database.credentials.revealed", "database", db.ID, map[string]interface{}{
		"api_key_id": middleware.GetAPIKeyID(r),
	})

	resp := h.databaseResponse(db, pw, true)
	utils.RespondJSON(w, http.StatusOK, resp)
}

func (h *DatabaseHandler) QueryDatabase(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	db, err := models.GetManagedDatabase(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if db == nil || strings.EqualFold(strings.TrimSpace(db.Status), "soft_deleted") {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}

	userID := middleware.GetUserID(r)
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Query                string `json:"query"`
		AllowWrite           bool   `json:"allow_write"`
		AcknowledgeRiskyQuery bool   `json:"acknowledge_risky_query"`
		MaxRows              int    `json:"max_rows"`
		TimeoutMS            int    `json:"timeout_ms"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		utils.RespondError(w, http.StatusBadRequest, "query is required")
		return
	}
	if req.MaxRows <= 0 {
		req.MaxRows = 100
	}
	if req.MaxRows > 1000 {
		utils.RespondError(w, http.StatusBadRequest, "max_rows cannot exceed 1000")
		return
	}
	if req.TimeoutMS <= 0 {
		req.TimeoutMS = 15000
	}
	if req.TimeoutMS > 120000 {
		utils.RespondError(w, http.StatusBadRequest, "timeout_ms cannot exceed 120000")
		return
	}

	firstKeyword := sqlFirstKeyword(req.Query)
	if !req.AllowWrite && !isReadOnlySQLKeyword(firstKeyword) {
		utils.RespondJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":        "query blocked in read-only mode",
			"first_keyword": firstKeyword,
			"hint":         "set allow_write=true and acknowledge_risky_query=true for write-capable execution",
		})
		return
	}
	if req.AllowWrite && !req.AcknowledgeRiskyQuery {
		utils.RespondError(w, http.StatusBadRequest, "allow_write requires acknowledge_risky_query=true")
		return
	}

	apiKeyID := middleware.GetAPIKeyID(r)
	apiKeyScopes := middleware.GetAPIKeyScopes(r)
	if req.AllowWrite && apiKeyID != "" && !models.HasAnyAPIKeyScope(apiKeyScopes, models.APIKeyScopeAdmin) {
		utils.RespondError(w, http.StatusForbidden, "write SQL queries via API key require admin scope")
		return
	}

	if strings.TrimSpace(db.EncryptedPassword) == "" {
		utils.RespondError(w, http.StatusBadRequest, "database credentials are not ready yet")
		return
	}
	password, err := utils.Decrypt(db.EncryptedPassword, h.Config.Crypto.EncryptionKey)
	if err != nil || strings.TrimSpace(password) == "" {
		utils.RespondError(w, http.StatusInternalServerError, "failed to decrypt database credentials")
		return
	}

	startedAt := time.Now()
	if h.Config != nil && h.Config.Kubernetes.Enabled && strings.HasPrefix(strings.TrimSpace(db.ContainerID), "k8s:") {
		if h.Worker == nil {
			utils.RespondError(w, http.StatusBadGateway, "kubernetes query executor unavailable")
			return
		}
		kd, err := h.Worker.GetKubeDeployer()
		if err != nil || kd == nil {
			utils.RespondError(w, http.StatusBadGateway, "failed to initialize kubernetes query executor")
			return
		}
		querySQL := req.Query
		if !req.AllowWrite {
			querySQL = "BEGIN READ ONLY;\n" + ensureSQLTerminated(req.Query) + "\nCOMMIT;"
		}
		stdout, stderr, err := kd.RunDatabaseQuery(db, password, querySQL, time.Duration(req.TimeoutMS)*time.Millisecond)
		if err != nil {
			details := strings.TrimSpace(stderr)
			if details == "" {
				details = err.Error()
			}
			utils.RespondJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "query failed", "details": details})
			return
		}

		columns, parsedRows, truncated, parsed := parsePSQLCSVOutput(stdout, req.MaxRows)
		auditDetails := map[string]interface{}{
			"allow_write":   req.AllowWrite,
			"first_keyword": firstKeyword,
			"max_rows":      req.MaxRows,
			"duration_ms":   time.Since(startedAt).Milliseconds(),
			"api_key_id":    apiKeyID,
			"execution_mode": "k8s_exec",
		}
		if parsed {
			auditDetails["rows_returned"] = len(parsedRows)
			auditDetails["rows_truncated"] = truncated
		} else {
			auditDetails["raw_output"] = strings.TrimSpace(stdout)
		}
		services.Audit(db.WorkspaceID, userID, "database.query_executed", "database", db.ID, auditDetails)

		if parsed {
			utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
				"database_id":       db.ID,
				"allow_write":       req.AllowWrite,
				"first_keyword":     firstKeyword,
				"execution_mode":    "k8s_exec",
				"execution_time_ms": time.Since(startedAt).Milliseconds(),
				"row_count":         len(parsedRows),
				"truncated":         truncated,
				"columns":           columns,
				"rows":              parsedRows,
			})
			return
		}

		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"database_id":       db.ID,
			"allow_write":       req.AllowWrite,
			"first_keyword":     firstKeyword,
			"execution_mode":    "k8s_exec",
			"execution_time_ms": time.Since(startedAt).Milliseconds(),
			"raw_output":        strings.TrimSpace(stdout),
			"stderr":            strings.TrimSpace(stderr),
		})
		return
	}

	host := strings.TrimSpace(db.Host)
	if host == "" || db.Port <= 0 || strings.TrimSpace(db.Username) == "" || strings.TrimSpace(db.DBName) == "" {
		utils.RespondError(w, http.StatusBadRequest, "database endpoint is not ready yet")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()

	hostCandidates := []string{host}
	if !strings.Contains(host, ".") {
		hostCandidates = append(hostCandidates, host+".railpush.svc.cluster.local")
	}
	sslModes := []string{"disable", "require"}
	var conn *stdsql.DB
	var tx *stdsql.Tx
	var connectErr error
	for _, candidateHost := range hostCandidates {
		for _, sslMode := range sslModes {
			dsn := buildPostgresDSN(candidateHost, db.Port, strings.TrimSpace(db.DBName), strings.TrimSpace(db.Username), password, sslMode)
			candidateConn, err := stdsql.Open("postgres", dsn)
			if err != nil {
				connectErr = err
				continue
			}
			candidateConn.SetMaxOpenConns(1)
			candidateConn.SetMaxIdleConns(0)
			candidateTx, err := candidateConn.BeginTx(ctx, &stdsql.TxOptions{ReadOnly: !req.AllowWrite})
			if err != nil {
				connectErr = err
				candidateConn.Close()
				continue
			}
			conn = candidateConn
			tx = candidateTx
			connectErr = nil
			break
		}
		if tx != nil {
			break
		}
	}
	if connectErr != nil || tx == nil || conn == nil {
		details := "connection failed"
		if connectErr != nil {
			details = connectErr.Error()
		}
		utils.RespondJSON(w, http.StatusBadGateway, map[string]interface{}{"error": "failed to start query transaction", "details": details})
		return
	}
	defer conn.Close()
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", req.TimeoutMS)); err != nil {
		utils.RespondError(w, http.StatusBadGateway, "failed to apply statement timeout")
		return
	}

	shouldReturnRows := isReadOnlySQLKeyword(firstKeyword) || strings.EqualFold(firstKeyword, "WITH") || strings.Contains(strings.ToLower(req.Query), "returning")

	if shouldReturnRows {
		rows, err := tx.QueryContext(ctx, req.Query)
		if err != nil {
			utils.RespondJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "query failed", "details": err.Error()})
			return
		}
		defer rows.Close()

		columns, err := rows.Columns()
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to read query columns")
			return
		}
		resultRows := make([]map[string]interface{}, 0)
		truncated := false
		for rows.Next() {
			if len(resultRows) >= req.MaxRows {
				truncated = true
				break
			}
			raw := make([]interface{}, len(columns))
			dest := make([]interface{}, len(columns))
			for i := range raw {
				dest[i] = &raw[i]
			}
			if err := rows.Scan(dest...); err != nil {
				utils.RespondError(w, http.StatusInternalServerError, "failed to scan query row")
				return
			}
			row := make(map[string]interface{}, len(columns))
			for i, col := range columns {
				row[col] = normalizeSQLValue(raw[i])
			}
			resultRows = append(resultRows, row)
		}
		if err := rows.Err(); err != nil {
			utils.RespondJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "query failed", "details": err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to finalize query transaction")
			return
		}

		services.Audit(db.WorkspaceID, userID, "database.query_executed", "database", db.ID, map[string]interface{}{
			"allow_write":    req.AllowWrite,
			"first_keyword":  firstKeyword,
			"max_rows":       req.MaxRows,
			"rows_returned":  len(resultRows),
			"rows_truncated": truncated,
			"duration_ms":    time.Since(startedAt).Milliseconds(),
			"api_key_id":     apiKeyID,
		})

		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"database_id":       db.ID,
			"allow_write":       req.AllowWrite,
			"first_keyword":     firstKeyword,
			"execution_time_ms": time.Since(startedAt).Milliseconds(),
			"row_count":         len(resultRows),
			"truncated":         truncated,
			"columns":           columns,
			"rows":              resultRows,
		})
		return
	}

	result, err := tx.ExecContext(ctx, req.Query)
	if err != nil {
		utils.RespondJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "query failed", "details": err.Error()})
		return
	}
	rowsAffected, _ := result.RowsAffected()
	if err := tx.Commit(); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to finalize query transaction")
		return
	}

	services.Audit(db.WorkspaceID, userID, "database.query_executed", "database", db.ID, map[string]interface{}{
		"allow_write":   req.AllowWrite,
		"first_keyword": firstKeyword,
		"max_rows":      req.MaxRows,
		"rows_affected": rowsAffected,
		"duration_ms":   time.Since(startedAt).Milliseconds(),
		"api_key_id":    apiKeyID,
	})

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"database_id":       db.ID,
		"allow_write":       req.AllowWrite,
		"first_keyword":     firstKeyword,
		"execution_time_ms": time.Since(startedAt).Milliseconds(),
		"rows_affected":     rowsAffected,
	})
}

func (h *DatabaseHandler) databaseResponse(db *models.ManagedDatabase, password string, revealCredentials bool) map[string]interface{} {
	maskedPassword := "<redacted>"
	passwordForURL := maskedPassword
	if revealCredentials && strings.TrimSpace(password) != "" {
		passwordForURL = password
	}

	internalURL := "postgresql://" + db.Username + ":" + passwordForURL + "@" + db.Host + ":" + intToStr(db.Port) + "/" + db.DBName
	psqlCommand := "PGPASSWORD=" + passwordForURL + " psql -h " + db.Host + " -p " + intToStr(db.Port) + " -U " + db.Username + " " + db.DBName

	resp := map[string]interface{}{
		"id":                    db.ID,
		"workspace_id":          db.WorkspaceID,
		"name":                  db.Name,
		"plan":                  db.Plan,
		"pg_version":            db.PGVersion,
		"container_id":          db.ContainerID,
		"host":                  db.Host,
		"port":                  db.Port,
		"external_port":         0,
		"db_name":               db.DBName,
		"username":              db.Username,
		"status":                db.Status,
		"ha_enabled":            db.HAEnabled,
		"ha_strategy":           db.HAStrategy,
		"standby_replica_id":    db.StandbyReplicaID,
		"init_script":           db.InitScript,
		"created_at":            db.CreatedAt,
		"internal_url":          internalURL,
		"external_url":          "",
		"external_psql_command": "",
		"external_access":       "disabled",
		"psql_command":          psqlCommand,
		"credentials_exposed":   revealCredentials,
	}
	if revealCredentials {
		resp["password"] = password
	}
	return resp
}

func (h *DatabaseHandler) UpdateDatabase(w http.ResponseWriter, r *http.Request) {
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
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	oldPlan := db.Plan
	if p, ok := services.NormalizePlan(oldPlan); ok {
		oldPlan = p
	} else {
		oldPlan = services.PlanStarter
	}

	planProvided := false
	desiredPlan := oldPlan
	deletionProtectionProvided := false
	deletionProtection := false
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if v, ok := updates["plan"].(string); ok {
		planProvided = true
		if p, ok := services.NormalizePlan(v); ok {
			desiredPlan = p
		} else {
			utils.RespondError(w, http.StatusBadRequest, "invalid plan")
			return
		}
	}
	if v, ok := updates["deletion_protection"].(bool); ok {
		deletionProtectionProvided = true
		deletionProtection = v
	}

	planChanged := planProvided && desiredPlan != oldPlan
	if isDryRunRequest(r) {
		projectedPlan := db.Plan
		if planChanged {
			projectedPlan = desiredPlan
		}
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"status":              "dry_run",
			"database_id":         db.ID,
			"workspace_id":        db.WorkspaceID,
			"plan":                projectedPlan,
			"plan_changed":        planChanged,
			"deletion_protection": deletionProtection,
			"has_deletion_toggle": deletionProtectionProvided,
		})
		return
	}

	if !planChanged && !deletionProtectionProvided {
		utils.RespondJSON(w, http.StatusOK, db)
		return
	}

	if planChanged {
		// Free tier: limit 1 free database per workspace
		if desiredPlan == services.PlanFree {
			count, err := models.CountResourcesByWorkspaceAndPlan(db.WorkspaceID, "database", "free")
			if err == nil && count >= 1 {
				utils.RespondError(w, http.StatusBadRequest, "free tier limit reached: 1 free database per workspace")
				return
			}
		}

		// Gate plan changes on Stripe success so users cannot upgrade resources without billing.
		if h.Stripe != nil && h.Stripe.Enabled() {
			if desiredPlan == services.PlanFree {
				if err := h.Stripe.RemoveSubscriptionItem("database", db.ID); err != nil {
					utils.RespondError(w, http.StatusBadGateway, "billing error: "+err.Error())
					return
				}
			} else {
				user, err := models.GetUserByID(userID)
				if err != nil || user == nil {
					utils.RespondError(w, http.StatusInternalServerError, "failed to get user")
					return
				}
				bc, err := h.Stripe.EnsureCustomer(userID, user.Email)
				if err != nil || bc == nil {
					if err == nil {
						err = fmt.Errorf("billing customer not found")
					}
					utils.RespondError(w, http.StatusBadGateway, "billing error: "+err.Error())
					return
				}
				if err := h.Stripe.AddSubscriptionItem(bc, db.WorkspaceID, "database", db.ID, db.Name, desiredPlan); err != nil {
					if errors.Is(err, services.ErrNoDefaultPaymentMethod) {
						utils.RespondError(w, http.StatusPaymentRequired, "payment method required. Please add a default payment method in billing settings.")
						return
					}
					utils.RespondError(w, http.StatusBadGateway, "billing error: "+err.Error())
					return
				}
			}
		}

		db.Plan = desiredPlan
		if err := models.UpdateManagedDatabasePlan(db.ID, desiredPlan); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to update database plan")
			return
		}

		// Best-effort: apply Kubernetes resource updates immediately.
		if h.Config != nil && h.Config.Kubernetes.Enabled && strings.HasPrefix(strings.TrimSpace(db.ContainerID), "k8s:") {
			if pw, err := utils.Decrypt(db.EncryptedPassword, h.Config.Crypto.EncryptionKey); err == nil && strings.TrimSpace(pw) != "" {
				var kd *services.KubeDeployer
				if h.Worker != nil {
					if k, err := h.Worker.GetKubeDeployer(); err == nil {
						kd = k
					}
				}
				if kd == nil {
					if k, err := services.NewKubeDeployer(h.Config); err == nil {
						kd = k
					}
				}
				if kd != nil {
					if _, err := kd.EnsureManagedDatabase(db, pw); err != nil {
						log.Printf("WARNING: k8s managed database update failed db=%s: %v", db.ID, err)
					}
				}
			}
		}
	}

	if deletionProtectionProvided {
		if err := models.SetResourceDeletionProtection("database", db.ID, db.WorkspaceID, db.Name, deletionProtection); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to update deletion protection")
			return
		}
	}

	var deletionProtectionAudit interface{}
	if deletionProtectionProvided {
		deletionProtectionAudit = deletionProtection
	}
	services.Audit(db.WorkspaceID, userID, "database.updated", "database", db.ID, map[string]interface{}{
		"plan":                db.Plan,
		"deletion_protection": deletionProtectionAudit,
	})

	utils.RespondJSON(w, http.StatusOK, db)
}

func (h *DatabaseHandler) DeleteDatabase(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)

	db, err := models.GetManagedDatabase(id)
	if err != nil || db == nil {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	state, err := models.GetResourceDeletionState("database", db.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to read deletion state")
		return
	}
	if state != nil && state.DeletionProtection {
		utils.RespondError(w, http.StatusForbidden, "deletion protection is enabled for this database")
		return
	}

	var req destructiveDeleteRequest
	if err := decodeOptionalJSONBody(w, r, &req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token := strings.TrimSpace(req.ConfirmationToken)
	if token == "" {
		confirmationToken, expiresAt, err := models.IssueResourceDeletionToken("database", db.ID, db.WorkspaceID, db.Name, deleteConfirmationTTL)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to issue confirmation token")
			return
		}
		utils.RespondJSON(w, http.StatusAccepted, map[string]interface{}{
			"status":                     "confirmation_required",
			"confirmation_token":         confirmationToken,
			"confirmation_token_expires": expiresAt,
			"hard_delete":                false,
			"recovery_window_hours":      int(softDeleteRecoveryWindow / time.Hour),
		})
		return
	}
	if err := models.VerifyResourceDeletionToken("database", db.ID, token); err != nil {
		switch {
		case errors.Is(err, models.ErrDeleteConfirmationExpired):
			utils.RespondError(w, http.StatusBadRequest, "confirmation token expired; request a new token")
		case errors.Is(err, models.ErrDeleteConfirmationInvalid):
			utils.RespondError(w, http.StatusBadRequest, "invalid confirmation token")
		default:
			utils.RespondError(w, http.StatusBadRequest, "confirmation token required")
		}
		return
	}

	if req.HardDelete {
		if state == nil || state.DeletedAt == nil {
			utils.RespondError(w, http.StatusConflict, "database must be soft-deleted before hard delete")
			return
		}
		if state.PurgeAfter != nil && time.Now().Before(*state.PurgeAfter) {
			utils.RespondError(w, http.StatusConflict, "database is in recovery window; hard delete available after "+state.PurgeAfter.Format(time.RFC3339))
			return
		}
		if err := h.hardDeleteDatabase(r, db); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to delete database")
			return
		}
		_ = models.DeleteResourceDeletionState("database", db.ID)
		services.Audit(db.WorkspaceID, userID, "database.deleted", "database", id, map[string]interface{}{
			"name": db.Name,
		})
		utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return
	}
	if state != nil && state.DeletedAt != nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"status":      "soft_deleted",
			"purge_after": state.PurgeAfter,
		})
		return
	}

	purgeAfter, err := models.MarkResourceSoftDeleted("database", db.ID, db.WorkspaceID, db.Name, softDeleteRecoveryWindow)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to soft-delete database")
		return
	}
	_ = models.UpdateManagedDatabaseStatus(db.ID, "soft_deleted", db.ContainerID)

	services.Audit(db.WorkspaceID, userID, "database.soft_deleted", "database", id, map[string]interface{}{
		"name":        db.Name,
		"purge_after": purgeAfter,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":                "soft_deleted",
		"purge_after":           purgeAfter,
		"recovery_window_hours": int(softDeleteRecoveryWindow / time.Hour),
	})
}

func (h *DatabaseHandler) RestoreDatabase(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	db, err := models.GetManagedDatabase(id)
	if err != nil || db == nil {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, db.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	state, err := models.GetResourceDeletionState("database", db.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to read deletion state")
		return
	}
	if state == nil || state.DeletedAt == nil {
		utils.RespondError(w, http.StatusBadRequest, "database is not soft-deleted")
		return
	}
	if err := models.RestoreSoftDeletedResource("database", db.ID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to restore database")
		return
	}
	_ = models.UpdateManagedDatabaseStatus(db.ID, "available", db.ContainerID)
	services.Audit(db.WorkspaceID, userID, "database.restored", "database", id, map[string]interface{}{
		"name": db.Name,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"status": "restored"})
}

func (h *DatabaseHandler) hardDeleteDatabase(r *http.Request, db *models.ManagedDatabase) error {
	id := db.ID

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
			// Remove TCP proxy entry so the external port is freed.
			if db.ExternalPort > 0 {
				ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
				_ = kd.RemoveTCPServiceEntry(ctx, db.ExternalPort)
				cancel()
			}
		}
	}

	// Remove any blueprint links to this database to avoid stale resources in blueprint UIs.
	_ = models.DeleteBlueprintResourcesByResource("database", id)
	if err := models.DeleteManagedDatabase(id); err != nil {
		return err
	}
	return nil
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
	pagination, err := parseCursorPagination(r)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
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
	paged, pageMeta := paginateSlice(backups, pagination)
	if pageMeta != nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"data":       paged,
			"pagination": pageMeta,
		})
		return
	}
	utils.RespondJSON(w, http.StatusOK, paged)
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

func isReadOnlySQLKeyword(keyword string) bool {
	switch strings.ToUpper(strings.TrimSpace(keyword)) {
	case "SELECT", "SHOW", "VALUES", "EXPLAIN", "DESCRIBE", "WITH":
		return true
	default:
		return false
	}
}

func sqlFirstKeyword(query string) string {
	s := strings.TrimSpace(query)
	for {
		s = strings.TrimSpace(s)
		if s == "" {
			return ""
		}
		if strings.HasPrefix(s, "--") {
			if idx := strings.IndexByte(s, '\n'); idx >= 0 {
				s = s[idx+1:]
				continue
			}
			return ""
		}
		if strings.HasPrefix(s, "/*") {
			if idx := strings.Index(s, "*/"); idx >= 0 {
				s = s[idx+2:]
				continue
			}
			return ""
		}
		if s[0] == ';' {
			s = s[1:]
			continue
		}
		break
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToUpper(fields[0])
}

func normalizeSQLValue(v interface{}) interface{} {
	switch val := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(val)
	case time.Time:
		return val.UTC().Format(time.RFC3339Nano)
	default:
		return val
	}
}

func parsePSQLCSVOutput(raw string, maxRows int) ([]string, []map[string]interface{}, bool, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil, false, false
	}
	r := csv.NewReader(strings.NewReader(trimmed))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil || len(records) < 2 {
		return nil, nil, false, false
	}
	columns := records[0]
	if len(columns) == 0 {
		return nil, nil, false, false
	}
	rows := make([]map[string]interface{}, 0, len(records)-1)
	truncated := false
	for _, rec := range records[1:] {
		if len(rows) >= maxRows {
			truncated = true
			break
		}
		row := make(map[string]interface{}, len(columns))
		for i, col := range columns {
			if i < len(rec) {
				row[col] = rec[i]
			} else {
				row[col] = ""
			}
		}
		rows = append(rows, row)
	}
	return columns, rows, truncated, true
}

func ensureSQLTerminated(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ";"
	}
	if strings.HasSuffix(trimmed, ";") {
		return trimmed
	}
	return trimmed + ";"
}

func buildPostgresDSN(host string, port int, dbName, username, password, sslMode string) string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(username, password),
		Host:   net.JoinHostPort(host, intToStr(port)),
		Path:   "/" + dbName,
	}
	q := u.Query()
	q.Set("sslmode", sslMode)
	u.RawQuery = q.Encode()
	return u.String()
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
	h.syncDatabaseLinks(primary.ID)
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

func (h *DatabaseHandler) linkedDatabaseURL(db *models.ManagedDatabase, password string, useInternal bool) string {
	if db == nil {
		return ""
	}
	host := strings.TrimSpace(db.Host)
	port := db.Port
	if !useInternal {
		// External managed database endpoints are currently disabled platform-wide
		// until IP allowlisting is available.
		useInternal = true
	}
	if host == "" || port <= 0 {
		return ""
	}
	url := "postgresql://" + db.Username + ":" + password + "@" + host + ":" + intToStr(port) + "/" + db.DBName
	return url
}

func (h *DatabaseHandler) syncDatabaseLinks(dbID string) {
	db, err := models.GetManagedDatabase(dbID)
	if err != nil || db == nil || strings.TrimSpace(db.EncryptedPassword) == "" {
		return
	}
	pw, err := utils.Decrypt(db.EncryptedPassword, h.Config.Crypto.EncryptionKey)
	if err != nil || strings.TrimSpace(pw) == "" {
		return
	}
	links, err := models.ListServiceDatabaseLinksByDatabase(db.ID)
	if err != nil || len(links) == 0 {
		return
	}
	for _, l := range links {
		svc, err := models.GetService(l.ServiceID)
		if err != nil || svc == nil {
			continue
		}
		if svc.WorkspaceID != db.WorkspaceID {
			continue
		}
		url := h.linkedDatabaseURL(db, pw, l.UseInternalURL)
		if strings.TrimSpace(url) == "" {
			continue
		}
		encrypted, err := utils.Encrypt(url, h.Config.Crypto.EncryptionKey)
		if err != nil {
			continue
		}
		_ = models.MergeUpsertEnvVars("service", svc.ID, []models.EnvVar{{
			Key:            l.EnvVarName,
			EncryptedValue: encrypted,
			IsSecret:       true,
		}})
	}
}

func (h *DatabaseHandler) LinkDatabaseToService(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)

	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		DatabaseID     string `json:"database_id"`
		EnvVarName     string `json:"env_var_name"`
		UseInternalURL *bool  `json:"use_internal_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.DatabaseID) == "" {
		utils.RespondError(w, http.StatusBadRequest, "database_id is required")
		return
	}
	db, err := models.GetManagedDatabase(strings.TrimSpace(req.DatabaseID))
	if err != nil || db == nil {
		utils.RespondError(w, http.StatusNotFound, "database not found")
		return
	}
	if db.WorkspaceID != svc.WorkspaceID {
		utils.RespondError(w, http.StatusBadRequest, "database and service must be in the same workspace")
		return
	}
	envVar := strings.ToUpper(strings.TrimSpace(req.EnvVarName))
	if envVar == "" {
		envVar = "DATABASE_URL"
	}
	if req.UseInternalURL != nil && !*req.UseInternalURL {
		utils.RespondError(w, http.StatusBadRequest, "external database URLs are disabled pending IP allowlisting support")
		return
	}
	useInternal := true

	if strings.TrimSpace(db.EncryptedPassword) == "" {
		utils.RespondError(w, http.StatusBadRequest, "database credentials are not ready yet")
		return
	}
	pw, err := utils.Decrypt(db.EncryptedPassword, h.Config.Crypto.EncryptionKey)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to decrypt database credentials")
		return
	}
	url := h.linkedDatabaseURL(db, pw, useInternal)
	if strings.TrimSpace(url) == "" {
		utils.RespondError(w, http.StatusBadRequest, "database endpoint is not ready yet")
		return
	}
	encrypted, err := utils.Encrypt(url, h.Config.Crypto.EncryptionKey)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to store database link")
		return
	}
	if err := models.MergeUpsertEnvVars("service", svc.ID, []models.EnvVar{{
		Key:            envVar,
		EncryptedValue: encrypted,
		IsSecret:       true,
	}}); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to inject env var")
		return
	}

	link := &models.ServiceDatabaseLink{ServiceID: svc.ID, DatabaseID: db.ID, EnvVarName: envVar, UseInternalURL: useInternal}
	if err := models.UpsertServiceDatabaseLink(link); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create link")
		return
	}

	services.Audit(svc.WorkspaceID, userID, "database.linked", "service", svc.ID, map[string]interface{}{
		"database_id":      db.ID,
		"env_var_name":     envVar,
		"use_internal_url": useInternal,
	})
	utils.RespondJSON(w, http.StatusCreated, link)
}

func (h *DatabaseHandler) ListServiceDatabaseLinks(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	links, err := models.ListServiceDatabaseLinks(serviceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list links")
		return
	}
	if links == nil {
		links = []models.ServiceDatabaseLink{}
	}
	utils.RespondJSON(w, http.StatusOK, links)
}

func (h *DatabaseHandler) UnlinkDatabaseFromService(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	databaseID := strings.TrimSpace(mux.Vars(r)["databaseId"])
	envVar := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("env_var_name")))
	if envVar == "" {
		envVar = "DATABASE_URL"
	}
	userID := middleware.GetUserID(r)

	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := models.DeleteServiceDatabaseLink(serviceID, databaseID, envVar); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete link")
		return
	}
	_ = models.DeleteEnvVarsByKeys("service", serviceID, []string{envVar})

	services.Audit(svc.WorkspaceID, userID, "database.unlinked", "service", svc.ID, map[string]interface{}{
		"database_id":  databaseID,
		"env_var_name": envVar,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "unlinked"})
}
