package models

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/railpush/api/database"
)

type AlertEvent struct {
	ID         string    `json:"id"`
	ReceivedAt time.Time `json:"received_at"`
	Status     string    `json:"status"`
	Receiver   string    `json:"receiver"`
	GroupKey   string    `json:"group_key"`
	AlertName  string    `json:"alertname"`
	Severity   string    `json:"severity"`
	Namespace  string    `json:"namespace"`
	Payload    []byte    `json:"-"`
}

// AlertIncident is an "incident rollup" keyed by Alertmanager's groupKey.
// ID is a URL-safe base64 encoding of the underlying group_key.
type AlertIncident struct {
	ID               string    `json:"id"`
	Status           string    `json:"status"`
	Receiver         string    `json:"receiver"`
	AlertName        string    `json:"alertname"`
	Severity         string    `json:"severity"`
	Namespace        string    `json:"namespace"`
	Summary          string    `json:"summary"`
	Description      string    `json:"description"`
	RunbookURL       string    `json:"runbook_url"`
	AlertsCount      int       `json:"alerts_count"`
	LatestEventID    string    `json:"latest_event_id"`
	LatestReceivedAt time.Time `json:"latest_received_at"`
	FirstSeenAt      time.Time `json:"first_seen_at"`
	LastSeenAt       time.Time `json:"last_seen_at"`
	EventCount       int       `json:"event_count"`

	// Ops actions (optional, populated if someone acknowledged/silenced via RailPush).
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy string     `json:"acknowledged_by,omitempty"`
	AckNote        string     `json:"ack_note,omitempty"`
	SilenceID      string     `json:"silence_id,omitempty"`
	SilencedUntil  *time.Time `json:"silenced_until,omitempty"`
	SilencedBy     string     `json:"silenced_by,omitempty"`
}

type AlertIncidentEvent struct {
	ID          string    `json:"id"`
	ReceivedAt  time.Time `json:"received_at"`
	Status      string    `json:"status"`
	Receiver    string    `json:"receiver"`
	AlertsCount int       `json:"alerts_count"`
}

type AlertIncidentDetail struct {
	AlertIncident
	LatestPayload json.RawMessage     `json:"latest_payload"`
	Events        []AlertIncidentEvent `json:"events"`
}

func CreateAlertEvent(e *AlertEvent) error {
	if e == nil {
		return nil
	}
	return database.DB.QueryRow(
		`INSERT INTO alert_events
			(status, receiver, group_key, alertname, severity, namespace, payload)
		 VALUES
			($1, $2, $3, $4, $5, $6, $7::jsonb)
		 RETURNING id, received_at`,
		e.Status, e.Receiver, e.GroupKey, e.AlertName, e.Severity, e.Namespace, string(e.Payload),
	).Scan(&e.ID, &e.ReceivedAt)
}

func incidentIDFromGroupKey(groupKey string) string {
	// Use URL-safe base64 without padding so the ID can be used in routes.
	return base64.RawURLEncoding.EncodeToString([]byte(groupKey))
}

// ListAlertIncidents returns incident rollups derived from alert_events.
//
// state: "active" (firing), "resolved", or "all".
func ListAlertIncidents(state string, limit, offset int) ([]AlertIncident, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := database.DB.Query(
		`
		WITH latest AS (
			SELECT DISTINCT ON (group_key)
				id,
				received_at,
				LOWER(COALESCE(status,'')) AS status,
				COALESCE(receiver,'') AS receiver,
				COALESCE(group_key,'') AS group_key,
				COALESCE(NULLIF(alertname,''), payload#>>'{commonLabels,alertname}', payload#>>'{alerts,0,labels,alertname}', '') AS alertname,
				COALESCE(NULLIF(severity,''), payload#>>'{commonLabels,severity}', payload#>>'{alerts,0,labels,severity}', '') AS severity,
				COALESCE(NULLIF(namespace,''), payload#>>'{commonLabels,namespace}', payload#>>'{alerts,0,labels,namespace}', '') AS namespace,
				COALESCE(NULLIF(payload#>>'{commonAnnotations,summary}',''), payload#>>'{commonAnnotations,message}', '') AS summary,
				COALESCE(payload#>>'{commonAnnotations,description}','') AS description,
				COALESCE(NULLIF(payload#>>'{commonAnnotations,runbook_url}',''), payload#>>'{commonAnnotations,runbook}', '') AS runbook_url,
				CASE WHEN jsonb_typeof(payload->'alerts')='array' THEN jsonb_array_length(payload->'alerts') ELSE 0 END AS alerts_count
			FROM alert_events
			WHERE COALESCE(group_key,'') <> ''
			ORDER BY group_key, received_at DESC
		),
		agg AS (
			SELECT
				group_key,
				MIN(received_at) AS first_seen_at,
				MAX(received_at) AS last_seen_at,
				COUNT(*)::int AS event_count
			FROM alert_events
			WHERE COALESCE(group_key,'') <> ''
			GROUP BY group_key
		),
		ack AS (
			SELECT DISTINCT ON (group_key)
				group_key,
				acknowledged_by,
				COALESCE(note,'') AS note,
				created_at AS acknowledged_at
			FROM incident_acknowledgements
			ORDER BY group_key, created_at DESC
		),
		sil AS (
			SELECT DISTINCT ON (group_key)
				group_key,
				silence_id,
				created_by,
				ends_at AS silenced_until
			FROM incident_silences
			WHERE ends_at > NOW()
			ORDER BY group_key, ends_at DESC
		)
		SELECT
			latest.id,
			latest.received_at,
			latest.status,
			latest.receiver,
			latest.group_key,
			latest.alertname,
			latest.severity,
			latest.namespace,
			latest.summary,
			latest.description,
			latest.runbook_url,
			latest.alerts_count,
			agg.first_seen_at,
			agg.last_seen_at,
			agg.event_count,
			ack.acknowledged_at,
			COALESCE(ack_u.username,'') AS acknowledged_by,
			COALESCE(ack.note,'') AS ack_note,
			COALESCE(sil.silence_id,'') AS silence_id,
			sil.silenced_until,
			COALESCE(sil_u.username,'') AS silenced_by
		FROM latest
		JOIN agg USING (group_key)
		LEFT JOIN ack USING (group_key)
		LEFT JOIN users ack_u ON ack_u.id = ack.acknowledged_by
		LEFT JOIN sil USING (group_key)
		LEFT JOIN users sil_u ON sil_u.id = sil.created_by
		WHERE
			($1 = 'all')
			OR ($1 = 'active' AND latest.status = 'firing')
			OR ($1 = 'resolved' AND latest.status = 'resolved')
		ORDER BY agg.last_seen_at DESC
		LIMIT $2 OFFSET $3
		`,
		state, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AlertIncident{}
	for rows.Next() {
		var (
			latestEventID string
			latestAt      time.Time
			status        string
			receiver      string
			groupKey      string
			alertName     string
			severity      string
			namespace     string
			summary       string
			description   string
			runbookURL    string
			alertsCount   int
			firstSeenAt   time.Time
			lastSeenAt    time.Time
			eventCount    int
			ackAt         sql.NullTime
			ackBy         string
			ackNote       string
			silenceID     string
			silencedUntil sql.NullTime
			silencedBy    string
		)
		if err := rows.Scan(
			&latestEventID,
			&latestAt,
			&status,
			&receiver,
			&groupKey,
			&alertName,
			&severity,
			&namespace,
			&summary,
			&description,
			&runbookURL,
			&alertsCount,
			&firstSeenAt,
			&lastSeenAt,
			&eventCount,
			&ackAt,
			&ackBy,
			&ackNote,
			&silenceID,
			&silencedUntil,
			&silencedBy,
		); err != nil {
			return nil, err
		}
		var ackAtPtr *time.Time
		if ackAt.Valid {
			t := ackAt.Time
			ackAtPtr = &t
		}
		var silUntilPtr *time.Time
		if silencedUntil.Valid {
			t := silencedUntil.Time
			silUntilPtr = &t
		}
		out = append(out, AlertIncident{
			ID:               incidentIDFromGroupKey(groupKey),
			Status:           status,
			Receiver:         receiver,
			AlertName:        alertName,
			Severity:         severity,
			Namespace:        namespace,
			Summary:          summary,
			Description:      description,
			RunbookURL:       runbookURL,
			AlertsCount:      alertsCount,
			LatestEventID:    latestEventID,
			LatestReceivedAt: latestAt,
			FirstSeenAt:      firstSeenAt,
			LastSeenAt:       lastSeenAt,
			EventCount:       eventCount,
			AcknowledgedAt:   ackAtPtr,
			AcknowledgedBy:   strings.TrimSpace(ackBy),
			AckNote:          strings.TrimSpace(ackNote),
			SilenceID:        strings.TrimSpace(silenceID),
			SilencedUntil:    silUntilPtr,
			SilencedBy:       strings.TrimSpace(silencedBy),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func GetAlertIncidentDetail(groupKey string, eventsLimit int) (*AlertIncidentDetail, error) {
	if groupKey == "" {
		return nil, nil
	}
	if eventsLimit <= 0 {
		eventsLimit = 50
	}
	if eventsLimit > 200 {
		eventsLimit = 200
	}

	var (
		d AlertIncidentDetail
		payloadText   []byte
		ackAt         sql.NullTime
		ackBy         string
		ackNote       string
		silenceID     string
		silencedUntil sql.NullTime
		silencedBy    string
	)
	err := database.DB.QueryRow(
		`
		WITH latest AS (
			SELECT
				id,
				received_at,
				LOWER(COALESCE(status,'')) AS status,
				COALESCE(receiver,'') AS receiver,
				COALESCE(group_key,'') AS group_key,
				COALESCE(NULLIF(alertname,''), payload#>>'{commonLabels,alertname}', payload#>>'{alerts,0,labels,alertname}', '') AS alertname,
				COALESCE(NULLIF(severity,''), payload#>>'{commonLabels,severity}', payload#>>'{alerts,0,labels,severity}', '') AS severity,
				COALESCE(NULLIF(namespace,''), payload#>>'{commonLabels,namespace}', payload#>>'{alerts,0,labels,namespace}', '') AS namespace,
				COALESCE(NULLIF(payload#>>'{commonAnnotations,summary}',''), payload#>>'{commonAnnotations,message}', '') AS summary,
				COALESCE(payload#>>'{commonAnnotations,description}','') AS description,
				COALESCE(NULLIF(payload#>>'{commonAnnotations,runbook_url}',''), payload#>>'{commonAnnotations,runbook}', '') AS runbook_url,
				CASE WHEN jsonb_typeof(payload->'alerts')='array' THEN jsonb_array_length(payload->'alerts') ELSE 0 END AS alerts_count,
				payload::text AS payload_text
			FROM alert_events
			WHERE group_key=$1 AND COALESCE(group_key,'') <> ''
			ORDER BY received_at DESC
			LIMIT 1
		),
		agg AS (
			SELECT
				group_key,
				MIN(received_at) AS first_seen_at,
				MAX(received_at) AS last_seen_at,
				COUNT(*)::int AS event_count
			FROM alert_events
			WHERE group_key=$1 AND COALESCE(group_key,'') <> ''
			GROUP BY group_key
		),
		ack AS (
			SELECT
				acknowledged_by,
				COALESCE(note,'') AS note,
				created_at AS acknowledged_at
			FROM incident_acknowledgements
			WHERE group_key=$1
			ORDER BY created_at DESC
			LIMIT 1
		),
		sil AS (
			SELECT
				silence_id,
				created_by,
				ends_at AS silenced_until
			FROM incident_silences
			WHERE group_key=$1 AND ends_at > NOW()
			ORDER BY ends_at DESC
			LIMIT 1
		)
		SELECT
			latest.id,
			latest.received_at,
			latest.status,
			latest.receiver,
			latest.alertname,
			latest.severity,
			latest.namespace,
			latest.summary,
			latest.description,
			latest.runbook_url,
			latest.alerts_count,
			agg.first_seen_at,
			agg.last_seen_at,
			agg.event_count,
			latest.payload_text,
			ack.acknowledged_at,
			COALESCE(ack_u.username,'') AS acknowledged_by,
			COALESCE(ack.note,'') AS ack_note,
			COALESCE(sil.silence_id,'') AS silence_id,
			sil.silenced_until,
			COALESCE(sil_u.username,'') AS silenced_by
		FROM latest
		JOIN agg USING (group_key)
		LEFT JOIN ack ON true
		LEFT JOIN users ack_u ON ack_u.id = ack.acknowledged_by
		LEFT JOIN sil ON true
		LEFT JOIN users sil_u ON sil_u.id = sil.created_by
		`,
		groupKey,
	).Scan(
		&d.LatestEventID,
		&d.LatestReceivedAt,
		&d.Status,
		&d.Receiver,
		&d.AlertName,
		&d.Severity,
		&d.Namespace,
		&d.Summary,
		&d.Description,
		&d.RunbookURL,
		&d.AlertsCount,
		&d.FirstSeenAt,
		&d.LastSeenAt,
		&d.EventCount,
		&payloadText,
		&ackAt,
		&ackBy,
		&ackNote,
		&silenceID,
		&silencedUntil,
		&silencedBy,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	d.ID = incidentIDFromGroupKey(groupKey)
	d.LatestPayload = json.RawMessage(payloadText)
	if ackAt.Valid {
		t := ackAt.Time
		d.AcknowledgedAt = &t
	}
	d.AcknowledgedBy = strings.TrimSpace(ackBy)
	d.AckNote = strings.TrimSpace(ackNote)
	d.SilenceID = strings.TrimSpace(silenceID)
	if silencedUntil.Valid {
		t := silencedUntil.Time
		d.SilencedUntil = &t
	}
	d.SilencedBy = strings.TrimSpace(silencedBy)

	rows, err := database.DB.Query(
		`
		SELECT
			id,
			received_at,
			LOWER(COALESCE(status,'')) AS status,
			COALESCE(receiver,'') AS receiver,
			CASE WHEN jsonb_typeof(payload->'alerts')='array' THEN jsonb_array_length(payload->'alerts') ELSE 0 END AS alerts_count
		FROM alert_events
		WHERE group_key=$1 AND COALESCE(group_key,'') <> ''
		ORDER BY received_at DESC
		LIMIT $2
		`,
		groupKey, eventsLimit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var ev AlertIncidentEvent
		if err := rows.Scan(&ev.ID, &ev.ReceivedAt, &ev.Status, &ev.Receiver, &ev.AlertsCount); err != nil {
			return nil, err
		}
		d.Events = append(d.Events, ev)
	}
	return &d, nil
}
