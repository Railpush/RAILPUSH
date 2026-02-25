package handlers

import "testing"

func TestParseGitHubOwnerRepo(t *testing.T) {
	tests := []struct {
		name     string
		repoURL  string
		owner    string
		repoName string
	}{
		{
			name:     "https with git suffix",
			repoURL:  "https://github.com/octocat/hello-world.git",
			owner:    "octocat",
			repoName: "hello-world",
		},
		{
			name:     "https without git suffix",
			repoURL:  "https://github.com/octocat/hello-world",
			owner:    "octocat",
			repoName: "hello-world",
		},
		{
			name:     "ssh with git suffix",
			repoURL:  "git@github.com:octocat/hello-world.git",
			owner:    "octocat",
			repoName: "hello-world",
		},
		{
			name:     "ssh without git suffix",
			repoURL:  "git@github.com:octocat/hello-world",
			owner:    "octocat",
			repoName: "hello-world",
		},
		{
			name:     "trim whitespace",
			repoURL:  "  https://github.com/octocat/hello-world.git  ",
			owner:    "octocat",
			repoName: "hello-world",
		},
		{
			name:     "invalid host",
			repoURL:  "https://gitlab.com/octocat/hello-world.git",
			owner:    "",
			repoName: "",
		},
		{
			name:     "invalid ssh format",
			repoURL:  "git@github.com:octocat",
			owner:    "",
			repoName: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			owner, repo := ParseGitHubOwnerRepo(tc.repoURL)
			if owner != tc.owner || repo != tc.repoName {
				t.Fatalf("expected (%q, %q), got (%q, %q)", tc.owner, tc.repoName, owner, repo)
			}
		})
	}
}
