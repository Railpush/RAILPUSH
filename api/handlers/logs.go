package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/database"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type LogHandler struct {
	Config *config.Config
	Worker *services.Worker
}

func NewLogHandler(cfg *config.Config, worker *services.Worker) *LogHandler {
	return &LogHandler{Config: cfg, Worker: worker}
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
	rows, err := database.DB.Query("SELECT id, COALESCE(status,''), COALESCE(build_log,''), started_at, finished_at FROM deploys WHERE service_id=$1 ORDER BY started_at DESC NULLS LAST LIMIT $2", svc.ID, limit)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to query logs")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, status, buildLog string
		var startedAt, finishedAt sql.NullTime
		if err := rows.Scan(&id, &status, &buildLog, &startedAt, &finishedAt); err != nil {
			continue
		}

		// In Kubernetes mode, build output is shipped to Loki via Promtail.
		// Keep DB output as metadata-only and hydrate with Loki at query time.
		if h != nil && h.Config != nil && h.Config.Kubernetes.Enabled {
			// Avoid duplicating logs for older deploys that still have build output persisted.
			if !strings.Contains(buildLog, "\n    ") && !strings.HasPrefix(buildLog, "    ") {
				ns := strings.TrimSpace(h.Config.Kubernetes.Namespace)
				if ns == "" {
					ns = "railpush"
				}
				lokiURL := strings.TrimSpace(h.Config.Logging.LokiURL)
				if lokiURL == "" {
					// Default matches deploy/k8s/logging/* (Loki gateway is cluster-internal).
					lokiURL = "http://loki-gateway.logging.svc.cluster.local"
				}

				jobName := services.KubeBuildJobName(id)
				if jobName != "" && lokiURL != "" {
					start := time.Now().UTC().Add(-30 * time.Minute)
					if startedAt.Valid {
						start = startedAt.Time.Add(-2 * time.Minute)
					}
					end := time.Now().UTC()
					if finishedAt.Valid {
						end = finishedAt.Time.Add(5 * time.Minute)
					}

					// Bound queries to avoid accidental huge scans.
					if end.Sub(start) > 6*time.Hour {
						start = end.Add(-6 * time.Hour)
					}

					logQL := fmt.Sprintf(`{namespace=%q, app=%q, component="build", container=~"clone|kaniko"}`, ns, jobName)
					ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
					lines, err := services.LokiQueryRange(ctx, lokiURL, logQL, start, end, 5000)
					cancel()
					if err == nil && len(lines) > 0 {
						if buildLog != "" && !strings.HasSuffix(buildLog, "\n") {
							buildLog += "\n"
						}
						buildLog += "==> Build logs (Loki):\n"
						// Cap output to keep the API response bounded.
						const maxBytes = 512 * 1024
						var bytes int
						for _, ln := range lines {
							container := ""
							if ln.Labels != nil {
								container = strings.TrimSpace(ln.Labels["container"])
							}
							prefix := "    "
							if container != "" {
								prefix = "    [" + container + "] "
							}
							line := prefix + strings.TrimRight(ln.Line, "\r\n")
							if line == "" {
								continue
							}
							if bytes+len(line)+1 > maxBytes {
								buildLog += "    (truncated; view full logs in Grafana Loki)\n"
								break
							}
							buildLog += line + "\n"
							bytes += len(line) + 1
						}
					}
				}
			}
		}

		var startedAtVal interface{} = nil
		if startedAt.Valid {
			startedAtVal = startedAt.Time
		}
		logs = append(logs, map[string]interface{}{"id": id, "status": status, "log": buildLog, "started_at": startedAtVal})
	}
	if logs == nil {
		logs = []map[string]interface{}{}
	}
	utils.RespondJSON(w, http.StatusOK, logs)
}

// QueryLogsOps is the ops-scoped version of QueryLogs (bypasses workspace RBAC).
func (h *LogHandler) QueryLogsOps(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	if err := services.EnsureOpsAccess(userID); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	serviceID := mux.Vars(r)["id"]
	limit := utils.GetQueryInt(r, "limit", 100)
	logType := utils.GetQueryString(r, "type", "runtime")
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondJSON(w, http.StatusOK, []map[string]interface{}{})
		return
	}

	if logType == "runtime" {
		h.queryRuntimeLogs(w, svc, limit)
		return
	}

	// Deploy logs (build logs from deploys table)
	var logs []map[string]interface{}
	rows, err := database.DB.Query("SELECT id, COALESCE(status,''), COALESCE(build_log,''), started_at, finished_at FROM deploys WHERE service_id=$1 ORDER BY started_at DESC NULLS LAST LIMIT $2", svc.ID, limit)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to query logs")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, status, buildLog string
		var startedAt, finishedAt sql.NullTime
		if err := rows.Scan(&id, &status, &buildLog, &startedAt, &finishedAt); err != nil {
			continue
		}

		// In Kubernetes mode, build output is shipped to Loki via Promtail.
		// Keep DB output as metadata-only and hydrate with Loki at query time.
		if h != nil && h.Config != nil && h.Config.Kubernetes.Enabled {
			// Avoid duplicating logs for older deploys that still have build output persisted.
			if !strings.Contains(buildLog, "\n    ") && !strings.HasPrefix(buildLog, "    ") {
				ns := strings.TrimSpace(h.Config.Kubernetes.Namespace)
				if ns == "" {
					ns = "railpush"
				}
				lokiURL := strings.TrimSpace(h.Config.Logging.LokiURL)
				if lokiURL == "" {
					// Default matches deploy/k8s/logging/* (Loki gateway is cluster-internal).
					lokiURL = "http://loki-gateway.logging.svc.cluster.local"
				}

				jobName := services.KubeBuildJobName(id)
				if jobName != "" && lokiURL != "" {
					start := time.Now().UTC().Add(-30 * time.Minute)
					if startedAt.Valid {
						start = startedAt.Time.Add(-2 * time.Minute)
					}
					end := time.Now().UTC()
					if finishedAt.Valid {
						end = finishedAt.Time.Add(5 * time.Minute)
					}

					// Bound queries to avoid accidental huge scans.
					if end.Sub(start) > 6*time.Hour {
						start = end.Add(-6 * time.Hour)
					}

					logQL := fmt.Sprintf(`{namespace=%q, app=%q, component="build", container=~"clone|kaniko"}`, ns, jobName)
					ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
					lines, err := services.LokiQueryRange(ctx, lokiURL, logQL, start, end, 5000)
					cancel()
					if err == nil && len(lines) > 0 {
						if buildLog != "" && !strings.HasSuffix(buildLog, "\n") {
							buildLog += "\n"
						}
						buildLog += "==> Build logs (Loki):\n"
						// Cap output to keep the API response bounded.
						const maxBytes = 512 * 1024
						var bytes int
						for _, ln := range lines {
							container := ""
							if ln.Labels != nil {
								container = strings.TrimSpace(ln.Labels["container"])
							}
							prefix := "    "
							if container != "" {
								prefix = "    [" + container + "] "
							}
							line := prefix + strings.TrimRight(ln.Line, "\r\n")
							if line == "" {
								continue
							}
							if bytes+len(line)+1 > maxBytes {
								buildLog += "    (truncated; view full logs in Grafana Loki)\n"
								break
							}
							buildLog += line + "\n"
							bytes += len(line) + 1
						}
					}
				}
			}
		}

		var startedAtVal interface{} = nil
		if startedAt.Valid {
			startedAtVal = startedAt.Time
		}
		logs = append(logs, map[string]interface{}{"id": id, "status": status, "log": buildLog, "started_at": startedAtVal})
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
	if h != nil && h.Config != nil && h.Config.Kubernetes.Enabled {
		h.queryRuntimeLogsKubernetes(w, svc, limit)
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

func (h *LogHandler) queryRuntimeLogsKubernetes(w http.ResponseWriter, svc *models.Service, limit int) {
	if h == nil || h.Worker == nil || h.Config == nil || svc == nil {
		utils.RespondJSON(w, http.StatusOK, []map[string]interface{}{})
		return
	}
	kd, err := h.Worker.GetKubeDeployer()
	if err != nil || kd == nil || kd.Client == nil {
		utils.RespondJSON(w, http.StatusOK, []map[string]interface{}{})
		return
	}

	ns := strings.TrimSpace(h.Config.Kubernetes.Namespace)
	if ns == "" {
		ns = "railpush"
	}
	labelSel := "railpush.com/service-id=" + svc.ID

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pods, err := kd.Client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: labelSel})
	if err != nil || len(pods.Items) == 0 {
		utils.RespondJSON(w, http.StatusOK, []map[string]interface{}{})
		return
	}

	tail := int64(limit)
	if tail <= 0 {
		tail = 100
	}

	type entry struct {
		ts      time.Time
		payload map[string]interface{}
	}
	var out []entry

	for _, pod := range pods.Items {
		podName := pod.Name
		logsRaw, err := kd.Client.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{
			Container:  "service",
			Timestamps: true,
			TailLines:  &tail,
		}).DoRaw(ctx)
		if err != nil || len(logsRaw) == 0 {
			continue
		}
		for _, line := range strings.Split(string(logsRaw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Kubernetes timestamps format: "2026-02-13T00:00:00.000000000Z message"
			parts := strings.SplitN(line, " ", 2)
			tsStr := ""
			msg := line
			if len(parts) == 2 {
				tsStr = parts[0]
				msg = parts[1]
			}
			ts := time.Now().UTC()
			if tsStr != "" {
				if parsed, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
					ts = parsed
				}
			}

			level := "info"
			lower := strings.ToLower(msg)
			if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic") {
				level = "error"
			} else if strings.Contains(lower, "warn") {
				level = "warn"
			} else if strings.Contains(lower, "debug") {
				level = "debug"
			}

			out = append(out, entry{
				ts: ts,
				payload: map[string]interface{}{
					"timestamp":   ts.Format(time.RFC3339Nano),
					"level":       level,
					"message":     msg,
					"instance_id": podName,
				},
			})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ts.Before(out[j].ts) })
	if len(out) > limit && limit > 0 {
		out = out[len(out)-limit:]
	}

	resp := make([]map[string]interface{}, 0, len(out))
	for _, e := range out {
		resp = append(resp, e.payload)
	}
	utils.RespondJSON(w, http.StatusOK, resp)
}
