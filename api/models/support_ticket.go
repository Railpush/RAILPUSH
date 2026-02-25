package models

import (
	"database/sql"
	"regexp"
	"strings"
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
	Component           string     `json:"component"`
	Tags                []string   `json:"tags,omitempty"`
	Status              string     `json:"status"`
	Priority            string     `json:"priority"`
	AssignedTo          string     `json:"assigned_to"`
	LastCustomerReplyAt *time.Time `json:"last_customer_reply_at,omitempty"`
	LastOpsReplyAt      *time.Time `json:"last_ops_reply_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

var supportTicketTagCleaner = regexp.MustCompile(`[^a-z0-9-]+`)
var supportTicketTagDashes = regexp.MustCompile(`-+`)

// NormalizeSupportTicketCategory validates and returns a canonical category value.
// Returns an empty string when the input is invalid.
func NormalizeSupportTicketCategory(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "support":
		return "support"
	case "feature", "feature_request", "feature-request":
		return "feature_request"
	case "bug", "bug_report", "bug-report":
		return "bug"
	case "security":
		return "security"
	case "billing":
		return "billing"
	case "how_to", "how-to", "howto":
		return "how_to"
	case "incident":
		return "incident"
	case "feedback":
		return "feedback"
	default:
		return ""
	}
}

func NormalizeSupportTicketComponent(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return ""
	case "services", "service":
		return "services"
	case "databases", "database":
		return "databases"
	case "key-value", "key_value", "keyvalue", "redis":
		return "key-value"
	case "deployments", "deployment", "deploy":
		return "deployments"
	case "env-vars", "env_vars", "envvars":
		return "env-vars"
	case "domains", "domain", "dns":
		return "domains"
	case "mcp-api", "mcp_api", "mcp", "api":
		return "mcp-api"
	case "billing":
		return "billing"
	case "auth", "authentication":
		return "auth"
	case "builds", "build":
		return "builds"
	case "dashboard", "ui":
		return "dashboard"
	default:
		return ""
	}
}

func NormalizeSupportTicketTags(raw []string) []string {
	if len(raw) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		t := strings.ToLower(strings.TrimSpace(item))
		if t == "" {
			continue
		}
		t = strings.ReplaceAll(t, "_", "-")
		t = strings.ReplaceAll(t, " ", "-")
		t = supportTicketTagCleaner.ReplaceAllString(t, "-")
		t = supportTicketTagDashes.ReplaceAllString(t, "-")
		t = strings.Trim(t, "-")
		if t == "" || seen[t] {
			continue
		}
		if len(t) > 48 {
			t = t[:48]
		}
		seen[t] = true
		out = append(out, t)
		if len(out) >= 24 {
			break
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
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
	if t.Component == "" {
		t.Component = ""
	}
	t.Tags = NormalizeSupportTicketTags(t.Tags)
	return database.DB.QueryRow(
		`INSERT INTO support_tickets (workspace_id, created_by, subject, category, component, tags, status, priority, assigned_to)
		 VALUES (NULLIF($1,'')::uuid, $2, $3, COALESCE(NULLIF($4,''), 'support'), COALESCE(NULLIF($5,''), ''), COALESCE($6, '{}'::text[]), COALESCE(NULLIF($7,''), 'open'), COALESCE(NULLIF($8,''), 'normal'), NULLIF($9,'')::uuid)
		 RETURNING id, created_at, updated_at`,
		t.WorkspaceID, t.CreatedBy, t.Subject, t.Category, t.Component, pq.Array(t.Tags), t.Status, t.Priority, t.AssignedTo,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

func GetSupportTicket(id string) (*SupportTicket, error) {
	t := &SupportTicket{}
	var lastCust, lastOps sql.NullTime
	var tags []string
	err := database.DB.QueryRow(
		`SELECT id::text, COALESCE(workspace_id::text,''), COALESCE(created_by::text,''), COALESCE(subject,''),
		        COALESCE(category,'support'), COALESCE(component,''), COALESCE(tags, '{}'::text[]), COALESCE(status,'open'), COALESCE(priority,'normal'), COALESCE(assigned_to::text,''),
		        last_customer_reply_at, last_ops_reply_at, created_at, updated_at
		   FROM support_tickets
		  WHERE id=$1`,
		id,
	).Scan(&t.ID, &t.WorkspaceID, &t.CreatedBy, &t.Subject, &t.Category, &t.Component, pq.Array(&tags), &t.Status, &t.Priority, &t.AssignedTo, &lastCust, &lastOps, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Tags = tags
	if t.Tags == nil {
		t.Tags = []string{}
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

type SupportTicketListFilters struct {
	Status    string
	Category  string
	Component string
	Query     string
	Tags      []string
}

func ListSupportTicketsForUser(userID string, limit, offset int, filters *SupportTicketListFilters) ([]SupportTicket, error) {
	status := ""
	category := ""
	component := ""
	q := ""
	var tagsArg interface{}
	if filters != nil {
		status = strings.TrimSpace(filters.Status)
		category = strings.TrimSpace(filters.Category)
		component = strings.TrimSpace(filters.Component)
		q = strings.TrimSpace(filters.Query)
		normalizedTags := NormalizeSupportTicketTags(filters.Tags)
		if len(normalizedTags) > 0 {
			tagsArg = pq.Array(normalizedTags)
		}
	}
	like := "%" + q + "%"

	rows, err := database.DB.Query(
		`SELECT id::text, COALESCE(workspace_id::text,''), COALESCE(created_by::text,''), COALESCE(subject,''),
		        COALESCE(category,'support'), COALESCE(component,''), COALESCE(tags, '{}'::text[]), COALESCE(status,'open'), COALESCE(priority,'normal'), COALESCE(assigned_to::text,''),
		        last_customer_reply_at, last_ops_reply_at, created_at, updated_at
		   FROM support_tickets
		  WHERE created_by=$1
		    AND ($2 = '' OR COALESCE(status,'') = $2)
		    AND ($3 = '' OR COALESCE(category,'support') = $3)
		    AND ($4 = '' OR COALESCE(component,'') = $4)
		    AND ($5 = '' OR COALESCE(subject,'') ILIKE $6)
		    AND ($7::text[] IS NULL OR COALESCE(tags, '{}'::text[]) @> $7::text[])
		  ORDER BY updated_at DESC
		  LIMIT $8 OFFSET $9`,
		userID, status, category, component, q, like, tagsArg, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SupportTicket
	for rows.Next() {
		var t SupportTicket
		var lastCust, lastOps sql.NullTime
		var tags []string
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.CreatedBy, &t.Subject, &t.Category, &t.Component, pq.Array(&tags), &t.Status, &t.Priority, &t.AssignedTo, &lastCust, &lastOps, &t.CreatedAt, &t.UpdatedAt); err != nil {
			continue
		}
		t.Tags = tags
		if t.Tags == nil {
			t.Tags = []string{}
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

func UpdateSupportTicketOpsFields(ticketID, status, priority string, assignedTo *string, category string, component *string, tags *[]string) error {
	var assignedArg interface{}
	if assignedTo != nil {
		assignedArg = strings.TrimSpace(*assignedTo)
	}
	var componentArg interface{}
	if component != nil {
		componentArg = strings.TrimSpace(*component)
	}
	var tagsArg interface{}
	if tags != nil {
		tagsArg = pq.Array(NormalizeSupportTicketTags(*tags))
	}

	_, err := database.DB.Exec(
		`UPDATE support_tickets
		    SET status = COALESCE(NULLIF($2,''), status),
		        priority = COALESCE(NULLIF($3,''), priority),
		        assigned_to = CASE WHEN $4::text IS NULL THEN assigned_to ELSE NULLIF($4,'')::uuid END,
		        category = COALESCE(NULLIF($5,''), category),
		        component = CASE WHEN $6::text IS NULL THEN component ELSE $6::text END,
		        tags = CASE WHEN $7::text[] IS NULL THEN tags ELSE $7::text[] END,
		        updated_at = NOW()
		  WHERE id=$1`,
		ticketID, status, priority, assignedArg, category, componentArg, tagsArg,
	)
	return err
}

func BulkUpdateSupportTicketOpsFields(ticketIDs []string, status, priority, category string, component *string, tags *[]string) (int64, error) {
	if len(ticketIDs) == 0 {
		return 0, nil
	}
	var componentArg interface{}
	if component != nil {
		componentArg = strings.TrimSpace(*component)
	}
	var tagsArg interface{}
	if tags != nil {
		tagsArg = pq.Array(NormalizeSupportTicketTags(*tags))
	}
	res, err := database.DB.Exec(
		`UPDATE support_tickets
		    SET status = COALESCE(NULLIF($2,''), status),
		        priority = COALESCE(NULLIF($3,''), priority),
		        category = COALESCE(NULLIF($4,''), category),
		        component = CASE WHEN $5::text IS NULL THEN component ELSE $5::text END,
		        tags = CASE WHEN $6::text[] IS NULL THEN tags ELSE $6::text[] END,
		        updated_at = NOW()
		  WHERE id = ANY($1::uuid[])`,
		pq.Array(ticketIDs), status, priority, category, componentArg, tagsArg,
	)
	if err != nil {
		return 0, err
	}
	updated, _ := res.RowsAffected()
	return updated, nil
}

func UpdateSupportTicketTags(ticketID string, tags []string) error {
	_, err := database.DB.Exec(
		`UPDATE support_tickets
		    SET tags = $2::text[],
		        updated_at = NOW()
		  WHERE id = $1`,
		ticketID, pq.Array(NormalizeSupportTicketTags(tags)),
	)
	return err
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
