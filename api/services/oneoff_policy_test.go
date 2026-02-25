package services

import "testing"

func TestEvaluateOneOffCommand(t *testing.T) {
	tests := []struct {
		name          string
		command       string
		expectBlocked bool
		expectRisky   bool
	}{
		{name: "safe", command: "python -m app.seed", expectBlocked: false, expectRisky: false},
		{name: "blocked rm root", command: "rm -rf /", expectBlocked: true, expectRisky: true},
		{name: "risky data mutation", command: "DELETE FROM users", expectBlocked: false, expectRisky: true},
		{name: "blocked shutdown", command: "shutdown -h now", expectBlocked: true, expectRisky: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateOneOffCommand(tc.command)
			if got.Blocked != tc.expectBlocked {
				t.Fatalf("blocked=%v want %v", got.Blocked, tc.expectBlocked)
			}
			if got.Risky != tc.expectRisky {
				t.Fatalf("risky=%v want %v", got.Risky, tc.expectRisky)
			}
		})
	}
}

func TestOneOffCommandPreview(t *testing.T) {
	preview := OneOffCommandPreview("  echo   hello\nworld  ", 32)
	if preview != "echo hello world" {
		t.Fatalf("unexpected preview: %q", preview)
	}
}
