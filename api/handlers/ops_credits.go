package handlers

import (
	"encoding/json"
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

type OpsCreditsHandler struct {
	Config *config.Config
}

func NewOpsCreditsHandler(cfg *config.Config) *OpsCreditsHandler {
	return &OpsCreditsHandler{Config: cfg}
}

func (h *OpsCreditsHandler) ensureOps(w http.ResponseWriter, r *http.Request) bool {
	userID := middleware.GetUserID(r)
	if err := services.EnsureOpsAccess(userID); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

type opsWorkspaceCreditItem struct {
	WorkspaceID   string    `json:"workspace_id"`
	WorkspaceName string    `json:"workspace_name"`
	OwnerEmail    string    `json:"owner_email"`
	BalanceCents  int64     `json:"balance_cents"`
	CreatedAt     time.Time `json:"created_at"`
}

func (h *OpsCreditsHandler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
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
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	like := "%" + q + "%"

	rows, err := database.DB.Query(
		`SELECT w.id::text, COALESCE(w.name,''), COALESCE(u.email,''), COALESCE(SUM(l.amount_cents),0) AS balance_cents, w.created_at
		   FROM workspaces w
		   LEFT JOIN users u ON u.id = w.owner_id
		   LEFT JOIN workspace_credit_ledger l ON l.workspace_id = w.id
		  WHERE ($1 = '' OR COALESCE(w.name,'') ILIKE $2 OR COALESCE(u.email,'') ILIKE $2)
		  GROUP BY w.id, w.name, u.email, w.created_at
		  ORDER BY balance_cents DESC, w.created_at DESC
		  LIMIT $3 OFFSET $4`,
		q, like, limit, offset,
	)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list credits")
		return
	}
	defer rows.Close()

	var out []opsWorkspaceCreditItem
	for rows.Next() {
		var it opsWorkspaceCreditItem
		if err := rows.Scan(&it.WorkspaceID, &it.WorkspaceName, &it.OwnerEmail, &it.BalanceCents, &it.CreatedAt); err != nil {
			continue
		}
		out = append(out, it)
	}
	if out == nil {
		out = []opsWorkspaceCreditItem{}
	}
	utils.RespondJSON(w, http.StatusOK, out)
}

func (h *OpsCreditsHandler) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	workspaceID := strings.TrimSpace(mux.Vars(r)["id"])
	var balance int64
	_ = database.DB.QueryRow("SELECT COALESCE(SUM(amount_cents),0) FROM workspace_credit_ledger WHERE workspace_id=$1", workspaceID).Scan(&balance)
	ledger, err := models.ListWorkspaceCreditLedger(workspaceID, 200)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load credit ledger")
		return
	}
	wsName := ""
	if ws, _ := models.GetWorkspace(workspaceID); ws != nil {
		wsName = ws.Name
	}
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"workspace_id":   workspaceID,
		"workspace_name": wsName,
		"balance_cents":  balance,
		"ledger":         ledger,
	})
}

func (h *OpsCreditsHandler) Grant(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	userID := middleware.GetUserID(r)
	workspaceID := strings.TrimSpace(mux.Vars(r)["id"])

	var req struct {
		AmountCents int    `json:"amount_cents"`
		Reason      string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AmountCents == 0 {
		utils.RespondError(w, http.StatusBadRequest, "amount_cents is required")
		return
	}

	entry := &models.WorkspaceCreditLedgerEntry{
		WorkspaceID: workspaceID,
		AmountCents: req.AmountCents,
		Reason:      strings.TrimSpace(req.Reason),
		CreatedBy:   userID,
	}
	if err := models.CreateWorkspaceCreditEntry(entry); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to grant credit")
		return
	}
	var balance int64
	_ = database.DB.QueryRow("SELECT COALESCE(SUM(amount_cents),0) FROM workspace_credit_ledger WHERE workspace_id=$1", workspaceID).Scan(&balance)
	utils.RespondJSON(w, http.StatusCreated, map[string]interface{}{
		"status":       "ok",
		"entry":        entry,
		"balance_cents": balance,
	})
}

