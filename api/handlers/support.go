package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"
)

type SupportHandler struct {
	Config *config.Config
}

func NewSupportHandler(cfg *config.Config) *SupportHandler {
	return &SupportHandler{Config: cfg}
}

func (h *SupportHandler) ListTickets(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
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

	tickets, err := models.ListSupportTicketsForUser(userID, limit, offset)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list tickets")
		return
	}
	utils.RespondJSON(w, http.StatusOK, tickets)
}

func (h *SupportHandler) CreateTicket(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	var req struct {
		Subject    string `json:"subject"`
		Message    string `json:"message"`
		WorkspaceID string `json:"workspace_id"`
		Priority   string `json:"priority"`
		Category   string `json:"category"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	subject := strings.TrimSpace(req.Subject)
	msgBody := strings.TrimSpace(req.Message)
	if subject == "" || msgBody == "" {
		utils.RespondError(w, http.StatusBadRequest, "subject and message are required")
		return
	}

	workspaceID := strings.TrimSpace(req.WorkspaceID)
	// Default to the user's primary workspace (owner) if not provided.
	if workspaceID == "" {
		if ws, err := models.GetWorkspaceByOwner(userID); err == nil && ws != nil {
			workspaceID = ws.ID
		}
	}

	t := &models.SupportTicket{
		WorkspaceID: workspaceID,
		CreatedBy:   userID,
		Subject:     subject,
		Category:    models.NormalizeSupportTicketCategory(strings.ToLower(strings.TrimSpace(req.Category))),
		Status:      "open",
		Priority:    strings.ToLower(strings.TrimSpace(req.Priority)),
	}
	if err := models.CreateSupportTicket(t); err != nil {
		log.Printf("support: create ticket failed user=%s workspace=%s err=%v", userID, workspaceID, err)
		utils.RespondError(w, http.StatusInternalServerError, "failed to create ticket")
		return
	}

	m := &models.SupportTicketMessage{
		TicketID:   t.ID,
		AuthorID:   userID,
		Body:       msgBody,
		IsInternal: false,
	}
	if err := models.CreateSupportTicketMessage(m); err != nil {
		log.Printf("support: create ticket message failed user=%s ticket=%s err=%v", userID, t.ID, err)
		utils.RespondError(w, http.StatusInternalServerError, "failed to create ticket message")
		return
	}
	_ = models.TouchSupportTicketCustomerReply(t.ID)

	utils.RespondJSON(w, http.StatusCreated, map[string]interface{}{
		"ticket":   t,
		"messages": []models.SupportTicketMessage{*m},
	})
}

func (h *SupportHandler) GetTicket(w http.ResponseWriter, r *http.Request) {
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
	if t.CreatedBy != userID {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	msgs, err := models.ListSupportTicketMessages(t.ID, 200)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load messages")
		return
	}
	// Customers never see internal notes.
	out := make([]models.SupportTicketMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.IsInternal {
			continue
		}
		out = append(out, m)
	}
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"ticket":   t,
		"messages": out,
	})
}

func (h *SupportHandler) CreateMessage(w http.ResponseWriter, r *http.Request) {
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
	if t.CreatedBy != userID {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Message string `json:"message"`
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
		IsInternal: false,
	}
	if err := models.CreateSupportTicketMessage(m); err != nil {
		log.Printf("support: create message failed user=%s ticket=%s err=%v", userID, t.ID, err)
		utils.RespondError(w, http.StatusInternalServerError, "failed to add message")
		return
	}
	_ = models.TouchSupportTicketCustomerReply(t.ID)
	utils.RespondJSON(w, http.StatusCreated, m)
}
