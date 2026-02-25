package handlers

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

const defaultPrometheusURL = "http://monitoring-kube-prometheus-prometheus.monitoring.svc:9090"

type MetricsHandler struct {
	Config *config.Config
}

type metricsPeriodSpec struct {
	Key      string
	Duration time.Duration
	Step     time.Duration
}

var metricsPeriods = []metricsPeriodSpec{
	{Key: "1h", Duration: time.Hour, Step: 30 * time.Second},
	{Key: "6h", Duration: 6 * time.Hour, Step: 2 * time.Minute},
	{Key: "24h", Duration: 24 * time.Hour, Step: 5 * time.Minute},
	{Key: "7d", Duration: 7 * 24 * time.Hour, Step: 30 * time.Minute},
	{Key: "30d", Duration: 30 * 24 * time.Hour, Step: 2 * time.Hour},
}

func NewMetricsHandler(cfg *config.Config) *MetricsHandler {
	return &MetricsHandler{Config: cfg}
}

func parseMetricsPeriod(raw string) metricsPeriodSpec {
	raw = strings.ToLower(strings.TrimSpace(raw))
	for _, period := range metricsPeriods {
		if raw == period.Key {
			return period
		}
	}
	for _, period := range metricsPeriods {
		if period.Key == "24h" {
			return period
		}
	}
	return metricsPeriods[0]
}

func (h *MetricsHandler) prometheusURL() string {
	if h == nil || h.Config == nil {
		return ""
	}
	if url := strings.TrimSpace(h.Config.Logging.PrometheusURL); url != "" {
		return strings.TrimSuffix(url, "/")
	}
	if h.Config.Kubernetes.Enabled {
		return defaultPrometheusURL
	}
	return ""
}

func (h *MetricsHandler) kubeNamespace() string {
	if h == nil || h.Config == nil {
		return "railpush"
	}
	ns := strings.TrimSpace(h.Config.Kubernetes.Namespace)
	if ns == "" {
		ns = "railpush"
	}
	return ns
}

func metricsServicePodRegex(serviceID string) string {
	return regexp.QuoteMeta(services.KubeServiceName(serviceID)) + ".*"
}

func metricPoints(samples []services.PrometheusSample) []map[string]interface{} {
	if len(samples) == 0 {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(samples))
	for _, sample := range samples {
		out = append(out, map[string]interface{}{
			"timestamp": sample.Timestamp.UTC().Format(time.RFC3339),
			"value":     roundMetricValue(sample.Value, 4),
		})
	}
	return out
}

func latestMetricValue(samples []services.PrometheusSample) float64 {
	if len(samples) == 0 {
		return 0
	}
	return samples[len(samples)-1].Value
}

func roundMetricValue(value float64, precision int) float64 {
	if precision < 0 {
		precision = 0
	}
	pow := math.Pow(10, float64(precision))
	return math.Round(value*pow) / pow
}

func formatMiB(value float64) string {
	if value <= 0 {
		return "0MiB"
	}
	if value >= 1024 {
		return fmt.Sprintf("%.2fGiB", value/1024)
	}
	return fmt.Sprintf("%.0fMiB", value)
}

func formatBytesPerSecond(value float64) string {
	if value <= 0 {
		return "0B/s"
	}
	units := []string{"B/s", "KiB/s", "MiB/s", "GiB/s", "TiB/s"}
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value = value / 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%.0f%s", value, units[unit])
	}
	return fmt.Sprintf("%.2f%s", value, units[unit])
}

func (h *MetricsHandler) queryServicePrometheusMetrics(ctx context.Context, svc *models.Service, start time.Time, end time.Time, step time.Duration) (map[string][]services.PrometheusSample, error) {
	promURL := h.prometheusURL()
	if promURL == "" {
		return nil, fmt.Errorf("prometheus url is not configured")
	}
	namespace := h.kubeNamespace()
	podRegex := metricsServicePodRegex(svc.ID)

	queries := map[string]string{
		"cpu_percent": fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace=%q,pod=~%q,container="service"}[5m])) * 100`, namespace, podRegex),
		"memory_mb": fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace=%q,pod=~%q,container="service"}) / 1024 / 1024`, namespace, podRegex),
		"memory_limit_mb": fmt.Sprintf(`sum(kube_pod_container_resource_limits{namespace=%q,pod=~%q,container="service",resource="memory",unit="byte"}) / 1024 / 1024`, namespace, podRegex),
		"network_in_bps": fmt.Sprintf(`sum(rate(container_network_receive_bytes_total{namespace=%q,pod=~%q,interface!="lo"}[5m]))`, namespace, podRegex),
		"network_out_bps": fmt.Sprintf(`sum(rate(container_network_transmit_bytes_total{namespace=%q,pod=~%q,interface!="lo"}[5m]))`, namespace, podRegex),
	}

	series := map[string][]services.PrometheusSample{}
	var mu sync.Mutex
	errors := make([]string, 0)

	var wg sync.WaitGroup
	for key, query := range queries {
		wg.Add(1)
		go func(metricKey string, promQL string) {
			defer wg.Done()
			samples, err := services.PrometheusQueryRange(ctx, promURL, promQL, start, end, step)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errors = append(errors, metricKey+": "+err.Error())
				series[metricKey] = []services.PrometheusSample{}
				return
			}
			series[metricKey] = samples
		}(key, query)
	}
	wg.Wait()

	haveData := false
	for _, key := range []string{"cpu_percent", "memory_mb", "network_in_bps", "network_out_bps", "memory_limit_mb"} {
		if samples, ok := series[key]; ok && len(samples) > 0 {
			haveData = true
			break
		}
	}

	if !haveData && len(errors) > 0 {
		return series, fmt.Errorf(strings.Join(errors, "; "))
	}

	return series, nil
}

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

	if svc.Status != "live" {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"cpu_percent":    "0",
			"memory_used":    "0MiB",
			"memory_total":   "",
			"memory_percent": "0",
			"network_in":     "0B/s",
			"network_out":    "0B/s",
			"pids":           "0",
			"status":         svc.Status,
			"container_id":   svc.ContainerID,
			"timestamp":      time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	if h != nil && h.Config != nil && h.Config.Kubernetes.Enabled {
		end := time.Now().UTC()
		start := end.Add(-15 * time.Minute)
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()

		series, promErr := h.queryServicePrometheusMetrics(ctx, svc, start, end, time.Minute)
		if promErr == nil {
			cpu := latestMetricValue(series["cpu_percent"])
			memoryMB := latestMetricValue(series["memory_mb"])
			memoryLimitMB := latestMetricValue(series["memory_limit_mb"])
			netInBPS := latestMetricValue(series["network_in_bps"])
			netOutBPS := latestMetricValue(series["network_out_bps"])

			memoryPercent := 0.0
			if memoryLimitMB > 0 {
				memoryPercent = (memoryMB / memoryLimitMB) * 100
			}

			utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
				"cpu_percent":    strconv.FormatFloat(roundMetricValue(cpu, 2), 'f', 2, 64),
				"memory_used":    formatMiB(memoryMB),
				"memory_total":   formatMiB(memoryLimitMB),
				"memory_percent": strconv.FormatFloat(roundMetricValue(memoryPercent, 2), 'f', 2, 64),
				"network_in":     formatBytesPerSecond(netInBPS),
				"network_out":    formatBytesPerSecond(netOutBPS),
				"pids":           "0",
				"status":         svc.Status,
				"container_id":   svc.ContainerID,
				"timestamp":      end.Format(time.RFC3339),
				"source":         "prometheus",
			})
			return
		}
	}

	if strings.TrimSpace(svc.ContainerID) == "" {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"cpu_percent":    "0",
			"memory_used":    "0MiB",
			"memory_total":   "",
			"memory_percent": "0",
			"network_in":     "0B/s",
			"network_out":    "0B/s",
			"pids":           "0",
			"status":         svc.Status,
			"container_id":   svc.ContainerID,
			"timestamp":      time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	out, err := exec.Command("docker", "stats", "--no-stream", "--format",
		"{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.PIDs}}",
		svc.ContainerID).Output()
	if err != nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"cpu_percent":    "0",
			"memory_used":    "0MiB",
			"memory_total":   "",
			"memory_percent": "0",
			"network_in":     "0B/s",
			"network_out":    "0B/s",
			"pids":           "0",
			"status":         svc.Status,
			"container_id":   svc.ContainerID,
			"timestamp":      time.Now().UTC().Format(time.RFC3339),
			"error":          "container stats unavailable",
		})
		return
	}

	parts := strings.Split(strings.TrimSpace(string(out)), "\t")
	if len(parts) < 5 {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"status":       svc.Status,
			"container_id": svc.ContainerID,
			"raw":          string(out),
			"timestamp":    time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	cpuStr := strings.TrimSuffix(strings.TrimSpace(parts[0]), "%")
	memParts := strings.Split(strings.TrimSpace(parts[1]), " / ")
	memUsed := ""
	memTotal := ""
	if len(memParts) == 2 {
		memUsed = strings.TrimSpace(memParts[0])
		memTotal = strings.TrimSpace(memParts[1])
	}
	memPerc := strings.TrimSuffix(strings.TrimSpace(parts[2]), "%")
	netParts := strings.Split(strings.TrimSpace(parts[3]), " / ")
	netIn := ""
	netOut := ""
	if len(netParts) == 2 {
		netIn = strings.TrimSpace(netParts[0])
		netOut = strings.TrimSpace(netParts[1])
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"cpu_percent":    cpuStr,
		"memory_used":    memUsed,
		"memory_total":   memTotal,
		"memory_percent": memPerc,
		"network_in":     netIn,
		"network_out":    netOut,
		"pids":           strings.TrimSpace(parts[4]),
		"status":         svc.Status,
		"container_id":   svc.ContainerID,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
		"source":         "docker",
	})
}

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

	period := parseMetricsPeriod(r.URL.Query().Get("period"))
	end := time.Now().UTC()
	start := end.Add(-period.Duration)

	emptySeries := map[string]interface{}{
		"cpu_percent":     []map[string]interface{}{},
		"memory_mb":       []map[string]interface{}{},
		"network_in_bps":  []map[string]interface{}{},
		"network_out_bps": []map[string]interface{}{},
	}

	if h == nil || h.Config == nil || !h.Config.Kubernetes.Enabled {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"service_id":   svc.ID,
			"status":       svc.Status,
			"source":       "unavailable",
			"period":       period.Key,
			"start":        start.Format(time.RFC3339),
			"end":          end.Format(time.RFC3339),
			"step_seconds": int(period.Step.Seconds()),
			"series":       emptySeries,
			"current": map[string]float64{
				"cpu_percent":     0,
				"memory_mb":       0,
				"memory_percent":  0,
				"network_in_bps":  0,
				"network_out_bps": 0,
			},
			"cpu":    []map[string]interface{}{},
			"memory": []map[string]interface{}{},
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	series, queryErr := h.queryServicePrometheusMetrics(ctx, svc, start, end, period.Step)

	cpuPoints := metricPoints(series["cpu_percent"])
	memoryPoints := metricPoints(series["memory_mb"])
	networkInPoints := metricPoints(series["network_in_bps"])
	networkOutPoints := metricPoints(series["network_out_bps"])
	memoryLimitMB := latestMetricValue(series["memory_limit_mb"])
	memoryMB := latestMetricValue(series["memory_mb"])
	memoryPercent := 0.0
	if memoryLimitMB > 0 {
		memoryPercent = (memoryMB / memoryLimitMB) * 100
	}

	response := map[string]interface{}{
		"service_id":   svc.ID,
		"status":       svc.Status,
		"source":       "prometheus",
		"period":       period.Key,
		"start":        start.Format(time.RFC3339),
		"end":          end.Format(time.RFC3339),
		"step_seconds": int(period.Step.Seconds()),
		"series": map[string]interface{}{
			"cpu_percent":     cpuPoints,
			"memory_mb":       memoryPoints,
			"network_in_bps":  networkInPoints,
			"network_out_bps": networkOutPoints,
		},
		"current": map[string]float64{
			"cpu_percent":     roundMetricValue(latestMetricValue(series["cpu_percent"]), 2),
			"memory_mb":       roundMetricValue(memoryMB, 2),
			"memory_percent":  roundMetricValue(memoryPercent, 2),
			"network_in_bps":  roundMetricValue(latestMetricValue(series["network_in_bps"]), 2),
			"network_out_bps": roundMetricValue(latestMetricValue(series["network_out_bps"]), 2),
		},
		"cpu":    cpuPoints,
		"memory": memoryPoints,
	}

	if queryErr != nil {
		response["source"] = "prometheus_unavailable"
		response["error"] = queryErr.Error()
	}

	utils.RespondJSON(w, http.StatusOK, response)
}
