package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type DiskHandler struct{}

func NewDiskHandler() *DiskHandler {
	return &DiskHandler{}
}

func (h *DiskHandler) ListServiceDisks(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	disk, err := models.GetDiskByService(serviceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list disks")
		return
	}
	if disk == nil {
		utils.RespondJSON(w, http.StatusOK, []models.Disk{})
		return
	}
	utils.RespondJSON(w, http.StatusOK, []models.Disk{*disk})
}

func (h *DiskHandler) UpsertServiceDisk(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	if svc.Instances > 1 {
		utils.RespondError(w, http.StatusBadRequest, "persistent disks require a single service instance")
		return
	}

	var req struct {
		Name      string `json:"name"`
		MountPath string `json:"mount_path"`
		SizeGB    int    `json:"size_gb"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.MountPath = strings.TrimSpace(req.MountPath)
	if req.Name == "" {
		utils.RespondError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.MountPath == "" || !strings.HasPrefix(req.MountPath, "/") {
		utils.RespondError(w, http.StatusBadRequest, "mount_path must be an absolute path")
		return
	}
	if req.SizeGB <= 0 {
		req.SizeGB = 1
	}
	if req.SizeGB > 1024 {
		utils.RespondError(w, http.StatusBadRequest, "size_gb too large")
		return
	}

	_ = models.DeleteDiskByService(serviceID)
	d := &models.Disk{ServiceID: serviceID, Name: req.Name, MountPath: req.MountPath, SizeGB: req.SizeGB}
	if err := models.CreateDisk(d); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to save disk")
		return
	}
	services.Audit(svc.WorkspaceID, userID, "service.disk_upserted", "service", svc.ID, map[string]interface{}{
		"disk_name":   d.Name,
		"mount_path":  d.MountPath,
		"size_gb":     d.SizeGB,
		"note":        "redeploy required to apply disk mount",
	})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"disk": d, "redeploy_required": true})
}

func (h *DiskHandler) DeleteServiceDisk(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["id"]
	userID := middleware.GetUserID(r)
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := models.DeleteDiskByService(serviceID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete disk")
		return
	}
	services.Audit(svc.WorkspaceID, userID, "service.disk_deleted", "service", svc.ID, map[string]interface{}{})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"status": "deleted", "redeploy_required": true})
}
