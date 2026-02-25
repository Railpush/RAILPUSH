package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
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
	Category            string     `json:"category"`
	Status              string     `json:"status"`
	Priority            string     `json:"priority"`
	AssignedTo          string     `json:"assigned_to"`
	LastCustomerReplyAt *time.Time `json:"last_customer_reply_at,omitempty"`
	LastOpsReplyAt      *time.Time `json:"last_ops_reply_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type opsTicketFacets struct {
	ByStatus   map[string]int64 `json:"by_status"`
	ByPriority map[string]int64 `json:"by_priority"`
	ByCategory map[string]int64 `json:"by_category"`
}

type opsTicketListResponse struct {
	Tickets []opsTicketItem `json:"tickets"`
	Total   int64           `json:"total"`
	Facets  opsTicketFacets `json:"facets"`
}

const opsTicketsWhereClause = `
	FROM support_tickets t
	LEFT JOIN workspaces w ON w.id = t.workspace_id
	LEFT JOIN users u ON u.id = t.created_by
	WHERE ($1 = '' OR COALESCE(t.status,'') = $1)
	  AND ($2 = '' OR COALESCE(t.subject,'') ILIKE $3 OR COALESCE(u.email,'') ILIKE $3 OR COALESCE(w.name,'') ILIKE $3)
	  AND ($4 = '' OR COALESCE(t.category,'support') = $4)
	  AND ($5 = '' OR COALESCE(t.priority,'normal') = $5)
	  AND ($6::timestamptz IS NULL OR t.created_at >= $6::timestamptz)
	  AND ($7::timestamptz IS NULL OR t.created_at <= $7::timestamptz)`

func normalizeSupportTicketStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "open", "pending", "solved", "closed":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func normalizeSupportTicketPriority(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "low", "normal", "high", "urgent":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func normalizeDateFilter(raw string, endOfDay bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		if endOfDay {
			t = t.Add(24*time.Hour - time.Nanosecond)
		}
		return t.UTC().Format(time.RFC3339), nil
	}
	return "", fmt.Errorf("invalid date format")
}

func buildOpsTicketOrderClause(sortBy, sortOrder string) (string, error) {
	order := "DESC"
	switch strings.ToLower(strings.TrimSpace(sortOrder)) {
	case "", "desc":
		order = "DESC"
	case "asc":
		order = "ASC"
	default:
		return "", fmt.Errorf("invalid sort_order")
	}

	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "", "updated_at":
		return "t.updated_at " + order + ", t.created_at DESC", nil
	case "created_at":
		return "t.created_at " + order + ", t.updated_at DESC", nil
	case "priority":
		return "CASE COALESCE(t.priority,'normal') WHEN 'urgent' THEN 4 WHEN 'high' THEN 3 WHEN 'normal' THEN 2 ELSE 1 END " + order + ", t.updated_at DESC", nil
	default:
		return "", fmt.Errorf("invalid sort_by")
	}
}

func listFacetCounts(expr string, args []interface{}) (map[string]int64, error) {
	rows, err := database.DB.Query(
		"SELECT "+expr+" AS value, COUNT(*) "+opsTicketsWhereClause+" GROUP BY value",
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			continue
		}
		out[key] = count
	}
	return out, nil
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

	statusInput := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	status := ""
	if statusInput != "" {
		status = normalizeSupportTicketStatus(statusInput)
		if status == "" {
			utils.RespondError(w, http.StatusBadRequest, "invalid status filter")
			return
		}
	}

	categoryInput := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("category")))
	category := ""
	if categoryInput != "" {
		category = models.NormalizeSupportTicketCategory(categoryInput)
		if category != categoryInput {
			utils.RespondError(w, http.StatusBadRequest, "invalid category filter")
			return
		}
	}

	priorityInput := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("priority")))
	priority := ""
	if priorityInput != "" {
		priority = normalizeSupportTicketPriority(priorityInput)
		if priority == "" {
			utils.RespondError(w, http.StatusBadRequest, "invalid priority filter")
			return
		}
	}

	q := strings.TrimSpace(r.URL.Query().Get("query"))
	like := "%" + q + "%"
	includeMeta := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_meta")), "true") || strings.TrimSpace(r.URL.Query().Get("include_meta")) == "1"

	createdAfter, err := normalizeDateFilter(r.URL.Query().Get("created_after"), false)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid created_after")
		return
	}
	createdBefore, err := normalizeDateFilter(r.URL.Query().Get("created_before"), true)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid created_before")
		return
	}

	orderClause, err := buildOpsTicketOrderClause(r.URL.Query().Get("sort_by"), r.URL.Query().Get("sort_order"))
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	var createdAfterArg interface{}
	if createdAfter != "" {
		createdAfterArg = createdAfter
	}
	var createdBeforeArg interface{}
	if createdBefore != "" {
		createdBeforeArg = createdBefore
	}

	filterArgs := []interface{}{status, q, like, category, priority, createdAfterArg, createdBeforeArg}
	listArgs := append(filterArgs, limit, offset)

	rows, err := database.DB.Query(
		`SELECT t.id::text, COALESCE(t.workspace_id::text,''), COALESCE(w.name,''), COALESCE(t.created_by::text,''),
		        COALESCE(u.email,''), COALESCE(u.username,''), COALESCE(t.subject,''),
		        COALESCE(t.category,'support'), COALESCE(t.status,'open'), COALESCE(t.priority,'normal'), COALESCE(t.assigned_to::text,''),
		        t.last_customer_reply_at, t.last_ops_reply_at, t.created_at, t.updated_at
		   `+opsTicketsWhereClause+`
		  ORDER BY `+orderClause+`
		  LIMIT $8 OFFSET $9`,
		listArgs...,
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
			&it.Category, &it.Status, &it.Priority, &it.AssignedTo,
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

	if !includeMeta {
		utils.RespondJSON(w, http.StatusOK, out)
		return
	}

	var total int64
	if err := database.DB.QueryRow("SELECT COUNT(*) "+opsTicketsWhereClause, filterArgs...).Scan(&total); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to count tickets")
		return
	}

	byStatus, err := listFacetCounts("COALESCE(t.status,'open')", filterArgs)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to aggregate ticket status facets")
		return
	}
	byPriority, err := listFacetCounts("COALESCE(t.priority,'normal')", filterArgs)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to aggregate ticket priority facets")
		return
	}
	byCategory, err := listFacetCounts("COALESCE(t.category,'support')", filterArgs)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to aggregate ticket category facets")
		return
	}

	utils.RespondJSON(w, http.StatusOK, opsTicketListResponse{
		Tickets: out,
		Total:   total,
		Facets: opsTicketFacets{
			ByStatus:   byStatus,
			ByPriority: byPriority,
			ByCategory: byCategory,
		},
	})
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
			"category":             t.Category,
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
		Category   string `json:"category"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cat := strings.ToLower(strings.TrimSpace(req.Category))
	if cat != "" {
		cat = models.NormalizeSupportTicketCategory(cat)
	}

	if err := models.UpdateSupportTicketOpsFields(id, strings.ToLower(strings.TrimSpace(req.Status)), strings.ToLower(strings.TrimSpace(req.Priority)), strings.TrimSpace(req.AssignedTo), cat); err != nil {
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

func (h *OpsTicketsHandler) BulkUpdateTickets(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	userID := middleware.GetUserID(r)

	var req struct {
		TicketIDs []string `json:"ticket_ids"`
		Status    string   `json:"status"`
		Priority  string   `json:"priority"`
		Category  string   `json:"category"`
		Reason    string   `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.TicketIDs) == 0 {
		utils.RespondError(w, http.StatusBadRequest, "ticket_ids is required")
		return
	}
	if len(req.TicketIDs) > 200 {
		utils.RespondError(w, http.StatusBadRequest, "ticket_ids limit exceeded (max 200)")
		return
	}

	ids := make([]string, 0, len(req.TicketIDs))
	seen := map[string]bool{}
	for _, rawID := range req.TicketIDs {
		id := strings.TrimSpace(rawID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		utils.RespondError(w, http.StatusBadRequest, "ticket_ids is required")
		return
	}

	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status != "" && normalizeSupportTicketStatus(status) == "" {
		utils.RespondError(w, http.StatusBadRequest, "invalid status")
		return
	}

	priority := strings.ToLower(strings.TrimSpace(req.Priority))
	if priority != "" && normalizeSupportTicketPriority(priority) == "" {
		utils.RespondError(w, http.StatusBadRequest, "invalid priority")
		return
	}

	category := strings.ToLower(strings.TrimSpace(req.Category))
	if category != "" {
		normalized := models.NormalizeSupportTicketCategory(category)
		if normalized != category {
			utils.RespondError(w, http.StatusBadRequest, "invalid category")
			return
		}
		category = normalized
	}

	if status == "" && priority == "" && category == "" {
		utils.RespondError(w, http.StatusBadRequest, "at least one of status, priority, or category is required")
		return
	}

	updated, err := models.BulkUpdateSupportTicketOpsFields(ids, status, priority, category)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to bulk update tickets")
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if reason != "" {
		for _, ticketID := range ids {
			ticket, err := models.GetSupportTicket(ticketID)
			if err != nil || ticket == nil {
				continue
			}
			m := &models.SupportTicketMessage{
				TicketID:   ticketID,
				AuthorID:   userID,
				Body:       reason,
				IsInternal: false,
			}
			if err := models.CreateSupportTicketMessage(m); err == nil {
				_ = models.TouchSupportTicketOpsReply(ticketID)
			}
		}
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"updated": updated,
	})
}
