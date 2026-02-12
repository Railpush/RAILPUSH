package services

import (
	"errors"
	"strings"

	"github.com/railpush/api/models"
)

var ErrForbidden = errors.New("forbidden")

func roleRank(role string) int {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case models.RoleOwner:
		return 4
	case models.RoleAdmin:
		return 3
	case models.RoleDeveloper:
		return 2
	case models.RoleViewer:
		return 1
	default:
		return 0
	}
}

func HasWorkspaceAccess(userID, workspaceID, minRole string) (bool, string, error) {
	role, err := models.GetWorkspaceMemberRole(workspaceID, userID)
	if err != nil {
		return false, "", err
	}
	if role == "" {
		return false, "", nil
	}
	allow := roleRank(role) >= roleRank(minRole)
	return allow, role, nil
}

func EnsureWorkspaceAccess(userID, workspaceID, minRole string) error {
	ok, _, err := HasWorkspaceAccess(userID, workspaceID, minRole)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}
