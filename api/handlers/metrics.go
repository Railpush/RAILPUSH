package handlers

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type MetricsHandler struct {
	Config *config.Config
}

func NewMetricsHandler(cfg *config.Config) *MetricsHandler {
	return &MetricsHandler{Config: cfg}
}

// GetServiceMetrics returns current docker stats for the service's container
func (h *MetricsHandler) GetServiceMetrics(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	serviceID := mux.Vars(r)["id"]

	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	if svc.ContainerID == "" || svc.Status != "live" {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"cpu_percent":    0,
			"memory_mb":      0,
			"memory_percent": 0,
			"network_in":     "0B",
			"network_out":    "0B",
			"status":         svc.Status,
		})
		return
	}

	// Run docker stats --no-stream for the container
	out, err := exec.Command("docker", "stats", "--no-stream", "--format",
		"{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.PIDs}}",
		svc.ContainerID).Output()
	if err != nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"cpu_percent":    0,
			"memory_mb":      0,
			"memory_percent": 0,
			"status":         svc.Status,
			"error":          "container stats unavailable",
		})
		return
	}

	parts := strings.Split(strings.TrimSpace(string(out)), "\t")
	if len(parts) < 5 {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"raw": string(out)})
		return
	}

	// Parse CPU percentage
	cpuStr := strings.TrimSuffix(parts[0], "%")
	// Parse memory
	memParts := strings.Split(parts[1], " / ")
	memUsed := ""
	memTotal := ""
	if len(memParts) == 2 {
		memUsed = strings.TrimSpace(memParts[0])
		memTotal = strings.TrimSpace(memParts[1])
	}
	memPerc := strings.TrimSuffix(parts[2], "%")
	// Parse network
	netParts := strings.Split(parts[3], " / ")
	netIn := ""
	netOut := ""
	if len(netParts) == 2 {
		netIn = strings.TrimSpace(netParts[0])
		netOut = strings.TrimSpace(netParts[1])
	}

	result := map[string]interface{}{
		"cpu_percent":    cpuStr,
		"memory_used":    memUsed,
		"memory_total":   memTotal,
		"memory_percent": memPerc,
		"network_in":     netIn,
		"network_out":    netOut,
		"pids":           parts[4],
		"status":         svc.Status,
		"container_id":   svc.ContainerID,
		"timestamp":      time.Now().Format(time.RFC3339),
	}

	utils.RespondJSON(w, http.StatusOK, result)
}

// GetServiceMetricsHistory returns metrics history (stored or simulated from docker stats)
func (h *MetricsHandler) GetServiceMetricsHistory(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	serviceID := mux.Vars(r)["id"]

	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := services.EnsureWorkspaceAccess(userID, svc.WorkspaceID, models.RoleViewer); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	if svc.ContainerID == "" || svc.Status != "live" {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"cpu": []interface{}{}, "memory": []interface{}{},
		})
		return
	}

	// Get current snapshot from docker stats
	out, err := exec.Command("docker", "stats", "--no-stream", "--format", "{{json .}}", svc.ContainerID).Output()
	if err != nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"cpu": []interface{}{}, "memory": []interface{}{},
		})
		return
	}

	var stats map[string]interface{}
	json.Unmarshal(out, &stats)

	now := time.Now()
	result := map[string]interface{}{
		"current":   stats,
		"timestamp": now.Format(time.RFC3339),
		"status":    svc.Status,
	}

	utils.RespondJSON(w, http.StatusOK, result)
}
