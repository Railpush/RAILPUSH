package services

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var logKVPattern = regexp.MustCompile(`([A-Za-z0-9_.@-]+)=((?:"(?:\\.|[^"])*")|(?:'(?:\\.|[^'])*')|[^\s]+)`)

type ParsedLogLine struct {
	Message   string
	Level     string
	Timestamp *time.Time
	Fields    map[string]string
}

func NormalizeLogLevel(level string) string {
	level = strings.ToLower(strings.TrimSpace(level))
	switch level {
	case "warning":
		return "warn"
	case "err", "fatal", "panic":
		return "error"
	default:
		return level
	}
}

func InferLogLevel(message string) string {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic") {
		return "error"
	}
	if strings.Contains(lower, "warn") {
		return "warn"
	}
	if strings.Contains(lower, "debug") {
		return "debug"
	}
	return "info"
}

func ParseStructuredLogLine(rawLine string) ParsedLogLine {
	line := strings.TrimSpace(rawLine)
	out := ParsedLogLine{
		Message: line,
		Level:   InferLogLevel(line),
		Fields:  map[string]string{},
	}
	if line == "" {
		return out
	}

	if fields, ok := parseJSONLogLine(line); ok {
		out.Fields = fields
		if msg := getFieldValue(fields, []string{"message", "msg", "log"}); msg != "" {
			out.Message = msg
		}
		if level := getFieldValue(fields, []string{"level", "severity", "log.level", "status"}); level != "" {
			out.Level = NormalizeLogLevel(level)
		}
		if ts := getFieldValue(fields, []string{"timestamp", "time", "ts", "@timestamp", "datetime", "created_at"}); ts != "" {
			if parsed, ok := parseFilterTime(ts); ok {
				out.Timestamp = &parsed
			}
		}
		if out.Level == "" {
			out.Level = InferLogLevel(out.Message)
		}
		return out
	}

	if fields := parseKVLogLine(line); len(fields) > 0 {
		out.Fields = fields
		if msg := getFieldValue(fields, []string{"message", "msg"}); msg != "" {
			out.Message = msg
		}
		if level := getFieldValue(fields, []string{"level", "severity", "status"}); level != "" {
			out.Level = NormalizeLogLevel(level)
		}
		if ts := getFieldValue(fields, []string{"timestamp", "time", "ts", "@timestamp", "datetime"}); ts != "" {
			if parsed, ok := parseFilterTime(ts); ok {
				out.Timestamp = &parsed
			}
		}
		if out.Level == "" {
			out.Level = InferLogLevel(out.Message)
		}
	}

	return out
}

func parseJSONLogLine(line string) (map[string]string, bool) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil || len(obj) == 0 {
		return nil, false
	}
	out := make(map[string]string, len(obj))
	for k, v := range obj {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = stringifyStructuredValue(v)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func parseKVLogLine(line string) map[string]string {
	matches := logKVPattern.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make(map[string]string, len(matches))
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		key := strings.TrimSpace(m[1])
		if key == "" {
			continue
		}
		val := strings.TrimSpace(m[2])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		out[key] = strings.TrimSpace(val)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stringifyStructuredValue(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case bool, float64, float32, int, int64, int32, uint, uint64, uint32:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return strings.TrimSpace(fmt.Sprintf("%v", t))
		}
		return strings.TrimSpace(string(b))
	}
}

func getFieldValue(fields map[string]string, candidates []string) string {
	if len(fields) == 0 || len(candidates) == 0 {
		return ""
	}
	for _, candidate := range candidates {
		needle := strings.ToLower(strings.TrimSpace(candidate))
		if needle == "" {
			continue
		}
		for k, v := range fields {
			if strings.EqualFold(strings.TrimSpace(k), needle) {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func parseFilterTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func ParseStructuredFilter(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}, nil
	}
	parts := regexp.MustCompile(`(?i)\s+AND\s+`).Split(raw, -1)
	out := map[string]string{}
	for _, p := range parts {
		term := strings.TrimSpace(p)
		if term == "" {
			continue
		}
		sep := strings.Index(term, ":")
		if sep < 0 {
			sep = strings.Index(term, "=")
		}
		if sep <= 0 || sep >= len(term)-1 {
			return nil, fmt.Errorf("invalid filter term %q", term)
		}
		k := strings.TrimSpace(term[:sep])
		v := strings.TrimSpace(term[sep+1:])
		if len(v) >= 2 {
			if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
				v = v[1 : len(v)-1]
			}
		}
		if k == "" || v == "" {
			return nil, fmt.Errorf("invalid filter term %q", term)
		}
		out[strings.ToLower(k)] = v
	}
	return out, nil
}

func MatchesStructuredFilter(fields map[string]string, filters map[string]string) bool {
	if len(filters) == 0 {
		return true
	}
	for k, want := range filters {
		matched := false
		for fk, fv := range fields {
			if strings.EqualFold(strings.TrimSpace(fk), strings.TrimSpace(k)) {
				if strings.EqualFold(strings.TrimSpace(fv), strings.TrimSpace(want)) {
					matched = true
				}
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
