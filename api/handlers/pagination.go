package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const (
	defaultCursorPaginationLimit = 50
	maxCursorPaginationLimit     = 100
)

type cursorPagination struct {
	Enabled bool
	Limit   int
	Offset  int
}

type cursorPaginationMeta struct {
	Total      int    `json:"total"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func parseCursorPagination(r *http.Request) (cursorPagination, error) {
	if r == nil {
		return cursorPagination{}, nil
	}
	limitRaw := strings.TrimSpace(r.URL.Query().Get("limit"))
	cursorRaw := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if limitRaw == "" && cursorRaw == "" {
		return cursorPagination{}, nil
	}

	limit := defaultCursorPaginationLimit
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil || parsed <= 0 {
			return cursorPagination{}, fmt.Errorf("invalid limit (must be an integer between 1 and %d)", maxCursorPaginationLimit)
		}
		if parsed > maxCursorPaginationLimit {
			parsed = maxCursorPaginationLimit
		}
		limit = parsed
	}

	offset, err := decodeCursorOffset(cursorRaw)
	if err != nil {
		return cursorPagination{}, err
	}

	return cursorPagination{Enabled: true, Limit: limit, Offset: offset}, nil
}

func decodeCursorOffset(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		if plain, parseErr := strconv.Atoi(raw); parseErr == nil && plain >= 0 {
			return plain, nil
		}
		return 0, fmt.Errorf("invalid cursor")
	}

	offset, err := strconv.Atoi(strings.TrimSpace(string(decoded)))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid cursor")
	}
	return offset, nil
}

func encodeCursorOffset(offset int) string {
	if offset <= 0 {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func paginateSlice[T any](items []T, pagination cursorPagination) ([]T, *cursorPaginationMeta) {
	if items == nil {
		items = make([]T, 0)
	}
	if !pagination.Enabled {
		return items, nil
	}

	total := len(items)
	start := pagination.Offset
	if start > total {
		start = total
	}
	end := start + pagination.Limit
	if end > total {
		end = total
	}

	page := items[start:end]
	if page == nil {
		page = make([]T, 0)
	}

	meta := &cursorPaginationMeta{
		Total:   total,
		HasMore: end < total,
	}
	if meta.HasMore {
		meta.NextCursor = encodeCursorOffset(end)
	}
	return page, meta
}

func paginateWindowMeta(total int, pagination cursorPagination, pageLen int) *cursorPaginationMeta {
	if !pagination.Enabled {
		return nil
	}
	if total < 0 {
		total = 0
	}
	if pageLen < 0 {
		pageLen = 0
	}
	nextOffset := pagination.Offset + pageLen
	if nextOffset > total {
		nextOffset = total
	}
	meta := &cursorPaginationMeta{
		Total:   total,
		HasMore: nextOffset < total,
	}
	if meta.HasMore {
		meta.NextCursor = encodeCursorOffset(nextOffset)
	}
	return meta
}
