package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
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

type logFilterOptions struct {
	search  string
	regex   bool
	level   string
	since   *time.Time
	until   *time.Time
	pattern *regexp.Regexp
}

func parseLogFilterOptions(r *http.Request) (logFilterOptions, error) {
	var out logFilterOptions
	out.search = strings.TrimSpace(utils.GetQueryString(r, "search", ""))
	out.level = normalizeLogLevel(strings.TrimSpace(utils.GetQueryString(r, "level", "")))

	rawRegex := strings.TrimSpace(strings.ToLower(utils.GetQueryString(r, "regex", "false")))
	out.regex = rawRegex == "1" || rawRegex == "true" || rawRegex == "yes"

	if raw := strings.TrimSpace(utils.GetQueryString(r, "since", "")); raw != "" {
		ts, err := parseLogFilterTime(raw)
		if err != nil {
			return out, fmt.Errorf("invalid since")
		}
		out.since = &ts
	}
	if raw := strings.TrimSpace(utils.GetQueryString(r, "until", "")); raw != "" {
		ts, err := parseLogFilterTime(raw)
		if err != nil {
			return out, fmt.Errorf("invalid until")
		}
		out.until = &ts
	}

	if out.since != nil && out.until != nil && out.since.After(*out.until) {
		return out, fmt.Errorf("since must be <= until")
	}

	if out.search != "" && out.regex {
		re, err := regexp.Compile(`(?i)` + out.search)
		if err != nil {
			return out, fmt.Errorf("invalid search regex")
		}
		out.pattern = re
	}

	if out.level != "" {
		switch out.level {
		case "debug", "info", "warn", "error":
		default:
			return out, fmt.Errorf("invalid level")
		}
	}

	return out, nil
}

func parseLogFilterTime(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func normalizeLogLevel(level string) string {
	level = strings.ToLower(strings.TrimSpace(level))
	switch level {
	case "warning":
		return "warn"
	case "err", "fatal", "panic":
		return "error"
	default:
		return level
	}
}

func inferLogLevel(message string) string {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic") {
		return "error"
	}
	if strings.Contains(lower, "warn") {
		return "warn"
	}
	if strings.Contains(lower, "debug") {
		return "debug"
	}
	return "info"
}

func entryMessage(entry map[string]interface{}) string {
	if v, ok := entry["message"].(string); ok {
		return v
	}
	if v, ok := entry["log"].(string); ok {
		return v
	}
	if v, ok := entry["status"].(string); ok {
		return v
	}
	return fmt.Sprintf("%v", entry)
}

func entryLevel(entry map[string]interface{}) string {
	if v, ok := entry["level"].(string); ok && strings.TrimSpace(v) != "" {
		return normalizeLogLevel(v)
	}
	if v, ok := entry["status"].(string); ok && strings.TrimSpace(v) != "" {
		return normalizeLogLevel(v)
	}
	return inferLogLevel(entryMessage(entry))
}

func entryTimestamp(entry map[string]interface{}) (*time.Time, bool) {
	for _, key := range []string{"timestamp", "started_at", "created_at", "updated_at"} {
		raw, ok := entry[key]
		if !ok || raw == nil {
			continue
		}
		switch v := raw.(type) {
		case time.Time:
			t := v
			return &t, true
		case *time.Time:
			if v != nil {
				t := *v
				return &t, true
			}
		case string:
			if strings.TrimSpace(v) == "" {
				continue
			}
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return &t, true
			}
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return &t, true
			}
		}
	}
	return nil, false
}

func applyLogFilters(entries []map[string]interface{}, filters logFilterOptions, limit int) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		msg := entryMessage(entry)

		if filters.search != "" {
			if filters.regex {
				if filters.pattern == nil || !filters.pattern.MatchString(msg) {
					continue
				}
			} else if !strings.Contains(strings.ToLower(msg), strings.ToLower(filters.search)) {
				continue
			}
		}

		if filters.level != "" {
			if entryLevel(entry) != filters.level {
				continue
			}
		}

		if filters.since != nil || filters.until != nil {
			ts, ok := entryTimestamp(entry)
			if !ok {
				continue
			}
			if filters.since != nil && ts.Before(*filters.since) {
				continue
			}
			if filters.until != nil && ts.After(*filters.until) {
				continue
			}
		}

		out = append(out, entry)
	}

	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func (h *LogHandler) QueryLogs(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	serviceID := mux.Vars(r)["id"]
	limit := utils.GetQueryInt(r, "limit", 100)
	logType := utils.GetQueryString(r, "type", "runtime")
	filters, err := parseLogFilterOptions(r)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
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
		h.queryRuntimeLogs(w, svc, limit, filters)
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

		if h != nil && h.Config != nil && h.Config.Kubernetes.Enabled {
			var startedPtr, finishedPtr *time.Time
			if startedAt.Valid {
				t := startedAt.Time
				startedPtr = &t
			}
			if finishedAt.Valid {
				t := finishedAt.Time
				finishedPtr = &t
			}
			buildLog = hydrateDeployBuildLogFromLoki(h.Config, id, buildLog, startedPtr, finishedPtr)
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
	utils.RespondJSON(w, http.StatusOK, applyLogFilters(logs, filters, limit))
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
	filters, err := parseLogFilterOptions(r)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	svc, err := models.GetService(serviceID)
	if err != nil || svc == nil {
		utils.RespondJSON(w, http.StatusOK, []map[string]interface{}{})
		return
	}

	if logType == "runtime" {
		h.queryRuntimeLogs(w, svc, limit, filters)
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

		if h != nil && h.Config != nil && h.Config.Kubernetes.Enabled {
			var startedPtr, finishedPtr *time.Time
			if startedAt.Valid {
				t := startedAt.Time
				startedPtr = &t
			}
			if finishedAt.Valid {
				t := finishedAt.Time
				finishedPtr = &t
			}
			buildLog = hydrateDeployBuildLogFromLoki(h.Config, id, buildLog, startedPtr, finishedPtr)
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
	utils.RespondJSON(w, http.StatusOK, applyLogFilters(logs, filters, limit))
}

func (h *LogHandler) queryRuntimeLogs(w http.ResponseWriter, svc *models.Service, limit int, filters logFilterOptions) {
	if svc == nil {
		utils.RespondJSON(w, http.StatusOK, []map[string]interface{}{})
		return
	}
	if h != nil && h.Config != nil && h.Config.Kubernetes.Enabled {
		h.queryRuntimeLogsKubernetes(w, svc, limit, filters)
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

		level := inferLogLevel(message)

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
	utils.RespondJSON(w, http.StatusOK, applyLogFilters(logs, filters, limit))
}

func (h *LogHandler) queryRuntimeLogsKubernetes(w http.ResponseWriter, svc *models.Service, limit int, filters logFilterOptions) {
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

			level := inferLogLevel(msg)

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
	utils.RespondJSON(w, http.StatusOK, applyLogFilters(resp, filters, limit))
}
