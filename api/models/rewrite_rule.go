package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

// RewriteRule represents a path-based proxy or redirect rule on a service.
// For example, a static frontend can proxy /api/* to a backend service.
type RewriteRule struct {
	ID            string    `json:"id"`
	ServiceID     string    `json:"service_id"`
	SourcePath    string    `json:"source_path"`
	DestServiceID string    `json:"dest_service_id"`
	DestPath      string    `json:"dest_path"`
	RuleType      string    `json:"rule_type"` // "proxy" or "redirect"
	Priority      int       `json:"priority"`
	CreatedAt     time.Time `json:"created_at"`

	// Joined fields (populated by list queries, not stored directly).
	DestServiceName string `json:"dest_service_name,omitempty"`
}

func CreateRewriteRule(r *RewriteRule) error {
	if r.DestPath == "" {
		r.DestPath = "/"
	}
	if r.RuleType == "" {
		r.RuleType = "proxy"
	}
	return database.DB.QueryRow(
		`INSERT INTO rewrite_rules (service_id, source_path, dest_service_id, dest_path, rule_type, priority)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		r.ServiceID, r.SourcePath, r.DestServiceID, r.DestPath, r.RuleType, r.Priority,
	).Scan(&r.ID, &r.CreatedAt)
}

func ListRewriteRules(serviceID string) ([]RewriteRule, error) {
	rows, err := database.DB.Query(
		`SELECT r.id, r.service_id, r.source_path, r.dest_service_id, r.dest_path,
		        r.rule_type, r.priority, r.created_at, COALESCE(s.name, '')
		 FROM rewrite_rules r
		 LEFT JOIN services s ON s.id = r.dest_service_id
		 WHERE r.service_id = $1
		 ORDER BY r.priority DESC, r.source_path`,
		serviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []RewriteRule
	for rows.Next() {
		var r RewriteRule
		if err := rows.Scan(&r.ID, &r.ServiceID, &r.SourcePath, &r.DestServiceID, &r.DestPath,
			&r.RuleType, &r.Priority, &r.CreatedAt, &r.DestServiceName); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, nil
}

func GetRewriteRule(id string) (*RewriteRule, error) {
	r := &RewriteRule{}
	err := database.DB.QueryRow(
		`SELECT r.id, r.service_id, r.source_path, r.dest_service_id, r.dest_path,
		        r.rule_type, r.priority, r.created_at, COALESCE(s.name, '')
		 FROM rewrite_rules r
		 LEFT JOIN services s ON s.id = r.dest_service_id
		 WHERE r.id = $1`,
		id,
	).Scan(&r.ID, &r.ServiceID, &r.SourcePath, &r.DestServiceID, &r.DestPath,
		&r.RuleType, &r.Priority, &r.CreatedAt, &r.DestServiceName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func DeleteRewriteRule(id string) error {
	_, err := database.DB.Exec("DELETE FROM rewrite_rules WHERE id = $1", id)
	return err
}
