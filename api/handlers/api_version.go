package handlers

import (
	"net/http"

	"github.com/railpush/api/utils"
	"github.com/railpush/api/versioning"
)

type APIVersionHandler struct{}

func NewAPIVersionHandler() *APIVersionHandler {
	return &APIVersionHandler{}
}

func (h *APIVersionHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
	utils.RespondJSON(w, http.StatusOK, versioning.VersionInfo())
}

func (h *APIVersionHandler) GetVersionChangelog(w http.ResponseWriter, r *http.Request) {
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"major_version": versioning.APIMajorVersion,
		"current":       versioning.CurrentAPIVersion,
		"entries":       versioning.Changelog(),
	})
}
