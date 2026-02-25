package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/railpush/api/utils"
)

func (h *EnvVarHandler) BulkSetEnvVars(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs        []string `json:"ids"`
		ServiceIDs []string `json:"service_ids"`
		EnvVars    []struct {
			Key      string `json:"key"`
			Value    string `json:"value"`
			IsSecret bool   `json:"is_secret"`
		} `json:"env_vars"`
		Delete             []string `json:"delete"`
		Mode               string   `json:"mode"`
		ConfirmDestructive bool     `json:"confirm_destructive"`
		DryRun             bool     `json:"dry_run"`
		Transactional      bool     `json:"transactional"`
		TransactionMode    string   `json:"transaction_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ids := normalizeBulkIDs(req.IDs, req.ServiceIDs)
	if len(ids) == 0 {
		utils.RespondError(w, http.StatusBadRequest, "ids is required")
		return
	}
	if len(ids) > 200 {
		utils.RespondError(w, http.StatusBadRequest, "maximum 200 ids per request")
		return
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "merge"
	}
	if mode != "merge" && mode != "replace" {
		utils.RespondError(w, http.StatusBadRequest, "invalid mode (use merge or replace)")
		return
	}

	transactionMode, err := normalizeBulkTransactionMode(req.TransactionMode, req.Transactional)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	dryRun := req.DryRun || isDryRunRequest(r)

	runForID := func(id string, dryRun bool) (bulkResultItem, bool) {
		vars := map[string]string{"id": id}
		if mode == "replace" {
			payload := map[string]interface{}{
				"env_vars":            req.EnvVars,
				"mode":                "replace",
				"confirm_destructive": req.ConfirmDestructive,
			}
			subReq, err := buildSubrequest(r, http.MethodPut, vars, payload)
			if err != nil {
				return bulkResultItem{ID: id, Status: "failed", HTTPStatus: http.StatusBadRequest, Error: "failed to build request"}, false
			}
			setDryRunRequest(subReq, dryRun)
			code, body := runSubrequest(h.BulkUpdateEnvVars, subReq)
			item := bulkResultItem{ID: id, HTTPStatus: code}
			if code >= 200 && code < 300 {
				if dryRun {
					item.Status = "validated"
				} else {
					item.Status = "updated"
				}
				item.Data = decodeJSONBody(body)
				return item, true
			}
			item.Status = "failed"
			if dryRun {
				item.Error = parseErrorBody(body, "failed to validate env var replace")
			} else {
				item.Error = parseErrorBody(body, "failed to replace env vars")
			}
			return item, false
		}

		payload := map[string]interface{}{
			"env_vars": req.EnvVars,
			"delete":   req.Delete,
		}
		subReq, err := buildSubrequest(r, http.MethodPatch, vars, payload)
		if err != nil {
			return bulkResultItem{ID: id, Status: "failed", HTTPStatus: http.StatusBadRequest, Error: "failed to build request"}, false
		}
		setDryRunRequest(subReq, dryRun)
		code, body := runSubrequest(h.MergeEnvVars, subReq)
		item := bulkResultItem{ID: id, HTTPStatus: code}
		if code >= 200 && code < 300 {
			if dryRun {
				item.Status = "validated"
			} else {
				item.Status = "updated"
			}
			item.Data = decodeJSONBody(body)
			return item, true
		}
		item.Status = "failed"
		if dryRun {
			item.Error = parseErrorBody(body, "failed to validate env var merge")
		} else {
			item.Error = parseErrorBody(body, "failed to merge env vars")
		}
		return item, false
	}

	if dryRun {
		results := make([]bulkResultItem, 0, len(ids))
		succeeded := 0
		for _, id := range ids {
			item, ok := runForID(id, true)
			if ok {
				succeeded++
			}
			results = append(results, item)
		}
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"status":           "dry_run",
			"action":           "bulk_set_env_vars",
			"mode":             mode,
			"dry_run":          true,
			"transaction_mode": transactionMode,
			"total":            len(results),
			"succeeded":        succeeded,
			"failed":           len(results) - succeeded,
			"results":          results,
		})
		return
	}

	if transactionMode == bulkTransactionAllOrNothing {
		preflight := make([]bulkResultItem, 0, len(ids))
		failedValidation := false
		for _, id := range ids {
			item, ok := runForID(id, true)
			if !ok {
				failedValidation = true
			}
			preflight = append(preflight, item)
		}
		if failedValidation {
			for i := range preflight {
				if preflight[i].Status == "validated" {
					preflight[i].Status = "aborted"
					preflight[i].HTTPStatus = http.StatusConflict
					preflight[i].Error = "transaction aborted because at least one item failed validation"
					preflight[i].Data = nil
				}
			}
			utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
				"status":           "aborted",
				"action":           "bulk_set_env_vars",
				"mode":             mode,
				"dry_run":          false,
				"transaction_mode": transactionMode,
				"total":            len(preflight),
				"succeeded":        0,
				"failed":           len(preflight),
				"results":          preflight,
			})
			return
		}
	}

	results := make([]bulkResultItem, 0, len(ids))
	succeeded := 0
	for _, id := range ids {
		item, ok := runForID(id, false)
		if ok {
			succeeded++
		}
		results = append(results, item)
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":           "completed",
		"action":           "bulk_set_env_vars",
		"mode":             mode,
		"dry_run":          false,
		"transaction_mode": transactionMode,
		"total":            len(results),
		"succeeded":        succeeded,
		"failed":           len(results) - succeeded,
		"results":          results,
	})
}
