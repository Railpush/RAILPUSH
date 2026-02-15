package handlers

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/railpush/api/config"
	"github.com/railpush/api/database"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type OpsPerformanceHandler struct {
	Config *config.Config
}

func NewOpsPerformanceHandler(cfg *config.Config) *OpsPerformanceHandler {
	return &OpsPerformanceHandler{Config: cfg}
}

func (h *OpsPerformanceHandler) ensureOps(w http.ResponseWriter, r *http.Request) bool {
	userID := middleware.GetUserID(r)
	if err := services.EnsureOpsAccess(userID); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

type percentileStats struct {
	Avg *float64 `json:"avg,omitempty"`
	P50 *float64 `json:"p50,omitempty"`
	P95 *float64 `json:"p95,omitempty"`
}

type topFailure struct {
	ServiceID   string `json:"service_id"`
	ServiceName string `json:"service_name"`
	Failures    int64  `json:"failures"`
}

func scanNullableFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

func (h *OpsPerformanceHandler) Summary(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}

	windowHours := utils.GetQueryInt(r, "window_hours", 24)
	if windowHours < 1 {
		windowHours = 1
	}
	if windowHours > 168 {
		windowHours = 168
	}

	interval := fmt.Sprintf("%d hours", windowHours)

	type deployCounts struct {
		Total     int64 `json:"total"`
		Pending   int64 `json:"pending"`
		Building  int64 `json:"building"`
		Deploying int64 `json:"deploying"`
		Live      int64 `json:"live"`
		Failed    int64 `json:"failed"`
	}

	counts := deployCounts{}
	_ = database.DB.QueryRow(
		`SELECT COUNT(*),
		        SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),
		        SUM(CASE WHEN status='building' THEN 1 ELSE 0 END),
		        SUM(CASE WHEN status='deploying' THEN 1 ELSE 0 END),
		        SUM(CASE WHEN status='live' THEN 1 ELSE 0 END),
		        SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END)
		   FROM deploys
		  WHERE COALESCE(created_at, NOW()) >= NOW() - $1::interval`,
		interval,
	).Scan(&counts.Total, &counts.Pending, &counts.Building, &counts.Deploying, &counts.Live, &counts.Failed)

	// Queue wait: started_at - created_at
	var qAvg, qP50, qP95 sql.NullFloat64
	_ = database.DB.QueryRow(
		`SELECT AVG(EXTRACT(EPOCH FROM (started_at - created_at))) AS avg_s,
		        percentile_cont(0.50) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (started_at - created_at))) AS p50_s,
		        percentile_cont(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (started_at - created_at))) AS p95_s
		   FROM deploys
		  WHERE started_at IS NOT NULL
		    AND created_at IS NOT NULL
		    AND created_at >= NOW() - $1::interval`,
		interval,
	).Scan(&qAvg, &qP50, &qP95)

	// Deploy duration: finished_at - started_at
	var dAvg, dP50, dP95 sql.NullFloat64
	_ = database.DB.QueryRow(
		`SELECT AVG(EXTRACT(EPOCH FROM (finished_at - started_at))) AS avg_s,
		        percentile_cont(0.50) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (finished_at - started_at))) AS p50_s,
		        percentile_cont(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (finished_at - started_at))) AS p95_s
		   FROM deploys
		  WHERE started_at IS NOT NULL
		    AND finished_at IS NOT NULL
		    AND created_at >= NOW() - $1::interval`,
		interval,
	).Scan(&dAvg, &dP50, &dP95)

	// Top failing services (by deploy failures)
	rows, err := database.DB.Query(
		`SELECT COALESCE(d.service_id::text,''), COALESCE(s.name,''), COUNT(*) AS failures
		   FROM deploys d
		   LEFT JOIN services s ON s.id = d.service_id
		  WHERE d.status='failed'
		    AND COALESCE(d.created_at, NOW()) >= NOW() - $1::interval
		  GROUP BY d.service_id, s.name
		  ORDER BY failures DESC
		  LIMIT 10`,
		interval,
	)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load performance summary")
		return
	}
	defer rows.Close()
	var failures []topFailure
	for rows.Next() {
		var f topFailure
		if err := rows.Scan(&f.ServiceID, &f.ServiceName, &f.Failures); err != nil {
			continue
		}
		failures = append(failures, f)
	}
	if failures == nil {
		failures = []topFailure{}
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"window_hours": windowHours,
		"deploys":      counts,
		"queue_wait_seconds": percentileStats{
			Avg: scanNullableFloat(qAvg),
			P50: scanNullableFloat(qP50),
			P95: scanNullableFloat(qP95),
		},
		"deploy_duration_seconds": percentileStats{
			Avg: scanNullableFloat(dAvg),
			P50: scanNullableFloat(dP50),
			P95: scanNullableFloat(dP95),
		},
		"top_failures": failures,
	})
}
