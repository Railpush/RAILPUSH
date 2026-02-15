package handlers

import (
	"encoding/json"
	"database/sql"
	"net/http"
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

type OpsTicketsHandler struct {
	Config *config.Config
}

func NewOpsTicketsHandler(cfg *config.Config) *OpsTicketsHandler {
	return &OpsTicketsHandler{Config: cfg}
}

func (h *OpsTicketsHandler) ensureOps(w http.ResponseWriter, r *http.Request) bool {
	userID := middleware.GetUserID(r)
	if err := services.EnsureOpsAccess(userID); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

type opsTicketItem struct {
	ID                 string     `json:"id"`
	WorkspaceID         string     `json:"workspace_id"`
	WorkspaceName       string     `json:"workspace_name"`
	CreatedBy           string     `json:"created_by"`
	CreatedByEmail      string     `json:"created_by_email"`
	CreatedByUsername   string     `json:"created_by_username"`
	Subject             string     `json:"subject"`
	Status              string     `json:"status"`
	Priority            string     `json:"priority"`
	AssignedTo          string     `json:"assigned_to"`
	LastCustomerReplyAt *time.Time `json:"last_customer_reply_at,omitempty"`
	LastOpsReplyAt      *time.Time `json:"last_ops_reply_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func (h *OpsTicketsHandler) ListTickets(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}

	limit := utils.GetQueryInt(r, "limit", 50)
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	offset := utils.GetQueryInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}

	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	like := "%" + q + "%"

	rows, err := database.DB.Query(
		`SELECT t.id::text, COALESCE(t.workspace_id::text,''), COALESCE(w.name,''), COALESCE(t.created_by::text,''),
		        COALESCE(u.email,''), COALESCE(u.username,''), COALESCE(t.subject,''),
		        COALESCE(t.status,'open'), COALESCE(t.priority,'normal'), COALESCE(t.assigned_to::text,''),
		        t.last_customer_reply_at, t.last_ops_reply_at, t.created_at, t.updated_at
		   FROM support_tickets t
		   LEFT JOIN workspaces w ON w.id = t.workspace_id
		   LEFT JOIN users u ON u.id = t.created_by
		  WHERE ($1 = '' OR COALESCE(t.status,'') = $1)
		    AND ($2 = '' OR COALESCE(t.subject,'') ILIKE $3 OR COALESCE(u.email,'') ILIKE $3 OR COALESCE(w.name,'') ILIKE $3)
		  ORDER BY t.updated_at DESC, t.created_at DESC
		  LIMIT $4 OFFSET $5`,
		status, q, like, limit, offset,
	)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list tickets")
		return
	}
	defer rows.Close()

	var out []opsTicketItem
	for rows.Next() {
		var it opsTicketItem
		var lastCust, lastOps sql.NullTime
		if err := rows.Scan(
			&it.ID, &it.WorkspaceID, &it.WorkspaceName, &it.CreatedBy,
			&it.CreatedByEmail, &it.CreatedByUsername, &it.Subject,
			&it.Status, &it.Priority, &it.AssignedTo,
			&lastCust, &lastOps, &it.CreatedAt, &it.UpdatedAt,
		); err != nil {
			continue
		}
		if lastCust.Valid {
			v := lastCust.Time
			it.LastCustomerReplyAt = &v
		}
		if lastOps.Valid {
			v := lastOps.Time
			it.LastOpsReplyAt = &v
		}
		out = append(out, it)
	}
	if out == nil {
		out = []opsTicketItem{}
	}
	utils.RespondJSON(w, http.StatusOK, out)
}

func (h *OpsTicketsHandler) GetTicket(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	t, err := models.GetSupportTicket(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load ticket")
		return
	}
	if t == nil {
		utils.RespondError(w, http.StatusNotFound, "ticket not found")
		return
	}

	msgs, err := models.ListSupportTicketMessages(t.ID, 500)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load messages")
		return
	}

	// Enrich creator identity for ops UI.
	creatorEmail := ""
	creatorUsername := ""
	if strings.TrimSpace(t.CreatedBy) != "" {
		if u, _ := models.GetUserByID(t.CreatedBy); u != nil {
			creatorEmail = u.Email
			creatorUsername = u.Username
		}
	}

	wsName := ""
	if strings.TrimSpace(t.WorkspaceID) != "" {
		if ws, _ := models.GetWorkspace(t.WorkspaceID); ws != nil {
			wsName = ws.Name
		}
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"ticket": map[string]interface{}{
			"id":                   t.ID,
			"workspace_id":         t.WorkspaceID,
			"workspace_name":       wsName,
			"created_by":           t.CreatedBy,
			"created_by_email":     creatorEmail,
			"created_by_username":  creatorUsername,
			"subject":              t.Subject,
			"status":               t.Status,
			"priority":             t.Priority,
			"assigned_to":          t.AssignedTo,
			"last_customer_reply_at": t.LastCustomerReplyAt,
			"last_ops_reply_at":      t.LastOpsReplyAt,
			"created_at":           t.CreatedAt,
			"updated_at":           t.UpdatedAt,
		},
		"messages": msgs,
	})
}

func (h *OpsTicketsHandler) UpdateTicket(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	var req struct {
		Status     string `json:"status"`
		Priority   string `json:"priority"`
		AssignedTo string `json:"assigned_to"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := models.UpdateSupportTicketOpsFields(id, strings.ToLower(strings.TrimSpace(req.Status)), strings.ToLower(strings.TrimSpace(req.Priority)), strings.TrimSpace(req.AssignedTo)); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update ticket")
		return
	}
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *OpsTicketsHandler) CreateMessage(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	userID := middleware.GetUserID(r)
	id := strings.TrimSpace(mux.Vars(r)["id"])
	t, err := models.GetSupportTicket(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load ticket")
		return
	}
	if t == nil {
		utils.RespondError(w, http.StatusNotFound, "ticket not found")
		return
	}

	var req struct {
		Message    string `json:"message"`
		IsInternal bool   `json:"is_internal"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body := strings.TrimSpace(req.Message)
	if body == "" {
		utils.RespondError(w, http.StatusBadRequest, "message is required")
		return
	}

	m := &models.SupportTicketMessage{
		TicketID:   t.ID,
		AuthorID:   userID,
		Body:       body,
		IsInternal: req.IsInternal,
	}
	if err := models.CreateSupportTicketMessage(m); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to add message")
		return
	}
	_ = models.TouchSupportTicketOpsReply(t.ID)

	utils.RespondJSON(w, http.StatusCreated, m)
}
