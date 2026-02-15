package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type OpsIncidentsHandler struct {
	Config *config.Config
}

func NewOpsIncidentsHandler(cfg *config.Config) *OpsIncidentsHandler {
	return &OpsIncidentsHandler{Config: cfg}
}

func (h *OpsIncidentsHandler) ensureOps(w http.ResponseWriter, r *http.Request) bool {
	userID := middleware.GetUserID(r)
	if err := services.EnsureOpsAccess(userID); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func groupKeyFromIncidentID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("missing incident id")
	}
	groupKeyRaw, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(groupKeyRaw) == 0 || len(groupKeyRaw) > 4096 {
		return "", fmt.Errorf("invalid incident id")
	}
	return string(groupKeyRaw), nil
}

func parseIntQuery(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	i, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return i
}

// ListIncidents returns incident rollups derived from Alertmanager webhook deliveries.
// Query params:
//   - state=active|resolved|all (default: active)
//   - limit (default: 50, max: 200)
//   - offset (default: 0)
func (h *OpsIncidentsHandler) ListIncidents(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}

	state := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("state")))
	switch state {
	case "", "active", "resolved", "all":
		if state == "" {
			state = "active"
		}
	default:
		utils.RespondError(w, http.StatusBadRequest, "invalid state")
		return
	}

	limit := parseIntQuery(r, "limit", 50)
	offset := parseIntQuery(r, "offset", 0)

	incidents, err := models.ListAlertIncidents(state, limit, offset)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list incidents")
		return
	}
	utils.RespondJSON(w, http.StatusOK, incidents)
}

// GetIncident returns details + recent event timeline for a specific incident.
// Route param :id is a URL-safe base64 encoding of the Alertmanager groupKey.
// Query params:
//   - events_limit (default: 50, max: 200)
func (h *OpsIncidentsHandler) GetIncident(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}

	id := strings.TrimSpace(mux.Vars(r)["id"])
	groupKey, err := groupKeyFromIncidentID(id)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	eventsLimit := parseIntQuery(r, "events_limit", 50)

	detail, err := models.GetAlertIncidentDetail(groupKey, eventsLimit)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load incident")
		return
	}
	if detail == nil {
		utils.RespondError(w, http.StatusNotFound, "incident not found")
		return
	}
	utils.RespondJSON(w, http.StatusOK, detail)
}

// AcknowledgeIncident marks an incident as acknowledged (internal only).
// Route param :id is a URL-safe base64 encoding of the Alertmanager groupKey.
func (h *OpsIncidentsHandler) AcknowledgeIncident(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}

	id := strings.TrimSpace(mux.Vars(r)["id"])
	groupKey, err := groupKeyFromIncidentID(id)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req)

	userID := middleware.GetUserID(r)
	ack, err := models.CreateIncidentAcknowledgement(groupKey, userID, req.Note)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to acknowledge")
		return
	}
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "ack": ack})
}

// SilenceIncident creates an Alertmanager silence for the incident.
// Route param :id is a URL-safe base64 encoding of the Alertmanager groupKey.
func (h *OpsIncidentsHandler) SilenceIncident(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}

	id := strings.TrimSpace(mux.Vars(r)["id"])
	groupKey, err := groupKeyFromIncidentID(id)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req struct {
		Scope           string `json:"scope"` // "group" | "alertname"
		DurationMinutes int    `json:"duration_minutes"`
		Comment         string `json:"comment"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		// Allow empty body for defaults.
		req.DurationMinutes = 0
		req.Scope = ""
		req.Comment = ""
	}

	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope == "" {
		scope = "group"
	}
	if scope != "group" && scope != "alertname" {
		utils.RespondError(w, http.StatusBadRequest, "invalid scope")
		return
	}

	durMin := req.DurationMinutes
	if durMin <= 0 {
		durMin = 120
	}
	if durMin > 60*24*7 {
		utils.RespondError(w, http.StatusBadRequest, "duration too long")
		return
	}

	comment := strings.TrimSpace(req.Comment)
	if comment == "" {
		comment = "Silenced via RailPush"
	}

	// Load latest Alertmanager payload to derive matchers safely.
	detail, err := models.GetAlertIncidentDetail(groupKey, 1)
	if err != nil || detail == nil || len(detail.LatestPayload) == 0 {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load incident payload")
		return
	}

	var payload struct {
		GroupLabels  map[string]string `json:"groupLabels"`
		CommonLabels map[string]string `json:"commonLabels"`
		Alerts       []struct {
			Labels map[string]string `json:"labels"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(detail.LatestPayload, &payload); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "invalid stored payload")
		return
	}

	labels := map[string]string{}
	switch scope {
	case "group":
		for k, v := range payload.GroupLabels {
			labels[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
		if len(labels) == 0 {
			for k, v := range payload.CommonLabels {
				labels[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
		// Safety: if alertname isn't part of grouping/common labels, add the first alertname to avoid oversilencing.
		if strings.TrimSpace(labels["alertname"]) == "" && len(payload.Alerts) > 0 && payload.Alerts[0].Labels != nil {
			if an := strings.TrimSpace(payload.Alerts[0].Labels["alertname"]); an != "" {
				labels["alertname"] = an
			}
		}
	case "alertname":
		an := strings.TrimSpace(payload.CommonLabels["alertname"])
		if an == "" && len(payload.Alerts) > 0 && payload.Alerts[0].Labels != nil {
			an = strings.TrimSpace(payload.Alerts[0].Labels["alertname"])
		}
		if an == "" {
			utils.RespondError(w, http.StatusBadRequest, "cannot derive alertname for silence")
			return
		}
		labels["alertname"] = an

		// Add a couple common scoping labels if present.
		ns := strings.TrimSpace(payload.CommonLabels["namespace"])
		if ns == "" && len(payload.Alerts) > 0 && payload.Alerts[0].Labels != nil {
			ns = strings.TrimSpace(payload.Alerts[0].Labels["namespace"])
		}
		if ns != "" {
			labels["namespace"] = ns
		}
		sev := strings.TrimSpace(payload.CommonLabels["severity"])
		if sev == "" && len(payload.Alerts) > 0 && payload.Alerts[0].Labels != nil {
			sev = strings.TrimSpace(payload.Alerts[0].Labels["severity"])
		}
		if sev != "" {
			labels["severity"] = sev
		}
	}

	matchers := make([]services.SilenceMatcher, 0, len(labels))
	for k, v := range labels {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		matchers = append(matchers, services.SilenceMatcher{Name: k, Value: v, IsRegex: false})
	}
	if len(matchers) == 0 {
		utils.RespondError(w, http.StatusBadRequest, "no matchers derived for silence")
		return
	}

	startsAt := time.Now().UTC().Add(-1 * time.Minute)
	endsAt := startsAt.Add(time.Duration(durMin) * time.Minute)
	userID := middleware.GetUserID(r)

	am := services.NewAlertmanagerClient(h.Config)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	silenceID, err := am.CreateSilence(ctx, services.CreateSilenceRequest{
		Matchers:  matchers,
		StartsAt:  startsAt,
		EndsAt:    endsAt,
		CreatedBy: "railpush:" + userID,
		Comment:   comment,
	})
	if err != nil {
		utils.RespondError(w, http.StatusBadGateway, "failed to create silence: "+err.Error())
		return
	}

	matchersJSON, _ := json.Marshal(matchers)
	_ = models.CreateIncidentSilence(&models.IncidentSilence{
		GroupKey:   groupKey,
		SilenceID:  silenceID,
		Scope:      scope,
		CreatedBy:  userID,
		Comment:    comment,
		Matchers:   json.RawMessage(matchersJSON),
		StartsAt:   startsAt,
		EndsAt:     endsAt,
	})

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"silence": map[string]interface{}{
			"silence_id":     silenceID,
			"scope":          scope,
			"comment":        comment,
			"starts_at":      startsAt,
			"ends_at":        endsAt,
			"duration_min":   durMin,
			"matchers":       matchers,
		},
	})
}
