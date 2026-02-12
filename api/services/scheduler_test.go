package services

import (
	"testing"
	"time"
)

func TestSchedulerShouldRunMinuteAligned(t *testing.T) {
	s := NewScheduler(nil)
	now := time.Date(2026, 2, 12, 3, 0, 15, 0, time.UTC)
	if !s.shouldRun("0 3 * * *", now) {
		t.Fatalf("expected schedule to run at minute boundary")
	}
}

func TestSchedulerShouldNotRunOutsideWindow(t *testing.T) {
	s := NewScheduler(nil)
	now := time.Date(2026, 2, 12, 3, 1, 10, 0, time.UTC)
	if s.shouldRun("0 3 * * *", now) {
		t.Fatalf("did not expect schedule to run outside target minute")
	}
}

func TestSchedulerShouldRunIntervalSpec(t *testing.T) {
	s := NewScheduler(nil)
	now := time.Date(2026, 2, 12, 12, 10, 0, 0, time.UTC)
	if !s.shouldRun("*/5 * * * *", now) {
		t.Fatalf("expected 5-minute schedule to run at minute 10")
	}
}

func TestSchedulerShouldRejectInvalidSpec(t *testing.T) {
	s := NewScheduler(nil)
	now := time.Now().UTC()
	if s.shouldRun("not-a-cron", now) {
		t.Fatalf("invalid cron spec should not run")
	}
}
