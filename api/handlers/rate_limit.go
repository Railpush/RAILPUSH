package handlers

import (
	"net/http"
	"time"

	"github.com/railpush/api/middleware"
	"github.com/railpush/api/utils"
)

type RateLimitHandler struct{}

func NewRateLimitHandler() *RateLimitHandler {
	return &RateLimitHandler{}
}

func (h *RateLimitHandler) GetCurrent(w http.ResponseWriter, r *http.Request) {
	info, ok := middleware.GetRateLimitInfo(r)
	if !ok {
		utils.RespondError(w, http.StatusInternalServerError, "rate limit info unavailable")
		return
	}
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"limit":      info.Limit,
		"remaining":  info.Remaining,
		"reset_at":   info.ResetAt.UTC().Format(time.RFC3339),
		"reset_unix": info.ResetAt.Unix(),
		"window":     info.Window.String(),
	})
}
