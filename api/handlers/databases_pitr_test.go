package handlers

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeDatabaseRestoreTarget(t *testing.T) {
	tests := []struct {
		raw   string
		want  string
		valid bool
	}{
		{raw: "", want: databaseRestoreTargetNewDatabase, valid: true},
		{raw: "new_database", want: databaseRestoreTargetNewDatabase, valid: true},
		{raw: "new", want: databaseRestoreTargetNewDatabase, valid: true},
		{raw: "in_place", want: databaseRestoreTargetInPlace, valid: true},
		{raw: "in-place", want: databaseRestoreTargetInPlace, valid: true},
		{raw: "invalid", want: "", valid: false},
	}

	for _, tc := range tests {
		got, ok := normalizeDatabaseRestoreTarget(tc.raw)
		if ok != tc.valid {
			t.Fatalf("normalizeDatabaseRestoreTarget(%q) valid=%v want %v", tc.raw, ok, tc.valid)
		}
		if got != tc.want {
			t.Fatalf("normalizeDatabaseRestoreTarget(%q)=%q want %q", tc.raw, got, tc.want)
		}
	}
}

func TestDefaultRestoredDatabaseName(t *testing.T) {
	now := time.Date(2026, 2, 25, 15, 30, 45, 0, time.UTC)
	name := defaultRestoredDatabaseName("My Main DB", now)
	if !strings.HasPrefix(name, "my-main-db-restored-") {
		t.Fatalf("unexpected restore name prefix: %q", name)
	}
	if len(name) > 63 {
		t.Fatalf("restore name too long: %d", len(name))
	}
}

func TestComputeDatabaseRecoveryWindow(t *testing.T) {
	now := time.Date(2026, 2, 25, 16, 0, 0, 0, time.UTC)
	backups := []databaseBackupSnapshot{
		{RestorePoint: now.Add(-10 * 24 * time.Hour)},
		{RestorePoint: now.Add(-1 * 24 * time.Hour)},
	}

	earliest, latest := computeDatabaseRecoveryWindow(backups, 7, now, "available")
	if earliest == nil || latest == nil {
		t.Fatalf("expected recovery window to be present")
	}
	wantEarliest := now.Add(-7 * 24 * time.Hour)
	if !earliest.Equal(wantEarliest) {
		t.Fatalf("earliest=%s want %s", earliest.Format(time.RFC3339), wantEarliest.Format(time.RFC3339))
	}
	if !latest.Equal(now) {
		t.Fatalf("latest=%s want %s", latest.Format(time.RFC3339), now.Format(time.RFC3339))
	}

	earliest2, latest2 := computeDatabaseRecoveryWindow(backups, 7, now, "failed")
	if earliest2 == nil || latest2 == nil {
		t.Fatalf("expected recovery window for failed status")
	}
	if !latest2.Equal(backups[len(backups)-1].RestorePoint) {
		t.Fatalf("failed status latest=%s want %s", latest2.Format(time.RFC3339), backups[len(backups)-1].RestorePoint.Format(time.RFC3339))
	}
}

func TestPointInTimeRequestDetection(t *testing.T) {
	if (pointInTimeRestoreRequest{}).hasPointInTimeFields() {
		t.Fatalf("empty request should not be treated as point-in-time restore")
	}
	req := pointInTimeRestoreRequest{TargetTime: "2026-02-24T14:46:00Z"}
	if !req.hasPointInTimeFields() {
		t.Fatalf("target_time should trigger point-in-time restore mode")
	}
}
