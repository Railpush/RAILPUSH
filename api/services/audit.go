package services

import (
	"log"

	"github.com/railpush/api/models"
)

func Audit(workspaceID, userID, action, resourceType, resourceID string, details interface{}) {
	if workspaceID == "" || userID == "" || action == "" {
		return
	}
	if err := models.CreateAuditLog(workspaceID, userID, action, resourceType, resourceID, details); err != nil {
		log.Printf("audit write failed: action=%s workspace=%s user=%s err=%v", action, workspaceID, userID, err)
	}
}
