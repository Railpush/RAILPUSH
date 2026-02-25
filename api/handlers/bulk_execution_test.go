package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestNormalizeBulkTransactionMode(t *testing.T) {
	tests := []struct {
		name          string
		mode          string
		transactional bool
		want          string
		wantErr       bool
	}{
		{name: "default", mode: "", transactional: false, want: bulkTransactionBestEffort},
		{name: "transactional alias", mode: "", transactional: true, want: bulkTransactionAllOrNothing},
		{name: "explicit all or nothing", mode: "all_or_nothing", transactional: false, want: bulkTransactionAllOrNothing},
		{name: "explicit best effort", mode: "best_effort", transactional: true, want: bulkTransactionBestEffort},
		{name: "invalid", mode: "serializable", transactional: false, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeBulkTransactionMode(tc.mode, tc.transactional)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestIsDryRunRequest(t *testing.T) {
	req := httptest.NewRequest("POST", "http://example.com/api/v1/services/bulk-update", nil)
	if isDryRunRequest(req) {
		t.Fatalf("expected false for default request")
	}

	req.Header.Set(bulkDryRunHeader, "true")
	if !isDryRunRequest(req) {
		t.Fatalf("expected true for header dry run")
	}

	req2 := httptest.NewRequest("POST", "http://example.com/api/v1/services/bulk-update?dry_run=1", nil)
	if !isDryRunRequest(req2) {
		t.Fatalf("expected true for query dry_run")
	}
}
