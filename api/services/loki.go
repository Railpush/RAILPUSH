package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type LokiLogLine struct {
	Timestamp time.Time
	Line      string
	Labels    map[string]string
}

type lokiQueryRangeResp struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// LokiQueryRange queries Loki for logs in the given time range.
// baseURL should be a Loki base URL, e.g. http://loki-gateway.logging.svc.cluster.local
func LokiQueryRange(ctx context.Context, baseURL string, logQL string, start time.Time, end time.Time, limit int) ([]LokiLogLine, error) {
	baseURL = strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("missing Loki URL")
	}
	logQL = strings.TrimSpace(logQL)
	if logQL == "" {
		return nil, fmt.Errorf("missing Loki query")
	}
	if limit <= 0 {
		limit = 5000
	}

	u, err := url.Parse(baseURL + "/loki/api/v1/query_range")
	if err != nil {
		return nil, fmt.Errorf("invalid Loki URL: %w", err)
	}
	q := u.Query()
	q.Set("query", logQL)
	q.Set("direction", "forward")
	q.Set("limit", strconv.Itoa(limit))
	if !start.IsZero() {
		q.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	}
	if !end.IsZero() {
		q.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	// Keep this small; log queries happen on the request path.
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("loki query failed: %s", msg)
	}

	var decoded lokiQueryRangeResp
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	if decoded.Status != "success" {
		return nil, fmt.Errorf("loki query failed: status=%s", decoded.Status)
	}

	var out []LokiLogLine
	for _, r := range decoded.Data.Result {
		labels := r.Stream
		for _, v := range r.Values {
			if len(v) != 2 {
				continue
			}
			ns, err := strconv.ParseInt(v[0], 10, 64)
			if err != nil {
				continue
			}
			out = append(out, LokiLogLine{
				Timestamp: time.Unix(0, ns).UTC(),
				Line:      v[1],
				Labels:    labels,
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})

	return out, nil
}

