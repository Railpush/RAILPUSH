package models

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/railpush/api/database"
)

type ServiceShellSession struct {
	ID                 string            `json:"id"`
	ServiceID          string            `json:"service_id"`
	WorkspaceID        string            `json:"workspace_id"`
	CreatedBy          *string           `json:"created_by,omitempty"`
	CWD                string            `json:"cwd"`
	BaseEnv            map[string]string `json:"-"`
	CurrentEnv         map[string]string `json:"-"`
	IdleTimeoutMinutes int               `json:"idle_timeout_minutes"`
	ExpiresAt          time.Time         `json:"expires_at"`
	LastUsedAt         time.Time         `json:"last_used_at"`
	CreatedAt          time.Time         `json:"created_at"`
}

func normalizeShellSessionEnv(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		if len(key) > 256 {
			continue
		}
		out[key] = v
	}
	return out
}

func encodeShellSessionEnv(env map[string]string) ([]byte, error) {
	normalized := normalizeShellSessionEnv(env)
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func decodeShellSessionEnv(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]string{}
	}
	return normalizeShellSessionEnv(out)
}

func CreateServiceShellSession(sess *ServiceShellSession) error {
	if sess == nil {
		return fmt.Errorf("missing session")
	}
	if strings.TrimSpace(sess.ServiceID) == "" || strings.TrimSpace(sess.WorkspaceID) == "" {
		return fmt.Errorf("missing service/workspace")
	}
	if sess.CWD == "" {
		sess.CWD = "/"
	}
	if sess.IdleTimeoutMinutes <= 0 {
		sess.IdleTimeoutMinutes = 30
	}
	if sess.IdleTimeoutMinutes > 120 {
		sess.IdleTimeoutMinutes = 120
	}
	if sess.ExpiresAt.IsZero() {
		sess.ExpiresAt = time.Now().UTC().Add(time.Duration(sess.IdleTimeoutMinutes) * time.Minute)
	}
	if sess.BaseEnv == nil {
		sess.BaseEnv = map[string]string{}
	}
	if sess.CurrentEnv == nil {
		sess.CurrentEnv = map[string]string{}
	}
	baseRaw, err := encodeShellSessionEnv(sess.BaseEnv)
	if err != nil {
		return err
	}
	currentRaw, err := encodeShellSessionEnv(sess.CurrentEnv)
	if err != nil {
		return err
	}
	return database.DB.QueryRow(
		`INSERT INTO service_shell_sessions
		 (service_id, workspace_id, created_by, cwd, base_env_json, current_env_json, idle_timeout_minutes, expires_at)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8)
		 RETURNING id, last_used_at, created_at`,
		sess.ServiceID,
		sess.WorkspaceID,
		sess.CreatedBy,
		sess.CWD,
		string(baseRaw),
		string(currentRaw),
		sess.IdleTimeoutMinutes,
		sess.ExpiresAt,
	).Scan(&sess.ID, &sess.LastUsedAt, &sess.CreatedAt)
}

func GetServiceShellSession(id string) (*ServiceShellSession, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	sess := &ServiceShellSession{}
	var createdBy sql.NullString
	var baseRaw, currentRaw string
	err := database.DB.QueryRow(
		`SELECT id,
		        service_id::text,
		        workspace_id::text,
		        created_by::text,
		        COALESCE(cwd, '/'),
		        COALESCE(base_env_json::text, '{}'),
		        COALESCE(current_env_json::text, '{}'),
		        COALESCE(idle_timeout_minutes, 30),
		        expires_at,
		        last_used_at,
		        created_at
		   FROM service_shell_sessions
		  WHERE id=$1`,
		id,
	).Scan(
		&sess.ID,
		&sess.ServiceID,
		&sess.WorkspaceID,
		&createdBy,
		&sess.CWD,
		&baseRaw,
		&currentRaw,
		&sess.IdleTimeoutMinutes,
		&sess.ExpiresAt,
		&sess.LastUsedAt,
		&sess.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if createdBy.Valid && strings.TrimSpace(createdBy.String) != "" {
		value := strings.TrimSpace(createdBy.String)
		sess.CreatedBy = &value
	}
	sess.BaseEnv = decodeShellSessionEnv(baseRaw)
	sess.CurrentEnv = decodeShellSessionEnv(currentRaw)
	if sess.CWD == "" {
		sess.CWD = "/"
	}
	if sess.IdleTimeoutMinutes <= 0 {
		sess.IdleTimeoutMinutes = 30
	}
	return sess, nil
}

func CountActiveServiceShellSessions(serviceID string) (int, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return 0, nil
	}
	var count int
	err := database.DB.QueryRow(
		`SELECT COUNT(*)
		   FROM service_shell_sessions
		  WHERE service_id=$1
		    AND expires_at > NOW()`,
		serviceID,
	).Scan(&count)
	return count, err
}

func UpdateServiceShellSessionState(id, cwd string, currentEnv map[string]string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("missing session id")
	}
	if cwd == "" {
		cwd = "/"
	}
	currentRaw, err := encodeShellSessionEnv(currentEnv)
	if err != nil {
		return err
	}
	_, err = database.DB.Exec(
		`UPDATE service_shell_sessions
		    SET cwd=$1,
		        current_env_json=$2::jsonb,
		        last_used_at=NOW(),
		        expires_at=NOW() + make_interval(mins => GREATEST(idle_timeout_minutes, 1))
		  WHERE id=$3`,
		cwd,
		string(currentRaw),
		id,
	)
	return err
}

func DeleteServiceShellSession(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	_, err := database.DB.Exec(`DELETE FROM service_shell_sessions WHERE id=$1`, id)
	return err
}

func DeleteExpiredServiceShellSessions() error {
	_, err := database.DB.Exec(`DELETE FROM service_shell_sessions WHERE expires_at <= NOW()`)
	return err
}
