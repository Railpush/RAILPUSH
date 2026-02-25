package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gorilla/mux"
)

type bulkResultItem struct {
	ID         string      `json:"id"`
	Status     string      `json:"status"`
	HTTPStatus int         `json:"http_status"`
	Error      string      `json:"error,omitempty"`
	Data       interface{} `json:"data,omitempty"`
}

func normalizeBulkIDs(primary []string, alternate []string) []string {
	ids := primary
	if len(ids) == 0 {
		ids = alternate
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func buildSubrequest(r *http.Request, method string, vars map[string]string, payload interface{}) (*http.Request, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}

	var (
		bodyBytes []byte
		err       error
	)
	if payload != nil {
		bodyBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}

	subReq := r.Clone(r.Context())
	subReq.Method = method
	subReq.Header = r.Header.Clone()
	if len(bodyBytes) > 0 {
		subReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		subReq.ContentLength = int64(len(bodyBytes))
		subReq.Header.Set("Content-Type", "application/json")
	} else {
		subReq.Body = http.NoBody
		subReq.ContentLength = 0
	}

	if vars != nil {
		subReq = mux.SetURLVars(subReq, vars)
	}
	return subReq, nil
}

func runSubrequest(handler func(http.ResponseWriter, *http.Request), req *http.Request) (int, []byte) {
	rec := httptest.NewRecorder()
	handler(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024))
	return res.StatusCode, body
}

func decodeJSONBody(body []byte) interface{} {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil
	}
	var out interface{}
	if err := json.Unmarshal(trimmed, &out); err != nil {
		return nil
	}
	return out
}

func parseErrorBody(body []byte, fallback string) string {
	if decoded := decodeJSONBody(body); decoded != nil {
		if m, ok := decoded.(map[string]interface{}); ok {
			for _, key := range []string{"error", "message", "detail"} {
				if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			}
		}
	}
	if s := strings.TrimSpace(string(body)); s != "" {
		return s
	}
	return fallback
}
