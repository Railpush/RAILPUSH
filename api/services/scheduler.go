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
	Config      *config.Config
	stop        chan struct{}
	parser      cron.Parser
	lastCleanup time.Time
	lastBackup  time.Time
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

// cleanupStaleData runs once per day to purge old webhook events (30-day retention).
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
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("Scheduler: cleaned up %d old webhook events", n)
	}
}

// runNightlyBackups runs pg_dump for all managed databases once per day (between 2:00-2:59 AM server time).
// Retention: keep last 7 daily backups per database, plus 4 weekly (Sunday) backups.
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
		"INSERT INTO backups (resource_type, resource_id, status, started_at) VALUES ($1, $2, $3, NOW()) RETURNING id",
		"database", db.ID, "running",
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

// cleanupOldBackups enforces retention: keep 7 daily + 4 weekly (most recent) per database.
func (s *Scheduler) cleanupOldBackups() {
	rows, err := database.DB.Query(
		`SELECT id, resource_id, file_path, started_at FROM backups
		 WHERE resource_type='database' AND status='completed'
		 ORDER BY resource_id, started_at DESC`)
	if err != nil {
		log.Printf("Scheduler: backup cleanup query failed: %v", err)
		return
	}
	defer rows.Close()

	type backupRow struct {
		id         string
		resourceID string
		filePath   string
		startedAt  time.Time
	}

	grouped := map[string][]backupRow{}
	for rows.Next() {
		var b backupRow
		if err := rows.Scan(&b.id, &b.resourceID, &b.filePath, &b.startedAt); err != nil {
			continue
		}
		grouped[b.resourceID] = append(grouped[b.resourceID], b)
	}

	now := time.Now()
	for _, backups := range grouped {
		var toDelete []backupRow
		dailyKept := 0
		weeklyKept := 0
		for _, b := range backups {
			age := now.Sub(b.startedAt)
			isSunday := b.startedAt.Weekday() == time.Sunday

			if age < 7*24*time.Hour && dailyKept < 7 {
				dailyKept++
				continue
			}
			if isSunday && age < 28*24*time.Hour && weeklyKept < 4 {
				weeklyKept++
				continue
			}
			toDelete = append(toDelete, b)
		}

		for _, b := range toDelete {
			os.Remove(b.filePath)
			database.DB.Exec("DELETE FROM backups WHERE id=$1", b.id)
		}
		if len(toDelete) > 0 {
			log.Printf("Scheduler: cleaned up %d old backups for database %s", len(toDelete), backups[0].resourceID)
		}
	}
}
