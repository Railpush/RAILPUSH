package handlers

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var retentionPattern = regexp.MustCompile(`^(\d+)\s*([dwmyDWMY]?)$`)

func parseRetentionDaysField(payload map[string]interface{}, field string, minDays, maxDays int) (int, bool, error) {
	v, ok := payload[field]
	if !ok {
		return 0, false, nil
	}
	days, err := parseRetentionDaysValue(v, field, minDays, maxDays)
	if err != nil {
		return 0, true, err
	}
	return days, true, nil
}

func parseRetentionDaysValue(v interface{}, field string, minDays, maxDays int) (int, error) {
	if minDays <= 0 {
		minDays = 1
	}
	if maxDays < minDays {
		maxDays = minDays
	}

	toDays := func(n int) (int, error) {
		if n < minDays || n > maxDays {
			return 0, fmt.Errorf("%s must be between %d and %d days", field, minDays, maxDays)
		}
		return n, nil
	}

	switch val := v.(type) {
	case float64:
		if math.Trunc(val) != val {
			return 0, fmt.Errorf("%s must be a whole number", field)
		}
		return toDays(int(val))
	case int:
		return toDays(val)
	case int64:
		return toDays(int(val))
	case string:
		raw := strings.TrimSpace(val)
		if raw == "" {
			return 0, fmt.Errorf("%s is required", field)
		}
		m := retentionPattern.FindStringSubmatch(raw)
		if len(m) != 3 {
			return 0, fmt.Errorf("%s must be like 30d, 12w, 6m, or 1y", field)
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, fmt.Errorf("invalid %s", field)
		}
		suffix := strings.ToLower(strings.TrimSpace(m[2]))
		mult := 1
		switch suffix {
		case "", "d":
			mult = 1
		case "w":
			mult = 7
		case "m":
			mult = 30
		case "y":
			mult = 365
		default:
			return 0, fmt.Errorf("%s has unsupported unit", field)
		}
		return toDays(n * mult)
	default:
		return 0, fmt.Errorf("%s must be a number of days or a duration string", field)
	}
}

func formatRetentionDays(days int) string {
	if days <= 0 {
		days = 1
	}
	return fmt.Sprintf("%dd", days)
}
