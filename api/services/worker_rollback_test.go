package services

import (
	"testing"

	"github.com/railpush/api/models"
)

func TestShouldAutoRollbackReason(t *testing.T) {
	tests := []struct {
		reason string
		want   bool
	}{
		{reason: "health_check_failed", want: true},
		{reason: "rollout_failed", want: true},
		{reason: "", want: false},
		{reason: "build_failed", want: false},
	}
	for _, tc := range tests {
		if got := shouldAutoRollbackReason(tc.reason); got != tc.want {
			t.Fatalf("shouldAutoRollbackReason(%q)=%v want %v", tc.reason, got, tc.want)
		}
	}
}

func TestSelectRollbackDeployCandidate(t *testing.T) {
	failed := &models.Deploy{ID: "d3", ImageTag: "img:new"}
	deploys := []models.Deploy{
		{ID: "d3", Status: "failed", ImageTag: "img:new"},
		{ID: "d2", Status: "failed", ImageTag: "img:bad"},
		{ID: "d1", Status: "live", ImageTag: "img:good"},
	}
	got := selectRollbackDeployCandidate(deploys, failed)
	if got == nil {
		t.Fatalf("expected candidate")
	}
	if got.ID != "d1" {
		t.Fatalf("candidate id=%q want d1", got.ID)
	}

	deploysNoCandidate := []models.Deploy{
		{ID: "d3", Status: "failed", ImageTag: "img:new"},
		{ID: "d2", Status: "live", ImageTag: "img:new"},
	}
	if got := selectRollbackDeployCandidate(deploysNoCandidate, failed); got != nil {
		t.Fatalf("expected nil candidate, got %+v", *got)
	}
}
