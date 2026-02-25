package services

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
			case <-s.stop:
				ticker.Stop()
				return
			}
		}
	}()
	log.Println("Scheduler started")
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
