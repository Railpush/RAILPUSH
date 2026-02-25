package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"
)

func firstSuggestion(rawID string, listFn func(string, int) ([]string, error)) string {
	rawID = strings.TrimSpace(rawID)
	if rawID == "" || listFn == nil {
		return ""
	}
	ids, err := listFn(rawID, 3)
	if err != nil || len(ids) == 0 {
		return ""
	}
	for _, candidate := range ids {
		if strings.EqualFold(strings.TrimSpace(candidate), rawID) {
			continue
		}
		return candidate
	}
	return ""
}

func respondServiceNotFound(w http.ResponseWriter, rawID string) {
	rawID = strings.TrimSpace(rawID)
	msg := "service not found"
	if rawID != "" {
		msg = fmt.Sprintf("no service with ID %s", rawID)
	}
	suggestion := ""
	if hit := firstSuggestion(rawID, models.SuggestServiceIDs); hit != "" {
		suggestion = fmt.Sprintf("Did you mean %s?", hit)
	}
	utils.RespondErrorWithOptions(w, http.StatusNotFound, utils.ErrorOptions{
		Code:       "SERVICE_NOT_FOUND",
		Message:    msg,
		Suggestion: suggestion,
	})
}

func respondDatabaseNotFound(w http.ResponseWriter, rawID string) {
	rawID = strings.TrimSpace(rawID)
	msg := "database not found"
	if rawID != "" {
		msg = fmt.Sprintf("no database with ID %s", rawID)
	}
	suggestion := ""
	if hit := firstSuggestion(rawID, models.SuggestManagedDatabaseIDs); hit != "" {
		suggestion = fmt.Sprintf("Did you mean %s?", hit)
	}
	utils.RespondErrorWithOptions(w, http.StatusNotFound, utils.ErrorOptions{
		Code:       "DATABASE_NOT_FOUND",
		Message:    msg,
		Suggestion: suggestion,
	})
}

func respondKeyValueNotFound(w http.ResponseWriter, rawID string) {
	rawID = strings.TrimSpace(rawID)
	msg := "key-value store not found"
	if rawID != "" {
		msg = fmt.Sprintf("no key-value store with ID %s", rawID)
	}
	suggestion := ""
	if hit := firstSuggestion(rawID, models.SuggestManagedKeyValueIDs); hit != "" {
		suggestion = fmt.Sprintf("Did you mean %s?", hit)
	}
	utils.RespondErrorWithOptions(w, http.StatusNotFound, utils.ErrorOptions{
		Code:       "KEYVALUE_NOT_FOUND",
		Message:    msg,
		Suggestion: suggestion,
	})
}
