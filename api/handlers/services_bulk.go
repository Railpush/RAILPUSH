package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/railpush/api/utils"
)

func (h *ServiceHandler) BulkUpdateServices(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs             []string               `json:"ids"`
		ServiceIDs      []string               `json:"service_ids"`
		Changes         map[string]interface{} `json:"changes"`
		DryRun          bool                   `json:"dry_run"`
		Transactional   bool                   `json:"transactional"`
		TransactionMode string                 `json:"transaction_mode"`
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
	if len(req.Changes) == 0 {
		utils.RespondError(w, http.StatusBadRequest, "changes is required")
		return
	}

	transactionMode, err := normalizeBulkTransactionMode(req.TransactionMode, req.Transactional)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	dryRun := req.DryRun || isDryRunRequest(r)

	runPreflight := func(id string) (bulkResultItem, bool) {
		subReq, err := buildSubrequest(r, http.MethodPatch, map[string]string{"id": id}, req.Changes)
		if err != nil {
			return bulkResultItem{ID: id, Status: "failed", HTTPStatus: http.StatusBadRequest, Error: "invalid changes payload"}, false
		}
		setDryRunRequest(subReq, true)
		code, body := runSubrequest(h.UpdateService, subReq)
		item := bulkResultItem{ID: id, HTTPStatus: code}
		if code >= 200 && code < 300 {
			item.Status = "validated"
			item.Data = decodeJSONBody(body)
			return item, true
		}
		item.Status = "failed"
		item.Error = parseErrorBody(body, "failed to validate service update")
		return item, false
	}

	if dryRun {
		results := make([]bulkResultItem, 0, len(ids))
		succeeded := 0
		for _, id := range ids {
			item, ok := runPreflight(id)
			if ok {
				succeeded++
			}
			results = append(results, item)
		}
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"status":           "dry_run",
			"action":           "bulk_update_services",
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
			item, ok := runPreflight(id)
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
				"action":           "bulk_update_services",
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
		subReq, err := buildSubrequest(r, http.MethodPatch, map[string]string{"id": id}, req.Changes)
		if err != nil {
			results = append(results, bulkResultItem{ID: id, Status: "failed", HTTPStatus: http.StatusBadRequest, Error: "invalid changes payload"})
			continue
		}
		setDryRunRequest(subReq, false)

		code, body := runSubrequest(h.UpdateService, subReq)
		item := bulkResultItem{ID: id, HTTPStatus: code}
		if code >= 200 && code < 300 {
			item.Status = "updated"
			item.Data = decodeJSONBody(body)
			succeeded++
		} else {
			item.Status = "failed"
			item.Error = parseErrorBody(body, "failed to update service")
		}
		results = append(results, item)
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":           "completed",
		"action":           "bulk_update_services",
		"dry_run":          false,
		"transaction_mode": transactionMode,
		"total":            len(results),
		"succeeded":        succeeded,
		"failed":           len(results) - succeeded,
		"results":          results,
	})
}

func (h *ServiceHandler) BulkRestartServices(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs             []string `json:"ids"`
		ServiceIDs      []string `json:"service_ids"`
		DryRun          bool     `json:"dry_run"`
		Transactional   bool     `json:"transactional"`
		TransactionMode string   `json:"transaction_mode"`
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

	transactionMode, err := normalizeBulkTransactionMode(req.TransactionMode, req.Transactional)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	dryRun := req.DryRun || isDryRunRequest(r)

	runPreflight := func(id string) (bulkResultItem, bool) {
		subReq, err := buildSubrequest(r, http.MethodPost, map[string]string{"id": id}, nil)
		if err != nil {
			return bulkResultItem{ID: id, Status: "failed", HTTPStatus: http.StatusBadRequest, Error: "failed to build request"}, false
		}
		setDryRunRequest(subReq, true)
		code, body := runSubrequest(h.RestartService, subReq)
		item := bulkResultItem{ID: id, HTTPStatus: code}
		if code >= 200 && code < 300 {
			item.Status = "validated"
			item.Data = decodeJSONBody(body)
			return item, true
		}
		item.Status = "failed"
		item.Error = parseErrorBody(body, "failed to validate service restart")
		return item, false
	}

	if dryRun {
		results := make([]bulkResultItem, 0, len(ids))
		succeeded := 0
		for _, id := range ids {
			item, ok := runPreflight(id)
			if ok {
				succeeded++
			}
			results = append(results, item)
		}
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"status":           "dry_run",
			"action":           "bulk_restart_services",
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
			item, ok := runPreflight(id)
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
				"action":           "bulk_restart_services",
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
		subReq, err := buildSubrequest(r, http.MethodPost, map[string]string{"id": id}, nil)
		if err != nil {
			results = append(results, bulkResultItem{ID: id, Status: "failed", HTTPStatus: http.StatusBadRequest, Error: "failed to build request"})
			continue
		}
		setDryRunRequest(subReq, false)

		code, body := runSubrequest(h.RestartService, subReq)
		item := bulkResultItem{ID: id, HTTPStatus: code}
		if code >= 200 && code < 300 {
			item.Status = "restarted"
			item.Data = decodeJSONBody(body)
			succeeded++
		} else {
			item.Status = "failed"
			item.Error = parseErrorBody(body, "failed to restart service")
		}
		results = append(results, item)
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"status":           "completed",
		"action":           "bulk_restart_services",
		"dry_run":          false,
		"transaction_mode": transactionMode,
		"total":            len(results),
		"succeeded":        succeeded,
		"failed":           len(results) - succeeded,
		"results":          results,
	})
}
