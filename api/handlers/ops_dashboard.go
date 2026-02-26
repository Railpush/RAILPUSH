package handlers

import (
	"encoding/json"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/database"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type OpsDashboardHandler struct {
	Config *config.Config
}

func NewOpsDashboardHandler(cfg *config.Config) *OpsDashboardHandler {
	return &OpsDashboardHandler{Config: cfg}
}

func (h *OpsDashboardHandler) ensureOps(w http.ResponseWriter, r *http.Request) bool {
	userID := middleware.GetUserID(r)
	if err := services.EnsureOpsAccess(userID); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func clamp(n, min, max int) int {
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func parseLimitOffset(r *http.Request) (limit int, offset int) {
	limit = clamp(utils.GetQueryInt(r, "limit", 50), 1, 200)
	offset = utils.GetQueryInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (h *OpsDashboardHandler) Overview(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}

	type counts struct {
		UsersTotal      int64 `json:"users_total"`
		WorkspacesTotal int64 `json:"workspaces_total"`
		ServicesTotal   int64 `json:"services_total"`

		DeploysPending   int64 `json:"deploys_pending"`
		DeploysBuilding  int64 `json:"deploys_building"`
		DeploysDeploying int64 `json:"deploys_deploying"`
		DeploysFailed24h int64 `json:"deploys_failed_24h"`

		EmailPending int64 `json:"email_pending"`
		EmailRetry   int64 `json:"email_retry"`
		EmailDead    int64 `json:"email_dead"`
	}

	out := counts{}
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&out.UsersTotal)
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM workspaces").Scan(&out.WorkspacesTotal)
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM services").Scan(&out.ServicesTotal)

	_ = database.DB.QueryRow("SELECT COUNT(*) FROM deploys WHERE status='pending'").Scan(&out.DeploysPending)
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM deploys WHERE status='building'").Scan(&out.DeploysBuilding)
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM deploys WHERE status='deploying'").Scan(&out.DeploysDeploying)
	_ = database.DB.QueryRow(
		"SELECT COUNT(*) FROM deploys WHERE status='failed' AND COALESCE(finished_at, created_at) >= NOW() - INTERVAL '24 hours'",
	).Scan(&out.DeploysFailed24h)

	_ = database.DB.QueryRow("SELECT COUNT(*) FROM email_outbox WHERE status='pending'").Scan(&out.EmailPending)
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM email_outbox WHERE status='retry'").Scan(&out.EmailRetry)
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM email_outbox WHERE status='dead'").Scan(&out.EmailDead)

	utils.RespondJSON(w, http.StatusOK, out)
}

func (h *OpsDashboardHandler) Settings(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	cfg := h.Config
	if cfg == nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"control_plane_domain": strings.TrimSpace(cfg.ControlPlane.Domain),
		"deploy_domain":        strings.TrimSpace(cfg.Deploy.Domain),
		"kube_enabled":         cfg.Kubernetes.Enabled,
		"email": map[string]interface{}{
			"enabled":  cfg.Email.Enabled(),
			"provider": strings.TrimSpace(cfg.Email.Provider),
			"from":     strings.TrimSpace(cfg.Email.SMTPFrom),
			"reply_to": strings.TrimSpace(cfg.Email.SMTPReplyTo),
			"smtp": map[string]interface{}{
				"host": strings.TrimSpace(cfg.Email.SMTPHost),
				"port": cfg.Email.SMTPPort,
			},
			"outbox": map[string]interface{}{
				"poll_interval_ms": cfg.Email.Outbox.PollIntervalMS,
				"batch_size":       cfg.Email.Outbox.BatchSize,
				"lease_seconds":    cfg.Email.Outbox.LeaseSeconds,
				"max_attempts":     cfg.Email.Outbox.MaxAttempts,
			},
		},
		"github": map[string]interface{}{
			"webhook_secret_configured": strings.TrimSpace(cfg.GitHub.WebhookSecret) != "",
			"callback_url":              strings.TrimSpace(cfg.GitHub.CallbackURL),
		},
		"alerts": map[string]interface{}{
			"alert_webhook_configured": strings.TrimSpace(cfg.Ops.AlertWebhookToken) != "",
			"alertmanager_url":         strings.TrimSpace(cfg.Ops.AlertmanagerURL),
		},
	})
}

// EnableAutoDeployAll flips auto_deploy=true for all services. This is an ops-only one-off action.
// Request body: { "confirm": "ENABLE" }
func (h *OpsDashboardHandler) EnableAutoDeployAll(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	var req struct {
		Confirm string `json:"confirm"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024)).Decode(&req)
	if strings.ToUpper(strings.TrimSpace(req.Confirm)) != "ENABLE" {
		utils.RespondError(w, http.StatusBadRequest, "confirmation required")
		return
	}
	res, err := database.DB.Exec("UPDATE services SET auto_deploy=true WHERE auto_deploy IS DISTINCT FROM true")
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update services")
		return
	}
	updated, _ := res.RowsAffected()
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"updated": updated,
	})
}

func (h *OpsDashboardHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	limit, offset := parseLimitOffset(r)
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	like := "%" + q + "%"

	rows, err := database.DB.Query(
		`SELECT id::text, COALESCE(username,''), COALESCE(email,''), COALESCE(role,'member'), COALESCE(is_suspended,false), created_at
		   FROM users
		  WHERE ($1 = '' OR username ILIKE $2 OR email ILIKE $2)
		  ORDER BY created_at DESC
		  LIMIT $3 OFFSET $4`,
		q, like, limit, offset,
	)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	defer rows.Close()

	type item struct {
		ID        string    `json:"id"`
		Username  string    `json:"username"`
		Email     string    `json:"email"`
		Role      string    `json:"role"`
		IsSuspended bool    `json:"is_suspended"`
		CreatedAt time.Time `json:"created_at"`
	}
	var out []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Username, &it.Email, &it.Role, &it.IsSuspended, &it.CreatedAt); err != nil {
			continue
		}
		out = append(out, it)
	}
	if out == nil {
		out = []item{}
	}
	utils.RespondJSON(w, http.StatusOK, out)
}

func (h *OpsDashboardHandler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	limit, offset := parseLimitOffset(r)
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	like := "%" + q + "%"

	rows, err := database.DB.Query(
		`SELECT w.id::text, COALESCE(w.name,''), COALESCE(w.owner_id::text,''), COALESCE(u.email,''), COALESCE(w.is_suspended,false), w.created_at
		   FROM workspaces w
		   LEFT JOIN users u ON u.id = w.owner_id
		  WHERE ($1 = '' OR w.name ILIKE $2 OR COALESCE(u.email,'') ILIKE $2)
		  ORDER BY w.created_at DESC
		  LIMIT $3 OFFSET $4`,
		q, like, limit, offset,
	)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list workspaces")
		return
	}
	defer rows.Close()

	type item struct {
		ID         string    `json:"id"`
		Name       string    `json:"name"`
		OwnerID    string    `json:"owner_id"`
		OwnerEmail string    `json:"owner_email"`
		IsSuspended bool     `json:"is_suspended"`
		CreatedAt  time.Time `json:"created_at"`
	}
	var out []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Name, &it.OwnerID, &it.OwnerEmail, &it.IsSuspended, &it.CreatedAt); err != nil {
			continue
		}
		out = append(out, it)
	}
	if out == nil {
		out = []item{}
	}
	utils.RespondJSON(w, http.StatusOK, out)
}

func (h *OpsDashboardHandler) ListServices(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	limit, offset := parseLimitOffset(r)
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	like := "%" + q + "%"
	status := strings.TrimSpace(r.URL.Query().Get("status"))

	rows, err := database.DB.Query(
		`SELECT s.id::text, COALESCE(s.workspace_id::text,''), COALESCE(w.name,''), COALESCE(s.name,''), COALESCE(s.subdomain,''),
		        COALESCE(s.type,''), COALESCE(s.runtime,''), COALESCE(s.status,'created'),
		        COALESCE(s.repo_url,''), COALESCE(s.branch,''), s.created_at, s.updated_at
		   FROM services s
		   LEFT JOIN workspaces w ON w.id = s.workspace_id
		  WHERE ($1 = '' OR s.name ILIKE $2 OR s.repo_url ILIKE $2 OR COALESCE(w.name,'') ILIKE $2)
		    AND ($3 = '' OR COALESCE(s.status,'') = $3)
		  ORDER BY s.updated_at DESC NULLS LAST, s.created_at DESC
		  LIMIT $4 OFFSET $5`,
		q, like, status, limit, offset,
	)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list services")
		return
	}
	defer rows.Close()

	type item struct {
		ID            string    `json:"id"`
		WorkspaceID   string    `json:"workspace_id"`
		WorkspaceName string    `json:"workspace_name"`
		Name          string    `json:"name"`
		Subdomain     string    `json:"subdomain"`
		Type          string    `json:"type"`
		Runtime       string    `json:"runtime"`
		Status        string    `json:"status"`
		RepoURL       string    `json:"repo_url"`
		Branch        string    `json:"branch"`
		CreatedAt     time.Time `json:"created_at"`
		UpdatedAt     time.Time `json:"updated_at"`
	}
	var out []item
	for rows.Next() {
		var it item
		if err := rows.Scan(
			&it.ID, &it.WorkspaceID, &it.WorkspaceName, &it.Name, &it.Subdomain,
			&it.Type, &it.Runtime, &it.Status, &it.RepoURL, &it.Branch, &it.CreatedAt, &it.UpdatedAt,
		); err != nil {
			continue
		}
		it.RepoURL = services.RedactRepoURLCredentials(it.RepoURL)
		out = append(out, it)
	}
	if out == nil {
		out = []item{}
	}
	utils.RespondJSON(w, http.StatusOK, out)
}

func (h *OpsDashboardHandler) ListDeploys(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	limit, offset := parseLimitOffset(r)
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	like := "%" + q + "%"
	status := strings.TrimSpace(r.URL.Query().Get("status"))

	rows, err := database.DB.Query(
		`SELECT d.id::text, COALESCE(d.service_id::text,''), COALESCE(s.name,''), COALESCE(d.status,'pending'),
		        COALESCE(d.trigger,''), COALESCE(d.branch,''), COALESCE(d.commit_sha,''), COALESCE(d.commit_message,''),
		        d.created_at, d.started_at, d.finished_at, COALESCE(d.last_error,'')
		   FROM deploys d
		   LEFT JOIN services s ON s.id = d.service_id
		  WHERE ($1 = '' OR COALESCE(d.status,'') = $1)
		    AND ($2 = '' OR COALESCE(s.name,'') ILIKE $3 OR COALESCE(d.commit_message,'') ILIKE $3)
		  ORDER BY COALESCE(d.started_at, d.created_at) DESC
		  LIMIT $4 OFFSET $5`,
		status, q, like, limit, offset,
	)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list deploys")
		return
	}
	defer rows.Close()

	type item struct {
		ID           string     `json:"id"`
		ServiceID    string     `json:"service_id"`
		ServiceName  string     `json:"service_name"`
		Status       string     `json:"status"`
		Trigger      string     `json:"trigger"`
		Branch       string     `json:"branch"`
		CommitSHA    string     `json:"commit_sha"`
		CommitMsg    string     `json:"commit_message"`
		CreatedAt    time.Time  `json:"created_at"`
		StartedAt    *time.Time `json:"started_at,omitempty"`
		FinishedAt   *time.Time `json:"finished_at,omitempty"`
		LastError    string     `json:"last_error,omitempty"`
	}
	var out []item
	for rows.Next() {
		var it item
		var startedAt, finishedAt sql.NullTime
		if err := rows.Scan(
			&it.ID, &it.ServiceID, &it.ServiceName, &it.Status,
			&it.Trigger, &it.Branch, &it.CommitSHA, &it.CommitMsg,
			&it.CreatedAt, &startedAt, &finishedAt, &it.LastError,
		); err != nil {
			continue
		}
		if startedAt.Valid {
			t := startedAt.Time
			it.StartedAt = &t
		}
		if finishedAt.Valid {
			t := finishedAt.Time
			it.FinishedAt = &t
		}
		out = append(out, it)
	}
	if out == nil {
		out = []item{}
	}
	utils.RespondJSON(w, http.StatusOK, out)
}

func (h *OpsDashboardHandler) ListEmailOutbox(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	limit, offset := parseLimitOffset(r)
	status := strings.TrimSpace(r.URL.Query().Get("status"))

	rows, err := database.DB.Query(
		`SELECT id::text, COALESCE(message_type,''), COALESCE(to_email,''), COALESCE(subject,''),
		        COALESCE(status,'pending'), COALESCE(attempts,0), COALESCE(last_error,''),
		        next_attempt_at, created_at, sent_at
		   FROM email_outbox
		  WHERE ($1 = '' OR COALESCE(status,'') = $1)
		  ORDER BY created_at DESC
		  LIMIT $2 OFFSET $3`,
		status, limit, offset,
	)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list email outbox")
		return
	}
	defer rows.Close()

	type item struct {
		ID          string     `json:"id"`
		MessageType string     `json:"message_type"`
		ToEmail     string     `json:"to_email"`
		Subject     string     `json:"subject"`
		Status      string     `json:"status"`
		Attempts    int        `json:"attempts"`
		LastError   string     `json:"last_error,omitempty"`
		NextAttempt time.Time  `json:"next_attempt_at"`
		CreatedAt   time.Time  `json:"created_at"`
		SentAt      *time.Time `json:"sent_at,omitempty"`
	}

	var out []item
	for rows.Next() {
		var it item
		var sentAt sql.NullTime
		if err := rows.Scan(
			&it.ID, &it.MessageType, &it.ToEmail, &it.Subject, &it.Status,
			&it.Attempts, &it.LastError, &it.NextAttempt, &it.CreatedAt, &sentAt,
		); err != nil {
			continue
		}
		if sentAt.Valid {
			t := sentAt.Time
			it.SentAt = &t
		}
		out = append(out, it)
	}
	if out == nil {
		out = []item{}
	}
	utils.RespondJSON(w, http.StatusOK, out)
}

// SendTestEmail enqueues a test message in the transactional outbox to validate SMTP setup.
// Body: { "to": "you@example.com" }
func (h *OpsDashboardHandler) SendTestEmail(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	if h == nil || h.Config == nil || !h.Config.Email.Enabled() {
		utils.RespondError(w, http.StatusServiceUnavailable, "email is disabled")
		return
	}
	var req struct {
		To string `json:"to"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024)).Decode(&req)
	to := strings.TrimSpace(req.To)
	if to == "" {
		utils.RespondError(w, http.StatusBadRequest, "missing recipient")
		return
	}

	subj := "RailPush test email"
	text := "This is a test email from RailPush.\n\nIf you received this, your SMTP configuration is working."
	id, err := models.EnqueueEmail("", "ops_test", to, subj, text, "")
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to enqueue email")
		return
	}
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "id": id})
}
