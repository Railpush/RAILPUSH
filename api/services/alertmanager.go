package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/railpush/api/config"
)

type AlertmanagerClient struct {
	BaseURL string
	HTTP    *http.Client
}

type SilenceMatcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
}

type CreateSilenceRequest struct {
	Matchers  []SilenceMatcher `json:"matchers"`
	StartsAt  time.Time        `json:"startsAt"`
	EndsAt    time.Time        `json:"endsAt"`
	CreatedBy string           `json:"createdBy"`
	Comment   string           `json:"comment"`
}

func NewAlertmanagerClient(cfg *config.Config) *AlertmanagerClient {
	base := ""
	if cfg != nil {
		base = strings.TrimSpace(cfg.Ops.AlertmanagerURL)
	}
	base = strings.TrimRight(base, "/")
	return &AlertmanagerClient{
		BaseURL: base,
		HTTP:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *AlertmanagerClient) CreateSilence(ctx context.Context, req CreateSilenceRequest) (string, error) {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return "", fmt.Errorf("alertmanager url not configured")
	}
	if len(req.Matchers) == 0 {
		return "", fmt.Errorf("no matchers")
	}
	if req.StartsAt.IsZero() || req.EndsAt.IsZero() || !req.EndsAt.After(req.StartsAt) {
		return "", fmt.Errorf("invalid silence window")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	u := c.BaseURL + "/api/v2/silences"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := c.HTTP.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1024*1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("alertmanager error: %s", strings.TrimSpace(string(raw)))
	}

	var out struct {
		SilenceID string `json:"silenceID"`
		SilenceId string `json:"silenceId"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	id := strings.TrimSpace(out.SilenceID)
	if id == "" {
		id = strings.TrimSpace(out.SilenceId)
	}
	if id == "" {
		return "", fmt.Errorf("alertmanager response missing silence id")
	}
	return id, nil
}

func (c *AlertmanagerClient) DeleteSilence(ctx context.Context, silenceID string) error {
	if c == nil || strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("alertmanager url not configured")
	}
	silenceID = strings.TrimSpace(silenceID)
	if silenceID == "" {
		return fmt.Errorf("missing silence id")
	}

	u := c.BaseURL + "/api/v2/silence/" + silenceID
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}

	res, err := c.HTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1024*1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("alertmanager error: %s", strings.TrimSpace(string(raw)))
	}
	return nil
}

