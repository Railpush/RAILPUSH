package models

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/railpush/api/database"
)

type ServiceLogDrain struct {
	ID             string     `json:"id"`
	ServiceID      string     `json:"service_id"`
	WorkspaceID    string     `json:"workspace_id"`
	Name           string     `json:"name"`
	Destination    string     `json:"destination"`
	ConfigEncrypted string    `json:"-"`
	FilterLogTypes []string   `json:"filter_log_types"`
	FilterMinLevel string     `json:"filter_min_level"`
	IncludePatterns []string  `json:"include_patterns"`
	ExcludePatterns []string  `json:"exclude_patterns"`
	Enabled        bool       `json:"enabled"`
	SentCount      int64      `json:"sent_count"`
	FailedCount    int64      `json:"failed_count"`
	LastError      string     `json:"last_error,omitempty"`
	LastDeliveryAt *time.Time `json:"last_delivery_at,omitempty"`
	LastCursorAt   *time.Time `json:"last_cursor_at,omitempty"`
	LastTestAt     *time.Time `json:"last_test_at,omitempty"`
	CreatedBy      *string    `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func NormalizeServiceLogDrainDestination(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "webhook", "generic", "generic_webhook":
		return "webhook"
	case "datadog":
		return "datadog"
	case "loki", "grafana", "grafana_cloud", "grafana-cloud":
		return "loki"
	case "splunk", "hec":
		return "splunk"
	case "elasticsearch", "elastic":
		return "elasticsearch"
	case "opensearch", "open_search":
		return "opensearch"
	case "cloudwatch", "aws_cloudwatch", "aws-cloudwatch":
		return "cloudwatch"
	case "s3", "s3_compatible", "s3-compatible":
		return "s3"
	default:
		return ""
	}
}

func NormalizeServiceLogDrainLevel(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "", "info":
		return "info"
	case "debug":
		return "debug"
	case "warn", "warning":
		return "warn"
	case "error", "err", "fatal", "panic":
		return "error"
	default:
		return ""
	}
}

func normalizeServiceLogDrainStringList(raw []string, max int) []string {
	if max <= 0 {
		max = 100
	}
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		k := strings.ToLower(v)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
		if len(out) >= max {
			break
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func NormalizeServiceLogDrainLogTypes(raw []string) []string {
	if len(raw) == 0 {
		return []string{"app"}
	}
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		v := strings.ToLower(strings.TrimSpace(item))
		switch v {
		case "app", "application", "runtime":
			v = "app"
		case "request", "http", "access":
			v = "request"
		case "build", "deploy", "build_log", "build-logs":
			v = "build"
		default:
			continue
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) == 0 {
		return []string{"app"}
	}
	return out
}

func ServiceLogDrainWantsType(logTypes []string, logType string) bool {
	logType = strings.ToLower(strings.TrimSpace(logType))
	if logType == "" {
		return false
	}
	norm := NormalizeServiceLogDrainLogTypes(logTypes)
	for _, item := range norm {
		if item == logType {
			return true
		}
	}
	return false
}

func parseServiceLogDrainTextList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return []string{}
	}

	var arr pq.StringArray
	if err := arr.Scan(raw); err == nil {
		return normalizeServiceLogDrainStringList([]string(arr), 50)
	}
	if err := arr.Scan([]byte(raw)); err == nil {
		return normalizeServiceLogDrainStringList([]string(arr), 50)
	}

	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		trimmed := strings.Trim(raw, "[]")
		parts := strings.Split(trimmed, ",")
		return normalizeServiceLogDrainStringList(parts, 50)
	}
	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		return normalizeServiceLogDrainStringList(parts, 50)
	}
	return normalizeServiceLogDrainStringList([]string{raw}, 50)
}

func scanServiceLogDrain(scanner interface{ Scan(dest ...interface{}) error }) (*ServiceLogDrain, error) {
	var d ServiceLogDrain
	var typesRaw sql.NullString
	var includeRaw sql.NullString
	var excludeRaw sql.NullString
	var createdBy sql.NullString
	var lastDelivery sql.NullTime
	var lastCursor sql.NullTime
	var lastTest sql.NullTime

	err := scanner.Scan(
		&d.ID,
		&d.ServiceID,
		&d.WorkspaceID,
		&d.Name,
		&d.Destination,
		&d.ConfigEncrypted,
		&typesRaw,
		&d.FilterMinLevel,
		&includeRaw,
		&excludeRaw,
		&d.Enabled,
		&d.SentCount,
		&d.FailedCount,
		&d.LastError,
		&lastDelivery,
		&lastCursor,
		&lastTest,
		&createdBy,
		&d.CreatedAt,
		&d.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan service_log_drain: %w", err)
	}

	d.Destination = NormalizeServiceLogDrainDestination(d.Destination)
	types := []string{}
	if typesRaw.Valid {
		types = parseServiceLogDrainTextList(typesRaw.String)
	}
	d.FilterLogTypes = NormalizeServiceLogDrainLogTypes(types)
	d.FilterMinLevel = NormalizeServiceLogDrainLevel(d.FilterMinLevel)
	if d.FilterMinLevel == "" {
		d.FilterMinLevel = "info"
	}
	if includeRaw.Valid {
		d.IncludePatterns = parseServiceLogDrainTextList(includeRaw.String)
	} else {
		d.IncludePatterns = []string{}
	}
	if excludeRaw.Valid {
		d.ExcludePatterns = parseServiceLogDrainTextList(excludeRaw.String)
	} else {
		d.ExcludePatterns = []string{}
	}
	d.LastError = strings.TrimSpace(d.LastError)

	if lastDelivery.Valid {
		t := lastDelivery.Time
		d.LastDeliveryAt = &t
	}
	if lastCursor.Valid {
		t := lastCursor.Time
		d.LastCursorAt = &t
	}
	if lastTest.Valid {
		t := lastTest.Time
		d.LastTestAt = &t
	}
	if createdBy.Valid {
		v := strings.TrimSpace(createdBy.String)
		if v != "" {
			d.CreatedBy = &v
		}
	}

	return &d, nil
}

func ListServiceLogDrains(serviceID string) ([]ServiceLogDrain, error) {
	rows, err := database.DB.Query(
		`SELECT id::text,
		        COALESCE(service_id::text, ''),
		        COALESCE(workspace_id::text, ''),
		        COALESCE(name,''),
		        COALESCE(destination,''),
		        COALESCE(config_encrypted,''),
		        COALESCE(filter_log_types::text, '{app}'),
		        COALESCE(filter_min_level, 'info'),
		        COALESCE(include_patterns::text, '{}'),
		        COALESCE(exclude_patterns::text, '{}'),
		        COALESCE(enabled, true),
		        COALESCE(sent_count, 0),
		        COALESCE(failed_count, 0),
		        COALESCE(last_error, ''),
		        last_delivery_at,
		        last_cursor_at,
		        last_test_at,
		        created_by::text,
		        created_at,
		        updated_at
		   FROM service_log_drains
		  WHERE service_id=$1
		  ORDER BY created_at DESC`,
		serviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ServiceLogDrain{}
	for rows.Next() {
		d, err := scanServiceLogDrain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func GetServiceLogDrainForService(serviceID, drainID string) (*ServiceLogDrain, error) {
	row := database.DB.QueryRow(
		`SELECT id::text,
		        COALESCE(service_id::text, ''),
		        COALESCE(workspace_id::text, ''),
		        COALESCE(name,''),
		        COALESCE(destination,''),
		        COALESCE(config_encrypted,''),
		        COALESCE(filter_log_types::text, '{app}'),
		        COALESCE(filter_min_level, 'info'),
		        COALESCE(include_patterns::text, '{}'),
		        COALESCE(exclude_patterns::text, '{}'),
		        COALESCE(enabled, true),
		        COALESCE(sent_count, 0),
		        COALESCE(failed_count, 0),
		        COALESCE(last_error, ''),
		        last_delivery_at,
		        last_cursor_at,
		        last_test_at,
		        created_by::text,
		        created_at,
		        updated_at
		   FROM service_log_drains
		  WHERE id=$1 AND service_id=$2`,
		drainID,
		serviceID,
	)
	d, err := scanServiceLogDrain(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

func CreateServiceLogDrain(d *ServiceLogDrain) error {
	if d == nil {
		return sql.ErrNoRows
	}
	logTypes := NormalizeServiceLogDrainLogTypes(d.FilterLogTypes)
	minLevel := NormalizeServiceLogDrainLevel(d.FilterMinLevel)
	if minLevel == "" {
		minLevel = "info"
	}
	include := normalizeServiceLogDrainStringList(d.IncludePatterns, 50)
	exclude := normalizeServiceLogDrainStringList(d.ExcludePatterns, 50)
	createdBy := ""
	if d.CreatedBy != nil {
		createdBy = strings.TrimSpace(*d.CreatedBy)
	}

	return database.DB.QueryRow(
		`INSERT INTO service_log_drains (
			service_id,
			workspace_id,
			name,
			destination,
			config_encrypted,
			filter_log_types,
			filter_min_level,
			include_patterns,
			exclude_patterns,
			enabled,
			created_by,
			created_at,
			updated_at
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			NULLIF($11,'')::uuid,
			NOW(),
			NOW()
		)
		RETURNING id, created_at, updated_at`,
		d.ServiceID,
		d.WorkspaceID,
		strings.TrimSpace(d.Name),
		NormalizeServiceLogDrainDestination(d.Destination),
		strings.TrimSpace(d.ConfigEncrypted),
		pq.Array(logTypes),
		minLevel,
		pq.Array(include),
		pq.Array(exclude),
		d.Enabled,
		createdBy,
	).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
}

func DeleteServiceLogDrain(serviceID, drainID string) error {
	_, err := database.DB.Exec(
		"DELETE FROM service_log_drains WHERE id=$1 AND service_id=$2",
		drainID,
		serviceID,
	)
	return err
}

func ListEnabledServiceLogDrains(limit int) ([]ServiceLogDrain, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := database.DB.Query(
		`SELECT id::text,
		        COALESCE(service_id::text, ''),
		        COALESCE(workspace_id::text, ''),
		        COALESCE(name,''),
		        COALESCE(destination,''),
		        COALESCE(config_encrypted,''),
		        COALESCE(filter_log_types::text, '{app}'),
		        COALESCE(filter_min_level, 'info'),
		        COALESCE(include_patterns::text, '{}'),
		        COALESCE(exclude_patterns::text, '{}'),
		        COALESCE(enabled, true),
		        COALESCE(sent_count, 0),
		        COALESCE(failed_count, 0),
		        COALESCE(last_error, ''),
		        last_delivery_at,
		        last_cursor_at,
		        last_test_at,
		        created_by::text,
		        created_at,
		        updated_at
		   FROM service_log_drains
		  WHERE enabled=true
		  ORDER BY updated_at ASC
		  LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ServiceLogDrain{}
	for rows.Next() {
		d, err := scanServiceLogDrain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func UpdateServiceLogDrainDeliveryStats(drainID string, sentDelta, failedDelta int64, lastError string, lastDeliveryAt, lastCursorAt *time.Time) error {
	_, err := database.DB.Exec(
		`UPDATE service_log_drains
		    SET sent_count = GREATEST(COALESCE(sent_count, 0) + $2, 0),
		        failed_count = GREATEST(COALESCE(failed_count, 0) + $3, 0),
		        last_error = COALESCE($4, ''),
		        last_delivery_at = COALESCE($5, last_delivery_at),
		        last_cursor_at = COALESCE($6, last_cursor_at),
		        updated_at = NOW()
		  WHERE id=$1`,
		drainID,
		sentDelta,
		failedDelta,
		strings.TrimSpace(lastError),
		lastDeliveryAt,
		lastCursorAt,
	)
	return err
}

func RecordServiceLogDrainTestResult(drainID string, success bool, lastError string) error {
	now := time.Now().UTC()
	_, err := database.DB.Exec(
		`UPDATE service_log_drains
		    SET last_test_at=$2,
		        last_error=COALESCE($3, ''),
		        updated_at=NOW()
		  WHERE id=$1`,
		drainID,
		now,
		strings.TrimSpace(lastError),
	)
	return err
}
