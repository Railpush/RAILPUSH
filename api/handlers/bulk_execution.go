package handlers

import (
	"fmt"
	"net/http"
	"strings"
)

const bulkDryRunHeader = "X-RailPush-Dry-Run"

const (
	bulkTransactionBestEffort   = "best_effort"
	bulkTransactionAllOrNothing = "all_or_nothing"
)

func boolLike(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func isDryRunRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if boolLike(r.Header.Get(bulkDryRunHeader)) {
		return true
	}
	if boolLike(r.URL.Query().Get("dry_run")) {
		return true
	}
	return false
}

func setDryRunRequest(r *http.Request, enabled bool) {
	if r == nil {
		return
	}
	if enabled {
		r.Header.Set(bulkDryRunHeader, "true")
		return
	}
	r.Header.Del(bulkDryRunHeader)
}

func normalizeBulkTransactionMode(raw string, transactional bool) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		if transactional {
			mode = bulkTransactionAllOrNothing
		} else {
			mode = bulkTransactionBestEffort
		}
	}
	switch mode {
	case bulkTransactionBestEffort, bulkTransactionAllOrNothing:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid transaction_mode (use %s or %s)", bulkTransactionBestEffort, bulkTransactionAllOrNothing)
	}
}
