package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/database"
	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"
	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	Config        *config.Config
	stop          chan struct{}
	parser        cron.Parser
	lastCleanup   time.Time
	lastBackup    time.Time
	lastUsageSync time.Time
	lastLogAlerts time.Time
	lastLogDrains time.Time
}

func NewScheduler(cfg *config.Config) *Scheduler {
	return &Scheduler{
		Config: cfg,
		stop:   make(chan struct{}),
		parser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor),
	}
}

func (s *Scheduler) Start() {
	ticker := time.NewTicker(time.Minute)
	go func() {
		for {
			select {
			case <-ticker.C:
				s.checkCronJobs()
				s.cleanupStaleData()
				s.runNightlyBackups()
				s.reportMeteredUsage()
				s.runLogAlertEvaluations()
				s.runLogDrainForwarding()
			case <-s.stop:
				ticker.Stop()
				return
			}
		}
	}()
	log.Println("Scheduler started")
}

func containsStringFold(haystack []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return false
	}
	for _, item := range haystack {
		if strings.EqualFold(strings.TrimSpace(item), needle) {
			return true
		}
	}
	return false
}

func (s *Scheduler) runLogAlertEvaluations() {
	if s.Config == nil {
		return
	}
	if time.Since(s.lastLogAlerts) < time.Minute {
		return
	}
	s.lastLogAlerts = time.Now()

	lokiURL := strings.TrimSpace(s.Config.Logging.LokiURL)
	if lokiURL == "" {
		if s.Config.Kubernetes.Enabled {
			lokiURL = "http://loki-gateway.logging.svc.cluster.local"
		}
	}
	if lokiURL == "" {
		return
	}

	rules, err := models.ListEnabledServiceLogAlerts(500)
	if err != nil {
		log.Printf("Scheduler: log alerts: list failed: %v", err)
		return
	}
	if len(rules) == 0 {
		return
	}

	ns := strings.TrimSpace(s.Config.Kubernetes.Namespace)
	if ns == "" {
		ns = "railpush"
	}

	now := time.Now().UTC()
	for _, rule := range rules {
		if err := s.evaluateLogAlertRule(lokiURL, ns, now, &rule); err != nil {
			_ = models.UpdateServiceLogAlertEvaluationState(rule.ID, "error", 0, nil, nil, err.Error())
			log.Printf("Scheduler: log alerts: evaluate failed rule=%s err=%v", rule.ID, err)
		}
	}
}

func compareLogAlertThreshold(comparison string, count int, threshold int) bool {
	switch strings.ToLower(strings.TrimSpace(comparison)) {
	case "greater_than_or_equal":
		return count >= threshold
	case "equal":
		return count == threshold
	default:
		return count > threshold
	}
}

func (s *Scheduler) evaluateLogAlertRule(lokiURL string, namespace string, now time.Time, rule *models.ServiceLogAlert) error {
	if rule == nil {
		return nil
	}
	if strings.TrimSpace(rule.ServiceID) == "" {
		return fmt.Errorf("missing service_id")
	}
	window := time.Duration(rule.WindowSeconds) * time.Second
	if window <= 0 {
		window = 5 * time.Minute
	}
	if window > 24*time.Hour {
		window = 24 * time.Hour
	}
	start := now.Add(-window)

	servicePodPrefix := regexp.QuoteMeta(kubeServiceName(rule.ServiceID)) + ".*"
	logQL := fmt.Sprintf(`{namespace=%q,pod=~%q,container="service"}`, namespace, servicePodPrefix)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	lines, err := LokiQueryRange(ctx, lokiURL, logQL, start, now, 5000)
	cancel()
	if err != nil {
		return err
	}

	filters, err := ParseStructuredFilter(rule.FilterQuery)
	if err != nil {
		return fmt.Errorf("invalid filter_query: %w", err)
	}

	var pattern *regexp.Regexp
	if strings.TrimSpace(rule.Pattern) != "" {
		pattern, err = regexp.Compile("(?i)" + strings.TrimSpace(rule.Pattern))
		if err != nil {
			return fmt.Errorf("invalid pattern: %w", err)
		}
	}

	matches := 0
	for _, line := range lines {
		parsed := ParseStructuredLogLine(strings.TrimSpace(line.Line))
		fields := map[string]string{
			"message": parsed.Message,
			"level":   NormalizeLogLevel(parsed.Level),
		}
		for k, v := range parsed.Fields {
			fields[k] = v
		}
		if !MatchesStructuredFilter(fields, filters) {
			continue
		}
		if pattern != nil && !pattern.MatchString(parsed.Message) {
			continue
		}
		matches++
	}

	triggered := compareLogAlertThreshold(rule.Comparison, matches, rule.Threshold)
	prevStatus := strings.ToLower(strings.TrimSpace(rule.Status))
	if prevStatus == "" {
		prevStatus = "ok"
	}

	shouldNotify := true
	if triggered && rule.LastTriggeredAt != nil && rule.CooldownSeconds > 0 {
		if now.Sub(rule.LastTriggeredAt.UTC()) < time.Duration(rule.CooldownSeconds)*time.Second {
			shouldNotify = false
		}
	}

	if triggered {
		if shouldNotify {
			s.notifyLogAlert(rule, matches, "firing", now)
			_ = models.UpdateServiceLogAlertEvaluationState(rule.ID, "firing", matches, &now, nil, "")
		} else {
			_ = models.UpdateServiceLogAlertEvaluationState(rule.ID, "firing", matches, nil, nil, "")
		}
		return nil
	}

	if prevStatus == "firing" {
		s.notifyLogAlert(rule, matches, "resolved", now)
		_ = models.UpdateServiceLogAlertEvaluationState(rule.ID, "resolved", matches, nil, &now, "")
		return nil
	}

	return models.UpdateServiceLogAlertEvaluationState(rule.ID, "ok", matches, nil, nil, "")
}

func (s *Scheduler) notifyLogAlert(rule *models.ServiceLogAlert, matches int, status string, now time.Time) {
	if s == nil || s.Config == nil || rule == nil {
		return
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "firing" && status != "resolved" {
		return
	}

	channels := models.NormalizeLogAlertChannels(rule.Channels)
	isIncident := containsStringFold(channels, "incident")
	isWebhook := containsStringFold(channels, "webhook")
	isEmail := containsStringFold(channels, "email")

	if isIncident {
		payload := map[string]interface{}{
			"status":   status,
			"receiver": "railpush-log-alert",
			"groupKey": "log-alert:" + rule.ID,
			"commonLabels": map[string]string{
				"alertname":    rule.Name,
				"severity":     strings.TrimSpace(rule.Priority),
				"namespace":    "railpush",
				"service_id":   rule.ServiceID,
				"log_alert_id": rule.ID,
			},
			"commonAnnotations": map[string]string{
				"summary":     fmt.Sprintf("Log alert %s (%s)", rule.Name, status),
				"description": fmt.Sprintf("Rule %q matched %d log lines in the last %s", rule.Name, matches, (time.Duration(rule.WindowSeconds) * time.Second).String()),
			},
			"alerts": []map[string]interface{}{{
				"status": status,
				"labels": map[string]string{
					"alertname":    rule.Name,
					"severity":     strings.TrimSpace(rule.Priority),
					"service_id":   rule.ServiceID,
					"log_alert_id": rule.ID,
				},
				"annotations": map[string]string{
					"summary":     fmt.Sprintf("Log alert %s (%s)", rule.Name, status),
					"description": fmt.Sprintf("Matched log lines: %d", matches),
				},
				"startsAt": now.Format(time.RFC3339Nano),
			}},
		}
		payloadJSON, _ := json.Marshal(payload)
		_ = models.CreateAlertEvent(&models.AlertEvent{
			Status:    status,
			Receiver:  "railpush-log-alert",
			GroupKey:  "log-alert:" + rule.ID,
			AlertName: rule.Name,
			Severity:  strings.TrimSpace(rule.Priority),
			Namespace: "railpush",
			Payload:   payloadJSON,
		})
	}

	if isWebhook && strings.TrimSpace(rule.WebhookURL) != "" {
		body, _ := json.Marshal(map[string]interface{}{
			"status":        status,
			"rule_id":       rule.ID,
			"rule_name":     rule.Name,
			"service_id":    rule.ServiceID,
			"workspace_id":  rule.WorkspaceID,
			"matched_count": matches,
			"threshold":     rule.Threshold,
			"window":        (time.Duration(rule.WindowSeconds) * time.Second).String(),
			"comparison":    rule.Comparison,
			"timestamp":     now.Format(time.RFC3339Nano),
		})
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(rule.WebhookURL), strings.NewReader(string(body)))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err == nil && resp != nil {
				resp.Body.Close()
			}
		}
		cancel()
	}

	if isEmail && s.Config.Email.Enabled() {
		ws, _ := models.GetWorkspace(rule.WorkspaceID)
		if ws != nil {
			owner, _ := models.GetUserByID(ws.OwnerID)
			if owner != nil && strings.TrimSpace(owner.Email) != "" {
				subject := fmt.Sprintf("[RailPush] Log alert %s: %s", strings.ToUpper(status), strings.TrimSpace(rule.Name))
				text := fmt.Sprintf("Log alert %q is %s.\nService: %s\nMatches: %d\nThreshold: %d\nWindow: %s\n", rule.Name, status, rule.ServiceID, matches, rule.Threshold, (time.Duration(rule.WindowSeconds) * time.Second).String())
				html := "<p>Log alert <strong>" + rule.Name + "</strong> is <strong>" + status + "</strong>.</p>" +
					"<p>Service: <code>" + rule.ServiceID + "</code><br/>" +
					"Matches: " + fmt.Sprintf("%d", matches) + "<br/>" +
					"Threshold: " + fmt.Sprintf("%d", rule.Threshold) + "<br/>" +
					"Window: " + (time.Duration(rule.WindowSeconds) * time.Second).String() + "</p>"
				dedupe := fmt.Sprintf("log-alert:%s:%s:%s", rule.ID, status, owner.Email)
				_, _ = models.EnqueueEmail(dedupe, "log_alert", strings.TrimSpace(owner.Email), subject, text, html)
			}
		}
	}
}

func (s *Scheduler) Stop() { close(s.stop) }

func (s *Scheduler) checkCronJobs() {
	// In Kubernetes mode, cron execution is handled by K8s CronJobs.
	// The control plane only needs to (re)deploy CronJobs when images/config change.
	if s.Config != nil && s.Config.Kubernetes.Enabled {
		return
	}
	svcs, err := models.ListServices("")
	if err != nil {
		return
	}
	now := time.Now()
	for _, svc := range svcs {
		if (svc.Type == "cron" || svc.Type == "cron_job") && strings.TrimSpace(svc.Schedule) != "" {
			if s.shouldRun(svc.Schedule, now) {
				log.Printf("Triggering cron: %s", svc.Name)
				d := &models.Deploy{ServiceID: svc.ID, Trigger: "cron"}
				models.CreateDeploy(d)
			}
		}
	}
}

func (s *Scheduler) shouldRun(schedule string, now time.Time) bool {
	spec, err := s.parser.Parse(strings.TrimSpace(schedule))
	if err != nil {
		return false
	}
	windowEnd := now.Truncate(time.Minute)
	windowStart := windowEnd.Add(-time.Minute)
	next := spec.Next(windowStart)
	return !next.After(windowEnd)
}

// cleanupStaleData runs once per day to enforce data retention policies.
func (s *Scheduler) cleanupStaleData() {
	if time.Since(s.lastCleanup) < 24*time.Hour {
		return
	}
	s.lastCleanup = time.Now()
	if database.DB == nil {
		return
	}

	res, err := database.DB.Exec("DELETE FROM stripe_webhook_events WHERE received_at < NOW() - INTERVAL '30 days'")
	if err != nil {
		log.Printf("Scheduler: webhook events cleanup failed: %v", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("Scheduler: cleaned up %d old webhook events", n)
	}

	s.cleanupAuditLogRetention()
	s.cleanupDeployHistoryRetention()
	s.cleanupBuildLogRetention()
}

// reportMeteredUsage reports accumulated usage minutes to Stripe for all active metered billing items.
// Runs every hour. For each metered billing item, calculates active minutes since last report and
// sends a usage record to Stripe. This is what drives per-minute billing.
func (s *Scheduler) reportMeteredUsage() {
	// Run every hour.
	if time.Since(s.lastUsageSync) < time.Hour {
		return
	}
	s.lastUsageSync = time.Now()

	if s.Config == nil || strings.TrimSpace(s.Config.Stripe.SecretKey) == "" {
		return
	}

	// Only run if metered prices are configured.
	hasMeterPrices := strings.TrimSpace(s.Config.Stripe.MeteredPriceStarter) != "" ||
		strings.TrimSpace(s.Config.Stripe.MeteredPriceStandard) != "" ||
		strings.TrimSpace(s.Config.Stripe.MeteredPricePro) != ""
	if !hasMeterPrices {
		return
	}

	items, err := models.ListActiveMeteredBillingItems()
	if err != nil {
		log.Printf("Scheduler: metered usage: failed to list items: %v", err)
		return
	}
	if len(items) == 0 {
		return
	}

	stripeSvc := NewStripeService(s.Config)
	now := time.Now()
	reported := 0

	for _, item := range items {
		since := item.CreatedAt
		if item.LastUsageReportedAt != nil {
			since = *item.LastUsageReportedAt
		}

		minutes, err := models.CalcActiveMinutesSince(item.ResourceType, item.ResourceID, since, now)
		if err != nil {
			log.Printf("Scheduler: metered usage: calc failed resource=%s/%s err=%v", item.ResourceType, item.ResourceID, err)
			continue
		}
		if minutes <= 0 {
			// Still update the checkpoint so we don't recalculate the same window.
			_ = models.UpdateBillingItemLastUsageReported(item.ID, now)
			continue
		}

		if err := stripeSvc.ReportUsageMinutes(item.StripeSubscriptionItemID, minutes, now); err != nil {
			log.Printf("Scheduler: metered usage: report failed resource=%s/%s minutes=%d err=%v", item.ResourceType, item.ResourceID, minutes, err)
			continue
		}

		_ = models.UpdateBillingItemLastUsageReported(item.ID, now)
		reported++
	}

	if reported > 0 {
		log.Printf("Scheduler: metered usage: reported usage for %d/%d items", reported, len(items))
	}
}

// runNightlyBackups runs pg_dump for all managed databases once per day (between 2:00-2:59 AM server time).
// Retention cleanup is applied afterwards using per-database policies.
func (s *Scheduler) runNightlyBackups() {
	now := time.Now()

	// Only run between 2:00-2:59 AM, and at most once per day.
	if now.Hour() != 2 {
		return
	}
	if now.Sub(s.lastBackup) < 23*time.Hour {
		return
	}
	s.lastBackup = now

	if s.Config == nil {
		return
	}
	backupDir := s.Config.Deploy.BackupDir
	if backupDir == "" {
		backupDir = "/var/lib/railpush/backups"
	}
	os.MkdirAll(backupDir, 0755)

	dbs, err := models.ListManagedDatabases()
	if err != nil {
		log.Printf("Scheduler: nightly backup: failed to list databases: %v", err)
		return
	}

	for _, db := range dbs {
		if db.Status != "ready" && db.Status != "live" {
			continue
		}
		s.backupDatabase(db, backupDir, now)
	}

	// Retention cleanup
	s.cleanupOldBackups()
}

func (s *Scheduler) backupDatabase(db models.ManagedDatabase, backupDir string, now time.Time) {
	var backupID string
	err := database.DB.QueryRow(
		"INSERT INTO backups (resource_type, resource_id, status, trigger_type, started_at) VALUES ($1, $2, $3, $4, NOW()) RETURNING id",
		"database", db.ID, "running", "automated",
	).Scan(&backupID)
	if err != nil {
		log.Printf("Scheduler: backup: failed to create record for %s: %v", db.Name, err)
		return
	}

	filename := fmt.Sprintf("%s_%s.sql", db.DBName, now.Format("20060102_150405"))
	filePath := filepath.Join(backupDir, filename)

	pw := ""
	if db.EncryptedPassword != "" {
		if decrypted, err := utils.Decrypt(db.EncryptedPassword, s.Config.Crypto.EncryptionKey); err == nil {
			pw = decrypted
		}
	}

	containerName := fmt.Sprintf("sr-db-%s", db.ID[:8])

	// Use Kubernetes kubectl exec if in K8s mode, otherwise docker exec
	var out []byte
	if s.Config.Kubernetes.Enabled {
		out, err = exec.Command("kubectl", "exec", containerName, "-n", s.Config.Kubernetes.Namespace, "--",
			"pg_dump", "-U", db.Username, "-d", db.DBName, "--clean", "--if-exists").CombinedOutput()
	} else {
		out, err = exec.Command("docker", "exec",
			"-e", fmt.Sprintf("PGPASSWORD=%s", pw),
			containerName,
			"pg_dump", "-U", db.Username, "-d", db.DBName, "--clean", "--if-exists").CombinedOutput()
	}

	if err != nil {
		log.Printf("Scheduler: backup failed for %s: %v", db.Name, err)
		database.DB.Exec("UPDATE backups SET status=$1, finished_at=NOW() WHERE id=$2", "failed", backupID)
		return
	}

	if err := os.WriteFile(filePath, out, 0644); err != nil {
		log.Printf("Scheduler: backup write failed for %s: %v", db.Name, err)
		database.DB.Exec("UPDATE backups SET status=$1, finished_at=NOW() WHERE id=$2", "failed", backupID)
		return
	}

	size := int64(len(out))
	database.DB.Exec("UPDATE backups SET status=$1, file_path=$2, size_bytes=$3, finished_at=NOW() WHERE id=$4",
		"completed", filePath, size, backupID)
	log.Printf("Scheduler: nightly backup completed for %s: %s (%d bytes)", db.Name, filePath, size)
}

// cleanupOldBackups enforces per-database retention policies for completed backups.
func (s *Scheduler) cleanupOldBackups() {
	rows, err := database.DB.Query(
		`SELECT b.id,
		        b.resource_id,
		        COALESCE(b.file_path, ''),
		        COALESCE(b.started_at, b.finished_at, NOW()),
		        COALESCE(NULLIF(b.trigger_type, ''), 'manual'),
		        COALESCE(d.backup_retention_automated_days, 30),
		        COALESCE(d.backup_retention_manual_days, 365)
		   FROM backups b
		   LEFT JOIN managed_databases d ON d.id = b.resource_id
		  WHERE b.resource_type='database' AND b.status='completed'`)
	if err != nil {
		log.Printf("Scheduler: backup cleanup query failed: %v", err)
		return
	}
	defer rows.Close()

	type backupRow struct {
		id                     string
		resourceID             string
		filePath               string
		startedAt              time.Time
		triggerType            string
		automatedRetentionDays int
		manualRetentionDays    int
	}

	toDelete := make([]backupRow, 0)
	now := time.Now()
	for rows.Next() {
		var b backupRow
		if err := rows.Scan(
			&b.id,
			&b.resourceID,
			&b.filePath,
			&b.startedAt,
			&b.triggerType,
			&b.automatedRetentionDays,
			&b.manualRetentionDays,
		); err != nil {
			continue
		}

		retentionDays := b.manualRetentionDays
		if strings.EqualFold(strings.TrimSpace(b.triggerType), "automated") {
			retentionDays = b.automatedRetentionDays
		}
		if retentionDays <= 0 {
			retentionDays = 1
		}
		if now.Sub(b.startedAt) >= (time.Duration(retentionDays) * 24 * time.Hour) {
			toDelete = append(toDelete, b)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("Scheduler: backup cleanup row scan failed: %v", err)
		return
	}

	deletedByDatabase := map[string]int{}
	for _, b := range toDelete {
		if strings.TrimSpace(b.filePath) != "" {
			_ = os.Remove(b.filePath)
		}
		if _, err := database.DB.Exec("DELETE FROM backups WHERE id=$1", b.id); err == nil {
			deletedByDatabase[b.resourceID]++
		}
	}
	for dbID, count := range deletedByDatabase {
		log.Printf("Scheduler: cleaned up %d expired backups for database %s", count, dbID)
	}
}

func (s *Scheduler) cleanupAuditLogRetention() {
	res, err := database.DB.Exec(
		`DELETE FROM audit_log a
		  USING workspaces w
		 WHERE a.workspace_id = w.id
		   AND a.created_at < NOW() - make_interval(days => GREATEST(COALESCE(w.audit_log_retention_days, 365), 1))`,
	)
	if err != nil {
		log.Printf("Scheduler: audit log retention cleanup failed: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("Scheduler: cleaned up %d expired audit log rows", n)
	}
}

func (s *Scheduler) cleanupDeployHistoryRetention() {
	res, err := database.DB.Exec(
		`DELETE FROM deploys d
		  USING services s, workspaces w
		 WHERE d.service_id = s.id
		   AND s.workspace_id = w.id
		   AND LOWER(COALESCE(d.status, '')) IN ('live', 'failed', 'canceled', 'cancelled')
		   AND COALESCE(d.finished_at, d.started_at) IS NOT NULL
		   AND COALESCE(d.finished_at, d.started_at) < NOW() - make_interval(days => GREATEST(COALESCE(w.deploy_history_retention_days, 180), 1))`,
	)
	if err != nil {
		log.Printf("Scheduler: deploy history retention cleanup failed: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("Scheduler: cleaned up %d expired deploy history rows", n)
	}
}

func (s *Scheduler) cleanupBuildLogRetention() {
	res, err := database.DB.Exec(
		`UPDATE deploys d
		    SET build_log = ''
		   FROM services s
		  WHERE d.service_id = s.id
		    AND COALESCE(d.build_log, '') <> ''
		    AND COALESCE(d.started_at, d.finished_at) IS NOT NULL
		    AND COALESCE(d.started_at, d.finished_at) < NOW() - make_interval(days => GREATEST(COALESCE(s.build_log_retention_days, 90), 1))`,
	)
	if err != nil {
		log.Printf("Scheduler: build log retention cleanup failed: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("Scheduler: redacted build logs for %d expired deploy rows", n)
	}
}
