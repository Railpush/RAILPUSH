package services

import "testing"

func TestServiceIngressDisabledFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "default public", env: map[string]string{}, want: false},
		{name: "internal only true", env: map[string]string{"RAILPUSH_INTERNAL_ONLY": "true"}, want: true},
		{name: "disable public ingress one", env: map[string]string{"RAILPUSH_DISABLE_PUBLIC_INGRESS": "1"}, want: true},
		{name: "legacy disable key", env: map[string]string{"DISABLE_PUBLIC_INGRESS": "yes"}, want: true},
		{name: "visibility internal", env: map[string]string{"RAILPUSH_NETWORK_VISIBILITY": "internal"}, want: true},
		{name: "visibility private", env: map[string]string{"RAILPUSH_NETWORK_VISIBILITY": "private"}, want: true},
		{name: "visibility public", env: map[string]string{"RAILPUSH_NETWORK_VISIBILITY": "public"}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := serviceIngressDisabledFromEnv(tc.env)
			if got != tc.want {
				t.Fatalf("serviceIngressDisabledFromEnv() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseTruthyEnv(t *testing.T) {
	truthy := []string{"1", "true", "TRUE", "yes", "on", "enabled"}
	for _, raw := range truthy {
		if !parseTruthyEnv(raw) {
			t.Fatalf("expected %q to be truthy", raw)
		}
	}

	falsy := []string{"", "0", "false", "off", "no", "disabled"}
	for _, raw := range falsy {
		if parseTruthyEnv(raw) {
			t.Fatalf("expected %q to be falsy", raw)
		}
	}
}
