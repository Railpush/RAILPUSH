package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type ProjectHandler struct {
	Config *config.Config
	Worker *services.Worker
	Stripe *services.StripeService
}

var errProjectFolderNotFound = errors.New("project folder not found")

const maxFolderDepth = 3 // Root counts as depth 0, so max nesting is 3 levels deep.

func NewProjectHandler(cfg *config.Config, worker *services.Worker, stripe *services.StripeService) *ProjectHandler {
	return &ProjectHandler{Config: cfg, Worker: worker, Stripe: stripe}
}

func resolveWorkspaceID(r *http.Request, requested string) (string, error) {
	if strings.TrimSpace(requested) != "" {
		return strings.TrimSpace(requested), nil
	}
	userID := middleware.GetUserID(r)
	ws, err := models.GetWorkspaceByOwner(userID)
	if err != nil || ws == nil {
		return "", err
	}
	return ws.ID, nil
}

func resolveProjectFolderID(workspaceID string, folderID *string) (*string, error) {
	if folderID == nil {
		return nil, nil
	}
	clean := strings.TrimSpace(*folderID)
	if clean == "" {
		return nil, nil
	}

	folder, err := models.GetProjectFolder(clean)
	if err != nil {
		return nil, err
	}
	if folder == nil || folder.WorkspaceID != workspaceID {
		return nil, errProjectFolderNotFound
	}

	return &clean, nil
}

func (h *ProjectHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	workspaceID, err := resolveWorkspaceID(r, r.URL.Query().Get("workspace_id"))
	if err != nil || workspaceID == "" {
		utils.RespondError(w, http.StatusBadRequest, "workspace not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	items, err := models.ListProjects(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}
	if items == nil {
		items = []models.Project{}
	}
	utils.RespondJSON(w, http.StatusOK, items)
}

func (h *ProjectHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkspaceID  string  `json:"workspace_id"`
		Name         string  `json:"name"`
		FolderID     *string `json:"folder_id"`
		Environments []struct {
			Name        string `json:"name"`
			IsProtected bool   `json:"is_protected"`
		} `json:"environments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		utils.RespondError(w, http.StatusBadRequest, "name is required")
		return
	}

	userID := middleware.GetUserID(r)
	workspaceID, err := resolveWorkspaceID(r, req.WorkspaceID)
	if err != nil || workspaceID == "" {
		utils.RespondError(w, http.StatusBadRequest, "workspace not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	folderID, err := resolveProjectFolderID(workspaceID, req.FolderID)
	if err != nil {
		if errors.Is(err, errProjectFolderNotFound) {
			utils.RespondError(w, http.StatusBadRequest, "folder not found")
			return
		}
		utils.RespondError(w, http.StatusInternalServerError, "failed to validate folder")
		return
	}

	p := &models.Project{WorkspaceID: workspaceID, Name: req.Name, FolderID: folderID}
	if err := models.CreateProject(p); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create project")
		return
	}

	envs := req.Environments
	if len(envs) == 0 {
		envs = []struct {
			Name        string `json:"name"`
			IsProtected bool   `json:"is_protected"`
		}{
			{Name: "production", IsProtected: true},
			{Name: "preview", IsProtected: false},
		}
	}
	for _, e := range envs {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		_ = models.CreateEnvironment(&models.Environment{
			ProjectID:   p.ID,
			Name:        name,
			IsProtected: e.IsProtected,
		})
	}

	services.Audit(workspaceID, userID, "project.created", "project", p.ID, map[string]interface{}{
		"name":      p.Name,
		"folder_id": p.FolderID,
	})
	utils.RespondJSON(w, http.StatusCreated, p)
}

func (h *ProjectHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	projectID := mux.Vars(r)["id"]
	p, err := models.GetProject(projectID)
	if err != nil || p == nil {
		utils.RespondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, p.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	envs, _ := models.ListEnvironments(p.ID)
	if envs == nil {
		envs = []models.Environment{}
	}
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"id":           p.ID,
		"workspace_id": p.WorkspaceID,
		"folder_id":    p.FolderID,
		"name":         p.Name,
		"created_at":   p.CreatedAt,
		"environments": envs,
	})
}

func (h *ProjectHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	projectID := mux.Vars(r)["id"]
	p, err := models.GetProject(projectID)
	if err != nil || p == nil {
		utils.RespondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, p.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	nameProvided := false
	folderProvided := false

	oldName := p.Name
	oldFolderID := p.FolderID

	if rawName, ok := req["name"]; ok {
		nameProvided = true
		name, ok := rawName.(string)
		if !ok {
			utils.RespondError(w, http.StatusBadRequest, "name must be a string")
			return
		}
		name = strings.TrimSpace(name)
		if name == "" {
			utils.RespondError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		p.Name = name
	}

	if rawFolderID, ok := req["folder_id"]; ok {
		folderProvided = true
		switch v := rawFolderID.(type) {
		case nil:
			p.FolderID = nil
		case string:
			nextFolderID, err := resolveProjectFolderID(p.WorkspaceID, &v)
			if err != nil {
				if errors.Is(err, errProjectFolderNotFound) {
					utils.RespondError(w, http.StatusBadRequest, "folder not found")
					return
				}
				utils.RespondError(w, http.StatusInternalServerError, "failed to validate folder")
				return
			}
			p.FolderID = nextFolderID
		default:
			utils.RespondError(w, http.StatusBadRequest, "folder_id must be a string or null")
			return
		}
	}

	if !nameProvided && !folderProvided {
		utils.RespondError(w, http.StatusBadRequest, "at least one field is required")
		return
	}

	nameChanged := p.Name != oldName
	folderChanged := false
	switch {
	case oldFolderID == nil && p.FolderID == nil:
		folderChanged = false
	case oldFolderID != nil && p.FolderID != nil:
		folderChanged = strings.TrimSpace(*oldFolderID) != strings.TrimSpace(*p.FolderID)
	default:
		folderChanged = true
	}

	if !nameChanged && !folderChanged {
		utils.RespondJSON(w, http.StatusOK, p)
		return
	}

	if err := models.UpdateProject(p); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update project")
		return
	}

	auditDetails := map[string]interface{}{}
	if nameChanged {
		auditDetails["old_name"] = oldName
		auditDetails["name"] = p.Name
	}
	if folderChanged {
		auditDetails["old_folder_id"] = oldFolderID
		auditDetails["folder_id"] = p.FolderID
	}
	services.Audit(p.WorkspaceID, userID, "project.updated", "project", p.ID, auditDetails)
	utils.RespondJSON(w, http.StatusOK, p)
}

func (h *ProjectHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	projectID := mux.Vars(r)["id"]
	p, err := models.GetProject(projectID)
	if err != nil || p == nil {
		utils.RespondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, p.WorkspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Delete services belonging to this project first (both project_id services and services in project environments).
	svcs, err := models.ListServicesByProject(projectID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load project services")
		return
	}
	var failures []string
	for i := range svcs {
		svc := &svcs[i]
		if svc == nil || strings.TrimSpace(svc.ID) == "" {
			continue
		}

		// Best-effort: remove billing mapping first so deleted services don't continue to accrue charges.
		if strings.TrimSpace(svc.Plan) != "" && strings.ToLower(strings.TrimSpace(svc.Plan)) != services.PlanFree {
			if h != nil && h.Stripe != nil && h.Stripe.Enabled() {
				if err := h.Stripe.RemoveSubscriptionItem("service", svc.ID); err != nil {
					log.Printf("Warning: failed to remove billing for service %s during project delete: %v", svc.ID, err)
				}
			}
		}

		// Best-effort: remove runtime resources.
		if h != nil && h.Config != nil && h.Config.Kubernetes.Enabled {
			var kd *services.KubeDeployer
			if h.Worker != nil {
				if k, err := h.Worker.GetKubeDeployer(); err == nil {
					kd = k
				}
			}
			if kd == nil {
				if k, err := services.NewKubeDeployer(h.Config); err == nil {
					kd = k
				}
			}
			if kd != nil {
				_ = kd.DeleteServiceResources(svc)
			}
		} else if h != nil && h.Worker != nil {
			if svc.ContainerID != "" {
				h.Worker.Deployer.RemoveContainer(svc.ContainerID)
			}
			if instances, err := models.ListServiceInstances(svc.ID); err == nil {
				for _, inst := range instances {
					if inst.ContainerID != "" {
						_ = h.Worker.Deployer.RemoveContainer(inst.ContainerID)
					}
				}
			}
			_ = models.DeleteServiceInstancesByService(svc.ID)
			if h.Config != nil && h.Worker.Router != nil && h.Config.Deploy.Domain != "" && h.Config.Deploy.Domain != "localhost" && !h.Config.Deploy.DisableRouter {
				domain := utils.ServiceHostLabel(svc.Name, svc.Subdomain) + "." + h.Config.Deploy.Domain
				h.Worker.Router.RemoveRoute(domain)
			}
		}

		// Remove any blueprint links to this service to avoid stale resources in blueprint UIs.
		_ = models.DeleteBlueprintResourcesByResource("service", svc.ID)
		if err := models.DeleteService(svc.ID); err != nil {
			failures = append(failures, strings.TrimSpace(svc.Name))
		} else {
			services.Audit(svc.WorkspaceID, userID, "service.deleted", "service", svc.ID, map[string]interface{}{
				"name":       svc.Name,
				"project_id": projectID,
				"source":     "project.delete",
			})
		}
	}
	if len(failures) > 0 {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete one or more services in this project")
		return
	}

	if err := models.DeleteProject(projectID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete project")
		return
	}

	services.Audit(p.WorkspaceID, userID, "project.deleted", "project", projectID, map[string]interface{}{
		"name":            p.Name,
		"deleted_services": len(svcs),
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *ProjectHandler) ListProjectFolders(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	workspaceID, err := resolveWorkspaceID(r, r.URL.Query().Get("workspace_id"))
	if err != nil || workspaceID == "" {
		utils.RespondError(w, http.StatusBadRequest, "workspace not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	items, err := models.ListProjectFolders(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list folders")
		return
	}
	if items == nil {
		items = []models.ProjectFolder{}
	}
	utils.RespondJSON(w, http.StatusOK, items)
}

func (h *ProjectHandler) CreateProjectFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkspaceID string  `json:"workspace_id"`
		Name        string  `json:"name"`
		ParentID    *string `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		utils.RespondError(w, http.StatusBadRequest, "name is required")
		return
	}

	userID := middleware.GetUserID(r)
	workspaceID, err := resolveWorkspaceID(r, req.WorkspaceID)
	if err != nil || workspaceID == "" {
		utils.RespondError(w, http.StatusBadRequest, "workspace not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Validate parent folder if provided.
	var parentID *string
	if req.ParentID != nil {
		pid := strings.TrimSpace(*req.ParentID)
		if pid != "" {
			parent, err := models.GetProjectFolder(pid)
			if err != nil || parent == nil || parent.WorkspaceID != workspaceID {
				utils.RespondError(w, http.StatusBadRequest, "parent folder not found")
				return
			}
			// Check depth limit: parent's depth + 1 for the new child.
			depth, err := models.FolderDepth(pid)
			if err != nil {
				utils.RespondError(w, http.StatusInternalServerError, "failed to check folder depth")
				return
			}
			if depth+1 > maxFolderDepth {
				utils.RespondError(w, http.StatusBadRequest, "maximum folder nesting depth exceeded")
				return
			}
			parentID = &pid
		}
	}

	f := &models.ProjectFolder{WorkspaceID: workspaceID, Name: req.Name, ParentID: parentID}
	if err := models.CreateProjectFolder(f); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create folder")
		return
	}

	services.Audit(workspaceID, userID, "project_folder.created", "project_folder", f.ID, map[string]interface{}{
		"name":      f.Name,
		"parent_id": f.ParentID,
	})
	utils.RespondJSON(w, http.StatusCreated, f)
}

func (h *ProjectHandler) UpdateProjectFolder(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	folderID := mux.Vars(r)["id"]
	f, err := models.GetProjectFolder(folderID)
	if err != nil || f == nil {
		utils.RespondError(w, http.StatusNotFound, "folder not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, f.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	changed := false
	auditDetails := map[string]interface{}{}

	if rawName, ok := req["name"]; ok {
		name, ok := rawName.(string)
		if !ok {
			utils.RespondError(w, http.StatusBadRequest, "name must be a string")
			return
		}
		name = strings.TrimSpace(name)
		if name == "" {
			utils.RespondError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		if name != f.Name {
			auditDetails["old_name"] = f.Name
			auditDetails["name"] = name
			f.Name = name
			changed = true
		}
	}

	if rawParent, ok := req["parent_id"]; ok {
		switch v := rawParent.(type) {
		case nil:
			if f.ParentID != nil {
				auditDetails["old_parent_id"] = f.ParentID
				auditDetails["parent_id"] = nil
				f.ParentID = nil
				changed = true
			}
		case string:
			pid := strings.TrimSpace(v)
			if pid == "" {
				if f.ParentID != nil {
					auditDetails["old_parent_id"] = f.ParentID
					auditDetails["parent_id"] = nil
					f.ParentID = nil
					changed = true
				}
			} else {
				// Cannot parent to self.
				if pid == folderID {
					utils.RespondError(w, http.StatusBadRequest, "a folder cannot be its own parent")
					return
				}
				// Cannot parent to a descendant (would create a cycle).
				descendants, err := models.FolderDescendantIDs(folderID)
				if err != nil {
					utils.RespondError(w, http.StatusInternalServerError, "failed to validate folder hierarchy")
					return
				}
				for _, did := range descendants {
					if did == pid {
						utils.RespondError(w, http.StatusBadRequest, "cannot move folder into one of its own subfolders")
						return
					}
				}
				parent, err := models.GetProjectFolder(pid)
				if err != nil || parent == nil || parent.WorkspaceID != f.WorkspaceID {
					utils.RespondError(w, http.StatusBadRequest, "parent folder not found")
					return
				}
				// Check depth: new parent's depth + 1 + subtree depth of this folder.
				parentDepth, err := models.FolderDepth(pid)
				if err != nil {
					utils.RespondError(w, http.StatusInternalServerError, "failed to check folder depth")
					return
				}
				if parentDepth+1 > maxFolderDepth {
					utils.RespondError(w, http.StatusBadRequest, "maximum folder nesting depth exceeded")
					return
				}
				if f.ParentID == nil || *f.ParentID != pid {
					auditDetails["old_parent_id"] = f.ParentID
					auditDetails["parent_id"] = pid
					f.ParentID = &pid
					changed = true
				}
			}
		default:
			utils.RespondError(w, http.StatusBadRequest, "parent_id must be a string or null")
			return
		}
	}

	if !changed {
		utils.RespondJSON(w, http.StatusOK, f)
		return
	}

	if err := models.UpdateProjectFolder(f); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update folder")
		return
	}

	services.Audit(f.WorkspaceID, userID, "project_folder.updated", "project_folder", f.ID, auditDetails)
	utils.RespondJSON(w, http.StatusOK, f)
}

func (h *ProjectHandler) DeleteProjectFolder(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	folderID := mux.Vars(r)["id"]
	f, err := models.GetProjectFolder(folderID)
	if err != nil || f == nil {
		utils.RespondError(w, http.StatusNotFound, "folder not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, f.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := models.DeleteProjectFolder(folderID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete folder")
		return
	}

	services.Audit(f.WorkspaceID, userID, "project_folder.deleted", "project_folder", folderID, map[string]interface{}{
		"name": f.Name,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *ProjectHandler) ListProjectEnvironments(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	projectID := mux.Vars(r)["id"]
	p, err := models.GetProject(projectID)
	if err != nil || p == nil {
		utils.RespondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, p.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	envs, err := models.ListEnvironments(projectID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list environments")
		return
	}
	if envs == nil {
		envs = []models.Environment{}
	}
	utils.RespondJSON(w, http.StatusOK, envs)
}

func (h *ProjectHandler) CreateProjectEnvironment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	projectID := mux.Vars(r)["id"]
	p, err := models.GetProject(projectID)
	if err != nil || p == nil {
		utils.RespondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, p.WorkspaceID, models.RoleDeveloper); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req struct {
		Name        string `json:"name"`
		IsProtected bool   `json:"is_protected"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		utils.RespondError(w, http.StatusBadRequest, "name is required")
		return
	}
	e := &models.Environment{
		ProjectID:   p.ID,
		Name:        req.Name,
		IsProtected: req.IsProtected,
	}
	if err := models.CreateEnvironment(e); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create environment")
		return
	}
	services.Audit(p.WorkspaceID, userID, "environment.created", "environment", e.ID, map[string]interface{}{
		"name":         e.Name,
		"is_protected": e.IsProtected,
		"project_id":   p.ID,
	})
	utils.RespondJSON(w, http.StatusCreated, e)
}

func (h *ProjectHandler) UpdateEnvironment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	envID := mux.Vars(r)["id"]
	e, err := models.GetEnvironment(envID)
	if err != nil || e == nil {
		utils.RespondError(w, http.StatusNotFound, "environment not found")
		return
	}
	p, err := models.GetProject(e.ProjectID)
	if err != nil || p == nil {
		utils.RespondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, p.WorkspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req struct {
		Name        *string `json:"name"`
		IsProtected *bool   `json:"is_protected"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != nil {
		e.Name = strings.TrimSpace(*req.Name)
		if e.Name == "" {
			utils.RespondError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
	}
	if req.IsProtected != nil {
		e.IsProtected = *req.IsProtected
	}
	if err := models.UpdateEnvironment(e); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update environment")
		return
	}
	services.Audit(p.WorkspaceID, userID, "environment.updated", "environment", e.ID, map[string]interface{}{
		"name":         e.Name,
		"is_protected": e.IsProtected,
	})
	utils.RespondJSON(w, http.StatusOK, e)
}

func (h *ProjectHandler) DeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	envID := mux.Vars(r)["id"]
	e, err := models.GetEnvironment(envID)
	if err != nil || e == nil {
		utils.RespondError(w, http.StatusNotFound, "environment not found")
		return
	}
	p, err := models.GetProject(e.ProjectID)
	if err != nil || p == nil {
		utils.RespondError(w, http.StatusNotFound, "project not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, p.WorkspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := models.DeleteEnvironment(envID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete environment")
		return
	}
	services.Audit(p.WorkspaceID, userID, "environment.deleted", "environment", envID, map[string]interface{}{
		"name": e.Name,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
