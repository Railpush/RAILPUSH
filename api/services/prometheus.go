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

type PrometheusSample struct {
	Timestamp time.Time
	Value     float64
}

type prometheusQueryResp struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][]interface{}   `json:"values"`
			Value  []interface{}     `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func PrometheusQueryRange(ctx context.Context, baseURL string, promQL string, start time.Time, end time.Time, step time.Duration) ([]PrometheusSample, error) {
	baseURL = strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("missing Prometheus URL")
	}
	promQL = strings.TrimSpace(promQL)
	if promQL == "" {
		return nil, fmt.Errorf("missing Prometheus query")
	}
	if step <= 0 {
		step = 30 * time.Second
	}

	u, err := url.Parse(baseURL + "/api/v1/query_range")
	if err != nil {
		return nil, fmt.Errorf("invalid Prometheus URL: %w", err)
	}

	q := u.Query()
	q.Set("query", promQL)
	q.Set("start", strconv.FormatFloat(float64(start.UTC().UnixNano())/float64(time.Second), 'f', 3, 64))
	q.Set("end", strconv.FormatFloat(float64(end.UTC().UnixNano())/float64(time.Second), 'f', 3, 64))
	q.Set("step", strconv.Itoa(int(step.Seconds()))+"s")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("prometheus query failed: %s", msg)
	}

	var decoded prometheusQueryResp
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	if decoded.Status != "success" {
		return nil, fmt.Errorf("prometheus query failed: status=%s", decoded.Status)
	}

	aggregated := map[int64]float64{}
	for _, series := range decoded.Data.Result {
		if len(series.Values) == 0 && len(series.Value) == 2 {
			ts, value, ok := parsePrometheusValuePair(series.Value)
			if ok {
				aggregated[ts] = aggregated[ts] + value
			}
			continue
		}
		for _, raw := range series.Values {
			ts, value, ok := parsePrometheusValuePair(raw)
			if !ok {
				continue
			}
			aggregated[ts] = aggregated[ts] + value
		}
	}

	if len(aggregated) == 0 {
		return []PrometheusSample{}, nil
	}

	timestamps := make([]int64, 0, len(aggregated))
	for ts := range aggregated {
		timestamps = append(timestamps, ts)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })

	out := make([]PrometheusSample, 0, len(timestamps))
	for _, ts := range timestamps {
		out = append(out, PrometheusSample{
			Timestamp: time.Unix(0, ts).UTC(),
			Value:     aggregated[ts],
		})
	}
	return out, nil
}

func parsePrometheusValuePair(raw []interface{}) (int64, float64, bool) {
	if len(raw) != 2 {
		return 0, 0, false
	}

	var tsFloat float64
	switch t := raw[0].(type) {
	case float64:
		tsFloat = t
	case json.Number:
		v, err := t.Float64()
		if err != nil {
			return 0, 0, false
		}
		tsFloat = v
	case string:
		v, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0, 0, false
		}
		tsFloat = v
	default:
		return 0, 0, false
	}

	valueRaw := strings.TrimSpace(fmt.Sprintf("%v", raw[1]))
	if valueRaw == "" {
		return 0, 0, false
	}
	value, err := strconv.ParseFloat(valueRaw, 64)
	if err != nil {
		return 0, 0, false
	}

	tsNano := int64(tsFloat * float64(time.Second))
	return tsNano, value, true
}
