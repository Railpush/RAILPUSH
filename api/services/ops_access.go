package services

import (
	"strings"

	"github.com/railpush/api/models"
)

// EnsureOpsAccess gates platform-level operations (cluster/infra visibility).
//
// For now this is backed by the global users.role field ("admin" / "ops").
// This keeps the ops surface separate from workspace-level RBAC.
func EnsureOpsAccess(userID string) error {
	u, err := models.GetUserByID(userID)
	if err != nil {
		return err
	}
	if u == nil {
		return ErrForbidden
	}
	role := strings.ToLower(strings.TrimSpace(u.Role))
	if role == "admin" || role == "ops" {
		return nil
	}
	return ErrForbidden
}

