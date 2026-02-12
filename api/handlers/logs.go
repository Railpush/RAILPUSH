package handlers

import (
	"fmt"
	"net/http"
	"os/exec"
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

type LogHandler struct {
	Config *config.Config
}

func NewLogHandler(cfg *config.Config) *LogHandler {
	return &LogHandler{Config: cfg}
}

func (h *LogHandler) QueryLogs(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	serviceID := mux.Vars(r)["id"]
	limit := utils.GetQueryInt(r, "limit", 100)
	logType := utils.GetQueryString(r, "type", "runtime")
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondJSON(w, http.StatusOK, []map[string]interface{}{})
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	if logType == "runtime" {
		h.queryRuntimeLogs(w, svc, limit)
		return
	}

	// Deploy logs (build logs from deploys table)
	var logs []map[string]interface{}
	rows, err := database.DB.Query("SELECT id, COALESCE(status,''), COALESCE(build_log,''), started_at FROM deploys WHERE service_id=$1 ORDER BY started_at DESC NULLS LAST LIMIT $2", svc.ID, limit)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to query logs")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, status, buildLog string
		var startedAt interface{}
		if err := rows.Scan(&id, &status, &buildLog, &startedAt); err != nil {
			continue
		}
		logs = append(logs, map[string]interface{}{"id": id, "status": status, "log": buildLog, "started_at": startedAt})
	}
	if logs == nil {
		logs = []map[string]interface{}{}
	}
	utils.RespondJSON(w, http.StatusOK, logs)
}

func (h *LogHandler) queryRuntimeLogs(w http.ResponseWriter, svc *models.Service, limit int) {
	if svc == nil {
		utils.RespondJSON(w, http.StatusOK, []map[string]interface{}{})
		return
	}
	if svc.ContainerID == "" {
		utils.RespondJSON(w, http.StatusOK, []map[string]interface{}{})
		return
	}

	// Fetch docker logs with timestamps
	out, err := exec.Command("docker", "logs", "--tail", fmt.Sprintf("%d", limit), "--timestamps", svc.ContainerID).CombinedOutput()
	if err != nil {
		utils.RespondJSON(w, http.StatusOK, []map[string]interface{}{})
		return
	}

	var logs []map[string]interface{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		timestamp := time.Now().Format(time.RFC3339)
		message := line

		// Docker --timestamps format: "2024-01-01T00:00:00.000000000Z message"
		if len(line) > 30 && (line[4] == '-' || line[10] == 'T') {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				timestamp = parts[0]
				message = parts[1]
			}
		}

		level := "info"
		lower := strings.ToLower(message)
		if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic") {
			level = "error"
		} else if strings.Contains(lower, "warn") {
			level = "warn"
		} else if strings.Contains(lower, "debug") {
			level = "debug"
		}

		cid := svc.ContainerID
		if len(cid) > 12 {
			cid = cid[:12]
		}

		logs = append(logs, map[string]interface{}{
			"timestamp":   timestamp,
			"level":       level,
			"message":     message,
			"instance_id": cid,
		})
	}
	if logs == nil {
		logs = []map[string]interface{}{}
	}
	utils.RespondJSON(w, http.StatusOK, logs)
}
