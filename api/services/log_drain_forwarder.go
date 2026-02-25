package services

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/railpush/api/database"
	"github.com/railpush/api/models"
)

func logDrainLevelWeight(level string) int {
	switch NormalizeLogLevel(level) {
	case "debug":
		return 10
	case "info":
		return 20
	case "warn":
		return 30
	case "error":
		return 40
	default:
		return 20
	}
}

func compileCaseInsensitivePatterns(patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, raw := range patterns {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		re, err := regexp.Compile("(?i)" + p)
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}

func filterLogDrainEntries(entries []LogDrainEntry, drain *models.ServiceLogDrain) ([]LogDrainEntry, error) {
	if drain == nil {
		return nil, fmt.Errorf("missing drain")
	}
	minLevel := models.NormalizeServiceLogDrainLevel(drain.FilterMinLevel)
	if minLevel == "" {
		minLevel = "info"
	}
	includes, err := compileCaseInsensitivePatterns(drain.IncludePatterns)
	if err != nil {
		return nil, fmt.Errorf("invalid include pattern: %w", err)
	}
	excludes, err := compileCaseInsensitivePatterns(drain.ExcludePatterns)
	if err != nil {
		return nil, fmt.Errorf("invalid exclude pattern: %w", err)
	}

	out := make([]LogDrainEntry, 0, len(entries))
	for _, entry := range entries {
		if !models.ServiceLogDrainWantsType(drain.FilterLogTypes, entry.LogType) {
			continue
		}
		msg := strings.TrimSpace(entry.Message)
		if msg == "" {
			continue
		}
		level := NormalizeLogLevel(entry.Level)
		if level == "" {
			level = InferLogLevel(msg)
		}
		if logDrainLevelWeight(level) < logDrainLevelWeight(minLevel) {
			continue
		}

		if len(includes) > 0 {
			matched := false
			for _, re := range includes {
				if re.MatchString(msg) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		excluded := false
		for _, re := range excludes {
			if re.MatchString(msg) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		entry.Level = level
		entry.Message = msg
		out = append(out, entry)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out, nil
}

func queryServiceRuntimeDrainEntries(lokiURL string, namespace string, serviceID string, start, end time.Time, limit int) ([]LogDrainEntry, error) {
	if strings.TrimSpace(lokiURL) == "" {
		return []LogDrainEntry{}, nil
	}
	if strings.TrimSpace(namespace) == "" {
		namespace = "railpush"
	}
	if limit <= 0 {
		limit = 5000
	}

	servicePodPrefix := regexp.QuoteMeta(kubeServiceName(serviceID)) + ".*"
	logQL := fmt.Sprintf(`{namespace=%q,pod=~%q,container="service"}`, namespace, servicePodPrefix)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	lines, err := LokiQueryRange(ctx, lokiURL, logQL, start, end, limit)
	cancel()
	if err != nil {
		return nil, err
	}

	out := make([]LogDrainEntry, 0, len(lines))
	for _, line := range lines {
		raw := strings.TrimSpace(line.Line)
		if raw == "" {
			continue
		}
		parsed := ParseStructuredLogLine(raw)
		ts := line.Timestamp.UTC()
		if parsed.Timestamp != nil {
			ts = parsed.Timestamp.UTC()
		}
		msg := strings.TrimSpace(parsed.Message)
		if msg == "" {
			msg = raw
		}
		level := NormalizeLogLevel(parsed.Level)
		if level == "" {
			level = InferLogLevel(msg)
		}
		instanceID := ""
		if line.Labels != nil {
			instanceID = strings.TrimSpace(line.Labels["pod"])
		}
		out = append(out, LogDrainEntry{
			Timestamp:  ts,
			Level:      level,
			Message:    msg,
			InstanceID: instanceID,
			LogType:    "app",
			Fields:     parsed.Fields,
		})
	}
	return out, nil
}

func queryServiceBuildDrainEntries(serviceID string, start, end time.Time, limit int) ([]LogDrainEntry, error) {
	if database.DB == nil {
		return []LogDrainEntry{}, nil
	}
	if limit <= 0 {
		limit = 200
	}

	rows, err := database.DB.Query(
		`SELECT COALESCE(started_at, created_at, NOW()), COALESCE(build_log, '')
		   FROM deploys
		  WHERE service_id=$1
		    AND COALESCE(started_at, created_at, NOW()) >= $2
		    AND COALESCE(started_at, created_at, NOW()) <= $3
		  ORDER BY COALESCE(started_at, created_at, NOW()) ASC
		  LIMIT $4`,
		serviceID,
		start,
		end,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []LogDrainEntry{}
	for rows.Next() {
		var ts sql.NullTime
		var buildLog string
		if err := rows.Scan(&ts, &buildLog); err != nil {
			continue
		}
		if strings.TrimSpace(buildLog) == "" {
			continue
		}
		entryTime := time.Now().UTC()
		if ts.Valid {
			entryTime = ts.Time.UTC()
		}
		for _, line := range strings.Split(buildLog, "\n") {
			raw := strings.TrimSpace(line)
			if raw == "" {
				continue
			}
			parsed := ParseStructuredLogLine(raw)
			msg := strings.TrimSpace(parsed.Message)
			if msg == "" {
				msg = raw
			}
			level := NormalizeLogLevel(parsed.Level)
			if level == "" {
				level = InferLogLevel(msg)
			}
			tsVal := entryTime
			if parsed.Timestamp != nil {
				tsVal = parsed.Timestamp.UTC()
			}
			out = append(out, LogDrainEntry{
				Timestamp:  tsVal,
				Level:      level,
				Message:    msg,
				InstanceID: "build",
				LogType:    "build",
				Fields:     parsed.Fields,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Scheduler) forwardServiceLogDrain(lokiURL string, namespace string, now time.Time, svc *models.Service, drain *models.ServiceLogDrain) error {
	if svc == nil || drain == nil {
		return nil
	}
	configMap, err := DecodeServiceLogDrainConfig(s.Config, drain.ConfigEncrypted)
	if err != nil {
		return err
	}
	if err := ValidateServiceLogDrainConfig(drain.Destination, configMap); err != nil {
		return err
	}

	start := now.Add(-2 * time.Minute)
	if drain.LastCursorAt != nil {
		start = drain.LastCursorAt.UTC()
	}
	if start.After(now) {
		start = now.Add(-1 * time.Minute)
	}
	if now.Sub(start) > 6*time.Hour {
		start = now.Add(-6 * time.Hour)
	}

	entries := []LogDrainEntry{}
	if models.ServiceLogDrainWantsType(drain.FilterLogTypes, "app") {
		runtimeEntries, err := queryServiceRuntimeDrainEntries(lokiURL, namespace, svc.ID, start, now, 5000)
		if err != nil {
			return err
		}
		entries = append(entries, runtimeEntries...)
	}
	if models.ServiceLogDrainWantsType(drain.FilterLogTypes, "build") {
		buildEntries, err := queryServiceBuildDrainEntries(svc.ID, start, now, 200)
		if err != nil {
			return err
		}
		entries = append(entries, buildEntries...)
	}

	if len(entries) == 0 {
		_ = models.UpdateServiceLogDrainDeliveryStats(drain.ID, 0, 0, "", nil, &now)
		return nil
	}

	filtered, err := filterLogDrainEntries(entries, drain)
	if err != nil {
		return err
	}
	if len(filtered) == 0 {
		_ = models.UpdateServiceLogDrainDeliveryStats(drain.ID, 0, 0, "", nil, &now)
		return nil
	}

	batchSize := ResolveServiceLogDrainBatchSize(configMap)
	sentTotal := int64(0)
	for i := 0; i < len(filtered); i += batchSize {
		end := i + batchSize
		if end > len(filtered) {
			end = len(filtered)
		}
		batch := filtered[i:end]
		if err := DeliverServiceLogDrainBatch(s.Config, drain, svc.Name, batch); err != nil {
			failedDelta := int64(len(batch))
			_ = models.UpdateServiceLogDrainDeliveryStats(drain.ID, sentTotal, failedDelta, err.Error(), nil, nil)
			return err
		}
		sentTotal += int64(len(batch))
	}

	_ = models.UpdateServiceLogDrainDeliveryStats(drain.ID, sentTotal, 0, "", &now, &now)
	return nil
}

func (s *Scheduler) runLogDrainForwarding() {
	if s == nil || s.Config == nil {
		return
	}
	if time.Since(s.lastLogDrains) < time.Minute {
		return
	}
	s.lastLogDrains = time.Now()

	drains, err := models.ListEnabledServiceLogDrains(500)
	if err != nil {
		log.Printf("Scheduler: log drains: list failed: %v", err)
		return
	}
	if len(drains) == 0 {
		return
	}

	namespace := strings.TrimSpace(s.Config.Kubernetes.Namespace)
	if namespace == "" {
		namespace = "railpush"
	}
	lokiURL := strings.TrimSpace(s.Config.Logging.LokiURL)
	if lokiURL == "" && s.Config.Kubernetes.Enabled {
		lokiURL = "http://loki-gateway.logging.svc.cluster.local"
	}

	now := time.Now().UTC()
	serviceCache := map[string]*models.Service{}
	for i := range drains {
		drain := &drains[i]
		if !drain.Enabled {
			continue
		}
		svc := serviceCache[drain.ServiceID]
		if svc == nil {
			loaded, err := models.GetService(drain.ServiceID)
			if err != nil || loaded == nil {
				_ = models.UpdateServiceLogDrainDeliveryStats(drain.ID, 0, 1, "service not found", nil, nil)
				continue
			}
			svc = loaded
			serviceCache[drain.ServiceID] = svc
		}

		if err := s.forwardServiceLogDrain(lokiURL, namespace, now, svc, drain); err != nil {
			log.Printf("Scheduler: log drain forward failed drain=%s service=%s err=%v", drain.ID, drain.ServiceID, err)
		}
	}
}
