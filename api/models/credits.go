package models

import (
	"strings"
	"time"

	"github.com/railpush/api/database"
)

type WorkspaceCreditLedgerEntry struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	AmountCents int       `json:"amount_cents"`
	Reason      string    `json:"reason"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

func CreateWorkspaceCreditEntry(e *WorkspaceCreditLedgerEntry) error {
	return database.DB.QueryRow(
		`INSERT INTO workspace_credit_ledger (workspace_id, amount_cents, reason, created_by)
		 VALUES ($1, $2, COALESCE(NULLIF($3,''), ''), NULLIF($4,'')::uuid)
		 RETURNING id, created_at`,
		e.WorkspaceID, e.AmountCents, e.Reason, e.CreatedBy,
	).Scan(&e.ID, &e.CreatedAt)
}

func ListWorkspaceCreditLedger(workspaceID string, limit int) ([]WorkspaceCreditLedgerEntry, error) {
	rows, err := database.DB.Query(
		`SELECT id::text, COALESCE(workspace_id::text,''), COALESCE(amount_cents,0), COALESCE(reason,''), COALESCE(created_by::text,''), created_at
		   FROM workspace_credit_ledger
		  WHERE workspace_id=$1
		  ORDER BY created_at DESC
		  LIMIT $2`,
		workspaceID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceCreditLedgerEntry
	for rows.Next() {
		var e WorkspaceCreditLedgerEntry
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.AmountCents, &e.Reason, &e.CreatedBy, &e.CreatedAt); err != nil {
			continue
		}
		out = append(out, e)
	}
	if out == nil {
		out = []WorkspaceCreditLedgerEntry{}
	}
	return out, nil
}

func GetWorkspaceCreditBalanceCents(workspaceID string) (int64, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return 0, nil
	}
	var balance int64
	if err := database.DB.QueryRow(
		"SELECT COALESCE(SUM(amount_cents),0) FROM workspace_credit_ledger WHERE workspace_id=$1",
		workspaceID,
	).Scan(&balance); err != nil {
		return 0, err
	}
	return balance, nil
}
