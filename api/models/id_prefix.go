package models

import (
	"strings"

	"github.com/railpush/api/database"
)

func normalizeUUIDPrefixCandidate(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	if len(s) < 8 || len(s) > 36 {
		return ""
	}
	for _, ch := range s {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || ch == '-' {
			continue
		}
		return ""
	}
	return s
}

func isUUIDPrefixCandidate(raw string) bool {
	return normalizeUUIDPrefixCandidate(raw) != ""
}

func listIDPrefixMatches(query string, prefix string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 5
	}
	p := strings.TrimSpace(strings.ToLower(prefix))
	if p == "" {
		return []string{}, nil
	}
	rows, err := database.DB.Query(query, p+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	matches := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		matches = append(matches, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}

func suggestIDPrefixes(query string, raw string, limit int) ([]string, error) {
	prefix := normalizeUUIDPrefixCandidate(raw)
	if prefix == "" {
		return []string{}, nil
	}
	return listIDPrefixMatches(query, prefix, limit)
}
