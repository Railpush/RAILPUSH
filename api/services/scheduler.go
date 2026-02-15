package services

import (
	"log"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	Config *config.Config
	stop   chan struct{}
	parser cron.Parser
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
