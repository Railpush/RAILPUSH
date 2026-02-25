package models

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/railpush/api/database"
)

type ServiceLogAlert struct {
	ID              string     `json:"id"`
	ServiceID       string     `json:"service_id"`
	WorkspaceID     string     `json:"workspace_id"`
	Name            string     `json:"name"`
	Enabled         bool       `json:"enabled"`
	FilterQuery     string     `json:"filter_query"`
	Pattern         string     `json:"pattern"`
	Threshold       int        `json:"threshold"`
	WindowSeconds   int        `json:"window_seconds"`
	Comparison      string     `json:"comparison"`
	CooldownSeconds int        `json:"cooldown_seconds"`
	Channels        []string   `json:"channels"`
	WebhookURL      string     `json:"webhook_url,omitempty"`
	Priority        string     `json:"priority"`
	Status          string     `json:"status"`
	LastMatchCount  int        `json:"last_match_count"`
	LastEvaluatedAt *time.Time `json:"last_evaluated_at,omitempty"`
	LastTriggeredAt *time.Time `json:"last_triggered_at,omitempty"`
	LastResolvedAt  *time.Time `json:"last_resolved_at,omitempty"`
	CreatedBy       *string    `json:"created_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func scanServiceLogAlert(scanner interface{ Scan(dest ...interface{}) error }) (*ServiceLogAlert, error) {
	var a ServiceLogAlert
	var channels []string
	var lastEval sql.NullTime
	var lastTrig sql.NullTime
	var lastRes sql.NullTime
	var createdBy sql.NullString

	err := scanner.Scan(
		&a.ID,
		&a.ServiceID,
		&a.WorkspaceID,
		&a.Name,
		&a.Enabled,
		&a.FilterQuery,
		&a.Pattern,
		&a.Threshold,
		&a.WindowSeconds,
		&a.Comparison,
		&a.CooldownSeconds,
		&channels,
		&a.WebhookURL,
		&a.Priority,
		&a.Status,
		&a.LastMatchCount,
		&lastEval,
		&lastTrig,
		&lastRes,
		&createdBy,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	a.Channels = normalizeLogAlertChannels(channels)
	if lastEval.Valid {
		t := lastEval.Time
		a.LastEvaluatedAt = &t
	}
	if lastTrig.Valid {
		t := lastTrig.Time
		a.LastTriggeredAt = &t
	}
	if lastRes.Valid {
		t := lastRes.Time
		a.LastResolvedAt = &t
	}
	if createdBy.Valid && strings.TrimSpace(createdBy.String) != "" {
		v := strings.TrimSpace(createdBy.String)
		a.CreatedBy = &v
	}
	return &a, nil
}

func normalizeLogAlertChannels(channels []string) []string {
	if len(channels) == 0 {
		return []string{"incident"}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(channels))
	for _, raw := range channels {
		ch := strings.ToLower(strings.TrimSpace(raw))
		switch ch {
		case "incident", "email", "webhook":
		default:
			continue
		}
		if _, ok := seen[ch]; ok {
			continue
		}
		seen[ch] = struct{}{}
		out = append(out, ch)
	}
	if len(out) == 0 {
		return []string{"incident"}
	}
	return out
}

func ListServiceLogAlerts(serviceID string) ([]ServiceLogAlert, error) {
	rows, err := database.DB.Query(
		`SELECT id, service_id, workspace_id, name, enabled, COALESCE(filter_query,''), COALESCE(pattern,''),
		        threshold, window_seconds, comparison, cooldown_seconds, COALESCE(channels, '{incident}'::text[]),
		        COALESCE(webhook_url,''), COALESCE(priority,'normal'), COALESCE(status,'ok'), COALESCE(last_match_count,0),
		        last_evaluated_at, last_triggered_at, last_resolved_at, created_by::text, created_at, updated_at
		   FROM service_log_alerts
		  WHERE service_id=$1
		  ORDER BY created_at DESC`,
		serviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ServiceLogAlert{}
	for rows.Next() {
		a, err := scanServiceLogAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func GetServiceLogAlert(id string) (*ServiceLogAlert, error) {
	row := database.DB.QueryRow(
		`SELECT id, service_id, workspace_id, name, enabled, COALESCE(filter_query,''), COALESCE(pattern,''),
		        threshold, window_seconds, comparison, cooldown_seconds, COALESCE(channels, '{incident}'::text[]),
		        COALESCE(webhook_url,''), COALESCE(priority,'normal'), COALESCE(status,'ok'), COALESCE(last_match_count,0),
		        last_evaluated_at, last_triggered_at, last_resolved_at, created_by::text, created_at, updated_at
		   FROM service_log_alerts
		  WHERE id=$1`,
		id,
	)
	a, err := scanServiceLogAlert(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func CreateServiceLogAlert(alert *ServiceLogAlert) error {
	if alert == nil {
		return fmt.Errorf("missing alert")
	}
	channels := normalizeLogAlertChannels(alert.Channels)
	return database.DB.QueryRow(
		`INSERT INTO service_log_alerts
			(service_id, workspace_id, name, enabled, filter_query, pattern, threshold, window_seconds, comparison,
			 cooldown_seconds, channels, webhook_url, priority, status, created_by)
		 VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::text[], $12, $13, 'ok', $14)
		 RETURNING id, created_at, updated_at`,
		alert.ServiceID,
		alert.WorkspaceID,
		strings.TrimSpace(alert.Name),
		alert.Enabled,
		strings.TrimSpace(alert.FilterQuery),
		strings.TrimSpace(alert.Pattern),
		alert.Threshold,
		alert.WindowSeconds,
		strings.TrimSpace(alert.Comparison),
		alert.CooldownSeconds,
		channels,
		strings.TrimSpace(alert.WebhookURL),
		strings.TrimSpace(alert.Priority),
		alert.CreatedBy,
	).Scan(&alert.ID, &alert.CreatedAt, &alert.UpdatedAt)
}

func UpdateServiceLogAlert(alert *ServiceLogAlert) error {
	if alert == nil || strings.TrimSpace(alert.ID) == "" {
		return fmt.Errorf("missing alert id")
	}
	channels := normalizeLogAlertChannels(alert.Channels)
	return database.DB.QueryRow(
		`UPDATE service_log_alerts
		    SET name=$2,
		        enabled=$3,
		        filter_query=$4,
		        pattern=$5,
		        threshold=$6,
		        window_seconds=$7,
		        comparison=$8,
		        cooldown_seconds=$9,
		        channels=$10::text[],
		        webhook_url=$11,
		        priority=$12,
		        updated_at=NOW()
		  WHERE id=$1
		  RETURNING updated_at`,
		alert.ID,
		strings.TrimSpace(alert.Name),
		alert.Enabled,
		strings.TrimSpace(alert.FilterQuery),
		strings.TrimSpace(alert.Pattern),
		alert.Threshold,
		alert.WindowSeconds,
		strings.TrimSpace(alert.Comparison),
		alert.CooldownSeconds,
		channels,
		strings.TrimSpace(alert.WebhookURL),
		strings.TrimSpace(alert.Priority),
	).Scan(&alert.UpdatedAt)
}

func DeleteServiceLogAlert(id string) error {
	_, err := database.DB.Exec("DELETE FROM service_log_alerts WHERE id=$1", id)
	return err
}

func ListEnabledServiceLogAlerts(limit int) ([]ServiceLogAlert, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := database.DB.Query(
		`SELECT id, service_id, workspace_id, name, enabled, COALESCE(filter_query,''), COALESCE(pattern,''),
		        threshold, window_seconds, comparison, cooldown_seconds, COALESCE(channels, '{incident}'::text[]),
		        COALESCE(webhook_url,''), COALESCE(priority,'normal'), COALESCE(status,'ok'), COALESCE(last_match_count,0),
		        last_evaluated_at, last_triggered_at, last_resolved_at, created_by::text, created_at, updated_at
		   FROM service_log_alerts
		  WHERE enabled=true
		  ORDER BY updated_at DESC
		  LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ServiceLogAlert{}
	for rows.Next() {
		a, err := scanServiceLogAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func UpdateServiceLogAlertEvaluationState(id string, status string, matchCount int, lastTriggeredAt, lastResolvedAt *time.Time, lastError string) error {
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		status = "ok"
	}
	_, err := database.DB.Exec(
		`UPDATE service_log_alerts
		    SET status=$2,
		        last_match_count=GREATEST($3, 0),
		        last_triggered_at=COALESCE($4, last_triggered_at),
		        last_resolved_at=COALESCE($5, last_resolved_at),
		        last_evaluated_at=NOW(),
		        last_error=COALESCE($6, ''),
		        updated_at=NOW()
		  WHERE id=$1`,
		id,
		status,
		matchCount,
		lastTriggeredAt,
		lastResolvedAt,
		strings.TrimSpace(lastError),
	)
	return err
}
