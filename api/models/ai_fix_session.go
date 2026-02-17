package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type AIFixSession struct {
	ID             string    `json:"id"`
	ServiceID      string    `json:"service_id"`
	Status         string    `json:"status"`
	MaxAttempts    int       `json:"max_attempts"`
	CurrentAttempt int       `json:"current_attempt"`
	LastDeployID   string    `json:"last_deploy_id"`
	LastAISummary  string    `json:"last_ai_summary"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func CreateAIFixSession(serviceID string) (*AIFixSession, error) {
	s := &AIFixSession{}
	err := database.DB.QueryRow(
		"INSERT INTO ai_fix_sessions (service_id) VALUES ($1) RETURNING id, service_id, status, max_attempts, current_attempt, COALESCE(last_deploy_id,''), COALESCE(last_ai_summary,''), created_at, updated_at",
		serviceID,
	).Scan(&s.ID, &s.ServiceID, &s.Status, &s.MaxAttempts, &s.CurrentAttempt, &s.LastDeployID, &s.LastAISummary, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func GetAIFixSession(id string) (*AIFixSession, error) {
	s := &AIFixSession{}
	err := database.DB.QueryRow(
		"SELECT id, service_id, status, max_attempts, current_attempt, COALESCE(last_deploy_id,''), COALESCE(last_ai_summary,''), created_at, updated_at FROM ai_fix_sessions WHERE id=$1",
		id,
	).Scan(&s.ID, &s.ServiceID, &s.Status, &s.MaxAttempts, &s.CurrentAttempt, &s.LastDeployID, &s.LastAISummary, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func GetActiveAIFixSessionForService(serviceID string) (*AIFixSession, error) {
	s := &AIFixSession{}
	err := database.DB.QueryRow(
		"SELECT id, service_id, status, max_attempts, current_attempt, COALESCE(last_deploy_id,''), COALESCE(last_ai_summary,''), created_at, updated_at FROM ai_fix_sessions WHERE service_id=$1 AND status='running' ORDER BY created_at DESC LIMIT 1",
		serviceID,
	).Scan(&s.ID, &s.ServiceID, &s.Status, &s.MaxAttempts, &s.CurrentAttempt, &s.LastDeployID, &s.LastAISummary, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func UpdateAIFixSessionAttempt(id string, currentAttempt int, lastDeployID string, lastAISummary string) error {
	_, err := database.DB.Exec(
		"UPDATE ai_fix_sessions SET current_attempt=$1, last_deploy_id=$2, last_ai_summary=$3, updated_at=NOW() WHERE id=$4",
		currentAttempt, lastDeployID, lastAISummary, id,
	)
	return err
}

func GetMostRecentAIFixSession(serviceID string) (*AIFixSession, error) {
	s := &AIFixSession{}
	err := database.DB.QueryRow(
		"SELECT id, service_id, status, max_attempts, current_attempt, COALESCE(last_deploy_id,''), COALESCE(last_ai_summary,''), created_at, updated_at FROM ai_fix_sessions WHERE service_id=$1 ORDER BY created_at DESC LIMIT 1",
		serviceID,
	).Scan(&s.ID, &s.ServiceID, &s.Status, &s.MaxAttempts, &s.CurrentAttempt, &s.LastDeployID, &s.LastAISummary, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func UpdateAIFixSessionStatus(id string, status string) error {
	_, err := database.DB.Exec(
		"UPDATE ai_fix_sessions SET status=$1, updated_at=NOW() WHERE id=$2",
		status, id,
	)
	return err
}
