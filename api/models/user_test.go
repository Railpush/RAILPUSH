package models

import "testing"

func TestNormalizeAndValidateCIDRAllowlist(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    []string
		wantErr bool
	}{
		{name: "empty", input: nil, want: nil},
		{name: "single ipv4", input: []string{"203.0.113.10"}, want: []string{"203.0.113.10/32"}},
		{name: "single ipv6", input: []string{"2001:db8::1"}, want: []string{"2001:db8::1/128"}},
		{name: "cidr + dedupe", input: []string{"10.0.0.0/8", "10.0.0.0/8", "  "}, want: []string{"10.0.0.0/8"}},
		{name: "invalid ip", input: []string{"not-an-ip"}, wantErr: true},
		{name: "invalid cidr", input: []string{"10.0.0.0/99"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeAndValidateCIDRAllowlist(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len=%d want %d (%v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got[%d]=%q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
