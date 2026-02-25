package models

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/railpush/api/database"
	"github.com/railpush/api/utils"
)

var ErrDeleteConfirmationRequired = errors.New("delete confirmation required")
var ErrDeleteConfirmationInvalid = errors.New("invalid confirmation token")
var ErrDeleteConfirmationExpired = errors.New("confirmation token expired")

type ResourceDeletionState struct {
	ResourceType        string     `json:"resource_type"`
	ResourceID          string     `json:"resource_id"`
	WorkspaceID         string     `json:"workspace_id"`
	ResourceName        string     `json:"resource_name"`
	DeletionProtection  bool       `json:"deletion_protection"`
	DeletedAt           *time.Time `json:"deleted_at,omitempty"`
	PurgeAfter          *time.Time `json:"purge_after,omitempty"`
	TokenHash           string     `json:"-"`
	TokenExpiresAt      *time.Time `json:"token_expires_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func GetResourceDeletionState(resourceType, resourceID string) (*ResourceDeletionState, error) {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if resourceType == "" || resourceID == "" {
		return nil, nil
	}

	state := &ResourceDeletionState{}
	var workspaceID sql.NullString
	var deletedAt sql.NullTime
	var purgeAfter sql.NullTime
	var tokenExpiresAt sql.NullTime

	err := database.DB.QueryRow(
		`SELECT resource_type, resource_id::text, COALESCE(workspace_id::text,''), COALESCE(resource_name,''), COALESCE(deletion_protection,false), deleted_at, purge_after, COALESCE(token_hash,''), token_expires_at, created_at, updated_at
		 FROM resource_deletion_states
		 WHERE resource_type=$1 AND resource_id=$2::uuid`,
		resourceType, resourceID,
	).Scan(
		&state.ResourceType,
		&state.ResourceID,
		&workspaceID,
		&state.ResourceName,
		&state.DeletionProtection,
		&deletedAt,
		&purgeAfter,
		&state.TokenHash,
		&tokenExpiresAt,
		&state.CreatedAt,
		&state.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if workspaceID.Valid {
		state.WorkspaceID = workspaceID.String
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		state.DeletedAt = &t
	}
	if purgeAfter.Valid {
		t := purgeAfter.Time
		state.PurgeAfter = &t
	}
	if tokenExpiresAt.Valid {
		t := tokenExpiresAt.Time
		state.TokenExpiresAt = &t
	}
	return state, nil
}

func SetResourceDeletionProtection(resourceType, resourceID, workspaceID, resourceName string, enabled bool) error {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	workspaceID = strings.TrimSpace(workspaceID)
	resourceName = strings.TrimSpace(resourceName)
	if resourceType == "" || resourceID == "" {
		return fmt.Errorf("missing resource")
	}

	_, err := database.DB.Exec(
		`INSERT INTO resource_deletion_states (resource_type, resource_id, workspace_id, resource_name, deletion_protection)
		 VALUES ($1, $2::uuid, NULLIF($3,'')::uuid, $4, $5)
		 ON CONFLICT (resource_type, resource_id)
		 DO UPDATE SET
		 	workspace_id = NULLIF(EXCLUDED.workspace_id::text,'')::uuid,
		 	resource_name = EXCLUDED.resource_name,
		 	deletion_protection = EXCLUDED.deletion_protection,
		 	updated_at = NOW()`,
		resourceType, resourceID, workspaceID, resourceName, enabled,
	)
	return err
}

func IssueResourceDeletionToken(resourceType, resourceID, workspaceID, resourceName string, ttl time.Duration) (string, time.Time, error) {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	workspaceID = strings.TrimSpace(workspaceID)
	resourceName = strings.TrimSpace(resourceName)
	if resourceType == "" || resourceID == "" {
		return "", time.Time{}, fmt.Errorf("missing resource")
	}

	token, err := utils.GenerateRandomString(24)
	if err != nil || strings.TrimSpace(token) == "" {
		if err == nil {
			err = fmt.Errorf("failed to generate confirmation token")
		}
		return "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(ttl)
	hash := hashDeletionToken(token)

	_, err = database.DB.Exec(
		`INSERT INTO resource_deletion_states (resource_type, resource_id, workspace_id, resource_name, token_hash, token_expires_at)
		 VALUES ($1, $2::uuid, NULLIF($3,'')::uuid, $4, $5, $6)
		 ON CONFLICT (resource_type, resource_id)
		 DO UPDATE SET
		 	workspace_id = NULLIF(EXCLUDED.workspace_id::text,'')::uuid,
		 	resource_name = EXCLUDED.resource_name,
		 	token_hash = EXCLUDED.token_hash,
		 	token_expires_at = EXCLUDED.token_expires_at,
		 	updated_at = NOW()`,
		resourceType, resourceID, workspaceID, resourceName, hash, expiresAt,
	)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func VerifyResourceDeletionToken(resourceType, resourceID, token string) error {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	token = strings.TrimSpace(token)
	if resourceType == "" || resourceID == "" || token == "" {
		return ErrDeleteConfirmationRequired
	}

	state, err := GetResourceDeletionState(resourceType, resourceID)
	if err != nil {
		return err
	}
	if state == nil || strings.TrimSpace(state.TokenHash) == "" {
		return ErrDeleteConfirmationRequired
	}
	if state.TokenExpiresAt == nil || time.Now().After(*state.TokenExpiresAt) {
		return ErrDeleteConfirmationExpired
	}
	if !hmac.Equal([]byte(state.TokenHash), []byte(hashDeletionToken(token))) {
		return ErrDeleteConfirmationInvalid
	}
	return nil
}

func MarkResourceSoftDeleted(resourceType, resourceID, workspaceID, resourceName string, recoveryWindow time.Duration) (time.Time, error) {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	workspaceID = strings.TrimSpace(workspaceID)
	resourceName = strings.TrimSpace(resourceName)
	if resourceType == "" || resourceID == "" {
		return time.Time{}, fmt.Errorf("missing resource")
	}
	if recoveryWindow <= 0 {
		recoveryWindow = 72 * time.Hour
	}
	seconds := int64(recoveryWindow / time.Second)

	var purgeAfter time.Time
	err := database.DB.QueryRow(
		`INSERT INTO resource_deletion_states (resource_type, resource_id, workspace_id, resource_name, deleted_at, purge_after, token_hash, token_expires_at)
		 VALUES ($1, $2::uuid, NULLIF($3,'')::uuid, $4, NOW(), NOW() + ($5::text || ' seconds')::interval, '', NULL)
		 ON CONFLICT (resource_type, resource_id)
		 DO UPDATE SET
		 	workspace_id = NULLIF(EXCLUDED.workspace_id::text,'')::uuid,
		 	resource_name = EXCLUDED.resource_name,
		 	deleted_at = NOW(),
		 	purge_after = NOW() + ($5::text || ' seconds')::interval,
		 	token_hash = '',
		 	token_expires_at = NULL,
		 	updated_at = NOW()
		 RETURNING purge_after`,
		resourceType, resourceID, workspaceID, resourceName, seconds,
	).Scan(&purgeAfter)
	if err != nil {
		return time.Time{}, err
	}
	return purgeAfter, nil
}

func RestoreSoftDeletedResource(resourceType, resourceID string) error {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if resourceType == "" || resourceID == "" {
		return fmt.Errorf("missing resource")
	}
	_, err := database.DB.Exec(
		`UPDATE resource_deletion_states
		 SET deleted_at=NULL, purge_after=NULL, token_hash='', token_expires_at=NULL, updated_at=NOW()
		 WHERE resource_type=$1 AND resource_id=$2::uuid`,
		resourceType, resourceID,
	)
	return err
}

func DeleteResourceDeletionState(resourceType, resourceID string) error {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if resourceType == "" || resourceID == "" {
		return nil
	}
	_, err := database.DB.Exec(
		`DELETE FROM resource_deletion_states WHERE resource_type=$1 AND resource_id=$2::uuid`,
		resourceType, resourceID,
	)
	return err
}

func hashDeletionToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
