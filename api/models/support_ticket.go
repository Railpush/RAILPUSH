package models

import (
	"database/sql"
	"time"

	"github.com/lib/pq"
	"github.com/railpush/api/database"
)

type SupportTicket struct {
	ID                 string     `json:"id"`
	WorkspaceID         string     `json:"workspace_id"`
	CreatedBy           string     `json:"created_by"`
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

// NormalizeSupportTicketCategory validates and returns a canonical category value.
func NormalizeSupportTicketCategory(raw string) string {
	switch raw {
	case "support", "feature_request", "bug_report":
		return raw
	default:
		return "support"
	}
}

type SupportTicketMessage struct {
	ID         string    `json:"id"`
	TicketID   string    `json:"ticket_id"`
	AuthorID   string    `json:"author_id"`
	Body       string    `json:"body"`
	IsInternal bool      `json:"is_internal"`
	CreatedAt  time.Time `json:"created_at"`
}

func CreateSupportTicket(t *SupportTicket) error {
	if t.Category == "" {
		t.Category = "support"
	}
	return database.DB.QueryRow(
		`INSERT INTO support_tickets (workspace_id, created_by, subject, category, status, priority, assigned_to)
		 VALUES (NULLIF($1,'')::uuid, $2, $3, COALESCE(NULLIF($4,''), 'support'), COALESCE(NULLIF($5,''), 'open'), COALESCE(NULLIF($6,''), 'normal'), NULLIF($7,'')::uuid)
		 RETURNING id, created_at, updated_at`,
		t.WorkspaceID, t.CreatedBy, t.Subject, t.Category, t.Status, t.Priority, t.AssignedTo,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

func GetSupportTicket(id string) (*SupportTicket, error) {
	t := &SupportTicket{}
	var lastCust, lastOps sql.NullTime
	err := database.DB.QueryRow(
		`SELECT id::text, COALESCE(workspace_id::text,''), COALESCE(created_by::text,''), COALESCE(subject,''),
		        COALESCE(category,'support'), COALESCE(status,'open'), COALESCE(priority,'normal'), COALESCE(assigned_to::text,''),
		        last_customer_reply_at, last_ops_reply_at, created_at, updated_at
		   FROM support_tickets
		  WHERE id=$1`,
		id,
	).Scan(&t.ID, &t.WorkspaceID, &t.CreatedBy, &t.Subject, &t.Category, &t.Status, &t.Priority, &t.AssignedTo, &lastCust, &lastOps, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastCust.Valid {
		v := lastCust.Time
		t.LastCustomerReplyAt = &v
	}
	if lastOps.Valid {
		v := lastOps.Time
		t.LastOpsReplyAt = &v
	}
	return t, nil
}

func ListSupportTicketsForUser(userID string, limit, offset int) ([]SupportTicket, error) {
	rows, err := database.DB.Query(
		`SELECT id::text, COALESCE(workspace_id::text,''), COALESCE(created_by::text,''), COALESCE(subject,''),
		        COALESCE(category,'support'), COALESCE(status,'open'), COALESCE(priority,'normal'), COALESCE(assigned_to::text,''),
		        last_customer_reply_at, last_ops_reply_at, created_at, updated_at
		   FROM support_tickets
		  WHERE created_by=$1
		  ORDER BY updated_at DESC
		  LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SupportTicket
	for rows.Next() {
		var t SupportTicket
		var lastCust, lastOps sql.NullTime
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.CreatedBy, &t.Subject, &t.Category, &t.Status, &t.Priority, &t.AssignedTo, &lastCust, &lastOps, &t.CreatedAt, &t.UpdatedAt); err != nil {
			continue
		}
		if lastCust.Valid {
			v := lastCust.Time
			t.LastCustomerReplyAt = &v
		}
		if lastOps.Valid {
			v := lastOps.Time
			t.LastOpsReplyAt = &v
		}
		out = append(out, t)
	}
	if out == nil {
		out = []SupportTicket{}
	}
	return out, nil
}

func CreateSupportTicketMessage(m *SupportTicketMessage) error {
	return database.DB.QueryRow(
		`INSERT INTO support_ticket_messages (ticket_id, author_id, body, is_internal)
		 VALUES ($1, NULLIF($2,'')::uuid, $3, $4)
		 RETURNING id, created_at`,
		m.TicketID, m.AuthorID, m.Body, m.IsInternal,
	).Scan(&m.ID, &m.CreatedAt)
}

func ListSupportTicketMessages(ticketID string, limit int) ([]SupportTicketMessage, error) {
	rows, err := database.DB.Query(
		`SELECT id::text, COALESCE(ticket_id::text,''), COALESCE(author_id::text,''), COALESCE(body,''), COALESCE(is_internal,false), created_at
		   FROM support_ticket_messages
		  WHERE ticket_id=$1
		  ORDER BY created_at ASC
		  LIMIT $2`,
		ticketID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SupportTicketMessage
	for rows.Next() {
		var m SupportTicketMessage
		if err := rows.Scan(&m.ID, &m.TicketID, &m.AuthorID, &m.Body, &m.IsInternal, &m.CreatedAt); err != nil {
			continue
		}
		out = append(out, m)
	}
	if out == nil {
		out = []SupportTicketMessage{}
	}
	return out, nil
}

func UpdateSupportTicketOpsFields(ticketID, status, priority, assignedTo, category string) error {
	_, err := database.DB.Exec(
		`UPDATE support_tickets
		    SET status = COALESCE(NULLIF($2,''), status),
		        priority = COALESCE(NULLIF($3,''), priority),
		        assigned_to = NULLIF($4,'')::uuid,
		        category = COALESCE(NULLIF($5,''), category),
		        updated_at = NOW()
		  WHERE id=$1`,
		ticketID, status, priority, assignedTo, category,
	)
	return err
}

func BulkUpdateSupportTicketOpsFields(ticketIDs []string, status, priority, category string) (int64, error) {
	if len(ticketIDs) == 0 {
		return 0, nil
	}
	res, err := database.DB.Exec(
		`UPDATE support_tickets
		    SET status = COALESCE(NULLIF($2,''), status),
		        priority = COALESCE(NULLIF($3,''), priority),
		        category = COALESCE(NULLIF($4,''), category),
		        updated_at = NOW()
		  WHERE id = ANY($1::uuid[])`,
		pq.Array(ticketIDs), status, priority, category,
	)
	if err != nil {
		return 0, err
	}
	updated, _ := res.RowsAffected()
	return updated, nil
}

func TouchSupportTicketCustomerReply(ticketID string) error {
	_, err := database.DB.Exec(
		`UPDATE support_tickets
		    SET last_customer_reply_at = NOW(),
		        updated_at = NOW()
		  WHERE id=$1`,
		ticketID,
	)
	return err
}

func TouchSupportTicketOpsReply(ticketID string) error {
	_, err := database.DB.Exec(
		`UPDATE support_tickets
		    SET last_ops_reply_at = NOW(),
		        updated_at = NOW()
		  WHERE id=$1`,
		ticketID,
	)
	return err
}
