package models

import (
	"database/sql"
	"strings"
	"time"

	"github.com/railpush/api/database"
)

type IncidentAcknowledgement struct {
	ID                     string    `json:"id"`
	GroupKey                string    `json:"group_key"`
	AcknowledgedBy          string    `json:"acknowledged_by"`
	AcknowledgedByUsername  string    `json:"acknowledged_by_username"`
	Note                   string    `json:"note"`
	CreatedAt              time.Time `json:"created_at"`
}

func CreateIncidentAcknowledgement(groupKey, userID, note string) (*IncidentAcknowledgement, error) {
	groupKey = strings.TrimSpace(groupKey)
	userID = strings.TrimSpace(userID)
	note = strings.TrimSpace(note)
	if groupKey == "" || userID == "" {
		return nil, nil
	}

	ack := &IncidentAcknowledgement{
		GroupKey:       groupKey,
		AcknowledgedBy: userID,
		Note:           note,
	}
	if err := database.DB.QueryRow(
		`INSERT INTO incident_acknowledgements (group_key, acknowledged_by, note)
		 VALUES ($1, $2, NULLIF($3,''))
		 RETURNING id, created_at`,
		groupKey, userID, note,
	).Scan(&ack.ID, &ack.CreatedAt); err != nil {
		return nil, err
	}

	if u, err := GetUserByID(userID); err == nil && u != nil {
		ack.AcknowledgedByUsername = strings.TrimSpace(u.Username)
	}
	return ack, nil
}

func GetLatestIncidentAcknowledgement(groupKey string) (*IncidentAcknowledgement, error) {
	groupKey = strings.TrimSpace(groupKey)
	if groupKey == "" {
		return nil, nil
	}

	var ack IncidentAcknowledgement
	var byID sql.NullString
	var byUsername sql.NullString
	var note sql.NullString

	err := database.DB.QueryRow(
		`
		SELECT
			a.id,
			a.group_key,
			a.acknowledged_by,
			COALESCE(u.username,'') AS acknowledged_by_username,
			COALESCE(a.note,'') AS note,
			a.created_at
		FROM incident_acknowledgements a
		LEFT JOIN users u ON u.id = a.acknowledged_by
		WHERE a.group_key=$1
		ORDER BY a.created_at DESC
		LIMIT 1
		`,
		groupKey,
	).Scan(&ack.ID, &ack.GroupKey, &byID, &byUsername, &note, &ack.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if byID.Valid {
		ack.AcknowledgedBy = strings.TrimSpace(byID.String)
	}
	if byUsername.Valid {
		ack.AcknowledgedByUsername = strings.TrimSpace(byUsername.String)
	}
	if note.Valid {
		ack.Note = strings.TrimSpace(note.String)
	}
	return &ack, nil
}

