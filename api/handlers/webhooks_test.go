package handlers

import "testing"

func TestParseBoolString(t *testing.T) {
	trueCases := []string{"1", "true", "TRUE", " yes ", "On", "y"}
	for _, v := range trueCases {
		if !parseBoolString(v) {
			t.Fatalf("expected %q to be true", v)
		}
	}

	falseCases := []string{"", "0", "false", "no", "off", "maybe"}
	for _, v := range falseCases {
		if parseBoolString(v) {
			t.Fatalf("expected %q to be false", v)
		}
	}
}

func TestStringInFold(t *testing.T) {
	if !stringInFold("ci", []string{"Deploy", "CI"}) {
		t.Fatal("expected case-insensitive match")
	}
	if stringInFold("release", []string{"Deploy", "CI"}) {
		t.Fatal("did not expect non-matching value")
	}
	if stringInFold("", []string{"CI"}) {
		t.Fatal("empty needle should not match")
	}
}

func TestSplitPathPatterns(t *testing.T) {
	out := splitPathPatterns("apps/web/**, apps/api/**\napps/web/**\t ./infra/**")
	if len(out) != 3 {
		t.Fatalf("expected 3 unique patterns, got %d (%v)", len(out), out)
	}
	if out[0] != "apps/web/**" || out[1] != "apps/api/**" || out[2] != "infra/**" {
		t.Fatalf("unexpected split result: %v", out)
	}
}

func TestCollectPushChangedPaths(t *testing.T) {
	commits := []struct {
		Added    []string `json:"added"`
		Removed  []string `json:"removed"`
		Modified []string `json:"modified"`
	}{
		{Added: []string{"./apps/web/main.go", "apps/web/main.go"}, Modified: []string{"apps/api/server.go"}},
		{Removed: []string{"\\infra\\k8s\\deploy.yaml"}},
	}

	got := collectPushChangedPaths(commits)
	if len(got) != 3 {
		t.Fatalf("expected 3 unique normalized paths, got %d (%v)", len(got), got)
	}
	if got[0] != "apps/web/main.go" || got[1] != "apps/api/server.go" || got[2] != "infra/k8s/deploy.yaml" {
		t.Fatalf("unexpected normalized paths: %v", got)
	}
}

func TestShouldTriggerForChangedPaths(t *testing.T) {
	if !shouldTriggerForChangedPaths([]string{"apps/web/main.go"}, "apps/web/**", "") {
		t.Fatal("expected include match to trigger")
	}
	if shouldTriggerForChangedPaths([]string{"apps/api/main.go"}, "apps/web/**", "") {
		t.Fatal("expected non-include match to skip")
	}
	if shouldTriggerForChangedPaths([]string{"apps/web/main.go"}, "apps/**", "apps/web/**") {
		t.Fatal("expected excluded path to skip")
	}
	if !shouldTriggerForChangedPaths(nil, "apps/**", "apps/web/**") {
		t.Fatal("expected empty changed-path list to trigger")
	}
}
