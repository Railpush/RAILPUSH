package models

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/railpush/api/database"
)

type IncidentSilence struct {
	ID                string          `json:"id"`
	GroupKey           string          `json:"group_key"`
	SilenceID          string          `json:"silence_id"`
	Scope              string          `json:"scope"`
	CreatedBy          string          `json:"created_by"`
	CreatedByUsername  string          `json:"created_by_username"`
	Comment            string          `json:"comment"`
	Matchers           json.RawMessage `json:"matchers"`
	StartsAt           time.Time       `json:"starts_at"`
	EndsAt             time.Time       `json:"ends_at"`
	CreatedAt          time.Time       `json:"created_at"`
}

func CreateIncidentSilence(s *IncidentSilence) error {
	if s == nil {
		return nil
	}
	s.GroupKey = strings.TrimSpace(s.GroupKey)
	s.SilenceID = strings.TrimSpace(s.SilenceID)
	s.Scope = strings.TrimSpace(s.Scope)
	s.CreatedBy = strings.TrimSpace(s.CreatedBy)
	s.Comment = strings.TrimSpace(s.Comment)
	if s.GroupKey == "" || s.SilenceID == "" || s.StartsAt.IsZero() || s.EndsAt.IsZero() || len(s.Matchers) == 0 {
		return nil
	}
	if s.Scope == "" {
		s.Scope = "group"
	}

	return database.DB.QueryRow(
		`INSERT INTO incident_silences
			(group_key, silence_id, scope, created_by, comment, matchers, starts_at, ends_at)
		 VALUES
			($1, $2, $3, NULLIF($4,'')::uuid, NULLIF($5,''), $6::jsonb, $7, $8)
		 RETURNING id, created_at`,
		s.GroupKey,
		s.SilenceID,
		s.Scope,
		s.CreatedBy,
		s.Comment,
		string(s.Matchers),
		s.StartsAt,
		s.EndsAt,
	).Scan(&s.ID, &s.CreatedAt)
}

func GetLatestActiveIncidentSilence(groupKey string) (*IncidentSilence, error) {
	groupKey = strings.TrimSpace(groupKey)
	if groupKey == "" {
		return nil, nil
	}

	var s IncidentSilence
	var createdBy sql.NullString
	var createdByUsername sql.NullString
	var comment sql.NullString
	var matchersText []byte

	err := database.DB.QueryRow(
		`
		SELECT
			s.id,
			s.group_key,
			s.silence_id,
			COALESCE(NULLIF(s.scope,''), 'group') AS scope,
			s.created_by,
			COALESCE(u.username,'') AS created_by_username,
			COALESCE(s.comment,'') AS comment,
			s.matchers::text AS matchers_text,
			s.starts_at,
			s.ends_at,
			s.created_at
		FROM incident_silences s
		LEFT JOIN users u ON u.id = s.created_by
		WHERE s.group_key=$1 AND s.ends_at > NOW()
		ORDER BY s.ends_at DESC
		LIMIT 1
		`,
		groupKey,
	).Scan(&s.ID, &s.GroupKey, &s.SilenceID, &s.Scope, &createdBy, &createdByUsername, &comment, &matchersText, &s.StartsAt, &s.EndsAt, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if createdBy.Valid {
		s.CreatedBy = strings.TrimSpace(createdBy.String)
	}
	if createdByUsername.Valid {
		s.CreatedByUsername = strings.TrimSpace(createdByUsername.String)
	}
	if comment.Valid {
		s.Comment = strings.TrimSpace(comment.String)
	}
	s.Matchers = json.RawMessage(matchersText)
	return &s, nil
}
