package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"
)

type LogDrainEntry struct {
	Timestamp  time.Time         `json:"timestamp"`
	Level      string            `json:"level"`
	Message    string            `json:"message"`
	InstanceID string            `json:"instance_id,omitempty"`
	LogType    string            `json:"log_type,omitempty"`
	Fields     map[string]string `json:"fields,omitempty"`
}

func DecodeServiceLogDrainConfig(cfg *config.Config, encrypted string) (map[string]interface{}, error) {
	if strings.TrimSpace(encrypted) == "" {
		return map[string]interface{}{}, nil
	}
	if cfg == nil {
		return nil, fmt.Errorf("missing config")
	}
	decrypted, err := utils.Decrypt(strings.TrimSpace(encrypted), cfg.Crypto.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt log drain config: %w", err)
	}
	decrypted = strings.TrimSpace(decrypted)
	if decrypted == "" {
		return map[string]interface{}{}, nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(decrypted), &out); err != nil {
		return nil, fmt.Errorf("decode log drain config: %w", err)
	}
	if out == nil {
		out = map[string]interface{}{}
	}
	return out, nil
}

func copyAndRedactSensitiveConfigValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := map[string]interface{}{}
		for key, val := range t {
			k := strings.ToLower(strings.TrimSpace(key))
			if strings.Contains(k, "secret") || strings.Contains(k, "token") || strings.Contains(k, "password") || strings.Contains(k, "api_key") || strings.Contains(k, "apikey") || strings.Contains(k, "authorization") {
				out[key] = "***"
				continue
			}
			out[key] = copyAndRedactSensitiveConfigValue(val)
		}
		return out
	case map[string]string:
		out := map[string]string{}
		for key, val := range t {
			k := strings.ToLower(strings.TrimSpace(key))
			if strings.Contains(k, "secret") || strings.Contains(k, "token") || strings.Contains(k, "password") || strings.Contains(k, "api_key") || strings.Contains(k, "apikey") || strings.Contains(k, "authorization") {
				out[key] = "***"
				continue
			}
			out[key] = val
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(t))
		for _, item := range t {
			out = append(out, copyAndRedactSensitiveConfigValue(item))
		}
		return out
	default:
		return t
	}
}

func RedactServiceLogDrainConfig(raw map[string]interface{}) map[string]interface{} {
	if raw == nil {
		return map[string]interface{}{}
	}
	out := map[string]interface{}{}
	for k, v := range raw {
		out[k] = copyAndRedactSensitiveConfigValue(v)
	}
	return out
}

func logDrainConfigString(raw map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if raw == nil {
			continue
		}
		v, ok := raw[key]
		if !ok || v == nil {
			continue
		}
		s := strings.TrimSpace(fmt.Sprintf("%v", v))
		if s == "" || strings.EqualFold(s, "<nil>") {
			continue
		}
		return s
	}
	return ""
}

func logDrainConfigInt(raw map[string]interface{}, key string, fallback int) int {
	if raw == nil {
		return fallback
	}
	v, ok := raw[key]
	if !ok || v == nil {
		return fallback
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "" || strings.EqualFold(s, "<nil>") {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

func logDrainConfigStringMap(raw map[string]interface{}, key string) map[string]string {
	out := map[string]string{}
	if raw == nil {
		return out
	}
	v, ok := raw[key]
	if !ok || v == nil {
		return out
	}
	switch m := v.(type) {
	case map[string]interface{}:
		for k, val := range m {
			kk := strings.TrimSpace(k)
			if kk == "" {
				continue
			}
			vv := strings.TrimSpace(fmt.Sprintf("%v", val))
			if vv == "" || strings.EqualFold(vv, "<nil>") {
				continue
			}
			out[kk] = vv
		}
	case map[string]string:
		for k, val := range m {
			kk := strings.TrimSpace(k)
			vv := strings.TrimSpace(val)
			if kk == "" || vv == "" {
				continue
			}
			out[kk] = vv
		}
	}
	return out
}

func logDrainConfigStringSlice(raw map[string]interface{}, key string) []string {
	out := []string{}
	if raw == nil {
		return out
	}
	v, ok := raw[key]
	if !ok || v == nil {
		return out
	}
	switch arr := v.(type) {
	case []interface{}:
		for _, item := range arr {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s == "" || strings.EqualFold(s, "<nil>") {
				continue
			}
			out = append(out, s)
		}
	case []string:
		for _, item := range arr {
			s := strings.TrimSpace(item)
			if s == "" {
				continue
			}
			out = append(out, s)
		}
	}
	return out
}

func normalizeHTTPURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("missing url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("url must be http or https")
	}
	if strings.TrimSpace(u.Host) == "" {
		return "", fmt.Errorf("url host is required")
	}
	return u.String(), nil
}

func ValidateServiceLogDrainConfig(destination string, raw map[string]interface{}) error {
	destination = models.NormalizeServiceLogDrainDestination(destination)
	if destination == "" {
		return fmt.Errorf("unsupported destination")
	}
	if raw == nil {
		raw = map[string]interface{}{}
	}

	switch destination {
	case "webhook":
		if _, err := normalizeHTTPURL(logDrainConfigString(raw, "url")); err != nil {
			return fmt.Errorf("webhook url is required")
		}
		format := strings.ToLower(strings.TrimSpace(logDrainConfigString(raw, "format")))
		if format == "" {
			format = "json"
		}
		switch format {
		case "json", "logfmt", "raw":
		default:
			return fmt.Errorf("webhook format must be json, logfmt, or raw")
		}
	case "datadog":
		if strings.TrimSpace(logDrainConfigString(raw, "api_key", "apikey")) == "" {
			return fmt.Errorf("datadog api_key is required")
		}
	case "loki":
		if _, err := normalizeHTTPURL(logDrainConfigString(raw, "url")); err != nil {
			return fmt.Errorf("loki url is required")
		}
	case "splunk":
		if _, err := normalizeHTTPURL(logDrainConfigString(raw, "url")); err != nil {
			return fmt.Errorf("splunk url is required")
		}
		if strings.TrimSpace(logDrainConfigString(raw, "token")) == "" {
			return fmt.Errorf("splunk token is required")
		}
	case "elasticsearch", "opensearch":
		if _, err := normalizeHTTPURL(logDrainConfigString(raw, "url")); err != nil {
			return fmt.Errorf("destination url is required")
		}
	case "cloudwatch", "s3":
		return fmt.Errorf("destination %s is not yet supported", destination)
	}

	return nil
}

func ResolveServiceLogDrainBatchSize(raw map[string]interface{}) int {
	n := logDrainConfigInt(raw, "batch_size", 100)
	if n <= 0 {
		n = 100
	}
	if n > 1000 {
		n = 1000
	}
	return n
}

func doLogDrainHTTPRequest(endpoint string, body []byte, headers map[string]string, basicUser, basicPass string) error {
	urlStr, err := normalizeHTTPURL(endpoint)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "railpush-log-drain/1.0")
	for k, v := range headers {
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		if key == "" || val == "" {
			continue
		}
		req.Header.Set(key, val)
	}
	if basicUser != "" {
		req.SetBasicAuth(basicUser, basicPass)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		msg := strings.TrimSpace(string(preview))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("log drain delivery failed: HTTP %d %s", resp.StatusCode, msg)
	}

	return nil
}

func buildLogDrainEventDocument(drain *models.ServiceLogDrain, serviceName string, entry LogDrainEntry) map[string]interface{} {
	msg := strings.TrimSpace(entry.Message)
	if msg == "" {
		msg = "(empty log line)"
	}
	level := NormalizeLogLevel(entry.Level)
	if level == "" {
		level = InferLogLevel(msg)
	}
	logType := strings.TrimSpace(entry.LogType)
	if logType == "" {
		logType = "app"
	}

	doc := map[string]interface{}{
		"timestamp":    entry.Timestamp.UTC().Format(time.RFC3339Nano),
		"level":        level,
		"message":      msg,
		"service_id":   strings.TrimSpace(drain.ServiceID),
		"service_name": strings.TrimSpace(serviceName),
		"workspace_id": strings.TrimSpace(drain.WorkspaceID),
		"log_type":     logType,
	}
	if strings.TrimSpace(entry.InstanceID) != "" {
		doc["instance_id"] = strings.TrimSpace(entry.InstanceID)
	}
	if len(entry.Fields) > 0 {
		doc["fields"] = entry.Fields
		for k, v := range entry.Fields {
			key := strings.TrimSpace(k)
			if key != "" {
				doc[key] = strings.TrimSpace(v)
			}
		}
	}
	return doc
}

func deliverWebhookDrain(drain *models.ServiceLogDrain, serviceName string, configMap map[string]interface{}, entries []LogDrainEntry) error {
	endpoint := logDrainConfigString(configMap, "url")
	format := strings.ToLower(strings.TrimSpace(logDrainConfigString(configMap, "format")))
	if format == "" {
		format = "json"
	}

	headers := logDrainConfigStringMap(configMap, "headers")
	if _, ok := headers["Content-Type"]; !ok {
		if format == "raw" {
			headers["Content-Type"] = "text/plain"
		} else {
			headers["Content-Type"] = "application/json"
		}
	}

	var body []byte
	switch format {
	case "raw":
		lines := make([]string, 0, len(entries))
		for _, entry := range entries {
			msg := strings.TrimSpace(entry.Message)
			if msg == "" {
				continue
			}
			lines = append(lines, msg)
		}
		body = []byte(strings.Join(lines, "\n"))
	case "logfmt":
		lines := make([]string, 0, len(entries))
		for _, entry := range entries {
			doc := buildLogDrainEventDocument(drain, serviceName, entry)
			parts := make([]string, 0, len(doc))
			for key, value := range doc {
				parts = append(parts, key+"="+strconv.Quote(strings.TrimSpace(fmt.Sprintf("%v", value))))
			}
			lines = append(lines, strings.Join(parts, " "))
		}
		body = []byte(strings.Join(lines, "\n"))
	default:
		items := make([]map[string]interface{}, 0, len(entries))
		for _, entry := range entries {
			items = append(items, buildLogDrainEventDocument(drain, serviceName, entry))
		}
		payload := map[string]interface{}{
			"service_id":   drain.ServiceID,
			"service_name": serviceName,
			"workspace_id": drain.WorkspaceID,
			"destination":  "webhook",
			"logs":         items,
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = encoded
	}

	return doLogDrainHTTPRequest(endpoint, body, headers, "", "")
}

func deliverDatadogDrain(drain *models.ServiceLogDrain, serviceName string, configMap map[string]interface{}, entries []LogDrainEntry) error {
	apiKey := strings.TrimSpace(logDrainConfigString(configMap, "api_key", "apikey"))
	site := strings.TrimSpace(logDrainConfigString(configMap, "site"))
	if site == "" {
		site = "datadoghq.com"
	}
	endpoint := "https://http-intake.logs." + site + "/api/v2/logs"

	tags := logDrainConfigStringSlice(configMap, "tags")
	if len(tags) == 0 {
		tags = []string{"service:" + strings.TrimSpace(serviceName)}
	}

	payload := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		doc := buildLogDrainEventDocument(drain, serviceName, entry)
		doc["ddsource"] = "railpush"
		doc["service"] = strings.TrimSpace(serviceName)
		doc["ddtags"] = strings.Join(tags, ",")
		payload = append(payload, doc)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	headers := logDrainConfigStringMap(configMap, "headers")
	headers["Content-Type"] = "application/json"
	headers["DD-API-KEY"] = apiKey
	return doLogDrainHTTPRequest(endpoint, body, headers, "", "")
}

func deliverLokiDrain(drain *models.ServiceLogDrain, serviceName string, configMap map[string]interface{}, entries []LogDrainEntry) error {
	endpoint := strings.TrimSpace(logDrainConfigString(configMap, "url"))
	if !strings.Contains(endpoint, "/loki/api/v1/push") {
		endpoint = strings.TrimRight(endpoint, "/") + "/loki/api/v1/push"
	}

	labels := map[string]string{
		"service_id":   strings.TrimSpace(drain.ServiceID),
		"service_name": strings.TrimSpace(serviceName),
		"workspace_id": strings.TrimSpace(drain.WorkspaceID),
		"source":       "railpush-drain",
	}
	for key, val := range logDrainConfigStringMap(configMap, "labels") {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(val) != "" {
			labels[key] = val
		}
	}

	values := make([][]string, 0, len(entries))
	for _, entry := range entries {
		ns := strconv.FormatInt(entry.Timestamp.UTC().UnixNano(), 10)
		line := strings.TrimSpace(entry.Message)
		if line == "" {
			line = "(empty log line)"
		}
		if len(entry.Fields) > 0 {
			enriched := map[string]interface{}{
				"message": line,
				"level":   NormalizeLogLevel(entry.Level),
				"fields":  entry.Fields,
			}
			if encoded, err := json.Marshal(enriched); err == nil {
				line = string(encoded)
			}
		}
		values = append(values, []string{ns, line})
	}

	payload := map[string]interface{}{
		"streams": []map[string]interface{}{{
			"stream": labels,
			"values": values,
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	headers := logDrainConfigStringMap(configMap, "headers")
	headers["Content-Type"] = "application/json"
	if token := strings.TrimSpace(logDrainConfigString(configMap, "token", "bearer_token")); token != "" {
		headers["Authorization"] = "Bearer " + token
	}

	username := strings.TrimSpace(logDrainConfigString(configMap, "username"))
	password := strings.TrimSpace(logDrainConfigString(configMap, "password"))
	return doLogDrainHTTPRequest(endpoint, body, headers, username, password)
}

func deliverSplunkDrain(drain *models.ServiceLogDrain, serviceName string, configMap map[string]interface{}, entries []LogDrainEntry) error {
	endpoint := strings.TrimSpace(logDrainConfigString(configMap, "url"))
	token := strings.TrimSpace(logDrainConfigString(configMap, "token"))

	var b strings.Builder
	for _, entry := range entries {
		event := map[string]interface{}{
			"time":   float64(entry.Timestamp.UTC().UnixNano()) / float64(time.Second),
			"source": "railpush",
			"event":  buildLogDrainEventDocument(drain, serviceName, entry),
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			return err
		}
		b.Write(encoded)
		b.WriteString("\n")
	}

	headers := logDrainConfigStringMap(configMap, "headers")
	headers["Content-Type"] = "application/json"
	headers["Authorization"] = "Splunk " + token
	return doLogDrainHTTPRequest(endpoint, []byte(b.String()), headers, "", "")
}

func deliverElasticDrain(drain *models.ServiceLogDrain, serviceName string, configMap map[string]interface{}, entries []LogDrainEntry) error {
	base := strings.TrimRight(strings.TrimSpace(logDrainConfigString(configMap, "url")), "/")
	index := strings.TrimSpace(logDrainConfigString(configMap, "index"))
	if index == "" {
		index = "railpush-logs"
	}
	endpoint := base + "/" + index + "/_bulk"

	var b strings.Builder
	for _, entry := range entries {
		b.WriteString("{\"index\":{}}\n")
		doc := buildLogDrainEventDocument(drain, serviceName, entry)
		encoded, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		b.Write(encoded)
		b.WriteString("\n")
	}

	headers := logDrainConfigStringMap(configMap, "headers")
	headers["Content-Type"] = "application/x-ndjson"
	if apiKey := strings.TrimSpace(logDrainConfigString(configMap, "api_key", "apikey")); apiKey != "" {
		headers["Authorization"] = "ApiKey " + apiKey
	}

	username := strings.TrimSpace(logDrainConfigString(configMap, "username"))
	password := strings.TrimSpace(logDrainConfigString(configMap, "password"))
	return doLogDrainHTTPRequest(endpoint, []byte(b.String()), headers, username, password)
}

func DeliverServiceLogDrainBatch(cfg *config.Config, drain *models.ServiceLogDrain, serviceName string, entries []LogDrainEntry) error {
	if drain == nil {
		return fmt.Errorf("missing drain")
	}
	if len(entries) == 0 {
		return nil
	}

	configMap, err := DecodeServiceLogDrainConfig(cfg, drain.ConfigEncrypted)
	if err != nil {
		return err
	}
	destination := models.NormalizeServiceLogDrainDestination(drain.Destination)
	if err := ValidateServiceLogDrainConfig(destination, configMap); err != nil {
		return err
	}

	switch destination {
	case "webhook":
		return deliverWebhookDrain(drain, serviceName, configMap, entries)
	case "datadog":
		return deliverDatadogDrain(drain, serviceName, configMap, entries)
	case "loki":
		return deliverLokiDrain(drain, serviceName, configMap, entries)
	case "splunk":
		return deliverSplunkDrain(drain, serviceName, configMap, entries)
	case "elasticsearch", "opensearch":
		return deliverElasticDrain(drain, serviceName, configMap, entries)
	default:
		return fmt.Errorf("destination %s is not yet supported", destination)
	}
}
