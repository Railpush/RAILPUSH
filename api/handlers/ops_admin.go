package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/database"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type OpsAdminHandler struct {
	Config *config.Config
	Worker *services.Worker
}

func NewOpsAdminHandler(cfg *config.Config, worker *services.Worker) *OpsAdminHandler {
	return &OpsAdminHandler{Config: cfg, Worker: worker}
}

func (h *OpsAdminHandler) ensureOps(w http.ResponseWriter, r *http.Request) bool {
	userID := middleware.GetUserID(r)
	if err := services.EnsureOpsAccess(userID); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func normalizeUserRole(raw string) (string, bool) {
	role := strings.ToLower(strings.TrimSpace(raw))
	switch role {
	case "member", "admin", "ops":
		return role, true
	default:
		return "", false
	}
}

func (h *OpsAdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	actorID := middleware.GetUserID(r)
	targetID := strings.TrimSpace(mux.Vars(r)["id"])

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role, ok := normalizeUserRole(req.Role)
	if !ok {
		utils.RespondError(w, http.StatusBadRequest, "invalid role")
		return
	}

	var before string
	_ = database.DB.QueryRow("SELECT COALESCE(role,'member') FROM users WHERE id=$1", targetID).Scan(&before)
	if strings.TrimSpace(before) == "" {
		utils.RespondError(w, http.StatusNotFound, "user not found")
		return
	}

	if _, err := database.DB.Exec("UPDATE users SET role=$1 WHERE id=$2", role, targetID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update user")
		return
	}

	_ = models.CreateAuditLog("", actorID, "ops.user.role_changed", "user", targetID, map[string]interface{}{
		"before": before,
		"after":  role,
	})

	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *OpsAdminHandler) SuspendUser(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	actorID := middleware.GetUserID(r)
	targetID := strings.TrimSpace(mux.Vars(r)["id"])

	res, err := database.DB.Exec("UPDATE users SET is_suspended=true, suspended_at=NOW() WHERE id=$1", targetID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to suspend user")
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		utils.RespondError(w, http.StatusNotFound, "user not found")
		return
	}

	_ = models.CreateAuditLog("", actorID, "ops.user.suspended", "user", targetID, nil)
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *OpsAdminHandler) ResumeUser(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	actorID := middleware.GetUserID(r)
	targetID := strings.TrimSpace(mux.Vars(r)["id"])

	res, err := database.DB.Exec("UPDATE users SET is_suspended=false, suspended_at=NULL WHERE id=$1", targetID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to resume user")
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		utils.RespondError(w, http.StatusNotFound, "user not found")
		return
	}

	_ = models.CreateAuditLog("", actorID, "ops.user.resumed", "user", targetID, nil)
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *OpsAdminHandler) SuspendWorkspace(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	actorID := middleware.GetUserID(r)
	wsID := strings.TrimSpace(mux.Vars(r)["id"])

	res, err := database.DB.Exec("UPDATE workspaces SET is_suspended=true, suspended_at=NOW() WHERE id=$1", wsID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to suspend workspace")
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		utils.RespondError(w, http.StatusNotFound, "workspace not found")
		return
	}

	// Best-effort: suspend all services in the workspace.
	svcs, err := models.ListServices(wsID)
	if err == nil && len(svcs) > 0 {
		for i := range svcs {
			svc := svcs[i]
			_ = models.SetServiceSuspended(svc.ID, true)
			_ = models.UpdateServiceStatus(svc.ID, "suspended", svc.ContainerID, svc.HostPort)
			if h.Config != nil && h.Config.Kubernetes.Enabled && h.Worker != nil {
				if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
					_ = kd.ScaleService(&svc, 0)
				}
			} else if h.Worker != nil && svc.ContainerID != "" {
				h.Worker.Deployer.StopContainer(svc.ContainerID)
			}
		}
	}

	_ = models.CreateAuditLog(wsID, actorID, "ops.workspace.suspended", "workspace", wsID, map[string]interface{}{
		"suspended_services": len(svcs),
	})
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *OpsAdminHandler) ResumeWorkspace(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	actorID := middleware.GetUserID(r)
	wsID := strings.TrimSpace(mux.Vars(r)["id"])

	res, err := database.DB.Exec("UPDATE workspaces SET is_suspended=false, suspended_at=NULL WHERE id=$1", wsID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to resume workspace")
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		utils.RespondError(w, http.StatusNotFound, "workspace not found")
		return
	}

	// Best-effort: resume all services in the workspace that are currently suspended.
	resumed := 0
	svcs, err := models.ListServices(wsID)
	if err == nil && len(svcs) > 0 {
		var kd *services.KubeDeployer
		if h.Config != nil && h.Config.Kubernetes.Enabled && h.Worker != nil {
			if d, err := h.Worker.GetKubeDeployer(); err == nil && d != nil {
				kd = d
			}
		}
		for i := range svcs {
			svc := svcs[i]
			if !svc.IsSuspended {
				continue
			}
			_ = models.SetServiceSuspended(svc.ID, false)
			_ = models.UpdateServiceStatus(svc.ID, "deploying", svc.ContainerID, svc.HostPort)
			resumed++

			go func(s models.Service) {
				if kd != nil {
					desired := int32(1)
					if s.Instances > 0 {
						desired = int32(s.Instances)
					}
					if err := kd.ScaleService(&s, desired); err != nil {
						_ = models.UpdateServiceStatus(s.ID, "deploy_failed", s.ContainerID, s.HostPort)
						return
					}
					_ = models.UpdateServiceStatus(s.ID, "live", s.ContainerID, s.HostPort)
					return
				}
				if h.Worker != nil && s.ContainerID != "" {
					if err := h.Worker.Deployer.StartContainer(s.ContainerID); err != nil {
						_ = models.UpdateServiceStatus(s.ID, "deploy_failed", s.ContainerID, s.HostPort)
						return
					}
					_ = models.UpdateServiceStatus(s.ID, "live", s.ContainerID, s.HostPort)
					return
				}
				_ = models.UpdateServiceStatus(s.ID, "deploy_failed", s.ContainerID, s.HostPort)
			}(svc)
		}
	}

	_ = models.CreateAuditLog(wsID, actorID, "ops.workspace.resumed", "workspace", wsID, map[string]interface{}{
		"resumed_services": resumed,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "resumed_services": resumed})
}

func (h *OpsAdminHandler) RestartService(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	actorID := middleware.GetUserID(r)
	id := strings.TrimSpace(mux.Vars(r)["id"])

	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}

	models.UpdateServiceStatus(id, "restarting", svc.ContainerID, svc.HostPort)
	go func() {
		if h.Config != nil && h.Config.Kubernetes.Enabled && h.Worker != nil {
			if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
				if err := kd.RestartService(svc); err != nil {
					_ = models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
					return
				}
				_ = models.UpdateServiceStatus(id, "live", svc.ContainerID, svc.HostPort)
				return
			}
		}
		if h.Worker != nil && svc.ContainerID != "" {
			if err := h.Worker.Deployer.RestartContainer(svc.ContainerID); err != nil {
				_ = models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
				return
			}
			_ = models.UpdateServiceStatus(id, "live", svc.ContainerID, svc.HostPort)
			return
		}
		_ = models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
	}()

	services.Audit(svc.WorkspaceID, actorID, "ops.service.restarted", "service", id, nil)
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "restarting"})
}

func (h *OpsAdminHandler) SuspendService(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	actorID := middleware.GetUserID(r)
	id := strings.TrimSpace(mux.Vars(r)["id"])

	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := models.SetServiceSuspended(id, true); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to suspend service")
		return
	}
	_ = models.UpdateServiceStatus(id, "suspended", svc.ContainerID, svc.HostPort)

	if h.Config != nil && h.Config.Kubernetes.Enabled && h.Worker != nil {
		if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
			_ = kd.ScaleService(svc, 0)
		}
	} else if h.Worker != nil && svc.ContainerID != "" {
		h.Worker.Deployer.StopContainer(svc.ContainerID)
	}

	services.Audit(svc.WorkspaceID, actorID, "ops.service.suspended", "service", id, nil)
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "suspended"})
}

func (h *OpsAdminHandler) ResumeService(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	actorID := middleware.GetUserID(r)
	id := strings.TrimSpace(mux.Vars(r)["id"])

	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := models.SetServiceSuspended(id, false); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to resume service")
		return
	}
	_ = models.UpdateServiceStatus(id, "deploying", svc.ContainerID, svc.HostPort)

	go func() {
		if h.Config != nil && h.Config.Kubernetes.Enabled && h.Worker != nil {
			if kd, err := h.Worker.GetKubeDeployer(); err == nil && kd != nil {
				desired := int32(1)
				if svc.Instances > 0 {
					desired = int32(svc.Instances)
				}
				if err := kd.ScaleService(svc, desired); err != nil {
					_ = models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
					return
				}
				_ = models.UpdateServiceStatus(id, "live", svc.ContainerID, svc.HostPort)
				return
			}
		}
		if h.Worker != nil && svc.ContainerID != "" {
			if err := h.Worker.Deployer.StartContainer(svc.ContainerID); err != nil {
				_ = models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
				return
			}
			_ = models.UpdateServiceStatus(id, "live", svc.ContainerID, svc.HostPort)
			return
		}
		_ = models.UpdateServiceStatus(id, "deploy_failed", svc.ContainerID, svc.HostPort)
	}()

	services.Audit(svc.WorkspaceID, actorID, "ops.service.resumed", "service", id, nil)
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deploying"})
}

type opsDatastoreItem struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	WorkspaceID   string    `json:"workspace_id"`
	WorkspaceName string    `json:"workspace_name"`
	OwnerEmail    string    `json:"owner_email"`
	Name          string    `json:"name"`
	Plan          string    `json:"plan"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

func (h *OpsAdminHandler) ListDatastores(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	limit, offset := parseLimitOffset(r)
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	like := "%" + q + "%"

	// Union databases + keyvalue with consistent shape.
	whereKind := ""
	if kind == "postgres" || kind == "keyvalue" {
		whereKind = kind
	}

	rows, err := database.DB.Query(
		`SELECT id, kind, workspace_id, workspace_name, owner_email, name, plan, status, created_at
		   FROM (
		         SELECT d.id::text AS id,
		                'postgres' AS kind,
		                COALESCE(d.workspace_id::text,'') AS workspace_id,
		                COALESCE(w.name,'') AS workspace_name,
		                COALESCE(u.email,'') AS owner_email,
		                COALESCE(d.name,'') AS name,
		                COALESCE(d.plan,'') AS plan,
		                COALESCE(d.status,'') AS status,
		                d.created_at AS created_at
		           FROM managed_databases d
		           LEFT JOIN workspaces w ON w.id = d.workspace_id
		           LEFT JOIN users u ON u.id = w.owner_id
		         UNION ALL
		         SELECT k.id::text AS id,
		                'keyvalue' AS kind,
		                COALESCE(k.workspace_id::text,'') AS workspace_id,
		                COALESCE(w.name,'') AS workspace_name,
		                COALESCE(u.email,'') AS owner_email,
		                COALESCE(k.name,'') AS name,
		                COALESCE(k.plan,'') AS plan,
		                COALESCE(k.status,'') AS status,
		                k.created_at AS created_at
		           FROM managed_keyvalue k
		           LEFT JOIN workspaces w ON w.id = k.workspace_id
		           LEFT JOIN users u ON u.id = w.owner_id
		        ) t
		  WHERE ($1 = '' OR t.kind = $1)
		    AND ($2 = '' OR t.name ILIKE $3 OR t.workspace_name ILIKE $3 OR t.owner_email ILIKE $3)
		  ORDER BY t.created_at DESC
		  LIMIT $4 OFFSET $5`,
		whereKind, q, like, limit, offset,
	)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list datastores")
		return
	}
	defer rows.Close()

	var out []opsDatastoreItem
	for rows.Next() {
		var it opsDatastoreItem
		if err := rows.Scan(&it.ID, &it.Kind, &it.WorkspaceID, &it.WorkspaceName, &it.OwnerEmail, &it.Name, &it.Plan, &it.Status, &it.CreatedAt); err != nil {
			continue
		}
		out = append(out, it)
	}
	if out == nil {
		out = []opsDatastoreItem{}
	}
	utils.RespondJSON(w, http.StatusOK, out)
}

type opsAuditItem struct {
	ID            string    `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
	WorkspaceName string    `json:"workspace_name"`
	UserID        string    `json:"user_id"`
	ActorEmail    string    `json:"actor_email"`
	Action        string    `json:"action"`
	ResourceType  string    `json:"resource_type"`
	ResourceID    string    `json:"resource_id"`
	CreatedAt     time.Time `json:"created_at"`
}

func (h *OpsAdminHandler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	limit, offset := parseLimitOffset(r)
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	like := "%" + q + "%"

	rows, err := database.DB.Query(
		`SELECT a.id::text,
		        COALESCE(a.workspace_id::text,'') AS workspace_id,
		        COALESCE(w.name,'') AS workspace_name,
		        COALESCE(a.user_id::text,'') AS user_id,
		        COALESCE(u.email,'') AS actor_email,
		        COALESCE(a.action,'') AS action,
		        COALESCE(a.resource_type,'') AS resource_type,
		        COALESCE(a.resource_id::text,'') AS resource_id,
		        a.created_at
		   FROM audit_log a
		   LEFT JOIN workspaces w ON w.id = a.workspace_id
		   LEFT JOIN users u ON u.id = a.user_id
		  WHERE ($1 = '' OR COALESCE(a.action,'') ILIKE $2
		                 OR COALESCE(u.email,'') ILIKE $2
		                 OR COALESCE(w.name,'') ILIKE $2
		                 OR COALESCE(a.resource_type,'') ILIKE $2)
		  ORDER BY a.created_at DESC
		  LIMIT $3 OFFSET $4`,
		q, like, limit, offset,
	)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list audit logs")
		return
	}
	defer rows.Close()

	var out []opsAuditItem
	for rows.Next() {
		var it opsAuditItem
		if err := rows.Scan(&it.ID, &it.WorkspaceID, &it.WorkspaceName, &it.UserID, &it.ActorEmail, &it.Action, &it.ResourceType, &it.ResourceID, &it.CreatedAt); err != nil {
			continue
		}
		out = append(out, it)
	}
	if out == nil {
		out = []opsAuditItem{}
	}
	utils.RespondJSON(w, http.StatusOK, out)
}
